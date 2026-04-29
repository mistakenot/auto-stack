package cli

import (
	"fmt"
	"io"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

func writeValidationErrors(stderr io.Writer, errs []sharedconfig.ValidationError) {
	for _, err := range errs {
		field := err.Field
		if field == "" {
			field = "<root>"
		}
		if err.Value != nil {
			fmt.Fprintf(stderr, "%s %s: %s (value=%v)\n", err.Code, field, err.Message, err.Value)
			continue
		}
		fmt.Fprintf(stderr, "%s %s: %s\n", err.Code, field, err.Message)
	}
}
