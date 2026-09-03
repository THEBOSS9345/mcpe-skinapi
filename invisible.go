package skinapi

import (
	"encoding/json"
	"image"
	"sort"
)

const (
	// DefaultMinVisibleAlpha is the minimum alpha value for a pixel to count
	// as visible, matching the renderer's alpha-test threshold.
	DefaultMinVisibleAlpha = 0.5 / 255.0

	// DefaultMinVisibleFraction is the default minimum fraction of
	// non-transparent pixels a body part must have to count as VISIBLE. A
	// part that is more than half transparent is effectively invisible to a
	// viewer, so the default requires a majority of a part's sampled pixels
	// to be opaque. Override it per Skin with SkinOptions.
	DefaultMinVisibleFraction = 0.5

	// DefaultMinGeometrySize is the size a geometry bone must reach to be
	// considered non-tiny: the largest axis of the box enclosing its cubes,
	// so a bone qualifies by being big enough in any one dimension. A flat
	// plane is visible; a bone that is small on every axis is not.
	DefaultMinGeometrySize = 0.5

	// DefaultMinVisibleParts is the default minimum number of standard body
	// parts that must be meaningfully visible for the skin to pass. This
	// catches half-invisible skins where a player hides one or more body
	// parts while keeping the rest opaque. Override it per Skin with
	// SkinOptions.
	DefaultMinVisibleParts = 4
)

// thresholds is the tunable part of one detection run. A zero field takes its
// Default* value, so the zero thresholds is the standard configuration.
type thresholds struct {
	minVisibleFraction float64
	minGeometrySize    float64
	minVisibleParts    int
}

func (t thresholds) resolved() thresholds {
	if t.minVisibleFraction <= 0 {
		t.minVisibleFraction = DefaultMinVisibleFraction
	}
	if t.minGeometrySize <= 0 {
		t.minGeometrySize = DefaultMinGeometrySize
	}
	if t.minVisibleParts <= 0 {
		t.minVisibleParts = DefaultMinVisibleParts
	}
	return t
}

// SkinPartResult holds the visibility result for one body part. When geometry
// is provided, results reflect the actual cube UV regions resolved from the
// geometry. Without geometry, the standard vanilla UV layout is assumed.
type SkinPartResult struct {
	Name        string
	Visible     bool
	Fraction    float64
	Pixels      int
	Transparent int
	FromGeo     bool // true when the part came from geometry, not the fallback layout
	Tiny        bool // true when the geometry defines the part below the minimum size
}

// SkinVisibilityResult holds the result of ValidateSkinVisibility.
//
// IsInvisible is true when no body part is visible. Suspicious flags a
// half-invisible skin: at least one part is visible, but fewer of the standard
// body parts than the configured minimum, DefaultMinVisibleParts by default.
type SkinVisibilityResult struct {
	IsInvisible    bool
	Pass           bool
	Suspicious     bool
	Parts          []SkinPartResult
	VisibleParts   int
	InvisibleParts int
}

// GeometryViolation records a bone whose world-space size is below the
// minimum threshold.
type GeometryViolation struct {
	Bone    string
	Size    float64
	Minimum float64
}

// GeometrySizeResult holds the result of ValidateGeometrySize.
type GeometrySizeResult struct {
	Pass       bool
	Violations []GeometryViolation
}

// standardBodyParts defines the UV layout for the six visible body parts of a
// standard Minecraft humanoid skin. Coordinates are texture pixels against a
// 64-wide texture, scaled proportionally for 128x128 etc. Each region is the
// front (north) face — the face visible in a standard head-on render.
var standardBodyParts = map[string]struct{ ux, uy, w, h, d float64 }{
	"head":     {8, 8, 8, 8, 8},
	"body":     {20, 20, 8, 12, 4},
	"rightArm": {44, 20, 4, 12, 4},
	"leftArm":  {36, 52, 4, 12, 4},
	"rightLeg": {4, 20, 4, 12, 4},
	"leftLeg":  {20, 52, 4, 12, 4},
}

// standardPartNames lists the body part names checked by the detector.
var standardPartNames = []string{
	"head", "body", "rightArm", "leftArm", "rightLeg", "leftLeg",
}

