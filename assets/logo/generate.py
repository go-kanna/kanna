#!/usr/bin/env python3
"""Generate the go-kanna mark.

The mark is a kanna-kuzu -- the shaving a hand plane lifts off a board. A real
shaving curls as a logarithmic spiral: the angle between the curve and the
radius stays constant as it winds in, which is why the coil reads as something
that grew rather than something that was drawn. Width decays geometrically over
the same sweep, so the band is thinnest at the tip that curled first and
thickest at the mouth where the blade is still cutting.

Run with no arguments to rewrite every SVG in this directory:

    python3 generate.py

PNGs are not produced here -- rasterizing needs a renderer this script should
not depend on. See README.md for the one-liner.
"""
import math
import os

# Geometry. All values are in the 256x256 design canvas.
CANVAS = 256
CENTER = (128, 128)
R_START = 90        # radius at the mouth
R_END = 23          # radius at the curled tip
TURNS = 1.06
PHASE = 214         # degrees; where the mouth sits on the circle
W_START = 34        # band width at the mouth
W_END = 12          # band width at the tip
SLANT = 44          # arc length the inner edge starts late by, raking the mouth
PAD = 34            # keeps the mark clear of the corners GitHub crops
CORNER = 56         # radius of the rounded variants

INK = "#0B1A21"
BLADE = "#00ADD8"
WHITE = "#FFFFFF"

SAMPLES = 200


def centreline():
    theta_max = TURNS * 2 * math.pi
    k = math.log(R_END / R_START) / theta_max
    phase = math.radians(PHASE)
    pts = []
    for i in range(SAMPLES + 1):
        t = theta_max * i / SAMPLES
        r = R_START * math.exp(k * t)
        a = t + phase
        pts.append((CENTER[0] + r * math.cos(a), CENTER[1] + r * math.sin(a)))
    return pts


def boundary(pts):
    """Offset the centreline into a closed band."""
    total = 0.0
    acc = [0.0]
    for a, b in zip(pts, pts[1:]):
        total += math.hypot(b[0] - a[0], b[1] - a[1])
        acc.append(total)

    ratio = W_END / W_START
    left, right = [], []
    for i, p in enumerate(pts):
        if i == 0:
            tx, ty = pts[1][0] - p[0], pts[1][1] - p[1]
        elif i == len(pts) - 1:
            tx, ty = p[0] - pts[-2][0], p[1] - pts[-2][1]
        else:
            tx, ty = pts[i + 1][0] - pts[i - 1][0], pts[i + 1][1] - pts[i - 1][1]
        n = math.hypot(tx, ty) or 1
        nx, ny = -ty / n, tx / n
        w = W_START * (ratio ** (acc[i] / total)) / 2
        left.append((p[0] + nx * w, p[1] + ny * w))
        right.append((p[0] - nx * w, p[1] - ny * w))

    # The angled face at the mouth comes from starting the inner edge further
    # along the centreline, not from displacing an endpoint: a point pushed
    # past its own neighbours makes the boundary double back into a sliver.
    start = 0
    while start < len(acc) - 2 and acc[start] < SLANT:
        start += 1
    return left + list(reversed(right[start:]))


def fit(pts):
    """Scale and centre the band inside the padded canvas."""
    xs = [p[0] for p in pts]
    ys = [p[1] for p in pts]
    avail = CANVAS - 2 * PAD
    s = min(avail / (max(xs) - min(xs)), avail / (max(ys) - min(ys)))
    return (s,
            CANVAS / 2 - s * (min(xs) + max(xs)) / 2,
            CANVAS / 2 - s * (min(ys) + max(ys)) / 2)


def svg(pts, size, ground, fg, corner):
    s, tx, ty = fit(pts)
    d = "M " + " L ".join("%.1f %.1f" % p for p in pts) + " Z"
    rect = ""
    if ground:
        rect = '<rect width="%d" height="%d" rx="%d" fill="%s"/>' % (
            CANVAS, CANVAS, corner, ground)
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" '
        'viewBox="0 0 %d %d">%s'
        '<g transform="translate(%.2f %.2f) scale(%.4f)">'
        '<path d="%s" fill="%s"/></g></svg>'
        % (size, size, CANVAS, CANVAS, rect, tx, ty, s, d, fg)
    )


# (filename, ground, foreground, size, corner radius)
#
# The avatars carry no corner radius: GitHub rounds an org avatar itself, and a
# radius baked into the upload leaves a sliver of the ground colour inside
# GitHub's own crop.
OUTPUTS = [
    ("avatar-blade", BLADE, INK, 1000, 0),
    ("avatar-ink", INK, BLADE, 1000, 0),
    ("avatar-white", WHITE, INK, 1000, 0),
    ("mark-blade-rounded", BLADE, INK, CANVAS, CORNER),
    ("mark-ink-rounded", INK, BLADE, CANVAS, CORNER),
    ("mark-transparent", None, "currentColor", CANVAS, 0),
]


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    pts = boundary(centreline())
    for name, ground, fg, size, corner in OUTPUTS:
        path = os.path.join(here, name + ".svg")
        with open(path, "w") as f:
            f.write(svg(pts, size, ground, fg, corner))
        print("wrote", name + ".svg")


if __name__ == "__main__":
    main()
