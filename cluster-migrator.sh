#!/bin/bash

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=util/load-env.sh
source "$SCRIPT_DIR/util/load-env.sh"

echo "cluster-migrator: not implemented" >&2
exit 1
