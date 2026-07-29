#!/bin/bash

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=util/common.sh
source "$SCRIPT_DIR/util/common.sh"

echo "cluster-migrator: not implemented" >&2
exit 1
