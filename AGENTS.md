# Notes for coding agents

Contributions written with AI assistance are welcome here. This file is the orientation the maintainer asks you to read first.

## Read the docs before writing code

Start with [`docs/README.md`](docs/README.md), then whichever of these the task touches:

| File | When it matters |
| --- | --- |
| [docs/skin-data.md](docs/skin-data.md) | Anything about how skins arrive over the wire |
| [docs/geometry-format.md](docs/geometry-format.md) | Anything parsing or interpreting geometry.json |
| [docs/rendering-pipeline.md](docs/rendering-pipeline.md) | Anything in `mesh.go`, `render.go`, `shader.go` |
| [docs/views-and-cameras.md](docs/views-and-cameras.md) | Bone scoping, framing, cameras |
| [docs/api-reference.md](docs/api-reference.md) | The exported surface |
| [docs/design-decisions.md](docs/design-decisions.md) | **Before changing anything that looks wrong** |

## What this project is

A pure Go library that renders Minecraft Bedrock skins to images. Texture in, `image.Image` or PNG bytes out. Software rasterizer built on `fauxgl` — no GPU, no browser, no external process.

It is a **library**, not a service. It has no HTTP layer and depends only on `fauxgl` and `golang.org/x/image`. Do not add a web framework.

## Structure

```
doc.go              package documentation
geometry.go         parsing geometry.json, both wire formats
defaults.go         the embedded vanilla humanoid model
mesh.go             bone matrices, cubes -> triangles, UV resolution
render.go           bone scoping, camera, rasterization internals
render_options.go   Options and Render, the public entry point
render2d.go         flat fallback for persona skins
shader.go           the unlit alpha-tested shader
bytes.go            byte-oriented API and image helpers
```

## Code that looks wrong but is not

Several things here are deliberate and were each a real bug once. Every one is documented in [docs/design-decisions.md](docs/design-decisions.md) with its failure mode. Do not "clean these up" without reading that page:

- **The V coordinate is pre-flipped** in `mesh.go`. It cancels fauxgl's internal `v = 1 - v`. Removing it does not produce an upside-down image — it produces a random-looking transparent/opaque mess.
- **The shader matrix stops after `Perspective`.** Adding `.Viewport(...)` renders a completely blank image.
- **The shader discards rather than blends.** Blending would make the overlay layer occlude the body.
- **Sampling is nearest-neighbour.** Atlas regions have no padding; bilinear bleeds into neighbours.
- **`SelectGeometry` falls back to the most cubes, not the first entry.** Bundles list the cape first.
- **The camera is computed from the bounding box.** A hardcoded distance breaks on any unusual model.
- **`CullNone` is intentional.** Winding order is not guaranteed consistent.
- **Rasterization uses the singular `DrawTriangle` in a loop.** The plural form is faster for one render but races on fauxgl's depth buffer, which would trip the race detector in every downstream service.

## Conventions

- Comments stay brief and point into `docs/`. If an explanation runs long, it belongs in a doc file, not an expanded comment.
- Keep the exported surface small and documented. Every exported symbol has a godoc comment.
- The library enforces no size, dimension or complexity limits. That is policy and belongs to the caller. Do not add ceilings.
- Tests use a procedural texture, never a real skin — no third-party artwork in the repo.

## Before opening a PR

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

CI runs all three. `gofmt -l .` must print nothing.

`-race` matters here more than usual: it is what caught the reason rasterization is single-threaded. It needs cgo and a C compiler, so on Windows you may hit `-race requires cgo` or `C compiler "gcc" not found` — set `CGO_ENABLED=1` and put a gcc on `PATH`.

To run the CI workflow itself locally, with [act](https://github.com/nektos/act) and Docker running:

```bash
act push -W .github/workflows/ci.yml -P ubuntu-latest=catthehacker/ubuntu:act-latest
```

If you change rendering behaviour, state what you verified it against. Much of this library's behaviour was established from real captured Bedrock traffic rather than documentation, and "it looks right" has been wrong here before.
