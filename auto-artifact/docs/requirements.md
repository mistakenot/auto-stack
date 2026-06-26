Auto Artifact will be a CLI command group that makes it easy for AI coding agents to save files like photos videos to a remote location where the URLs can then be embedded into PR pull request comments and that kind of thing. The goal here is that often when executing work an agent makes you know takes a screenshot or makes a video and that acts as evidence that it's successfully completed the tasks that was given to it. Those artifacts then need to be linked from pull re-requests. and other kind of locations where the reviewing engineer can then check to see if everything's correct. So we're gonna add a CLI command group simply for uploading files. When you upload a file the system saves it and returns you a URL with a signed with a like a signed URL to access the file

In terms of architecture, I'm thinking of starting off basic. We will set up a S3 compatible file bucket somewhere. This bucket. will I'll then create a pair of keys like I am keys that can be used to upload to that bucket those keys will be stored in the global configuration for auto artifact the only thing those keys will allow is you know uploading a file and creating a signed URL to view said file. The S3 bucket by default will have a 90-day retention period on objects after which they're auto-deleted but this can be overridden by the CLI command to upload to the bucket. The CLI group should also show a you know have an option where I think also maybe like it has an option where it outputs the AWS command it wants to run to create the bucket in the first place to make it easier to setup and I think that's it I think when we upload buckets the the object should maintain the name of the object as it was on disk but the but it should be put in a folder which is maybe like something like a GUID combined with like a timestamp. I don't think it really matters too much. and then that command should just return the URL like the the signed url to fetch the object from I think the signed URL I don't know does it need to be signed because you you kind of want that signature to last a while like enough time to review the file which could be a couple days you know up to a week so if we can use a signed URL that lasts up a week that would be great to get the object then we don't have to make the whole bucket public but if that's not the case we can always just make the bucket public and just use the GUIDs on the names and just make sure that people can't list objects from the buckets maybe I'm not sure

---

## Consolidated Requirements (v1)

### Purpose

CLI command group (`auto artifact`) for AI coding agents to upload evidence files (screenshots, videos, logs) to S3 and get back permanent public URLs for embedding in PR comments and reviews.

### Architecture

- **Provider**: AWS S3 (initial target)
- **Access model**: Public-read bucket, `ListBucket` denied. Objects are accessible by direct URL but not enumerable. UUIDv4 path prefixes provide unguessability (122 bits of entropy).
- **No signed URLs** — permanent public URLs that stay valid until the object's lifecycle rule deletes it. Avoids expiry problems on long-lived PRs.
- **HTTPS only** — all generated artifact URLs must use `https://`. The upload command must never emit an `http://` URL.

### Object key structure

```
{retention_prefix}/{uuidv4}/{original_filename}
```

Example: `90d/a1b2c3d4-e5f6-7890-abcd-ef1234567890/screenshot.png`

### Retention

Prefix-scoped S3 lifecycle rules with fixed tiers:

| Prefix | Expiration | CLI flag      |
|--------|------------|---------------|
| `7d/`  | 7 days     | `--retain 7d` |
| `30d/` | 30 days    | `--retain 30d`|
| `90d/` | 90 days    | (default)     |
| `365d/`| 365 days   | `--retain 365d`|

Every upload must land in one of these prefixes — no uploads without a lifecycle rule. Default is `90d`.

### Commands

| Command | Description |
|---------|-------------|
| `auto artifact upload <file>` | Upload a single file, return its public URL. Auto-detects Content-Type from extension so images/videos render inline in browsers. |
| `auto artifact delete <url-or-key>` | Delete an object before its retention expires. |
| `auto artifact setup` | Takes `--region`, `--bucket`, `--profile` flags. Outputs a bash script that creates the S3 bucket, bucket policy (public GetObject, deny ListBucket), lifecycle rules, IAM role, IAM user, and access keys. User runs the script in their own authenticated context. |
| `auto artifact init` | Store credentials and bucket config in `~/.auto/artifact/settings.json`. |
| `auto artifact doctor` | Validate config, credentials, and bucket access. |

