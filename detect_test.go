package skinapi

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Report used to claim a cache it did not have, so every helper re-ran the
// whole analysis. Sharing one Skin across goroutines must also be safe.
func TestSkinReportIsCachedAndConcurrent(t *testing.T) {
	s := NewSkin(makeTexture(255), nil)

	var wg sync.WaitGroup
	reports := make([]SkinReport, 8)
	for i := range reports {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i] = s.Report()
			_ = s.IsInvisible()
			_ = s.IsSuspicious()
			_ = s.InvisibleParts()
		}(i)
	}
	wg.Wait()

	for i, r := range reports {
		if r.Pass != reports[0].Pass || len(r.Parts) != len(reports[0].Parts) {
			t.Fatalf("report %d disagrees with report 0", i)
		}
	}
}

// Each call hands back its own slices, so a caller sorting or truncating the
// result cannot corrupt the cached report.
func TestSkinReportSlicesAreNotShared(t *testing.T) {
	s := NewSkin(makeTexture(255), nil)
	first := s.Report()
	if len(first.Parts) == 0 {
		t.Fatal("expected parts")
	}
	first.Parts[0].Name = "clobbered"

	if second := s.Report(); second.Parts[0].Name == "clobbered" {
		t.Error("mutating a returned report changed the cached one")
	}
}

// SkinReport is documented as safe to marshal straight to JSON, which requires
// a stable Parts order. Ranging the bone map directly reshuffled it per call.
func TestSkinReportPartOrderIsStable(t *testing.T) {
	geom := []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[
		{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[0,0]}]},
		{"name":"hat","parent":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,24,-4],"size":[8,8,8],"uv":[32,0]}]},
		{"name":"jacket","pivot":[0,24,0],"cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,32]}]},
		{"name":"cape","pivot":[0,24,0],"cubes":[{"origin":[-5,8,3],"size":[10,16,1],"uv":[0,0]}]},
		{"name":"leftSleeve","pivot":[0,24,0],"cubes":[{"origin":[4,12,-2],"size":[4,12,4],"uv":[48,48]}]}
	]}]}`)

	var want []string
	for i := 0; i < 20; i++ {
		var got []string
		for _, p := range NewSkin(makeTexture(255), geom).Report().Parts {
			got = append(got, p.Name)
		}
		if want == nil {
			want = got
			continue
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("part order changed on run %d:\n got %v\nwant %v", i, got, want)
			}
		}
	}

	// Standard parts keep their fixed order and lead; the rest are sorted.
	if want[0] != "head" {
		t.Errorf("first part = %q, want the standard parts first", want[0])
	}
}

// ValidateGeometrySize's Violations had the same map-ordering problem.
func TestGeometrySizeViolationOrderIsStable(t *testing.T) {
	geom := []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g"},"bones":[
		{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[0,0,0],"size":[0.1,0.1,0.1],"uv":[0,0]}]},
		{"name":"body","pivot":[0,24,0],"cubes":[{"origin":[0,0,0],"size":[0.1,0.1,0.1],"uv":[0,0]}]},
		{"name":"leftArm","pivot":[0,24,0],"cubes":[{"origin":[0,0,0],"size":[0.1,0.1,0.1],"uv":[0,0]}]},
		{"name":"rightArm","pivot":[0,24,0],"cubes":[{"origin":[0,0,0],"size":[0.1,0.1,0.1],"uv":[0,0]}]}
	]}]}`)

	var want []string
	for i := 0; i < 20; i++ {
		var got []string
		for _, v := range ValidateGeometrySize(geom, DefaultMinGeometrySize).Violations {
			got = append(got, v.Bone)
		}
		if want == nil {
			want = got
			continue
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("violation order changed on run %d:\n got %v\nwant %v", i, got, want)
			}
		}
	}
}

// PartTiny was documented but unreachable: suppressing a tiny bone zeroed its
// Fraction, so it arrived indistinguishable from a transparent part.
func TestPartTinyIsReported(t *testing.T) {
	geom := []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"g","texture_width":64,"texture_height":64},"bones":[
		{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[0,24,0],"size":[0.1,0.1,0.1],"uv":[0,0]}]},
		{"name":"body","pivot":[0,24,0],"cubes":[{"origin":[-4,12,-2],"size":[8,12,4],"uv":[16,16]}]}
	]}]}`)

	rep := NewSkin(makeTexture(255), geom).Report()

	var head *PartReport
	for i := range rep.Parts {
		if rep.Parts[i].Name == "head" {
			head = &rep.Parts[i]
		}
	}
	if head == nil {
		t.Fatal("no head part in report")
	}
	if head.Visibility != PartTiny {
		t.Errorf("head visibility = %s, want tiny (texture is opaque, geometry is 0.1 units)", head.Visibility)
	}

	// A tiny standard part still counts as invisible for the summary list.
	found := false
	for _, n := range rep.Invisible {
		if n == "head" {
			found = true
		}
	}
	if !found {
		t.Errorf("Invisible = %v, want it to include the tiny head", rep.Invisible)
	}
}

// One detection run must parse the geometry once. It used to unmarshal the
// same document three times — for the part scan, for the has-geometry gate,
// and again inside ValidateGeometrySize.
func BenchmarkSkinReportWithGeometry(b *testing.B) {
	geom, err := os.ReadFile(filepath.Join("testdata", "bench-skin", "geometry.json"))
	if err != nil {
		b.Skipf("bench fixture absent: %v", err)
	}
	tex := makeTexture(255)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewSkin(tex, geom).Report()
	}
}

func BenchmarkSkinReportNoGeometry(b *testing.B) {
	tex := makeTexture(255)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewSkin(tex, nil).Report()
	}
}

// An empty Parts must marshal as [] rather than null, so the report's JSON
// shape does not depend on whether it happened to find any parts.
func TestSkinReportEmptySlicesMarshalAsArrays(t *testing.T) {
	data, err := json.Marshal(NewSkin(nil, nil).Report())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"Parts":[]`, `"Invisible":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report JSON missing %s:\n%s", want, data)
		}
	}

	// And a populated report still carries its entries.
	full, err := json.Marshal(NewSkin(makeTexture(255), nil).Report())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(full), `"Parts":[]`) {
		t.Errorf("populated report lost its parts:\n%s", full)
	}
}
