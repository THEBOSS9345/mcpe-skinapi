package skinapi

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// A normal opaque vanilla skin must be reported as visible with all six body
// parts present, and never flagged invisible or suspicious.
func TestSkinReportNormalVisible(t *testing.T) {
	s := NewSkin(testTexture(), nil)
	rep := s.Report()

	if !rep.Pass {
		t.Error("expected Pass=true for a normal opaque skin")
	}
	if rep.IsInvisible {
		t.Error("expected IsInvisible=false for a normal skin")
	}
	if rep.IsSuspicious {
		t.Error("expected IsSuspicious=false for a normal skin")
	}
	if rep.VisibleParts != 6 || rep.InvisibleParts != 0 {
		t.Errorf("visible=%d invisible=%d, want 6/0", rep.VisibleParts, rep.InvisibleParts)
	}
	if len(rep.Invisible) != 0 {
		t.Errorf("expected no invisible parts, got %v", rep.Invisible)
	}
	if len(rep.Parts) != 6 {
		t.Errorf("got %d parts, want 6", len(rep.Parts))
	}
	for _, p := range rep.Parts {
		if !p.Visible {
			t.Errorf("part %s should be visible", p.Name)
		}
		if p.Visibility != PartVisible {
			t.Errorf("part %s visibility=%v, want visible", p.Name, p.Visibility)
		}
	}
}

// A fully transparent skin must be reported invisible with every standard body
// part listed in the Invisible slice.
func TestSkinReportFullyInvisible(t *testing.T) {
	s := NewSkin(makeTexture(0), nil)
	rep := s.Report()

	if !rep.IsInvisible {
		t.Error("expected IsInvisible=true for fully transparent skin")
	}
	if rep.Pass {
		t.Error("expected Pass=false for fully transparent skin")
	}
	if rep.VisibleParts != 0 {
		t.Errorf("visible=%d, want 0", rep.VisibleParts)
	}
	if len(rep.Invisible) != len(standardPartNames) {
		t.Errorf("invisible parts=%v, want all %d", rep.Invisible, len(standardPartNames))
	}
	for _, p := range rep.Parts {
		if p.Visibility != PartInvisible {
			t.Errorf("part %s visibility=%v, want invisible", p.Name, p.Visibility)
		}
	}
}

// The Parts()/IsInvisible()/IsSuspicious()/InvisibleParts() helpers must
// mirror Report().
func TestSkinHelperMethods(t *testing.T) {
	s := NewSkin(makeTexture(0), nil)
	if !s.IsInvisible() {
		t.Error("IsInvisible() should be true")
	}
	if s.IsSuspicious() {
		t.Error("IsSuspicious() should be false for fully invisible")
	}
	if got := s.InvisibleParts(); len(got) != len(standardPartNames) {
		t.Errorf("InvisibleParts()=%v, want all parts", got)
	}
	if len(s.Parts()) != 6 {
		t.Errorf("Parts() length=%d, want 6", len(s.Parts()))
	}
}

// A half-invisible skin (head-only) must be Suspicious but not fully invisible
// on the texture-only path.
func TestSkinReportSuspicious(t *testing.T) {
	tex := makeTexture(0)
	// Head north face at (16,16,8,8) on 64x64.
	w := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 16; y < 24; y++ {
		for x := 16; x < 24; x++ {
			tex.Set(x, y, w)
		}
	}
	s := NewSkin(tex, nil)
	rep := s.Report()

	if rep.IsInvisible {
		t.Error("expected not invisible (head is visible)")
	}
	if !rep.IsSuspicious {
		t.Error("expected Suspicious for head-only skin")
	}
	if rep.VisibleParts != 1 {
		t.Errorf("visible=%d, want 1", rep.VisibleParts)
	}
}

// The tiny captured skin (only left leg opaque) must be reported invisible via
// its geometry.
func TestSkinReportTinySkin(t *testing.T) {
	skipWithoutTestdata(t)
	tex := loadTestPNG(t, "tiny-skin.png")
	geo := mustReadFile(t, filepath.Join("testdata", "tiny-skin-geometry.json"))
	s := NewSkin(tex, geo)
	rep := s.Report()
	if !rep.IsInvisible {
		t.Errorf("expected tiny skin invisible, got Pass=%v vis=%d", rep.Pass, rep.VisibleParts)
	}
	if rep.VisibleParts != 1 {
		t.Errorf("expected 1 visible part (left leg), got %d", rep.VisibleParts)
	}
}

// The persona captured skin must NOT be flagged invisible or suspicious.
func TestSkinReportPersonaNotFlagged(t *testing.T) {
	skipWithoutTestdata(t)
	tex := loadTestPNG(t, filepath.Join("captures", "THE_BOSS9345-20260903-101055", "texture.png"))
	geo := mustReadFile(t, filepath.Join("testdata", "captures", "THE_BOSS9345-20260903-101055", "geometry.json"))
	s := NewSkin(tex, geo)
	rep := s.Report()
	if rep.IsInvisible {
		t.Errorf("false positive: persona skin flagged invisible")
	}
	if rep.IsSuspicious {
		t.Errorf("false positive: persona skin flagged suspicious")
	}
	if !rep.Pass {
		t.Errorf("expected persona skin to pass")
	}
}

// PartVisibility.String must produce stable labels.
func TestPartVisibilityString(t *testing.T) {
	want := map[PartVisibility]string{
		PartVisible:    "visible",
		PartInvisible:  "invisible",
		PartSuspicious: "suspicious",
		PartTiny:       "tiny",
	}
	for v, exp := range want {
		if v.String() != exp {
			t.Errorf("%v.String()=%q, want %q", int(v), v.String(), exp)
		}
	}
}

// mustReadFile reads a file or fails the test.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
