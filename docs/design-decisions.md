# Design decisions

Why the library works the way it does. Most of these are the residue of a specific bug or a specific surprise in real data — worth reading before changing the corresponding code, because several of them look arbitrary until you know what they prevent.

## Why geometry is optional

Requiring geometry would reject the most common skin in the game.

A Bedrock client sends **no mesh at all** for a skin using a built-in model. `SkinGeometryData` contains the literal JSON `null`, and the model is named only in the resource patch. Both ends already have the model, so it never travels the wire. Geometry appears only for genuinely custom meshes.

So `Options.Geometry: nil` is not a convenience shortcut — it is the correct input for most real skins, and the library treats it as a first-class case rather than an error path. See [skin-data.md](skin-data.md#most-skins-send-no-geometry-at-all).

## Why the default geometry is embedded

`default_geometry.json` was **captured from a real client**, not hand-authored, so it matches what the game actually draws. Hand-writing a humanoid model means hand-writing several dozen cube origins and UV offsets, and any transcription error shows up as a subtly wrong render that is tedious to diagnose.

It is parsed once at package init into a shared `[]Geometry`. Rendering never mutates geometry, so every caller falling back to the default shares the same entries rather than re-parsing 16KB of JSON per render. This is why `DefaultGeometry()` documents its result as read-only.

Parse failure panics rather than returning an error. The input is compiled into the binary, so a failure cannot be caused by anything a caller did — it means the library was built broken, which is a programming error, not a runtime condition.

## Why select by cube count

`SelectGeometry`'s fallback picks the entry with the **most cubes**, not the first entry. This fixed a real bug.

A bundled geometry file commonly lists the **cape first**. A real capture has `geometry.cape` (3 bones, 1 cube, no `head` bone at all) ahead of `geometry.humanoid.custom` (17 bones, 12 cubes). Falling back to `geos[0]` silently selected the cape whenever a caller omitted the identifier — which is the common case, since most callers do not know a bundle holds multiple entries. Every head-scoped view then matched zero bones and failed with "nothing to render", despite perfectly good geometry having been supplied.

Cube count is a general, format-agnostic heuristic: a cosmetic entry like a cape is reliably far sparser than the actual body, for any skin, with no need to hardcode `humanoid` or any other identifier.

The wide and slim variants tie at 12 cubes, and ties break toward the earlier entry, so the wide body wins by default. That is the right outcome, but it does depend on entry ordering — if you need a guarantee, pass an explicit `Identifier`.

## Why the resource patch beats ArmSize

Real captures show them disagreeing: a skin reporting `ArmSize: "wide"` whose resource patch names `geometry.humanoid.customSlim`.

The patch is what the client renders from, so the patch wins. `ArmSize` is at best a hint. This is documented in the `Options.Identifier` doc comment specifically because it is the kind of thing someone will otherwise rediscover the hard way.

## Why persona skins fall back to 2D

Persona (avatar-builder) skins arrive with real, named bones and **zero cubes in all of them**. Bedrock never sends mesh data for them.

There is genuinely nothing to rasterize — this is not a parse failure or a corrupt upload. Failing would mean a caller proxying real players sees errors for a large class of perfectly normal skins. So `Render` checks `TotalCubes() == 0` and falls back to a flat texture crop, which is a reasonable approximation of what the client shows.

Putting that check inside `Render` rather than leaving it to callers is deliberate: it is the single most likely thing for someone to omit, and omitting it produces confusing failures on real traffic. It is the main reason to prefer `Render` over driving the mesh functions directly.

## Why 2D cropping reuses boxUVRects

`Render2D` could hardcode its crop rectangles. It calls `boxUVRects` instead, the same function the 3D path uses, so the two cannot drift apart. Coordinates are expressed against a 64-wide texture and scaled by the actual width, so 128×128 skins work without a second table.

## Why the camera is computed from the bounding box

Never a hardcoded distance.

A fixed distance/FOV pair was once confirmed, by computing actual clip-space coordinates, to push **every single vertex** outside the frustum for a head wearing a helmet — the real extent did not match the assumed ~8-unit head size. Any model with unusual proportions breaks a hardcoded camera, and custom skins have unusual proportions by definition.

Framing from the actual triangle bounding box is scale-free: a skin with enormous wings frames correctly at the same margin as a vanilla body.

## Why yaw=0 sits on −Z

Settled empirically, not derived.

The process: decode a real skin's texture with a proper PNG decoder, confirm the face and eyes are painted in the `north` UV region, render through this exact pipeline, and check which camera position shows that region. Only the −Z side does.

Worth noting how that was nearly missed — an earlier hand-rolled PNG parser that skipped per-row filter bytes produced corrupted pixel data and pointed at the wrong conclusion. If you are verifying claims like this, use a real decoder.

## Why the V coordinate is pre-flipped

fauxgl's `Texture.Sample` internally computes `v = 1 - v`, following OpenGL's bottom-up convention. UV rectangles here are computed top-down, matching PNG row order and the geometry format. So the vertex builder pre-flips V to cancel fauxgl's flip.

The reason this deserves a comment in the code as well as a doc entry: the failure mode is **not** a cleanly upside-down image. It is a seemingly random pattern of transparent and opaque patches, because the atlas is dense enough that a mirrored read sometimes lands on plausible pixels. It looks like a UV *bounds* bug, not a flip.

## Why no Viewport in the shader matrix

The shader matrix stops after `LookAt` and `Perspective`. fauxgl's `Context` applies the NDC→screen mapping itself, after the perspective divide.

Chaining `.Viewport(...)` double-applies it *before* the divide, producing a fully blank render — confirmed as zero non-zero-alpha pixels — even though the mesh builds correctly at plausible coordinates. A blank image with a healthy-looking mesh is a confusing symptom; this is where to look.

## Why alpha test rather than alpha blend

Discard skips colour **and depth**, and the depth part is the point.

Minecraft's second skin layer (hat, jacket, sleeves, pants) is cubes inflated 0.25 units outside the base body, textured mostly transparent. Alpha-blended transparent fragments would still write depth and occlude the body underneath, producing a person-shaped hole. Discarding them leaves the depth buffer alone so the base layer shows through.

The 0.5 threshold mirrors the `alphaTest: 0.5` used by the reference browser renderer.

## Why nearest-neighbour sampling

Two independent reasons.

Correctness: Minecraft's atlas packs UV regions edge to edge with **no padding**, and every triangle here has vertices exactly on a region boundary. Bilinear sampling there blends into the neighbouring region, which is frequently transparent.

Aesthetics: Minecraft is pixelated on purpose, and bilinear filtering makes a skin render look wrong even where it is technically correct.

## Why CullNone

Winding order is not guaranteed consistent across generated faces, so back-face culling would drop real geometry. The cost of drawing back faces is negligible at a few hundred triangles.

## Why DrawTriangles rather than a loop

fauxgl's plural `DrawTriangles` spawns `runtime.NumCPU()` workers internally and stripes the triangle list across them. A loop over the singular form uses one core.

A skin is only a few hundred triangles, so this barely matters for one render in isolation. It matters under concurrent load: many in-flight renders each finish faster instead of each occupying one core for its full duration.

## Concurrency

Because a single render already parallelizes across all cores, "how many renders keep the CPU busy" is the wrong question — one already does. The right question is how many can run without starving each other of cache and scheduler time, and the answer is a small constant, not something scaled to `NumCPU`.

A service should bound concurrent renders with a semaphore. Something like 4 is a sensible starting point. The non-render parts of a request — decode, encode, I/O — are not parallelized, so some overlap still helps throughput.

## Why there are no built-in limits

The library enforces no size, dimension or complexity limits, because what counts as "too large" is **policy**, and policy depends on the caller. A service rendering its own trusted assets wants no limits at all; one accepting arbitrary uploads wants aggressive ones. Baking in a number would be wrong for one of them and unremovable for the other.

`Complexity` gives callers the number to make that decision with. [recipes.md](recipes.md#handling-untrusted-uploads) shows the checks a public service should apply — notably bounding image dimensions with `image.DecodeConfig` *before* a full decode, since a few-KB PNG can declare enormous dimensions and force a multi-gigabyte allocation.

## Why cycle detection exists

`boneWorldMatrices` and `isDescendant` both carry a `seen` set. A malformed geometry file can contain a parent cycle, and without the guard the recursive resolve would never return — a hang, not an error, triggered by untrusted input. A cycle resolves to identity instead.

## Why the library has no HTTP layer

An HTTP framework is a heavy, opinionated dependency, and pinning one on every importer to serve a handful of handlers is a poor trade. The library depends only on `fauxgl` and `golang.org/x/image`.

`ParseParts` is the one concession — it parses a comma-separated form value, which is HTTP-shaped — but it is 10 lines of `strings` work with no dependency, and having it here means every service built on the library parses parts lists identically.