**Not in v1**: `list` (breaks security model — no bucket enumeration), `get` (public URL is already directly accessible), batch upload.

### IAM permissions

The IAM user created by the setup script gets:
- `s3:PutObject` — upload files
- `s3:DeleteObject` — early deletion
- No `s3:ListBucket`, `s3:GetObject` (object reads are via public bucket policy)

### Output format

- **Default (JSON)**: `{"url": "...", "bucket": "...", "key": "...", "retention": "90d", "content_type": "image/png", "size_bytes": 12345}`
- **`--format text`**: bare URL only, for piping into `gh pr comment` etc.

### Content-Type

Auto-detect from file extension using Go's `mime` package. Set on the S3 object so browsers render images/videos inline.

### Local upload log

Each upload appends a record to `~/.auto/artifact/uploads.jsonl` with: key, URL, original path, timestamp, retention tier, size, content type. Provides local history without querying the bucket.

### Config

`~/.auto/artifact/settings.json`:
```json
{
  "endpoint": "https://s3.us-east-1.amazonaws.com",
  "bucket": "my-artifact-bucket",
  "region": "us-east-1",
  "access_key_id": "AKIA...",
  "secret_access_key": "...",
  "default_retention": "90d"
}
```

### Constraints

- Max file size: 1 GB
- Any file type allowed (no MIME restriction)
- Single file per invocation in v1

---

## Acceptance Criteria

Each AC lists the conformance command that must pass for it to be considered complete. All commands assume a working S3 bucket has been provisioned via `auto artifact setup` and credentials stored via `auto artifact init`. Test files are created inline where needed.

### AC-1: Upload returns a valid public URL

A file uploaded via `auto artifact upload` returns JSON containing a `url` field, and that URL is publicly accessible via HTTP GET.

```bash
# Conformance
echo "hello" > /tmp/ac1-test.txt \
  && URL=$(auto artifact upload /tmp/ac1-test.txt | jq -r '.url') \
  && [ -n "$URL" ] \
  && curl -sf -o /dev/null "$URL" \
  && echo "AC-1 PASS" || echo "AC-1 FAIL"
```

### AC-2: Object key follows `{retention}/{uuid}/{filename}` structure

The returned `key` field matches the pattern `{retention_prefix}/{uuidv4}/{original_filename}`.

```bash
# Conformance
KEY=$(echo "test" | auto artifact upload /dev/stdin --retain 30d | jq -r '.key') \
  && echo "$KEY" | grep -qE '^30d/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/' \
  && echo "AC-2 PASS" || echo "AC-2 FAIL"
```

### AC-3: Default retention is 90d

When `--retain` is not specified, the object key starts with `90d/`.

```bash
# Conformance
echo "test" > /tmp/ac3-test.txt \
  && KEY=$(auto artifact upload /tmp/ac3-test.txt | jq -r '.key') \
  && echo "$KEY" | grep -qE '^90d/' \
  && echo "AC-3 PASS" || echo "AC-3 FAIL"
```

### AC-4: All four retention tiers are accepted

`--retain` accepts exactly `7d`, `30d`, `90d`, and `365d`. Any other value is rejected.

```bash
# Conformance
echo "test" > /tmp/ac4-test.txt \
  && auto artifact upload /tmp/ac4-test.txt --retain 7d   | jq -r '.key' | grep -q '^7d/'   \
  && auto artifact upload /tmp/ac4-test.txt --retain 30d  | jq -r '.key' | grep -q '^30d/'  \
  && auto artifact upload /tmp/ac4-test.txt --retain 90d  | jq -r '.key' | grep -q '^90d/'  \
  && auto artifact upload /tmp/ac4-test.txt --retain 365d | jq -r '.key' | grep -q '^365d/' \
  && ! auto artifact upload /tmp/ac4-test.txt --retain 60d 2>/dev/null \
  && echo "AC-4 PASS" || echo "AC-4 FAIL"
```

