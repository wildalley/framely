"""Render editable SVG brand assets with librsvg (rsvg-convert)."""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
renderer = shutil.which("rsvg-convert")
if renderer is None:
    raise SystemExit("Install librsvg's rsvg-convert before rendering icons")
assets = ROOT / "assets"
for name in ("app-icon", "tray-icon", "tray-template", "tray-recording"):
    subprocess.run([renderer, str(assets / f"{name}.svg"), "-o", str(assets / f"{name}.png")], check=True)
for size in (32, 64, 128, 256, 512):
    subprocess.run([renderer, "-w", str(size), "-h", str(size), str(assets / "app-icon.svg"), "-o", str(assets / "icons" / f"{size}x{size}.png")], check=True)
