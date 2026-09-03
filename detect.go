package skinapi

import (
	"encoding/json"
	"fmt"
	"image"
	"sync"
)

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

// MarshalJSON encodes the level as its lowercase name rather than its integer
// value, so a SkinReport handed straight to json.Marshal reads as
// "visibility":"invisible" instead of "visibility":1.
func (p PartVisibility) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// UnmarshalJSON accepts the names MarshalJSON produces, and the bare integers
// an older client may still send.
func (p *PartVisibility) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		switch name {
		case "visible":
			*p = PartVisible
		case "invisible":
			*p = PartInvisible
		case "suspicious":
			*p = PartSuspicious
		case "tiny":
			*p = PartTiny
		default:
			return fmt.Errorf("skinapi: unknown part visibility %q", name)
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("skinapi: part visibility: %w", err)
	}
	if n < int(PartVisible) || n > int(PartTiny) {
		return fmt.Errorf("skinapi: part visibility %d out of range", n)
	}
	*p = PartVisibility(n)
	return nil
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
//
// A Skin must not be copied after first use, and must be used through the
// pointer NewSkin returns.
type Skin struct {
	texture  image.Image
	geomData []byte
	th       thresholds

	once   sync.Once
	report SkinReport
}

// SkinOptions tunes the thresholds one Skin judges by. A zero field takes its
// Default* value, so the zero SkinOptions is exactly what NewSkin uses.
//
// The knobs exist because what counts as unacceptable is policy, the same
// reasoning that keeps size and complexity limits out of the library. A
// lobby that only ever shows head icons might care solely about the head; a
// strict server might demand every part be near-opaque.
type SkinOptions struct {
	// MinVisibleFraction is the share of a part's sampled pixels that must be
	// opaque for it to count as visible. Zero means DefaultMinVisibleFraction.
	MinVisibleFraction float64

	// MinGeometrySize is the world-space size a bone must reach to avoid
	// being judged too small to see. Zero means DefaultMinGeometrySize. It
	// applies only when geometry is supplied.
	MinGeometrySize float64

	// MinVisibleParts is how many of the six standard body parts must be
	// visible before the skin stops being suspicious. Zero means
	// DefaultMinVisibleParts.
	MinVisibleParts int
}

// NewSkin builds a Skin for analysis with the default thresholds. geometry is
// raw geometry.json or nil.
func NewSkin(texture image.Image, geometry []byte) *Skin {
	return NewSkinWithOptions(texture, geometry, SkinOptions{})
}

// NewSkinWithOptions builds a Skin that judges by opts rather than the
// defaults. It is otherwise identical to NewSkin, caching and all.
func NewSkinWithOptions(texture image.Image, geometry []byte, opts SkinOptions) *Skin {
	return &Skin{
		texture:  texture,
		geomData: geometry,
		th: thresholds{
			minVisibleFraction: opts.MinVisibleFraction,
			minGeometrySize:    opts.MinGeometrySize,
			minVisibleParts:    opts.MinVisibleParts,
		}.resolved(),
	}
}

// Report runs the full visibility analysis and returns the breakdown.
//
// The result is computed once and reused, so asking several questions of one
// Skin costs a single pass over the texture. It is safe to call concurrently
// from multiple goroutines, which a service sharing one Skin across requests
// will do. Each call returns its own copy of the slices, so a caller is free
// to sort or filter them without disturbing the cached report.
func (s *Skin) Report() SkinReport {
	s.once.Do(func() { s.report = s.analyze() })
	return s.report.clone()
}

// clone returns a copy that shares no slice backing with the receiver.
func (r SkinReport) clone() SkinReport {
	out := r
	out.Parts = append([]PartReport(nil), r.Parts...)
	out.Invisible = append([]string(nil), r.Invisible...)
	return out
}

func (s *Skin) analyze() SkinReport {
	base := validateWith(s.texture, s.geomData, s.th)
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
			Visibility:  partVisibility(p, s.th),
		}
		rep.Parts = append(rep.Parts, pr)
		// PartTiny counts as invisible here: a bone too small to see does not
		// render, whatever its texture says.
		if standardPartSet[p.Name] && (pr.Visibility == PartInvisible || pr.Visibility == PartTiny) {
			rep.Invisible = append(rep.Invisible, p.Name)
		}
	}
	return rep
}

// partVisibility maps an internal SkinPartResult to the public PartVisibility so
// callers can tell "too small" (tiny) apart from "transparent" (invisible).
//
// Tiny is checked first and comes from the geometry-size pass, not the pixel
// fraction. Reading it off Fraction alone could not work: suppressing a tiny
// bone zeroes its Fraction, so every tiny part arrived here indistinguishable
// from a fully transparent one and PartTiny was never returned at all.
func partVisibility(p SkinPartResult, th thresholds) PartVisibility {
	th = th.resolved()
	if p.Tiny {
		return PartTiny
	}
	if !p.Visible {
		if p.Fraction > 0 && p.Fraction < th.minVisibleFraction {
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
