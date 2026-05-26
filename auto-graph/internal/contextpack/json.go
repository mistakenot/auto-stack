package contextpack

import (
	"bytes"
	"encoding/json"
)

// RenderJSON renders the Pack as pretty-printed JSON with indentation.
// It uses json.Encoder with stable struct field order (the order declared
// in the Pack struct definition).
func RenderJSON(p *Pack) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// JSONEstimator returns a FormatEstimator that estimates tokens based on
// rendering the pack as JSON.
func JSONEstimator() FormatEstimator {
	return func(p *Pack) int {
		rendered, err := RenderJSON(p)
		if err != nil {
			// Fallback to a simple estimate if rendering fails.
			return defaultEstimator(p)
		}
		return EstimateTokens(rendered)
	}
}
