package skinapi

import (
	"image"
	"testing"
)

// The library's two untrusted inputs are a geometry document and an encoded
// image, both arriving from an arbitrary Bedrock client. Neither parser
// validates much by design — policy belongs to the caller — so the contract
// these targets pin down is narrow but the one that matters: whatever comes
// in, nothing panics.
//
// This is not hypothetical. A cube with a short "size" array parses cleanly
// and used to panic on the first index, in both the render path and the
// detector, from any login packet.
//
// Run longer than the seed corpus with:
//
//	go test -run=NONE -fuzz=FuzzParseGeometryThenRender -fuzztime=60s

var fuzzGeometrySeeds = []string{
	`null`,
	`{}`,
	`[]`,
	``,
	`not json at all`,
	`{"format_version":"1.12.0","minecraft:geometry":[]}`,
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.humanoid.custom","texture_width":64,"texture_height":64},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}]}]}`,
	// Short size/origin: the shapes that used to panic.
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8],"uv":[0,0]}]}]}]}`,
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g"},"bones":[{"name":"head","cubes":[{"origin":[],"size":[],"uv":{}}]}]}]}`,
	// Per-face UV form.
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":{"north":{"uv":[0,0],"uv_size":[8,8]}}}]}]}]}`,
	// Legacy pre-1.12 flat form.
	`{"format_version":"1.8.0","geometry.humanoid":{"texturewidth":64,"textureheight":32,"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]}]}}`,
	// Parent cycles and self-parenting.
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[{"name":"a","parent":"b","cubes":[{"origin":[0,0,0],"size":[1,1,1],"uv":[0,0]}]},{"name":"b","parent":"a"}]}]}`,
	// Extreme and non-finite numbers.
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":0,"texture_height":0},"bones":[{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-1e300,0,0],"size":[1e300,1,1],"uv":[0,0]}]}]}]}`,
	`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[{"name":"head","inflate":-99,"cubes":[{"origin":[0,0,0],"size":[-8,-8,-8],"uv":[-5,-5]}]}]}]}`,
}

func addGeometrySeeds(f *testing.F) {
	f.Helper()
	for _, s := range fuzzGeometrySeeds {
		f.Add([]byte(s))
	}
}

// FuzzParseGeometry checks the parser alone: it may reject input, but it may
// not panic, and success must not return a nil-but-no-error result.
func FuzzParseGeometry(f *testing.F) {
	addGeometrySeeds(f)
	f.Fuzz(func(t *testing.T, raw []byte) {
		geos, err := ParseGeometry(raw)
		if err != nil {
			if geos != nil {
				t.Errorf("ParseGeometry returned %d entries alongside error %v", len(geos), err)
			}
			return
		}
		// Every accessor must survive whatever parsed.
		Complexity(geos)
		SelectGeometry(geos, "")
		SelectGeometry(geos, "geometry.humanoid.custom")
		FindCape(geos)
		for i := range geos {
			geos[i].TotalCubes()
			geos[i].BoneByName("head")
		}
	})
}

// FuzzParseGeometryThenRender drives parsed geometry all the way through the
// renderer, which is where the indexing happened. An error is a fine outcome;
// a panic is not.
func FuzzParseGeometryThenRender(f *testing.F) {
	addGeometrySeeds(f)
	tex := testTexture()

	f.Fuzz(func(t *testing.T, raw []byte) {
		geos, err := ParseGeometry(raw)
		if err != nil {
			return
		}
		for _, view := range []View{ViewBody, ViewChest, ViewHead, ViewAvatar} {
			// Size stays small: this runs thousands of times, and output
			// dimensions are not what is under test.
			_, _ = Render(Options{Texture: tex, Geometry: geos, View: view, Size: 32})
		}
		_, _ = Render(Options{Texture: tex, Geometry: geos, Parts: []string{"head"}, Size: 32})
		_, _ = Render(Options{Texture: tex, Geometry: geos, Cape: tex, Size: 32})
	})
}

// FuzzValidateSkinInvisibility covers the detector, which walks the same cube
// arrays as the renderer and panicked on the same input.
func FuzzValidateSkinInvisibility(f *testing.F) {
	addGeometrySeeds(f)
	opaque := makeTexture(255)
	transparent := makeTexture(0)

	f.Fuzz(func(t *testing.T, raw []byte) {
		for _, tex := range []image.Image{opaque, transparent} {
			ValidateSkinInvisibility(tex, raw)
			ValidateSkinVisibility(tex, raw, DefaultMinVisibleFraction)
			NewSkin(tex, raw).Report()
		}
		ValidateGeometrySize(raw, DefaultMinGeometrySize)
		IsSkinTiny(raw)
	})
}

// FuzzParseResourcePatch checks the resource-patch decoder, which reads
// attacker-controlled JSON from the login packet.
func FuzzParseResourcePatch(f *testing.F) {
	for _, s := range []string{
		``,
		`null`,
		`{}`,
		`{"geometry":{"default":"geometry.humanoid.customSlim"}}`,
		`{"geometry":{"default":"geometry.humanoid.custom","cape":"geometry.cape"}}`,
		`{"geometry":[]}`,
		`{"geometry":{"default":123}}`,
		`truncated`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		patch, err := ParseResourcePatch(raw)
		if err != nil && (patch.Default != "" || patch.Cape != "") {
			t.Errorf("ParseResourcePatch returned %+v alongside error %v", patch, err)
		}
	})
}

// FuzzImageDimensions checks the header-only reader callers are told to bound
// untrusted uploads with, before ever decoding them.
func FuzzImageDimensions(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("not an image"))
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		w, h, err := ImageDimensions(raw)
		if err != nil {
			return
		}
		if w <= 0 || h <= 0 {
			t.Errorf("ImageDimensions reported %dx%d with no error", w, h)
			return
		}
		// Decode only what the header says is small. This is the very
		// discipline the helper exists for: a few bytes of PNG can declare
		// dimensions that would allocate gigabytes on decode, and fuzzing the
		// decoder without the bound just exhausts memory.
		if w <= 256 && h <= 256 {
			_, _ = DecodeImage(raw)
		}
	})
}
