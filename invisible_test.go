package skinapi

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func makeTexture(alpha uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{R: 100, G: 100, B: 100, A: alpha})
		}
	}
	return img
}

func fptr(v float64) *float64 { return &v }

func TestValidateSkinVisibilityAllTransparentNoGeo(t *testing.T) {
	tex := makeTexture(0)
	r := ValidateSkinVisibility(tex, nil, DefaultMinVisibleFraction)
	if !r.IsInvisible {
		t.Error("expected invisible for fully transparent texture")
	}
	if r.Pass {
		t.Error("expected Pass=false")
	}
	for _, p := range r.Parts {
		if p.Visible {
			t.Errorf("part %q unexpectedly visible", p.Name)
		}
	}
}

func TestValidateSkinVisibilityFullyVisibleNoGeo(t *testing.T) {
	r := ValidateSkinVisibility(testTexture(), nil, DefaultMinVisibleFraction)
	if r.IsInvisible {
		t.Error("expected visible for opaque test texture")
	}
	if !r.Pass {
		t.Error("expected Pass=true")
	}
	for _, p := range r.Parts {
		if !p.Visible {
			t.Errorf("part %q should be visible on opaque texture", p.Name)
		}
		if p.Fraction != 1.0 {
			t.Errorf("part %q fraction = %v, want 1.0", p.Name, p.Fraction)
		}
	}
}

func TestValidateSkinVisibilityHeadOnlyNoGeo(t *testing.T) {
	tex := makeTexture(0)
	// Head north face UV is at (16, 16, 8, 8) on a 64x64 texture.
	for y := 16; y < 24; y++ {
		for x := 16; x < 24; x++ {
			tex.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}

	r := ValidateSkinVisibility(tex, nil, DefaultMinVisibleFraction)
	if r.IsInvisible {
		t.Error("expected not invisible (head is visible)")
	}
	for _, p := range r.Parts {
		if p.Name == "head" && !p.Visible {
			t.Error("head should be visible")
		}
		if p.Name != "head" && p.Visible {
			t.Errorf("part %q should not be visible", p.Name)
		}
	}
}

func TestValidateSkinVisibility128x128NoGeo(t *testing.T) {
	tex := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			tex.Set(x, y, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	r := ValidateSkinVisibility(tex, nil, DefaultMinVisibleFraction)
	if r.IsInvisible {
		t.Error("expected visible for opaque 128x128 texture")
	}
	if len(r.Parts) != 6 {
		t.Fatalf("got %d parts, want 6", len(r.Parts))
	}
}

func TestValidateSkinVisibilityTextureTooSmallNoGeo(t *testing.T) {
	tex := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	r := ValidateSkinVisibility(tex, nil, DefaultMinVisibleFraction)
	if !r.IsInvisible {
		t.Error("expected invisible for texture smaller than body part regions")
	}
}

// Cross-reference: geometry says "I have a head cube" but the texture is
// transparent where that cube's UV maps to — the head is flagged invisible.
func TestValidateSkinVisibilityGeoTransparentTexture(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}, UV: mustJSON(`[20,20]`)},
			}},
		},
	}
	tex := makeTexture(0)
	r := ValidateSkinVisibility(tex, getGeometryBytes(geo), DefaultMinVisibleFraction)
	if !r.IsInvisible {
		t.Error("expected invisible — geometry defines parts but texture is transparent")
	}
	// Geometry parts should be flagged invisible even though they exist
	for _, p := range r.Parts {
		if p.Visible {
			t.Errorf("part %q should not be visible (texture is transparent)", p.Name)
		}
		if !p.FromGeo {
			t.Errorf("part %q should come from geometry", p.Name)
		}
	}
}

// Cross-reference: geometry has head with visible texture pixels.
func TestValidateSkinVisibilityGeoVisibleTexture(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}, UV: mustJSON(`[20,20]`)},
			}},
		},
	}
	r := ValidateSkinVisibility(testTexture(), getGeometryBytes(geo), DefaultMinVisibleFraction)
	if r.IsInvisible {
		t.Error("expected visible — geometry parts map to opaque texture")
	}
	for _, p := range r.Parts {
		if !p.Visible {
			t.Errorf("part %q should be visible", p.Name)
		}
	}
}

