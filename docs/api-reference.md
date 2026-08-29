# API reference

Every exported symbol in `github.com/THEBOSS9345/mcpe-skinapi`.

```go
import skinapi "github.com/THEBOSS9345/mcpe-skinapi"
```

---

## Rendering

### `func Render(opts Options) (image.Image, error)`

The entry point. Rasterizes a skin into a square image.

```go
img, err := skinapi.Render(skinapi.Options{Texture: tex})
```

With only a texture, that renders the full body of a standard humanoid, straight on, at 512×512.

`Render` handles the awkward cases so callers do not have to:

- `Geometry` nil or empty → falls back to `DefaultGeometry()`.
- Selected geometry has no cubes (a persona skin) → falls back to `Render2D` instead of failing.
- `Identifier` matching nothing → falls back to the entry with the most cubes.

Errors:

| Condition | Error |
| --- | --- |
| `Texture` is nil | `ErrNoTexture` |
| Geometry contains no usable entries | `"geometry has no usable entries"` |
| `Parts` matched no bones | `"no bones matched the requested parts"` |
| A view matched no cubes | `"nothing to render for this view"` |

### `type Options`

```go
type Options struct {
	Texture    image.Image
	Geometry   []Geometry
	Identifier string
	Cape       image.Image
	View       View
	Angle      Angle
	Parts      []string
	Camera     *Camera
	Size       int
}
```

| Field | Zero value behaviour |
| --- | --- |
| `Texture` | **Required.** Nil returns `ErrNoTexture`. |
| `Geometry` | Uses `DefaultGeometry()`. This is correct for most real skins — see [skin-data.md](skin-data.md). |
| `Identifier` | Picks the entry with the most cubes. |
| `Cape` | No cape. Requires geometry with a `cape` bone that has a cube. |
| `View` | `ViewBody`. Ignored when `Parts` is set. |
| `Angle` | `defaultAngleFor(View)` — iso for head, front otherwise. Ignored when `Camera` is set. |
| `Parts` | Uses `View` instead. |
| `Camera` | Uses `View`/`Angle` presets. |
| `Size` | `DefaultSize` (512). Output is always square. |

### `type Camera`

```go
type Camera struct {
	Yaw    float64 // degrees; positive swings toward the model's left
	Pitch  float64 // degrees; positive looks down
	FOV    float64 // degrees; 0 keeps the view's preset
	Margin float64 // 1.0 is tightest; 0 keeps the view's preset
}
```

