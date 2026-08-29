package skinapi

import "github.com/fogleman/fauxgl"

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
