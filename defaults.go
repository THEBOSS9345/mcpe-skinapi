package skinapi

import (
	_ "embed"
	"fmt"
)

// defaultGeometryJSON is the vanilla humanoid geometry bundle, captured
// verbatim from a real Bedrock client rather than hand-authored. It holds the
// three entries a stock client ships with: "geometry.humanoid.custom" (wide
// arms), "geometry.humanoid.customSlim" (slim arms), and "geometry.cape".
//
//go:embed default_geometry.json
var defaultGeometryJSON []byte

// defaultGeometry is parsed once at package init and shared: rendering never
// mutates a Geometry, so there is no reason to re-parse 16KB per render.
var defaultGeometry = mustParseDefault()

func mustParseDefault() []Geometry {
	geos, err := ParseGeometry(defaultGeometryJSON)
	if err != nil {
		// Compiled into the binary, so this cannot fail on anything a caller
		// did - it means the library was built broken.
		panic(fmt.Sprintf("skinapi: embedded default_geometry.json is invalid: %v", err))
	}
	if len(geos) == 0 {
		panic("skinapi: embedded default_geometry.json produced no geometry entries")
	}
	return geos
}

// DefaultGeometry returns the standard vanilla humanoid geometry: the wide
// ("geometry.humanoid.custom") and slim ("geometry.humanoid.customSlim") body
// variants, plus "geometry.cape". Set Options.Identifier to choose between
// them; with no identifier the wide variant wins.
//
// This is the right model for most real skins, not merely a fallback: a
// Bedrock client sends no mesh at all for a skin using a built-in model.
// See docs/skin-data.md#most-skins-send-no-geometry-at-all.
//
// The returned slice is shared across calls and must not be modified.
func DefaultGeometry() []Geometry {
	return defaultGeometry
}
