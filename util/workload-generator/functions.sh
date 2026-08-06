#!/bin/bash

WORKLOAD_PIPELINES=(
  "target|Cluster Migrator Target|target-only|util/workload-generator/.buildkite/target/pipeline.yml"
  "shared-consumer|Cluster Migrator Shared Queue Consumer|shared|util/workload-generator/.buildkite/shared-consumer/pipeline.yml"
  "group-peer|Cluster Migrator Concurrency Group Peer|peer-only|util/workload-generator/.buildkite/group-peer/pipeline.yml"
  "isolated-multi|Cluster Migrator Isolated Multi Queue|private-a|util/workload-generator/.buildkite/isolated-multi/pipeline.yml"
  "dynamic-topology|Cluster Migrator Dynamic Topology|dynamic-bootstrap|util/workload-generator/.buildkite/dynamic-topology/pipeline.yml"
  "control|Cluster Migrator Unrelated Control|control-only|util/workload-generator/.buildkite/control/pipeline.yml"
)

WORKLOAD_QUEUE_KEYS=(
  "target-only"
  "shared"
  "peer-only"
  "private-a"
  "private-b"
  "dynamic-bootstrap"
  "dynamic-blue"
  "dynamic-green"
  "dynamic-rare"
  "control-only"
)

workload_pipeline_slug() {
  local prefix=$1
  local suffix=$2

  printf '%s-%s\n' "$prefix" "$suffix"
}

workload_queue_key() {
  local prefix=$1
  local key=$2

  printf '%s-%s\n' "$prefix" "$key"
}

workload_pipeline_name() {
  local prefix=$1
  local name=$2

  if [[ "$prefix" == "cluster-migrator" ]]; then
    printf '%s\n' "$name"
  else
    printf '%s [%s]\n' "$name" "$prefix"
  fi
}

workload_schedule() {
  local load=$1

  case "$load" in
    smoke|steady|pressure|sparse)
      printf '%s\n' \
        target \
        shared-consumer \
        group-peer \
        isolated-multi \
        dynamic-topology \
        control
      ;;
    *)
      return 1
      ;;
  esac
}

dynamic_queue_variant() {
  local sequence=$1

  if ((sequence % 2 == 0)); then
    printf 'dynamic-green\n'
  else
    printf 'dynamic-blue\n'
  fi
}

demo_pipeline() {
  local pipeline_slug=$1

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug"
}

build_payload() {
  local default_branch=$1
  local sequence=$2
  local dynamic_queue=$3
  local delayed_queue=$4

  jq -n \
    --arg branch "$default_branch" \
    --arg commit "$WORKLOAD_COMMIT" \
    --arg message "Cluster migration workload $WORKLOAD_RUN_ID/$sequence ($WORKLOAD_LOAD)" \
    --arg run_id "$WORKLOAD_RUN_ID" \
    --arg prefix "$WORKLOAD_RESOURCE_PREFIX" \
    --arg load "$WORKLOAD_LOAD" \
    --arg sequence "$sequence" \
    --arg dynamic_queue "$dynamic_queue" \
    --arg delayed_queue "$delayed_queue" \
    --arg quick_seconds "$WORKLOAD_QUICK_SECONDS" \
    --arg medium_seconds "$WORKLOAD_MEDIUM_SECONDS" \
    --arg long_seconds "$WORKLOAD_LONG_SECONDS" \
    '{
      commit: $commit,
      branch: $branch,
      message: $message,
      env: {
        MIGRATION_WORKLOAD_RUN_ID: $run_id,
        MIGRATION_WORKLOAD_PREFIX: $prefix,
        MIGRATION_WORKLOAD_LOAD: $load,
        MIGRATION_WORKLOAD_SEQUENCE: $sequence,
        MIGRATION_QUEUE_VARIANT: $dynamic_queue,
        MIGRATION_DYNAMIC_QUEUE: $dynamic_queue,
        MIGRATION_DELAYED_QUEUE: $delayed_queue,
        MIGRATION_QUICK_SECONDS: $quick_seconds,
        MIGRATION_MEDIUM_SECONDS: $medium_seconds,
        MIGRATION_LONG_SECONDS: $long_seconds
      }
    }'
}

create_demo_build() {
  local pipeline_slug=$1
  local default_branch=$2
  local sequence=$3
  local dynamic_queue=$4
  local delayed_queue=$5
  local response web_url

  response=$(build_payload \
      "$default_branch" \
      "$sequence" \
      "$dynamic_queue" \
      "$delayed_queue" |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug/builds") ||
    return 1

  if ! web_url=$(jq -er \
      '.web_url | select(type == "string" and length > 0)' <<< "$response"); then
    printf 'workload-generator: create build response for pipeline %s did not include web_url\n' \
      "$pipeline_slug" >&2
    return 1
  fi

  printf 'pipeline=%s sequence=%s dynamic_queue=%s url=%s\n' \
    "$pipeline_slug" "$sequence" "$dynamic_queue" "$web_url"
}

pipeline_branch() {
  local expected_suffix=$1
  local index

  for index in "${!PIPELINE_SUFFIXES[@]}"; do
    if [[ "${PIPELINE_SUFFIXES[$index]}" == "$expected_suffix" ]]; then
      printf '%s\n' "${DEFAULT_BRANCHES[$index]}"
      return 0
    fi
  done

  return 1
}

create_workload_cycle() {
  local pipeline_suffix pipeline_slug default_branch dynamic_queue delayed_queue

  while IFS= read -r pipeline_suffix; do
    pipeline_slug=$(workload_pipeline_slug "$WORKLOAD_RESOURCE_PREFIX" "$pipeline_suffix")
    default_branch=$(pipeline_branch "$pipeline_suffix")

    dynamic_queue=dynamic-blue
    delayed_queue=dynamic-green
    if [[ "$pipeline_suffix" == "dynamic-topology" ]]; then
      DYNAMIC_SEQUENCE=$((DYNAMIC_SEQUENCE + 1))
      dynamic_queue=$(dynamic_queue_variant "$DYNAMIC_SEQUENCE")
      if [[ "$dynamic_queue" == "dynamic-blue" ]]; then
        delayed_queue=dynamic-green
      else
        delayed_queue=dynamic-blue
      fi
    fi

    create_demo_build \
      "$pipeline_slug" \
      "$default_branch" \
      "$SEQUENCE" \
      "$dynamic_queue" \
      "$delayed_queue"
    SEQUENCE=$((SEQUENCE + 1))
    sleep "$BUILD_INTERVAL_SECONDS"
  done < <(workload_schedule "$WORKLOAD_LOAD")
}
