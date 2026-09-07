package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/cli"
	"github.com/mistakenot/auto-mail/internal/store"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/pflag"
)

// runCLI drives the command tree in-process under whatever $HOME the caller has
// set, returning stdout, stderr, and the process exit code. Nothing here opens
// a network connection or needs a daemon (D-11).
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cwd, _ := os.Getwd()
	root := cli.NewRootCmd(app.New(&outBuf, &errBuf, cwd))
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	err := root.ExecuteContext(context.Background())
	code = 0
	if err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil {
				errBuf.WriteString(exitErr.Err.Error())
			}
		} else {
			code = 1
			errBuf.WriteString(err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// TestInitCreatesAlphaStore: `init` creates ~/.auto/mail/alpha-store.db — the
// alpha marker is in the filename, not only in the docs (G10 / D-2).
func TestInitCreatesAlphaStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, stderr, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init exit %d, stderr: %s", code, stderr)
	}

	var payload struct {
		Store   string `json:"store"`
		Created bool   `json:"created"`
		Alpha   bool   `json:"alpha"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("init stdout is not JSON: %v\n%s", err, stdout)
	}
	want := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	if payload.Store != want {
		t.Errorf("store = %q, want %q", payload.Store, want)
	}
	if !payload.Created {
		t.Errorf("created = false on a fresh HOME, want true")
	}
	if !payload.Alpha {
		t.Errorf("alpha = false, want true")
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("store file not on disk after init: %v", err)
	}

	// Re-running init is safe and reports the store as already present.
	stdout, stderr, code = runCLI(t, "init")
	if code != 0 {
		t.Fatalf("second init exit %d, stderr: %s", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("second init stdout is not JSON: %v\n%s", err, stdout)
	}
	if payload.Created {
		t.Errorf("created = true on an existing store, want false")
	}
}

// TestListEmptyReturnsJSONArray: with no mail, `list` prints `[]` on stdout and
// nothing else — pure JSON, diagnostics on stderr (project CLI convention).
func TestListEmptyReturnsJSONArray(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, stderr, code := runCLI(t, "init"); code != 0 {
		t.Fatalf("init exit %d, stderr: %s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("list stdout = %q, want %q", stdout, "[]")
	}
	var deliveries []map[string]any
	if err := json.Unmarshal([]byte(stdout), &deliveries); err != nil {
		t.Fatalf("list stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(deliveries) != 0 {
		t.Errorf("list returned %d deliveries on a fresh store, want 0", len(deliveries))
	}
	if stderr != "" {
		t.Errorf("list wrote to stderr: %q", stderr)
	}
}

// TestListWithoutInitStillWorks: mail never requires a separate setup step —
// `list` opens (creating) the store itself, so an agent's first call succeeds.
func TestListWithoutInitStillWorks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, stderr, code := runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("list stdout = %q, want %q", stdout, "[]")
	}
}

// TestDocsStatesTheAlphaContract: an agent must be able to discover the surface
// it is asked to use, and the alpha marker must be discoverable there (G10).
func TestDocsStatesTheAlphaContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, _, code := runCLI(t, "docs")
	if code != 0 {
		t.Fatalf("docs exit %d", code)
	}
	for _, want := range []string{"auto mail", "alpha-store.db", "## init", "## list"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("docs output missing %q", want)
		}
	}
}

// workspace makes a directory and moves the process into it. Each workspace is
// one "agent": with no tmux and no ntm in a test process, the binding ladder
// falls to its cwd rung, so two directories are two independently bound
// callers — the same thing two container workspaces are in the harness.
func workspace(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create workspace %s: %v", name, err)
	}
	return dir
}

func decode[T any](t *testing.T, stdout string) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return payload
}

type subscribePayload struct {
	Address      string `json:"address"`
	Subscription string `json:"subscription"`
	Backfilled   int    `json:"backfilled"`
}

type sendPayload struct {
	ID            string `json:"id"`
	To            string `json:"to"`
	Subscriptions int    `json:"subscriptions"`
	Bound         int    `json:"bound"`
}

type deliveryPayload struct {
	ID     string         `json:"id"`
	From   string         `json:"from"`
	SentAt string         `json:"sentAt"`
	Body   map[string]any `json:"body"`
}

type ackPayload struct {
	ID            string `json:"id"`
	Acked         bool   `json:"acked"`
	WonTransition bool   `json:"wonTransition"`
}

// TestC1Loop runs the epic's transcript verbatim: agent A subscribes to its own
// reply address, agent B subscribes to the target, A sends, B lists and acks,
// and B's second list is empty. Every payload shape is asserted, because C1 is
// the contract other tools will be written against.
func TestC1Loop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentA := workspace(t, home, "auto-stack")
	agentB := workspace(t, home, "auto-web")

	// A subscribes to its own reply address first. That is what makes `from`
	// resolve by rung 2 of the ladder with no --from flag — and what makes A
	// reachable for a reply.
	t.Chdir(agentA)
	stdout, stderr, code := runCLI(t, "subscribe", "auto-stack/reviewer")
	if code != 0 {
		t.Fatalf("subscribe (A) exit %d, stderr: %s", code, stderr)
	}
	replyTo := decode[subscribePayload](t, stdout)
	if replyTo.Address != "auto-stack/reviewer" {
		t.Errorf("address = %q, want %q", replyTo.Address, "auto-stack/reviewer")
	}
	if !strings.HasPrefix(replyTo.Subscription, "sub_") {
		t.Errorf("subscription = %q, want a sub_-prefixed id", replyTo.Subscription)
	}
	if replyTo.Backfilled != 0 {
		t.Errorf("backfilled = %d on a fresh address, want 0", replyTo.Backfilled)
	}

	// B subscribes to the target address.
	t.Chdir(agentB)
	stdout, stderr, code = runCLI(t, "subscribe", "auto-web/bugs")
	if code != 0 {
		t.Fatalf("subscribe (B) exit %d, stderr: %s", code, stderr)
	}
	inbox := decode[subscribePayload](t, stdout)
	if inbox.Subscription == replyTo.Subscription {
		t.Fatalf("both agents share subscription %q; two callers must get two subscriptions", inbox.Subscription)
	}

	// A sends.
	const text = "normalizeRemote drops the port on ssh:// URLs"
	t.Chdir(agentA)
	stdout, stderr, code = runCLI(t, "send", "--to", "auto-web/bugs", "--message", text)
	if code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	sent := decode[sendPayload](t, stdout)
	if sent.ID == "" {
		t.Fatal("send returned no mail id")
	}
	if sent.To != "auto-web/bugs" {
		t.Errorf("to = %q, want %q", sent.To, "auto-web/bugs")
	}
	if sent.Subscriptions != 1 || sent.Bound != 1 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 1 and 1", sent.Subscriptions, sent.Bound)
	}
	if stderr != "" {
		t.Errorf("send wrote to stderr with a subscriber present: %q", stderr)
	}

	// B lists. Same id, resolved from-address, and the body under the key the
	// --message flag names.
	t.Chdir(agentB)
	stdout, stderr, code = runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	listed := decode[[]deliveryPayload](t, stdout)
	if len(listed) != 1 {
		t.Fatalf("list returned %d deliveries, want 1: %s", len(listed), stdout)
	}
	if listed[0].ID != sent.ID {
		t.Errorf("list id = %q, want the id send returned (%q)", listed[0].ID, sent.ID)
	}
	if listed[0].From != "auto-stack/reviewer" {
		t.Errorf("from = %q, want the sender's subscription address (rung 2)", listed[0].From)
	}
	if listed[0].SentAt == "" {
		t.Error("sentAt is empty")
	}
	if listed[0].Body["message"] != text {
		t.Errorf("body = %v, want the message under its own key", listed[0].Body)
	}
	if stderr != "" {
		t.Errorf("list wrote to stderr: %q", stderr)
	}

	// B acks — a separate explicit call, which wins the transition.
	stdout, stderr, code = runCLI(t, "ack", sent.ID)
	if code != 0 {
		t.Fatalf("ack exit %d, stderr: %s", code, stderr)
	}
	acked := decode[ackPayload](t, stdout)
	if acked.ID != sent.ID || !acked.Acked || !acked.WonTransition {
		t.Errorf("ack payload = %+v, want the id acked and the transition won", acked)
	}
	if stderr != "" {
		t.Errorf("a winning ack wrote to stderr: %q", stderr)
	}

	// And the acked mail is gone from the default view.
	stdout, stderr, code = runCLI(t, "list")
	if code != 0 {
		t.Fatalf("second list exit %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("second list = %q, want []", stdout)
	}
	if stderr != "" {
		t.Errorf("second list wrote to stderr: %q", stderr)
	}
}

// TestListDoesNotRetire is G3 at the command surface: two lists with no ack
// between them return the same mail twice, and no flag on `list` can ack.
func TestListDoesNotRetire(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}
	stdout, stderr, code := runCLI(t, "send", "--to", "auto-web/bugs", "--message", "hello")
	if code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	sent := decode[sendPayload](t, stdout)

	for i := range 2 {
		stdout, stderr, code := runCLI(t, "list")
		if code != 0 {
			t.Fatalf("list #%d exit %d, stderr: %s", i+1, code, stderr)
		}
		listed := decode[[]deliveryPayload](t, stdout)
		if len(listed) != 1 || listed[0].ID != sent.ID {
			t.Fatalf("list #%d returned %+v; reading must not retire mail", i+1, listed)
		}
	}

	// The surface itself must make conflation impossible.
	root := cli.NewRootCmd(app.New(io.Discard, io.Discard, home))
	for _, cmd := range root.Commands() {
		if cmd.Name() != "list" {
			continue
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if strings.Contains(strings.ToLower(f.Name), "ack") {
				t.Errorf("`list` has an --%s flag; ack must always be a separate call (G3)", f.Name)
			}
		})
	}
}

// TestAckLosingTheRaceStillExitsZero (D-062-7): losing a race is a correct,
// expected outcome, not invalid usage. The caller is told on stderr and the
// payload says which question got which answer.
func TestAckLosingTheRaceStillExitsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}
	stdout, _, _ := runCLI(t, "send", "--to", "auto-web/bugs", "--message", "hello")
	sent := decode[sendPayload](t, stdout)

	if _, stderr, code := runCLI(t, "ack", sent.ID); code != 0 {
		t.Fatalf("first ack exit %d, stderr: %s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "ack", sent.ID)
	if code != 0 {
		t.Fatalf("a losing ack exited %d; losing a race is not invalid usage", code)
	}
	lost := decode[ackPayload](t, stdout)
	if !lost.Acked {
		t.Error("acked = false; the delivery is acked now, whoever did it")
	}
	if lost.WonTransition {
		t.Error("wonTransition = true on the second ack; a delivery transitions exactly once")
	}
	if !strings.Contains(stderr, "already acked") {
		t.Errorf("the loser was not told: stderr = %q", stderr)
	}
}

// TestAckUnknownIDExitsNonZeroWithRemediation: a genuine mistake is fail-fast,
// and the error says what to do about it.
func TestAckUnknownIDExitsNonZeroWithRemediation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	stdout, stderr, code := runCLI(t, "ack", "01NOSUCHMAIL0000000000000")
	if code == 0 {
		t.Error("acking an unknown id exited 0")
	}
	if stdout != "" {
		t.Errorf("a failed ack wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "auto mail list") {
		t.Errorf("the error carries no remediation hint: %q", stderr)
	}
}

// TestSendWithNoSubscriptionPersists is G6: the send succeeds, the mail is
// durable, and the sender is warned on stderr rather than by an exit code —
// which is the mitigation free-form addresses (D-9) rely on for typos.
func TestSendWithNoSubscriptionPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "sender"))

	stdout, stderr, code := runCLI(t, "send", "--to", "auto-web/typo", "--message", "hello?")
	if code != 0 {
		t.Fatalf("send to an unsubscribed address exit %d, stderr: %s", code, stderr)
	}
	sent := decode[sendPayload](t, stdout)
	if sent.Subscriptions != 0 || sent.Bound != 0 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 0 and 0", sent.Subscriptions, sent.Bound)
	}
	if !strings.Contains(stderr, "typo") {
		t.Errorf("stderr carries no likely-typo hint: %q", stderr)
	}
	// The hint is stderr-only: stdout in JSON mode is strictly parseable
	// payload, so a caller piping it into jq never sees a diagnostic. (The
	// address itself carries the word, so the match is on the hint's own
	// phrasing rather than on "typo".)
	if strings.Contains(stdout, "check the address") {
		t.Errorf("the likely-typo hint leaked into stdout: %q", stdout)
	}

	// The J2 half: a later subscriber receives it.
	stdout, stderr, code = runCLI(t, "subscribe", "auto-web/typo")
	if code != 0 {
		t.Fatalf("late subscribe exit %d, stderr: %s", code, stderr)
	}
	if got := decode[subscribePayload](t, stdout).Backfilled; got != 1 {
		t.Errorf("backfilled = %d, want 1", got)
	}
	stdout, _, _ = runCLI(t, "list")
	listed := decode[[]deliveryPayload](t, stdout)
	if len(listed) != 1 || listed[0].ID != sent.ID {
		t.Errorf("late subscriber sees %+v, want the earlier mail %s", listed, sent.ID)
	}
}

// TestSubscribeFromNowOptsOutOfTheBacklog is D-10 at the command surface. The
// default backfills, because an agent that subscribes late still needs what it
// missed (that is the J2 journey); --from-now is the explicit opt-out for an
// agent that wants a clean slate, and it must still receive everything sent
// afterwards or it would be a mute subscription rather than a fresh one.
func TestSubscribeFromNowOptsOutOfTheBacklog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sender := workspace(t, home, "sender")
	fresh := workspace(t, home, "fresh")

	t.Chdir(sender)
	stdout, stderr, code := runCLI(t, "send", "--to", "auto-web/backlog", "--message", "before")
	if code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	before := decode[sendPayload](t, stdout)

	t.Chdir(fresh)
	stdout, stderr, code = runCLI(t, "subscribe", "auto-web/backlog", "--from-now")
	if code != 0 {
		t.Fatalf("subscribe --from-now exit %d, stderr: %s", code, stderr)
	}
	if got := decode[subscribePayload](t, stdout).Backfilled; got != 0 {
		t.Errorf("--from-now backfilled = %d, want 0", got)
	}
	stdout, _, _ = runCLI(t, "list")
	if listed := decode[[]deliveryPayload](t, stdout); len(listed) != 0 {
		t.Errorf("--from-now subscriber sees %+v, want nothing before its cursor", listed)
	}

	t.Chdir(sender)
	stdout, stderr, code = runCLI(t, "send", "--to", "auto-web/backlog", "--message", "after")
	if code != 0 {
		t.Fatalf("second send exit %d, stderr: %s", code, stderr)
	}
	after := decode[sendPayload](t, stdout)
	if after.Subscriptions != 1 || after.Bound != 1 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 1 and 1", after.Subscriptions, after.Bound)
	}

	t.Chdir(fresh)
	stdout, _, _ = runCLI(t, "list")
	listed := decode[[]deliveryPayload](t, stdout)
	if len(listed) != 1 || listed[0].ID != after.ID {
		t.Errorf("--from-now subscriber sees %+v, want only %s", listed, after.ID)
	}
	if len(listed) == 1 && listed[0].ID == before.ID {
		t.Errorf("--from-now subscriber received %s, which predates its cursor", before.ID)
	}
}

// TestFromLadder asserts all three rungs of the sender's from-address, since a
// wrong answer here is how a reply gets lost. Rung 3 is exercised outside a
// registered project, where the literal is the whole point.
func TestFromLadder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reader := workspace(t, home, "reader")
	sender := workspace(t, home, "sender")

	t.Chdir(reader)
	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}

	fromOf := func(t *testing.T, args ...string) string {
		t.Helper()
		t.Chdir(sender)
		stdout, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("send exit %d, stderr: %s", code, stderr)
		}
		id := decode[sendPayload](t, stdout).ID
		t.Chdir(reader)
		stdout, _, _ = runCLI(t, "list")
		for _, d := range decode[[]deliveryPayload](t, stdout) {
			if d.ID == id {
				return d.From
			}
		}
		t.Fatalf("mail %s never reached the reader", id)
		return ""
	}

	// Rung 3 first, while the sender holds no subscription: outside a
	// registered project the from-address names no project it cannot verify.
	if got := fromOf(t, "send", "--to", "auto-web/bugs", "--message", "rung 3"); got != "unregistered/agent" {
		t.Errorf("rung 3 from = %q, want %q", got, "unregistered/agent")
	}

	// Rung 1: an explicit --from wins over everything.
	if got := fromOf(t, "send", "--to", "auto-web/bugs", "--message", "rung 1", "--from", "auto-stack/explicit"); got != "auto-stack/explicit" {
		t.Errorf("rung 1 from = %q, want %q", got, "auto-stack/explicit")
	}

	// Rung 2: the sender subscribes to its own reply address, which then
	// resolves with no flag.
	t.Chdir(sender)
	if _, stderr, code := runCLI(t, "subscribe", "auto-stack/reviewer"); code != 0 {
		t.Fatalf("sender subscribe exit %d, stderr: %s", code, stderr)
	}
	if got := fromOf(t, "send", "--to", "auto-web/bugs", "--message", "rung 2"); got != "auto-stack/reviewer" {
		t.Errorf("rung 2 from = %q, want %q", got, "auto-stack/reviewer")
	}
}

// TestSendRejectsInvalidUsage: flag conflicts and missing bodies are fail-fast
// through cobra, per the project's CLI convention.
func TestSendRejectsInvalidUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "sender"))

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no body", []string{"send", "--to", "auto-web/bugs"}},
		{"both body flags", []string{"send", "--to", "auto-web/bugs", "--message", "x", "--body", `{"a":1}`}},
		{"body is not JSON", []string{"send", "--to", "auto-web/bugs", "--body", "not json"}},
		{"no destination", []string{"send", "--message", "x"}},
		{"empty destination", []string{"send", "--to", "", "--message", "x"}},
		{"padded destination", []string{"send", "--to", " auto-web/bugs ", "--message", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Errorf("%v exited 0, want a failure", tc.args)
			}
			if stdout != "" {
				t.Errorf("a rejected send wrote to stdout: %q", stdout)
			}
			if stderr == "" {
				t.Error("a rejected send explained nothing on stderr")
			}
		})
	}
}

// TestSendWithAJSONBody: --message is sugar, --body is the general form.
func TestSendWithAJSONBody(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}
	if _, stderr, code := runCLI(t, "send", "--to", "auto-web/bugs", "--body", `{"kind":"bug","detail":"ssh:// port"}`); code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	stdout, _, _ := runCLI(t, "list")
	listed := decode[[]deliveryPayload](t, stdout)
	if len(listed) != 1 {
		t.Fatalf("list returned %d deliveries, want 1", len(listed))
	}
	if listed[0].Body["kind"] != "bug" || listed[0].Body["detail"] != "ssh:// port" {
		t.Errorf("body = %v, want the JSON object as sent", listed[0].Body)
	}
}

// TestListFilters: --address scopes to one of the caller's subscriptions, and
// the two filter modes cannot be combined.
func TestListFilters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	for _, address := range []string{"auto-web/bugs", "auto-stack/reviewer"} {
		if _, stderr, code := runCLI(t, "subscribe", address); code != 0 {
			t.Fatalf("subscribe %s exit %d, stderr: %s", address, code, stderr)
		}
		if _, stderr, code := runCLI(t, "send", "--to", address, "--message", "hello", "--from", "someone/else"); code != 0 {
			t.Fatalf("send to %s exit %d, stderr: %s", address, code, stderr)
		}
	}

	stdout, _, _ := runCLI(t, "list")
	if got := len(decode[[]deliveryPayload](t, stdout)); got != 2 {
		t.Errorf("unfiltered list returned %d deliveries, want everything (2)", got)
	}

	stdout, stderr, code := runCLI(t, "list", "--address", "auto-web/bugs")
	if code != 0 {
		t.Fatalf("filtered list exit %d, stderr: %s", code, stderr)
	}
	if got := len(decode[[]deliveryPayload](t, stdout)); got != 1 {
		t.Errorf("--address list returned %d deliveries, want 1", got)
	}

	if _, _, code := runCLI(t, "list", "--address", "auto-web/bugs", "--subscription", "sub_01"); code == 0 {
		t.Error("combining --address and --subscription exited 0; one filter mode at a time")
	}
}

type resetPayload struct {
	Removed []string `json:"removed"`
}

// TestResetWipesTheStoreAndTheC1LoopRunsAgain is AC-11's second half (G10):
// the alpha store is disposable, so `reset` is a supported operation rather
// than a workaround. It removes both halves of the on-disk state — the store
// and the pending flags — reports what it removed, and the tool then works
// normally from empty, which is asserted by running the whole C1 loop again.
func TestResetWipesTheStoreAndTheC1LoopRunsAgain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reader := workspace(t, home, "reader")
	t.Chdir(reader)

	storePath := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	flagsDir := filepath.Join(home, ".auto", "mail", "alpha-flags")

	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}
	stdout, stderr, code := runCLI(t, "send", "--to", "auto-web/bugs", "--message", "before the reset")
	if code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	sent := decode[sendPayload](t, stdout)
	// The send raised this binding's pending flag, so both halves of the state
	// are on disk before the reset.
	if entries, err := os.ReadDir(flagsDir); err != nil || len(entries) == 0 {
		t.Fatalf("no pending flag under %s before the reset (err = %v)", flagsDir, err)
	}

	// A store that still holds mail is refused, with a hint that says what to
	// do about it — wiping unacked mail must be something a caller asked for.
	stdout, stderr, code = runCLI(t, "reset")
	if code == 0 {
		t.Error("reset on a non-empty store exited 0; it must refuse without --yes")
	}
	if stdout != "" {
		t.Errorf("a refused reset wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("the refusal carries no remediation hint: %q", stderr)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Errorf("a refused reset removed the store anyway: %v", err)
	}

	stdout, stderr, code = runCLI(t, "reset", "--yes")
	if code != 0 {
		t.Fatalf("reset --yes exit %d, stderr: %s", code, stderr)
	}
	removed := decode[resetPayload](t, stdout).Removed
	for _, want := range []string{storePath, flagsDir} {
		if !slices.Contains(removed, want) {
			t.Errorf("removed = %v, want it to name %q", removed, want)
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Errorf("%s survived the reset (stat err = %v)", want, err)
		}
	}

	// The old mail is genuinely gone, ids and all.
	if _, _, code := runCLI(t, "ack", sent.ID); code == 0 {
		t.Error("acking pre-reset mail succeeded; the store was not wiped")
	}

	// And the whole C1 loop runs again from empty.
	stdout, stderr, code = runCLI(t, "subscribe", "auto-web/bugs")
	if code != 0 {
		t.Fatalf("subscribe after reset exit %d, stderr: %s", code, stderr)
	}
	if backfilled := decode[subscribePayload](t, stdout).Backfilled; backfilled != 0 {
		t.Errorf("backfilled = %d after a reset, want 0", backfilled)
	}
	stdout, stderr, code = runCLI(t, "send", "--to", "auto-web/bugs", "--message", "after the reset")
	if code != 0 {
		t.Fatalf("send after reset exit %d, stderr: %s", code, stderr)
	}
	fresh := decode[sendPayload](t, stdout)
	stdout, stderr, code = runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list after reset exit %d, stderr: %s", code, stderr)
	}
	listed := decode[[]deliveryPayload](t, stdout)
	if len(listed) != 1 || listed[0].ID != fresh.ID {
		t.Fatalf("list after reset = %s, want just the new mail %s", stdout, fresh.ID)
	}
	stdout, stderr, code = runCLI(t, "ack", fresh.ID)
	if code != 0 {
		t.Fatalf("ack after reset exit %d, stderr: %s", code, stderr)
	}
	if acked := decode[ackPayload](t, stdout); !acked.Acked || !acked.WonTransition {
		t.Errorf("ack after reset = %+v, want it acked and the transition won", acked)
	}
}

// TestResetOnAFreshHostRemovesNothing: `reset` must not report removing a
// store it created on its way in. A host with nothing to wipe is answered
// before anything is opened, so the empty answer is also the honest one.
func TestResetOnAFreshHostRemovesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(workspace(t, home, "reader"))

	stdout, stderr, code := runCLI(t, "reset")
	if code != 0 {
		t.Fatalf("reset on a fresh host exit %d, stderr: %s", code, stderr)
	}
	if removed := decode[resetPayload](t, stdout).Removed; len(removed) != 0 {
		t.Errorf("removed = %v on a host with no store, want []", removed)
	}
	if strings.TrimSpace(stdout) == "" || !strings.Contains(stdout, "removed") {
		t.Errorf("reset stdout is not the removed payload: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".auto", "mail", "alpha-store.db")); !os.IsNotExist(err) {
		t.Errorf("reset created the store it was asked to remove (stat err = %v)", err)
	}
}

// TestDocsStatesTheDeliveryContract is AC-7's documented half (G4): the
// contract has to be discoverable by the agent being asked to honour it, in as
// many words, alongside the alpha terms G10 requires.
func TestDocsStatesTheDeliveryContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, _, code := runCLI(t, "docs")
	if code != 0 {
		t.Fatalf("docs exit %d", code)
	}
	lower := strings.ToLower(stdout)
	for _, want := range []string{
		"at-least-once",
		"unordered",
		"idempotent",
		"no upcasters",
		"no migrations",
		"no compatibility guarantee",
		"wiped on upgrade",
		"## reset",
		"## subscribe",
		"## send",
		"## ack",
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("docs output missing %q", want)
		}
	}
	// Exactly-once and ordering may only ever be mentioned to disclaim them.
	if strings.Contains(lower, "exactly-once") && !strings.Contains(lower, "promises exactly-once") {
		t.Error("docs mention exactly-once outside the disclaimer; the contract is at-least-once")
	}
}

// TestResetRecoversAStoreWrittenByAnotherSchema is the escape hatch G10's
// "no upcasters, no migrations" position needs to be honest.
//
// The alpha store is disposable, and a build that meets a store written by a
// different schema says so and names `auto mail reset --yes`. That remediation
// has to work on a store this build cannot open — otherwise the advice is a
// dead end and the only way out is `rm` by hand. So every other verb refuses
// with the mismatch named, and `reset --yes` wipes the file without opening it.
func TestResetRecoversAStoreWrittenByAnotherSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, stderr, code := runCLI(t, "init"); code != 0 {
		t.Fatalf("init exit %d, stderr: %s", code, stderr)
	}
	storePath := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	// Written through the one package allowed to open the mail store (G11), so
	// this test observes the CLI's behaviour without reaching around the seam.
	if err := store.RecordSchemaVersion(storePath, 99); err != nil {
		t.Fatalf("stamp a foreign schema version: %v", err)
	}

	// Ordinary verbs refuse, and say what to do about it.
	_, stderr, code := runCLI(t, "list")
	if code == 0 {
		t.Error("list against a foreign schema exited 0; it cannot read that store")
	}
	if !strings.Contains(stderr, "different schema") {
		t.Errorf("list stderr = %q, want it to name the schema mismatch", stderr)
	}

	// So does reset without --yes: emptiness cannot be read from a store
	// nobody can read, so the wipe is never implicit.
	_, stderr, code = runCLI(t, "reset")
	if code == 0 {
		t.Error("reset without --yes wiped a store it could not read")
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("reset stderr = %q, want it to name `auto mail reset --yes`", stderr)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Errorf("the refused reset removed the store anyway: %v", err)
	}

	stdout, stderr, code := runCLI(t, "reset", "--yes")
	if code != 0 {
		t.Fatalf("reset --yes exit %d, stderr: %s", code, stderr)
	}
	if removed := decode[resetPayload](t, stdout).Removed; !slices.Contains(removed, storePath) {
		t.Errorf("reset --yes removed %v, want it to include %s", removed, storePath)
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Errorf("the store is still on disk after reset --yes: %v", err)
	}

	// And the tool works again from there — "start again" is the migration path.
	if _, stderr, code := runCLI(t, "subscribe", "auto-web/bugs"); code != 0 {
		t.Fatalf("subscribe after the reset exit %d, stderr: %s", code, stderr)
	}
}

// TestParentHandleAtTheCommandSurface is J1 as an agent actually types it, plus
// AC-3's refusal.
//
// The supervisor and its Subagent are one workspace, because that is what an
// in-process Subagent is: it shares its supervisor's pane and working
// directory, so it computes the same binding and needs no identifier of its
// own. `#parent` is the whole of what the child supplies.
func TestParentHandleAtTheCommandSurface(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	supervisor := workspace(t, home, "supervisor")
	t.Chdir(supervisor)
	binding := mail.BindingFor(supervisor)

	if _, stderr, code := runCLI(t, "subscribe", "auto-stack/supervisor"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}

	// Before any Subagent is recorded, `#parent` is refused: exit 1, stdout
	// completely empty, and the remediation on stderr.
	stdout, stderr, code := runCLI(t, "send", "--to", "#parent", "--message", "hello?")
	if code != 1 {
		t.Fatalf("send --to #parent from a non-Subagent exit %d, want 1 (stdout %q)", code, stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q on a refused send, want it empty — a caller parsing it "+
			"must never be handed half a payload", stdout)
	}
	for _, want := range []string{"#parent", "Subagent", "auto mail docs"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal on stderr does not mention %q: %s", want, stderr)
		}
	}

	// The hook records that a Subagent is acting under this binding. That is
	// the only thing that changes between the refusal above and the send below.
	mail.ObserveHookEvent(home, binding, "PreToolUse", mail.ActiveAgent{
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
		SessionID: "the-supervisor-session",
	})

	stdout, stderr, code = runCLI(t, "send", "--to", "#parent", "--message", "phase 3 blocked")
	if code != 0 {
		t.Fatalf("send --to #parent exit %d, stderr: %s", code, stderr)
	}
	sent := decode[handleSendPayload](t, stdout)
	if sent.To != "auto-stack/supervisor" {
		t.Errorf("to = %q, want the supervisor's absolute address", sent.To)
	}
	if sent.ResolvedFrom != "#parent" {
		t.Errorf("resolvedFrom = %q, want %q", sent.ResolvedFrom, "#parent")
	}

	// The supervisor reads it, and only an explicit ack retires it (G3).
	stdout, stderr, code = runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	delivered := decode[[]deliveryPayload](t, stdout)
	if len(delivered) != 1 || delivered[0].ID != sent.ID {
		t.Fatalf("the supervisor listed %+v, want the mail %s", delivered, sent.ID)
	}
	if delivered[0].Body["message"] != "phase 3 blocked" {
		t.Errorf("body = %v, want the Subagent's text", delivered[0].Body)
	}

	stdout, stderr, code = runCLI(t, "ack", sent.ID)
	if code != 0 {
		t.Fatalf("ack exit %d, stderr: %s", code, stderr)
	}
	if acked := decode[ackPayload](t, stdout); !acked.WonTransition {
		t.Errorf("ack = %+v, want the first ack to win the transition", acked)
	}
}

// handleSendPayload is sendPayload plus the key a resolved Handle adds. It is a
// separate type rather than a field on sendPayload so T1's own assertions keep
// asserting T1's exact shape (D-063-10).
type handleSendPayload struct {
	ID           string `json:"id"`
	To           string `json:"to"`
	ResolvedFrom string `json:"resolvedFrom"`
}

// TestSendToAnUnknownHandleIsRefused: `#` is reserved for the family, so a typo
// is an error rather than a new channel silently created under a name no reader
// can ever subscribe to.
func TestSendToAnUnknownHandleIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent := workspace(t, home, "agent")
	t.Chdir(agent)

	stdout, stderr, code := runCLI(t, "send", "--to", "#parnet", "--message", "…")
	if code != 1 {
		t.Fatalf("send --to #parnet exit %d, want 1 (stdout %q)", code, stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q on a refused send, want it empty", stdout)
	}
	if !strings.Contains(stderr, "#parnet") || !strings.Contains(stderr, "#parent") {
		t.Errorf("the refusal must name the typo and list the handles that exist: %s", stderr)
	}

	// And no address beginning with `#` reached the store.
	st, err := store.Open(filepath.Join(home, ".auto", "mail", "alpha-store.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	var rows int
	if err := st.QueryRowContext(context.Background(),
		`SELECT count(*) FROM mail WHERE to_address LIKE '#%'`).Scan(&rows); err != nil {
		t.Fatalf("count handle-shaped mail: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d mail rows are addressed to a handle, want 0", rows)
	}
}

// TestHandleIsRejectedInEveryAddressPosition is AC-4 and D-063-2 at the command
// surface: `#` is legal in exactly one position, `send --to`, and refused in the
// three that take something durable.
//
// The loop covers both refusals in every position, because they are different
// mistakes: `#parent` is a real handle in the wrong place, and `#parnet` does
// not exist anywhere. Being told the wrong one of those sends the caller off to
// fix something that is not broken.
func TestHandleIsRejectedInEveryAddressPosition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent := workspace(t, home, "agent")
	t.Chdir(agent)

	// A real subscription first, so the store exists and holds ordinary rows.
	// The closing assertion is "no row begins with `#`", and against an empty
	// database that would pass without proving anything.
	if _, stderr, code := runCLI(t, "subscribe", "auto-stack/supervisor"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}

	positions := map[string][]string{
		"subscribe":      {"subscribe", "%s"},
		"list --address": {"list", "--address", "%s"},
		"send --from":    {"send", "--from", "%s", "--to", "auto-web/bugs", "--message", "…"},
		"send --to":      {"send", "--to", "%s", "--message", "…"},
	}
	for position, template := range positions {
		for _, value := range []string{"#parent", "#parnet"} {
			// `send --to '#parent'` is the one legal pairing, and it fails here
			// for a different reason entirely (no Subagent marker), which
			// TestParentHandleAtTheCommandSurface already covers.
			if position == "send --to" && value == "#parent" {
				continue
			}
			args := slices.Clone(template)
			for i, arg := range args {
				if arg == "%s" {
					args[i] = value
				}
			}

			stdout, stderr, code := runCLI(t, args...)
			if code != 1 {
				t.Errorf("%s %q exit %d, want 1 (stdout %q)", position, value, code, stdout)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("%s %q printed %q on stdout, want it empty — a refusal must "+
					"never leave parseable output", position, value, stdout)
			}
			if !strings.Contains(stderr, value) {
				t.Errorf("%s %q: the refusal does not name the offending value: %s",
					position, value, stderr)
			}
			switch value {
			case "#parnet":
				if !strings.Contains(stderr, "#parent") {
					t.Errorf("%s %q: an unknown handle must list the handles that "+
						"exist: %s", position, value, stderr)
				}
			case "#parent":
				if !strings.Contains(stderr, position) {
					t.Errorf("%s %q: the position refusal must name the position it "+
						"is about: %s", position, value, stderr)
				}
			}
		}
	}

	// The loop that closes AC-4: a refusal that still wrote is the failure this
	// catches, and it is asserted against the store rather than against the
	// command's own report of what it did.
	assertNoHandleRows(t, home)
}

// assertNoHandleRows is AC-4's stored half: nothing beginning with `#` may reach
// either column that holds an address. Reading the store directly is what makes
// this an assertion rather than a restatement of the CLI's own output.
func assertNoHandleRows(t *testing.T, home string) {
	t.Helper()
	st, err := store.Open(filepath.Join(home, ".auto", "mail", "alpha-store.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	for _, query := range []string{
		`SELECT count(*) FROM subscriptions WHERE address LIKE '#%'`,
		`SELECT count(*) FROM mail WHERE to_address LIKE '#%'`,
		`SELECT count(*) FROM mail WHERE json_extract(envelope, '$.from') LIKE '#%'`,
	} {
		var rows int
		if err := st.QueryRowContext(context.Background(), query).Scan(&rows); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if rows != 0 {
			t.Errorf("%d rows match %s, want 0 — a handle is resolved at send time "+
				"and never stored (G5)", rows, query)
		}
	}
}

// TestTheFourRefusalsAreDistinguishable is the core of AC-3, AC-4 and AC-10
// taken together: four failures, four fixes, and an agent reading stderr has
// only the sentence to tell them apart.
//
// Pairwise inequality is the weak half. The stronger half is that each message
// carries the thing its own fix needs — become a Subagent, subscribe in the
// supervisor, spell the handle correctly, use an absolute address — which is
// what a caller actually acts on.
func TestTheFourRefusalsAreDistinguishable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Each scenario gets its own workspace, and therefore its own binding: with
	// no tmux the cwd rung is what separates two agents, and `#parent` resolves
	// from the binding, so a shared directory would let one scenario resolve
	// against another's subscription.
	notASubagent := workspace(t, home, "not-a-subagent")
	noSupervisor := workspace(t, home, "no-supervisor")

	t.Chdir(notASubagent)
	if _, stderr, code := runCLI(t, "subscribe", "auto-stack/supervisor"); code != 0 {
		t.Fatalf("subscribe exit %d, stderr: %s", code, stderr)
	}
	refusals := map[string]string{
		// A supervisor's subscription exists and is resolvable; no marker does.
		"not a subagent": refusal(t, "send", "--to", "#parent", "--message", "hello?"),
	}

	// A marker, and deliberately no subscription under this binding.
	t.Chdir(noSupervisor)
	mail.ObserveHookEvent(home, mail.BindingFor(noSupervisor), "PreToolUse", mail.ActiveAgent{
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
	})
	refusals["no supervisor"] = refusal(t, "send", "--to", "#parent", "--message", "phase 3 blocked")
	refusals["unknown handle"] = refusal(t, "send", "--to", "#parnet", "--message", "…")
	refusals["not allowed here"] = refusal(t, "subscribe", "#parent")

	wanted := map[string][]string{
		"not a subagent":   {"Subagent", "auto mail docs"},
		"no supervisor":    {"auto mail subscribe", "supervisor"},
		"unknown handle":   {"#parnet", "#parent"},
		"not allowed here": {"subscribe", "absolute address"},
	}
	for name, text := range refusals {
		for _, want := range wanted[name] {
			if !strings.Contains(text, want) {
				t.Errorf("the %q refusal does not mention %q — it must carry what its "+
					"own fix needs: %s", name, want, text)
			}
		}
		for otherName, other := range refusals {
			if otherName != name && text == other {
				t.Errorf("the %q and %q refusals are the same sentence: %s",
					name, otherName, text)
			}
		}
	}

	assertNoHandleRows(t, home)
}

// refusal runs a command that must fail, and returns what it said on stderr.
// It asserts the shared half of every refusal — exit 1, stdout empty — so the
// callers above can be about the sentences.
func refusal(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, args...)
	if code != 1 {
		t.Fatalf("%v exit %d, want 1 (stdout %q, stderr %q)", args, code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("%v printed %q on stdout, want it empty", args, stdout)
	}
	return stderr
}

// TestResetRemovesSubagentMarkers is AC-9's last clause, and it is the clause
// with the sharpest failure mode.
//
// A store or a flag that outlives a reset makes the next run *noisy* — a stale
// nudge, a listing that is not empty. A marker that outlives one makes it
// *wrong and quiet*: `#parent` from a process that is not a Subagent at all
// resolves to a real address and the mail goes somewhere plausible, instead of
// being refused with a hint. So the marker directory is wiped with the rest,
// named in `removed`, and — the part a reader of the payload depends on — a
// host whose only leftover state is a marker is not told there was nothing to
// remove.
func TestResetRemovesSubagentMarkers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	supervisor := workspace(t, home, "supervisor")
	t.Chdir(supervisor)
	agentsDir := filepath.Join(home, ".auto", "mail", "alpha-agents")

	// A marker and nothing else: no store, no flags. This is the case the
	// "nothing to reset" short-circuit would answer wrongly if it only looked
	// at the store.
	mail.ObserveHookEvent(home, mail.BindingFor(supervisor), "PreToolUse", mail.ActiveAgent{
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
	})
	if _, err := os.Stat(agentsDir); err != nil {
		t.Fatalf("the hook left no marker directory to reset (%v)", err)
	}

	stdout, stderr, code := runCLI(t, "reset")
	if code != 0 {
		t.Fatalf("reset with only a marker on disk exit %d, stderr: %s", code, stderr)
	}
	if removed := decode[resetPayload](t, stdout).Removed; !slices.Contains(removed, agentsDir) {
		t.Errorf("removed = %v, want it to name %q — a host holding only a stale marker "+
			"has something to wipe, and reporting nothing would leave it there", removed, agentsDir)
	}
	if _, err := os.Stat(agentsDir); !os.IsNotExist(err) {
		t.Errorf("the marker directory survived the reset (stat err = %v)", err)
	}

	// And the state the reset was for: `#parent` refuses again, because there
	// is no longer any evidence that a Subagent is acting here.
	if _, _, code := runCLI(t, "subscribe", "auto-stack/supervisor"); code != 0 {
		t.Fatalf("subscribe after reset exit %d", code)
	}
	stdout, stderr, code = runCLI(t, "send", "--to", "#parent", "--message", "still a Subagent?")
	if code != 1 {
		t.Fatalf("send --to #parent after a reset exit %d, want 1 — the marker is gone, "+
			"so the caller can no longer be shown to be a Subagent (stdout %q)", code, stdout)
	}
	if !strings.Contains(stderr, "Subagent") {
		t.Errorf("the post-reset refusal does not name the constraint: %s", stderr)
	}

	// A reset that wipes a store as well still names all three artifacts, so
	// the marker directory is not only removed on the marker-only path.
	if _, stderr, code := runCLI(t, "send", "--to", "auto-stack/supervisor", "--message", "mail"); code != 0 {
		t.Fatalf("send exit %d, stderr: %s", code, stderr)
	}
	mail.ObserveHookEvent(home, mail.BindingFor(supervisor), "PreToolUse", mail.ActiveAgent{
		AgentID: "a84a3676a847c5c0b", AgentType: "phase3",
	})
	stdout, stderr, code = runCLI(t, "reset", "--yes")
	if code != 0 {
		t.Fatalf("reset --yes exit %d, stderr: %s", code, stderr)
	}
	removed := decode[resetPayload](t, stdout).Removed
	for _, want := range []string{
		filepath.Join(home, ".auto", "mail", "alpha-store.db"),
		filepath.Join(home, ".auto", "mail", "alpha-flags"),
		agentsDir,
	} {
		if !slices.Contains(removed, want) {
			t.Errorf("removed = %v, want it to name %q", removed, want)
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Errorf("%s survived the reset (stat err = %v)", want, err)
		}
	}
}
