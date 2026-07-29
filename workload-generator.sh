#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=util/common.sh
source "$SCRIPT_DIR/util/common.sh"

die() {
  echo "workload-generator: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./workload-generator.sh run [--builds-per-minute=<1..60>]

Generates builds across the cluster migration pipelines created by
setup-organization.sh at the total requested rate.
EOF
}

case "${1:-}" in
  run)
    shift
    ;;
  --help|-h)
    usage
    exit 0
    ;;
  "")
    die "command is required (expected: run)"
    ;;
  *)
    die "unknown command: $1"
    ;;
esac

BUILDS_PER_MINUTE=4
while (($#)); do
  case "$1" in
    --builds-per-minute=*)
      BUILDS_PER_MINUTE=${1#*=}
      ;;
    --builds-per-minute)
      [[ $# -ge 2 ]] || die "--builds-per-minute requires a value"
      BUILDS_PER_MINUTE=$2
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
  shift
done

if [[ ! "$BUILDS_PER_MINUTE" =~ ^[0-9]+$ ]] ||
  ((10#$BUILDS_PER_MINUTE < 1 || 10#$BUILDS_PER_MINUTE > 60)); then
  die "--builds-per-minute must be an integer from 1 to 60"
fi
BUILDS_PER_MINUTE=$((10#$BUILDS_PER_MINUTE))
BUILD_INTERVAL_SECONDS=$(awk -v rate="$BUILDS_PER_MINUTE" 'BEGIN { print 60 / rate }')

: "${BUILDKITE_API_TOKEN:?Set BUILDKITE_API_TOKEN in .env or the environment}"
BUILDKITE_API_URL=${BUILDKITE_API_URL:-http://api.buildkite.localhost/v2}
API_AUTH_HEADER_FILE=$(create_curl_auth_header_file Bearer "$BUILDKITE_API_TOKEN") ||
  die "could not create a protected API authentication file"
trap 'rm -f "$API_AUTH_HEADER_FILE"' EXIT

COMMON_CURL_ARGS=(
  --fail
  --silent
  --show-error
  --header "@$API_AUTH_HEADER_FILE"
)
if [[ -z ${BUILDKITE_ORGANIZATION_SLUG:-} ]]; then
  BUILDKITE_ORGANIZATION_SLUG=$(curl "${COMMON_CURL_ARGS[@]}" "$BUILDKITE_API_URL/organizations" | jq -er '.[0].slug')
fi
export BUILDKITE_ORGANIZATION_SLUG

# shellcheck source=util/workload-generator/functions.sh
source "$SCRIPT_DIR/util/workload-generator/functions.sh"

DEFAULT_BRANCHES=()

for index in "${!WORKLOAD_PIPELINE_SLUGS[@]}"; do
  pipeline_slug=${WORKLOAD_PIPELINE_SLUGS[$index]}

  if ! PIPELINE=$(demo_pipeline "$pipeline_slug"); then
    die "pipeline '$pipeline_slug' does not exist; run ./setup-organization.sh first"
  fi

  if ! default_branch=$(jq -er \
      '.default_branch | select(type == "string" and length > 0)' <<< "$PIPELINE"); then
    die "pipeline '$pipeline_slug' does not have a default branch"
  fi
  DEFAULT_BRANCHES+=("$default_branch")
done

SEQUENCE=1

while true; do
  create_workload_cycle
done
