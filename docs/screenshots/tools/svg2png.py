"""Renders a freeze SVG to PNG with headless Chromium, adjusting the letter
spacing so the text matches the character grid freeze assumed."""
import pathlib, re, sys
from playwright.sync_api import sync_playwright

svg, out = sys.argv[1], sys.argv[2]
data = pathlib.Path(svg).read_text()
w = float(re.search(r'<svg width="([\d.]+)"', data).group(1))
h = float(re.search(r'height="([\d.]+)"', data).group(1))
TARGET = 14 * 0.6 * 0.99206  # character advance assumed by freeze at 14px
with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={"width": int(w + 1), "height": int(h + 1)}, device_scale_factor=2)
    pg.set_content(f'<html><body style="margin:0;background:transparent">{data[data.index("<svg"):]}</body></html>')
    pg.wait_for_timeout(300)
    adv = pg.evaluate('''() => { let best=[0,0]; for (const e of document.querySelectorAll("body > svg text")) {
        const n=e.textContent.length; if (n>best[0]) best=[n, e.getComputedTextLength()] } return best[1]/best[0] }''')
    pg.add_style_tag(content=f"body > svg text {{ letter-spacing: {TARGET-adv:.4f}px; }}")
    pg.wait_for_timeout(100)
    pg.locator("body > svg").screenshot(path=out, omit_background=True)
    b.close()
