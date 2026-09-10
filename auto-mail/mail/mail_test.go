package mail_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/internal/store"
	"github.com/mistakenot/auto-mail/mail"
)

// moduleRoot is auto-mail/ — this test file lives in auto-mail/mail/.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %q has no go.mod: %v", root, err)
	}
	return root
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no Go files under %s", root)
	}
	return out
}

// messageWord matches "message" as a whole word component of an identifier,
// case-insensitively and across camelCase boundaries (Message, mailMessage,
// MessageID, messages).
var messageWord = regexp.MustCompile(`(?i)(^|[^a-z])message`)

// TestNoMessageEntity is the greppable half of G16 (AC-1): nothing in auto-mail
// names the stored unit a message. That word is already bound to "a single
// role-tagged exchange within a Session" in the ubiquitous language, which is
// the collision D-4 exists to avoid. The stored unit is Mail.
//
// The check is scoped to names that outlive a function: declared types,
// functions and methods, struct field names, and JSON field tags. The one
// allowed use is the `--message` body flag and the {"message": …} body key it
// writes — those name the *body's own field*, not the entity — so string
// literals and local variables are deliberately out of scope.
func TestNoMessageEntity(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	for _, path := range goFiles(t, root) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)

		report := func(kind, name string, pos token.Pos) {
			if messageWord.MatchString(name) {
				t.Errorf("%s:%d: %s %q names the stored unit a message — "+
					"use Mail (docs/concepts/UBIQUITOUS_LANGUAGE.md); "+
					"`--message` is the body field's own name and stays a flag",
					rel, fset.Position(pos).Line, kind, name)
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.TypeSpec:
				report("type", node.Name.Name, node.Name.Pos())
			case *ast.FuncDecl:
				report("func", node.Name.Name, node.Name.Pos())
			case *ast.StructType:
				for _, field := range node.Fields.List {
					for _, name := range field.Names {
						report("struct field", name.Name, name.Pos())
					}
					if field.Tag == nil {
						continue
					}
					tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
					jsonName, _, _ := strings.Cut(tag.Get("json"), ",")
					if jsonName != "" {
						report("json field", jsonName, field.Tag.Pos())
					}
				}
			}
			return true
		})
	}
}

// sqlMessageTable matches a SQL statement naming a `message`/`messages` table.
var sqlMessageTable = regexp.MustCompile(`(?i)\b(create\s+table(\s+if\s+not\s+exists)?|insert\s+into|update|from|join)\s+messages?\b`)

// alphaEventType matches an event-type string literal in the module.
var alphaEventType = regexp.MustCompile(`"(alpha\.[a-z.]+)"`)

// TestSchemaAndEventTypesUseTheMailVocabulary: no table is named message, and
// every event type carries the `alpha.` prefix G10 requires and the `mail`
// noun D-4 settled.
func TestSchemaAndEventTypesUseTheMailVocabulary(t *testing.T) {
	root := moduleRoot(t)
	for _, path := range goFiles(t, root) {
		if strings.HasSuffix(path, "mail_test.go") {
			continue // this file names the forbidden word in its own patterns
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		if loc := sqlMessageTable.FindString(string(src)); loc != "" {
			t.Errorf("%s: SQL names a message table (%q) — the stored unit is `mail`", rel, loc)
		}
		for _, m := range alphaEventType.FindAllStringSubmatch(string(src), -1) {
			if !strings.HasPrefix(m[1], "alpha.mail.") {
				t.Errorf("%s: event type %q is not under the alpha.mail.* namespace", rel, m[1])
			}
			if messageWord.MatchString(m[1]) {
				t.Errorf("%s: event type %q names a message — the stored unit is mail", rel, m[1])
			}
		}
	}
}

// TestNewDirectOpensTheAlphaStore: the alpha marker is in the store filename,
// not only in the docs (G10 / D-2), and the client needs no daemon (D-11).
func TestNewDirectOpensTheAlphaStore(t *testing.T) {
	home := t.TempDir()

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	want := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("store not created at %s: %v", want, err)
	}

	deliveries, err := client.List(context.Background(), mail.ListInput{})
	if err != nil {
		t.Fatalf("List on a fresh store: %v", err)
	}
	if deliveries == nil {
		t.Error("List returned a nil slice; want an empty one so it marshals as []")
	}
	if len(deliveries) != 0 {
		t.Errorf("List returned %d deliveries on a fresh store, want 0", len(deliveries))
	}
}

