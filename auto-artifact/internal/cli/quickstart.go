package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto artifact — quickstart

Upload evidence files (screenshots, videos, logs) to S3 and get back a
permanent public HTTPS URL you can drop into a PR comment.

## 1. Provision a bucket (once)

Print and run the provisioning script in an AWS-authenticated shell:

    auto artifact setup --region eu-west-1 --bucket my-artifacts > setup.sh
    bash setup.sh

It creates the bucket (public-read, no-list), the four retention lifecycle
rules, an IAM user with PutObject/DeleteObject, and prints the access keys.

## 2. Configure (once)

    auto artifact init \
      --endpoint "https://s3.eu-west-1.amazonaws.com" \
      --bucket "my-artifacts" \
      --region "eu-west-1" \
      --access-key-id "AKIA..." \
      --secret-access-key "..."

Config is written to ~/.auto/artifact/settings.json (mode 0600).

## 3. Upload

    auto artifact upload screenshot.png
    # → {"url":"https://my-artifacts.s3.eu-west-1.amazonaws.com/90d/<uuid>/screenshot.png", ...}

Pipe the bare URL straight into a PR comment:

    URL=$(auto artifact upload screenshot.png --format text)
    gh pr comment 123 --body "Evidence: ![shot]($URL)"

Choose a retention tier with --retain 7d|30d|90d|365d (default 90d).

## 4. Delete early (optional)

    auto artifact delete "90d/<uuid>/screenshot.png"   # key or full URL

## 5. Check things are healthy

    auto artifact doctor   # validates config + a real PUT/DELETE bucket probe
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using auto artifact",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
