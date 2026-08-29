# The Bedrock geometry.json format

A model is a flat list of **bones**. Each bone has a name, an optional parent, a pivot point, an optional rotation, and a list of axis-aligned **cubes**. There are no arbitrary meshes — a Minecraft model is boxes, all the way down.

## Two wire formats

Bedrock has shipped two shapes for the same data, and both still appear in live traffic. `ParseGeometry` detects which it has and normalizes both to `[]Geometry`.

### Modern (`format_version` 1.12.0 and later)

Entries live in a `minecraft:geometry` array, each with a `description` block:

```json
{
  "format_version": "1.12.0",
  "minecraft:geometry": [
    {
      "description": {
        "identifier": "geometry.humanoid.custom",
        "texture_width": 64,
        "texture_height": 64
      },
      "bones": [ ... ]
    }
  ]
}
```

### Legacy (pre-1.12)

The identifier is a **top-level key**, and the texture dimensions lose their underscores:

```json
{
  "format_version": "1.8.0",
  "geometry.humanoid.custom": {
    "texturewidth": 64,
    "textureheight": 64,
    "bones": [ ... ]
  }
}
```

Note `texturewidth` / `textureheight` here versus `texture_width` / `texture_height` above. This form was confirmed against a real capture of `2ndBirthday.PartyPlasticCreeper`.

Bone and cube fields are **identical** between the two. Only the outer wrapper differs, which is why normalizing is a small job.

## One file, several models

A geometry file almost always holds more than one entry. A real capture contains three:

| Identifier | Bones | Cubes | What it is |
| --- | --- | --- | --- |
| `geometry.cape` | 3 | 1 | The cape, self-contained |
| `geometry.humanoid.custom` | 17 | 12 | Wide-armed body |
| `geometry.humanoid.customSlim` | 17 | 12 | Slim-armed body |

`SelectGeometry` picks between them. See [design-decisions.md](design-decisions.md#why-select-by-cube-count).

## Bones

```json
{
  "name": "leftArm",
  "parent": "body",
  "pivot": [5, 22, 0],
  "rotation": [0, 0, 0],
  "inflate": 0,
  "cubes": [ ... ]
}
```

| Field | Meaning |
| --- | --- |
| `name` | Unique within the entry. Referenced by parents and by bone scoping. |
| `parent` | Name of the parent bone, or absent for a root. |
| `pivot` | The point this bone rotates around, in model space. |
| `rotation` | Degrees around X, Y, Z. |
| `inflate` | Grows every cube in the bone outward by this much on all sides. |
| `cubes` | May be absent or empty — plenty of bones are pure structure. |

Bones with no cubes are completely normal. In the vanilla model, `root`, `waist`, `cape`, `leftItem` and `rightItem` all carry no geometry; they exist to position other things.

The vanilla humanoid bone set is:

```
root, body, waist, head, cape, hat,
leftArm, leftSleeve, leftItem,
rightArm, rightSleeve, rightItem,
leftLeg, leftPants, rightLeg, rightPants,
jacket
```

Custom skins add their own bones to this — ears, tails, wings, hats — parented somewhere in the standard tree. Nothing in this library hardcodes that list; see [views-and-cameras.md](views-and-cameras.md).

## Cubes

```json
{
  "origin": [-4, 24, -4],
  "size": [8, 8, 8],
  "uv": [0, 0],
  "inflate": 0.25,
  "mirror": false
}
```

| Field | Meaning |
| --- | --- |
| `origin` | The cube's minimum corner in model space. |
| `size` | Extent along X, Y, Z. |
| `uv` | Where the cube's faces live in the texture. Two possible forms — below. |
| `inflate` | Overrides the bone's inflate for this cube. |
| `mirror` | Flips the east/west faces' horizontal texture direction. |

### Inflate and the layer system

`inflate` is how Minecraft does its second skin layer. The overlay bones — `hat`, `jacket`, `leftSleeve`, `rightSleeve`, `leftPants`, `rightPants` — are geometrically *identical* to the body parts underneath, but with `inflate: 0.25`, so they sit just outside and never z-fight.

Their textures are mostly transparent, and that transparency is what makes them work. See the alpha-test discussion in [rendering-pipeline.md](rendering-pipeline.md#alpha-testing).

### Wide vs slim, concretely

The only real difference between the two humanoid variants:

| | Wide (`custom`) | Slim (`customSlim`) |
| --- | --- | --- |
| `leftArm` origin | `[4, 12, -2]` | `[4, 11.5, -2]` |
| `leftArm` size | `[4, 12, 4]` | `[3, 12, 4]` |
| `rightArm` origin | `[-8, 12, -2]` | `[-7, 11.5, -2]` |

One unit narrower, and half a unit lower to keep the shoulder line right.

## UV mapping

A cube's `uv` field takes one of two forms, which is why `Cube.UV` is kept as `json.RawMessage` and resolved at mesh-build time.

### Box UV (the common case)

A single `[u, v]` origin. All six faces are laid out around it in Minecraft's standard unwrapped-box arrangement, derived from the cube's own size `(w, h, d)`:

```
              ┌─────┬─────┐
              │ up  │down │            up:    (u+d,     v,     w, d)
              │ w×d │ w×d │            down:  (u+d+w,   v,     w, d)
        ┌─────┼─────┼─────┼─────┐      west:  (u,       v+d,   d, h)
        │west │north│east │south│      north: (u+d,     v+d,   w, h)
        │ d×h │ w×h │ d×h │ w×h │      east:  (u+d+w,   v+d,   d, h)
        └─────┴─────┴─────┴─────┘      south: (u+d+w+d, v+d,   w, h)
```

`north` is the **front** — the face with the eyes on it. That was not assumed; it was confirmed by decoding a real skin's pixels and checking where the face is painted.

### Per-face UV

Each face gets its own explicit rectangle:

```json
"uv": {
  "north": { "uv": [0, 0],  "uv_size": [8, 8] },
  "south": { "uv": [16, 0], "uv_size": [8, 8] }
}
```

Faces that are absent are simply not drawn. This form is used by custom models whose parts do not fit the box layout.

### Coordinates are in texture pixels

Both forms give coordinates in **texture pixels**, not normalized 0–1. Conversion to normalized coordinates uses the entry's declared `texture_width` / `texture_height`, which is why those fields matter and why a mismatch between declared and actual texture size skews the whole model.

## Bone rotation: the one unverified corner

Local transforms are built as: rotate around X, then Y, then Z, about the bone's own origin; then translate by `ownPivot - parentPivot`.

This matches the reference browser implementation, but it is **the one part of this library not confirmed against real data** — every skin captured so far has `rotation: [0, 0, 0]` on every bone, so nothing has exercised it. If you have a skin with genuinely rotated bones and it renders wrong, the axis order or a sign convention here is the first place to look.

Everything else in this document was verified against captures.
