package skinapi

import (
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// Verifies fastImageTexture.Sample matches fauxgl.ImageTexture.Sample exactly
// across the UV domain for an *image.NRGBA, so the orientation flip and
// coordinate handling produce identical output to the stock texture.
func TestFastTextureMatchesFauxgl(t *testing.T) {
	tex := makeTexture(255)
	for i := 0; i < len(tex.Pix); i += 4 {
		tex.Pix[i] = uint8(i * 7 % 255)
		tex.Pix[i+1] = uint8(i * 3 % 255)
		tex.Pix[i+2] = uint8(i * 11 % 255)
	}
	fast := newFastImageTexture(tex)
	old := fauxgl.NewImageTexture(tex)
	for i := 0; i < 5000; i++ {
		u := math.Mod(float64(i)*0.731, 2.0)
		v := math.Mod(float64(i)*0.337, 2.0)
		a := fast.Sample(u, v)
		b := old.Sample(u, v)
		if math.Abs(a.R-b.R) > 1e-6 || math.Abs(a.G-b.G) > 1e-6 ||
			math.Abs(a.B-b.B) > 1e-6 || math.Abs(a.A-b.A) > 1e-6 {
			t.Fatalf("mismatch at u=%f v=%f: fast=%+v old=%+v", u, v, a, b)
		}
	}
}

// A render through the fast texture must be free of the per-fragment
// allocations that dominated the old path. Loose bound: well under a tenth of
// the previous ~79k allocations at 512x512.
func TestFastTextureRenderFewerAllocs(t *testing.T) {
	tex := makeTexture(255)
	before := testing.AllocsPerRun(5, func() {
		if _, err := Render(Options{Texture: tex, Size: 512}); err != nil {
			t.Fatal(err)
		}
	})
	if before > 15000 {
		t.Errorf("Render still allocates %d times per run; fast texture not effective", int(before))
	}
	t.Logf("allocations per 512 render: %d", int(before))
}
