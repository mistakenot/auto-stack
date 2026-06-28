package cli

import (
	"errors"
	"fmt"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/setupscript"
	"github.com/spf13/cobra"
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
	fmt.Fprint(cmd.OutOrStdout(), setupscript.Generate(params))
	return nil
}
