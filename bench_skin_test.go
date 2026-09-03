package skinapi

import (
	"image"
	"os"
	"path/filepath"
	"testing"
)

// loadBenchSkin loads the committed, scrubbed benchmark skin: a vanilla 3D
// humanoid (25 cubes) built from a real captured geometry + texture, with no
// identity data. It lives in testdata/bench-skin/, which is safe to commit,
// unlike testdata/captures/.
func loadBenchSkin(b *testing.B) (image.Image, []Geometry) {
	b.Helper()
	dir := filepath.Join("testdata", "bench-skin")

	texBytes, err := os.ReadFile(filepath.Join(dir, "texture.png"))
	if err != nil {
		b.Fatalf("read bench texture: %v", err)
	}
	tex, err := DecodeImage(texBytes)
	if err != nil {
		b.Fatalf("decode bench texture: %v", err)
	}

	geoBytes, err := os.ReadFile(filepath.Join(dir, "geometry.json"))
	if err != nil {
		b.Fatalf("read bench geometry: %v", err)
	}
	geos, err := ParseGeometry(geoBytes)
	if err != nil {
		b.Fatalf("parse bench geometry: %v", err)
	}

	return tex, geos
}

// BenchmarkRenderHead times a single head render of the committed 3D skin at
// the default 512px output size.
func BenchmarkRenderHead(b *testing.B) {
	tex, geos := loadBenchSkin(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(Options{Texture: tex, Geometry: geos, View: ViewHead}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderBody times a single full-body render of the committed 3D skin
// at the default 512px output size.
func BenchmarkRenderBody(b *testing.B) {
	tex, geos := loadBenchSkin(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(Options{Texture: tex, Geometry: geos, View: ViewBody}); err != nil {
			b.Fatal(err)
		}
	}
}