// TestResetWipesTheAlphaStore: G10 makes wiping a supported operation rather
// than a workaround — there are no upcasters and no migrations, so "start
// again" *is* the migration path. A store that still holds events is refused
// without Force, so a reset can never quietly discard unacked mail.
func TestResetWipesTheAlphaStore(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}

	binding := mail.Binding{Manager: "cwd", Target: filepath.Join(home, "workspace")}
	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs", Binding: binding}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := client.Send(ctx, mail.SendInput{
		To:      "auto-web/bugs",
		From:    "auto-stack/reviewer",
		Body:    map[string]any{"message": "hi"},
		Binding: binding,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	if _, err := client.Reset(ctx, mail.ResetInput{}); !errors.Is(err, mail.ErrStoreNotEmpty) {
		t.Fatalf("Reset on a store holding mail = %v, want ErrStoreNotEmpty", err)
	}

	result, err := client.Reset(ctx, mail.ResetInput{Force: true})
	if err != nil {
		t.Fatalf("Reset --yes: %v", err)
	}
	storePath := config.StorePathIn(home)
	flagsDir := config.FlagsDirIn(home)
	if !slices.Contains(result.Removed, storePath) {
		t.Errorf("removed = %v, want it to name the store %q", result.Removed, storePath)
	}
	if !slices.Contains(result.Removed, flagsDir) {
		t.Errorf("removed = %v, want it to name the flag directory %q", result.Removed, flagsDir)
	}
	for _, path := range []string{storePath, flagsDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists after reset (stat err = %v)", path, err)
		}
	}
	_ = client.Close()

	// And the tool works normally afterwards, from an empty state.
	fresh, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect after reset: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	deliveries, err := fresh.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List after reset: %v", err)
	}
	if len(deliveries) != 0 {
		t.Errorf("List after reset returned %d deliveries, want 0", len(deliveries))
	}
	// An empty store needs no Force: nothing can be lost.
	if _, err := fresh.Reset(ctx, mail.ResetInput{}); err != nil {
		t.Errorf("Reset on an empty store = %v, want it to succeed without Force", err)
	}
}

// TestValidateAddressIsPermissive: addresses are virtual and free-form (D-9),
// so validation rejects only what cannot round-trip or cannot have been meant.
// Everything else — including a `/`, which is an ordinary character and never a
// hierarchy — is accepted verbatim.
func TestValidateAddressIsPermissive(t *testing.T) {
	accepted := []string{
		"auto-web/bugs",       // C1's address; the separator is allowed
		"bugs",                // no separator at all is equally valid
		"a/b/c/d",             // and neither is the count of separators
		"Auto-Web/Bugs",       // case is not normalised
		"team.alpha+urgent#1", // free-form means free-form
		strings.Repeat("x", mail.MaxAddressLength),
	}
	for _, address := range accepted {
		if err := mail.ValidateAddress(address); err != nil {
			t.Errorf("ValidateAddress(%q) = %v, want nil", address, err)
		}
	}

	rejected := map[string]string{
		"empty":              "",
		"leading space":      " auto-web/bugs",
		"trailing space":     "auto-web/bugs ",
		"trailing newline":   "auto-web/bugs\n",
		"embedded control":   "auto-web/\x00bugs",
		"embedded newline":   "auto-web/\nbugs",
		"over maximum bytes": strings.Repeat("x", mail.MaxAddressLength+1),
	}
	for name, address := range rejected {
		err := mail.ValidateAddress(address)
		if err == nil {
			t.Errorf("ValidateAddress(%q) [%s] = nil, want an error", address, name)
			continue
		}
		if !errors.Is(err, mail.ErrInvalidAddress) {
			t.Errorf("ValidateAddress(%q) [%s] = %v, want ErrInvalidAddress", address, name, err)
		}
		// Every hard error carries a remediation hint, not just a diagnosis.
		if len(strings.Fields(err.Error())) < 5 {
			t.Errorf("ValidateAddress(%q) [%s] error %q has no remediation", address, name, err)
		}
	}
}

// TestBindingLadder walks the three rungs D-062-2 documents. The pair is opaque
// on every rung — a manager is a data value, so T3 adds one without a schema
// change — and the cwd rung is what makes the harness scenario possible, since
// a container has no tmux.
func TestBindingLadder(t *testing.T) {
	cwd := t.TempDir()

	tmux := mail.BindingFromContext(map[string]string{
		"TMUX":               "/tmp/tmux-1000/default,123,0",
		"tmux_pane_id":       "%7",
		"tmux_session":       "planners",
		"NTM_SPAWN_BATCH_ID": "batch-9",
	}, cwd)
	if tmux.Manager != mail.ManagerTmux || tmux.Target != "%7" {
		t.Errorf("tmux rung = %+v, want manager=tmux target=%%7", tmux)
	}
	if tmux.Session != "planners" {
		t.Errorf("tmux rung session = %q, want %q", tmux.Session, "planners")
	}

	ntm := mail.BindingFromContext(map[string]string{
		"NTM_SPAWN_BATCH_ID": "batch-9",
		"NTM_SPAWN_ORDER":    "3",
	}, cwd)
	if ntm.Manager != mail.ManagerNTM || ntm.Target != "batch-9/3" {
		t.Errorf("ntm rung = %+v, want manager=ntm target=batch-9/3", ntm)
	}

	fallback := mail.BindingFromContext(nil, cwd)
	if fallback.Manager != mail.ManagerCwd {
		t.Errorf("fallback rung = %+v, want manager=cwd", fallback)
	}
	if fallback.Target == "" {
		t.Error("fallback rung has an empty target; nothing could ever match it")
	}

	// Two spellings of one directory must read as one agent, not two.
	nested := filepath.Join(cwd, "sub", "..")
	if got := mail.BindingFromContext(nil, nested); got.Target != fallback.Target {
		t.Errorf("BindingFromContext(nil, %q).Target = %q, want %q", nested, got.Target, fallback.Target)
	}
}

