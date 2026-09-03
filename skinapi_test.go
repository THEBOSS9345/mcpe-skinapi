package skinapi

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
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

// A cube whose size/origin arrays are shorter than three components used to
// panic with index out of range rather than being skipped. Any client can send
// such geometry, so the render path must survive it.
func TestRenderSkipsMalformedCubes(t *testing.T) {
	tex := testTexture()

	for _, tc := range []struct {
		name string
		geom string
	}{
		{"short size", `{"origin":[-4,24,-4],"size":[8],"uv":[0,0]}`},
		{"short origin", `{"origin":[-4,24],"size":[8,8,8],"uv":[0,0]}`},
		{"both empty", `{"origin":[],"size":[],"uv":[0,0]}`},
		{"missing both", `{"uv":[0,0]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.t","texture_width":64,"texture_height":64},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[` +
				tc.geom + `,{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}]}]}`)
			geos, err := ParseGeometry(raw)
			if err != nil {
				t.Fatalf("ParseGeometry: %v", err)
			}
			// The malformed cube is skipped; the well-formed one still renders.
			if _, err := Render(Options{Texture: tex, Geometry: geos, Size: 64}); err != nil {
				t.Fatalf("Render: %v", err)
			}
		})
	}
}

// A bone of nothing but malformed cubes contributes no triangles, which must
// surface as the normal "nothing to render" error rather than a panic.
func TestRenderAllCubesMalformed(t *testing.T) {
	raw := []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.t","texture_width":64,"texture_height":64},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[0],"size":[1],"uv":[0,0]}]}]}]}`)
	geos, err := ParseGeometry(raw)
	if err != nil {
		t.Fatalf("ParseGeometry: %v", err)
	}
	if _, err := Render(Options{Texture: testTexture(), Geometry: geos, Size: 64}); err == nil {
		t.Error("expected an error when every cube is malformed, got nil")
	}
}

// customBodyGeometry is a skin with its own mesh and no cape entry, which is
// what a real custom skin's geometry.json looks like: capes always travel in
// their own entry and are never merged into a body.
func customBodyGeometry(t *testing.T) []Geometry {
	t.Helper()
	geos, err := ParseGeometry([]byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.custom","texture_width":64,"texture_height":64},"bones":[
		{"name":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,16]}]},
		{"name":"head","parent":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}
	]}]}`))
	if err != nil {
		t.Fatalf("ParseGeometry: %v", err)
	}
	if _, ok := FindCape(geos); ok {
		t.Fatal("fixture should not contain a cape entry")
	}
	return geos
}

func renderPNG(t *testing.T, opts Options) []byte {
	t.Helper()
	out, err := opts.RenderPNG()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

// An equipped cape used to be dropped silently for any skin shipping its own
// geometry, because the cape entry was only ever looked for in that geometry.
func TestCapeRendersWithCustomGeometry(t *testing.T) {
	geos := customBodyGeometry(t)
	tex := testTexture()

	without := renderPNG(t, Options{Texture: tex, Geometry: geos, View: ViewBody, Size: 96})
	with := renderPNG(t, Options{Texture: tex, Geometry: geos, View: ViewBody, Cape: tex, Size: 96})

	if bytes.Equal(without, with) {
		t.Error("cape made no difference to a custom-geometry skin: it was dropped")
	}
}

// A head or avatar crop does not show a cape, so none should be built for one.
func TestCapeExcludedFromHeadViews(t *testing.T) {
	tex := testTexture()
	for _, view := range []View{ViewHead, ViewAvatar} {
		t.Run(string(view), func(t *testing.T) {
			without := renderPNG(t, Options{Texture: tex, View: view, Size: 96})
			with := renderPNG(t, Options{Texture: tex, View: view, Cape: tex, Size: 96})
			if !bytes.Equal(without, with) {
				t.Errorf("%s view changed when a cape was equipped", view)
			}
		})
	}
}

// The camera frames on the cape as well as the body, so a cape reaching past
// the body cannot be pushed out of shot.
func TestCapeIsIncludedInFraming(t *testing.T) {
	tex := testTexture()
	without := renderPNG(t, Options{Texture: tex, View: ViewBody, Size: 96})
	with := renderPNG(t, Options{Texture: tex, View: ViewBody, Cape: tex, Size: 96})
	if bytes.Equal(without, with) {
		t.Error("equipping a cape did not change the body render")
	}
}

// A custom model can define a "cape" bone in the body entry itself. That bone
// is already drawn as part of the body mesh with the skin texture, so taking
// the cape from the same entry drew it twice and left the two z-fighting.
func TestCapeNotTakenFromTheRenderedBodyEntry(t *testing.T) {
	geos, err := ParseGeometry([]byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.body_with_cape","texture_width":64,"texture_height":64},"bones":[
		{"name":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,16]}]},
		{"name":"head","parent":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]},
		{"name":"cape","parent":"body","pivot":[0,24,3],"cubes":[{"origin":[-5,8,3],"size":[10,16,1],"uv":[0,0]}]}
	]}]}`))
	if err != nil {
		t.Fatalf("ParseGeometry: %v", err)
	}
	if _, ok := FindCape(geos); !ok {
		t.Fatal("fixture should carry a cape bone in the body entry")
	}

	// capeGeometryFor must skip the entry being rendered and reach the
	// built-in cape instead.
	body, _ := SelectGeometry(geos, "")
	capeGeo, found := capeGeometryFor(geos, body)
	if !found {
		t.Fatal("no cape geometry resolved")
	}
	if capeGeo.Identifier == body.Identifier {
		t.Errorf("cape came from the body entry %q, which is already drawn", capeGeo.Identifier)
	}

	// And the render still succeeds with a cape equipped.
	if _, err := Render(Options{Texture: testTexture(), Geometry: geos, Cape: testTexture(), Size: 64}); err != nil {
		t.Fatalf("render with cape: %v", err)
	}
}

