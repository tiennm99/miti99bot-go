#!/bin/sh
# Re-attaches the 10 FullAccess managed policies to github-deploy-miti99bot.
# Idempotent: attach-role-policy succeeds even if policy already attached.
# Use as emergency rollback during Phase 4 cutover (plans/260518-1019-iam-least-privilege).
#
# Usage:
#   bash aws/iam-rollback-fullaccess.sh
#   AWS_PROFILE=admin bash aws/iam-rollback-fullaccess.sh
ROLE=github-deploy-miti99bot
POLICIES="
  arn:aws:iam::aws:policy/AWSCloudFormationFullAccess
  arn:aws:iam::aws:policy/AWSLambda_FullAccess
  arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess
  arn:aws:iam::aws:policy/AmazonEventBridgeFullAccess
  arn:aws:iam::aws:policy/AmazonSQSFullAccess
  arn:aws:iam::aws:policy/AmazonSSMFullAccess
  arn:aws:iam::aws:policy/CloudWatchLogsFullAccess
  arn:aws:iam::aws:policy/AWSBudgetsActionsWithAWSResourceControlAccess
  arn:aws:iam::aws:policy/IAMFullAccess
  arn:aws:iam::aws:policy/AmazonS3FullAccess
"

PROFILE_FLAG=""
if [ -n "$AWS_PROFILE" ]; then
  PROFILE_FLAG="--profile $AWS_PROFILE"
fi

for arn in $POLICIES; do
  for try in 1 2 3 4 5; do
    if aws iam attach-role-policy --role-name "$ROLE" --policy-arn "$arn" $PROFILE_FLAG 2>&1; then
      break
    fi
    echo "retry $try for $arn after throttle..."
    sleep $((try * 2))
  done
done

# Verify final state -- exit non-zero if anything is missing.
ATTACHED=$(aws iam list-attached-role-policies --role-name "$ROLE" $PROFILE_FLAG \
  --query 'AttachedPolicies[].PolicyArn' --output text)
MISSING=0
for arn in $POLICIES; do
  echo "$ATTACHED" | grep -q "$arn" || { echo "MISSING: $arn"; MISSING=1; }
done

if [ "$MISSING" = 0 ]; then
  echo "Rollback complete -- all 10 FullAccess policies attached."
else
  echo "Rollback INCOMPLETE -- see MISSING lines above. Re-run or attach via console."
  exit 1
fi
