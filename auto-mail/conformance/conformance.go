// Package conformance is the executable contract for mail.Client.
//
// It is written once, against the interface, and run against every
// implementation — the auto-shared/rpc/conformance pattern. T1 ships one
// implementation (the direct-store client, D-11), so the suite has exactly one
// target today; the point is that when T3's RPC client lands it becomes a
// second `go test` target rather than a redesign (D-062-5). Paying for the
// suite now is deliberate: with two implementations in hand, the second one is
// precisely when skipping it is most tempting.
//
// Everything here asserts through the seam. The one exception is EventCounter,
// an *optional* capability an implementation may also satisfy, used for the
// claims that are about the log rather than about what a caller sees.
//
// # Deliberately absent
//
// Nothing in this suite asserts ordering. Mail is at-least-once and unordered
// (G4); the store happens to return mail in ULID order, and a test that pinned
// that would turn an accident into a promise callers would then depend on. The
// duplicate case below is the other half of the same rail: a caller that sees
// one mail id twice must still resolve to exactly one transition.
package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/mail"
)

// EventCounter is an optional capability a Client implementation may also
// satisfy, so the suite can assert the claims that are about the append-only
// log rather than about what a caller sees — "a second ack appends no second
// event" is one of those.
//
// It is deliberately *not* on mail.Client: counting the log is an observation
// of an implementation, not part of the contract every implementation owes a
// caller. A client that does not offer it simply skips those assertions, which
// is better than widening the seam to make one test convenient.
type EventCounter interface {
	CountEvents(ctx context.Context, eventType string) (int, error)
}

// RunSuite runs every conformance case against a fresh client.
//
// newClient is called once per case and must return a client over storage no
// other case shares: the cases assume an empty mailbox and would otherwise
// couple to each other's leftovers.
func RunSuite(t *testing.T, newClient func(t *testing.T) mail.Client) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.run(&fixture{t: t, ctx: context.Background(), client: newClient(t)})
		})
	}
}

// suiteCase is one named case in the suite.
type suiteCase struct {
	name string
	run  func(f *fixture)
}

var cases = []suiteCase{
	{"reading-never-retires-mail", readingNeverRetires},
	{"ack-reports-whether-it-won-the-transition", ackReportsTheTransition},
	{"re-ack-appends-no-second-event", reAckAppendsNoSecondEvent},
	{"subscribe-backfills-the-backlog", subscribeBackfills},
	{"from-now-opts-out-of-the-backlog", fromNowOptsOut},
	{"send-reports-subscriptions-and-bound-separately", bothCounts},
	{"broadcast-gives-each-subscription-its-own-ack-state", broadcastAckState},
	{"send-to-nothing-persists-for-a-later-subscriber", sendToNothingPersists},
	{"addresses-are-free-form-and-never-normalised", freeFormAddresses},
	{"a-duplicated-delivery-still-transitions-once", duplicateDelivery},
	{"parent-resolves-to-the-supervisor-for-a-subagent-sender", parentResolvesForASubagent},
	{"parent-is-refused-for-a-sender-that-is-not-a-subagent", parentRefusesANonSubagent},
	{"parent-is-refused-when-the-supervisor-holds-no-subscription", parentRefusesWithNoSupervisor},
	{"an-unknown-handle-is-refused-whatever-the-sender-is", unknownHandleIsRefused},
	{"an-absolute-send-reports-no-resolved-from", absoluteSendHasNoResolvedFrom},
	{"a-subagents-mail-tells-the-supervisor-which-child-wrote", subagentMailIsAttributed},
	{"attribution-is-withheld-rather-than-guessed", attributionIsWithheldNotGuessed},
	{"an-ordinary-agents-mail-carries-no-attributes", ordinaryMailIsUnattributed},
}

// ── the driver ───────────────────────────────────────────────────────────────

