#!/bin/bash

top_level_help() {
  cat <<'EOF'
Usage:
  agent-scaler <command> [options...]

Available commands:
  cluster      Scale clustered agent capacity
  unclustered  Scale unclustered agent capacity
  status       Show clustered and unclustered agent capacity

Use "agent-scaler <command> --help" for more information about a command.
EOF
}

pool_help() {
  local pool=$1

  cat <<EOF
Usage:

    agent-scaler $pool [options...]

Description:

Scales the $pool agent pool for one queue by reconciling local Buildkite
agent processes started by this tool.

Exactly one of --count or --percentage is required. Percentage targets are
calculated from the queue's initial unclustered baseline.

Example:

    \$ agent-scaler $pool --queue default --count 10

Options:

  --queue value               Queue to scale (required)
  --count value               Absolute non-negative agent target; mutually exclusive with --percentage
  --percentage value          Percentage of the initial unclustered baseline (0..100); mutually exclusive with --count
  --wait                      Wait for the target; restore after 10 unchanged checks (default: false)
  --unclustered-token value   Unclustered agent token and baseline credentials [\$BUILDKITE_UNCLUSTERED_AGENT_TOKEN]
  --cluster-token value       Cluster agent token [\$BUILDKITE_CLUSTER_AGENT_TOKEN]
  --help, -h                  Show help
EOF
}

status_help() {
  cat <<'EOF'
Usage:

    agent-scaler status [options...]

Description:

Shows Buildkite-observed and locally managed agent capacity by queue for the
clustered and unclustered pools. This does not report migration readiness.

Options:

  --queue value               Show one queue instead of all queues
  --unclustered-token value   Unclustered agent token [$BUILDKITE_UNCLUSTERED_AGENT_TOKEN]
  --cluster-token value       Cluster agent token [$BUILDKITE_CLUSTER_AGENT_TOKEN]
  --help, -h                  Show help
EOF
}

die() {
  echo "agent-scaler: $*" >&2
  exit 1
}

die_with_pool_help() {
  echo "agent-scaler: $*" >&2
  echo >&2
  pool_help "$POOL" >&2
  exit 1
}

metrics() {
  local auth_header_file=$1

  curl \
    --fail \
    --silent \
    --show-error \
    --header "@$auth_header_file" \
    "${AGENT_ENDPOINT%/}/metrics"
}

status_queue_metrics() {
  local payload=$1
  local queue=$2

  printf '%s\n' "$payload" |
    jq -r --arg queue "$queue" '
      def count:
        if type == "number" and . >= 0 and floor == . then tostring else "-" end;

      .agents.queues[$queue] as $metrics
      | if $metrics == null then
          "-/-/-"
        else
          [($metrics.total | count), ($metrics.idle | count), ($metrics.busy | count)]
          | join("/")
        end
    '
}

