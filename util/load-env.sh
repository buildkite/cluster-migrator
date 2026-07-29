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

load_repository_env
unset -f load_repository_env