// standardPartSet is the same name set as standardPartNames for O(1) lookup.
// Only these six count toward the visibility classification; clothing overlay
// layers (sleeves, pants, jacket, hat) and accessories are reported but do not
// inflate the visible-part count, so an opaque pant leg over an invisible limb
// is not mistaken for two visible parts.
var standardPartSet = map[string]bool{
	"head": true, "body": true, "rightArm": true, "leftArm": true, "rightLeg": true, "leftLeg": true,
}

// overlayBones maps clothing overlay bones to the standard body part they
// belong to, so their visibility folds into the parent part.
var overlayBones = map[string]string{
	"hat":         "head",
	"jacket":      "body",
	"leftSleeve":  "leftArm",
	"rightSleeve": "rightArm",
	"leftPants":   "leftLeg",
	"rightPants":  "rightLeg",
}

// accessoryBones are geometry bones that are not body parts. Wearing an opaque
// accessory such as a cape must not make an otherwise invisible body pass the
// detector, so these are reported for inspection but never counted toward the
// visible-parts tally.
var accessoryBones = map[string]bool{
	"cape": true,
}

// legacy32BodyParts is the pre-1.8 64x32 layout. Its left arm and left leg
// UV regions point off the bottom half of a 32-tall atlas, so they resolve to
// no pixels and count as invisible. The other four parts map to their real
// face regions.
var legacy32BodyParts = map[string]struct{ ux, uy, w, h, d float64 }{
	"head":     {0, 0, 8, 8, 8},
	"body":     {16, 16, 8, 12, 4},
	"rightArm": {40, 16, 4, 12, 4},
	"rightLeg": {0, 16, 4, 12, 4},
}

// ValidateSkinVisibility checks whether a skin texture has visible body parts.
// When geomData is provided (raw geometry.json), every bone with cubes is
// examined: its UV rectangles are resolved from the geometry and the actual
// texture pixels are checked for alpha. This cross-references geometry against
// the texture so a player cannot hide behind geometry that maps to transparent
// regions.
//
// Without geometry (nil geomData), the standard vanilla humanoid UV layout is
// used as the fallback — correct for most real skins.
//
// Persona geometry - geometry that parses into bones, none of which carry
// cubes - has no usable box UVs to cross-reference and comes from Minecraft's
// curated skin market, so it is always treated as visible and never flagged
// suspicious. Geometry that fails to parse is not that: it falls through to
// the texture-only check, exactly as nil geomData does.
//
// A skin is invisible if every body part has fewer than minVisibleFraction
// non-transparent pixels at its rendered UV regions.
func ValidateSkinVisibility(texture image.Image, geomData []byte, minVisibleFraction float64) SkinVisibilityResult {
	scan := scanParts(texture, getGeometry(geomData))
	if !scan.usable {
		return unusableResult()
	}
	if scan.persona {
		return personaResult()
	}
	return classifyVisibility(scan.parts, thresholds{minVisibleFraction: minVisibleFraction}.resolved(), scan.strict)
}

// partScan is one pass of raw per-part measurement, before any verdict is
// drawn from it. Separating the two lets ValidateSkinInvisibility fold in the
// geometry-size check and then classify exactly once - classifying twice used
// to work only because the tiny-bone suppression happened to zero Fraction as
// well as Visible, which the second pass would otherwise have undone.
type partScan struct {
	parts   []SkinPartResult
	strict  bool // regions came from authoritative box-UV geometry
	persona bool // real bones, no cubes: trusted visible
	usable  bool // false when there is no texture to analyse at all
}

// unusableResult is the verdict for input there is nothing to analyse.
// IsInvisible is set as well as Pass so that a caller gating on the natural
// read - if !skin.IsInvisible() - rejects a missing texture rather than
// admitting it.
func unusableResult() SkinVisibilityResult {
	return SkinVisibilityResult{Pass: false, IsInvisible: true}
}

func personaResult() SkinVisibilityResult {
	return SkinVisibilityResult{Pass: true, IsInvisible: false, Parts: visibleStandardParts()}
}

