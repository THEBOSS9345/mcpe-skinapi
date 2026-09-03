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

## Why rasterization is single-threaded

`rasterize` calls fauxgl's singular `DrawTriangle` in a loop. The plural `DrawTriangles` spawns `runtime.NumCPU()` workers and stripes the triangle list across them, which is faster for one isolated render — and it is deliberately not used.

Two reasons, in order of importance.

**It races.** fauxgl's parallel path reads the depth buffer without holding the lock (`context.go:232`, carrying its author's own `// safe w/out lock?` comment) while another worker writes it under the lock. In practice the race looks benign — the values are aligned float64s and the authoritative depth test is repeated inside the lock — but it is a data race by the Go memory model, and `go test -race` reports it every time.

That is not merely a CI annoyance. A library that trips the race detector poisons the test suite of **every downstream service that renders a skin**. Someone running `go test -race` on their own server would get a race report pointing into this package, with no way to fix it from their side. Shipping that is not acceptable for a library.

**It is also slower where it counts.** Measured on a 28-thread machine:

| | Parallel | Serial |
| --- | --- | --- |
| One body render at 512 | 2.45 ms | 6.66 ms |
| One avatar render at 128 | 1.60 ms | 2.46 ms |
| Concurrent renders (`RunParallel`) | 881 µs/op | **761 µs/op** |

Parallel wins by 2.7× on a single isolated render and *loses* by 14% under concurrent load, because every in-flight render spawning `NumCPU` workers oversubscribes the machine badly. A server — the main thing this library gets embedded in — lives in the bottom row.

The cost is real but small in absolute terms: a few extra milliseconds on a one-off render, against a race-free library that is faster under load. `bench_test.go` holds the benchmarks if you want to re-measure.

## Concurrency

Each render occupies exactly one goroutine, so parallelism comes from running several renders at once rather than from splitting one. That makes the scaling story simple: throughput rises with concurrency up to the core count.

A service should still bound concurrent renders with a semaphore, to cap how much memory is in flight and to fail fast rather than queue without limit under a spike. Rendering is pure CPU work, so a bound near `GOMAXPROCS` is a reasonable starting point.

## Why there are no built-in limits

The library enforces no size, dimension or complexity limits, because what counts as "too large" is **policy**, and policy depends on the caller. A service rendering its own trusted assets wants no limits at all; one accepting arbitrary uploads wants aggressive ones. Baking in a number would be wrong for one of them and unremovable for the other.

