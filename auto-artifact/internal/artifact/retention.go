package artifact

import (
	"fmt"
	"slices"
	"strings"
)

// DefaultRetention is the tier used when no --retain flag (and no config
// default) is given.
const DefaultRetention = "90d"

// RetentionTiers is the exact, closed set of accepted retention tiers. Each maps
// to a prefix-scoped S3 lifecycle rule; uploads must land in one of them.
var RetentionTiers = []string{"7d", "30d", "90d", "365d"}

// ValidRetention reports whether tier is one of the four accepted tiers.
func ValidRetention(tier string) bool {
	return slices.Contains(RetentionTiers, tier)
}

// ResolveRetention picks the effective retention tier: the --retain flag when
// set, else the config default, else DefaultRetention. It validates the result
// and returns an error (for rejection before any S3 call) on an unknown tier.
func ResolveRetention(flagValue, configDefault string) (string, error) {
	tier := strings.TrimSpace(flagValue)
	if tier == "" {
		tier = strings.TrimSpace(configDefault)
	}
	if tier == "" {
		tier = DefaultRetention
	}
	if !ValidRetention(tier) {
		return "", fmt.Errorf("invalid retention tier %q: must be one of %s", tier, strings.Join(RetentionTiers, ", "))
	}
	return tier, nil
}