### AC-5: Content-Type is auto-detected and set on the S3 object

Uploading a `.png` file sets `Content-Type: image/png` on the S3 object so it renders inline in browsers.

```bash
# Conformance
printf '\x89PNG\r\n\x1a\n' > /tmp/ac5-test.png \
  && URL=$(auto artifact upload /tmp/ac5-test.png | jq -r '.url') \
  && CT=$(curl -sI "$URL" | grep -i '^content-type:' | tr -d '\r' | awk '{print $2}') \
  && [ "$CT" = "image/png" ] \
  && echo "AC-5 PASS" || echo "AC-5 FAIL"
```

### AC-6: JSON output contains all required fields

Upload output includes: `url`, `bucket`, `key`, `retention`, `content_type`, `size_bytes`.

```bash
# Conformance
echo "test" > /tmp/ac6-test.txt \
  && OUT=$(auto artifact upload /tmp/ac6-test.txt) \
  && echo "$OUT" | jq -e '.url, .bucket, .key, .retention, .content_type, .size_bytes' > /dev/null \
  && echo "AC-6 PASS" || echo "AC-6 FAIL"
```

### AC-7: `--format text` outputs bare URL only

When `--format text` is passed, stdout contains only the URL with no JSON wrapping.

```bash
# Conformance
echo "test" > /tmp/ac7-test.txt \
  && OUT=$(auto artifact upload /tmp/ac7-test.txt --format text) \
  && echo "$OUT" | grep -qE '^https://' \
  && ! echo "$OUT" | grep -q '{' \
  && echo "AC-7 PASS" || echo "AC-7 FAIL"
```

### AC-8: Upload appends to local JSONL log

Each upload appends a record to `~/.auto/artifact/uploads.jsonl` containing the key, URL, original path, timestamp, retention, size, and content type.

```bash
# Conformance
BEFORE=$(wc -l < ~/.auto/artifact/uploads.jsonl 2>/dev/null || echo 0) \
  && echo "test" > /tmp/ac8-test.txt \
  && auto artifact upload /tmp/ac8-test.txt > /dev/null \
  && AFTER=$(wc -l < ~/.auto/artifact/uploads.jsonl) \
  && [ "$AFTER" -gt "$BEFORE" ] \
  && tail -1 ~/.auto/artifact/uploads.jsonl | jq -e '.key, .url, .original_path, .timestamp, .retention, .size_bytes, .content_type' > /dev/null \
  && echo "AC-8 PASS" || echo "AC-8 FAIL"
```

### AC-9: Delete removes the object

`auto artifact delete` removes the object from S3; a subsequent HTTP GET returns 403 or 404.

```bash
# Conformance
echo "delete-me" > /tmp/ac9-test.txt \
  && RESULT=$(auto artifact upload /tmp/ac9-test.txt) \
  && URL=$(echo "$RESULT" | jq -r '.url') \
  && KEY=$(echo "$RESULT" | jq -r '.key') \
  && curl -sf -o /dev/null "$URL" \
  && auto artifact delete "$KEY" \
  && HTTP=$(curl -s -o /dev/null -w '%{http_code}' "$URL") \
  && [ "$HTTP" = "403" ] || [ "$HTTP" = "404" ] \
  && echo "AC-9 PASS" || echo "AC-9 FAIL"
```

### AC-10: Setup outputs a valid bash script

`auto artifact setup` outputs a self-contained bash script that, when inspected, contains the required AWS CLI commands for bucket creation, bucket policy, lifecycle rules, IAM role, IAM user, and access key creation.

```bash
# Conformance
SCRIPT=$(auto artifact setup --region us-east-1 --bucket test-bucket --profile default) \
  && echo "$SCRIPT" | grep -q 'create-bucket' \
  && echo "$SCRIPT" | grep -q 'put-bucket-policy' \
  && echo "$SCRIPT" | grep -q 'put-bucket-lifecycle-configuration' \
  && echo "$SCRIPT" | grep -q 'create-role' \
  && echo "$SCRIPT" | grep -q 'create-user' \
  && echo "$SCRIPT" | grep -q 'create-access-key' \
  && bash -n <(echo "$SCRIPT") \
  && echo "AC-10 PASS" || echo "AC-10 FAIL"
```

