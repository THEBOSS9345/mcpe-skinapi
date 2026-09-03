package skinapi

import (
	"encoding/json"
	"errors"
	"image"
	"strings"
	"testing"
)

func TestParseView(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want View
	}{
		{"", ViewBody},
		{"   ", ViewBody},
		{"body", ViewBody},
		{"chest", ViewChest},
		{"head", ViewHead},
		{"avatar", ViewAvatar},
		{"AVATAR", ViewAvatar},
		{"  Head  ", ViewHead},
	} {
		got, err := ParseView(tc.in)
		if err != nil {
			t.Errorf("ParseView(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseView(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// An unrecognised view must be an error, not a silent fall back to the body:
// the whole point is that a service can tell a typo from a valid request.
func TestParseViewRejectsUnknown(t *testing.T) {
	for _, in := range []string{"torso", "avatr", "full-body", "1"} {
		got, err := ParseView(in)
		if err == nil {
			t.Errorf("ParseView(%q) = %q, want an error", in, got)
			continue
		}
		if !errors.Is(err, ErrUnknownView) {
			t.Errorf("ParseView(%q) error %v does not wrap ErrUnknownView", in, err)
		}
		if !strings.Contains(err.Error(), in) {
			t.Errorf("ParseView(%q) error %q does not name the input", in, err)
		}
	}
}

func TestParseAngle(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Angle
	}{
		{"", ""}, // zero Angle: "the default for the chosen view"
		{"front", AngleFront},
		{"iso", AngleIso},
		{"ISO", AngleIso},
		{" front ", AngleFront},
	} {
		got, err := ParseAngle(tc.in)
		if err != nil {
			t.Errorf("ParseAngle(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseAngle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := ParseAngle("side"); !errors.Is(err, ErrUnknownAngle) {
		t.Errorf("ParseAngle(\"side\") error = %v, want ErrUnknownAngle", err)
	}
}

// Render's failures are all bad caller input, so a service must be able to
// classify them with errors.Is instead of matching message text.
func TestRenderErrorsAreComparable(t *testing.T) {
	tex := testTexture()

	if _, err := Render(Options{}); !errors.Is(err, ErrNoTexture) {
		t.Errorf("no texture: %v, want ErrNoTexture", err)
	}
	if _, err := Render(Options{Texture: tex, Geometry: []Geometry{}, Identifier: "nope"}); err != nil {
		// Empty geometry falls back to DefaultGeometry, so this is not an error.
		t.Errorf("empty geometry should fall back to the default, got %v", err)
	}
	if _, err := Render(Options{Texture: tex, Parts: []string{"nonexistentBone"}}); !errors.Is(err, ErrNoMatchingParts) {
		t.Errorf("bad parts: %v, want ErrNoMatchingParts", err)
	}

	// A geometry with no head bone leaves ViewHead with nothing to draw.
	headless, err := ParseGeometry([]byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[{"name":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,16]}]}]}]}`))
	if err != nil {
		t.Fatalf("ParseGeometry: %v", err)
	}
	if _, err := Render(Options{Texture: tex, Geometry: headless, View: ViewHead}); !errors.Is(err, ErrEmptyView) {
		t.Errorf("empty view: %v, want ErrEmptyView", err)
	}
}

// The messages documented in docs/api-reference.md are part of the contract
// for anyone already matching on them.
func TestRenderErrorMessagesUnchanged(t *testing.T) {
	for err, want := range map[error]string{
		ErrNoGeometry:      "geometry has no usable entries",
		ErrNoMatchingParts: "no bones matched the requested parts",
		ErrEmptyView:       "nothing to render for this view",
		ErrNoTexture:       "texture is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v does not contain %q", err, want)
		}
	}
}

func TestParseResourcePatch(t *testing.T) {
	patch, err := ParseResourcePatch([]byte(`{"geometry":{"default":"geometry.humanoid.customSlim"}}`))
	if err != nil {
		t.Fatalf("ParseResourcePatch: %v", err)
	}
	if patch.Default != "geometry.humanoid.customSlim" {
		t.Errorf("Default = %q", patch.Default)
	}
	if patch.Cape != "" {
		t.Errorf("Cape = %q, want empty", patch.Cape)
	}

	patch, err = ParseResourcePatch([]byte(`{"geometry":{"default":"geometry.humanoid.custom","cape":"geometry.cape"}}`))
	if err != nil {
		t.Fatalf("ParseResourcePatch: %v", err)
	}
	if patch.Default != "geometry.humanoid.custom" || patch.Cape != "geometry.cape" {
		t.Errorf("got %+v", patch)
	}
}

// Nothing sent is not an error, the same way IsEmpty treats geometry.
func TestParseResourcePatchEmpty(t *testing.T) {
	for _, in := range []string{"", "null", "  null  "} {
		patch, err := ParseResourcePatch([]byte(in))
		if err != nil {
			t.Errorf("ParseResourcePatch(%q): %v", in, err)
		}
		if patch.Default != "" || patch.Cape != "" {
			t.Errorf("ParseResourcePatch(%q) = %+v, want zero", in, patch)
		}
	}

	if _, err := ParseResourcePatch([]byte(`{"geometry":`)); err == nil {
		t.Error("expected an error for malformed JSON")
	}
}

// The patch drives identifier selection, which is the reason it exists: the
// two humanoid variants must produce different renders.
func TestParseResourcePatchSelectsArmVariant(t *testing.T) {
	patch, err := ParseResourcePatch([]byte(`{"geometry":{"default":"geometry.humanoid.customSlim"}}`))
	if err != nil {
		t.Fatalf("ParseResourcePatch: %v", err)
	}
	slim := renderPNG(t, Options{Texture: testTexture(), Identifier: patch.Default, Size: 64})
	wide := renderPNG(t, Options{Texture: testTexture(), Identifier: "geometry.humanoid.custom", Size: 64})
	if string(slim) == string(wide) {
		t.Error("slim and wide identifiers produced the same image")
	}
}

func TestImageDimensions(t *testing.T) {
	data := testTextureBytes(t)
	w, h, err := ImageDimensions(data)
	if err != nil {
		t.Fatalf("ImageDimensions: %v", err)
	}
	img, err := DecodeImage(data)
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if b := img.Bounds(); w != b.Dx() || h != b.Dy() {
		t.Errorf("ImageDimensions = %dx%d, decoded image is %dx%d", w, h, b.Dx(), b.Dy())
	}

	if _, _, err := ImageDimensions(nil); err == nil {
		t.Error("expected an error for empty input")
	}
	if _, _, err := ImageDimensions([]byte("not an image")); err == nil {
		t.Error("expected an error for garbage")
	}
}

// SkinReport is documented as safe to hand to json.Marshal, which means the
// visibility field has to read as a name rather than an integer.
func TestPartVisibilityJSON(t *testing.T) {
	for _, v := range []PartVisibility{PartVisible, PartInvisible, PartSuspicious, PartTiny} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %v: %v", v, err)
		}
		if want := `"` + v.String() + `"`; string(data) != want {
			t.Errorf("marshal %v = %s, want %s", v, data, want)
		}

		var back PartVisibility
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != v {
			t.Errorf("round trip %v -> %s -> %v", v, data, back)
		}
	}

	// Integers still decode, for a client written against the old encoding.
	var v PartVisibility
	if err := json.Unmarshal([]byte("3"), &v); err != nil || v != PartTiny {
		t.Errorf("unmarshal 3 = %v, %v; want PartTiny", v, err)
	}
	if err := json.Unmarshal([]byte(`"bogus"`), &v); err == nil {
		t.Error("expected an error for an unknown name")
	}
	if err := json.Unmarshal([]byte("99"), &v); err == nil {
		t.Error("expected an error for an out-of-range integer")
	}
}

func TestSkinReportMarshalsReadably(t *testing.T) {
	data, err := json.Marshal(NewSkin(makeTexture(0), nil).Report())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"Visibility":"invisible"`) {
		t.Errorf("report JSON does not carry a readable visibility:\n%s", data)
	}
}

