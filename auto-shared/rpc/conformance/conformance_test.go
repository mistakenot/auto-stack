package conformance

import (
	"context"
	"encoding/json"
	"errors"
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

// ---------------------------------------------------------------------------
// AC-5: concurrent call correlation, transport-invariant
// (promotes peer_test.go TestConcurrentCallsNoCrosstalk)
// ---------------------------------------------------------------------------

// concurrentCorrelationScenario fires N concurrent Calls against the canned
// "echo" handler (which returns its params unchanged, i.e. identity) and asserts
// every response correlates to its own request — no crosstalk — on each
// transport. Runs on the plain (non-fault) fixtures.
type concurrentCorrelationScenario struct{}

func (s *concurrentCorrelationScenario) Name() string { return "concurrent-correlation" }

func (s *concurrentCorrelationScenario) Run(t testing.TB, f Fixture) {
	client := f.Client()

	const n = 20
	type result struct {
		idx int
		val int
		err error
	}
	results := make(chan result, n)

	for i := range n {
		go func(idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), conformanceTestTimeout)
			defer cancel()
			raw, err := client.Call(ctx, "echo", map[string]int{"idx": idx})
			if err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			var got map[string]int
			if err := json.Unmarshal(raw, &got); err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			results <- result{idx: idx, val: got["idx"]}
		}(i)
	}

	// Bounded collection: every call must complete within the timeout.
	deadline := time.After(conformanceTestTimeout)
	for range n {
		select {
		case r := <-results:
			if r.err != nil {
				t.Errorf("call %d: %v", r.idx, r.err)
				continue
			}
			if r.val != r.idx {
				t.Errorf("call %d: got val %d (crosstalk)", r.idx, r.val)
			}
		case <-deadline:
			t.Fatal("timed out collecting concurrent call results")
		}
	}

	if got := f.Obs().DispatchCount("echo"); got != n {
		t.Errorf("DispatchCount(echo) = %d, want %d", got, n)
	}
}

// ---------------------------------------------------------------------------
// AC-6: connection drop mid-call releases pending callers, transport-invariant
// (promotes peer_test.go TestPendingCallReturnsOnEOF)
// ---------------------------------------------------------------------------

// dropMidCallScenario stalls the producer's write so a Call is reliably held
// in flight (request enqueued, waiter registered, frame stuck in the stalled
// transport so no response can race the sever), then severs the connection and
// asserts the pending caller returns ErrClosed promptly. Determinism comes from
// the stall, not from a sleep: without it the canned echo handler would respond
// before the sever and the test would flake.
type dropMidCallScenario struct{}

func (s *dropMidCallScenario) Name() string { return "drop-mid-call" }

func (s *dropMidCallScenario) Run(t testing.TB, f Fixture) {
	ff, ok := f.(FaultFixture)
	if !ok {
		t.Fatalf("drop-mid-call requires a FaultFixture, got %T", f)
	}
	client := f.Client()

	// Hold the producer's write so the in-flight request cannot be answered.
	ff.StallConsumer()

	callErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), conformanceTestTimeout)
		defer cancel()
		_, err := client.Call(ctx, "echo", map[string]string{"msg": "inflight"})
		callErr <- err
	}()

	// Sever the connection under the in-flight Call.
	ff.Sever()

	// The pending caller must return ErrClosed within a bounded timeout.
	select {
	case err := <-callErr:
		if !errors.Is(err, rpc.ErrClosed) {
			t.Errorf("in-flight Call returned %v, want ErrClosed", err)
		}
	case <-time.After(conformanceTestTimeout):
		t.Fatal("in-flight Call did not return after Sever")
	}

	// Observe "pending map empty / no leaked waiter": there is no public
	// accessor for the pending map, so we assert the observable consequence of
	// shutdown — which replaces pending with a fresh empty map — namely that a
	// subsequent Call short-circuits to ErrClosed (the peer is closed, so no new
	// waiter is ever registered). Together with the in-flight Call having
	// returned ErrClosed (its waiter was closed and removed), this proves no
	// waiter leaked.
	subCtx, subCancel := context.WithTimeout(context.Background(), conformanceTestTimeout)
	defer subCancel()
	subDone := make(chan error, 1)
	go func() {
		_, err := client.Call(subCtx, "echo", nil)
		subDone <- err
	}()
	select {
	case err := <-subDone:
		if !errors.Is(err, rpc.ErrClosed) {
			t.Errorf("post-sever Call returned %v, want ErrClosed", err)
		}
	case <-time.After(conformanceTestTimeout):
		t.Fatal("post-sever Call did not return ErrClosed")
	}
}

