# Recipes

Working snippets for the things people actually build with this.

## Render a skin file to a PNG

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

	if err := png.Encode(out, img); err != nil {
		panic(err)
	}
}
```

## Bytes in, bytes out

If you already hold file bytes — from an upload, a database, a cache — skip the decode/encode entirely:

```go
out, err := skinapi.RenderBytes(skinapi.BytesOptions{
	Texture:  textureBytes,  // encoded PNG or JPEG
	Geometry: geometryBytes, // raw geometry.json; nil or "null" is fine
	View:     skinapi.ViewAvatar,
	Size:     128,
})
if err != nil {
	return err
}
// out is PNG bytes, ready to write or serve.
```

`Geometry` here takes the raw `geometry.json` and applies `IsEmpty` internally, so a stock skin's field — including the literal `null` — passes straight through to the default model. Malformed geometry is still an error.

The two paths produce identical output, so mix them freely. If you have a decoded image but want bytes back:

```go
raw, err := skinapi.Options{Texture: tex, View: skinapi.ViewHead}.RenderPNG()
```

## A profile-picture avatar

Tight head crop at the classic angle. This is the most common use.

```go
img, err := skinapi.Render(skinapi.Options{
	Texture: tex,
	View:    skinapi.ViewAvatar,
	Angle:   skinapi.AngleIso,
	Size:    128,
})
```

The output has a transparent background, so it composites onto any page.

## Render whatever a player is wearing, from a proxy

The realistic case: a skin off the wire, where geometry is usually absent and the model is named by the resource patch. See [skin-data.md](skin-data.md) for the field details.

```go
func renderPlayer(data login.ClientData) (image.Image, error) {
	// Texture: raw RGBA, not a PNG. TextureFromRGBA wraps it without copying
	// and checks the length against the declared dimensions.
	raw, err := base64.StdEncoding.DecodeString(data.SkinData)
	if err != nil {
		return nil, err
	}
	tex, err := skinapi.TextureFromRGBA(raw, data.SkinImageWidth, data.SkinImageHeight)
	if err != nil {
		return nil, err
	}

	// Geometry: usually the literal "null".
	var geos []skinapi.Geometry
	if geomRaw, err := base64.StdEncoding.DecodeString(data.SkinGeometry); err == nil {
		if !skinapi.IsEmpty(geomRaw) {
			if geos, err = skinapi.ParseGeometry(geomRaw); err != nil {
				return nil, fmt.Errorf("geometry: %w", err)
			}
		}
	}

	// The resource patch names the model — this is the authoritative
	// wide-vs-slim selector, not data.ArmSize.
	var identifier string
	if patchRaw, err := base64.StdEncoding.DecodeString(data.SkinResourcePatch); err == nil {
		var patch struct {
			Geometry struct {
				Default string `json:"default"`
			} `json:"geometry"`
		}
		if json.Unmarshal(patchRaw, &patch) == nil {
			identifier = patch.Geometry.Default
		}
	}

	return skinapi.Render(skinapi.Options{
		Texture:    tex,       // required
		Geometry:   geos,      // nil is normal and correct
		Identifier: identifier, // "" is fine
		View:       skinapi.ViewBody,
	})
}
```

Every optional piece degrades to a sensible default, so partial data still renders.

## Add a cape

A cape needs both a texture and geometry defining a `cape` bone with a cube. The bundled default includes `geometry.cape`, so capes work even with no geometry supplied.

```go
img, err := skinapi.Render(skinapi.Options{
	Texture: tex,
	Cape:    capeTex,
	View:    skinapi.ViewBody,
})
```

To check whether a cape will actually render before committing:

```go
geos := opts.Geometry
if len(geos) == 0 {
	geos = skinapi.DefaultGeometry()
}
if _, ok := skinapi.FindCape(geos); !ok {
	// This skin's geometry has no cape bone; the cape texture is ignored.
}
```

## Turntable frames

Because framing is derived from the bounding box, every frame stays the same size.

```go
const frames = 36

