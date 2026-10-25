#!/usr/bin/env python3
"""Generate Presyo Bantay brand assets from the source icon.

Usage:
    python3 scripts/make_brand_assets.py .scratch/presyo-bantay-icon.png

Reads the source icon path from argv[1] and writes outputs under
site/public/ (Vite copies site/public/ to dist/ verbatim):

    site/public/brand/logo-mark-96.webp  + .png fallback (circle-cropped)
    site/public/brand/logo-mark-144.webp + .png fallback (circle-cropped)
    site/public/favicon-32.png
    site/public/favicon-16.png
    site/public/apple-touch-icon.png (180x180, opaque)

The header mark is circle-cropped with a transparent outside via an
antialiased circular alpha mask (supersampled 4x, LANCZOS downsample).
Favicons / touch icons keep the full opaque square.
"""

from __future__ import annotations

import sys
from pathlib import Path

from PIL import Image, ImageDraw

# Circle geometry in source pixels (1024x1024 icon: cream circle on a
# lighter cream square, nearly full-bleed; edges sampled at ~12px top,
# ~24px left, ~1004px right, ~1010px bottom).
SRC_SIZE = 1024
CIRCLE_CENTER = (512, 512)
CIRCLE_RADIUS = 500
SUPERSAMPLE = 4


def circle_alpha(size: int) -> Image.Image:
    """Antialiased circular alpha mask (white inside) at `size` px."""
    big = size * SUPERSAMPLE
    scale = big / SRC_SIZE
    cx, cy = CIRCLE_CENTER[0] * scale, CIRCLE_CENTER[1] * scale
    r = CIRCLE_RADIUS * scale
    mask = Image.new("L", (big, big), 0)
    ImageDraw.Draw(mask).ellipse([cx - r, cy - r, cx + r, cy + r], fill=255)
    return mask.resize((size, size), Image.LANCZOS)


def save_mark(src: Image.Image, size: int, out_png: Path, out_webp: Path) -> None:
    mark = src.resize((size, size), Image.LANCZOS)
    mark.putalpha(circle_alpha(size))
    mark.save(out_png, optimize=True)
    mark.save(out_webp, quality=85, method=6)


def save_square(src: Image.Image, size: int, out: Path) -> None:
    src.resize((size, size), Image.LANCZOS).save(out, optimize=True)


def main() -> None:
    if len(sys.argv) != 2:
        print(__doc__.strip().splitlines()[3].strip(), file=sys.stderr)
        raise SystemExit(2)
    src_path = Path(sys.argv[1])
    root = Path(__file__).resolve().parent.parent
    public = root / "site" / "public"
    brand = public / "brand"
    brand.mkdir(parents=True, exist_ok=True)

    src = Image.open(src_path).convert("RGB")
    if src.size != (SRC_SIZE, SRC_SIZE):
        src = src.resize((SRC_SIZE, SRC_SIZE), Image.LANCZOS)

    save_mark(src, 96, brand / "logo-mark-96.png", brand / "logo-mark-96.webp")
    save_mark(src, 144, brand / "logo-mark-144.png", brand / "logo-mark-144.webp")
    save_square(src, 32, public / "favicon-32.png")
    save_square(src, 16, public / "favicon-16.png")
    save_square(src, 180, public / "apple-touch-icon.png")

    for p in [
        brand / "logo-mark-96.webp",
        brand / "logo-mark-144.webp",
        brand / "logo-mark-96.png",
        brand / "logo-mark-144.png",
        public / "favicon-32.png",
        public / "favicon-16.png",
        public / "apple-touch-icon.png",
    ]:
        print(f"{p.relative_to(root)} {p.stat().st_size} bytes")


if __name__ == "__main__":
    main()
