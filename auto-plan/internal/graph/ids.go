package graph

import (
	"errors"
	"math/rand/v2"
	"regexp"
	"strings"
)

// crockford is the lowercase Crockford base32 alphabet (no i, l, o, u).
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// IDPattern is the shape of every generated node ID (D-11).
var IDPattern = regexp.MustCompile(`^[a-z]{1,4}-[0-9a-hjkmnp-tv-z]{4}$`)

// maxIDAttempts bounds collision retries. 32^4 IDs per prefix makes hitting
// it practically impossible; it exists so a broken source cannot spin forever.
const maxIDAttempts = 10000

// ErrIDSpaceExhausted is returned when no free ID was found.
var ErrIDSpaceExhausted = errors.New("could not generate a free node ID")

// NewID returns prefix + "-" + 4 random Crockford base32 characters,
// regenerated while taken reports a collision (retired IDs included).
func NewID(prefix string, r *rand.Rand, taken func(string) bool) (string, error) {
	for range maxIDAttempts {
		var b strings.Builder
		b.WriteString(prefix)
		b.WriteByte('-')
		for range 4 {
			b.WriteByte(crockford[r.IntN(len(crockford))])
		}
		if id := b.String(); !taken(id) {
			return id, nil
		}
	}
	return "", ErrIDSpaceExhausted
}

// rankDigits orders rank characters; ASCII order matches string comparison.
const rankDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

// RankFirst is the rank of the first sibling.
const RankFirst = "a0"

// RankPattern is the shape of a rank key.
var RankPattern = regexp.MustCompile(`^[0-9a-z]+$`)

// RankAfter returns a rank that sorts after last. An empty last yields
// RankFirst. The last character is incremented; after 'z' a digit is
// appended, so keys stay short and never end in '0' (leaving room before
// them for inserts).
func RankAfter(last string) string {
	if last == "" {
		return RankFirst
	}
	i := strings.IndexByte(rankDigits, last[len(last)-1])
	if i >= 0 && i < len(rankDigits)-1 {
		return last[:len(last)-1] + string(rankDigits[i+1])
	}
	return last + "1"
}

// ParseRef splits a possibly qualified reference. `005:r-8hw3` yields
// ("005", "r-8hw3", true); a plan-local `r-8hw3` yields ("", "r-8hw3", false).
func ParseRef(ref string) (plan, id string, qualified bool) {
	if p, rest, ok := strings.Cut(ref, ":"); ok {
		return p, rest, true
	}
	return "", ref, false
}
