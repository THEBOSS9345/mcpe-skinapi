package skinapi

import (
	"bytes"
	"encoding/json"
)

// Bone and Cube mirror Bedrock's geometry.json schema. UV stays raw JSON
// because a cube's "uv" field is either a [u,v] pair or a per-face object,
// resolved in mesh.go. See docs/geometry-format.md.
type Bone struct {
	Name     string    `json:"name"`
	Parent   string    `json:"parent,omitempty"`
	Pivot    []float64 `json:"pivot,omitempty"`
	Rotation []float64 `json:"rotation,omitempty"`
	Inflate  float64   `json:"inflate,omitempty"`
	Cubes    []Cube    `json:"cubes,omitempty"`
}

type Cube struct {
	Origin  []float64       `json:"origin"`
	Size    []float64       `json:"size"`
	UV      json.RawMessage `json:"uv"`
	Inflate *float64        `json:"inflate,omitempty"`
	Mirror  bool            `json:"mirror,omitempty"`
}

// Geometry is one normalized model - a body, a cape - regardless of which of
// Bedrock's two wire formats it came from. See ParseGeometry.
type Geometry struct {
	Identifier    string
	TextureWidth  float64
	TextureHeight float64
	Bones         []Bone
}

// BoneByName returns the bone with the given name, and whether it exists.
func (g *Geometry) BoneByName(name string) (Bone, bool) {
	for _, b := range g.Bones {
		if b.Name == name {
			return b, true
		}
	}
	return Bone{}, false
}

// IsEmpty reports whether raw carries no geometry at all: either nothing, or
// the literal JSON null a Bedrock client sends for a skin whose model is
// built into the client. Both mean "no mesh supplied", not "broken upload" -
// use it to tell those apart before calling ParseGeometry.
//
// See docs/skin-data.md#most-skins-send-no-geometry-at-all.
func IsEmpty(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// Complexity reports total bones and cubes across every entry in geos, which
// is roughly what mesh-building costs. It sums the whole document, not just
// the entry that will be rendered, because SelectGeometry's fallback and
// FindCape both walk all of it.
//
// The library enforces no limit itself - what counts as too large is policy.
// See docs/recipes.md#handling-untrusted-uploads.
func Complexity(geos []Geometry) (bones, cubes int) {
	for _, g := range geos {
		bones += len(g.Bones)
		cubes += g.TotalCubes()
	}
	return bones, cubes
}

// TotalCubes is the number of cubes across every bone in the entry. Zero
// means the entry carries no mesh data at all, which is exactly what a
// persona skin looks like: real bones, no cubes.
func (g *Geometry) TotalCubes() int {
	n := 0
	for _, b := range g.Bones {
		n += len(b.Cubes)
	}
	return n
}

// modernGeometryDoc is Bedrock's format_version >= 1.12.0 shape.
type modernGeometryDoc struct {
	MinecraftGeometry []struct {
		Description struct {
			Identifier    string  `json:"identifier"`
			TextureWidth  float64 `json:"texture_width"`
			TextureHeight float64 `json:"texture_height"`
		} `json:"description"`
		Bones []Bone `json:"bones"`
	} `json:"minecraft:geometry"`
}

// legacyGeometryEntryRaw is one entry of Bedrock's pre-1.12 flat format: the
// identifier is a top-level key, and texture dimensions lose their
// underscores. See docs/geometry-format.md#legacy-pre-112.
type legacyGeometryEntryRaw struct {
	TextureWidth  float64 `json:"texturewidth"`
	TextureHeight float64 `json:"textureheight"`
	Bones         []Bone  `json:"bones"`
}

// ParseGeometry parses raw into normalized entries, detecting whichever of
// Bedrock's two formats it is - bone and cube fields are identical between
// them, only the wrapper differs.
//
// Valid JSON carrying no geometry, including the literal "null" a client
// sends for a built-in model, returns zero entries and no error. An error
// means genuinely malformed input.
func ParseGeometry(raw []byte) ([]Geometry, error) {
	var modern modernGeometryDoc
	if err := json.Unmarshal(raw, &modern); err == nil && len(modern.MinecraftGeometry) > 0 {
		out := make([]Geometry, len(modern.MinecraftGeometry))
		for i, g := range modern.MinecraftGeometry {
			out[i] = Geometry{
				Identifier:    g.Description.Identifier,
				TextureWidth:  g.Description.TextureWidth,
				TextureHeight: g.Description.TextureHeight,
				Bones:         g.Bones,
			}
		}
		return out, nil
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	var out []Geometry
	for key, val := range top {
		if key == "format_version" {
			continue
		}
		var entry legacyGeometryEntryRaw
		if err := json.Unmarshal(val, &entry); err != nil || len(entry.Bones) == 0 {
			continue
		}
		out = append(out, Geometry{
			Identifier:    key,
			TextureWidth:  entry.TextureWidth,
			TextureHeight: entry.TextureHeight,
			Bones:         entry.Bones,
		})
	}
	return out, nil
}

// SelectGeometry picks the entry matching identifier, falling back to the one
// with the most cubes when identifier is empty or matches nothing.
//
// The fallback is deliberately not geos[0]: bundles commonly list the sparse
// cape entry first, so taking the first would select it and leave every
// head-scoped view empty. See docs/design-decisions.md#why-select-by-cube-count.
func SelectGeometry(geos []Geometry, identifier string) (Geometry, bool) {
	if identifier != "" {
		for _, g := range geos {
			if g.Identifier == identifier {
				return g, true
			}
		}
	}
	if len(geos) > 0 {
		best := geos[0]
		for _, g := range geos[1:] {
			if g.TotalCubes() > best.TotalCubes() {
				best = g
			}
		}
		return best, true
	}
	return Geometry{}, false
}

// FindCape returns the entry holding a bone literally named "cape" that has
// a cube. Capes always live in their own entry, never merged into the body.
func FindCape(geos []Geometry) (Geometry, bool) {
	for _, g := range geos {
		if b, ok := g.BoneByName("cape"); ok && len(b.Cubes) > 0 {
			return g, true
		}
	}
	return Geometry{}, false
}