// TestNoPhysicalIdentityInStoredMail is G5 made executable: a sender running
// under tmux and ntm must leave no trace of either in what is stored. Physical
// context belongs on the bindings row, as the opaque pair, and nowhere else —
// otherwise an address would silently become a machine reference and the
// virtual-address guarantee would be gone.
func TestNoPhysicalIdentityInStoredMail(t *testing.T) {
	home := t.TempDir()
	// Every fixture value is deliberately distinctive: the assertion below is a
	// substring search over the stored row, so a short or numeric value (an
	// order of "7") would match a digit of the timestamp and fail for the wrong
	// reason.
	physical := map[string]string{
		"TMUX":               "/tmp/tmux-1000/default,4242,0",
		"TMUX_PANE":          "%42",
		"tmux_pane_id":       "%42",
		"tmux_session":       "planners-4242",
		"NTM_SPAWN_BATCH_ID": "ntm-batch-4242",
		"NTM_SPAWN_ORDER":    "ntm-order-7",
	}
	binding := mail.BindingFromContext(physical, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs", Binding: binding}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sent, err := client.Send(ctx, mail.SendInput{
		To:      "auto-web/bugs",
		Body:    map[string]any{"message": "the port is dropped on ssh:// URLs"},
		Binding: binding,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The resolved from-address is rung 2 — a virtual address, not a pane.
	listed, err := client.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List returned %d deliveries, want 1", len(listed))
	}
	if listed[0].From != "auto-web/bugs" {
		t.Errorf("from = %q, want the subscription's address (rung 2 of the ladder)", listed[0].From)
	}

	// Read the row itself, not the client's view of it: the guarantee is about
	// what is persisted. The read goes through internal/store rather than a
	// second sqlite handle, because "nothing outside the store package opens
	// the mail store" (G11) is a rule this test is not exempt from.
	st, err := store.Open(config.StorePathIn(home))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var toAddress, envelope, body string
	if err := st.QueryRowContext(ctx,
		`SELECT to_address, envelope, body FROM mail WHERE id = ?`, sent.ID).
		Scan(&toAddress, &envelope, &body); err != nil {
		t.Fatalf("read mail row: %v", err)
	}
	row := toAddress + "\x00" + envelope + "\x00" + body
	for key, value := range physical {
		if strings.Contains(row, value) {
			t.Errorf("the stored mail row contains the physical identity %s=%q: %s", key, value, row)
		}
	}

	// And it is present exactly where it should be: the bindings row.
	var manager, target string
	if err := st.QueryRowContext(ctx, `SELECT manager, target FROM bindings LIMIT 1`).Scan(&manager, &target); err != nil {
		t.Fatalf("read binding row: %v", err)
	}
	if manager != mail.ManagerTmux || target != "%42" {
		t.Errorf("binding row = (%q, %q), want the opaque tmux pair", manager, target)
	}
}

// TestHasPendingOpensNoStore is AC-10's central claim, made assertable: the
// hook's mail check costs one stat and opens the store on **neither** path.
//
// A timing assertion could not say this — a fast machine passes a stopwatch
// test that opens SQLite anyway — so the assertion is structural: the package's
// only store opener is counted, and the count must stay at zero across a
// present flag, an absent flag, an absent flag directory and an unreadable one.
func TestHasPendingOpensNoStore(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	opens, restore := mail.CountStoreOpens()
	t.Cleanup(restore)

	// Absent flag directory entirely — the state of a host that never ran
	// `auto mail init`.
	if mail.HasPending(home, binding) {
		t.Error("HasPending on a home with no mail directory = true, want false")
	}

	// Present flag.
	if err := mail.SetPendingFor(home, binding); err != nil {
		t.Fatalf("SetPendingFor: %v", err)
	}
	if !mail.HasPending(home, binding) {
		t.Error("HasPending with the flag present = false, want true")
	}

	// Absent flag, present directory.
	other := mail.BindingFromContext(nil, t.TempDir())
	if mail.HasPending(home, other) {
		t.Error("HasPending for an unflagged binding = true, want false")
	}

	// An unreadable flag directory reads as "no mail" rather than as an error
	// the hook would have to handle.
	unreadable := t.TempDir()
	if err := mail.SetPendingFor(unreadable, binding); err != nil {
		t.Fatalf("SetPendingFor: %v", err)
	}
	dir := filepath.Dir(mail.FlagPathFor(unreadable, binding))
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if os.Geteuid() != 0 {
		// Root ignores the mode bits, so the assertion is only meaningful for
		// an ordinary user; the call itself must be safe either way.
		if mail.HasPending(unreadable, binding) {
			t.Error("HasPending on an unreadable flag directory = true, want false")
		}
	} else {
		_ = mail.HasPending(unreadable, binding)
	}

	// An empty binding names nothing and must never match a shared flag.
	if mail.HasPending(home, mail.Binding{}) {
		t.Error("HasPending for an empty binding = true, want false")
	}

	if got := opens(); got != 0 {
		t.Errorf("the mail check opened the store %d times, want 0 — the hook "+
			"path must cost one stat (G8/AC-10)", got)
	}
}

