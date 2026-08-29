package skinapi

import (
	"image"
	"image/color"
	"testing"
)

// testTexture builds a synthetic 64x64 skin texture. It is deliberately not a
// real skin: the tests here check geometry handling and framing, and a
// procedural texture keeps the repository free of anyone else's artwork.
func testTexture() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	return img
}

func TestDefaultGeometryEntries(t *testing.T) {
	geos := DefaultGeometry()

	want := map[string]bool{
		"geometry.cape":                false,
		"geometry.humanoid.custom":     false,
		"geometry.humanoid.customSlim": false,
	}
	for _, g := range geos {
		if _, ok := want[g.Identifier]; !ok {
			t.Errorf("unexpected entry %q", g.Identifier)
			continue
		}
		want[g.Identifier] = true
	}
	for id, found := range want {
		if !found {
			t.Errorf("default geometry is missing %q", id)
		}
	}
}

// The wide and slim variants must differ in exactly the way Bedrock's do:
// three-wide arms instead of four. If they ever came out identical, the
// Identifier option would silently stop meaning anything.
func TestDefaultGeometryWideVersusSlim(t *testing.T) {
	armWidth := func(identifier string) float64 {
		t.Helper()
		geo, ok := SelectGeometry(DefaultGeometry(), identifier)
		if !ok {
			t.Fatalf("no entry %q", identifier)
		}
		bone, ok := geo.BoneByName("leftArm")
		if !ok || len(bone.Cubes) == 0 {
			t.Fatalf("%q has no leftArm cube", identifier)
		}
		return bone.Cubes[0].Size[0]
	}

	if got := armWidth("geometry.humanoid.custom"); got != 4 {
		t.Errorf("wide arm width = %v, want 4", got)
	}
	if got := armWidth("geometry.humanoid.customSlim"); got != 3 {
		t.Errorf("slim arm width = %v, want 3", got)
	}
}

// With no identifier the body must win over the cape. The cape entry is
// listed first in a real bundle, so falling back to the first entry rather
// than the richest one would select it and leave every head-scoped view
// empty.
func TestSelectGeometryPrefersBodyOverCape(t *testing.T) {
	geo, ok := SelectGeometry(DefaultGeometry(), "")
	if !ok {
		t.Fatal("SelectGeometry found nothing")
	}
	if geo.Identifier != "geometry.humanoid.custom" {
		t.Errorf("fallback selected %q, want geometry.humanoid.custom", geo.Identifier)
	}
}

func TestSelectGeometryByIdentifier(t *testing.T) {
	geo, ok := SelectGeometry(DefaultGeometry(), "geometry.cape")
	if !ok {
		t.Fatal("SelectGeometry found nothing")
	}
	if geo.Identifier != "geometry.cape" {
		t.Errorf("selected %q, want geometry.cape", geo.Identifier)
	}
}

