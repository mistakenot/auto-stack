package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const docsMarkdown = `# auto artifact — command reference

Upload evidence files to a public-read S3 bucket and get back permanent public
HTTPS URLs. JSON output by default; diagnostics go to stderr.

## upload <file>

Upload a single file (≤ 1 GiB). Object key is {retention}/{uuidv4}/{filename};
Content-Type is auto-detected so images/videos render inline. Appends a record
to ~/.auto/artifact/uploads.jsonl.

Flags:
  --retain 7d|30d|90d|365d   retention tier (default: config default, else 90d)
  --format json|text         json (default) or the bare URL for piping

JSON fields: url, bucket, key, retention, content_type, size_bytes.

## delete <url-or-key>

Delete an object before its lifecycle expires. Accepts a bare object key or a
full artifact URL.

## setup --region <r> --bucket <b> [--profile <p>]

Print a bash provisioning script to stdout (bucket, public-read/no-list policy,
retention lifecycle rules, IAM user + access keys, optional instance-profile
role). Makes no AWS calls itself — run the script yourself.

## init --endpoint --bucket --region --access-key-id --secret-access-key [--default-retention]

Write ~/.auto/artifact/settings.json (mode 0600). default-retention defaults to 90d.

## doctor

Validate config and bucket access (a real PUT+DELETE probe — the IAM user lacks
ListBucket/GetObject, so a head probe would false-negative). Exit 0 when healthy,
non-zero with JSON diagnostics otherwise.

## agents

Insert a managed one-line pointer to ` + "`auto artifact quickstart`" + ` into the
repo's root CLAUDE.md / AGENTS.md so coding agents discover the tool. The line is
wrapped in HTML-comment markers and upserted in place (idempotent — insert if
absent, replace if drifted, no-op if identical). Operates only on existing files;
errors if neither is present.

## quickstart

Print the happy-path end-to-end guide.

## Security model

Public-read bucket with ListBucket denied; unguessable UUIDv4 key prefixes;
permanent public URLs (no signed/expiring URLs); HTTPS-only; 1 GiB size cap.
`

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Print the full auto artifact command reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), docsMarkdown)
			return nil
		},
	}
}
