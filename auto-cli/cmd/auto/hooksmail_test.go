package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/mail"
)

// The mail nudge is the riskiest integration in auto-mail's walking skeleton:
// hook → notification, in-band, at stat cost, without ever being able to stall
// or break the agent's turn (G8/AC-10). These tests are the auto-cli half of
// that claim — the package-level half (zero store opens on either path) lives
// in auto-mail/mail/mail_test.go.

// isolateHookEnv gives a test its own HOME and forces the binding down to the
// cwd rung, so the flag a test plants and the flag the hook stats are the same
// one regardless of whether the test process happens to be running under tmux
// or ntm. AUTO_WATCH_HOOK_ADDR points at a closed port so the bus POST drops
// immediately instead of waiting on a daemon that is not there.
func isolateHookEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")
	t.Setenv("TMUX", "")
	t.Setenv("NTM_SPAWN_BATCH_ID", "")
	return home
}

// seedPendingMail subscribes the given workspace's binding to an address and
// sends it one mail, leaving a real pending flag behind. It deliberately goes
// through the client rather than writing the flag file directly: the thing
// under test is that a send by one agent nudges another, and hand-planting the
// marker would test the test's idea of the flag path rather than mail's.
func seedPendingMail(t *testing.T, home, workspace, address string) string {
	t.Helper()
	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	reader := mail.BindingFor(workspace)
	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: address, Binding: reader}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sent, err := client.Send(ctx, mail.SendInput{
		To:      address,
		From:    "auto-stack/reviewer",
		Body:    map[string]any{"message": "the port is dropped on ssh:// URLs"},
		Binding: mail.BindingFor(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !mail.HasPending(home, reader) {
		t.Fatalf("the send left no pending flag for %s; nothing could ever nudge", workspace)
	}
	return sent.ID
}

// ackAll retires everything the workspace's binding holds, which is what lowers
// the flag again.
func ackAll(t *testing.T, home, workspace string) {
	t.Helper()
	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	binding := mail.BindingFor(workspace)
	listed, err := client.List(ctx, mail.ListInput{Binding: binding})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, delivery := range listed {
		if _, err := client.Ack(ctx, mail.AckInput{MailID: delivery.ID, Binding: binding}); err != nil {
			t.Fatalf("Ack %s: %v", delivery.ID, err)
		}
	}
}

// postToolUsePayload is the minimal PostToolUse payload a hook fires with.
func postToolUsePayload(cwd string) string {
	return toJSON(map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      "sess-mail",
		"cwd":             cwd,
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": filepath.Join(cwd, "README.md")},
	})
}

// TestFireNudgesOnlyWhenMailIsWaiting is the round trip: no flag, no stdout;
// mail waiting, one nudge naming the command that reads it; acked, silent
// again. The nudge is the whole in-band notification path, so its presence and
// its absence are equally the contract.
func TestFireNudgesOnlyWhenMailIsWaiting(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")

	if out := runFire(t, "claude", postToolUsePayload(workspace)); strings.TrimSpace(out) != "" {
		t.Fatalf("fired with no mail waiting and got stdout: %q", out)
	}

	seedPendingMail(t, home, workspace, "auto-web/bugs")

	resp := decodeHint(t, runFire(t, "claude", postToolUsePayload(workspace)))
	if resp.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", resp.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "auto mail list") {
		t.Errorf("additionalContext = %q, want it to name `auto mail list`",
			resp.HookSpecificOutput.AdditionalContext)
	}

	// A different workspace is a different binding: the nudge is addressed, not
	// broadcast to every agent on the host.
	other := initGitRepo(t, "main")
	if out := runFire(t, "claude", postToolUsePayload(other)); strings.TrimSpace(out) != "" {
		t.Errorf("an unrelated workspace was nudged: %q", out)
	}

	ackAll(t, home, workspace)
	if out := runFire(t, "claude", postToolUsePayload(workspace)); strings.TrimSpace(out) != "" {
		t.Errorf("fired after the ack emptied the mailbox and got stdout: %q", out)
	}
}

// TestFireNudgeCarriesNoMailboxContent: the nudge is a constant instruction to
// go and read mail. The sender controls the address and the body, and this text
// lands in the recipient's context window, so nothing sender-controlled may
// appear in it (G14's content rule).
func TestFireNudgeCarriesNoMailboxContent(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")

	const address = "SENDER-CONTROLLED-ADDRESS-4242"
	seedPendingMail(t, home, workspace, address)

	resp := decodeHint(t, runFire(t, "claude", postToolUsePayload(workspace)))
	got := resp.HookSpecificOutput.AdditionalContext
	for _, sender := range []string{address, "auto-stack/reviewer", "ssh://"} {
		if strings.Contains(got, sender) {
			t.Errorf("the nudge interpolated sender-controlled content %q: %q", sender, got)
		}
	}
	if got != mail.NudgeText() {
		t.Errorf("additionalContext = %q, want exactly the constant %q", got, mail.NudgeText())
	}
}