// fixture is one client plus the calls every case repeats. Each helper fails
// the test on error, so a case body reads as the property it is asserting
// rather than as error plumbing.
type fixture struct {
	t      *testing.T
	ctx    context.Context
	client mail.Client
}

// binding is one agent's opaque physical pair. Two different targets are two
// independently bound callers — the same thing two workspaces are in the
// harness, and the reason one client can play several agents here.
func binding(name string) mail.Binding {
	return mail.Binding{Manager: mail.ManagerCwd, Target: "/workspace/" + name}
}

// senderAddress is passed explicitly on every send so the suite never depends
// on the host's project registry to resolve a from-address.
const senderAddress = "auto-stack/reviewer"

func (f *fixture) subscribe(agent, address string) mail.SubscribeResult {
	f.t.Helper()
	return f.subscribeFrom(agent, address, false)
}

func (f *fixture) subscribeFrom(agent, address string, fromNow bool) mail.SubscribeResult {
	f.t.Helper()
	out, err := f.client.Subscribe(f.ctx, mail.SubscribeInput{
		Address: address,
		FromNow: fromNow,
		Binding: binding(agent),
	})
	if err != nil {
		f.t.Fatalf("subscribe %s to %q: %v", agent, address, err)
	}
	if out.Address != address {
		f.t.Errorf("subscribe returned address %q, want %q verbatim", out.Address, address)
	}
	if out.Subscription == "" {
		f.t.Fatalf("subscribe %s to %q returned no subscription id", agent, address)
	}
	return out
}

func (f *fixture) send(to, text string) mail.SendResult {
	f.t.Helper()
	out, err := f.client.Send(f.ctx, mail.SendInput{
		To:   to,
		From: senderAddress,
		Body: map[string]any{"message": text},
	})
	if err != nil {
		f.t.Fatalf("send to %q: %v", to, err)
	}
	if out.ID == "" {
		f.t.Fatalf("send to %q returned no id", to)
	}
	if out.To != to {
		f.t.Errorf("send returned to = %q, want %q verbatim", out.To, to)
	}
	return out
}

// sendAs posts through the seam as a named sender, and returns the error rather
// than failing on it: the handle cases are about refusals as much as about
// resolution, so the error is the value under test.
//
// It passes a Binding, which f.send deliberately does not — `#parent` resolves
// from the caller's Binding, so every case here needs its own agent name or it
// would resolve against a subscription another case created (AddressForBinding
// is keyed by Binding, and answers with the first subscription made under it).
func (f *fixture) sendAs(agent, to string, sender mail.Sender) (mail.SendResult, error) {
	f.t.Helper()
	return f.client.Send(f.ctx, mail.SendInput{
		To:      to,
		Body:    map[string]any{"message": "phase 3 blocked"},
		Binding: binding(agent),
		Sender:  sender,
	})
}

func (f *fixture) list(agent string) []mail.Delivery {
	f.t.Helper()
	out, err := f.client.List(f.ctx, mail.ListInput{Binding: binding(agent)})
	if err != nil {
		f.t.Fatalf("list for %s: %v", agent, err)
	}
	return out
}

// ids is what an assertion matches on: presence of a mail id, never a count or
// a position, because mail is at-least-once and unordered (G4).
func (f *fixture) ids(agent string) map[string]int {
	f.t.Helper()
	seen := map[string]int{}
	for _, d := range f.list(agent) {
		seen[d.ID]++
	}
	return seen
}

// delivered returns the one delivery an agent holds for a mail id. It fails
// rather than returning a zero value: every caller below is about the *shape*
// of a delivery, and a zero one would satisfy an absence assertion for the
// wrong reason.
func (f *fixture) delivered(agent, mailID string) mail.Delivery {
	f.t.Helper()
	for _, d := range f.list(agent) {
		if d.ID == mailID {
			return d
		}
	}
	f.t.Fatalf("%s holds no delivery for %s", agent, mailID)
	return mail.Delivery{}
}