See [views-and-cameras.md](views-and-cameras.md#explicit-cameras).

### `const DefaultSize = 512`

Used when `Options.Size` is zero.

### `var ErrNoTexture`

Returned by `Render` when `Options.Texture` is nil.

### `func Render2D(texture image.Image, view View, size int) image.Image`

Flat "paper doll" render, cropped from the texture's standard box-UV regions. No geometry involved.

`Render` calls this automatically for persona skins, so you rarely need it directly. Reach for it when you explicitly want the flat look, or when you have a texture and know it uses standard proportions.

Honours `ViewHead`/`ViewAvatar` (head only), `ViewChest` (no legs) and `ViewBody` (everything).

---

## Bytes in, bytes out

For callers holding encoded file or wire bytes who want PNG bytes back, without touching `image.Decode` or `png.Encode`. Same renderer, same behaviour — decode and encode folded in.

### `func RenderBytes(opts BytesOptions) ([]byte, error)`

```go
out, err := skinapi.RenderBytes(skinapi.BytesOptions{
	Texture: textureBytes,   // encoded PNG or JPEG
	Geometry: geometryBytes, // raw geometry.json; nil or "null" is fine
	View:    skinapi.ViewAvatar,
	Size:    128,
})
```

Returns encoded PNG bytes. Identical output to the `image.Image` path — there is a test asserting the two are byte-for-byte equal.

`Geometry` takes the raw `geometry.json` bytes and applies `IsEmpty` internally, so a stock skin's field, including the literal `null`, passes straight through to the default model. Malformed geometry is still an error.

### `type BytesOptions`

```go
type BytesOptions struct {
	Texture    []byte // encoded PNG/JPEG. Required.
	Geometry   []byte // raw geometry.json. Nil, empty or "null" uses the default.
	Cape       []byte // encoded PNG/JPEG.
	Identifier string
	View       View
	Angle      Angle
	Parts      []string
	Camera     *Camera
	Size       int
}
```

Every field behaves exactly as its `Options` counterpart.

### Method forms

Both option types render themselves, if you prefer building and rendering in one expression:

```go
img, err := skinapi.Options{Texture: tex}.Render()          // image.Image
raw, err := skinapi.Options{Texture: tex}.RenderPNG()       // []byte
raw, err := skinapi.BytesOptions{Texture: b}.RenderPNG()    // []byte
```

`Options.RenderPNG` is the useful hybrid: decoded image in, PNG bytes out.

### `func TextureFromRGBA(pix []byte, width, height int) (image.Image, error)`

Wraps raw non-premultiplied RGBA pixel data as an image. **This is the form Bedrock actually sends skins in** — `SkinData` decodes to `width*height*4` bytes with no header, dimensions arriving separately.

```go
raw, err := base64.StdEncoding.DecodeString(data.SkinData)
if err != nil {
	return err
}
tex, err := skinapi.TextureFromRGBA(raw, data.SkinImageWidth, data.SkinImageHeight)
```

The slice backs the image directly rather than being copied, so do not modify it afterwards. A length disagreeing with the dimensions is an error rather than a garbled image.

Note that `BytesOptions.Texture` expects an *encoded* image, so wire data goes through this function and the `Options` path instead.

### `func DecodeImage(data []byte) (image.Image, error)`

Decodes PNG or JPEG. Applies **no size limit** — check `image.DecodeConfig` first for untrusted input, since it reads only the header. See [recipes.md](recipes.md#handling-untrusted-uploads).

### `func EncodePNG(img image.Image) ([]byte, error)`

Encodes an image as PNG bytes.

---

## Views and angles

### `type View string`

```go
const (
	ViewBody   View = "body"   // full figure
	ViewChest  View = "chest"  // waist-up, arms included
	ViewHead   View = "head"   // head and everything parented under it
	ViewAvatar View = "avatar" // head, tighter framing
)
```

### `type Angle string`

```go
const (
	AngleFront Angle = "front" // straight-on
	AngleIso   Angle = "iso"   // 35° yaw, 25° pitch
)
```

### `func ParseParts(raw string) []string`

Splits a comma-separated bone list, trimming whitespace and dropping empties.

```go
skinapi.ParseParts(" head , leftArm ,, rightArm ") // ["head" "leftArm" "rightArm"]
```

Returns nil for blank input, which `Options.Parts` reads as "everything".

---

## Geometry

### `func ParseGeometry(raw []byte) ([]Geometry, error)`

Parses a `geometry.json` into normalized entries, accepting both Bedrock formats — the modern `minecraft:geometry` array and the pre-1.12 flat top-level-key form — and detecting which it has.

Returns **zero entries with no error** for input that is valid JSON but carries no geometry, including the literal `null` a Bedrock client sends for a built-in model. An error means genuinely malformed JSON.

```go
geos, err := skinapi.ParseGeometry(raw)
if err != nil {
	return err // malformed
}
// len(geos) == 0 is fine: pass nil to Render and get the default model.
```

### `func DefaultGeometry() []Geometry`

The vanilla humanoid bundle, captured from a real Bedrock client: `geometry.humanoid.custom` (wide), `geometry.humanoid.customSlim` (slim), and `geometry.cape`.

Parsed once at package init. **The returned slice is shared and must not be modified.**

### `func SelectGeometry(geos []Geometry, identifier string) (Geometry, bool)`

Picks one entry. Matches `identifier` exactly if given; otherwise, or if nothing matches, returns the entry with the **most cubes**.

Returns `false` only when `geos` is empty.

### `func FindCape(geos []Geometry) (Geometry, bool)`

Finds the entry containing a bone literally named `cape` that has at least one cube. Capes always live in their own entry, never merged into the body.

### `func IsEmpty(raw []byte) bool`

Reports whether raw geometry carries no mesh at all — empty, whitespace, or the literal `null` (with or without a trailing newline). Use it to separate "this skin has no custom model" from "this upload is broken" before parsing.

```go
if skinapi.IsEmpty(raw) {
	geos = nil // built-in model; Render will use the default
} else if geos, err = skinapi.ParseGeometry(raw); err != nil {
	return err
}
```

### `func Complexity(geos []Geometry) (bones, cubes int)`

Total bones and cubes across **every** entry, not just the one that will be rendered — `SelectGeometry`'s fallback and `FindCape` both walk the whole document, so the total is what bounds worst-case cost.

The library enforces no limit itself. See [design-decisions.md](design-decisions.md#why-there-are-no-built-in-limits).

```go
if bones, cubes := skinapi.Complexity(geos); bones > 2000 || cubes > 5000 {
	return errors.New("geometry too complex")
}
```

---

## Types

### `type Geometry`

```go
type Geometry struct {
	Identifier    string
	TextureWidth  float64
	TextureHeight float64
	Bones         []Bone
}
```

One normalized model, regardless of which wire format it came from.

**`func (g *Geometry) BoneByName(name string) (Bone, bool)`** — look up a bone.

**`func (g *Geometry) TotalCubes() int`** — cubes across every bone. **Zero means a persona skin**: real bones, no mesh.

### `type Bone`

```go
type Bone struct {
	Name     string
	Parent   string
	Pivot    []float64
	Rotation []float64
	Inflate  float64
	Cubes    []Cube
}
```

### `type Cube`

```go
type Cube struct {
	Origin  []float64
	Size    []float64
	UV      json.RawMessage
	Inflate *float64
	Mirror  bool
}
```

`UV` stays raw because Bedrock allows two shapes — a `[u,v]` pair or a per-face object — resolved at mesh-build time. `Inflate` is a pointer so an explicit `0` can be told apart from absent, which matters since absent means "inherit the bone's value".

See [geometry-format.md](geometry-format.md) for what these fields mean.
