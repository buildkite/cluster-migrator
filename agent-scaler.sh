#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=util/common.sh
source "$SCRIPT_DIR/util/common.sh"

# shellcheck source=util/agent-scaler/functions.sh
source "$SCRIPT_DIR/util/agent-scaler/functions.sh"

COMMAND=""
POOL=""

case "${1:-}" in
  ""|--help|-h)
    top_level_help
    exit 0
    ;;
  cluster|unclustered)
    COMMAND=scale
    POOL=$1
    shift
    ;;
  status)
    COMMAND=status
    shift
    ;;
  *)
    echo "agent-scaler: unknown command: $1" >&2
    echo >&2
    top_level_help >&2
    exit 1
    ;;
esac

QUEUE=""
COUNT=""
COUNT_SET=false
PERCENTAGE=""
PERCENTAGE_SET=false
WAIT=false
UNCLUSTERED_TOKEN=""
CLUSTER_TOKEN=""

while (($#)); do
  case "$1" in
    --queue=*)
      QUEUE=${1#*=}
      ;;
    --queue)
      [[ $# -ge 2 ]] || die "--queue requires a value"
      QUEUE=$2
      shift
      ;;
    --count=*)
      COUNT=${1#*=}
      COUNT_SET=true
      ;;
    --count)
      [[ $# -ge 2 ]] || die "--count requires a value"
      COUNT=$2
      COUNT_SET=true
      shift
      ;;
    --percentage=*)
      PERCENTAGE=${1#*=}
      PERCENTAGE_SET=true
      ;;
    --percentage)
      [[ $# -ge 2 ]] || die "--percentage requires a value"
      PERCENTAGE=$2
      PERCENTAGE_SET=true
      shift
      ;;
    --wait)
      WAIT=true
      ;;
    --unclustered-token=*)
      UNCLUSTERED_TOKEN=${1#*=}
      ;;
    --unclustered-token)
      [[ $# -ge 2 ]] || die "--unclustered-token requires a value"
      UNCLUSTERED_TOKEN=$2
      shift
      ;;
    --cluster-token=*)
      CLUSTER_TOKEN=${1#*=}
      ;;
    --cluster-token)
      [[ $# -ge 2 ]] || die "--cluster-token requires a value"
      CLUSTER_TOKEN=$2
      shift
      ;;
    --help|-h)
      if [[ "$COMMAND" == "status" ]]; then
        status_help
      else
        pool_help "$POOL"
      fi
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
  shift
done

if [[ "$COMMAND" == "status" ]]; then
  [[ "$COUNT_SET" == false && "$PERCENTAGE_SET" == false && "$WAIT" == false ]] ||
    die "status accepts only --queue and token options"
else
  [[ -n "$QUEUE" ]] || die_with_pool_help "--queue is required"
  [[ "$COUNT_SET" != "$PERCENTAGE_SET" ]] ||
    die_with_pool_help "exactly one of --count or --percentage is required"

  if [[ "$COUNT_SET" == true ]]; then
    [[ "$COUNT" =~ ^[0-9]+$ ]] || die "--count must be a non-negative integer"
    COUNT=$((10#$COUNT))
  else
    [[ "$PERCENTAGE" =~ ^[0-9]+$ ]] || die "--percentage must be an integer from 0 to 100"
    PERCENTAGE=$((10#$PERCENTAGE))
    ((PERCENTAGE <= 100)) || die "--percentage must be an integer from 0 to 100"
  fi
fi

UNCLUSTERED_TOKEN=${UNCLUSTERED_TOKEN:-${BUILDKITE_UNCLUSTERED_AGENT_TOKEN:-}}
CLUSTER_TOKEN=${CLUSTER_TOKEN:-${BUILDKITE_CLUSTER_AGENT_TOKEN:-}}

if [[ "$COMMAND" == "status" ]]; then
  [[ -n "$UNCLUSTERED_TOKEN" ]] ||
    die "set BUILDKITE_UNCLUSTERED_AGENT_TOKEN or pass --unclustered-token"
  [[ -n "$CLUSTER_TOKEN" ]] ||
    die "set BUILDKITE_CLUSTER_AGENT_TOKEN or pass --cluster-token"
elif [[ "$POOL" == "cluster" ]]; then
  AGENT_TOKEN=$CLUSTER_TOKEN
  POOL_LABEL="clustered"
  [[ -n "$AGENT_TOKEN" ]] || die "set BUILDKITE_CLUSTER_AGENT_TOKEN or pass --cluster-token"
else
  AGENT_TOKEN=$UNCLUSTERED_TOKEN
  POOL_LABEL="unclustered"
  [[ -n "$AGENT_TOKEN" ]] || die "set BUILDKITE_UNCLUSTERED_AGENT_TOKEN or pass --unclustered-token"
fi

UNCLUSTERED_AUTH_HEADER_FILE=""
CLUSTER_AUTH_HEADER_FILE=""
trap 'rm -f "$UNCLUSTERED_AUTH_HEADER_FILE" "$CLUSTER_AUTH_HEADER_FILE"' EXIT

if [[ -n "$UNCLUSTERED_TOKEN" ]]; then
  UNCLUSTERED_AUTH_HEADER_FILE=$(create_curl_auth_header_file Token "$UNCLUSTERED_TOKEN") ||
    die "could not create a protected unclustered-agent authentication file"
fi
if [[ -n "$CLUSTER_TOKEN" ]]; then
  CLUSTER_AUTH_HEADER_FILE=$(create_curl_auth_header_file Token "$CLUSTER_TOKEN") ||
    die "could not create a protected clustered-agent authentication file"
fi

if [[ "$POOL" == "cluster" ]]; then
  AGENT_AUTH_HEADER_FILE=$CLUSTER_AUTH_HEADER_FILE
else
  AGENT_AUTH_HEADER_FILE=$UNCLUSTERED_AUTH_HEADER_FILE
fi

for command in curl jq ps; do
  command -v "$command" >/dev/null || die "$command is required"
done
if [[ "$COMMAND" == "status" ]]; then
  for command in column sort; do
    command -v "$command" >/dev/null || die "$command is required"
  done
else
  command -v buildkite-agent >/dev/null || die "buildkite-agent is required"
fi

AGENT_ENDPOINT=${BUILDKITE_AGENT_ENDPOINT:-https://agent.buildkite.com/v3}
STATE_HOME=${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}
STATE_ROOT="$STATE_HOME/agent-scaler"
shopt -s nullglob

if [[ "$COMMAND" == "status" ]]; then
  show_status
  exit 0
fi

QUEUE_KEY=$(jq -nr --arg queue "$QUEUE" '$queue | @uri')
QUEUE_DIR="$STATE_ROOT/queues/$QUEUE_KEY"
BASELINE_FILE="$QUEUE_DIR/unclustered-baseline"

if [[ "$POOL" == "cluster" ]]; then
  POOL_DIR=$QUEUE_DIR
else
  POOL_DIR="$QUEUE_DIR/unclustered"
fi

RUNNING_DIR="$POOL_DIR/running"
STOPPING_DIR="$POOL_DIR/stopping"
BUILDS_DIR="$POOL_DIR/builds"
LOGS_DIR="$POOL_DIR/logs"

mkdir -p "$RUNNING_DIR" "$STOPPING_DIR" "$BUILDS_DIR" "$LOGS_DIR"
printf '%s\n' "$QUEUE" > "$QUEUE_DIR/queue-name"

remove_dead_pid_files
wait_for_stopping_agents

BASELINE=""
if [[ "$COUNT_SET" == true ]]; then
  TARGET=$COUNT
  if [[ "$POOL" == "unclustered" ]]; then
    write_unclustered_baseline "$COUNT"
  fi
else
  BASELINE=$(read_unclustered_baseline)
  TARGET=$(((BASELINE * PERCENTAGE + 99) / 100))
fi

RUNNING_PID_FILES=("$RUNNING_DIR"/*.pid)
CURRENT=${#RUNNING_PID_FILES[@]}

if [[ -n "$BASELINE" ]]; then
  echo "Scaling $POOL_LABEL agents on queue '$QUEUE' from $CURRENT to $TARGET (baseline: $BASELINE)."
else
  echo "Scaling $POOL_LABEL agents on queue '$QUEUE' from $CURRENT to $TARGET."
fi

if ((CURRENT < TARGET)); then
  start_agents $((TARGET - CURRENT))
elif ((CURRENT > TARGET)); then
  stop_agents $((CURRENT - TARGET))
fi

if [[ "$WAIT" == true ]]; then
  LAST_OBSERVED=""
  UNCHANGED_CHECKS=0
  MAX_UNCHANGED_CHECKS=10

  while true; do
    OBSERVED=$(pool_total) || die "metrics did not include a valid $POOL_LABEL total for queue '$QUEUE'"
    [[ "$OBSERVED" == "$TARGET" ]] && break

    if [[ "$OBSERVED" == "$LAST_OBSERVED" ]]; then
      UNCHANGED_CHECKS=$((UNCHANGED_CHECKS + 1))
    else
      LAST_OBSERVED=$OBSERVED
      UNCHANGED_CHECKS=0
    fi

    if ((UNCHANGED_CHECKS >= MAX_UNCHANGED_CHECKS)); then
      echo "Buildkite's observed $POOL_LABEL agent count has remained at $OBSERVED for $MAX_UNCHANGED_CHECKS checks."
      echo "Restoring $POOL_LABEL agents on queue '$QUEUE' to $CURRENT."

      remove_dead_pid_files
      wait_for_stopping_agents
      RUNNING_PID_FILES=("$RUNNING_DIR"/*.pid)
      ROLLBACK_CURRENT=${#RUNNING_PID_FILES[@]}
      if ((ROLLBACK_CURRENT < CURRENT)); then
        start_agents $((CURRENT - ROLLBACK_CURRENT))
      elif ((ROLLBACK_CURRENT > CURRENT)); then
        stop_agents $((ROLLBACK_CURRENT - CURRENT))
      fi

      die "Buildkite did not observe the target of $TARGET $POOL_LABEL agents on queue '$QUEUE'"
    fi

    echo "Waiting for Buildkite to observe $TARGET $POOL_LABEL agents on queue '$QUEUE' (currently $OBSERVED)."
    sleep 2
  done
fi

echo "Queue '$QUEUE' is scaled to $TARGET $POOL_LABEL agents."