func (f *fixture) ack(agent, mailID string) mail.AckResult {
	f.t.Helper()
	out, err := f.client.Ack(f.ctx, mail.AckInput{MailID: mailID, Binding: binding(agent)})
	if err != nil {
		f.t.Fatalf("ack %s as %s: %v", mailID, agent, err)
	}
	if !out.Acked {
		f.t.Errorf("ack %s as %s reported acked=false; the delivery is acked either way", mailID, agent)
	}
	return out
}

// countEvents reads the log through the optional capability, reporting whether
// this implementation offers one at all.
func (f *fixture) countEvents(eventType string) (int, bool) {
	f.t.Helper()
	counter, ok := f.client.(EventCounter)
	if !ok {
		return 0, false
	}
	n, err := counter.CountEvents(f.ctx, eventType)
	if err != nil {
		f.t.Fatalf("count %s events: %v", eventType, err)
	}
	return n, true
}

// ── the cases ────────────────────────────────────────────────────────────────

// readingNeverRetires is G3 at the seam: ack is a separate explicit call, so
// two lists with nothing between them return the same mail twice. A client that
// retired on read would make every crashed reader lose its mail.
func readingNeverRetires(f *fixture) {
	f.subscribe("reader", "auto-web/bugs")
	sent := f.send("auto-web/bugs", "normalizeRemote drops the port on ssh:// URLs")

	for i := 1; i <= 2; i++ {
		if f.ids("reader")[sent.ID] == 0 {
			f.t.Fatalf("list #%d does not hold %s; reading must never retire mail", i, sent.ID)
		}
	}

	f.ack("reader", sent.ID)
	if f.ids("reader")[sent.ID] != 0 {
		f.t.Errorf("%s is still listed after an ack; acked mail leaves the default view", sent.ID)
	}
}

// ackReportsTheTransition is G13: acked and wonTransition answer two different
// questions. "Is it acked now" is what a caller acts on; "was it me" is what
// makes a race reportable instead of silent.
func ackReportsTheTransition(f *fixture) {
	f.subscribe("reader", "auto-web/bugs")
	sent := f.send("auto-web/bugs", "who wins")

	won := f.ack("reader", sent.ID)
	if !won.WonTransition {
		f.t.Error("the first ack did not report winning the transition")
	}

	lost := f.ack("reader", sent.ID)
	if lost.WonTransition {
		f.t.Error("the second ack reported winning; a delivery transitions exactly once")
	}
	if lost.AckedBy == "" || lost.AckedAt.IsZero() {
		f.t.Errorf("the losing ack was not told who won it: %+v", lost)
	}
}

// reAckAppendsNoSecondEvent is the log-level half of the same rail (G4/AC-7):
// idempotence means the second call changes nothing, not that it quietly
// records a second transition.
func reAckAppendsNoSecondEvent(f *fixture) {
	f.subscribe("reader", "auto-web/bugs")
	sent := f.send("auto-web/bugs", "ack me twice")

	f.ack("reader", sent.ID)
	before, ok := f.countEvents(mail.EventTypeAcked)
	if !ok {
		f.t.Skip("this client offers no EventCounter; the log-level half of G4 is asserted where it can be")
	}
	if before != 1 {
		f.t.Fatalf("%s events = %d after one ack, want 1", mail.EventTypeAcked, before)
	}

	f.ack("reader", sent.ID)
	f.ack("reader", sent.ID)
	if after, _ := f.countEvents(mail.EventTypeAcked); after != before {
		f.t.Errorf("%s events = %d after re-acking, want %d — a re-ack appends nothing",
			mail.EventTypeAcked, after, before)
	}
}

