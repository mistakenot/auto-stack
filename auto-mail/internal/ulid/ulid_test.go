package ulid_test

import (
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-mail/internal/ulid"
)

// TestMonotonicAndSortable is the property the subscription cursor rests on:
// a plain string comparison (`m.id > from_cursor`) is only a correct
// "everything after this point" test if byte order matches generation order.
// 10k ids in one process is enough to land many of them in the same
// millisecond, which is exactly the case the monotonic step exists for.
func TestMonotonicAndSortable(t *testing.T) {
	const n = 10_000
	ids := make([]string, n)
	for i := range ids {
		ids[i] = ulid.New()
		if len(ids[i]) != ulid.Length {
			t.Fatalf("id %d = %q, want %d characters", i, ids[i], ulid.Length)
		}
	}

	for i := 1; i < n; i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("id %d (%q) does not sort after id %d (%q)", i, ids[i], i-1, ids[i-1])
		}
	}

	shuffled := slices.Clone(ids)
	sort.Sort(sort.Reverse(sort.StringSlice(shuffled)))
	sort.Strings(shuffled)
	if !slices.Equal(shuffled, ids) {
		t.Error("sorting the ids does not reproduce generation order")
	}
}

// TestCharacterSet: only Crockford base32 characters, in ascending ASCII order.
// The ordering of the alphabet is what makes the encoded form sort like the
// 128-bit value it encodes.
func TestCharacterSet(t *testing.T) {
	const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for i := 1; i < len(crockford); i++ {
		if crockford[i] <= crockford[i-1] {
			t.Fatalf("alphabet is not ascending at index %d", i)
		}
	}
	for range 100 {
		for _, r := range ulid.New() {
			if !strings.ContainsRune(crockford, r) {
				t.Fatalf("id contains %q, which is outside the Crockford alphabet", r)
			}
		}
	}
}

// TestTimestampOrdersAcrossMilliseconds: ids minted a second apart sort by
// time, not merely by draw order.
func TestTimestampOrdersAcrossMilliseconds(t *testing.T) {
	base := time.Date(2026, 8, 25, 10, 14, 2, 0, time.UTC)
	earlier := ulid.NewAt(base)
	later := ulid.NewAt(base.Add(time.Second))
	if later <= earlier {
		t.Errorf("later id %q does not sort after earlier id %q", later, earlier)
	}
}

// TestSubscriptionIDShape: `sub_` plus the ULID's leading 8 characters, which
// is the shape C1 shows and the timestamp prefix the "lowest subscription id"
// tie-break in the from-address ladder depends on.
func TestSubscriptionIDShape(t *testing.T) {
	first := ulid.SubscriptionID()
	second := ulid.SubscriptionID()

	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "sub_") {
			t.Errorf("subscription id %q has no sub_ prefix", id)
		}
		if len(id) != len("sub_")+8 {
			t.Errorf("subscription id %q is %d characters, want %d", id, len(id), len("sub_")+8)
		}
	}
	if second < first {
		t.Errorf("subscription ids do not sort by creation time: %q then %q", first, second)
	}
}

// TestSubscriptionIDShortFormCollides pins the reason the store lengthens the
// prefix rather than trusting the canonical form. Eight characters carry only
// the top 40 bits of the timestamp, so two subscriptions created in the same
// ~256ms window — two agents subscribing back to back, which is ordinary here —
// mint the identical string. This is a documented property, not a bug to fix in
// the id: C1's `sub_01K9X2M4` shape is what the short form is for.
func TestSubscriptionIDShortFormCollides(t *testing.T) {
	first := ulid.SubscriptionID()
	second := ulid.SubscriptionID()
	if first != second {
		t.Skipf("%q and %q landed in different 256ms windows; nothing to pin here", first, second)
	}
}

// TestSubscriptionIDFromLengthensUntilUnique: lengthening the prefix is the
// collision escape hatch, and it terminates because the full ULID is unique.
func TestSubscriptionIDFromLengthensUntilUnique(t *testing.T) {
	a, b := ulid.New(), ulid.New()
	if a == b {
		t.Fatalf("two ULIDs collided: %q", a)
	}
	if got := ulid.SubscriptionIDFrom(a, ulid.Length); got != "sub_"+a {
		t.Errorf("SubscriptionIDFrom(%q, %d) = %q, want the whole ULID", a, ulid.Length, got)
	}
	if ulid.SubscriptionIDFrom(a, ulid.Length) == ulid.SubscriptionIDFrom(b, ulid.Length) {
		t.Error("full-length subscription ids collided; the extension can never terminate")
	}
	// Below the canonical length the id is clamped, never truncated further.
	if got := ulid.SubscriptionIDFrom(a, 1); len(got) != len("sub_")+ulid.MinSubscriptionChars {
		t.Errorf("SubscriptionIDFrom(%q, 1) = %q, want the canonical short form", a, got)
	}
	// Asking for more characters than the ULID has is clamped, not a panic.
	if got := ulid.SubscriptionIDFrom(a, 999); got != "sub_"+a {
		t.Errorf("SubscriptionIDFrom(%q, 999) = %q, want the whole ULID", a, got)
	}
}
