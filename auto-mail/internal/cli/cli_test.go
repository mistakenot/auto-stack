package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/cli"
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
