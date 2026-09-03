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
	// PartSuspicious means the part is partially visible but below the
	// minimum visible fraction.
	PartSuspicious
	// PartTiny means the geometry defines the part below the minimum size.
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

// Verdict is the overall judgement on a skin.
//
// It is one value with a fixed set of states rather than the three
// independent booleans this used to carry (Pass, IsInvisible, IsSuspicious),
// which could express five combinations that mean nothing - invisible and
// suspicious at once, or passing while invisible.
type Verdict int

const (
	// VerdictUnknown is the zero value: no analysis has been run. It exists
	// so an uninitialized SkinReport does not claim a skin is fine. OK
	// reports false for it, so the zero value fails closed.
	VerdictUnknown Verdict = iota
	// VerdictOK means the skin renders normally.
	VerdictOK
	// VerdictSuspicious means some standard body parts do not render, but
	// enough do that the skin is not simply invisible. A soft signal: worth
	// logging or reviewing, not necessarily worth rejecting.
	VerdictSuspicious
	// VerdictInvisible means nothing renders, or only a stray limb does.
	VerdictInvisible
)

// String returns a stable lowercase name for the verdict.
func (v Verdict) String() string {
	switch v {
	case VerdictOK:
		return "ok"
	case VerdictSuspicious:
		return "suspicious"
	case VerdictInvisible:
		return "invisible"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the verdict as its lowercase name.
func (v Verdict) MarshalJSON() ([]byte, error) { return json.Marshal(v.String()) }

// UnmarshalJSON accepts the names MarshalJSON produces.
func (v *Verdict) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("skinapi: verdict: %w", err)
	}
	switch name {
	case "unknown":
		*v = VerdictUnknown
	case "ok":
		*v = VerdictOK
	case "suspicious":
		*v = VerdictSuspicious
	case "invisible":
		*v = VerdictInvisible
	default:
		return fmt.Errorf("skinapi: unknown verdict %q", name)
	}
	return nil
}

// PartReport describes the visibility of one body part after analysis.
type PartReport struct {
	// Name is the body part or overlay/geometry bone name (head, hat, cape, ...).
	Name string `json:"name"`
	// Visibility is why this part counts as rendering or not. Visible reports
	// the same thing as a bool when the distinction does not matter.
	Visibility PartVisibility `json:"visibility"`
	// OpaqueRatio is the share of the part's sampled pixels that are opaque,
	// from 0 to 1.
	OpaqueRatio float64 `json:"opaque_ratio"`
	// Pixels is how many texture pixels were sampled for this part, and
	// Transparent how many of those were see-through.
	Pixels      int `json:"pixels"`
	Transparent int `json:"transparent_pixels"`
	// FromGeometry is true when this part was resolved from real geometry
	// cube UVs rather than the standard-layout fallback.
	FromGeometry bool `json:"from_geometry"`
}

// Visible reports whether the part renders. It is derived from Visibility
// rather than stored, so the two can never disagree.
func (p PartReport) Visible() bool { return p.Visibility == PartVisible }

// SkinReport is the result of analyzing a skin for invisible or suspicious
// body parts. It is safe to marshal straight to JSON from an API.
//
// Everything derivable is a method rather than a field, so there is exactly
// one place each fact is stated and no way for the JSON to contradict itself.
type SkinReport struct {
	// Verdict is the overall judgement.
	Verdict Verdict `json:"verdict"`
	// VisibleParts is how many of the standard body parts render, out of
	// TotalParts. Overlay layers fold into the part they cover and
	// accessories are ignored, so an opaque cape cannot mask an invisible
	// body.
	VisibleParts int `json:"visible_parts"`
	TotalParts   int `json:"total_parts"`
	// Parts is the per-part breakdown: the standard body parts first, in a
	// fixed order, then every other bone sorted by name.
	Parts []PartReport `json:"parts"`
}

// OK reports whether the skin is acceptable. It is false for a zero
// SkinReport, so a report that was never filled in does not read as a pass.
func (r SkinReport) OK() bool { return r.Verdict == VerdictOK }

// InvisibleParts returns the names of the standard body parts that do not
// render, in the order they appear in Parts. It is derived from Parts on each
// call rather than stored.
func (r SkinReport) InvisibleParts() []string {
	out := []string{}
	for _, p := range r.Parts {
		if standardPartSet[p.Name] && !p.Visible() {
			out = append(out, p.Name)
		}
	}
	return out
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
//
// An empty slice stays empty rather than becoming nil: appending to a nil
// slice yields nil when there is nothing to copy, which would marshal an
// empty Parts as JSON null instead of [] and make the report's shape depend
// on whether it happened to find any parts.
func (r SkinReport) clone() SkinReport {
	out := r
	out.Parts = make([]PartReport, len(r.Parts))
	copy(out.Parts, r.Parts)
	return out
}

func (s *Skin) analyze() SkinReport {
	base := validateWith(s.texture, s.geomData, s.th)
	rep := SkinReport{
		Verdict:      verdictOf(base),
		VisibleParts: base.VisibleParts,
		TotalParts:   len(standardPartNames),
		Parts:        make([]PartReport, 0, len(base.Parts)),
	}
	for _, p := range base.Parts {
		rep.Parts = append(rep.Parts, PartReport{
			Name:         p.Name,
			Visibility:   partVisibility(p, s.th),
			OpaqueRatio:  p.Fraction,
			Pixels:       p.Pixels,
			Transparent:  p.Transparent,
			FromGeometry: p.FromGeo,
		})
	}
	return rep
}

// verdictOf collapses the internal result's booleans into the single value
// the public report carries.
func verdictOf(r SkinVisibilityResult) Verdict {
	switch {
	case r.IsInvisible:
		return VerdictInvisible
	case r.Suspicious:
		return VerdictSuspicious
	default:
		return VerdictOK
	}
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
	return s.Report().Verdict == VerdictInvisible
}

// IsSuspicious reports whether the skin is half-invisible (not fully
// invisible, but several body parts are missing).
func (s *Skin) IsSuspicious() bool {
	return s.Report().Verdict == VerdictSuspicious
}

// InvisibleParts returns the names of the invisible body parts.
func (s *Skin) InvisibleParts() []string {
	return s.Report().InvisibleParts()
}

// OK reports whether the skin is acceptable, the single question most callers
// have. Equivalent to Report().OK().
func (s *Skin) OK() bool {
	return s.Report().OK()
}