// scanParts measures every part's opaque-pixel fraction, from real cube UVs
// when the geometry supplies them and from the standard vanilla layout
// otherwise. It draws no conclusions; classifyVisibility does that.
//
// It takes an already-parsed bone map rather than raw bytes so that one
// detection run parses the geometry once. Taking bytes here meant the same
// document was unmarshalled three times per run: for this scan, for the
// has-geometry gate, and again inside ValidateGeometrySize.
func scanParts(texture image.Image, bones map[string]Bone) partScan {
	if texture == nil {
		return partScan{}
	}

	bounds := texture.Bounds()
	texW := float64(bounds.Dx())
	texH := float64(bounds.Dy())
	if texW <= 0 || texH <= 0 {
		return partScan{}
	}

	hasCubes := false
	for _, b := range bones {
		if len(b.Cubes) > 0 {
			hasCubes = true
			break
		}
	}

	switch {
	case hasCubes:
		// Box-UV geometry gives authoritative part regions, so the verdict
		// can be strict: a "only one limb renders" skin is definitively
		// invisible rather than merely suspicious.
		return partScan{
			parts:  checkFromGeometry(bones, texture, texW, texH),
			strict: true,
			usable: true,
		}

	case len(bones) > 0:
		// Persona skin: real bones, no cubes, so no box UVs to sample.
		// Trusted visible.
		//
		// This branch tests parsed bones, NOT "the caller passed some
		// bytes". Geometry that fails to parse, or parses to nothing, is
		// not a persona skin, and treating it as one let anyone defeat the
		// detector outright by attaching a byte of garbage to
		// SkinGeometryData: a fully transparent skin came back Pass=true.
		// See docs/design-decisions.md#why-persona-detection-tests-parsed-bones.
		return partScan{persona: true, usable: true}

	default:
		// No geometry, or geometry we could not read. Either way there is
		// nothing to cross-reference, so check the texture against the
		// standard layout - the same thing nil geometry does. That layout is
		// only inferred, so the verdict stays lenient (strict false).
		return partScan{
			parts:  checkFromStandardUV(texture, texW, texH),
			usable: true,
		}
	}
}

// visibleStandardParts returns a per-part report treating every standard body
// part as visible, used for persona skins where the mesh UVs are not box UVs
// the detector can sample.
func visibleStandardParts() []SkinPartResult {
	parts := make([]SkinPartResult, 0, len(standardPartNames))
	for _, name := range standardPartNames {
		parts = append(parts, SkinPartResult{Name: name, Visible: true, Fraction: 1, FromGeo: true})
	}
	return parts
}

// checkFromGeometry resolves each bone's cubes to UV rectangles and checks the
// texture. Every bone with cubes is checked, and standard body part names are
// mapped to human-readable labels. Bones with no cubes are skipped (they
// produce no rendered pixels).
func checkFromGeometry(bones map[string]Bone, texture image.Image, texW, texH float64) []SkinPartResult {
	// Scale UV rectangles from geometry texture dimensions to actual texture
	// dimensions. A 128x128 skin with a geometry declaring texture_width=128
	// needs no scaling; a geometry declaring 64 on a 128 texture scales 2x.
	scaleX, scaleY := uvScale(texW, texH)

	seen := map[string]bool{}
	var results []SkinPartResult

	measure := func(name string, bone Bone) SkinPartResult {
		total, transparent := countBoneTexture(bone, texture, scaleX, scaleY)
		part := SkinPartResult{
			Name:        name,
			Visible:     true,
			Pixels:      total,
			Transparent: transparent,
			FromGeo:     true,
		}
		if total > 0 {
			part.Fraction = float64(total-transparent) / float64(total)
		}
		return part
	}

	for _, name := range standardPartNames {
		bone, ok := bones[name]
		if !ok || len(bone.Cubes) == 0 {
			continue
		}
		seen[name] = true
		results = append(results, measure(name, bone))
	}

	// The six standard parts come first, in their fixed order; everything
	// else follows in name order. Ranging the map directly instead reshuffled
	// SkinReport.Parts between identical calls, which breaks response
	// caching, ETags and golden comparisons for a struct documented as safe
	// to marshal straight to JSON.
	rest := make([]string, 0, len(bones))
	for name, bone := range bones {
		if seen[name] || len(bone.Cubes) == 0 {
			continue
		}
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		results = append(results, measure(name, bones[name]))
	}

	return results
}