// A client with no custom model sends the literal JSON null, sometimes with
// a trailing newline. Both mean "no mesh", not "broken upload".
func TestIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", true},
		{"whitespace", "  \n\t ", true},
		{"null", "null", true},
		{"null with newline", "null\n", true},
		{"real geometry", `{"format_version":"1.12.0"}`, false},
		{"malformed", "{ nope", false},
	} {
		if got := IsEmpty([]byte(tc.in)); got != tc.want {
			t.Errorf("%s: IsEmpty(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

// Bedrock sends geometry in two shapes and both must normalize identically.
func TestParseGeometryBothFormats(t *testing.T) {
	modern := `{
		"format_version": "1.12.0",
		"minecraft:geometry": [{
			"description": {"identifier": "geometry.test", "texture_width": 64, "texture_height": 64},
			"bones": [{"name": "head", "cubes": [{"origin": [-4,24,-4], "size": [8,8,8], "uv": [0,0]}]}]
		}]
	}`
	legacy := `{
		"format_version": "1.8.0",
		"geometry.test": {
			"texturewidth": 64, "textureheight": 64,
			"bones": [{"name": "head", "cubes": [{"origin": [-4,24,-4], "size": [8,8,8], "uv": [0,0]}]}]
		}
	}`

	for name, raw := range map[string]string{"modern": modern, "legacy": legacy} {
		geos, err := ParseGeometry([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(geos) != 1 {
			t.Fatalf("%s: got %d entries, want 1", name, len(geos))
		}
		g := geos[0]
		if g.Identifier != "geometry.test" {
			t.Errorf("%s: identifier = %q", name, g.Identifier)
		}
		if g.TextureWidth != 64 || g.TextureHeight != 64 {
			t.Errorf("%s: texture = %vx%v, want 64x64", name, g.TextureWidth, g.TextureHeight)
		}
		if g.TotalCubes() != 1 {
			t.Errorf("%s: %d cubes, want 1", name, g.TotalCubes())
		}
	}
}

// "null" parses as valid JSON carrying no entries. It must not be an error,
// so callers can hand a stock skin's field straight through.
func TestParseGeometryNullYieldsNoEntries(t *testing.T) {
	geos, err := ParseGeometry([]byte("null"))
	if err != nil {
		t.Fatalf("ParseGeometry(null) errored: %v", err)
	}
	if len(geos) != 0 {
		t.Errorf("got %d entries, want 0", len(geos))
	}
}

func TestComplexity(t *testing.T) {
	bones, cubes := Complexity(DefaultGeometry())
	if bones == 0 || cubes == 0 {
		t.Fatalf("Complexity = %d bones, %d cubes; want both non-zero", bones, cubes)
	}
	// Sanity bound: the vanilla bundle is small. A wild number here means
	// entries are being double-counted or missed.
	if bones > 100 || cubes > 100 {
		t.Errorf("Complexity = %d bones, %d cubes; implausible for the vanilla bundle", bones, cubes)
	}
}

func TestRenderTextureOnly(t *testing.T) {
	img, err := Render(Options{Texture: testTexture()})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := img.Bounds().Dx(); got != DefaultSize {
		t.Errorf("width = %d, want %d", got, DefaultSize)
	}
	if got := img.Bounds().Dy(); got != DefaultSize {
		t.Errorf("height = %d, want %d", got, DefaultSize)
	}
}

func TestRenderRequiresTexture(t *testing.T) {
	if _, err := Render(Options{}); err != ErrNoTexture {
		t.Errorf("err = %v, want ErrNoTexture", err)
	}
}

func TestRenderViewsAndSizes(t *testing.T) {
	for _, view := range []View{ViewBody, ViewChest, ViewHead, ViewAvatar} {
		for _, angle := range []Angle{AngleFront, AngleIso} {
			img, err := Render(Options{
				Texture: testTexture(),
				View:    view,
				Angle:   angle,
				Size:    64,
			})
			if err != nil {
				t.Errorf("%s/%s: %v", view, angle, err)
				continue
			}
			if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
				t.Errorf("%s/%s: bounds = %v, want 64x64", view, angle, img.Bounds())
			}
		}
	}
}

// The wide and slim models must actually produce different pixels, otherwise
// Identifier is not doing anything.
func TestRenderIdentifierChangesOutput(t *testing.T) {
	render := func(identifier string) *image.NRGBA {
		t.Helper()
		img, err := Render(Options{
			Texture:    testTexture(),
			Identifier: identifier,
			Size:       64,
		})
		if err != nil {
			t.Fatalf("%s: %v", identifier, err)
		}
		out := image.NewNRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				out.Set(x, y, img.At(x, y))
			}
		}
		return out
	}

	wide := render("geometry.humanoid.custom")
	slim := render("geometry.humanoid.customSlim")

	same := true
	for i := range wide.Pix {
		if wide.Pix[i] != slim.Pix[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("wide and slim renders are identical")
	}
}

// A persona skin has bones but no cubes. Render must fall back to a flat
// texture crop instead of failing.
func TestRenderPersonaFallsBackTo2D(t *testing.T) {
	persona := []Geometry{{
		Identifier:    "geometry.persona",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones:         []Bone{{Name: "root"}, {Name: "body"}, {Name: "head"}},
	}}

	img, err := Render(Options{Texture: testTexture(), Geometry: persona, Size: 128})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if img.Bounds().Dx() != 128 {
		t.Errorf("width = %d, want 128", img.Bounds().Dx())
	}
}

func TestRenderParts(t *testing.T) {
	img, err := Render(Options{
		Texture: testTexture(),
		Parts:   []string{"head"},
		Size:    64,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if img.Bounds().Dx() != 64 {
		t.Errorf("width = %d, want 64", img.Bounds().Dx())
	}
}

func TestRenderUnknownPartFails(t *testing.T) {
	_, err := Render(Options{
		Texture: testTexture(),
		Parts:   []string{"definitely-not-a-bone"},
		Size:    64,
	})
	if err == nil {
		t.Error("expected an error for a parts list matching no bones")
	}
}

func TestRenderCamera(t *testing.T) {
	img, err := Render(Options{
		Texture: testTexture(),
		Camera:  &Camera{Yaw: 35, Pitch: 15, FOV: 30, Margin: 1.4},
		Size:    64,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if img.Bounds().Dx() != 64 {
		t.Errorf("width = %d, want 64", img.Bounds().Dx())
	}
}

func TestParseParts(t *testing.T) {
	got := ParseParts(" head , leftArm ,, rightArm ")
	want := []string{"head", "leftArm", "rightArm"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}