// TestNudgeTextCarriesNoMailboxContent: the nudge is a constant instruction to
// go and read mail, never a rendering of what is waiting. It lands in another
// agent's context window and the sender controls the mail, so interpolating a
// sender, an address, a subject or a body would make the sender an author of
// the recipient's context (G14's content rule).
func TestNudgeTextCarriesNoMailboxContent(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	before := mail.NudgeText()
	if !strings.Contains(before, "auto mail list") {
		t.Errorf("NudgeText() = %q, want it to name `auto mail list`", before)
	}
	if !strings.Contains(before, "auto mail ack") {
		t.Errorf("NudgeText() = %q, want it to name `auto mail ack`", before)
	}

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	const secret = "SENDER-CONTROLLED-STRING-4242"
	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: secret, Binding: binding}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := client.Send(ctx, mail.SendInput{
		To:      secret,
		From:    secret,
		Body:    map[string]any{"message": secret},
		Binding: binding,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := mail.NudgeText(); got != before {
		t.Errorf("NudgeText() changed after a send: %q, want the constant %q", got, before)
	}
	if strings.Contains(mail.NudgeText(), secret) {
		t.Errorf("NudgeText() interpolated sender-controlled content: %q", mail.NudgeText())
	}
}

// TestPendingFlagLifecycle walks send → list → ack. The flag is what a hook
// stats, so its lifecycle is the notification path's whole contract: raised for
// every bound subscription a send delivered to, still raised after a read
// (reading never retires mail, G3), and lowered only once the binding has
// nothing unacked left.
func TestPendingFlagLifecycle(t *testing.T) {
	home := t.TempDir()
	reader := mail.BindingFromContext(nil, t.TempDir())
	sender := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if mail.HasPending(home, reader) {
		t.Fatal("a fresh home reports pending mail")
	}

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs", Binding: reader}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if mail.HasPending(home, reader) {
		t.Error("subscribing raised the flag; only a send may raise it")
	}

	sent, err := client.Send(ctx, mail.SendInput{
		To:      "auto-web/bugs",
		From:    "auto-stack/reviewer",
		Body:    map[string]any{"message": "the port is dropped on ssh:// URLs"},
		Binding: sender,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !mail.HasPending(home, reader) {
		t.Fatal("send did not raise the reader's flag; no hook would ever nudge")
	}
	if mail.HasPending(home, sender) {
		t.Error("send raised the sender's own flag; it subscribes to nothing")
	}

	// Reading does not retire mail (G3), so the flag survives a list.
	listed, err := client.List(ctx, mail.ListInput{Binding: reader})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List returned %d deliveries, want 1", len(listed))
	}
	if !mail.HasPending(home, reader) {
		t.Error("listing lowered the flag while the mail was still unacked")
	}

	if _, err := client.Ack(ctx, mail.AckInput{MailID: sent.ID, Binding: reader}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if mail.HasPending(home, reader) {
		t.Error("the flag survived the ack that emptied the mailbox")
	}
}

// TestPendingFlagStaysUpWhileAnythingIsUnacked: the flag means "there may be
// something here", so it may only fall when the binding has nothing left. Two
// mail items, one ack, and it must still be raised.
func TestPendingFlagStaysUpWhileAnythingIsUnacked(t *testing.T) {
	home := t.TempDir()
	reader := mail.BindingFromContext(nil, t.TempDir())
	sender := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs", Binding: reader}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	var ids []string
	for _, text := range []string{"first", "second"} {
		sent, err := client.Send(ctx, mail.SendInput{
			To: "auto-web/bugs", From: "auto-stack/reviewer",
			Body: map[string]any{"message": text}, Binding: sender,
		})
		if err != nil {
			t.Fatalf("Send(%s): %v", text, err)
		}
		ids = append(ids, sent.ID)
	}

	if _, err := client.Ack(ctx, mail.AckInput{MailID: ids[0], Binding: reader}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if !mail.HasPending(home, reader) {
		t.Error("the flag fell with one mail still unacked")
	}
	if _, err := client.Ack(ctx, mail.AckInput{MailID: ids[1], Binding: reader}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if mail.HasPending(home, reader) {
		t.Error("the flag survived the ack that emptied the mailbox")
	}
}

// TestFalsePositiveFlagHealsItself is the other half of D-062-3's bounded
// drift. The flag is authoritative for the *decision to nudge* — which is safe
// precisely because the nudge carries no mailbox content — so a wrong flag can
// neither leak nor misstate anything. It costs the agent one wasted
// `auto mail list`, and that list is what removes it.
func TestFalsePositiveFlagHealsItself(t *testing.T) {
	home := t.TempDir()
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	// Nothing was ever sent to this binding; the flag is a pure false positive.
	if err := mail.SetPendingFor(home, binding); err != nil {
		t.Fatalf("SetPendingFor: %v", err)
	}
	if !mail.HasPending(home, binding) {
		t.Fatal("the planted flag is not readable")
	}

	listed, err := client.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("List returned %d deliveries for a false-positive flag, want 0", len(listed))
	}
	if mail.HasPending(home, binding) {
		t.Error("the false-positive flag survived the list it caused; drift is sticky, not self-healing")
	}
}

// TestPendingFlagIsPerBinding: two agents on one host have independent flags,
// so mail for one never nudges the other. The cwd rung is what makes this
// observable in a container (D-062-2).
func TestPendingFlagIsPerBinding(t *testing.T) {
	home := t.TempDir()
	a := mail.BindingFromContext(nil, t.TempDir())
	b := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs", Binding: b}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := client.Send(ctx, mail.SendInput{
		To: "auto-web/bugs", From: "auto-stack/reviewer",
		Body: map[string]any{"message": "for b only"}, Binding: a,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !mail.HasPending(home, b) {
		t.Error("the subscriber's flag was not raised")
	}
	if mail.HasPending(home, a) {
		t.Error("the sender's flag was raised; flags are per binding")
	}
	if mail.FlagPathFor(home, a) == mail.FlagPathFor(home, b) {
		t.Error("two distinct bindings hash to one flag path")
	}
}

// TestParentHandleResolvesToTheSupervisorAndIsNeverStored is AC-1.
//
// A Subagent knows no address — it is handed a prompt, not an identity — so the
// whole of what it supplies is the literal `#parent`. The client answers with
// the supervisor's absolute address, and the *stored* row must carry that
// address and nothing else: a Handle names a recipient at a moment, and a
// stored moment is meaningless later (G5).
func TestParentHandleResolvesToTheSupervisorAndIsNeverStored(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	// The supervisor and its Subagent share a pane and a working directory, so
	// they compute the same binding — which is the entire mechanism.
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{
		Address: "auto-stack/supervisor",
		Binding: binding,
	}); err != nil {
		t.Fatalf("the supervisor could not subscribe: %v", err)
	}

	// The hook has seen the Subagent act under this binding; that is the whole
	// of its self-identification.
	mail.ObserveHookEvent(home, binding, "PreToolUse", mail.ActiveAgent{
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
		SessionID: "the-supervisor-session",
	})

	sent, err := client.Send(ctx, mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "phase 3 blocked: the fixture has no agent_id"},
		Binding: binding,
		Sender:  mail.CallerSender(home, binding),
	})
	if err != nil {
		t.Fatalf("send to %s: %v", mail.HandleParent, err)
	}
	if sent.To != "auto-stack/supervisor" {
		t.Errorf("to = %q, want the supervisor's absolute address", sent.To)
	}
	if sent.ResolvedFrom != mail.HandleParent {
		t.Errorf("resolvedFrom = %q, want %q", sent.ResolvedFrom, mail.HandleParent)
	}
	if sent.Subscriptions != 1 || sent.Bound != 1 {
		t.Errorf("subscriptions/bound = %d/%d, want 1/1", sent.Subscriptions, sent.Bound)
	}

	// The supervisor reads it, and reading does not retire it (G3).
	listed, err := client.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sent.ID {
		t.Fatalf("the supervisor listed %+v, want the mail %s", listed, sent.ID)
	}

	// And nothing beginning with `#` reached the store — not the mail row, not
	// the envelope, not a subscription address.
	st, err := store.Open(config.StorePathIn(home))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var toAddress, envelope string
	if err := st.QueryRowContext(ctx,
		`SELECT to_address, envelope FROM mail WHERE id = ?`, sent.ID).Scan(&toAddress, &envelope); err != nil {
		t.Fatalf("read mail row: %v", err)
	}
	if toAddress != "auto-stack/supervisor" {
		t.Errorf("stored to_address = %q, want the resolved absolute address", toAddress)
	}
	if strings.Contains(envelope, mail.HandlePrefix) {
		t.Errorf("the stored envelope carries a handle: %s", envelope)
	}
	// Nor is the Subagent's physical identity anywhere in the row (G5).
	for _, physical := range []string{"a84a3676a847c5c0b", "the-supervisor-session"} {
		if strings.Contains(toAddress+"\x00"+envelope, physical) {
			t.Errorf("the stored row carries the physical identity %q", physical)
		}
	}

	var addresses int
	if err := st.QueryRowContext(ctx,
		`SELECT count(*) FROM subscriptions WHERE address LIKE '#%'`).Scan(&addresses); err != nil {
		t.Fatalf("count handle-shaped subscriptions: %v", err)
	}
	if addresses != 0 {
		t.Errorf("%d subscription addresses begin with `#`; a handle is never an address", addresses)
	}
}