### AC-11: Init stores config

`auto artifact init` writes credentials and bucket config to `~/.auto/artifact/settings.json` with the required fields.

```bash
# Conformance
auto artifact init \
    --endpoint "https://s3.us-east-1.amazonaws.com" \
    --bucket "test-bucket" \
    --region "us-east-1" \
    --access-key-id "AKIATEST" \
    --secret-access-key "secret" \
  && jq -e '.endpoint, .bucket, .region, .access_key_id, .secret_access_key, .default_retention' \
       ~/.auto/artifact/settings.json > /dev/null \
  && echo "AC-11 PASS" || echo "AC-11 FAIL"
```

### AC-12: Doctor validates configuration

`auto artifact doctor` exits 0 when config and S3 access are valid, exits non-zero with a diagnostic JSON error when something is wrong.

```bash
# Conformance — happy path (requires valid config + bucket)
auto artifact doctor \
  && echo "AC-12a PASS" || echo "AC-12a FAIL"

# Conformance — missing config
mv ~/.auto/artifact/settings.json ~/.auto/artifact/settings.json.bak 2>/dev/null; \
  ! auto artifact doctor 2>&1 | grep -qi 'error\|missing\|not found' \
  ; RESULT=$?; mv ~/.auto/artifact/settings.json.bak ~/.auto/artifact/settings.json 2>/dev/null \
  && [ "$RESULT" -eq 0 ] \
  && echo "AC-12b PASS" || echo "AC-12b FAIL"
```

### AC-13: Files over 1 GB are rejected

Uploading a file larger than 1 GB fails with a clear error message before any S3 call is made.

```bash
# Conformance (uses truncate to create a sparse file — no disk space consumed)
truncate -s 1025M /tmp/ac13-test.bin \
  && ! auto artifact upload /tmp/ac13-test.bin 2>&1 | grep -qi 'size\|too large\|exceeds' \
  ; RESULT=$?; rm -f /tmp/ac13-test.bin \
  && [ "$RESULT" -eq 0 ] \
  && echo "AC-13 PASS" || echo "AC-13 FAIL"
```

### AC-14: Upload fails gracefully without config

Running `upload` before `init` exits non-zero with a message directing the user to run `auto artifact init`.

```bash
# Conformance
mv ~/.auto/artifact/settings.json ~/.auto/artifact/settings.json.bak 2>/dev/null; \
  ERR=$(auto artifact upload /tmp/ac1-test.txt 2>&1); RC=$?; \
  mv ~/.auto/artifact/settings.json.bak ~/.auto/artifact/settings.json 2>/dev/null; \
  [ "$RC" -ne 0 ] && echo "$ERR" | grep -qi 'init' \
  && echo "AC-14 PASS" || echo "AC-14 FAIL"
```

### AC-15: The bucket is not listable

The bucket policy denies `ListBucket`. An unauthenticated `ListObjects` request must fail.

```bash
# Conformance
BUCKET=$(jq -r '.bucket' ~/.auto/artifact/settings.json) \
  && REGION=$(jq -r '.region' ~/.auto/artifact/settings.json) \
  && HTTP=$(curl -s -o /dev/null -w '%{http_code}' "https://${BUCKET}.s3.${REGION}.amazonaws.com/") \
  && [ "$HTTP" = "403" ] \
  && echo "AC-15 PASS" || echo "AC-15 FAIL"
```

### AC-16: Binary builds and registers as `auto artifact` subcommand

The module compiles and `auto artifact --help` shows the subcommand group.

```bash
# Conformance
cd /home/vscode/src/auto-stack && go build ./... \
  && auto artifact --help 2>&1 | grep -qi 'artifact' \
  && echo "AC-16 PASS" || echo "AC-16 FAIL"
```