// A stricter caller can demand near-total opacity; a lenient one can accept a
// mostly-transparent part. The defaults must stay what NewSkin uses.
func TestSkinOptionsThresholds(t *testing.T) {
	// A texture whose parts are half opaque: exactly at the default bar.
	tex := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			a := uint8(0)
			if y%2 == 0 {
				a = 255
			}
			tex.Pix[(y*64+x)*4+3] = a
			tex.Pix[(y*64+x)*4+0] = 120
			tex.Pix[(y*64+x)*4+1] = 120
			tex.Pix[(y*64+x)*4+2] = 120
		}
	}

	lenient := NewSkinWithOptions(tex, nil, SkinOptions{MinVisibleFraction: 0.25}).Report()
	strict := NewSkinWithOptions(tex, nil, SkinOptions{MinVisibleFraction: 0.9}).Report()

	if lenient.VisibleParts <= strict.VisibleParts {
		t.Errorf("a lenient threshold saw %d visible parts, a strict one %d; expected lenient to see more",
			lenient.VisibleParts, strict.VisibleParts)
	}

	// The zero SkinOptions must behave exactly like NewSkin.
	a := NewSkin(tex, nil).Report()
	b := NewSkinWithOptions(tex, nil, SkinOptions{}).Report()
	if a.Pass != b.Pass || a.VisibleParts != b.VisibleParts || len(a.Parts) != len(b.Parts) {
		t.Error("zero SkinOptions does not match NewSkin's defaults")
	}
}

// MinVisibleParts moves the suspicious bar without touching the invisible one.
func TestSkinOptionsMinVisibleParts(t *testing.T) {
	tex := makeTexture(255)
	relaxed := NewSkinWithOptions(tex, nil, SkinOptions{MinVisibleParts: 1}).Report()
	if relaxed.IsSuspicious {
		t.Error("a fully opaque skin should never be suspicious")
	}

	demanding := NewSkinWithOptions(tex, nil, SkinOptions{MinVisibleParts: 99}).Report()
	if !demanding.IsSuspicious {
		t.Error("demanding 99 visible parts should make even an opaque skin suspicious")
	}
	if demanding.IsInvisible {
		t.Error("raising the suspicious bar must not make a skin invisible")
	}
}