// TestParentHandleRefusesANonSubagent is AC-3's first half at the seam.
//
// The refusal is what makes the handle safe to offer: a process that cannot
// tell whether it is a Subagent must not be allowed to act as one, because the
// alternative is mailing a stranger's supervisor. The token is bare and the
// remediation is on the wrapped error, so a caller branches on the first and a
// user reads the second.
func TestParentHandleRefusesANonSubagent(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{
		Address: "auto-stack/supervisor",
		Binding: binding,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// No marker was ever written, so CallerSender answers "ordinary agent" —
	// and the supervisor's own subscription being right there is exactly the
	// thing that must not make this succeed.
	_, err = client.Send(ctx, mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "hello?"},
		Binding: binding,
		Sender:  mail.CallerSender(home, binding),
	})
	if !errors.Is(err, mail.ErrNotSubagent) {
		t.Fatalf("send from a non-Subagent = %v, want ErrNotSubagent", err)
	}
	text := err.Error()
	for _, want := range []string{mail.HandleParent, "Subagent", "auto-stack/supervisor", "auto mail docs"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not mention %q — it must name the constraint, "+
				"an absolute alternative, and where to read more: %s", want, text)
		}
	}

	// Nothing was created: no mail row, and no event in the log.
	listed, err := client.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("a refused send delivered %+v, want nothing", listed)
	}
	counter, ok := client.(interface {
		CountEvents(context.Context, string) (int, error)
	})
	if !ok {
		t.Fatal("the direct client no longer counts events")
	}
	sentEvents, err := counter.CountEvents(ctx, mail.EventTypeSent)
	if err != nil {
		t.Fatalf("count sent events: %v", err)
	}
	if sentEvents != 0 {
		t.Errorf("%d alpha.mail.sent events after a refused send, want 0", sentEvents)
	}
}

