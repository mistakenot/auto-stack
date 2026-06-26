#!/usr/bin/env bash
set -euo pipefail

BUCKET="auto-artifact-datadyne"
REGION="eu-west-1"
PROFILE="default"
IAM_USER="auto-artifact-uploader"
IAM_ROLE="auto-artifact-role"
IAM_POLICY="auto-artifact-policy"
ACCOUNT_ID=$(aws sts get-caller-identity --profile "$PROFILE" --query Account --output text)

echo "=== Auto Artifact AWS Setup ==="
echo "Bucket:  $BUCKET"
echo "Region:  $REGION"
echo "Account: $ACCOUNT_ID"
echo ""

# --- 1. Create S3 bucket ---
echo "[1/6] Creating S3 bucket..."
aws s3api create-bucket \
  --bucket "$BUCKET" \
  --region "$REGION" \
  --create-bucket-configuration LocationConstraint="$REGION" \
  --profile "$PROFILE"

# --- 2. Configure Block Public Access (allow public bucket policy, block ACLs) ---
echo "[2/6] Configuring public access settings..."
aws s3api put-public-access-block \
  --bucket "$BUCKET" \
  --public-access-block-configuration \
    'BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=false,RestrictPublicBuckets=false' \
  --profile "$PROFILE"

# --- 3. Set bucket policy: public GetObject, deny ListBucket ---
echo "[3/6] Setting bucket policy (public read, no list)..."
aws s3api put-bucket-policy \
  --bucket "$BUCKET" \
  --policy "$(cat <<POLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "PublicReadGetObject",
      "Effect": "Allow",
      "Principal": "*",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::${BUCKET}/*"
    },
    {
      "Sid": "DenyListBucket",
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::${BUCKET}"
    }
  ]
}
POLICY
)" \
  --profile "$PROFILE"

# --- 4. Set lifecycle rules for retention prefixes ---
echo "[4/6] Setting lifecycle rules (7d, 30d, 90d, 365d)..."
aws s3api put-bucket-lifecycle-configuration \
  --bucket "$BUCKET" \
  --lifecycle-configuration "$(cat <<LIFECYCLE
{
  "Rules": [
    {
      "ID": "expire-7d",
      "Filter": { "Prefix": "7d/" },
      "Status": "Enabled",
      "Expiration": { "Days": 7 }
    },
    {
      "ID": "expire-30d",
      "Filter": { "Prefix": "30d/" },
      "Status": "Enabled",
      "Expiration": { "Days": 30 }
    },
    {
      "ID": "expire-90d",
      "Filter": { "Prefix": "90d/" },
      "Status": "Enabled",
      "Expiration": { "Days": 90 }
    },
    {
      "ID": "expire-365d",
      "Filter": { "Prefix": "365d/" },
      "Status": "Enabled",
      "Expiration": { "Days": 365 }
    }
  ]
}
LIFECYCLE
)" \
  --profile "$PROFILE"

# --- 5. Create IAM policy, role, and user ---
echo "[5/6] Creating IAM policy, role, and user..."

# Create the policy
POLICY_ARN=$(aws iam create-policy \
  --policy-name "$IAM_POLICY" \
  --policy-document "$(cat <<IAMPOLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ArtifactUploadDelete",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:DeleteObject"
      ],
      "Resource": "arn:aws:s3:::${BUCKET}/*"
    }
  ]
}
IAMPOLICY
)" \
  --profile "$PROFILE" \
  --query 'Policy.Arn' --output text)

echo "  Policy ARN: $POLICY_ARN"

# Create the user
aws iam create-user \
  --user-name "$IAM_USER" \
  --profile "$PROFILE"

# Attach the policy to the user
aws iam attach-user-policy \
  --user-name "$IAM_USER" \
  --policy-arn "$POLICY_ARN" \
  --profile "$PROFILE"

# --- 6. Create access keys ---
echo "[6/6] Creating access keys..."
KEYS=$(aws iam create-access-key \
  --user-name "$IAM_USER" \
  --profile "$PROFILE" \
  --query 'AccessKey.[AccessKeyId,SecretAccessKey]' --output text)

ACCESS_KEY_ID=$(echo "$KEYS" | awk '{print $1}')
SECRET_ACCESS_KEY=$(echo "$KEYS" | awk '{print $2}')

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Run this to configure auto artifact:"
echo ""
echo "  auto artifact init \\"
echo "    --endpoint \"https://s3.${REGION}.amazonaws.com\" \\"
echo "    --bucket \"${BUCKET}\" \\"
echo "    --region \"${REGION}\" \\"
echo "    --access-key-id \"${ACCESS_KEY_ID}\" \\"
echo "    --secret-access-key \"${SECRET_ACCESS_KEY}\""
echo ""
echo "Artifact URLs will be:"
echo "  https://${BUCKET}.s3.${REGION}.amazonaws.com/{retention}/{uuid}/{filename}"
echo ""
echo "IMPORTANT: Save the secret access key above — it cannot be retrieved again."
