# The rendering pipeline

How a bone tree and a flat PNG become a picture, stage by stage. This is the page to read before changing anything in `mesh.go`, `render.go` or `shader.go`.

```
[]Geometry ──1──► Geometry ──2──► map[bone]Matrix ──3──► []*fauxgl.Triangle
                                                                │
                                                                4
                                                                ▼
                                          image.Image ◄──5── camera (eye, center)
```

1. **Select** one entry from the bundle.
2. **Flatten** the bone hierarchy into absolute matrices.
3. **Tessellate** every cube into triangles with texture coordinates.
4. **Frame** a camera around the triangles that actually exist.
5. **Rasterize** on the CPU.

## Stage 1 — Selecting an entry

A geometry file holds several models (body, slim body, cape). `SelectGeometry` picks one: by identifier if given, otherwise the entry with the most cubes.

That fallback is deliberate and is *not* "the first entry" — see [design-decisions.md](design-decisions.md#why-select-by-cube-count) for the bug that motivated it.

## Stage 2 — Flattening the bone hierarchy

Bones form a tree by name reference, but the rasterizer has no scene graph. fauxgl draws flat triangle soups in world space, so the hierarchy has to be **baked into vertex positions** before anything is drawn.

Each bone's local transform is:

```
local = RotateX(rx) · RotateY(ry) · RotateZ(rz) · Translate(ownPivot − parentPivot)
```

Positioning relative to the parent's pivot rather than absolutely is what makes the tree compose: each bone only knows its offset from its parent, so a rotation at the shoulder carries the whole arm with it.

World transforms come from walking up the parent chain:

```
world(bone) = world(parent) · local(bone)
```

`boneWorldMatrices` resolves these recursively with memoization, so a deep chain costs the same as a shallow one. It also carries a `seen` set: a malformed file can contain a **parent cycle**, and without cycle detection that recursion would never return. A cycle resolves to identity rather than hanging.

Unknown parent names degrade to identity too. A model referencing a bone that does not exist still renders, just unparented.

## Stage 3 — Cubes to triangles

For each included bone, for each of its cubes, `addCube` emits up to six faces, two triangles each.

### Inflate

```go
sx := c.Size[0] + 2*inflate
```

Inflate grows the cube on **both** sides of each axis, so the size grows by twice the value. A cube-level `inflate` overrides the bone's. This is what separates the overlay layer from the base layer — see [geometry-format.md](geometry-format.md#inflate-and-the-layer-system).

### Positioning

Vertices are built around the cube's centre, expressed relative to the owning bone's pivot:

```go
centerAbs   = origin + size/2
centerLocal = centerAbs − bonePivot
```

The bone's world matrix then maps that into model space. Working relative to the pivot is what lets the same cube data sit correctly under any bone transform.

### Face geometry

Every face is generated from the same parametric corner loop:

```
(-1,-1) ──► (1,-1) ──► (1,1) ──► (-1,1)
```

`faceCorner` maps that (u, v) pair to a 3D offset from the cube centre for each of the six faces, using half-extents `(hx, hy, hz)`. For example:

```go
case "north": return Vector{X: -u * hx, Y: v * hy, Z: -hz}  // front, fixed at -Z
case "east":  return Vector{X: hx, Y: v * hy, Z: -u * hz}   // fixed at +X
```

The pairing of texture rows to geometry rows matters and is easy to get backwards: the **bottom** of a face pairs with the texture's bottom row. Pairing them the other way renders every side face upside down — that was found by rendering, not by reasoning.

### Mirroring

`mirror: true` swaps `u0` and `u1` on the east and west faces only, flipping their horizontal texture direction. This is how a model reuses one arm's texture for both arms.

### The texture coordinate flip

This one is worth stating precisely, because it produced a genuinely confusing bug.

UV rectangles are computed **top-down**: row 0 is the top, matching normal PNG row order and the convention used throughout the geometry format. But fauxgl's `Texture.Sample` internally does `v = 1 - v`, following OpenGL's bottom-up convention.

So the V coordinate is pre-flipped when the vertex is built:

```go
Texture: fauxgl.Vector{
	X: uvCorners[i][0] / texW,
	Y: 1 - uvCorners[i][1]/texH,  // counteracts fauxgl's internal flip
}
```

Without it, every sample reads from the vertically mirrored texel. The symptom is not a cleanly upside-down render — it is a seemingly random pattern of transparent and opaque patches, because the skin atlas is dense and a mirrored read sometimes lands on plausible-looking pixels by coincidence.

Division by `texW`/`texH` converts texture pixels to normalized coordinates, which is where the entry's declared texture dimensions get used.

## Stage 4 — Framing the camera

The camera is **always** derived from the actual triangles, never from a hardcoded distance:

```go
min, max   := boundingBoxOf(triangles)
center     := (min + max) / 2
halfExtent := max((max.X-min.X)/2, (max.Y-min.Y)/2, (max.Z-min.Z)/2)
distance   := halfExtent / tan(fov/2) * margin
```

That is just the trigonometry of fitting a sphere of radius `halfExtent` into a cone of angle `fov`, with `margin` as breathing room. `margin` of 1.0 frames as tightly as possible without clipping.

The eye is then placed by yaw and pitch:

```go
offset = Vector{
	X:  distance * sin(yaw) * cos(pitch),
	Y:  distance * sin(pitch),
	Z: -distance * cos(yaw) * cos(pitch),
}
eye = center + offset
```

At `yaw=0, pitch=0` the camera sits on the **−Z** side looking toward +Z, with up = +Y. That baseline was settled empirically: decode a real skin's pixels, confirm the face is painted in the `north` UV region, render, and check which camera position shows it. Only −Z does.

Positive yaw swings the eye toward +X, bringing the model's left side into view. Positive pitch raises the eye to look down on top faces.

Why bounding-box framing rather than a fixed distance: a fixed distance/FOV pair was once confirmed, by computing actual clip-space coordinates, to push *every* vertex outside the frustum for a head wearing a helmet — the real extent simply did not match the assumed ~8-unit head. Any model with unusual proportions breaks a hardcoded camera. Measuring cannot.

## Stage 5 — Rasterizing

```go
dc := fauxgl.NewContext(size, size)
dc.Cull = fauxgl.CullNone
dc.ClearColorBufferWith(fauxgl.Transparent)

matrix := fauxgl.LookAt(eye, center, fauxgl.Vector{Y: 1}).
	Perspective(fovDegrees, 1.0, 1, 500)

dc.Shader = newAlphaTestTextureShader(matrix, fauxgl.NewImageTexture(texture))
for _, t := range triangles {
	dc.DrawTriangle(t)
}
```

Four details, each of which was a bug at some point:

**Clip space only — no `Viewport`.** The shader matrix must stop after `LookAt` and `Perspective`. fauxgl's `Context` applies the NDC→screen mapping itself, after the perspective divide, using its own internal screen matrix. Chaining `.Viewport(...)` here double-applies that transform *before* the divide and produces a completely blank render — zero non-transparent pixels — even though the mesh built correctly at sensible coordinates.

**`CullNone`.** Winding order is not guaranteed consistent across faces, so back-face culling would drop real geometry.

**Transparent clear.** Output is RGBA with a transparent background, so renders composite onto any page or canvas.

**`DrawTriangle`, singular, in a loop.** The plural `DrawTriangles` spawns `runtime.NumCPU()` workers and is faster for a single render, but its workers race on the depth buffer — which would trip the race detector in every downstream service — and it is slower under concurrent load. See [design-decisions.md](design-decisions.md#why-rasterization-is-single-threaded).

### Alpha testing

The shader is unlit — it samples the texture and returns the colour, matching Minecraft's flat skin rendering. There is no lighting model to get wrong.

What it does do is **discard**:

```go
c := s.Texture.Sample(v.Texture.X, v.Texture.Y)
if c.A < s.Threshold {  // 0.5
	return fauxgl.Discard
}
return c
```

Discard skips colour **and depth**. That is the whole point. The overlay layer (hat, jacket, sleeves, pants) consists of cubes sitting 0.25 units outside the base body, textured mostly transparent. If those transparent fragments were alpha-*blended* instead of discarded, they would still write depth and would occlude the body underneath — you would get a person-shaped hole. Alpha testing is what makes the second layer work at all.

The threshold of 0.5 mirrors the `alphaTest: 0.5` the reference browser renderer uses.

### Nearest-neighbour sampling

Sampling is nearest-neighbour, not bilinear, for two reasons. Minecraft's skin atlas packs UV regions **edge to edge with no padding**, and every one of these triangles has vertices sitting exactly on a region boundary — bilinear sampling there blends into the neighbouring region, which is frequently transparent. It also happens to be the correct aesthetic: Minecraft is pixelated on purpose.

### Capes

The cape is drawn as a second pass with its own texture, into the same depth buffer, so it occludes and is occluded correctly:

```go
if capeTexture != nil && len(capeTriangles) > 0 {
	dc.Shader = newAlphaTestTextureShader(matrix, fauxgl.NewImageTexture(capeTexture))
	for _, t := range capeTriangles {
		dc.DrawTriangle(t)
	}
}
```

The cape entry is its own self-contained mini-hierarchy (`body` → `waist` → `cape`), resolved purely from parent names within that entry, so it positions itself without reference to the body model.

## The 2D fallback

When the selected geometry has no cubes anywhere — a persona skin — there is nothing to tessellate. `Render2D` crops the standard vanilla box-UV regions straight out of the texture and composites a flat paper doll:

```
        ┌──────┐
        │ head │
   ┌────┼──────┼────┐
   │ L  │ body │ R  │
   │arm │      │arm │
   └────┼──┬───┼────┘
        │L │ R │
        │leg leg│
        └──┴───┘
```

It reuses `boxUVRects` to find the `north` (front) face of each standard part, so the crop coordinates come from the same code path as the 3D renderer rather than a second hardcoded table. Coordinates are expressed against a 64-wide texture and scaled proportionally, so 128×128 skins work unchanged.

Final scaling is nearest-neighbour, keeping the pixelated look.

This is exact for any skin using standard proportions, which is every texture-only skin worth worrying about — a skin with a genuinely custom shape always ships its geometry, and therefore takes the 3D path.
