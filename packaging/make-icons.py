#!/usr/bin/env python3
"""Regenerates the app icons (icon.png, icon.ico, icon.icns) from the logo in web/index.html.
Needs Pillow. The outputs are committed, so the release build does not run this."""
import os
from PIL import Image, ImageDraw

S = 1024
k = S / 32
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)
d.rounded_rectangle((0, 0, S - 1, S - 1), radius=int(S * 0.22), fill="#2f6fed")
w = int(2.5 * k * 1.15)
# clock face, hand and crown: same shapes as the logo (viewBox 32), scaled 1.15 about the centre
def p(x, y):
    return ((x - 16) * 1.15 + 16) * k, ((y - 16) * 1.15 + 16) * k
cx, cy = p(16, 18); r = 11 * 1.15 * k
d.ellipse((cx - r, cy - r, cx + r, cy + r), outline="white", width=w)
pts = [p(16, 11), p(16, 18), p(20, 21)]
d.line(pts, fill="white", width=w, joint="curve")
for q in (pts[0], pts[2]):
    d.ellipse((q[0] - w / 2, q[1] - w / 2, q[0] + w / 2, q[1] + w / 2), fill="white")
x0, y0 = p(13, 3); x1, y1 = p(19, 6)
d.rounded_rectangle((x0, y0, x1, y1), radius=int(k * 1.2), fill="white")

here = os.path.dirname(os.path.abspath(__file__))
img.resize((512, 512), Image.LANCZOS).save(os.path.join(here, "icon.png"))
img.save(os.path.join(here, "icon.ico"), sizes=[(s, s) for s in (16, 24, 32, 48, 64, 128, 256)])
img.save(os.path.join(here, "icon.icns"))
