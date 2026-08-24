#!/bin/bash
# Graded verifier. Reward is the F1 of /app/answer.json against the neutral
# ground-truth importer set. python3 is baked into the image, so no network.
set -uo pipefail

mkdir -p /logs/verifier
python3 /tests/score.py

# score.py always writes /logs/verifier/reward.txt; fall back to 0 if it didn't.
if [ ! -s /logs/verifier/reward.txt ]; then
  echo 0 > /logs/verifier/reward.txt
fi
