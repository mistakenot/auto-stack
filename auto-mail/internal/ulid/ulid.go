// Package ulid mints the sender-generated ids mail uses for its events and its
// stored mail rows.
//
// Three properties are load-bearing and none of them are decoration:
//
//   - **Sender-generated.** A sender must be able to post mail with nothing else
//     alive (D-11), so the id cannot come from a database sequence or a daemon.
//   - **Lexicographically sortable.** The subscription cursor is a plain string
//     comparison against a mail id (`m.id > from_cursor`), which is only a
//     correct "everything after this point" test if byte order matches time
//     order. It is also the property a future cross-host log merge depends on
//     (docs/auto-mail-beyond-mvp.md §3).
//   - **Monotonic within a process.** Two mails sent in the same millisecond
//     must still order, or the cursor would admit or skip both together.
package ulid

import (
	"crypto/rand"
	"sync"
	"time"
)

// Length is the character length of the canonical ULID encoding.
const Length = 26

// crockford is Crockford base32, minus I, L, O and U. Its characters are in
// ascending ASCII order, which is what makes the encoded form sort the same way
// the 128-bit value does.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
)

// New returns a new ULID: 48 bits of Unix milliseconds followed by 80 bits of
// entropy, encoded as 26 Crockford base32 characters.
//
// Ids minted in the same millisecond are made monotonic by incrementing the
// entropy of the previous id rather than drawing fresh randomness, so a rapid
// burst still sorts in generation order.
func New() string {
	return NewAt(time.Now())
}

// NewAt is New with an explicit clock, for tests that need a fixed timestamp.
func NewAt(t time.Time) string {
	ms := uint64(t.UnixMilli())

	mu.Lock()
	switch {
	case ms > lastMS:
		lastMS = ms
		randomize(&lastRand)
	default:
		// Same millisecond, or a clock that went backwards: stay on the
		// previous timestamp and step the entropy, so the id still sorts after
		// its predecessor. A carry out of the top byte is a 2^80 event; it
		// borrows a millisecond rather than repeating an id.
		if !increment(&lastRand) {
			lastMS++
			randomize(&lastRand)
		}
		ms = lastMS
	}
	var raw [16]byte
	for i := range 6 {
		raw[i] = byte(ms >> (8 * (5 - uint(i))))
	}
	copy(raw[6:], lastRand[:])
	mu.Unlock()

	return encode(raw)
}

// MinSubscriptionChars is how many ULID characters a subscription id carries in
// the common case — the `sub_01K9X2M4` shape C1 shows.
//
// Eight characters is a *display* length, not a unique one: they encode only
// the top 40 bits of the 48-bit timestamp, so every subscription created within
// the same ~256ms window produces the identical string. Two agents subscribing
// back to back is not a rare event on this host, so callers must resolve the
// collision rather than assume it away — see SubscriptionIDFrom.
const MinSubscriptionChars = 8

// SubscriptionID is a subscription's identifier in its canonical short form:
// `sub_` plus the leading 8 characters of a fresh ULID. The leading characters
// are the timestamp, so subscription ids sort by creation time — which is what
// makes "lowest subscription id" a deterministic tie-break when the sender's
// from-address is resolved.
//
// Because that form is not collision-free (see MinSubscriptionChars), the store
// mints ids through SubscriptionIDFrom and lengthens the prefix until the id is
// free.
func SubscriptionID() string {
	return SubscriptionIDFrom(New(), MinSubscriptionChars)
}

// SubscriptionIDFrom builds a subscription id from an existing ULID using its
// leading `chars` characters, clamped to the canonical short form at the bottom
// and to the whole ULID at the top. Lengthening the prefix is how a caller
// resolves a collision without ever reaching for a second source of ids: the
// full ULID is unique, so a long enough prefix always is.
func SubscriptionIDFrom(u string, chars int) string {
	chars = max(chars, MinSubscriptionChars)
	chars = min(chars, len(u))
	return "sub_" + u[:chars]
}

func randomize(b *[10]byte) {
	// crypto/rand.Read never returns an error on any supported platform (it
	// panics internally on a broken entropy source), so there is nothing here
	// a caller could usefully handle.
	_, _ = rand.Read(b[:])
}

// increment adds one to a big-endian 80-bit value, reporting false on overflow.
func increment(b *[10]byte) bool {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return true
		}
	}
	return false
}

// encode writes the 128-bit value as 26 base32 characters, most significant bit
// first. 26 characters carry 130 bits, so the value is left-padded with two
// zero bits — the canonical ULID layout.
func encode(raw [16]byte) string {
	out := make([]byte, Length)
	bit := 0
	for i := range out {
		var v byte
		for range 5 {
			// The first two bit positions are the zero padding; everything
			// after them indexes into the 128-bit value.
			if idx := bit - 2; idx >= 0 {
				v = v<<1 | (raw[idx/8]>>(7-uint(idx%8)))&1
			} else {
				v <<= 1
			}
			bit++
		}
		out[i] = crockford[v]
	}
	return string(out)
}
