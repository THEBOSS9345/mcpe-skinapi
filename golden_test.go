package skinapi

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// Nothing else in this suite pins actual pixels. The existing render tests
// assert that output changes when it should — a different identifier produces
// a different image, a cape makes a difference — which catches a renderer that
// stops responding to its inputs but not one that quietly starts drawing
// something wrong. A regression in the UV flip, the face corner mapping or the
// camera math would leave every one of them passing.
//
// These fixtures are rendered from the procedural test texture, so no
// third-party skin artwork enters the repo.
//
// Regenerate after an intentional rendering change with:
//
//	go test -run TestGoldenRenders -update
//
// and say in the commit what you verified the new images against. "It looks
// right" has been wrong here before.
var updateGolden = flag.Bool("update", false, "rewrite the golden render fixtures")

const goldenDir = "testdata/golden"

// goldenTolerance is the per-channel difference two renders may show before
// they count as different. Rasterization is float64 throughout and IEEE
// arithmetic is reproducible, so exact equality is the norm; the slack exists
// so a Go release that reassociates a floating-point expression reports a
// visible regression rather than a red build over one least-significant bit.
const goldenTolerance = 2

func goldenCases() map[string]Options {
	tex := testTexture()
	return map[string]Options{
		"body-front":   {Texture: tex, View: ViewBody, Angle: AngleFront, Size: 96},
		"body-iso":     {Texture: tex, View: ViewBody, Angle: AngleIso, Size: 96},
		"chest-front":  {Texture: tex, View: ViewChest, Size: 96},
		"head-default": {Texture: tex, View: ViewHead, Size: 96},
		"avatar":       {Texture: tex, View: ViewAvatar, Size: 96},
		"slim":         {Texture: tex, Identifier: "geometry.humanoid.customSlim", Size: 96},
		"body-cape":    {Texture: tex, View: ViewBody, Cape: tex, Size: 96},
		"parts-head-arm": {
			Texture: tex,
			Parts:   []string{"head", "leftArm"},
			Size:    96,
		},
		"camera-explicit": {
			Texture: tex,
			Camera:  &Camera{Yaw: 200, Pitch: -15, FOV: 50, Margin: 1.2},
			Size:    96,
		},
	}
}

func TestGoldenRenders(t *testing.T) {
	for name, opts := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			got, err := Render(opts)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			path := filepath.Join(goldenDir, name+".png")

			if *updateGolden {
				writeGolden(t, path, got)
				return
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (regenerate with -update): %v", err)
			}
			want, err := DecodeImage(raw)
			if err != nil {
				t.Fatalf("decode golden: %v", err)
			}
			if diff := compareImages(got, want); diff != "" {
				t.Errorf("render no longer matches %s: %s\n"+
					"If the change is intended, regenerate with -update and say what you verified it against.",
					path, diff)
			}
		})
	}
}

func writeGolden(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := EncodePNG(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	t.Logf("wrote %s", path)
}

// compareImages returns a description of how two renders differ, or "" when
// they match within goldenTolerance.
func compareImages(got, want image.Image) string {
	gb, wb := got.Bounds(), want.Bounds()
	if gb.Dx() != wb.Dx() || gb.Dy() != wb.Dy() {
		return fmt.Sprintf("size %dx%d, want %dx%d", gb.Dx(), gb.Dy(), wb.Dx(), wb.Dy())
	}

	differing, maxDelta := 0, 0
	var firstX, firstY int
	for y := 0; y < gb.Dy(); y++ {
		for x := 0; x < gb.Dx(); x++ {
			gr, gg, gbl, ga := got.At(gb.Min.X+x, gb.Min.Y+y).RGBA()
			wr, wg, wbl, wa := want.At(wb.Min.X+x, wb.Min.Y+y).RGBA()
			delta := 0
			for _, d := range [4]int{
				abs(int(gr>>8) - int(wr>>8)),
				abs(int(gg>>8) - int(wg>>8)),
				abs(int(gbl>>8) - int(wbl>>8)),
				abs(int(ga>>8) - int(wa>>8)),
			} {
				if d > delta {
					delta = d
				}
			}
			if delta > goldenTolerance {
				if differing == 0 {
					firstX, firstY = x, y
				}
				differing++
			}
			if delta > maxDelta {
				maxDelta = delta
			}
		}
	}
	if differing == 0 {
		return ""
	}
	total := gb.Dx() * gb.Dy()
	return fmt.Sprintf("%d of %d pixels differ (%.2f%%), largest channel delta %d, first at (%d,%d)",
		differing, total, 100*float64(differing)/float64(total), maxDelta, firstX, firstY)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