`Complexity` gives callers the number to make that decision with. [recipes.md](recipes.md#handling-untrusted-uploads) shows the checks a public service should apply — notably bounding image dimensions with `image.DecodeConfig` *before* a full decode, since a few-KB PNG can declare enormous dimensions and force a multi-gigabyte allocation.

## Why cycle detection exists

`boneWorldMatrices` and `isDescendant` both carry a `seen` set. A malformed geometry file can contain a parent cycle, and without the guard the recursive resolve would never return — a hang, not an error, triggered by untrusted input. A cycle resolves to identity instead.

## Why the library has no HTTP layer

An HTTP framework is a heavy, opinionated dependency, and pinning one on every importer to serve a handful of handlers is a poor trade. The library depends only on `fauxgl` and `golang.org/x/image`.

`ParseParts` is the one concession — it parses a comma-separated form value, which is HTTP-shaped — but it is 10 lines of `strings` work with no dependency, and having it here means every service built on the library parses parts lists identically.

## Why malformed cubes are skipped

`Cube.Origin` and `Cube.Size` are `[]float64` because that is what the JSON gives, and nothing in the format guarantees three components. Bedrock's own geometry always supplies three; an arbitrary client's upload need not, and `c.Size[2]` on `{"size":[8]}` panics with an index-out-of-range that takes down the whole process — reachable from any login packet, in both the render path and the detector.

`cubeDims` bounds-checks once and every caller skips the cube. A skin with one bad cube still renders the rest, which matches the library's general stance that policy belongs to the caller: nothing here decides that a slightly malformed upload is worth rejecting outright. A bone made entirely of malformed cubes produces no triangles and surfaces as the ordinary "nothing to render" error, and measures 0 in the size check, which correctly reads as too small to see.

## Why persona detection tests parsed bones

A persona skin is geometry that parsed, has bones, and has no cubes on any of them. The middle condition is not decoration.

The check used to be "the caller passed some geometry bytes, and we found no cubes". `getGeometry` returns nil when the document fails to parse or yields no entries, so *unreadable* geometry satisfied that condition and took the persona branch, which returns trusted-visible without looking at a single pixel. A fully transparent skin came back `Pass=true` if you attached `{}` — or literally `not json at all` — to `SkinGeometryData`. The detector's entire purpose was one junk byte away from being switched off.

Geometry that cannot be read now falls through to the texture-only standard-layout check, which is exactly what sending no geometry does, and is the conservative reading: we could not confirm a custom mesh, so we check the texture against the model the client would otherwise use.

## Why bone size is a bounding box

`boneWorldSize` measures the largest axis of the box enclosing a bone's cubes. It used to sum each axis across cubes instead, which is not a size of anything — a hundred cubes of 0.05 units stacked in the same place summed to 5.0 and cleared `DefaultMinGeometrySize`, so the tiny check could be defeated by splitting one invisible cube into many.

For the single-cube bones that make up every ordinary skin the two agree exactly (`size + 2*inflate` on each axis, then the max), so only that bypass changes verdict.

## Why the cape is built before the camera

`cameraForYawPitch` frames on the triangles it is given. Building the cape after the camera meant framing on the body alone, and a cape hangs behind and below the body — a long one would have been pushed out of shot by its own subject's framing.

The cape is also skipped for `ViewHead` and `ViewAvatar`. It was previously built for every view regardless: a head crop put cape geometry into the scene that happened to land outside the frame, which was luck rather than design, and would have stopped being true the moment the head framing widened.

Finally, the cape entry is looked for in the caller's geometry — skipping the body entry already being drawn, since a custom model may define its own `cape` bone and rendering it twice leaves the two z-fighting — and then in `DefaultGeometry()`. Capes always travel in their own entry and are never merged into a body, so a skin with a custom mesh ships geometry with no cape bone in it — searching only the supplied geometry meant `Options.Cape` silently did nothing for exactly those skins, with no error to explain it.

## Why one detection run parses the geometry once

`validateWith` parses `geomData` a single time and hands the bone map to both the part scan and the size check. It used to pass raw bytes down instead, so the same document was unmarshalled three times per run: once in `scanParts`, once for the has-geometry gate, and once more inside `ValidateGeometrySize`.

That is 2.4x the wall time and roughly twice the allocation for a check a service runs on every login. The exported `ValidateGeometrySize` still takes bytes — it is a standalone entry point — and now parses once and delegates to `geometrySizeOf`.

## Why legacy entries are sorted

`ParseGeometry`'s modern branch returns entries in the order the `minecraft:geometry` array lists them. Its legacy branch cannot: a pre-1.12 document keys its entries off the top-level object, and ranging a Go map yields a different order every time.

That order is load-bearing. `SelectGeometry` falls back to the entry with the most cubes and breaks a tie by position — and vanilla's two arm variants have *identical* cube counts. So a legacy bundle carrying both wide and slim chose between them at random, per call: measured over 200 parses of the same bytes, the same skin selected `geometry.humanoid.custom` 179 times and `geometry.humanoid.customSlim` 21 times. Rendering it twice gave a player different arms.

Entries are sorted by identifier. JSON object keys carry no order to preserve, so there is no original ordering to be faithful to, and identifier order is the only stable choice available.

## Why the report carries a verdict, not three booleans

`SkinReport` used to expose `Pass`, `IsInvisible` and `IsSuspicious` side by side. Three independent booleans have eight combinations and the analysis produces three, so five of them mean nothing — invisible *and* suspicious at once, or passing *while* invisible. Nothing prevented a caller constructing one, and nothing prevented a future edit letting the fields drift apart.

`Verdict` is one value. `VerdictUnknown` is deliberately its zero value, so an uninitialised report fails closed: `OK()` returns false for it. That property used to hold by luck — `Pass` happened to be false when zeroed — rather than by design.

The same reasoning removed `Pass`, `InvisibleParts` (the count) and `PartReport.Visible` as stored fields. Each was exactly derivable from another field: `Pass` from the verdict, the count from `VisibleParts` and `TotalParts`, `Visible` from `Visibility` — which also carried strictly more information, since it distinguishes transparent from too-small. They are methods now. Each fact is stated in one place, so the JSON cannot contradict itself.

The struct is also JSON-tagged. It is documented as safe to hand to an API, and without tags `encoding/json` emitted Go identifiers — `"IsInvisible"` rather than `"is_invisible"`. Doing this later would have been a breaking change for every consumer; the package had none yet.
