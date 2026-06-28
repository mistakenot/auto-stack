package cli

import (
	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/artifact"
	"github.com/mistakenot/auto-artifact/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	var cfg config.Settings
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Store S3 bucket config and upload credentials in ~/.auto/artifact/settings.json",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, application, cfg)
		},
	}
	cmd.Flags().StringVar(&cfg.Endpoint, "endpoint", "", "S3 endpoint URL, e.g. https://s3.eu-west-1.amazonaws.com (required)")
	cmd.Flags().StringVar(&cfg.Bucket, "bucket", "", "S3 bucket name (required)")
	cmd.Flags().StringVar(&cfg.Region, "region", "", "AWS region, e.g. eu-west-1 (required)")
	cmd.Flags().StringVar(&cfg.AccessKeyID, "access-key-id", "", "IAM access key id (required)")
	cmd.Flags().StringVar(&cfg.SecretAccessKey, "secret-access-key", "", "IAM secret access key (required)")
	cmd.Flags().StringVar(&cfg.DefaultRetention, "default-retention", artifact.DefaultRetention, "default retention tier: 7d, 30d, 90d, or 365d")
	return cmd
}

func runInit(cmd *cobra.Command, _ *app.App, cfg config.Settings) error {
	path, err := config.SettingsPath()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	if errs := config.Validate(path, cfg); len(errs) > 0 {
		return &ExitError{Code: 1, Err: &config.ValidationErrorsError{Path: path, Errors: errs}}
	}

	if err := config.WriteSecure(path, cfg); err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	if err := writeJSON(cmd.OutOrStdout(), map[string]any{
		"path":    path,
		"bucket":  cfg.Bucket,
		"region":  cfg.Region,
		"written": true,
	}); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