// Cross-reference: geometry has custom bones not in standard layout.
func TestValidateSkinVisibilityCustomBones(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "tail", Cubes: []Cube{
				{Origin: []float64{-1, 20, 4}, Size: []float64{2, 2, 6}, UV: mustJSON(`[48,0]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}, UV: mustJSON(`[20,20]`)},
			}},
			{Name: "leftArm", Cubes: []Cube{
				{Origin: []float64{4, 12, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[36,52]`)},
			}},
			{Name: "rightArm", Cubes: []Cube{
				{Origin: []float64{-8, 12, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[44,20]`)},
			}},
			{Name: "leftLeg", Cubes: []Cube{
				{Origin: []float64{-4, 0, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[20,52]`)},
			}},
			{Name: "rightLeg", Cubes: []Cube{
				{Origin: []float64{0, 0, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[4,20]`)},
			}},
		},
	}
	r := ValidateSkinVisibility(testTexture(), getGeometryBytes(geo), DefaultMinVisibleFraction)
	if r.IsInvisible {
		t.Error("expected visible")
	}
	found := false
	for _, p := range r.Parts {
		if p.Name == "tail" {
			found = true
			if !p.Visible {
				t.Error("custom 'tail' bone should be visible on opaque texture")
			}
			if !p.FromGeo {
				t.Error("tail should come from geometry")
			}
		}
	}
	if !found {
		t.Error("expected to find 'tail' bone in results")
	}
}

// Cross-reference: geometry is tiny but texture is opaque — still invisible
// because the body parts are too small to render.
func TestValidateSkinVisibilityTinyGeoOpaqueTexture(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[20,20]`)},
			}},
		},
	}
	r := ValidateSkinVisibility(testTexture(), getGeometryBytes(geo), DefaultMinVisibleFraction)
	for _, p := range r.Parts {
		if p.Visible {
			t.Errorf("part %q should not be visible (geometry too tiny)", p.Name)
		}
	}
}

func TestValidateSkinVisibilityCustomFraction(t *testing.T) {
	tex := makeTexture(0)
	// Head north face is at (16, 16, 8, 8) on a 64x64 texture.
	for y := 16; y < 24; y++ {
		for x := 16; x < 24; x++ {
			tex.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	r := ValidateSkinVisibility(tex, nil, 0.5)
	if r.IsInvisible {
		t.Error("expected visible with 50% threshold and fully opaque head")
	}
}

func TestValidateGeometrySizeNormal(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
			}},
		},
	}
	r := ValidateGeometrySize(getGeometryBytes(geo), DefaultMinGeometrySize)
	if !r.Pass {
		t.Error("expected pass for normal 8x8x8 head")
	}
}

func TestValidateGeometrySizeTiny(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
			{Name: "leftArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
			{Name: "rightArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
			{Name: "leftLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
			{Name: "rightLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
		},
	}
	r := ValidateGeometrySize(getGeometryBytes(geo), DefaultMinGeometrySize)
	if r.Pass {
		t.Error("expected fail for 0.2-unit bones")
	}
	if len(r.Violations) < 1 {
		t.Fatal("expected at least one violation")
	}
}

func TestValidateGeometrySizeNoHead(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}},
			}},
		},
	}
	r := ValidateGeometrySize(getGeometryBytes(geo), DefaultMinGeometrySize)
	if r.Pass {
		t.Error("expected fail when head bone is missing")
	}
	if len(r.Violations) == 0 || r.Violations[0].Bone != "head" {
		t.Error("expected head violation for missing head")
	}
}

func TestValidateGeometrySizeCubeLessBonesIgnored(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
			}},
			{Name: "hat", Parent: "head"},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}},
			}},
		},
	}
	r := ValidateGeometrySize(getGeometryBytes(geo), DefaultMinGeometrySize)
	if !r.Pass {
		t.Error("expected pass; cube-less bones should be ignored")
	}
}

func TestValidateGeometrySizeInflate(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, Inflate: fptr(10)},
			}},
			{Name: "leftArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, Inflate: fptr(10)},
			}},
			{Name: "rightArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, Inflate: fptr(10)},
			}},
			{Name: "leftLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, Inflate: fptr(10)},
			}},
			{Name: "rightLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, Inflate: fptr(10)},
			}},
		},
	}
	r := ValidateGeometrySize(getGeometryBytes(geo), DefaultMinGeometrySize)
	if !r.Pass {
		t.Error("expected pass; inflate 10 makes all parts large enough")
	}
}