// uvScale returns the scale to apply to UV coordinates in texture pixels to
// map a 64-unit-wide reference layout onto an arbitrary texture. The standard
// vanilla 64x64 layout is anchored at 64 on both axes and is scaled
// proportionally. A 64x32 (pre-1.8) atlas uses the absolute coordinates of the
// legacy layout, so both scales are 1 no matter the atlas size.
func uvScale(texW, texH float64) (scaleX, scaleY float64) {
	scaleX = texW / 64.0
	scaleY = texH / 64.0
	// Legacy 64x32 atlases use their own compact layout whose coordinates are
	// given against a 64-wide reference.
	if texH == 32.0 {
		scaleX = 1.0
		scaleY = 1.0
	}
	return
}

// countBoneTexture sums total and transparent pixel counts across every visible
// face of every cube in the bone.
func countBoneTexture(bone Bone, texture image.Image, scaleX, scaleY float64) (total, transparent int) {
	for _, cube := range bone.Cubes {
		rects := cubeUVRects(cube)
		if rects == nil {
			continue
		}
		for _, rect := range rects {
			scaled := image.Rect(
				int(rect.x*scaleX),
				int(rect.y*scaleY),
				int((rect.x+rect.w)*scaleX),
				int((rect.y+rect.h)*scaleY),
			)
			_, t, tr := regionVisibility(texture, scaled, DefaultMinVisibleAlpha)
			total += t
			transparent += tr
		}
	}
	return
}

// checkFromStandardUV falls back to the standard vanilla humanoid UV layout
// when no geometry is provided. Each part's fraction is the visible-pixel
// fraction of its front (north) face. A 64x32 (pre-1.8) atlas uses the legacy
// layout, whose left arm/left leg faces point off-texture and are reported as
// invisible.
func checkFromStandardUV(texture image.Image, texW, texH float64) []SkinPartResult {
	scaleX, scaleY := uvScale(texW, texH)
	parts := standardBodyParts
	if texH == 32.0 {
		parts = legacy32BodyParts
	}

	results := make([]SkinPartResult, 0, len(standardPartNames))

	for _, name := range standardPartNames {
		p, ok := parts[name]
		if !ok {
			results = append(results, SkinPartResult{Name: name, Visible: false, FromGeo: false})
			continue
		}
		r := boxUVRects(p.ux*scaleX, p.uy*scaleY, p.w*scaleX, p.h*scaleY, p.d*scaleY)["north"]
		region := image.Rect(int(r.x), int(r.y), int(r.x+r.w), int(r.y+r.h))

		_, px, tr := regionVisibility(texture, region, DefaultMinVisibleAlpha)

		part := SkinPartResult{
			Name:        name,
			Visible:     true,
			Pixels:      px,
			Transparent: tr,
			FromGeo:     false,
		}
		if px > 0 {
			part.Fraction = float64(px-tr) / float64(px)
		}
		results = append(results, part)
	}

	return results
}

// classifyVisibility resolves each part's persistent Visible flag from its
// measured fraction and decides whether the skin as a whole is invisible. A
// part is VISIBLE only when at least minVisibleFraction of its pixels are
// opaque — so a mostly-transparent part counts as invisible.
//
// The verdict is driven by how many of the six standard body parts are
// visible. Clothing overlays fold into their parent part and accessories
// (e.g. capes) are ignored, so they can't mask an invisible body.
//
// When strict is true the part regions come from authoritative box-UV
// geometry, so a skin with 0 or 1 standard part visible (nothing, or only a
// stray limb) is INVISIBLE, 2-3 visible is SUSPICIOUS, and 4+ is visible.
// When strict is false the layout is only inferred (texture-only fallback), so
// only 0 visible parts counts as invisible and 1-3 is SUSPICIOUS.
func classifyVisibility(results []SkinPartResult, th thresholds, strict bool) SkinVisibilityResult {
	out := SkinVisibilityResult{}
	visParent := map[string]bool{}
	for i := range results {
		name := results[i].Name
		results[i].Visible = results[i].Fraction >= th.minVisibleFraction
		if accessoryBones[name] {
			continue
		}
		standard := name
		if overlayBones[name] != "" {
			standard = overlayBones[name]
		}
		if results[i].Visible {
			visParent[standard] = true
		}
	}
	out.Parts = results
	for _, name := range standardPartNames {
		if visParent[name] {
			out.VisibleParts++
		} else {
			out.InvisibleParts++
		}
	}
	if strict {
		out.IsInvisible = out.VisibleParts <= 1
	} else {
		out.IsInvisible = out.VisibleParts == 0
	}
	out.Suspicious = out.VisibleParts >= 1 && out.VisibleParts < th.minVisibleParts && !out.IsInvisible
	out.Pass = !out.IsInvisible
	return out
}

