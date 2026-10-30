# shared settings of the screenshot tools (sourced)
SHOTS_DIR=${SHOTS_DIR:-/tmp/vegasyncor-shots}
TOOLS=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$TOOLS/../../.." && pwd)
DEMO=$SHOTS_DIR/demo
OUT=$SHOTS_DIR/out
FREEZE=${FREEZE:-$HOME/.local/bin/freeze}
