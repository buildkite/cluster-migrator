#!/bin/bash

SETUP_CLUSTER_NAME="Cluster Migrator Demo"
SETUP_CLUSTER_TOKEN_DESCRIPTION="Cluster Migrator Demo clustered agents"
SETUP_UNCLUSTERED_TOKEN_DESCRIPTION="Cluster Migrator Demo unclustered agents"
WORKLOAD_PIPELINE_NAMES=(
  "Cluster Migrator Target"
  "Cluster Migrator Shared Queue Consumer"
  "Cluster Migrator Unrelated Control"
)
WORKLOAD_PIPELINE_DEFAULT_QUEUES=(
  "target-only"
  "shared"
  "control-only"
)
WORKLOAD_PIPELINE_FILES=(
  "util/workload-generator/.buildkite/target/pipeline.yml"
  "util/workload-generator/.buildkite/shared-consumer/pipeline.yml"
  "util/workload-generator/.buildkite/control/pipeline.yml"
)

cluster_by_name() {
  local cluster_name=$1

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters" |
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
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/clusters/$cluster_id/queues" |
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

everyone_team_id() {
  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/teams" |
    jq -r '.[] | select(.name == "Everyone") | .id'
}

create_demo_pipeline() {
  local pipeline_slug=$1
  local pipeline_name=$2
  local default_queue=$3
  local pipeline_file=$4
  local team_id=$5
  local default_branch=$6

  jq -n \
    --arg slug "$pipeline_slug" \
    --arg name "$pipeline_name" \
    --arg default_queue "$default_queue" \
    --arg pipeline_file "$pipeline_file" \
    --arg team_id "$team_id" \
    --arg default_branch "$default_branch" \
    '{
      name: $name,
      slug: $slug,
      repository: "https://github.com/buildkite/cluster-migrator",
      default_branch: $default_branch,
      cluster_id: null,
      teams: {($team_id): "manage_build_and_read"},
      configuration: ("agents:\n  queue: \"" + $default_queue + "\"\n\nsteps:\n  - label: \"Pipeline upload\"\n    command: \"buildkite-agent pipeline upload " + $pipeline_file + "\"")
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

ensure_demo_pipeline() {
  local pipeline_slug=$1
  local pipeline_name=$2
  local default_queue=$3
  local pipeline_file=$4
  local team_id=$5
  local expected_default_branch=$6
  local pipeline existing_default_branch

  pipeline=$(pipeline_by_slug "$pipeline_slug")
  if [[ "$pipeline" != "null" ]]; then
    if ! jq -e 'has("cluster_id") and .cluster_id == null' <<< "$pipeline" >/dev/null; then
      printf 'pipeline %s is assigned to a cluster; expected an unclustered pipeline\n' \
        "$pipeline_slug" >&2
      return 1
    fi
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
    printf '%s\n' "$pipeline"
  else
    create_demo_pipeline \
      "$pipeline_slug" \
      "$pipeline_name" \
      "$default_queue" \
      "$pipeline_file" \
      "$team_id" \
      "$expected_default_branch"
  fi
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