// ValidateGeometrySize checks whether the geometry defines body parts that are
// large enough to be visible. Every bone with cubes is checked: the largest
// axis of the box enclosing its cubes, inflate included, must meet minSize.
// Bones with no cubes are ignored (they produce no rendered geometry).
//
// Violations are ordered by bone name.
func ValidateGeometrySize(geomData []byte, minSize float64) GeometrySizeResult {
	return geometrySizeOf(getGeometry(geomData), minSize)
}

// geometrySizeOf is ValidateGeometrySize on already-parsed bones, so a
// detection run can share one parse with scanParts.
func geometrySizeOf(bones map[string]Bone, minSize float64) GeometrySizeResult {
	if minSize <= 0 {
		minSize = DefaultMinGeometrySize
	}

	if _, hasHead := bones["head"]; !hasHead {
		return GeometrySizeResult{
			Pass: false,
			Violations: []GeometryViolation{
				{Bone: "head", Size: 0, Minimum: minSize},
			},
		}
	}

	// Sorted by bone name: ranging the map directly made Violations arrive in
	// a different order on every call, for a struct callers marshal to JSON.
	names := make([]string, 0, len(bones))
	for name := range bones {
		names = append(names, name)
	}
	sort.Strings(names)

	var violations []GeometryViolation
	for _, name := range names {
		bone := bones[name]
		if len(bone.Cubes) == 0 {
			continue
		}
		sz := boneWorldSize(bone)
		if sz < minSize {
			violations = append(violations, GeometryViolation{
				Bone:    name,
				Size:    sz,
				Minimum: minSize,
			})
		}
	}

	if len(violations) > 0 {
		return GeometrySizeResult{Pass: false, Violations: violations}
	}
	return GeometrySizeResult{Pass: true}
}

// ValidateSkinInvisibility is the main entry point. It cross-references the
// geometry against the texture: for every bone the geometry defines, the
// corresponding UV regions are checked in the actual texture pixels. A skin is
// invisible if every rendered body part is either transparent in the texture
// or defined by geometry too small to see.
//
// geomData is the raw geometry.json bytes. Pass nil for standard skins (most
// real skins send no geometry — the default geometry is assumed).
func ValidateSkinInvisibility(texture image.Image, geomData []byte) SkinVisibilityResult {
	return validateWith(texture, geomData, thresholds{})
}

// validateWith is ValidateSkinInvisibility with the thresholds made explicit,
// so a Skin built by NewSkinWithOptions can tune them.
func validateWith(texture image.Image, geomData []byte, th thresholds) SkinVisibilityResult {
	th = th.resolved()

	// Parsed once, here, and shared with both passes below.
	bones := getGeometry(geomData)

	scan := scanParts(texture, bones)
	if !scan.usable {
		return unusableResult()
	}
	if scan.persona {
		return personaResult()
	}

	// The geometry-size check only makes sense when geometry actually parsed
	// into bones. With no geometry - or geometry we could not read - there is
	// nothing to judge as too small, and running the check anyway would flag
	// its missing-head violation against an ordinary skin.
	if len(bones) > 0 {
		tiny := map[string]bool{}
		for _, v := range geometrySizeOf(bones, th.minGeometrySize).Violations {
			tiny[v.Bone] = true
		}
		for i := range scan.parts {
			if tiny[scan.parts[i].Name] {
				// Tiny is what lets a caller tell "too small to see" apart
				// from "transparent"; Fraction is zeroed so the single
				// classification below reaches the same verdict.
				scan.parts[i].Tiny = true
				scan.parts[i].Fraction = 0
			}
		}
	}

	return classifyVisibility(scan.parts, th, scan.strict)
}

// IsSkinInvisible reports whether the skin is invisible — every body part
// defined by the geometry (or the standard UV layout) has transparent pixels.
func IsSkinInvisible(texture image.Image) bool {
	return ValidateSkinVisibility(texture, nil, DefaultMinVisibleFraction).IsInvisible
}