// subscribeBackfills is D-10 and the J2 half of G6: mail outlives the absence
// of a reader, so a subscription created afterwards still sees it, and the
// count it reports is the backlog its cursor admits.
func subscribeBackfills(f *fixture) {
	sent := f.send("auto-web/bugs", "sent before anyone was listening")

	late := f.subscribe("late", "auto-web/bugs")
	if late.Backfilled != 1 {
		f.t.Errorf("backfilled = %d, want 1 — the cursor admits the mail already stored", late.Backfilled)
	}
	if f.ids("late")[sent.ID] == 0 {
		f.t.Errorf("the late subscriber does not hold %s", sent.ID)
	}
}

// fromNowOptsOut is the other direction of the same flag: an agent that wants a
// clean slate says so explicitly, and still receives everything sent after it
// subscribed.
func fromNowOptsOut(f *fixture) {
	before := f.send("auto-web/bugs", "before")

	fresh := f.subscribeFrom("fresh", "auto-web/bugs", true)
	if fresh.Backfilled != 0 {
		f.t.Errorf("--from-now backfilled = %d, want 0", fresh.Backfilled)
	}
	if f.ids("fresh")[before.ID] != 0 {
		f.t.Errorf("a --from-now subscriber holds %s, which predates its cursor", before.ID)
	}

	after := f.send("auto-web/bugs", "after")
	if f.ids("fresh")[after.ID] == 0 {
		f.t.Errorf("a --from-now subscriber does not hold %s, sent after it subscribed", after.ID)
	}
}

// bothCounts is G6: subscriptions counts durable readers, bound counts those
// with a binding row, and a sender is told both because they mean different
// things — "nobody will ever read this" versus "nobody is holding it right
// now". Through this seam every subscription is bound at creation, so the case
// where the two diverge is a store-level test (see internal/store); what a
// client must guarantee is that the two are reported separately and both track
// the readers of the address.
func bothCounts(f *fixture) {
	nobody := f.send("auto-web/unclaimed", "hello?")
	if nobody.Subscriptions != 0 || nobody.Bound != 0 {
		f.t.Errorf("send to an unsubscribed address reported subscriptions=%d bound=%d, want 0 and 0",
			nobody.Subscriptions, nobody.Bound)
	}

	f.subscribe("first", "auto-web/bugs")
	one := f.send("auto-web/bugs", "one reader")
	if one.Subscriptions != 1 || one.Bound != 1 {
		f.t.Errorf("with one reader, send reported subscriptions=%d bound=%d, want 1 and 1",
			one.Subscriptions, one.Bound)
	}

	f.subscribe("second", "auto-web/bugs")
	two := f.send("auto-web/bugs", "two readers")
	if two.Subscriptions != 2 || two.Bound != 2 {
		f.t.Errorf("with two readers, send reported subscriptions=%d bound=%d, want 2 and 2",
			two.Subscriptions, two.Bound)
	}
}

// broadcastAckState is D-7, and the case the delivery join exists for: with one
// reader the join looks like pointless indirection, and with two it is the only
// thing keeping one agent's ack from retiring the other's mail.
func broadcastAckState(f *fixture) {
	first := f.subscribe("a", "auto-web/bugs")
	second := f.subscribe("b", "auto-web/bugs")
	if first.Subscription == second.Subscription {
		f.t.Fatalf("two agents on one address share subscription %q; that is not broadcast",
			first.Subscription)
	}

	sent := f.send("auto-web/bugs", "both of you")
	if f.ids("a")[sent.ID] == 0 || f.ids("b")[sent.ID] == 0 {
		f.t.Fatalf("%s did not reach both subscriptions", sent.ID)
	}

	f.ack("a", sent.ID)
	if f.ids("a")[sent.ID] != 0 {
		f.t.Errorf("agent a still holds %s after acking it", sent.ID)
	}
	if f.ids("b")[sent.ID] == 0 {
		f.t.Errorf("agent b lost %s when agent a acked; ack state is per subscription", sent.ID)
	}
}

