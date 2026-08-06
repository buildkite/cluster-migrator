#!/bin/bash

SETUP_CLUSTER_NAME="Cluster Migrator Demo"
SETUP_CLUSTER_TOKEN_DESCRIPTION="Cluster Migrator Demo clustered agents"
SETUP_UNCLUSTERED_TOKEN_DESCRIPTION="Cluster Migrator Demo unclustered agents"

cluster_by_name() {
  local cluster_name=$1

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters?per_page=100" |
    jq -c --arg name "$cluster_name" '
      [.[] | select(.name == $name)] |
      if length == 0 then null
      elif length == 1 then .[0]
      else error("multiple clusters named " + $name)
      end
    '
}

create_cluster() {
  local cluster_name=$1

  jq -n --arg name "$cluster_name" '{name: $name}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters"
}

ensure_cluster() {
  local cluster_name=$1
  local cluster

  cluster=$(cluster_by_name "$cluster_name")
  if [[ "$cluster" == "null" ]]; then
    create_cluster "$cluster_name"
  else
    printf '%s\n' "$cluster"
  fi
}

cluster_queue_by_key() {
  local cluster_id=$1
  local queue_key=$2

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters/$cluster_id/queues?per_page=100" |
    jq -c --arg key "$queue_key" '
      [.[] | select(.key == $key)] |
      if length == 0 then null
      elif length == 1 then .[0]
      else error("multiple queues with key " + $key)
      end
    '
}

create_cluster_queue() {
  local cluster_id=$1
  local queue_key=$2

  jq -n --arg key "$queue_key" '{key: $key}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters/$cluster_id/queues"
}

ensure_cluster_queue() {
  local cluster_id=$1
  local queue_key=$2
  local queue

  queue=$(cluster_queue_by_key "$cluster_id" "$queue_key")
  if [[ "$queue" == "null" ]]; then
    create_cluster_queue "$cluster_id" "$queue_key"
  else
    printf '%s\n' "$queue"
  fi
}

setup_workload_queues() {
  local cluster_id=$1
  local resource_prefix=$2
  local queue_suffix queue_key

  for queue_suffix in "${WORKLOAD_QUEUE_KEYS[@]}"; do
    queue_key=$(workload_queue_key "$resource_prefix" "$queue_suffix")
    ensure_cluster_queue "$cluster_id" "$queue_key" >/dev/null
  done
}

everyone_team_id() {
  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/teams?per_page=100" |
    jq -r '.[] | select(.name == "Everyone") | .id'
}

workload_bootstrap_configuration() {
  local default_queue=$1
  local pipeline_file=$2
  local resource_prefix=$3

  cat <<EOF
env:
  MIGRATION_WORKLOAD_PREFIX: "$resource_prefix"

agents:
  queue: "$default_queue"

steps:
  - label: "Pipeline upload"
    command: "buildkite-agent pipeline upload $pipeline_file"
EOF
}

create_demo_pipeline() {
  local pipeline_slug=$1
  local pipeline_name=$2
  local default_queue=$3
  local pipeline_file=$4
  local team_id=$5
  local default_branch=$6
  local resource_prefix=$7
  local repository=$8
  local configuration

  configuration=$(workload_bootstrap_configuration \
    "$default_queue" "$pipeline_file" "$resource_prefix")

  jq -n \
    --arg slug "$pipeline_slug" \
    --arg name "$pipeline_name" \
    --arg team_id "$team_id" \
    --arg default_branch "$default_branch" \
    --arg repository "$repository" \
    --arg configuration "$configuration" \
    '{
      name: $name,
      slug: $slug,
      repository: $repository,
      default_branch: $default_branch,
      cluster_id: null,
      teams: {($team_id): "manage_build_and_read"},
      configuration: $configuration
    }' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines"
}

pipeline_by_slug() {
  local pipeline_slug=$1
  local response status

  response=$(curl "${COMMON_CURL_ARGS[@]}" \
    --no-fail \
    --write-out $'\n%{http_code}' \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug")
  status=${response##*$'\n'}
  response=${response%$'\n'*}

  case "$status" in
    200)
      printf '%s\n' "$response"
      ;;
    404)
      printf 'null\n'
      ;;
    *)
      printf 'could not read pipeline %s: API returned HTTP %s\n' \
        "$pipeline_slug" "$status" >&2
      return 1
      ;;
  esac
}

assert_demo_pipeline_configuration() {
  local pipeline=$1
  local pipeline_slug=$2
  local expected_queue=$3
  local expected_pipeline_file=$4
  local expected_prefix=$5
  local expected_repository=$6
  local existing_repository existing_cluster configuration

  existing_repository=$(jq -r '.repository // .provider.settings.repository // empty' <<< "$pipeline")
  if [[ -n "$existing_repository" && "$existing_repository" != "$expected_repository" ]]; then
    printf 'pipeline %s uses repository %s; expected %s\n' \
      "$pipeline_slug" "$existing_repository" "$expected_repository" >&2
    return 1
  fi

  existing_cluster=$(jq -r '.cluster.id // .cluster_id // empty' <<< "$pipeline")
  if [[ -n "$existing_cluster" ]]; then
    printf 'pipeline %s is already assigned to cluster %s; expected unclustered\n' \
      "$pipeline_slug" "$existing_cluster" >&2
    return 1
  fi

  configuration=$(jq -r '.configuration // empty' <<< "$pipeline")
  if [[ "$configuration" != *"queue: \"$expected_queue\""* ||
        "$configuration" != *"pipeline upload $expected_pipeline_file"* ||
        "$configuration" != *"MIGRATION_WORKLOAD_PREFIX: \"$expected_prefix\""* ]]; then
    printf 'pipeline %s has incompatible workload bootstrap configuration\n' \
      "$pipeline_slug" >&2
    return 1
  fi
}

