package graph

import (
	"errors"
	"fmt"
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

// ErrNoRankBetween is returned when no rank key sorts strictly between two
// bounds (they are equal, out of order, or adjacent such as "a" and "a0").
var ErrNoRankBetween = errors.New("no rank sorts between the neighbours")

// RankBetween returns a rank that sorts strictly between lo and hi. An empty
// lo means "before everything", an empty hi "after everything". The result
// never ends in '0', so there is always room to insert before it again.
func RankBetween(lo, hi string) (string, error) {
	if hi != "" && lo >= hi {
		return "", ErrNoRankBetween
	}
	for _, k := range []string{lo, hi} {
		if k != "" && !RankPattern.MatchString(k) {
			return "", fmt.Errorf("rank %q must match %s", k, RankPattern)
		}
	}
	r := rankMid(lo, hi)
	if r <= lo || (hi != "" && r >= hi) {
		return "", ErrNoRankBetween
	}
	return r, nil
}

// rankMid is the fractional-index midpoint of two base-36 digit strings read
// as fractions (lo < hi; hi == "" is 1). After rocicorp's fractional-indexing.
func rankMid(lo, hi string) string {
	if hi != "" {
		n := 0
		for n < len(hi) && digitAt(lo, n) == hi[n] {
			n++
		}
		if n > 0 {
			if n == len(hi) {
				// lo extends hi with zeros: nothing fits between them.
				return hi
			}
			return hi[:n] + rankMid(suffix(lo, n), hi[n:])
		}
	}
	dLo := 0
	if lo != "" {
		dLo = strings.IndexByte(rankDigits, lo[0])
	}
	dHi := len(rankDigits)
	if hi != "" {
		dHi = strings.IndexByte(rankDigits, hi[0])
	}
	if dHi-dLo > 1 {
		return string(rankDigits[(dLo+dHi+1)/2])
	}
	// Adjacent first digits. hi's first digit alone sorts between them unless
	// hi is that digit followed only by zeros.
	if len(hi) > 1 && strings.Trim(hi[1:], "0") != "" {
		return hi[:1]
	}
	return string(rankDigits[dLo]) + rankMid(suffix(lo, 1), "")
}

func digitAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return '0'
}

func suffix(s string, n int) string {
	if n >= len(s) {
		return ""
	}
	return s[n:]
}

// ParseRef splits a possibly qualified reference. `005:r-8hw3` yields
// ("005", "r-8hw3", true); a plan-local `r-8hw3` yields ("", "r-8hw3", false).
func ParseRef(ref string) (plan, id string, qualified bool) {
	if p, rest, ok := strings.Cut(ref, ":"); ok {
		return p, rest, true
	}
	return "", ref, false
}
