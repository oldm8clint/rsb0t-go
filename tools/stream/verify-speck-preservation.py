"""Check frozen before/after PNGs: only isolated dark components may change."""
import json
import sys
from pathlib import Path
from PIL import Image

before_dir, after_dir = map(Path, sys.argv[1:3])
report = []
for before_file in sorted(before_dir.glob("*.png")):
    after_file = after_dir / before_file.name
    before = Image.open(before_file).convert("RGB")
    after = Image.open(after_file).convert("RGB")
    assert before.size == after.size
    width, height = before.size
    old, new = list(before.get_flattened_data()), list(after.get_flattened_data())
    dark = bytearray(sum(pixel) <= 24 for pixel in old)
    seen, allowed = bytearray(len(dark)), bytearray(len(dark))
    components = 0
    for start, value in enumerate(dark):
        if not value or seen[start]:
            continue
        stack, pixels, edge = [start], [], False
        seen[start] = 1
        while stack:
            at = stack.pop()
            x, y = at % width, at // width
            pixels.append(at)
            edge |= x == 0 or y == 0 or x == width - 1 or y == height - 1
            for neighbour in (at - 1 if x else -1,
                              at + 1 if x < width - 1 else -1,
                              at - width if y else -1,
                              at + width if y < height - 1 else -1):
                if neighbour >= 0 and dark[neighbour] and not seen[neighbour]:
                    seen[neighbour] = 1
                    stack.append(neighbour)
        if len(pixels) <= 8 and not edge:
            components += 1
            for at in pixels:
                allowed[at] = 1
                assert sum(new[at]) > 24, (before_file.name, "speck remains", at)
    changed = 0
    for at, (a, b) in enumerate(zip(old, new)):
        if a != b:
            changed += 1
            assert allowed[at], (before_file.name, "non-speck pixel changed", at, a, b)
    report.append({"frame": before_file.name, "removed_components": components, "changed_pixels": changed})
assert report, "No frozen frames found"
print(json.dumps({"frames": len(report), "removed_components": sum(r["removed_components"] for r in report),
                  "non_speck_pixels_changed": 0, "detail": report}))