// TestAbsoluteSendPayloadIsUnchanged is D-063-10: `resolvedFrom` is omitempty,
// so an absolute send still marshals to exactly T1's four keys.
//
// The assertion is on the marshalled bytes rather than on the struct, because
// the thing that must not change is what an existing consumer parses — and
// every one of them was written against T1's payload.
func TestAbsoluteSendPayloadIsUnchanged(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	sent, err := client.Send(ctx, mail.SendInput{
		To:      "auto-web/bugs",
		From:    "auto-stack/reviewer",
		Body:    map[string]any{"message": "the port is dropped on ssh:// URLs"},
		Binding: binding,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	encoded, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("marshal the send payload: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatalf("unmarshal the send payload: %v", err)
	}
	want := []string{"id", "to", "subscriptions", "bound"}
	if len(keys) != len(want) {
		t.Errorf("an absolute send printed %d keys (%s), want exactly T1's %v", len(keys), encoded, want)
	}
	for _, key := range want {
		if _, ok := keys[key]; !ok {
			t.Errorf("the send payload lost the key %q: %s", key, encoded)
		}
	}
	if _, ok := keys["resolvedFrom"]; ok {
		t.Errorf("an absolute send emitted resolvedFrom: %s", encoded)
	}
}

// TestParentHandleRefusesWhenTheSupervisorNeverSubscribed is AC-10 at the seam.
//
// This is the second refusal, and the reason there are four rather than one:
// the caller here did everything right — it *is* a Subagent, the marker is
// there — and the thing that has to change is in the supervisor, not in the
// child. A message that told it "you are not a Subagent" would send it off to
// fix something that is not broken, which is why the two are separate tokens
// and separate sentences.
func TestParentHandleRefusesWhenTheSupervisorNeverSubscribed(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// The marker exists — the hook saw a Subagent act here — and no
	// subscription does. That pair is the whole of this case.
	mail.ObserveHookEvent(home, binding, "PreToolUse", mail.ActiveAgent{
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
		SessionID: "the-supervisor-session",
	})
	sender := mail.CallerSender(home, binding)
	if sender.Kind != mail.SenderSubagent {
		t.Fatalf("CallerSender = %+v, want a Subagent — this case is about a "+
			"recognised Subagent with an unsubscribed supervisor", sender)
	}

	_, err = client.Send(ctx, mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "phase 3 blocked"},
		Binding: binding,
		Sender:  sender,
	})
	if !errors.Is(err, mail.ErrNoSupervisor) {
		t.Fatalf("send with no supervisor subscription = %v, want ErrNoSupervisor", err)
	}
	if errors.Is(err, mail.ErrNotSubagent) {
		t.Fatalf("the refusal also matches ErrNotSubagent; a caller branching on the " +
			"token would be told to become something it already is")
	}
	text := err.Error()
	for _, want := range []string{mail.HandleParent, "auto mail subscribe", "supervisor"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not mention %q — it must name the fix and who "+
				"applies it: %s", want, text)
		}
	}

	// AC-10's "textually distinct" clause, asserted rather than eyeballed: the
	// two refusals share a caller and a command, so an agent reading stderr has
	// only the sentence to tell them apart.
	other := handleRefusalText(t, client, home, mail.BindingFromContext(nil, t.TempDir()))
	if text == other {
		t.Errorf("the no-supervisor and non-Subagent refusals are the same sentence: %s", text)
	}

	// Nothing was created by the refusal.
	sentEvents, err := client.(interface {
		CountEvents(context.Context, string) (int, error)
	}).CountEvents(ctx, mail.EventTypeSent)
	if err != nil {
		t.Fatalf("count sent events: %v", err)
	}
	if sentEvents != 0 {
		t.Errorf("%d alpha.mail.sent events after a refused send, want 0", sentEvents)
	}
}

// handleRefusalText returns what an ordinary agent is told when it tries
// `#parent`, so the no-supervisor case above can compare against it.
func handleRefusalText(t *testing.T, client mail.Client, home string, binding mail.Binding) string {
	t.Helper()
	_, err := client.Send(context.Background(), mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "hello?"},
		Binding: binding,
		Sender:  mail.CallerSender(home, binding),
	})
	if !errors.Is(err, mail.ErrNotSubagent) {
		t.Fatalf("send from an ordinary agent = %v, want ErrNotSubagent", err)
	}
	return err.Error()
}

