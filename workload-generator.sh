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
Usage:
  ./workload-generator.sh run [options]
  ./workload-generator.sh trigger --workload=late-queue [options]

Run options:
  --load=<smoke|steady|pressure|sparse>  Workload timing profile (default: smoke)
  --cycles=<count>                      Complete topology cycles (default: 1)
  --continuous                          Run complete cycles until interrupted
  --builds-per-minute=<1..60>           Override the profile's build rate

Common options:
  --prefix=<resource-prefix>            Setup resource prefix (default: cluster-migrator)
  --commit=<git-sha>                    Immutable revision to build (default: local HEAD)
  --run-id=<id>                         Label included in every build

Every run profile traverses the same canonical pipeline topology. Profiles
change only pacing and job duration. The late queue stays dormant until the
focused trigger is invoked.
EOF
}

case "${1:-}" in
  run|trigger)
    COMMAND=$1
    shift
    ;;
  --help|-h)
    usage
    exit 0
    ;;
  "")
    die "command is required (expected: run or trigger)"
    ;;
  *)
    die "unknown command: $1"
    ;;
esac

WORKLOAD_LOAD=smoke
CYCLES=1
CONTINUOUS=false
BUILDS_PER_MINUTE=""
BUILDS_PER_MINUTE_SET=false
TRIGGER_WORKLOAD=""
WORKLOAD_RESOURCE_PREFIX=${MIGRATION_WORKLOAD_PREFIX:-cluster-migrator}
WORKLOAD_COMMIT=""
WORKLOAD_RUN_ID=""
while (($#)); do
  case "$1" in
    --load=*)
      WORKLOAD_LOAD=${1#*=}
      ;;
    --load)
      [[ $# -ge 2 ]] || die "--load requires a value"
      WORKLOAD_LOAD=$2
      shift
      ;;
    --cycles=*)
      CYCLES=${1#*=}
      ;;
    --cycles)
      [[ $# -ge 2 ]] || die "--cycles requires a value"
      CYCLES=$2
      shift
      ;;
    --continuous)
      CONTINUOUS=true
      ;;
    --builds-per-minute=*)
      BUILDS_PER_MINUTE=${1#*=}
      BUILDS_PER_MINUTE_SET=true
      ;;
    --builds-per-minute)
      [[ $# -ge 2 ]] || die "--builds-per-minute requires a value"
      BUILDS_PER_MINUTE=$2
      BUILDS_PER_MINUTE_SET=true
      shift
      ;;
    --workload=*)
      TRIGGER_WORKLOAD=${1#*=}
      ;;
    --workload)
      [[ $# -ge 2 ]] || die "--workload requires a value"
      TRIGGER_WORKLOAD=$2
      shift
      ;;
    --prefix=*)
      WORKLOAD_RESOURCE_PREFIX=${1#*=}
      ;;
    --prefix)
      [[ $# -ge 2 ]] || die "--prefix requires a value"
      WORKLOAD_RESOURCE_PREFIX=$2
      shift
      ;;
    --commit=*)
      WORKLOAD_COMMIT=${1#*=}
      ;;
    --commit)
      [[ $# -ge 2 ]] || die "--commit requires a value"
      WORKLOAD_COMMIT=$2
      shift
      ;;
    --run-id=*)
      WORKLOAD_RUN_ID=${1#*=}
      ;;
    --run-id)
      [[ $# -ge 2 ]] || die "--run-id requires a value"
      WORKLOAD_RUN_ID=$2
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

case "$WORKLOAD_LOAD" in
  smoke)
    DEFAULT_BUILDS_PER_MINUTE=12
    WORKLOAD_QUICK_SECONDS=1
    WORKLOAD_MEDIUM_SECONDS=3
    WORKLOAD_LONG_SECONDS=8
    ;;
  steady)
    DEFAULT_BUILDS_PER_MINUTE=4
    WORKLOAD_QUICK_SECONDS=5
    WORKLOAD_MEDIUM_SECONDS=15
    WORKLOAD_LONG_SECONDS=45
    ;;
  pressure)
    DEFAULT_BUILDS_PER_MINUTE=20
    WORKLOAD_QUICK_SECONDS=10
    WORKLOAD_MEDIUM_SECONDS=30
    WORKLOAD_LONG_SECONDS=90
    ;;
  sparse)
    DEFAULT_BUILDS_PER_MINUTE=1
    WORKLOAD_QUICK_SECONDS=2
    WORKLOAD_MEDIUM_SECONDS=10
    WORKLOAD_LONG_SECONDS=30
    ;;
  *)
    die "--load must be one of smoke, steady, pressure, or sparse"
    ;;
esac

if [[ "$COMMAND" == "run" ]]; then
  [[ -z "$TRIGGER_WORKLOAD" ]] || die "--workload is only valid with trigger"
  if [[ ! "$CYCLES" =~ ^[0-9]+$ ]] || ((10#$CYCLES < 1)); then
    die "--cycles must be a positive integer"
  fi
  CYCLES=$((10#$CYCLES))
else
  [[ "$TRIGGER_WORKLOAD" == "late-queue" ]] ||
    die "trigger requires --workload=late-queue"
  [[ "$CONTINUOUS" == false && "$CYCLES" == 1 && "$BUILDS_PER_MINUTE_SET" == false ]] ||
    die "trigger does not accept run pacing options"
fi

if [[ "$BUILDS_PER_MINUTE_SET" == false ]]; then
  BUILDS_PER_MINUTE=$DEFAULT_BUILDS_PER_MINUTE
fi
if [[ ! "$BUILDS_PER_MINUTE" =~ ^[0-9]+$ ]] ||
  ((10#$BUILDS_PER_MINUTE < 1 || 10#$BUILDS_PER_MINUTE > 60)); then
  die "--builds-per-minute must be an integer from 1 to 60"
fi
BUILDS_PER_MINUTE=$((10#$BUILDS_PER_MINUTE))
BUILD_INTERVAL_SECONDS=$(awk -v rate="$BUILDS_PER_MINUTE" 'BEGIN { print 60 / rate }')
[[ "$WORKLOAD_RESOURCE_PREFIX" =~ ^[a-z0-9][a-z0-9-]*$ ]] ||
  die "--prefix must contain only lowercase letters, numbers, and hyphens"

if [[ -z "$WORKLOAD_COMMIT" ]]; then
  WORKLOAD_COMMIT=$(git -C "$SCRIPT_DIR" rev-parse HEAD) ||
    die "could not resolve the local Git revision; pass --commit"
fi
[[ "$WORKLOAD_COMMIT" =~ ^[0-9a-fA-F]{40}$ || "$WORKLOAD_COMMIT" =~ ^[0-9a-fA-F]{64}$ ]] ||
  die "--commit must be a complete Git object ID"
WORKLOAD_RUN_ID=${WORKLOAD_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}
[[ -n "$WORKLOAD_RUN_ID" ]] || die "--run-id requires a non-empty value"

: "${BUILDKITE_ORGANIZATION_SLUG:?Set BUILDKITE_ORGANIZATION_SLUG in .env or the environment}"
: "${BUILDKITE_API_TOKEN:?Set BUILDKITE_API_TOKEN in .env or the environment}"
BUILDKITE_API_URL=${BUILDKITE_API_URL:-https://api.buildkite.com/v2}
API_AUTH_HEADER_FILE=$(create_curl_auth_header_file Bearer "$BUILDKITE_API_TOKEN") ||
  die "could not create a protected API authentication file"
trap 'rm -f "$API_AUTH_HEADER_FILE"' EXIT

COMMON_CURL_ARGS=(
  --fail
  --silent
  --show-error
  --header "@$API_AUTH_HEADER_FILE"
)
export BUILDKITE_ORGANIZATION_SLUG

# shellcheck source=util/workload-generator/functions.sh
source "$SCRIPT_DIR/util/workload-generator/functions.sh"

PIPELINE_SUFFIXES=()
DEFAULT_BRANCHES=()

for pipeline_record in "${WORKLOAD_PIPELINES[@]}"; do
  IFS='|' read -r pipeline_suffix _ <<< "$pipeline_record"
  pipeline_slug=$(workload_pipeline_slug "$WORKLOAD_RESOURCE_PREFIX" "$pipeline_suffix")

  if ! PIPELINE=$(demo_pipeline "$pipeline_slug"); then
    die "pipeline '$pipeline_slug' does not exist; run ./setup-organization.sh first"
  fi

  if ! default_branch=$(jq -er \
      '.default_branch | select(type == "string" and length > 0)' <<< "$PIPELINE"); then
    die "pipeline '$pipeline_slug' does not have a default branch"
  fi
  PIPELINE_SUFFIXES+=("$pipeline_suffix")
  DEFAULT_BRANCHES+=("$default_branch")
done

SEQUENCE=1
DYNAMIC_SEQUENCE=0

if [[ "$COMMAND" == "trigger" ]]; then
  create_demo_build \
    "$(workload_pipeline_slug "$WORKLOAD_RESOURCE_PREFIX" dynamic-topology)" \
    "$(pipeline_branch dynamic-topology)" \
    "$SEQUENCE" \
    dynamic-rare \
    dynamic-blue
elif [[ "$CONTINUOUS" == true ]]; then
  while true; do
    create_workload_cycle
  done
else
  for ((cycle = 1; cycle <= CYCLES; cycle++)); do
    create_workload_cycle
  done
fi