// sendToNothingPersists is G6's first half: sending to an address nobody holds
// is a success, not an error. An agent that finds a bug at 3am must be able to
// post it whether or not anyone is listening yet.
func sendToNothingPersists(f *fixture) {
	sent := f.send("auto-web/nobody-yet", "found a bug")
	if sent.Subscriptions != 0 {
		f.t.Fatalf("something was subscribed to auto-web/nobody-yet: %+v", sent)
	}

	late := f.subscribe("late", "auto-web/nobody-yet")
	if late.Backfilled != 1 {
		f.t.Errorf("backfilled = %d, want 1 — the mail must have outlived the absence of a reader",
			late.Backfilled)
	}
	if f.ids("late")[sent.ID] == 0 {
		f.t.Fatalf("the later subscriber does not hold %s", sent.ID)
	}
	if won := f.ack("late", sent.ID); !won.WonTransition {
		f.t.Error("the later subscriber could not transition the backfilled mail")
	}
}

// freeFormAddresses is D-9/G5: an address is a free-form name for a channel.
// `/` is an ordinary character, never a hierarchy and never normalised away —
// which is what keeps prefix subscriptions buildable later instead of
// accidentally shipped now.
func freeFormAddresses(f *fixture) {
	for _, address := range []string{"auto-web/bugs", "bugs", "auto-web/bugs/normalize-remote"} {
		sub := f.subscribe("reader-"+address, address)
		if sub.Address != address {
			f.t.Errorf("subscribe normalised %q to %q", address, sub.Address)
		}
		sent := f.send(address, "addressed to "+address)
		if f.ids("reader-" + address)[sent.ID] == 0 {
			f.t.Errorf("mail sent to %q did not reach its subscriber", address)
		}
	}

	// The separator is not a hierarchy: a reader of `auto-web` is not a reader
	// of `auto-web/bugs`.
	f.subscribe("prefix", "auto-web")
	scoped := f.send("auto-web/bugs", "scoped to the exact address")
	if f.ids("prefix")[scoped.ID] != 0 {
		f.t.Errorf("a subscriber of %q received mail sent to %q; `/` is a character, not a hierarchy",
			"auto-web", "auto-web/bugs")
	}

	for name, address := range map[string]string{
		"the empty string": "",
		"padded":           " auto-web/bugs ",
	} {
		_, err := f.client.Subscribe(f.ctx, mail.SubscribeInput{Address: address, Binding: binding("reject")})
		if !errors.Is(err, mail.ErrInvalidAddress) {
			f.t.Errorf("subscribing to %s returned %v, want ErrInvalidAddress", name, err)
		}
	}
}

// duplicateDelivery is the caller-facing half of G4 (AC-7). At-least-once means
// a consumer can be handed the same mail id twice, so the duplicate is
// simulated where duplicates can actually appear — at the client boundary, with
// a decorator that repeats one entry in the List result.
//
// It is emphatically **not** simulated by inserting a second deliveries row:
// that table is keyed by (subscription_id, mail_id) and correctly forbids one,
// and a test that demanded the row would be asserting against the schema G2
// requires.
func duplicateDelivery(f *fixture) {
	doubled := &repeatingClient{Client: f.client}
	seam := &fixture{t: f.t, ctx: f.ctx, client: doubled}

	seam.subscribe("reader", "auto-web/bugs")
	sent := seam.send("auto-web/bugs", "you will see this twice")

	if got := seam.ids("reader")[sent.ID]; got < 2 {
		f.t.Fatalf("the decorator handed the caller %s %d times, want at least 2", sent.ID, got)
	}

	// A consumer idempotent on the mail id acks each copy it saw. Exactly one
	// of those calls transitioned the delivery, and both report it as acked.
	var wins int
	for range 2 {
		if seam.ack("reader", sent.ID).WonTransition {
			wins++
		}
	}
	if wins != 1 {
		f.t.Errorf("%d of 2 acks won the transition, want exactly 1", wins)
	}
	if n, ok := f.countEvents(mail.EventTypeAcked); ok && n != 1 {
		f.t.Errorf("%s events = %d after acking a duplicated delivery, want 1", mail.EventTypeAcked, n)
	}
	if seam.ids("reader")[sent.ID] != 0 {
		f.t.Errorf("%s is still listed after both acks", sent.ID)
	}
}

