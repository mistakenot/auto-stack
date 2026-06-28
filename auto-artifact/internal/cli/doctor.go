package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/config"
	"github.com/mistakenot/auto-artifact/internal/s3"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func newDoctorCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, credentials, and bucket access",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, application)
		},
	}
}

func runDoctor(cmd *cobra.Command, _ *app.App) error {
	checks := doctorChecks(cmd)

	data, err := json.MarshalIndent(checks, "", "  ")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))

	for _, c := range checks {
		if c.Status == "fail" {
			return &ExitError{Code: 1}
		}
	}
	return nil
}

func doctorChecks(cmd *cobra.Command) []doctorCheck {
	path, err := config.SettingsPath()
	if err != nil {
		return []doctorCheck{{
			Check:   "settings_path",
			Status:  "fail",
			Message: fmt.Sprintf("cannot determine settings path: %v", err),
			Hint:    "run `auto artifact init`",
		}}
	}

	cfg, err := config.LoadValidated(path)
	if err != nil {
		return []doctorCheck{{
			Check:   "settings",
			Status:  "fail",
			Message: fmt.Sprintf("settings invalid or missing at %s: %v", path, err),
			Hint:    "run `auto artifact init` with endpoint/bucket/region/access-key-id/secret-access-key",
		}}
	}

	checks := []doctorCheck{{
		Check:   "settings",
		Status:  "pass",
		Message: "settings valid at " + path,
	}}

	client, err := s3.NewClient(cfg)
	if err != nil {
		return append(checks, doctorCheck{
			Check:   "client",
			Status:  "fail",
			Message: fmt.Sprintf("cannot build S3 client: %v", err),
			Hint:    "check endpoint/bucket/region in settings; run `auto artifact init`",
		})
	}

	// Bucket access: PUT then DELETE a tiny object (D-5). The IAM user lacks
	// ListBucket/GetObject, so HeadBucket/HeadObject would false-negative.
	if err := client.Probe(cmd.Context()); err != nil {
		return append(checks, doctorCheck{
			Check:   "bucket_access",
			Status:  "fail",
			Message: fmt.Sprintf("bucket write/delete probe failed: %v", err),
			Hint:    "verify credentials and the bucket policy (s3:PutObject/s3:DeleteObject)",
		})
	}
	return append(checks, doctorCheck{
		Check:   "bucket_access",
		Status:  "pass",
		Message: "PUT+DELETE probe succeeded against bucket " + cfg.Bucket,
	})
}
