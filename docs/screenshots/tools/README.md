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

The voice-over (`video/narration.py`: the spoken line of each caption or slide) is synthesised by
`video/voice.py` with [Kokoro](https://github.com/thewh1teagle/kokoro-onnx) (local neural TTS, voice
`af_heart`), in a Python 3.12 venv with the model files from the kokoro-onnx release `model-files-v1.0`.
`render.py` lengthens every segment whose line is longer than it, so voice and picture stay in sync.

```bash
python3 docs/screenshots/tools/video/record.py
uv venv -p 3.12 $SHOTS_DIR/tts && VIRTUAL_ENV=$SHOTS_DIR/tts uv pip install kokoro-onnx soundfile   # once
$SHOTS_DIR/tts/bin/python docs/screenshots/tools/video/voice.py $SHOTS_DIR/out/video/timeline.json \
  $SHOTS_DIR/tts-models $SHOTS_DIR/out/video/voice
L=$SHOTS_DIR/libs/root/usr/lib/x86_64-linux-gnu
LD_LIBRARY_PATH=$L:$L/nss $SHOTS_DIR/venv/bin/python docs/screenshots/tools/video/render.py \
  $SHOTS_DIR/out/video/timeline.json OUT.mp4 $SHOTS_DIR/out/video/voice/voice.json
```
