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
