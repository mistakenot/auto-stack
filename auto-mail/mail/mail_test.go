package mail_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

// TestUnimplementedVerbsSaySo: the seam is complete before the implementation
// is, so a verb a later phase fills must fail loudly rather than silently
// succeeding with an empty result. Only Reset is still unbuilt — it lands with
// the rest of the alpha contract.
func TestUnimplementedVerbsSaySo(t *testing.T) {
	client, err := mail.NewDirect(t.TempDir())
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.Reset(context.Background()); !errors.Is(err, mail.ErrNotImplemented) {
		t.Errorf("Reset error = %v, want ErrNotImplemented", err)
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
