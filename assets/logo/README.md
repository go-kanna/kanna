# Logo

## The mark

The mark is a *kanna-kuzu* — the shaving a hand plane lifts off a board.

The plane itself is the obvious thing to draw and the wrong one: a wooden block with an angled blade collapses into an
unreadable smudge below about 40px. The shaving survives, and it says more. A shaving thin enough to see through is
exactly what the tool is judged by, which is the same claim the generators make about their output.

The curl is a logarithmic spiral, so the angle between the curve and the radius stays constant as it winds in — that is
what makes the coil read as something that grew rather than something that was drawn. Width decays geometrically over
the same sweep: thinnest at the tip that curled first, thickest at the mouth. The mouth is raked back on a slant because
a blade leaves an angled face, never a square one.

Shaving and ground are the only two colors. There is no wood brown anywhere: drawn in Go's cyan, what is being cut is
not timber but Go itself.

## Files

| File                     | Use                                                                             |
|--------------------------|---------------------------------------------------------------------------------|
| `avatar-blade.png`       | **Org avatar.** 1000×1000, square corners                                       |
| `avatar-ink.png`         | Same, dark ground                                                               |
| `avatar-white.png`       | Same, white ground                                                              |
| `mark-blade-rounded.svg` | Rounded, for READMEs and docs                                                   |
| `mark-ink-rounded.svg`   | Same, dark ground                                                               |
| `mark-transparent.svg`   | No ground; fill is `currentColor`, so it takes the color of whatever it sits in |
| `generate.py`            | Regenerates every SVG above                                                     |

Prefer the blade ground for anything that lands on GitHub. GitHub's dark theme sits at `#0d1117`, close enough to the
ink ground that the avatar's silhouette disappears into the page and only the shaving is left floating. The cyan ground
holds its edge in both themes.

## Palette

|       | Hex       |                                          |
|-------|-----------|------------------------------------------|
| ink   | `#0B1A21` | ground, and the shaving on cyan          |
| blade | `#00ADD8` | Go cyan; the shaving on ink              |
| paper | `#F2F6F7` | cool off-white, for surrounding surfaces |

## Regenerating

```sh
python3 generate.py
```

Geometry lives in the constants at the top of the script — turns, radii, band width, and the rake of the mouth. Nothing
else needs editing.

Rasterizing is left out of the script so it stays dependency-free. With `rsvg-convert`:

```sh
for n in avatar-blade avatar-ink avatar-white; do
  rsvg-convert -w 1000 -h 1000 "$n.svg" -o "$n.png"
done
```

Or with headless Chrome, which needs no extra install on macOS:

```sh
for n in avatar-blade avatar-ink avatar-white; do
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    --headless --disable-gpu --hide-scrollbars \
    --window-size=1000,1000 --screenshot="$n.png" "file://$PWD/$n.svg"
done
```

## Why the avatars have square corners

GitHub rounds an org avatar itself. Uploading an image with the radius already baked in leaves a sliver of the ground
color outside the curve but inside GitHub's own crop, which reads as a dirty edge. The avatars are therefore square to
the edge; the rounded variants exist only for places that do no cropping of their own.