// TestParentResolvesToOneAddressUnderConcurrency is D-063-4's central claim,
// made falsifiable (AC-8).
//
// The claim is not that the marker race cannot happen — it can, constantly —
// but that it cannot express a wrong *recipient*. `#parent` resolves through
// AddressForBinding with the Binding the caller computed for itself, and every
// in-process Subagent of one supervisor shares a pane and a working directory,
// so all four compute the same key. Whichever marker writer wins the race, the
// lookup is the same lookup.
//
// Each sender opens its **own** store handle, which is 062 phase 5's rule and
// the whole point of the assertion: a shared handle is serialised by the
// standard library's connection pool, so a test that shared one would be
// asserting that the pool works rather than that this design does. The marker
// directory is churning throughout, so resolution happens under exactly the
// interleaving D-13 accepted as a limitation.
func TestParentResolvesToOneAddressUnderConcurrency(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	binding := mail.BindingFromContext(nil, t.TempDir())

	supervisor, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = supervisor.Close() })
	if _, err := supervisor.Subscribe(ctx, mail.SubscribeInput{
		Address: "auto-stack/supervisor",
		Binding: binding,
	}); err != nil {
		t.Fatalf("the supervisor could not subscribe: %v", err)
	}

	agents := swarm()
	for _, agent := range agents {
		mail.ObserveHookEvent(home, binding, "PreToolUse", agent)
	}

	// Marker refreshes run for the whole of the send, so every resolution is
	// made while the directory is being rewritten underneath it.
	done := make(chan struct{})
	var churn sync.WaitGroup
	for _, agent := range agents {
		churn.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					mail.ObserveHookEvent(home, binding, "PostToolUse", agent)
				}
			}
		})
	}

	type outcome struct {
		id, to, resolvedFrom string
		err                  error
	}
	results := make([]outcome, len(agents))
	var senders sync.WaitGroup
	for i, agent := range agents {
		senders.Go(func() {
			client, err := mail.NewDirect(home)
			if err != nil {
				results[i] = outcome{err: fmt.Errorf("open a store handle of its own: %w", err)}
				return
			}
			defer func() { _ = client.Close() }()
			sent, err := client.Send(ctx, mail.SendInput{
				To:      mail.HandleParent,
				Body:    map[string]any{"message": "blocked, from " + agent.AgentID},
				Binding: binding,
				Sender:  mail.CallerSender(home, binding),
			})
			results[i] = outcome{id: sent.ID, to: sent.To, resolvedFrom: sent.ResolvedFrom, err: err}
		})
	}
	senders.Wait()
	close(done)
	churn.Wait()

	for i, got := range results {
		if got.err != nil {
			t.Fatalf("%s: send to %s failed: %v", agents[i].AgentID, mail.HandleParent, got.err)
		}
		if got.to != "auto-stack/supervisor" {
			t.Errorf("%s resolved %s to %q, want the one supervisor address — the recipient "+
				"comes from the Binding, which every sibling shares, so no interleaving may "+
				"change it (D-063-4)", agents[i].AgentID, mail.HandleParent, got.to)
		}
		if got.resolvedFrom != mail.HandleParent {
			t.Errorf("%s: resolvedFrom = %q, want %q", agents[i].AgentID, got.resolvedFrom, mail.HandleParent)
		}
	}

	// Every one of them landed, and on the one subscription. The match is on
	// presence of each id rather than on a count: mail is at-least-once (G4).
	listed, err := supervisor.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	delivered := make(map[string]bool, len(listed))
	for _, item := range listed {
		delivered[item.ID] = true
	}
	for i, got := range results {
		if !delivered[got.id] {
			t.Errorf("the mail %s sent by %s never reached the supervisor; delivered: %v",
				got.id, agents[i].AgentID, slices.Sorted(maps.Keys(delivered)))
		}
	}
}

// TestTheSupervisorsOwnParentResolvesToItself asserts the residual D-063-4
// documents rather than pretending it away (AC-8).
//
// A supervisor has no agent_id of its own, so while one of its children is live
// its own `#parent` reads as a Subagent's and resolves — to *its own*
// Subscription, because that is what its own Binding is bound to. That is the
// whole of the residual: bounded self-delivery, never a stranger's mailbox. The
// alternative reading, that a marker under one Binding could make a *different*
// agent's `#parent` resolve, is the failure this rules out.
func TestTheSupervisorsOwnParentResolvesToItself(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	supervisor := mail.BindingFromContext(nil, t.TempDir())
	stranger := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	for binding, address := range map[mail.Binding]string{
		supervisor: "auto-stack/supervisor",
		stranger:   "auto-web/stranger",
	} {
		if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: address, Binding: binding}); err != nil {
			t.Fatalf("subscribe %q: %v", address, err)
		}
	}

	// One child is live under the supervisor's binding, and the supervisor —
	// not the child — is the one sending.
	mail.ObserveHookEvent(home, supervisor, "PreToolUse", mail.ActiveAgent{
		AgentID: "a84a3676a847c5c0b", AgentType: "Explore", SessionID: "the-supervisor-session",
	})

	sent, err := client.Send(ctx, mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "sent by the supervisor itself"},
		Binding: supervisor,
		Sender:  mail.CallerSender(home, supervisor),
	})
	if err != nil {
		t.Fatalf("the supervisor's own %s: %v", mail.HandleParent, err)
	}
	if sent.To != "auto-stack/supervisor" {
		t.Errorf("the supervisor's own %s resolved to %q, want its own address — the "+
			"documented residual is self-delivery, and anything else is a stranger's "+
			"mailbox reached by accident", mail.HandleParent, sent.To)
	}

	// The stranger is subscribed on the same host, with a marker live nowhere
	// near it, and must be untouched.
	strangerMail, err := client.List(ctx, mail.ListInput{Binding: stranger})
	if err != nil {
		t.Fatalf("List for the stranger: %v", err)
	}
	if len(strangerMail) != 0 {
		t.Errorf("an unrelated agent received %+v from a %s it had nothing to do with",
			strangerMail, mail.HandleParent)
	}
}