// ── the handle contract (D-063-8) ────────────────────────────────────────────
//
// Handles are asserted here, at the interface, rather than only against the
// direct client, because resolution is a *client* obligation: T3's RPC client
// will resolve `#parent` over a wire against a store on another host, and the
// four answers a caller may receive must be the same four. What is deliberately
// not here is *who the caller is* — Sender is established caller-side from the
// local filesystem (D-063-8) and passed in, so the suite states it outright
// instead of planting markers a remote implementation would never read.

// parentResolvesForASubagent is G5 and J1 at the seam: the handle is resolved at
// send time, and the envelope carries the absolute address the supervisor can be
// replied to at. The Handle itself is reported separately, and never stored.
func parentResolvesForASubagent(f *fixture) {
	f.subscribe("supervisor", "auto-stack/supervisor")

	sent, err := f.sendAs("supervisor", mail.HandleParent, mail.Sender{Kind: mail.SenderSubagent})
	if err != nil {
		f.t.Fatalf("send to %s as a Subagent: %v", mail.HandleParent, err)
	}
	if sent.To != "auto-stack/supervisor" {
		f.t.Errorf("to = %q, want the supervisor's absolute address — the stored "+
			"envelope must never carry a handle (G5)", sent.To)
	}
	if sent.ResolvedFrom != mail.HandleParent {
		f.t.Errorf("resolvedFrom = %q, want %q", sent.ResolvedFrom, mail.HandleParent)
	}
	if f.ids("supervisor")[sent.ID] == 0 {
		f.t.Errorf("the supervisor did not receive %s; resolution reported an address "+
			"it was not delivered to", sent.ID)
	}
}

// parentRefusesANonSubagent is AC-3 at the seam. The supervisor's subscription
// is right there and resolvable — the only thing standing between an ordinary
// agent and somebody else's inbox is this refusal.
func parentRefusesANonSubagent(f *fixture) {
	f.subscribe("plain-agent", "auto-stack/supervisor")

	_, err := f.sendAs("plain-agent", mail.HandleParent, mail.Sender{Kind: mail.SenderAgent})
	if !errors.Is(err, mail.ErrNotSubagent) {
		f.t.Fatalf("send to %s as an ordinary agent = %v, want ErrNotSubagent",
			mail.HandleParent, err)
	}
	if got := f.list("plain-agent"); len(got) != 0 {
		f.t.Errorf("a refused send delivered %+v, want nothing", got)
	}

	// The zero Sender is the same refusal, and that is the safe default: a
	// caller that forgot to establish one is refused rather than handed
	// whatever supervisor its Binding happens to resolve to.
	if _, err := f.sendAs("plain-agent", mail.HandleParent, mail.Sender{}); !errors.Is(err, mail.ErrNotSubagent) {
		f.t.Errorf("send with the zero Sender = %v, want ErrNotSubagent", err)
	}
}

// parentRefusesWithNoSupervisor is AC-10: a Subagent whose supervisor never
// subscribed gets its own answer, because the fix is the supervisor's.
func parentRefusesWithNoSupervisor(f *fixture) {
	// No subscribe under this binding at all. A subscription elsewhere is what
	// makes the case honest: the store is not empty, it just holds nothing
	// bound to this caller.
	f.subscribe("somebody-else", "auto-web/bugs")

	_, err := f.sendAs("orphan", mail.HandleParent, mail.Sender{Kind: mail.SenderSubagent})
	if !errors.Is(err, mail.ErrNoSupervisor) {
		f.t.Fatalf("send to %s with no supervisor subscription = %v, want ErrNoSupervisor",
			mail.HandleParent, err)
	}
	if errors.Is(err, mail.ErrNotSubagent) {
		f.t.Errorf("the refusal also matches ErrNotSubagent; the two have different fixes")
	}
	if got := f.list("somebody-else"); len(got) != 0 {
		f.t.Errorf("a refused send reached an unrelated subscription: %+v", got)
	}
}

