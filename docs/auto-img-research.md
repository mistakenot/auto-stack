---
hash: "f97f5d7c"
id: "50ed0334"
read_when: "when designing image storage or progressive disclosure patterns"
summary: "Research on optimising image storage and retrieval for AI coding agents, covering token costs, progressive disclosure, and S3 patterns"
title: "auto-img Research: Context-Protective Image Access for Coding Agents"
---

# auto-img Research: Context-Protective Image Access for Coding Agents

## Token Cost Model

Resolution is the only lever for token cost. Format (JPEG/PNG/WebP) and compression do not affect token cost — only pixel dimensions matter. Claude tiles images at 384x384 pixels, each tile costing ~170 tokens plus ~85 base overhead.

Formula: `ceil(width/384) * ceil(height/384) * 170 + 85`

| Image Size | Tiles | Approx Tokens |
|---|---|---|
| 384x384 | 1 | ~255 |
| 768x768 | 4 | ~765 |
| 1024x1024 | 9 | ~1,615 |
| 1536x1536 | 16 | ~2,805 |
| 1920x1080 | 15 | ~2,635 |
| 4000x3000 | ~88 | ~15,045 |

- Max image size: 8000x8000 (larger images downscaled automatically)
- Images > ~5MB rejected or downscaled by API
- Supported formats: JPEG, PNG, GIF, WebP, BMP, TIFF, SVG
- Halving both dimensions cuts token cost to roughly 1/4

## Optimal Resolutions by Content Type

| Content Type | Recommended Resolution | Token Cost | Notes |
|---|---|---|---|
| Architecture diagrams | 768x768 | ~765 | Text labels remain readable |
| Screenshots (full page) | 1024x768 | ~1,275 | Good balance for UI review |
| Error dialogs/modals | 512x512 | ~425 | Focused area, details preserved |
| Thumbnails/previews | 384x384 | ~255 | Minimum useful size for browsing |
| Code screenshots | Avoid — use text | 0 | Text extraction is unreliable from images |

## Three-Tier Resolution Strategy

```
Original (1920x1080, ~2,635 tokens)
  → Thumbnail (384x384, ~255 tokens)     # 10x cheaper, for browsing
  → Medium (768x768, ~765 tokens)         # 3.5x cheaper, usually sufficient
  → Full (1920x1080, ~2,635 tokens)       # only when agent explicitly needs detail
```

- **384x384** is the minimum useful unit (one tile). Natural thumbnail size for Claude.
- **768x768** is the practical sweet spot. Most diagrams and screenshots remain interpretable. ~3x cheaper than full HD.
- **Beyond 1536x1536** — diminishing returns. Agents rarely need pixel-perfect detail.

## Format Recommendations

Format does not affect token cost but does affect storage/transfer:

| Factor | JPEG | PNG | WebP |
|---|---|---|---|
| Token cost (same dims) | Same | Same | Same |
| File size | Smallest (lossy) | Largest | Smaller than JPEG |
| Best for | Screenshots, photos | Diagrams with sharp text | Best default |

Recommendation: WebP as default, PNG for text-heavy diagrams where compression artifacts could hurt readability.

## Text Extraction Is the Biggest Win

- An OCR/description field in metadata often gives the agent 80% of the information at near-zero token cost
- Many images (error messages, terminal output, labeled diagrams) don't even need to be viewed
- Extracted text should always be included in the index/metadata
- Tools: Tesseract OCR (CLI), or use a vision model for description generation

## Progressive Disclosure Pattern

### Tier 1: Metadata (always loaded, near-zero tokens)

```json
{
  "id": "img_abc123",
  "description": "Architecture diagram showing ETL pipeline with 5 components",
  "content_type": "diagram",
  "text_extracted": "Components: Ingester, Transformer, Validator, Loader, Monitor",
  "original_dimensions": "1920x1080",
  "thumbnail_tokens": 255,
  "full_tokens": 2635,
  "file_size_bytes": 245000,
  "tags": ["architecture", "etl"],
  "project": "auto-stack",
  "created": "2026-04-01T10:00:00Z"
}
```

### Tier 2: Thumbnail (on request, ~255 tokens)

- 384x384 WebP
- Agent views when metadata suggests relevance but needs visual confirmation

### Tier 3: Full resolution (on explicit request, ~2,635 tokens)

- Only fetched when agent needs pixel-level detail (e.g. reading small text, UI bugs)

### Required Metadata Fields

- `description` — one-line summary of what the image shows
- `content_type` — enum: screenshot, diagram, error, chart, photo, ui_mockup
- `text_extracted` — OCR output or known text labels
- `original_dimensions` — for token cost estimation
- `thumbnail_tokens` / `full_tokens` — pre-computed token costs
- `file_size_bytes` — for transfer budget awareness
- `tags` — searchable keywords
- `project` — namespace

## S3 Storage Pattern

### Bucket Structure

```
s3://auto-images-{account-id}/
  {project}/originals/     # full resolution, infrequent access
  {project}/thumbnails/    # 384x384, frequent access
  {project}/medium/        # 768x768, standard access
  {project}/metadata/      # JSON sidecars
```

### Lifecycle Rules

- `originals/` → STANDARD_IA at 30 days, GLACIER_IR at 90 days
- `thumbnails/` → keep in Standard (small, frequently accessed)
- `medium/` → Standard
- `temp/` → expire after 7 days

### Access Pattern

- Always use pre-signed URLs with short expiry
- Thumbnails: 15-minute expiry (quick preview, regenerate if needed)
- Full resolution: 5-minute expiry (only when agent explicitly requests)
- Upload: 10-minute expiry, enforce content-type and max size via conditions
- For production: CloudFront signed URLs (lower latency, caching)

### CloudFormation Template (Minimal)

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Resources:
  ImageBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub 'auto-images-${AWS::AccountId}'
      PublicAccessBlockConfiguration:
        BlockPublicAcls: true
        BlockPublicPolicy: true
        IgnorePublicAcls: true
        RestrictPublicBuckets: true
      VersioningConfiguration:
        Status: Enabled
      LifecycleConfiguration:
        Rules:
          - Id: originals-lifecycle
            Prefix: originals/
            Status: Enabled
            Transitions:
              - TransitionInDays: 30
                StorageClass: STANDARD_IA
              - TransitionInDays: 90
                StorageClass: GLACIER_INSTANT_RETRIEVAL
          - Id: temp-cleanup
            Prefix: temp/
            Status: Enabled
            ExpirationInDays: 7

  ImageUser:
    Type: AWS::IAM::User
    Properties:
      Policies:
        - PolicyName: ImageBucketAccess
          PolicyDocument:
            Statement:
              - Effect: Allow
                Action:
                  - s3:GetObject
                  - s3:PutObject
                  - s3:ListBucket
                  - s3:DeleteObject
                Resource:
                  - !GetAtt ImageBucket.Arn
                  - !Sub '${ImageBucket.Arn}/*'
```

## Ecosystem Gap

No existing coding agent tool implements progressive image disclosure. This is a genuine gap. The metadata-first pattern (description + extracted text + tags → thumbnail → full) can reduce image token consumption by 5-10x in typical workflows.

## CLI Design Sketch

```bash
autoimg init                           # deploy CloudFormation, configure credentials
autoimg upload <file> [--tag X]        # upload, generate thumbnails, extract text, index
autoimg list [--project X] [--tag Y]   # metadata-only listing (no images loaded)
autoimg show <id>                      # show thumbnail (default)
autoimg show <id> --full               # fetch full resolution
autoimg search "architecture diagram"  # search descriptions and extracted text
autoimg gc                             # clean up orphaned/expired images
```
