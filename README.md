# mcpe-skinapi

Render Minecraft Bedrock skins to images, in pure Go.

Texture in, `image.Image` out. No GPU, no headless browser, no external process — a small software rasterizer built on [fauxgl](https://github.com/fogleman/fauxgl).

```bash
go get github.com/THEBOSS9345/mcpe-skinapi
```

## Quick start

```go
package main

import (
	"image/png"
	"os"

	skinapi "github.com/THEBOSS9345/mcpe-skinapi"
)

func main() {
	f, err := os.Open("skin.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	tex, err := png.Decode(f)
	if err != nil {
		panic(err)
	}

	img, err := skinapi.Render(skinapi.Options{Texture: tex})
	if err != nil {
		panic(err)
	}

	out, err := os.Create("body.png")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	png.Encode(out, img)
}
```

That renders the full body of a standard humanoid, straight on, at 512×512.

### Or work in bytes

If you already hold encoded bytes and want encoded bytes back, skip the decode and encode:

```go
out, err := skinapi.RenderBytes(skinapi.BytesOptions{
	Texture:  textureBytes,  // encoded PNG or JPEG
	Geometry: geometryBytes, // raw geometry.json; nil or "null" is fine
	View:     skinapi.ViewAvatar,
	Size:     128,
})
```

Both paths run the same renderer and produce identical output — use whichever fits. Options render themselves too, if you prefer:

```go
img, err := skinapi.Options{Texture: tex}.Render()     // image.Image
raw, err := skinapi.Options{Texture: tex}.RenderPNG()  // []byte
```

Skins coming off the wire arrive as raw RGBA rather than an encoded image; `TextureFromRGBA` wraps those without copying.

## Geometry is optional, and that matters

Passing no geometry is not a shortcut — for most real skins it is the correct input.

A Bedrock client sends **no mesh at all** for a skin that uses one of the built-in models. Its login packet carries the literal JSON `null` in `SkinGeometryData`, and names the model only in the skin's resource patch:

```json
{ "geometry": { "default": "geometry.humanoid.custom" } }
```

Both ends already have the model, so it never travels the wire. Geometry only shows up for skins with a genuinely custom mesh. If you are proxying real players, you will very often have nothing to pass — so `Options.Geometry: nil` falls back to the vanilla humanoid bundle, which is what the client itself would draw.

When a skin *does* carry geometry, parse and pass it:

```go
geos, err := skinapi.ParseGeometry(raw)
if err != nil {
	return err
}

img, err := skinapi.Render(skinapi.Options{
	Texture:    tex,
	Geometry:   geos,
	Identifier: "geometry.humanoid.customSlim",
	View:       skinapi.ViewAvatar,
	Angle:      skinapi.AngleIso,
	Size:       256,
})
```

`ParseGeometry` accepts both of Bedrock's on-the-wire formats — the modern `minecraft:geometry` array and the pre-1.12 flat top-level-key form — and detects which is which.

To tell "this skin has no custom mesh" apart from "this upload is broken" before parsing, use `IsEmpty`. It reports true for empty input and for that literal `null` (with or without a trailing newline, which real captures show both of).

### Picking wide vs slim

The bundled default carries three entries: `geometry.humanoid.custom` (wide arms), `geometry.humanoid.customSlim` (slim arms), and `geometry.cape`. Set `Identifier` to choose; omit it and the wide body wins.

Take that identifier from the skin's **resource patch**, not from the login packet's `ArmSize` field. Real captures show the two disagreeing — a skin reporting `ArmSize: "wide"` whose patch names `customSlim`. The patch is authoritative.

## Options

| Field | Meaning |
| --- | --- |
| `Texture` | Decoded skin image. The only required field. |
| `Geometry` | From `ParseGeometry`. Nil uses `DefaultGeometry()`. |
| `Identifier` | Which entry to render. Empty picks the one with the most cubes. |
| `Cape` | Cape texture. Renders only if the geometry defines a `cape` bone with a cube. |
| `View` | `ViewBody`, `ViewChest`, `ViewHead`, `ViewAvatar`. Zero means `ViewBody`. |
| `Angle` | `AngleFront` or `AngleIso`. Zero means the view's own default. |
| `Parts` | Explicit bone names, e.g. `[]string{"head", "leftArm"}`. Overrides `View`. |
| `Camera` | Explicit yaw/pitch/FOV/margin. Overrides `Angle`. |
| `Size` | Output edge length. Zero means 512. Always square. |

Bone scoping is ancestry-based, so naming `head` also pulls in whatever is parented under it — a hat, hair, ears, a party hat. Custom-geometry skins work with no special-casing and no hardcoded bone list.

## Persona skins

Persona (avatar-builder) skins have real bones but no cubes at all, because Bedrock never sends mesh data for them. There is genuinely nothing to rasterize, so `Render` detects this and falls back to a flat crop of the texture rather than returning an error — matching what the client shows. `Render2D` exposes that path directly.

## Untrusted input

The library enforces no limits of its own, because what counts as too large is policy, not physics. If you accept arbitrary uploads:

- Bound the geometry document with `Complexity`, which returns total bones and cubes across every entry, before rendering.
- Bound image dimensions with `image.DecodeConfig` before a full decode. A few-KB PNG can declare enormous dimensions and force a huge allocation.
- Bound concurrency. `Render` is pure CPU work and already parallelizes a single render across cores, so unbounded concurrent renders mostly contend rather than scale.

## Documentation

The [`docs/`](docs/) directory goes well beyond this README:

| | |
| --- | --- |
| [skin-data.md](docs/skin-data.md) | What a Bedrock client actually sends over the wire |
| [geometry-format.md](docs/geometry-format.md) | The geometry.json format, both versions |
| [rendering-pipeline.md](docs/rendering-pipeline.md) | How a bone tree becomes pixels, stage by stage |
| [views-and-cameras.md](docs/views-and-cameras.md) | Bone scoping, framing and camera math |
| [api-reference.md](docs/api-reference.md) | Every exported symbol |
| [recipes.md](docs/recipes.md) | Worked examples |
| [design-decisions.md](docs/design-decisions.md) | Why the library works the way it does |

## Contributing

Pull requests welcome.

**Using AI to write your contribution is completely fine** — no objection here at all. One ask: **point it at [`docs/`](docs/) before it writes anything.** That directory exists precisely so a newcomer, human or model, can understand what this project is and how it fits together without guessing.

[docs/rendering-pipeline.md](docs/rendering-pipeline.md) and [docs/design-decisions.md](docs/design-decisions.md) are the two that matter most. A lot of this code looks arbitrary until you know what it prevents — the texture coordinate flip, the missing `Viewport` call, alpha testing instead of blending, selecting geometry by cube count. Each of those is a real bug that was already fixed once, and each is easy to "clean up" straight back into existence. The docs explain the failure mode for every one.

[AGENTS.md](AGENTS.md) carries the same instructions in the form coding agents pick up automatically.

Practical notes:

- Run `gofmt -l .`, `go vet ./...` and `go test ./...` before opening a PR. CI runs all three.
- Comments in the code stay brief and point into `docs/`. Put long explanations in the docs rather than expanding the comments back out.
- If you change rendering behaviour, say what you verified it against. Much of this was established from real captured traffic, not from documentation, and "it looks right" has been wrong before.

## Notes

The bundled `default_geometry.json` is the vanilla humanoid model, captured from a real Bedrock client rather than hand-authored, so it matches what the game draws. It is Mojang's model data, included here for interoperability.

## License

[The Unlicense](LICENSE) — public domain.
