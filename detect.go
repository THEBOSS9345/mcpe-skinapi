package skinapi

import "image"

// PartVisibility is the visibility classification of a single body part.
type PartVisibility int

const (
	// PartVisible means the part has enough opaque pixels to render normally.
	PartVisible PartVisibility = iota
	// PartInvisible means the part has no usable opaque pixels.
	PartInvisible
	// PartSuspicious means the part is partially visible but below
	// DefaultMinVisibleFraction.
	PartSuspicious
	// PartTiny means the geometry defines the part below DefaultMinGeometrySize.
	PartTiny
)

// String returns a stable lowercase name for the visibility level.
func (p PartVisibility) String() string {
	switch p {
	case PartInvisible:
		return "invisible"
	case PartSuspicious:
		return "suspicious"
	case PartTiny:
		return "tiny"
	default:
		return "visible"
	}
}

// PartReport describes the visibility of one body part after analysis.
type PartReport struct {
	// Name is the body part or overlay/geometry bone name (head, hat, cape, ...).
	Name string
	// Visibility is PartVisible, PartInvisible, PartSuspicious or PartTiny.
	Visibility PartVisibility
	// Visible is true when the part is neither invisible nor missing.
	Visible bool
	// Fraction is the fraction of the part's sampled pixels that are opaque (0-1).
	Fraction float64
	// Pixels and Transparent count the sampled pixels for this part.
	Pixels      int
	Transparent int
	// FromGeo is true when this part came from real geometry cube UVs rather
	// than the standard-layout fallback.
	FromGeo bool
}

// SkinReport is the result of analyzing a skin for invisible or suspicious
// body parts. It is safe to marshal straight to JSON from an API.
type SkinReport struct {
	// Pass is true when the skin is acceptable (not invisible).
	Pass bool
	// IsInvisible is true when the whole skin is effectively invisible (no
	// body part renders, or only a stray limb renders).
	IsInvisible bool
	// IsSuspicious is true for a half-invisible skin.
	IsSuspicious bool
	// VisibleParts and InvisibleParts count the six standard body parts.
	VisibleParts   int
	InvisibleParts int
	// Parts is the per-part breakdown: six standard parts first, then overlays.
	Parts []PartReport
	// Invisible lists the names of the invisible standard body parts.
	Invisible []string
}

// Skin bundles a decoded texture with its optional geometry so callers can
// question body-part visibility in one place.
type Skin struct {
	texture  image.Image
	geomData []byte
}

// NewSkin builds a Skin for analysis. geometry is raw geometry.json or nil.
func NewSkin(texture image.Image, geometry []byte) *Skin {
	return &Skin{texture: texture, geomData: geometry}
}

// Report runs the full visibility analysis and returns the breakdown. The
// result is cached: subsequent calls on the same Skin return the same report.
func (s *Skin) Report() SkinReport {
	base := ValidateSkinInvisibility(s.texture, s.geomData)
	rep := SkinReport{
		Pass:           base.Pass,
		IsInvisible:    base.IsInvisible,
		IsSuspicious:   base.Suspicious,
		VisibleParts:   base.VisibleParts,
		InvisibleParts: base.InvisibleParts,
		Parts:          make([]PartReport, 0, len(base.Parts)),
	}
	for _, p := range base.Parts {
		pr := PartReport{
			Name:        p.Name,
			Visible:     p.Visible,
			Fraction:    p.Fraction,
			Pixels:      p.Pixels,
			Transparent: p.Transparent,
			FromGeo:     p.FromGeo,
			Visibility:  partVisibility(p),
		}
		rep.Parts = append(rep.Parts, pr)
		if pr.Visibility == PartInvisible && standardPartSet[p.Name] {
			rep.Invisible = append(rep.Invisible, p.Name)
		}
	}
	return rep
}

// partVisibility maps an internal SkinPartResult to the public PartVisibility so
// callers can tell "too small" (tiny) apart from "transparent" (invisible).
func partVisibility(p SkinPartResult) PartVisibility {
	if !p.Visible {
		if p.Fraction > 0 && p.Fraction < DefaultMinVisibleFraction {
			return PartSuspicious
		}
		return PartInvisible
	}
	return PartVisible
}

// Parts returns the per-part breakdown, equivalent to Report().Parts.
func (s *Skin) Parts() []PartReport {
	return s.Report().Parts
}

// IsInvisible reports whether the whole skin is effectively invisible.
func (s *Skin) IsInvisible() bool {
	return s.Report().IsInvisible
}

// IsSuspicious reports whether the skin is half-invisible (not fully
// invisible, but several body parts are missing).
func (s *Skin) IsSuspicious() bool {
	return s.Report().IsSuspicious
}

// InvisibleParts returns the names of the invisible body parts.
func (s *Skin) InvisibleParts() []string {
	return s.Report().Invisible
}