// IsSkinTiny reports whether the geometry defines body parts too small to see.
func IsSkinTiny(geomData []byte) bool {
	return !ValidateGeometrySize(geomData, DefaultMinGeometrySize).Pass
}

// regionVisibility checks whether any pixel in region has alpha >= threshold.
func regionVisibility(img image.Image, region image.Rectangle, threshold float64) (visible bool, total, transparent int) {
	bounds := img.Bounds()
	r := clampedBounds(region, bounds)
	w := r.Dx()
	h := r.Dy()
	if w <= 0 || h <= 0 {
		return false, 0, 0
	}

	total = w * h
	transparent = 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if float64(a)/0xffff <= threshold {
				transparent++
			}
		}
	}
	return transparent < total, total, transparent
}

// clampedBounds returns the intersection of inner with outer, clamped to the
// outer bounds.
func clampedBounds(inner, outer image.Rectangle) image.Rectangle {
	x0 := inner.Min.X
	if x0 < outer.Min.X {
		x0 = outer.Min.X
	}
	y0 := inner.Min.Y
	if y0 < outer.Min.Y {
		y0 = outer.Min.Y
	}
	x1 := inner.Max.X
	if x1 > outer.Max.X {
		x1 = outer.Max.X
	}
	y1 := inner.Max.Y
	if y1 > outer.Max.Y {
		y1 = outer.Max.Y
	}
	if x0 >= x1 || y0 >= y1 {
		return image.Rectangle{}
	}
	return image.Rect(x0, y0, x1, y1)
}

// boneWorldSize returns the largest axis of the bounding box enclosing every
// cube in the bone, accounting for inflate. A cube-level inflate overrides the
// bone's inflate. Malformed cubes are skipped; a bone of nothing but those
// measures 0 and is correctly flagged as too small.
//
// It is a bounding box rather than a per-axis sum of cube sizes. Summing let a
// bone made of many separately-invisible cubes add up to a passing figure, so a
// skin built from a hundred 0.05-unit cubes cleared the tiny check while
// rendering nothing a player could see. For the single-cube bones that make up
// every ordinary skin the two agree exactly, so only that bypass changes
// verdict. See docs/design-decisions.md#why-bone-size-is-a-bounding-box.
func boneWorldSize(b Bone) float64 {
	var min, max [3]float64
	measured := false
	for _, c := range b.Cubes {
		size, origin, ok := cubeDims(c)
		if !ok {
			continue
		}
		inflate := b.Inflate
		if c.Inflate != nil {
			inflate = *c.Inflate
		}
		for i := 0; i < 3; i++ {
			lo, hi := origin[i]-inflate, origin[i]+size[i]+inflate
			if !measured {
				min[i], max[i] = lo, hi
				continue
			}
			if lo < min[i] {
				min[i] = lo
			}
			if hi > max[i] {
				max[i] = hi
			}
		}
		measured = true
	}
	if !measured {
		return 0
	}
	largest := max[0] - min[0]
	for i := 1; i < 3; i++ {
		if e := max[i] - min[i]; e > largest {
			largest = e
		}
	}
	return largest
}

// getGeometry parses geometry.json into a bone map keyed by name, selecting the
// entry with the most cubes (same fallback as SelectGeometry). Returns nil for
// nil/empty input.
func getGeometry(geomData []byte) map[string]Bone {
	if len(geomData) == 0 {
		return nil
	}
	geos, err := ParseGeometry(geomData)
	if err != nil || len(geos) == 0 {
		return nil
	}

	geo := geos[0]
	for _, g := range geos[1:] {
		if g.TotalCubes() > geo.TotalCubes() {
			geo = g
		}
	}

	bones := make(map[string]Bone, len(geo.Bones))
	for _, b := range geo.Bones {
		bones[b.Name] = b
	}
	return bones
}

// getGeometryBytes marshals a Geometry back to JSON for testing.
func getGeometryBytes(geo Geometry) []byte {
	doc := map[string]interface{}{
		"format_version": "1.12.0",
		"minecraft:geometry": []map[string]interface{}{
			{
				"description": map[string]interface{}{
					"identifier":     geo.Identifier,
					"texture_width":  geo.TextureWidth,
					"texture_height": geo.TextureHeight,
				},
				"bones": geo.Bones,
			},
		},
	}
	b, _ := json.Marshal(doc)
	return b
}