status_local_counts() {
  local queue=$1
  local pool=$2
  local queue_key queue_dir pool_dir directory pid_file pid agent_id agent_name command_line
  local live=0
  local stale=0

  queue_key=$(jq -nr --arg queue "$queue" '$queue | @uri')
  queue_dir="$STATE_ROOT/queues/$queue_key"
  if [[ "$pool" == "cluster" ]]; then
    pool_dir=$queue_dir
  else
    pool_dir="$queue_dir/unclustered"
  fi

  for directory in "$pool_dir/running" "$pool_dir/stopping"; do
    for pid_file in "$directory"/*.pid; do
      if ! read -r pid < "$pid_file" || [[ ! "$pid" =~ ^[0-9]+$ ]]; then
        stale=$((stale + 1))
        continue
      fi

      agent_id=$(basename "$pid_file" .pid)
      if [[ "$pool" == "cluster" ]]; then
        agent_name="cluster-migrator-$agent_id"
      else
        agent_name="cluster-migrator-unclustered-$agent_id"
      fi

      if command_line=$(ps -p "$pid" -o command= 2>/dev/null) &&
          [[ "$command_line" == *"--name $agent_name"* ]]; then
        live=$((live + 1))
      else
        stale=$((stale + 1))
      fi
    done
  done

  printf '%s %s\n' "$live" "$stale"
}

status_baseline() {
  local queue=$1
  local queue_key baseline_file baseline

  queue_key=$(jq -nr --arg queue "$queue" '$queue | @uri')
  baseline_file="$STATE_ROOT/queues/$queue_key/unclustered-baseline"
  if [[ ! -f "$baseline_file" ]]; then
    printf '%s\n' '-'
    return
  fi

  if read -r baseline < "$baseline_file" && [[ "$baseline" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$baseline"
  else
    printf '%s\n' invalid
  fi
}

status_queue_names() {
  local unclustered_metrics=$1
  local clustered_metrics=$2
  local queue_dir

  if [[ -n "$QUEUE" ]]; then
    printf '%s\n' "$QUEUE"
    return
  fi

  {
    printf '%s\n' "$unclustered_metrics" | jq -r '.agents.queues | keys[]'
    printf '%s\n' "$clustered_metrics" | jq -r '.agents.queues | keys[]'
    for queue_dir in "$STATE_ROOT/queues"/*; do
      [[ -f "$queue_dir/queue-name" ]] && cat "$queue_dir/queue-name"
    done
  } | LC_ALL=C sort -u
}

show_status() {
  local unclustered_metrics clustered_metrics queue baseline
  local unclustered_queue_metrics clustered_queue_metrics
  local unclustered_local unclustered_stale clustered_local clustered_stale

  if ! unclustered_metrics=$(metrics "$UNCLUSTERED_AUTH_HEADER_FILE"); then
    die "could not read unclustered agent metrics"
  fi
  if ! clustered_metrics=$(metrics "$CLUSTER_AUTH_HEADER_FILE"); then
    die "could not read clustered agent metrics"
  fi
  printf '%s\n' "$unclustered_metrics" | jq -e '.agents.queues | type == "object"' >/dev/null ||
    die "unclustered metrics did not include agent queues"
  printf '%s\n' "$clustered_metrics" | jq -e '.agents.queues | type == "object"' >/dev/null ||
    die "clustered metrics did not include agent queues"

  {
    printf 'QUEUE\tBASELINE\tUNCLUSTERED (TOTAL/IDLE/BUSY)\tCLUSTERED (TOTAL/IDLE/BUSY)\tLOCAL (U/C)\tSTALE (U/C)\n'
    while IFS= read -r queue; do
      [[ -n "$queue" ]] || continue
      baseline=$(status_baseline "$queue")
      unclustered_queue_metrics=$(status_queue_metrics "$unclustered_metrics" "$queue")
      clustered_queue_metrics=$(status_queue_metrics "$clustered_metrics" "$queue")
      read -r unclustered_local unclustered_stale < <(status_local_counts "$queue" unclustered)
      read -r clustered_local clustered_stale < <(status_local_counts "$queue" cluster)
      printf '%s\t%s\t%s\t%s\t%s/%s\t%s/%s\n' \
        "$queue" \
        "$baseline" \
        "$unclustered_queue_metrics" \
        "$clustered_queue_metrics" \
        "$unclustered_local" \
        "$clustered_local" \
        "$unclustered_stale" \
        "$clustered_stale"
    done < <(status_queue_names "$unclustered_metrics" "$clustered_metrics")
  } | column -t -s $'\t'
}

read_unclustered_baseline() {
  local baseline temporary_file

  if [[ ! -f "$BASELINE_FILE" ]]; then
    [[ -n "$UNCLUSTERED_TOKEN" ]] ||
      die "set BUILDKITE_UNCLUSTERED_AGENT_TOKEN or pass --unclustered-token to establish the percentage baseline"
    temporary_file="$BASELINE_FILE.$$"
    if ! metrics "$UNCLUSTERED_AUTH_HEADER_FILE" |
        jq -er --arg queue "$QUEUE" '
          .agents.queues[$queue].total
          | select(type == "number" and . >= 0 and floor == .)
        ' > "$temporary_file"; then
      rm -f "$temporary_file"
      die "metrics did not include a valid unclustered total for queue '$QUEUE'"
    fi
    mv "$temporary_file" "$BASELINE_FILE"
  fi

  baseline=$(cat "$BASELINE_FILE")
  [[ "$baseline" =~ ^[0-9]+$ ]] || die "invalid baseline in $BASELINE_FILE"
  printf '%s\n' "$baseline"
}

write_unclustered_baseline() {
  local baseline=$1
  local temporary_file

  [[ -f "$BASELINE_FILE" ]] && return

  temporary_file="$BASELINE_FILE.$$"
  printf '%s\n' "$baseline" > "$temporary_file"
  mv "$temporary_file" "$BASELINE_FILE"
}

pool_total() {
  metrics "$AGENT_AUTH_HEADER_FILE" |
    jq -er --arg queue "$QUEUE" '
      (.agents.queues[$queue].total // 0)
      | select(type == "number" and . >= 0 and floor == .)
    '
}

name_for_agent() {
  local agent_id=$1

  if [[ "$POOL" == "cluster" ]]; then
    printf 'cluster-migrator-%s\n' "$agent_id"
  else
    printf 'cluster-migrator-unclustered-%s\n' "$agent_id"
  fi
}

agent_is_owned() {
  local pid_file=$1
  local pid agent_id agent_name command_line

  pid=$(cat "$pid_file")
  agent_id=$(basename "$pid_file" .pid)
  agent_name=$(name_for_agent "$agent_id")
  command_line=$(ps -p "$pid" -o command= 2>/dev/null) || return 1
  [[ "$command_line" == *"--name $agent_name"* ]]
}

remove_dead_pid_files() {
  local directory pid_file pid

  for directory in "$RUNNING_DIR" "$STOPPING_DIR"; do
    for pid_file in "$directory"/*.pid; do
      pid=$(cat "$pid_file")
      [[ "$pid" =~ ^[0-9]+$ ]] || die "invalid PID in $pid_file"

      if ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$pid_file"
      elif ! agent_is_owned "$pid_file"; then
        die "refusing to manage PID $pid because it is not the recorded agent process"
      fi
    done
  done
}

wait_for_stopping_agents() {
  local pid_file pid

  for pid_file in "$STOPPING_DIR"/*.pid; do
    pid=$(cat "$pid_file")
    while kill -0 "$pid" 2>/dev/null; do
      if ! agent_is_owned "$pid_file"; then
        kill -0 "$pid" 2>/dev/null || break
        die "refusing to wait for PID $pid because it is not the recorded agent process"
      fi
      sleep 1
    done
    rm -f "$pid_file"
  done
}

start_agents() {
  local count=$1
  local number agent_id agent_name build_path log_path pid

  for ((number = 1; number <= count; number++)); do
    agent_id="$(date +%s)-$$-$number"
    agent_name=$(name_for_agent "$agent_id")
    build_path="$BUILDS_DIR/$agent_id"
    log_path="$LOGS_DIR/$agent_id.log"
    mkdir -p "$build_path"

    BUILDKITE_AGENT_TOKEN="$AGENT_TOKEN" \
      BUILDKITE_BUILD_PATH="$build_path" \
      buildkite-agent start \
        --name "$agent_name" \
        --tags "queue=$QUEUE,cluster-migrator-owned=true" > "$log_path" 2>&1 &
    pid=$!
    printf '%s\n' "$pid" > "$RUNNING_DIR/$agent_id.pid"
  done
}

stop_agents() {
  local count=$1
  local stopped=0 pid_file pid stopping_file

  for pid_file in "$RUNNING_DIR"/*.pid; do
    ((stopped >= count)) && break

    pid=$(cat "$pid_file")
    stopping_file="$STOPPING_DIR/$(basename "$pid_file")"
    mv "$pid_file" "$stopping_file"

    agent_is_owned "$stopping_file" ||
      die "refusing to stop PID $pid because it is not the recorded agent process"
    if ! kill -TERM "$pid" 2>/dev/null; then
      rm -f "$stopping_file"
    fi
    stopped=$((stopped + 1))
  done

  wait_for_stopping_agents
}