// The ordinary case is unaffected: a bundle with a separate cape entry still
// draws the cape from it, not from the built-in fallback.
func TestCapeUsesTheBundlesOwnCapeEntry(t *testing.T) {
	geos := DefaultGeometry()
	body, _ := SelectGeometry(geos, "geometry.humanoid.custom")
	capeGeo, found := capeGeometryFor(geos, body)
	if !found {
		t.Fatal("no cape geometry resolved")
	}
	if capeGeo.Identifier != "geometry.cape" {
		t.Errorf("cape entry = %q, want geometry.cape", capeGeo.Identifier)
	}
}

// legacyBundle is a pre-1.12 document carrying both arm variants with equal
// cube counts, as vanilla's do, plus the sparse cape entry.
func legacyBundle() []byte {
	body := func(uv int) string {
		cubes := make([]string, 0, 12)
		for i := 0; i < 12; i++ {
			cubes = append(cubes, fmt.Sprintf(
				`{"origin":[-4,%d,-4],"size":[8,8,8],"uv":[%d,%d]}`, i, uv, i))
		}
		return `{"texturewidth":64,"textureheight":64,"bones":[{"name":"head","pivot":[0,24,0],"cubes":[` +
			strings.Join(cubes, ",") + `]}]}`
	}
	return []byte(`{"format_version":"1.8.0",` +
		`"geometry.humanoid.custom":` + body(0) + `,` +
		`"geometry.humanoid.customSlim":` + body(32) + `,` +
		`"geometry.cape":{"texturewidth":64,"textureheight":32,"bones":[{"name":"cape","pivot":[0,24,3],"cubes":[{"origin":[-5,8,3],"size":[10,16,1],"uv":[0,0]}]}]}}`)
}

// The legacy branch ranges a map, so without an explicit sort the same bytes
// parsed twice returned entries in different orders.
func TestParseGeometryLegacyOrderIsStable(t *testing.T) {
	var want []string
	for i := 0; i < 50; i++ {
		geos, err := ParseGeometry(legacyBundle())
		if err != nil {
			t.Fatalf("ParseGeometry: %v", err)
		}
		got := make([]string, len(geos))
		for j, g := range geos {
			got[j] = g.Identifier
		}
		if want == nil {
			want = got
			continue
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("entry order changed on run %d:\n got %v\nwant %v", i, got, want)
			}
		}
	}
	if len(want) != 3 {
		t.Fatalf("got %d entries, want 3: %v", len(want), want)
	}
}

// SelectGeometry breaks a cube-count tie by position, and vanilla's two arm
// variants tie exactly. Unstable parse order therefore picked between wide and
// slim at random, so the same skin rendered with different arms per call.
func TestSelectGeometryStableOnTiedLegacyBundle(t *testing.T) {
	picked := map[string]int{}
	for i := 0; i < 200; i++ {
		geos, err := ParseGeometry(legacyBundle())
		if err != nil {
			t.Fatalf("ParseGeometry: %v", err)
		}
		sel, ok := SelectGeometry(geos, "")
		if !ok {
			t.Fatal("SelectGeometry found nothing")
		}
		picked[sel.Identifier]++
	}
	if len(picked) != 1 {
		t.Errorf("SelectGeometry chose %d different entries across identical input: %v", len(picked), picked)
	}

	// Naming an entry explicitly must still win over the fallback.
	geos, _ := ParseGeometry(legacyBundle())
	sel, _ := SelectGeometry(geos, "geometry.humanoid.customSlim")
	if sel.Identifier != "geometry.humanoid.customSlim" {
		t.Errorf("explicit identifier = %q", sel.Identifier)
	}
}

// The modern format keeps its document order, which the sort must not disturb.
func TestParseGeometryModernKeepsDocumentOrder(t *testing.T) {
	raw := []byte(`{"format_version":"1.12.0","minecraft:geometry":[
		{"description":{"identifier":"zzz.last","texture_width":64,"texture_height":64},"bones":[{"name":"head","cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}]},
		{"description":{"identifier":"aaa.first","texture_width":64,"texture_height":64},"bones":[{"name":"head","cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}]}]}`)
	geos, err := ParseGeometry(raw)
	if err != nil {
		t.Fatalf("ParseGeometry: %v", err)
	}
	if len(geos) != 2 || geos[0].Identifier != "zzz.last" || geos[1].Identifier != "aaa.first" {
		t.Errorf("modern order not preserved: %v, %v", geos[0].Identifier, geos[1].Identifier)
	}
}
