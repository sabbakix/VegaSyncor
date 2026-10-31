# Screenshot tools

Development only: they produce the PNGs in `docs/screenshots` from the real TUI.

Pipeline: `tmux capture-pane -e` → `fixansi.py` (colour state per line) →
[freeze](https://github.com/charmbracelet/freeze) (SVG) → `svg2png.py` (headless Chromium via
Playwright, letter spacing fixed to freeze's character grid).

```bash
docs/screenshots/tools/setup.sh                    # once: Playwright venv + Chromium libraries
docs/screenshots/tools/demo.sh                     # demo data + service in tmux session "d"
docs/screenshots/tools/take.sh                     # takes the screenshots into $OUT
```

`SHOTS_DIR` (default `/tmp/vegasyncor-shots`) holds the venv, the demo data and the output; freeze
is expected in `~/.local/bin/freeze` (`FREEZE` to change it). Requires `tmux`, `rclone`, `curl`.

## Presentation video

`video/record.py` drives the real TUI of the demo (it runs `demo.sh` itself) and writes the timeline of
frames, captions and key presses; `video/render.py` draws each frame with headless Chromium (title
slides, terminal window, chapter list, caption, key pressed) and encodes the MP4 with the ffmpeg of
`imageio-ffmpeg` (`$SHOTS_DIR/venv/bin/pip install imageio-ffmpeg`).

```bash
python3 docs/screenshots/tools/video/record.py
L=$SHOTS_DIR/libs/root/usr/lib/x86_64-linux-gnu
LD_LIBRARY_PATH=$L:$L/nss $SHOTS_DIR/venv/bin/python docs/screenshots/tools/video/render.py $SHOTS_DIR/out/video/timeline.json OUT.mp4
```