// unknownHandleIsRefused is AC-4's typo half. `#` is reserved for the family, so
// an unrecognised member is an error rather than a channel created under a name
// no reader can ever subscribe to.
func unknownHandleIsRefused(f *fixture) {
	f.subscribe("typist", "auto-stack/supervisor")

	// Asserted with a Subagent sender on purpose: this refusal is about the
	// handle, not about who is asking, so the one caller that *could* have used
	// `#parent` is still refused `#parnet`.
	_, err := f.sendAs("typist", "#parnet", mail.Sender{Kind: mail.SenderSubagent})
	if !errors.Is(err, mail.ErrUnknownHandle) {
		f.t.Fatalf("send to %q = %v, want ErrUnknownHandle", "#parnet", err)
	}
	for _, want := range []string{"#parnet", mail.HandleParent} {
		if !strings.Contains(err.Error(), want) {
			f.t.Errorf("the refusal does not mention %q — it must name the typo and "+
				"list the handles that exist: %v", want, err)
		}
	}
	if got := f.list("typist"); len(got) != 0 {
		f.t.Errorf("a refused send delivered %+v, want nothing", got)
	}
}

// absoluteSendHasNoResolvedFrom is D-063-10 at the interface: the key is the
// signal, so an absolute send must leave it empty and marshal it away.
func absoluteSendHasNoResolvedFrom(f *fixture) {
	f.subscribe("reader", "auto-web/bugs")

	sent := f.send("auto-web/bugs", "the port is dropped on ssh:// URLs")
	if sent.ResolvedFrom != "" {
		f.t.Errorf("an absolute send reported resolvedFrom = %q, want it empty — "+
			"presence of the key is what tells a caller a handle was used",
			sent.ResolvedFrom)
	}
}

// ── the attributes contract (D-063-3, D-063-11) ──────────────────────────────

// subagentMailIsAttributed is J1's outcome at the seam: the supervisor receives
// mail from its own address — an in-process Subagent has no inbox of its own, so
// it must not have a from-address of its own — and still learns which child
// wrote, because the child's identity travels as envelope attributes rather
// than as an address (D-063-3).
//
// It is asserted here rather than only against the direct client because the
// attributes are part of what a caller is owed: T3's RPC client carries the same
// Sender over a wire, and a delivery that arrived without them would be a
// supervisor silently losing the answer to "which of my five children failed".
func subagentMailIsAttributed(f *fixture) {
	f.subscribe("supervisor", "auto-stack/supervisor")

	sent, err := f.sendAs("supervisor", mail.HandleParent, mail.Sender{
		Kind:      mail.SenderSubagent,
		AgentID:   "a84a3676a847c5c0b",
		AgentType: "phase3",
	})
	if err != nil {
		f.t.Fatalf("send to %s as a Subagent: %v", mail.HandleParent, err)
	}

	got := f.delivered("supervisor", sent.ID)
	if got.From == mail.HandleParent {
		f.t.Errorf("from = %q; a Handle must never reach a stored envelope (G5)", got.From)
	}
	if got.Attributes["senderKind"] != "subagent" {
		f.t.Errorf("attributes = %v, want senderKind \"subagent\" — it is the fact that "+
			"survives concurrency, so it is unconditional", got.Attributes)
	}
	if got.Attributes["senderAgentType"] != "phase3" {
		f.t.Errorf("attributes = %v, want senderAgentType \"phase3\" — exactly one Subagent "+
			"is live and it has a name, so the name is knowable", got.Attributes)
	}
	if _, ambiguous := got.Attributes["senderAmbiguous"]; ambiguous {
		f.t.Errorf("attributes = %v; a named sender is not ambiguous", got.Attributes)
	}
	// The opaque id is carried on the Sender for diagnostics and is deliberately
	// not an attribute: it is not what a reader is meant to match on (G5).
	if _, leaked := got.Attributes["senderAgentId"]; leaked {
		f.t.Errorf("attributes = %v; the opaque agent id is not part of the contract",
			got.Attributes)
	}
}

