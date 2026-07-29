#!/bin/bash

load_repository_env() {
  local env_file

  env_file=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.env
  [[ -f "$env_file" ]] || return 0

  if [[ $- == *a* ]]; then
    # shellcheck disable=SC1090
    source "$env_file"
  else
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
  fi
}

create_curl_auth_header_file() {
  local scheme=$1
  local token=$2
  local auth_header_file

  [[ -n "$scheme" && -n "$token" ]] || return 1
  [[ "$scheme" != *$'\r'* && "$scheme" != *$'\n'* ]] || return 1
  [[ "$token" != *$'\r'* && "$token" != *$'\n'* ]] || return 1

  auth_header_file=$(mktemp "${TMPDIR:-/tmp}/cluster-migrator-curl-auth.XXXXXX") || return 1
  if ! printf 'Authorization: %s %s\n' "$scheme" "$token" > "$auth_header_file" ||
      ! chmod 600 "$auth_header_file"; then
    rm -f "$auth_header_file"
    return 1
  fi

  printf '%s\n' "$auth_header_file"
}

load_repository_env
unset -f load_repository_env
