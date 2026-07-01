#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$script_dir/.." && pwd)"
repo_root="$(cd "$project_dir/.." && pwd)"

alloy_version="${ALLOY_VERSION:-6.2.0}"
cache_dir="${ALLOY_CACHE_DIR:-$script_dir/.cache}"
output_dir="${ALLOY_OUTPUT_DIR:-$script_dir/output}"
model_path="${1:-$script_dir/session_message_conformance.als}"
jar_name="org.alloytools.alloy.dist-$alloy_version.jar"
jar_path="$cache_dir/$jar_name"
spike_jar="$repo_root/.tmp/spike-001-alloy-core-model/$jar_name"
maven_url="https://repo1.maven.org/maven2/org/alloytools/org.alloytools.alloy.dist/$alloy_version/$jar_name"

mkdir -p "$cache_dir" "$output_dir"

if [[ ! -f "$jar_path" ]]; then
  if [[ -f "$spike_jar" ]]; then
    cp "$spike_jar" "$jar_path"
  else
    tmp_jar="$jar_path.tmp"
    curl -fsSL "$maven_url" -o "$tmp_jar"
    mv "$tmp_jar" "$jar_path"
  fi
fi

rm -rf "$output_dir"
mkdir -p "$output_dir"

java -jar "$jar_path" \
  exec \
  -f \
  -t json \
  -o "$output_dir" \
  "$model_path" | tee "$output_dir/run.log"

echo
echo "Alloy output: $output_dir"
