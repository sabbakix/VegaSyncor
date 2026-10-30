#!/bin/bash
# shot.sh NAME [--trim-top]: captures the tmux session "t" into $OUT/NAME.png
set -e
. "$(dirname "$0")/env.sh"
mkdir -p "$OUT"
tmux capture-pane -t t -p -e > "$OUT/$1.raw"
python3 "$TOOLS/fixansi.py" "$OUT/$1.raw" "$OUT/$1.ansi" $2
timeout 30 "$FREEZE" "$OUT/$1.ansi" --language ansi --window --padding 16,20 --margin 0 --font.size 14 \
  --font.family 'DejaVu Sans Mono' --font.file /usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf \
  --line-height 1.25 --border.radius 10 --background "#1B2333" -o "$OUT/$1.svg" </dev/null >/dev/null 2>&1
L=$SHOTS_DIR/libs/root/usr/lib/x86_64-linux-gnu
LD_LIBRARY_PATH=$L:$L/nss "$SHOTS_DIR/venv/bin/python" "$TOOLS/svg2png.py" "$OUT/$1.svg" "$OUT/$1.png"
echo "$OUT/$1.png"
