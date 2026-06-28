package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/s3"
	"github.com/spf13/cobra"
)

func newDeleteCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <url-or-key>",
		Short: "Delete an artifact object before its retention expires",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, application, args[0])
		},
	}
}

func runDelete(cmd *cobra.Command, _ *app.App, target string) error {
	cfg, err := loadSettingsForUpload()
	if err != nil {
		return err
	}

	key, err := keyFromTarget(target)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	client, err := s3.NewClient(cfg)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if err := client.DeleteObject(cmd.Context(), key); err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	if err := writeJSON(cmd.OutOrStdout(), map[string]any{"deleted": true, "key": key}); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

// keyFromTarget accepts either a bare object key or a full artifact URL and
// returns the object key. URL paths are decoded so the key matches what was
// uploaded (the client re-encodes on the wire).
func keyFromTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("empty url or key")
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		u, err := url.Parse(target)
		if err != nil {
			return "", fmt.Errorf("parse url %q: %w", target, err)
		}
		key := strings.TrimPrefix(u.Path, "/")
		if key == "" {
			return "", fmt.Errorf("url %q has no object key", target)
		}
		return key, nil
	}
	return strings.TrimPrefix(target, "/"), nil
}