// attributionIsWithheldNotGuessed is D-063-11: the two ways a name can fail to
// be knowable, and the one attribute that says so.
//
// Both arms produce the same answer on purpose. A supervisor that cannot be
// given a name must be told the name is missing, whether that is because
// several children are live and the sending process cannot tell which it is, or
// because upstream spawned this one with no type at all — a real SubagentStop on
// this host carried an empty agent_type. What must never happen is a blank name
// or a sibling's, because a supervisor acting on a confidently wrong name is
// worse off than one told nothing.
func attributionIsWithheldNotGuessed(f *fixture) {
	f.subscribe("supervisor", "auto-stack/supervisor")

	arms := []struct {
		name   string
		sender mail.Sender
	}{
		{"several are live", mail.Sender{Kind: mail.SenderSubagent, Ambiguous: true}},
		{"upstream gave it no type", mail.Sender{Kind: mail.SenderSubagent, AgentID: "ad24f740374961c32"}},
	}
	for _, arm := range arms {
		sent, err := f.sendAs("supervisor", mail.HandleParent, arm.sender)
		if err != nil {
			f.t.Fatalf("send to %s (%s): %v", mail.HandleParent, arm.name, err)
		}
		got := f.delivered("supervisor", sent.ID)
		if got.Attributes["senderKind"] != "subagent" {
			f.t.Errorf("%s: attributes = %v, want senderKind — the kind is knowable even "+
				"when the name is not", arm.name, got.Attributes)
		}
		if ambiguous, ok := got.Attributes["senderAmbiguous"].(bool); !ok || !ambiguous {
			f.t.Errorf("%s: attributes = %v, want senderAmbiguous true — a supervisor must "+
				"be able to tell a withheld name from an unattributed send", arm.name, got.Attributes)
		}
		if _, named := got.Attributes["senderAgentType"]; named {
			f.t.Errorf("%s: attributes = %v carry a name that could only be a guess",
				arm.name, got.Attributes)
		}
	}
}

// ordinaryMailIsUnattributed is D-063-10 on the read side: a caller that is not
// a Subagent must not be able to tell this task shipped. The map is nil, not
// empty, so the key marshals away entirely and every existing consumer's
// assertion about a delivery's keys still holds.
func ordinaryMailIsUnattributed(f *fixture) {
	f.subscribe("reader", "auto-web/bugs")

	sent := f.send("auto-web/bugs", "the port is dropped on ssh:// URLs")
	got := f.delivered("reader", sent.ID)
	if got.Attributes != nil {
		f.t.Errorf("a delivery from an ordinary agent carries attributes %v, want none — "+
			"presence of the key is the signal, and an empty map would spend it",
			got.Attributes)
	}
}

// repeatingClient is a mail.Client that hands its caller every delivery twice.
// It is the whole simulation of at-least-once: nothing in the store changes,
// and the property under test is that a consumer which is idempotent on the
// mail id is unharmed by the repetition.
type repeatingClient struct {
	mail.Client
}

func (r *repeatingClient) List(ctx context.Context, in mail.ListInput) ([]mail.Delivery, error) {
	listed, err := r.Client.List(ctx, in)
	if err != nil {
		return nil, err
	}
	doubled := make([]mail.Delivery, 0, len(listed)*2)
	for _, d := range listed {
		doubled = append(doubled, d, d)
	}
	return doubled, nil
}