for i := 0; i < frames; i++ {
	img, err := skinapi.Render(skinapi.Options{
		Texture: tex,
		Camera:  &skinapi.Camera{Yaw: float64(i) * (360.0 / frames), Pitch: 10},
		Size:    256,
	})
	if err != nil {
		return err
	}
	// ... encode frame i ...
}
```

## Just the head and one arm

```go
img, err := skinapi.Render(skinapi.Options{
	Texture: tex,
	Parts:   []string{"head", "rightArm"},
	Camera:  &skinapi.Camera{Yaw: 25, Pitch: 10},
})
```

Each name pulls in its descendants, so `head` also brings the hat and any custom bones parented to it.

Taking that list from a request or a flag:

```go
parts := skinapi.ParseParts(r.FormValue("parts")) // "head, rightArm"
```

## Handling untrusted uploads

The library sets no limits — that is policy, and policy is yours. A public service wants all of these.

```go
const (
	maxGeometryBytes = 2 << 20 // 2MB; real ones are tens of KB
	maxBones         = 2000
	maxCubes         = 5000
	maxDimension     = 4096
	maxSize          = 2048
)

// 1. Bound the geometry document before parsing.
if len(geomBytes) > maxGeometryBytes {
	return errTooLarge
}

geos, err := skinapi.ParseGeometry(geomBytes)
if err != nil {
	return err
}

// 2. Bound mesh-building cost. Complexity sums across every entry, which is
//    what actually bounds worst-case work.
if bones, cubes := skinapi.Complexity(geos); bones > maxBones || cubes > maxCubes {
	return errTooComplex
}

// 3. Bound image dimensions BEFORE decoding. DecodeConfig reads only the
//    header — this is the actual decompression-bomb defense. A few-KB PNG
//    can declare enormous dimensions and force a huge allocation.
cfg, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
if err != nil {
	return err
}
if cfg.Width > maxDimension || cfg.Height > maxDimension {
	return errTooBig
}
if cfg.Width <= 0 || cfg.Height <= 0 {
	return errBadImage
}

tex, _, err := image.Decode(bytes.NewReader(imgBytes))
if err != nil {
	return err
}

// 4. Bound the output size — cost is quadratic in it.
if size > maxSize {
	size = maxSize
}
```

### Bound concurrency too

Each render occupies one goroutine, so throughput comes from running several at once. Bound that anyway, to cap memory in flight and fail fast under a spike rather than queueing without limit:

```go
var renderSlots = make(chan struct{}, 4)

func render(opts skinapi.Options) (image.Image, error) {
	select {
	case renderSlots <- struct{}{}:
		defer func() { <-renderSlots }()
	case <-time.After(10 * time.Second):
		return nil, errAtCapacity // fail fast rather than queue forever
	}
	return skinapi.Render(opts)
}
```

## An HTTP endpoint

The library ships no HTTP layer on purpose ([why](design-decisions.md#why-the-library-has-no-http-layer)). A minimal handler with `net/http`:

```go
func handleRender(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("texture")
	if err != nil {
		http.Error(w, "texture is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tex, _, err := image.Decode(file) // add the dimension checks above
	if err != nil {
		http.Error(w, "bad texture", http.StatusBadRequest)
		return
	}

	var geos []skinapi.Geometry
	if gf, _, err := r.FormFile("geometry"); err == nil {
		defer gf.Close()
		raw, err := io.ReadAll(gf)
		if err == nil && !skinapi.IsEmpty(raw) {
			if geos, err = skinapi.ParseGeometry(raw); err != nil {
				http.Error(w, "bad geometry", http.StatusBadRequest)
				return
			}
		}
	}

	img, err := skinapi.Render(skinapi.Options{
		Texture:    tex,
		Geometry:   geos,
		Identifier: r.FormValue("identifier"),
		View:       skinapi.View(r.FormValue("view")),
		Angle:      skinapi.Angle(r.FormValue("angle")),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}
```

Note that an empty or unrecognised `view`/`angle` is not an error — both fall back to their defaults, so a missing form field behaves sensibly.

## Inspecting a model

```go
geos, err := skinapi.ParseGeometry(raw)
if err != nil {
	return err
}

for _, g := range geos {
	fmt.Printf("%s — %d bones, %d cubes, texture %vx%v\n",
		g.Identifier, len(g.Bones), g.TotalCubes(), g.TextureWidth, g.TextureHeight)

	for _, b := range g.Bones {
		fmt.Printf("    %-12s parent=%-10s cubes=%d\n", b.Name, b.Parent, len(b.Cubes))
	}
}
```

Useful for finding the extra bones on a custom skin so you can name them in `Parts`.