// ---------------------------------------------------------------------------
// AC-7: slow consumer triggers drop-on-full without blocking the producer
// (promotes peer_test.go TestStalledReaderTriggersDropAndClose)
// ---------------------------------------------------------------------------

// slowConsumerScenario stalls the consumer (a blocking-write fault conn) and
// pushes notifications past the bounded outbound buffer. The producer must
// never block: it observes ErrClosed (drop-on-full → shutdown) within a bounded
// timeout. Asserted via the producer's returned error, never quiescence.
type slowConsumerScenario struct{}

func (s *slowConsumerScenario) Name() string { return "slow-consumer-drop-on-full" }

func (s *slowConsumerScenario) Run(t testing.TB, f Fixture) {
	ff, ok := f.(FaultFixture)
	if !ok {
		t.Fatalf("slow-consumer requires a FaultFixture, got %T", f)
	}
	client := f.Client()

	// Stall the producer's write so its bounded out channel fills up.
	ff.StallConsumer()
	// Defensive: ensure any stalled write is released even if the assertion
	// path changes; shutdown's Close already unblocks it.
	defer ff.ReleaseConsumer()

	// Hammer notifications from a goroutine so we can assert the producer never
	// blocks: it must reach ErrClosed within the bounded timeout.
	const maxPush = 10000
	pushDone := make(chan error, 1)
	go func() {
		for range maxPush {
			if err := client.Notify("spam", nil); err != nil {
				pushDone <- err
				return
			}
		}
		pushDone <- nil
	}()

	select {
	case err := <-pushDone:
		if !errors.Is(err, rpc.ErrClosed) {
			t.Errorf("producer Notify loop returned %v, want ErrClosed (drop-on-full)", err)
		}
	case <-time.After(conformanceTestTimeout):
		t.Fatal("producer blocked: never returned ErrClosed within timeout (drop-on-full failed)")
	}
}

// TestConcurrentCorrelationAcrossFixtures runs AC-5 across pipe/unix/tcp.
func TestConcurrentCorrelationAcrossFixtures(t *testing.T) {
	RunAcrossFixtures(t, &concurrentCorrelationScenario{}, FakeFixtures()...)
}

// TestDropMidCallAcrossFixtures runs AC-6 across pipe/unix/tcp fault fixtures.
func TestDropMidCallAcrossFixtures(t *testing.T) {
	RunAcrossFixtures(t, &dropMidCallScenario{}, FaultFixtures()...)
}

// TestSlowConsumerAcrossFixtures runs AC-7 across pipe/unix/tcp fault fixtures.
func TestSlowConsumerAcrossFixtures(t *testing.T) {
	RunAcrossFixtures(t, &slowConsumerScenario{}, FaultFixtures()...)
}

// Ensure the scenarios satisfy the Scenario interface at compile time.
var (
	_ Scenario = (*echoAndPushScenario)(nil)
	_ Scenario = (*concurrentCorrelationScenario)(nil)
	_ Scenario = (*dropMidCallScenario)(nil)
	_ Scenario = (*slowConsumerScenario)(nil)
)

// Ensure rpc.Request is used (suppress unused import if Notifications channel
// type changes).
var _ = (rpc.Request{}).Method