// TestAttributionIsWithheldWhenSeveralSubagentsAreLive is D-063-11 at the seam,
// and the assertion that stops attribution quietly reverting to "pick the
// newest marker" — the bug the epic's review caught (AC-8).
//
// The calling process has no agent_id of its own. With four markers live, the
// newest is a sibling's as often as it is the caller's, so a name here is a
// plausible wrong answer, and a supervisor acting on a confident wrong name is
// worse off than one told nothing at all. What survives concurrency is the
// *kind* — every one of them is a Subagent of this supervisor — and that is
// precisely what `#parent` resolves on.
//
// The single-Subagent arm at the end is not decoration: without it this test
// would pass just as well against an implementation that never attributed
// anything, which is the wrong fix for the same symptom.
//
// The envelope half is written as an implication rather than an equality
// because attributes arrive in phase 5: today no `sender*` attribute exists, so
// the clause is vacuous — but the moment one is emitted, an envelope produced
// under ambiguity has to carry `senderAmbiguous` and must not name a Subagent,
// or this fails. It cannot be satisfied by adding the attributes and forgetting
// the rule.
func TestAttributionIsWithheldWhenSeveralSubagentsAreLive(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	binding := mail.BindingFromContext(nil, t.TempDir())

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Subscribe(ctx, mail.SubscribeInput{
		Address: "auto-stack/supervisor",
		Binding: binding,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	agents := swarm()
	for _, agent := range agents {
		mail.ObserveHookEvent(home, binding, "PreToolUse", agent)
	}

	sender := mail.CallerSender(home, binding)
	if sender.Kind != mail.SenderSubagent {
		t.Fatalf("CallerSender = %+v, want a Subagent — the kind is the part that "+
			"survives concurrency", sender)
	}
	if !sender.Ambiguous {
		t.Fatalf("Ambiguous = false with %d live Subagents: %+v — an implementation that "+
			"picked the newest marker would look exactly like this", len(agents), sender)
	}
	if sender.AgentID != "" || sender.AgentType != "" {
		t.Errorf("a name was reported under concurrency: %+v — it can only be a guess, and "+
			"a sibling's name is worse for a supervisor than no name (D-063-11)", sender)
	}

	sent, err := client.Send(ctx, mail.SendInput{
		To:      mail.HandleParent,
		Body:    map[string]any{"message": "one of four, and it cannot say which"},
		Binding: binding,
		Sender:  sender,
	})
	if err != nil {
		t.Fatalf("send under ambiguity: %v", err)
	}
	if sent.To != "auto-stack/supervisor" {
		t.Errorf("to = %q, want the supervisor — ambiguity withholds the name, never the "+
			"delivery", sent.To)
	}

	st, err := store.Open(config.StorePathIn(home))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	var envelope string
	if err := st.QueryRowContext(ctx,
		`SELECT envelope FROM mail WHERE id = ?`, sent.ID).Scan(&envelope); err != nil {
		t.Fatalf("read the envelope: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(envelope), &decoded); err != nil {
		t.Fatalf("the envelope is not JSON: %v", err)
	}

	// No Subagent is named, by any spelling. The agent types are checked as
	// well as the ids because a name is what a supervisor would act on.
	for _, agent := range agents {
		for _, physical := range []string{agent.AgentID, agent.AgentType} {
			if physical != "" && strings.Contains(envelope, physical) {
				t.Errorf("the envelope names %q while four Subagents were live: %s — "+
					"under ambiguity it is a guess with a one-in-four chance", physical, envelope)
			}
		}
	}
	if _, named := decoded["senderAgentType"]; named {
		t.Errorf("senderAgentType is present under ambiguity: %s", envelope)
	}
	// Phase 5's gate: attributes may arrive, but not without the flag that says
	// the name was withheld deliberately.
	if _, kind := decoded["senderKind"]; kind {
		if ambiguous, ok := decoded["senderAmbiguous"].(bool); !ok || !ambiguous {
			t.Errorf("the envelope carries senderKind but not senderAmbiguous: true, so a "+
				"reader cannot tell a withheld name from an unattributed send: %s", envelope)
		}
	}

	// And with exactly one live, the name comes back — otherwise the assertions
	// above would hold for an implementation that attributed nothing at all.
	for _, agent := range agents[1:] {
		mail.ObserveHookEvent(home, binding, "SubagentStop", agent)
	}
	alone := mail.CallerSender(home, binding)
	if alone.Ambiguous || alone.AgentID != agents[0].AgentID || alone.AgentType != agents[0].AgentType {
		t.Errorf("with one Subagent live, CallerSender = %+v, want %q/%q named and no "+
			"ambiguity", alone, agents[0].AgentID, agents[0].AgentType)
	}
}
