package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/rpc"
)

// testTimeout bounds all blocking operations in tests.
const conformanceTestTimeout = 5 * time.Second

// ---------------------------------------------------------------------------
// AC-6: Sample scenario — call, notification, dispatch count
// ---------------------------------------------------------------------------

// echoAndPushScenario is a Scenario that:
//  1. Calls the canned "echo" method and asserts the result.
//  2. Calls the canned "push" method which triggers a server-push notification,
//     then asserts the client receives it.
//  3. Checks Obs().DispatchCount for both methods.
//
// All assertions use real observables (dispatch counts, received frames).
type echoAndPushScenario struct{}

func (s *echoAndPushScenario) Name() string { return "echo-and-push" }

func (s *echoAndPushScenario) Run(t testing.TB, f Fixture) {
	ctx, cancel := context.WithTimeout(context.Background(), conformanceTestTimeout)
	defer cancel()

	client := f.Client()

	// --- Step 1: Call "echo" and assert the result ---

	result, err := client.Call(ctx, "echo", map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("echo Call: %v", err)
	}

	var echoResult map[string]string
	if err := json.Unmarshal(result, &echoResult); err != nil {
		t.Fatalf("unmarshal echo result: %v", err)
	}
	if echoResult["msg"] != "hello" {
		t.Errorf("echo result.msg = %q, want hello", echoResult["msg"])
	}

	// --- Step 2: Call "push" which triggers a server-push notification ---

	result, err = client.Call(ctx, "push", map[string]string{"data": "test"})
	if err != nil {
		t.Fatalf("push Call: %v", err)
	}

	var pushResult string
	if err := json.Unmarshal(result, &pushResult); err != nil {
		t.Fatalf("unmarshal push result: %v", err)
	}
	if pushResult != "ok" {
		t.Errorf("push result = %q, want ok", pushResult)
	}

	// Wait for the server-push notification on the client's notification channel.
	// This is a real observable — the notification must arrive within the timeout.
	select {
	case notif := <-client.Notifications():
		if notif.Method != "server.pushed" {
			t.Errorf("notification method = %q, want server.pushed", notif.Method)
		}
		var notifParams map[string]string
		if err := json.Unmarshal(notif.Params, &notifParams); err != nil {
			t.Fatalf("unmarshal notification params: %v", err)
		}
		if notifParams["data"] != "test" {
			t.Errorf("notification params.data = %q, want test", notifParams["data"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for server.pushed notification")
	}

	// --- Step 3: Check dispatch counts ---

	if got := f.Obs().DispatchCount("echo"); got != 1 {
		t.Errorf("DispatchCount(echo) = %d, want 1", got)
	}
	if got := f.Obs().DispatchCount("push"); got != 1 {
		t.Errorf("DispatchCount(push) = %d, want 1", got)
	}
	// Unregistered method should have zero count.
	if got := f.Obs().DispatchCount("nonexistent"); got != 0 {
		t.Errorf("DispatchCount(nonexistent) = %d, want 0", got)
	}
}

// TestConformanceAcrossFixtures runs the echoAndPush scenario through all 3
// fixture factories (net.Pipe, unix, tcp). All must produce identical results.
func TestConformanceAcrossFixtures(t *testing.T) {
	RunAcrossFixtures(t, &echoAndPushScenario{}, FakeFixtures()...)
}

// Ensure the scenario satisfies the Scenario interface at compile time.
var _ Scenario = (*echoAndPushScenario)(nil)

// Ensure rpc.Request is used (suppress unused import if Notifications channel
// type changes).
var _ = (rpc.Request{}).Method
