package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/artifact"
	"github.com/mistakenot/auto-artifact/internal/config"
	"github.com/mistakenot/auto-artifact/internal/s3"
	"github.com/spf13/cobra"
)

// maxUploadBytes caps uploads at 1 GiB; larger files are rejected before any
// S3 call (AC-13).
const maxUploadBytes = 1 << 30

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
	var retain string
	var format string
	cmd := &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a file to S3 and return its permanent public URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(cmd, application, args[0], retain, format)
		},
	}
	cmd.Flags().StringVar(&retain, "retain", "", "retention tier: 7d, 30d, 90d (default), or 365d")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json or text (bare URL)")
	return cmd
}

func runUpload(cmd *cobra.Command, _ *app.App, filePath, retainFlag, format string) error {
	switch format {
	case "json", "text":
	default:
		return &ExitError{Code: 1, Err: fmt.Errorf("invalid --format %q: must be json or text", format)}
	}

	cfg, err := loadSettingsForUpload()
	if err != nil {
		return err
	}

	// Resolve and validate retention before touching the filesystem or S3 so
	// a bad tier is rejected up front (AC-4).
	retention, err := artifact.ResolveRetention(retainFlag, cfg.DefaultRetention)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("stat %s: %w", filePath, err)}
	}
	if info.Size() > maxUploadBytes {
		return &ExitError{Code: 1, Err: fmt.Errorf(
			"file too large: %d bytes exceeds the 1 GiB (%d byte) limit", info.Size(), maxUploadBytes)}
	}

	contentType := artifact.DetectContentType(filePath)

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

	if err := appendUploadLog(filePath, result); err != nil {
		// The object is already in S3; a log failure should not fail the
		// upload, but surface it on stderr.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not append to upload log: %v\n", err)
	}

	if format == "text" {
		fmt.Fprintln(cmd.OutOrStdout(), result.URL)
		return nil
	}
	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

// loadSettingsForUpload loads config, mapping a missing/invalid file to an
// init-pointing error (AC-14).
func loadSettingsForUpload() (config.Settings, error) {
	settingsPath, err := config.SettingsPath()
	if err != nil {
		return config.Settings{}, &ExitError{Code: 1, Err: err}
	}
	cfg, err := config.LoadValidated(settingsPath)
	if err != nil {
		return config.Settings{}, &ExitError{Code: 1, Err: fmt.Errorf(
			"no usable artifact config at %s (%w) — run `auto artifact init`", settingsPath, err)}
	}
	return cfg, nil
}

func appendUploadLog(originalPath string, result uploadResult) error {
	logPath, err := config.UploadsLogPath()
	if err != nil {
		return err
	}
	return artifact.AppendUploadLog(logPath, artifact.UploadRecord{
		Key:          result.Key,
		URL:          result.URL,
		OriginalPath: originalPath,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Retention:    result.Retention,
		SizeBytes:    result.SizeBytes,
		ContentType:  result.ContentType,
	})
}