func TestValidateSkinInvisibilityTransparentTexture(t *testing.T) {
	r := ValidateSkinInvisibility(makeTexture(0), nil)
	if r.Pass {
		t.Error("expected fail for fully transparent texture")
	}
	if !r.IsInvisible {
		t.Error("expected IsInvisible=true")
	}
}

func TestValidateSkinInvisibilityNormalSkin(t *testing.T) {
	r := ValidateSkinInvisibility(testTexture(), nil)
	if !r.Pass {
		t.Error("expected pass for normal opaque texture")
	}
	if r.IsInvisible {
		t.Error("expected IsInvisible=false")
	}
}

// Combined: opaque texture but tiny geometry — invisible because parts are too
// small to render.
func TestValidateSkinInvisibilityTinyGeoOpaqueTexture(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[20,20]`)},
			}},
			{Name: "leftArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[36,52]`)},
			}},
			{Name: "rightArm", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[44,20]`)},
			}},
			{Name: "leftLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[20,52]`)},
			}},
			{Name: "rightLeg", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}, UV: mustJSON(`[4,20]`)},
			}},
		},
	}
	r := ValidateSkinInvisibility(testTexture(), getGeometryBytes(geo))
	if r.Pass {
		t.Error("expected fail — tiny geometry overrides visible texture")
	}
	if !r.IsInvisible {
		t.Error("expected IsInvisible=true")
	}
}

