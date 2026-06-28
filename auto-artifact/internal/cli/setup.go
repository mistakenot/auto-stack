package cli

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/setupscript"
	"github.com/spf13/cobra"
)

// Strict formats for setup inputs. The generated script also single-quotes
// these values, but validating up front rejects malformed input with a clear
// error instead of emitting a script that AWS would reject (defense in depth).
var (
	bucketRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionRe  = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]$`)
	profileRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

func newSetupCmd(application *app.App) *cobra.Command {
	var params setupscript.Params
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Print a bash script that provisions the S3 bucket, policy, lifecycle rules, and IAM",
		Long: "Print a self-contained bash provisioning script to stdout. The tool makes no AWS calls — " +
			"run the emitted script yourself in an AWS-authenticated shell.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, application, params)
		},
	}
	cmd.Flags().StringVar(&params.Region, "region", "", "AWS region for the bucket (required)")
	cmd.Flags().StringVar(&params.Bucket, "bucket", "", "S3 bucket name to create (required)")
	cmd.Flags().StringVar(&params.Profile, "profile", "", "AWS CLI profile to use (optional; omit for ambient credentials)")
	return cmd
}

func runSetup(cmd *cobra.Command, _ *app.App, params setupscript.Params) error {
	if params.Region == "" {
		return &ExitError{Code: 1, Err: errors.New("--region is required")}
	}
	if params.Bucket == "" {
		return &ExitError{Code: 1, Err: errors.New("--bucket is required")}
	}
	if !regionRe.MatchString(params.Region) {
		return &ExitError{Code: 1, Err: fmt.Errorf("invalid --region %q: expected an AWS region like eu-west-1", params.Region)}
	}
	if !bucketRe.MatchString(params.Bucket) {
		return &ExitError{Code: 1, Err: fmt.Errorf("invalid --bucket %q: must be 3-63 chars, lowercase letters/digits/.-, starting and ending alphanumeric", params.Bucket)}
	}
	if params.Profile != "" && !profileRe.MatchString(params.Profile) {
		return &ExitError{Code: 1, Err: fmt.Errorf("invalid --profile %q: allowed characters are letters, digits, and _.-", params.Profile)}
	}
	fmt.Fprint(cmd.OutOrStdout(), setupscript.Generate(params))
	return nil
}
