# mcpe-skinapi documentation

Everything about how this library turns a Minecraft Bedrock skin into an image, why it is built the way it is, and how to build on top of it.

Start with whichever matches what you are doing:

| I want to… | Read |
| --- | --- |
| Call the library and get a picture | [api-reference.md](api-reference.md) |
| Solve a specific problem | [recipes.md](recipes.md) |
| Check a skin for invisible/invalid parts | [api-reference.md](api-reference.md#invisibility-detection) |
| Understand what a Bedrock client actually sends | [skin-data.md](skin-data.md) |
| Understand geometry.json | [geometry-format.md](geometry-format.md) |
| Understand how a mesh becomes pixels | [rendering-pipeline.md](rendering-pipeline.md) |
| Understand framing, bone scoping, cameras | [views-and-cameras.md](views-and-cameras.md) |
| Know *why* something is done a particular way | [design-decisions.md](design-decisions.md) |

## The short version

A Bedrock skin is two things: a **texture** (a flat PNG atlas) and a **model** (`geometry.json`, a tree of named bones holding axis-aligned cubes). Rendering means walking that bone tree, turning every cube into triangles with texture coordinates, pointing a camera at the result, and rasterizing it on the CPU.

The library has two halves:

- **Rendering** — texture + geometry in, `image.Image` out.
- **Invisibility detection** — the same inputs, but asking "is this skin invisible, or partly invisible?" Instead of pixels it answers with a structured report: which body parts are missing, whether the skin is suspicious, and per-part opaque fractions. See [api-reference.md](api-reference.md#invisibility-detection). The high-level `Skin` type bundles a texture and geometry and answers `IsInvisible`/`IsSuspicious`/`Parts()` in one call.

```
geometry.json ──▶ ParseGeometry ──▶ []Geometry
                                      │
                        SelectGeometry (pick one entry)
                                      │
                              boneWorldMatrices
                          (flatten the bone hierarchy)
                                      │
                                    addCube
                    (cubes ──▶ triangles, with UVs)
                                      │
                              cameraForYawPitch
                     (frame the actual bounding box)
                                      │
                                   rasterize
                     (fauxgl, alpha-tested, unlit)
                                      │
                                   image.Image
```

The one thing that surprises most people is covered in [skin-data.md](skin-data.md): **most real skins carry no geometry at all**, so the model has to come from somewhere else. That single fact shapes a lot of this library's API.

## Provenance

The behaviour described in these docs was established against real captured traffic from a Bedrock client, not from documentation. Where a doc says "confirmed against captures", it means exactly that. Where something remains unverified, it says so — see the bone-rotation note in [geometry-format.md](geometry-format.md).