// Combined: normal geometry, visible texture — passes.
func TestValidateSkinInvisibilityNormalGeoVisibleTexture(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}, UV: mustJSON(`[8,8]`)},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}, UV: mustJSON(`[20,20]`)},
			}},
			{Name: "leftArm", Cubes: []Cube{
				{Origin: []float64{4, 12, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[36,52]`)},
			}},
			{Name: "rightArm", Cubes: []Cube{
				{Origin: []float64{-8, 12, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[44,20]`)},
			}},
			{Name: "leftLeg", Cubes: []Cube{
				{Origin: []float64{-4, 0, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[20,52]`)},
			}},
			{Name: "rightLeg", Cubes: []Cube{
				{Origin: []float64{0, 0, -2}, Size: []float64{4, 12, 4}, UV: mustJSON(`[4,20]`)},
			}},
		},
	}
	r := ValidateSkinInvisibility(testTexture(), getGeometryBytes(geo))
	if !r.Pass {
		t.Error("expected pass — normal geometry with visible texture")
	}
	if r.IsInvisible {
		t.Error("expected IsInvisible=false")
	}
}

func TestIsSkinInvisible(t *testing.T) {
	if !IsSkinInvisible(makeTexture(0)) {
		t.Error("expected invisible for transparent texture")
	}
	if IsSkinInvisible(testTexture()) {
		t.Error("expected not invisible for opaque texture")
	}
}

func TestIsSkinTiny(t *testing.T) {
	normal := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
			}},
			{Name: "body", Cubes: []Cube{
				{Origin: []float64{-4, 12, -2}, Size: []float64{8, 12, 4}},
			}},
		},
	}
	if IsSkinTiny(getGeometryBytes(normal)) {
		t.Error("expected not tiny for normal geometry")
	}

	tiny := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-0.1, 0, -0.1}, Size: []float64{0.2, 0.2, 0.2}},
			}},
		},
	}
	if !IsSkinTiny(getGeometryBytes(tiny)) {
		t.Error("expected tiny for 0.2-unit head")
	}
}

func TestClampedBounds(t *testing.T) {
	outer := image.Rect(0, 0, 64, 64)
	tests := []struct {
		name  string
		inner image.Rectangle
		want  image.Rectangle
	}{
		{"fully inside", image.Rect(10, 10, 20, 20), image.Rect(10, 10, 20, 20)},
		{"partial overlap", image.Rect(50, 50, 80, 80), image.Rect(50, 50, 64, 64)},
		{"no overlap", image.Rect(100, 100, 120, 120), image.Rectangle{}},
		{"full cover", image.Rect(-10, -10, 80, 80), image.Rect(0, 0, 64, 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampedBounds(tc.inner, outer)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBoneWorldSize(t *testing.T) {
	b := Bone{Name: "head", Inflate: 0.25, Cubes: []Cube{
		{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
	}}
	sz := boneWorldSize(b)
	if sz != 8.5 {
		t.Errorf("boneWorldSize = %v, want 8.5 (8 + 2*0.25)", sz)
	}
}

func TestBoneWorldSizeCubeInflateOverride(t *testing.T) {
	b := Bone{Name: "head", Inflate: 0.25, Cubes: []Cube{
		{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}, Inflate: fptr(1)},
	}}
	sz := boneWorldSize(b)
	if sz != 10.0 {
		t.Errorf("boneWorldSize = %v, want 10.0 (8 + 2*1.0)", sz)
	}
}

func TestBoneWorldSizeMultipleCubes(t *testing.T) {
	b := Bone{Name: "body", Cubes: []Cube{
		{Origin: []float64{0, 0, 0}, Size: []float64{4, 4, 4}},
		{Origin: []float64{5, 0, 0}, Size: []float64{2, 2, 2}},
	}}
	sz := boneWorldSize(b)
	if sz != 6.0 {
		t.Errorf("boneWorldSize = %v, want 6.0 (4+2)", sz)
	}
}

func TestGetGeometryNil(t *testing.T) {
	if bones := getGeometry(nil); bones != nil {
		t.Error("expected nil for nil geometry data")
	}
}

func TestGetGeometryEmpty(t *testing.T) {
	if bones := getGeometry([]byte{}); bones != nil {
		t.Error("expected nil for empty geometry data")
	}
}

func TestGetGeometryValid(t *testing.T) {
	geo := Geometry{
		Identifier:    "test",
		TextureWidth:  64,
		TextureHeight: 64,
		Bones: []Bone{
			{Name: "head", Cubes: []Cube{
				{Origin: []float64{-4, 24, -4}, Size: []float64{8, 8, 8}},
			}},
		},
	}
	bones := getGeometry(getGeometryBytes(geo))
	if bones == nil {
		t.Fatal("expected non-nil for valid geometry")
	}
	if _, ok := bones["head"]; !ok {
		t.Error("expected head bone")
	}
}

func TestRegionVisibilityFullOpacity(t *testing.T) {
	tex := makeTexture(255)
	vis, total, transparent := regionVisibility(tex, image.Rect(0, 0, 10, 10), DefaultMinVisibleAlpha)
	if !vis {
		t.Error("expected visible for fully opaque region")
	}
	if total != 100 {
		t.Errorf("total = %d, want 100", total)
	}
	if transparent != 0 {
		t.Errorf("transparent = %d, want 0", transparent)
	}
}

func TestRegionVisibilityFullTransparency(t *testing.T) {
	tex := makeTexture(0)
	vis, total, transparent := regionVisibility(tex, image.Rect(0, 0, 10, 10), DefaultMinVisibleAlpha)
	if vis {
		t.Error("expected not visible for fully transparent region")
	}
	if transparent != total {
		t.Errorf("transparent = %d, want %d", transparent, total)
	}
}

// --- Real capture cross-validation tests ---
// These load every captured skin from testdata/captures/ and verify the
// detector does not false-flag them. Each capture is a real skin from a
// live Bedrock client.

func loadCapture(t *testing.T, name string) (image.Image, []byte) {
	t.Helper()
	dir := filepath.Join("testdata", "captures", name)

	texFile, err := os.Open(filepath.Join(dir, "texture.png"))
	if err != nil {
		t.Fatalf("open texture: %v", err)
	}
	defer texFile.Close()
	tex, err := png.Decode(texFile)
	if err != nil {
		t.Fatalf("decode texture: %v", err)
	}

	geoData, err := os.ReadFile(filepath.Join(dir, "geometry.json"))
	if err != nil {
		t.Fatalf("read geometry: %v", err)
	}

	return tex, geoData
}

func TestInvisibleDetectorRealCapturesNoFalseFlags(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "captures"))
	if err != nil {
		t.Fatalf("read captures dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no captures in testdata/captures — copy them from mc-skinapi/mc-proxy/captures")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			tex, geoData := loadCapture(t, name)

			// ValidateSkinVisibility with geometry cross-reference
			vr := ValidateSkinVisibility(tex, geoData, DefaultMinVisibleFraction)
			if vr.IsInvisible {
				t.Errorf("FALSE POSITIVE: ValidateSkinVisibility flagged %q as invisible", name)
				for _, p := range vr.Parts {
					t.Logf("  %s: visible=%v fraction=%.4f pixels=%d transparent=%d fromGeo=%v",
						p.Name, p.Visible, p.Fraction, p.Pixels, p.Transparent, p.FromGeo)
				}
			}

			// ValidateSkinInvisibility (combined geo + texture check)
			ivr := ValidateSkinInvisibility(tex, geoData)
			if ivr.IsInvisible {
				t.Errorf("FALSE POSITIVE: ValidateSkinInvisibility flagged %q as invisible", name)
				for _, p := range ivr.Parts {
					t.Logf("  %s: visible=%v fraction=%.4f fromGeo=%v",
						p.Name, p.Visible, p.Fraction, p.FromGeo)
				}
			}
			if ivr.Suspicious {
				t.Errorf("FALSE POSITIVE: ValidateSkinInvisibility flagged %q as suspicious (half-invisible)", name)
				for _, p := range ivr.Parts {
					t.Logf("  %s: visible=%v fraction=%.4f fromGeo=%v",
						p.Name, p.Visible, p.Fraction, p.FromGeo)
				}
			}

			// IsSkinInvisible (no geometry, standard UV fallback)
			if IsSkinInvisible(tex) {
				t.Errorf("FALSE POSITIVE: IsSkinInvisible flagged %q as invisible (standard UV)", name)
			}

			// Geometry size check
			gsr := ValidateGeometrySize(geoData, DefaultMinGeometrySize)
			if !gsr.Pass {
				t.Errorf("FALSE POSITIVE: ValidateGeometrySize flagged %q as tiny", name)
				for _, v := range gsr.Violations {
					t.Logf("  bone=%s size=%.2f min=%.2f", v.Bone, v.Size, v.Minimum)
				}
			}

			// Print per-part results for visibility
			t.Logf("results for %s:", name)
			for _, p := range vr.Parts {
				status := "VISIBLE"
				if !p.Visible {
					status = "invisible"
				}
				t.Logf("  %s: %s fraction=%.2f%% (%d/%d pixels) fromGeo=%v",
					p.Name, status, p.Fraction*100, p.Pixels-p.Transparent, p.Pixels, p.FromGeo)
			}
		})
	}
}

func TestInvisibleDetectorRealCapturesGeometryDetails(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "captures"))
	if err != nil {
		t.Fatalf("read captures dir: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			tex, geoData := loadCapture(t, name)

			geos, err := ParseGeometry(geoData)
			if err != nil {
				t.Fatalf("parse geometry: %v", err)
			}
			geo, _ := SelectGeometry(geos, "")
			t.Logf("geometry: %s (%d bones, %d cubes, %.0fx%.0f texture)",
				geo.Identifier, len(geo.Bones), geo.TotalCubes(),
				geo.TextureWidth, geo.TextureHeight)

			bounds := tex.Bounds()
			t.Logf("texture: %dx%d", bounds.Dx(), bounds.Dy())

			for _, bone := range geo.Bones {
				if len(bone.Cubes) == 0 {
					continue
				}
				sz := boneWorldSize(bone)
				t.Logf("  bone %-14s cubes=%d worldSize=%.2f inflate=%.3f",
					bone.Name, len(bone.Cubes), sz, bone.Inflate)
			}
		})
	}
}

func TestRenderRealCapturesStillWorks(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "captures"))
	if err != nil {
		t.Fatalf("read captures dir: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			tex, geoData := loadCapture(t, name)

			var geos []Geometry
			if !IsEmpty(geoData) {
				geos, err = ParseGeometry(geoData)
				if err != nil {
					t.Fatalf("parse geometry: %v", err)
				}
			}

			img, err := Render(Options{
				Texture:  tex,
				Geometry: geos,
				Size:     128,
			})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			bounds := img.Bounds()
			if bounds.Dx() != 128 || bounds.Dy() != 128 {
				t.Errorf("output bounds = %v, want 128x128", bounds)
			}

			// Verify the output has at least some non-transparent pixels
			nrgba, ok := img.(*image.NRGBA)
			if !ok {
				t.Skip("output is not *image.NRGBA, cannot count pixels")
			}
			nonTransparent := 0
			for _, a := range nrgba.Pix {
				if a%4 == 3 && a > 0 {
					nonTransparent++
				}
			}
			// Check alpha channel specifically
			nonTransparent = 0
			for i := 3; i < len(nrgba.Pix); i += 4 {
				if nrgba.Pix[i] > 0 {
					nonTransparent++
				}
			}
			totalPixels := bounds.Dx() * bounds.Dy()
			if nonTransparent == 0 {
				t.Errorf("output is fully transparent — render produced no visible pixels")
			}
			visibleFraction := float64(nonTransparent) / float64(totalPixels)
			t.Logf("render output: %d/%d visible pixels (%.1f%%)", nonTransparent, totalPixels, visibleFraction*100)
		})
	}
}

func mustJSON(s string) []byte {
	return []byte(s)
}

func loadTestPNG(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()
	tex, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return tex
}

// TestInvisibleSkinsDetected guards against regressions on real invisible
// skins the maintainer logged in-game. Each one must be flagged invisible
// (or, for the half-invisible 64x32, suspicious) rather than passing.
func TestInvisibleSkinsDetected(t *testing.T) {
	type tc struct {
		tex      string
		geo      string
		wantInv  bool
		wantSusp bool
	}
	cases := []tc{
		// Fully transparent 64x64 skin captured through the proxy.
		{tex: "invisible-skin-64x64.png", geo: "invisible-skin-64x64-geometry.json", wantInv: true},
		// Second 64x64 invisible skin, slightly different texture.
		{tex: "invisible-skin-64x64-b.png", geo: "invisible-skin-64x64-b-geometry.json", wantInv: true},
		// Half-invisible 64x32 classic skin: body/rightArm visible, head and
		// legs transparent/missing.
		{tex: "invisible-skin.png", wantSusp: true},
		// Tiny skin: only the left leg (+ its pants overlay) is opaque on a
		// 128x128 atlas, so the body is effectively invisible.
		{tex: "tiny-skin.png", geo: "tiny-skin-geometry.json", wantInv: true},
		// Fully transparent 128x128 skin (capture 104515).
		{tex: "invisible-skin-128.png", geo: "invisible-skin-128-geometry.json", wantInv: true},
	}

	for _, c := range cases {
		t.Run(c.tex, func(t *testing.T) {
			tex := loadTestPNG(t, c.tex)
			var geoData []byte
			if c.geo != "" {
				var err error
				geoData, err = os.ReadFile(filepath.Join("testdata", c.geo))
				if err != nil {
					t.Fatalf("read geo: %v", err)
				}
			}

			vr := ValidateSkinInvisibility(tex, geoData)
			t.Logf("visible=%d invisible=%d IsInvisible=%v Suspicious=%v",
				vr.VisibleParts, vr.InvisibleParts, vr.IsInvisible, vr.Suspicious)
			for _, p := range vr.Parts {
				t.Logf("  %s: visible=%v frac=%.3f (%d/%d) fromGeo=%v",
					p.Name, p.Visible, p.Fraction, p.Pixels-p.Transparent, p.Pixels, p.FromGeo)
			}

			if c.wantInv && !vr.IsInvisible {
				t.Errorf("expected IsInvisible but skin passed")
			}
			if !c.wantInv && vr.IsInvisible && !c.wantSusp {
				t.Errorf("did not expect IsInvisible")
			}
			if c.wantSusp && !vr.Suspicious {
				t.Errorf("expected Suspicious but was %v", vr.Suspicious)
			}
		})
	}
}

// An opaque cape must not make an otherwise invisible body pass the detector.
// The cape is an accessory, not a body part.
func TestCapeDoesNotCountAsVisibleBodyPart(t *testing.T) {
	tex := image.NewNRGBA(image.Rect(0, 0, 64, 64))

	// Fixture: head cube at UV (0,0), body cube at UV (16,16) on a standard
	// 64x64 texture. Both are fully transparent -> invisible body. The cape
	// uses a separate UV region (32,0) that is painted fully opaque.
	geo := `{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.humanoid.custom","texture_width":64,"texture_height":64},"bones":[
		{"name":"head","cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]},
		{"name":"body","cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,16]}]},
		{"name":"cape","cubes":[{"origin":[-5,8,3],"size":[10,16,1],"uv":[32,0]}]}
	]}]}`

	// Make only the cape region opaque (uv [32,0] -> slab occupies roughly
	// x=32.. and y=0..). The head/body UVs stay transparent.
	for y := 0; y < 16; y++ {
		for x := 32; x < 48; x++ {
			tex.Set(x, y, color.NRGBA{R: 200, G: 80, B: 80, A: 255})
		}
	}

	vr := ValidateSkinInvisibility(tex, mustJSON(geo))
	if !vr.IsInvisible {
		t.Errorf("expected IsInvisible with only an opaque cape; got Pass=%v visible=%d",
			vr.Pass, vr.VisibleParts)
	}
	for _, p := range vr.Parts {
		if p.Name == "cape" && !p.Visible {
			t.Error("cape should be reported visible (but not counted)")
		}
	}
}