ensure_demo_pipeline() {
  local pipeline_slug=$1
  local pipeline_name=$2
  local default_queue=$3
  local pipeline_file=$4
  local team_id=$5
  local expected_default_branch=$6
  local resource_prefix=$7
  local repository=$8
  local pipeline existing_default_branch

  pipeline=$(pipeline_by_slug "$pipeline_slug")
  if [[ "$pipeline" != "null" ]]; then
    if ! existing_default_branch=$(jq -er \
        '.default_branch | select(type == "string" and length > 0)' <<< "$pipeline"); then
      printf 'pipeline %s does not have a default branch\n' "$pipeline_slug" >&2
      return 1
    fi
    if [[ "$existing_default_branch" != "$expected_default_branch" ]]; then
      printf 'pipeline %s uses default branch %s; expected %s\n' \
        "$pipeline_slug" "$existing_default_branch" "$expected_default_branch" >&2
      return 1
    fi
    assert_demo_pipeline_configuration \
      "$pipeline" \
      "$pipeline_slug" \
      "$default_queue" \
      "$pipeline_file" \
      "$resource_prefix" \
      "$repository" || return 1
    printf '%s\n' "$pipeline"
  else
    create_demo_pipeline \
      "$pipeline_slug" \
      "$pipeline_name" \
      "$default_queue" \
      "$pipeline_file" \
      "$team_id" \
      "$expected_default_branch" \
      "$resource_prefix" \
      "$repository"
  fi
}

setup_workload_pipelines() {
  local team_id=$1
  local default_branch=$2
  local resource_prefix=$3
  local repository=$4
  local record suffix name default_queue_suffix pipeline_file
  local pipeline_slug pipeline_name default_queue

  for record in "${WORKLOAD_PIPELINES[@]}"; do
    IFS='|' read -r suffix name default_queue_suffix pipeline_file <<< "$record"
    pipeline_slug=$(workload_pipeline_slug "$resource_prefix" "$suffix")
    pipeline_name=$(workload_pipeline_name "$resource_prefix" "$name")
    default_queue=$(workload_queue_key "$resource_prefix" "$default_queue_suffix")
    ensure_demo_pipeline \
      "$pipeline_slug" \
      "$pipeline_name" \
      "$default_queue" \
      "$pipeline_file" \
      "$team_id" \
      "$default_branch" \
      "$resource_prefix" \
      "$repository" >/dev/null
  done
}

create_cluster_agent_token() {
  local cluster_id=$1
  local description=$2

  jq -n --arg description "$description" '{description: $description}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters/$cluster_id/tokens"
}

graphql_request() {
  local query=$1

  jq -n --arg query "$query" '{query: $query}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_GRAPHQL_URL" |
    jq -c 'if .errors then error([.errors[].message] | join("; ")) else . end'
}

organization_graphql_id() {
  local organization_slug=$1
  local slug_literal query

  slug_literal=$(jq -Rn --arg value "$organization_slug" '$value')
  query="query { organization(slug: $slug_literal) { id } }"
  graphql_request "$query" | jq -er '.data.organization.id'
}

create_unclustered_agent_token() {
  local organization_id=$1
  local description=$2
  local organization_id_literal description_literal query

  organization_id_literal=$(jq -Rn --arg value "$organization_id" '$value')
  description_literal=$(jq -Rn --arg value "$description" '$value')
  query="mutation { agentTokenCreate(input: { organizationID: $organization_id_literal, description: $description_literal }) { tokenValue agentTokenEdge { node { id } } } }"
  graphql_request "$query"
}

save_env_value() {
  local env_file=$1
  local key=$2
  local value=$3
  local shell_value temporary_file

  printf -v shell_value '%q' "$value"
  touch "$env_file"
  temporary_file=$(mktemp "$env_file.XXXXXX")
  if ! ENV_KEY="$key" ENV_VALUE="$shell_value" awk '
    BEGIN { found = 0 }
    index($0, ENVIRON["ENV_KEY"] "=") == 1 {
      if (!found) print ENVIRON["ENV_KEY"] "=" ENVIRON["ENV_VALUE"]
      found = 1
      next
    }
    { print }
    END {
      if (!found) print ENVIRON["ENV_KEY"] "=" ENVIRON["ENV_VALUE"]
    }
  ' "$env_file" > "$temporary_file"; then
    rm -f "$temporary_file"
    return 1
  fi
  if ! chmod 600 "$temporary_file"; then
    rm -f "$temporary_file"
    return 1
  fi
  mv "$temporary_file" "$env_file"
}
