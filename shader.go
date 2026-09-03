package skinapi

import (
	"image"
	"math"

	"github.com/fogleman/fauxgl"
)

// fastImageTexture is an allocation-free fauxgl.Texture for the image types a
// skin actually is (*image.NRGBA in every path: DecodeImage normally yields
// it and TextureFromRGBA always does). fauxgl's stock ImageTexture calls
// image.Image.At once per sampled pixel, which boxes a fresh color value into
// an interface for every fragment - that single interface allocation is over
// 90% of all allocations in a render. Sampling the raw Pix slice directly
// removes it completely.
//
// Every skin the library decodes is *image.NRGBA (or TextureFromRGBA's NRGBA),
// so the common path aliases Pix with zero copies and zero per-pixel
// allocation. Any other image type is converted once, up front, into the same
// packed RGBA layout; sampling then never touches the generic image
// interface either.
type fastImageTexture struct {
	width, height int
	pix           []uint8 // packed R,G,B,A per pixel, straight (unpremultiplied) alpha
}

func newFastImageTexture(im image.Image) fauxgl.Texture {
	if n, ok := im.(*image.NRGBA); ok {
		b := n.Bounds()
		if n.Stride == b.Dx()*4 && b.Min.X == 0 && b.Min.Y == 0 {
			return &fastImageTexture{width: b.Dx(), height: b.Dy(), pix: n.Pix}
		}
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := make([]uint8, 0, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
			pix = append(pix, uint8(r>>8), uint8(g>>8), uint8(bl>>8), uint8(a>>8))
		}
	}
	return &fastImageTexture{width: w, height: h, pix: pix}
}

// Sample replicates fauxgl.ImageTexture.Sample's coordinate handling exactly -
// including its internal v=1-v, which cancels the V flip applied in mesh.go.
// The alpha is straight and each channel is byte/255, matching what
// MakeColor(At(...).RGBA()) produces for the byte values of NRGBA.
func (t *fastImageTexture) Sample(u, v float64) fauxgl.Color {
	v = 1 - v
	u -= math.Floor(u)
	v -= math.Floor(v)
	x := int(u * float64(t.width))
	y := int(v * float64(t.height))
	i := (y*t.width + x) * 4
	p := t.pix
	return fauxgl.Color{
		R: float64(p[i]) / 255,
		G: float64(p[i+1]) / 255,
		B: float64(p[i+2]) / 255,
		A: float64(p[i+3]) / 255,
	}
}

// BilinearSample is unused by the alpha-test shader (which samples with
// nearest-neighbour), but fauxgl.Texture requires it. It matches fauxgl's
// behaviour so a future shader that switches to bilinear gets the same image
// as the stock texture would.
func (t *fastImageTexture) BilinearSample(u, v float64) fauxgl.Color {
	v = 1 - v
	u -= math.Floor(u)
	v -= math.Floor(v)
	w, h := t.width, t.height
	x := u * float64(w-1)
	y := v * float64(h-1)
	x0, y0 := int(x), int(y)
	x1, y1 := x0+1, y0+1
	tx, ty := x-float64(x0), y-float64(y0)
	c00 := t.at(x0, y0)
	c01 := t.at(x0, y1)
	c10 := t.at(x1, y0)
	c11 := t.at(x1, y1)
	return fauxgl.Color{
		R: lerp(lerp(c00.R, c10.R, tx), lerp(c01.R, c11.R, tx), ty),
		G: lerp(lerp(c00.G, c10.G, tx), lerp(c01.G, c11.G, tx), ty),
		B: lerp(lerp(c00.B, c10.B, tx), lerp(c01.B, c11.B, tx), ty),
		A: lerp(lerp(c00.A, c10.A, tx), lerp(c01.A, c11.A, tx), ty),
	}
}

func (t *fastImageTexture) at(x, y int) fauxgl.Color {
	if x < 0 {
		x = 0
	} else if x >= t.width {
		x = t.width - 1
	}
	if y < 0 {
		y = 0
	} else if y >= t.height {
		y = t.height - 1
	}
	i := (y*t.width + x) * 4
	p := t.pix
	return fauxgl.Color{
		R: float64(p[i]) / 255,
		G: float64(p[i+1]) / 255,
		B: float64(p[i+2]) / 255,
		A: float64(p[i+3]) / 255,
	}
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// alphaTestTextureShader samples a texture unlit, matching Minecraft's flat
// skin rendering, and discards fragments below the alpha threshold - colour
// and depth both. Discarding rather than blending is what lets the inflated
// overlay layer (hat, jacket, sleeves, pants) show the body underneath.
//
// See docs/rendering-pipeline.md#alpha-testing.
type alphaTestTextureShader struct {
	Matrix    fauxgl.Matrix
	Texture   fauxgl.Texture
	Threshold float64
}

func newAlphaTestTextureShader(matrix fauxgl.Matrix, texture fauxgl.Texture) *alphaTestTextureShader {
	return &alphaTestTextureShader{Matrix: matrix, Texture: texture, Threshold: 0.5}
}

func (s *alphaTestTextureShader) Vertex(v fauxgl.Vertex) fauxgl.Vertex {
	v.Output = s.Matrix.MulPositionW(v.Position)
	return v
}

func (s *alphaTestTextureShader) Fragment(v fauxgl.Vertex) fauxgl.Color {
	// Nearest-neighbour, not bilinear: atlas regions are packed edge to edge
	// with no padding. See docs/rendering-pipeline.md for why that matters.
	c := s.Texture.Sample(v.Texture.X, v.Texture.Y)
	if c.A < s.Threshold {
		return fauxgl.Discard
	}
	return c
}
