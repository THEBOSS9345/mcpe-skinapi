package skinapi

import "testing"

func BenchmarkRenderBody512(b *testing.B) {
	tex := testTexture()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(Options{Texture: tex, Size: 512}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderAvatar128(b *testing.B) {
	tex := testTexture()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(Options{Texture: tex, View: ViewAvatar, Size: 128}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderParallel approximates a server under load: many independent
// renders in flight at once.
func BenchmarkRenderParallel(b *testing.B) {
	tex := testTexture()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := Render(Options{Texture: tex, Size: 512}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