// TestFireNudgesOnPostToolUseOnly: every other installed event stays silent, so
// the hook never writes stdout an agent has no response contract for.
func TestFireNudgesOnPostToolUseOnly(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "main")
	seedPendingMail(t, home, workspace, "auto-web/bugs")

	for _, event := range []string{"PreToolUse", "SessionStart", "SessionEnd", "Stop", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			payload := toJSON(map[string]any{
				"hook_event_name": event,
				"cwd":             workspace,
			})
			if out := runFire(t, "claude", payload); strings.TrimSpace(out) != "" {
				t.Errorf("%s emitted stdout: %q", event, out)
			}
		})
	}

	// The same state on PostToolUse does nudge — otherwise this test would pass
	// on a nudge that never fires at all.
	if out := runFire(t, "claude", postToolUsePayload(workspace)); strings.TrimSpace(out) == "" {
		t.Error("PostToolUse emitted nothing with mail waiting; the negative cases above prove nothing")
	}
}

// TestFireEmitsOneObjectWhenMailAndHintBothMatch is D-062-9 made observable.
// Two producers share one stdout, and two JSON objects on one hook's stdout is
// undefined behaviour in both agents' hook contracts — so they must ride a
// single hookSpecificOutput, with mail first so a project's hint rules can
// never bury the nudge.
func TestFireEmitsOneObjectWhenMailAndHintBothMatch(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "feat/login")
	writeHooksConfig(t, workspace, validHooksConfig)
	seedPendingMail(t, home, workspace, "auto-web/bugs")

	out := runFire(t, "claude", bashPushPayload(workspace))

	if n := strings.Count(strings.TrimSpace(out), "\n"); n != 0 {
		t.Fatalf("the hook wrote %d lines, want exactly one JSON object:\n%s", n+1, out)
	}
	var resp hookResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("stdout is not one JSON object: %v (%q)", err, out)
	}
	ctx := resp.HookSpecificOutput.AdditionalContext
	nudgeAt := strings.Index(ctx, "auto mail list")
	hintAt := strings.Index(ctx, "feat/login")
	if nudgeAt < 0 {
		t.Fatalf("additionalContext = %q, want the mail nudge", ctx)
	}
	if hintAt < 0 {
		t.Fatalf("additionalContext = %q, want the matching hint too", ctx)
	}
	if nudgeAt > hintAt {
		t.Errorf("additionalContext = %q, want mail first — the ordering is fixed so a "+
			"project's hint rules can never suppress the nudge", ctx)
	}
}

// TestFireSurvivesABrokenMailDirectory: the store absent, the flag directory
// unreadable, and the flag path occupied by something that is not a flag at
// all. Every one of them must read as "no mail" — exit 0, nothing on stdout.
// This is the "can never break the hook" half of AC-10, and it is the reason
// HasPending has no error return.
func TestFireSurvivesABrokenMailDirectory(t *testing.T) {
	cases := map[string]func(t *testing.T, home, workspace string){
		"no mail directory at all": func(*testing.T, string, string) {},
		"mail directory is a file": func(t *testing.T, home, _ string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(home, ".auto"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".auto", "mail"), []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt store file": func(t *testing.T, home, _ string) {
			t.Helper()
			dir := filepath.Join(home, ".auto", "mail")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "alpha-store.db"), []byte("this is not sqlite"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable flag directory": func(t *testing.T, home, _ string) {
			t.Helper()
			dir := filepath.Join(home, ".auto", "mail", "alpha-flags")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			// Mode is applied after creation so the parents stay traversable —
			// the case under test is one unreadable directory, not an
			// unreachable home.
			if err := os.Chmod(dir, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			home := isolateHookEnv(t)
			workspace := initGitRepo(t, "main")
			breakIt(t, home, workspace)

			if out := runFire(t, "claude", postToolUsePayload(workspace)); strings.TrimSpace(out) != "" {
				t.Errorf("emitted stdout with a broken mail directory: %q", out)
			}
		})
	}
}

// TestFireCreatesNoMailStore is the observable form of G8: the hook stats one
// path and never opens the store, so firing hooks on a host where
// `auto mail init` was never run leaves alpha-store.db non-existent. A store
// open in the agent's hot path would create it, which is exactly what makes
// this assertion able to catch the regression.
func TestFireCreatesNoMailStore(t *testing.T) {
	home := isolateHookEnv(t)
	workspace := initGitRepo(t, "feat/login")
	writeHooksConfig(t, workspace, validHooksConfig)

	for _, payload := range []string{postToolUsePayload(workspace), bashPushPayload(workspace)} {
		runFire(t, "claude", payload)
	}

	for _, path := range []string{
		filepath.Join(home, ".auto", "mail", "alpha-store.db"),
		filepath.Join(home, ".auto", "mail", "alpha-flags"),
		filepath.Join(home, ".auto", "mail"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("firing hooks created %s (err=%v); the mail check must cost one stat "+
				"and open nothing (G8/AC-10)", path, err)
		}
	}
}
