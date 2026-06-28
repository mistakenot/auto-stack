package cli

import (
	"fmt"
	"os"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/artifact"
	"github.com/mistakenot/auto-artifact/internal/config"
	"github.com/mistakenot/auto-artifact/internal/s3"
	"github.com/spf13/cobra"
)

// uploadResult is the JSON shape emitted by a successful upload.
type uploadResult struct {
	URL         string `json:"url"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Retention   string `json:"retention"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

func newUploadCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a file to S3 and return its permanent public URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(cmd, application, args[0])
		},
	}
}

func runUpload(cmd *cobra.Command, _ *app.App, filePath string) error {
	settingsPath, err := config.SettingsPath()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	cfg, err := config.Load(settingsPath)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("load settings: %w", err)}
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("stat %s: %w", filePath, err)}
	}

	retention := "90d"
	contentType := "application/octet-stream"

	uuid, err := artifact.NewUUIDv4()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	key := artifact.BuildKey(retention, uuid, filePath)

	client, err := s3.NewClient(cfg)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("open %s: %w", filePath, err)}
	}
	defer func() { _ = f.Close() }()

	if err := client.PutObject(cmd.Context(), key, f, info.Size(), contentType); err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	result := uploadResult{
		URL:         client.PublicURL(key),
		Bucket:      cfg.Bucket,
		Key:         key,
		Retention:   retention,
		ContentType: contentType,
		SizeBytes:   info.Size(),
	}
	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
