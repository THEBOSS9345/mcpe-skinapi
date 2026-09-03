package skinapi

import (
	"encoding/json"
	"image"
)

const (
	// DefaultMinVisibleAlpha is the minimum alpha value for a pixel to count
	// as visible, matching the renderer's alpha-test threshold.
	DefaultMinVisibleAlpha = 0.5 / 255.0

	// DefaultMinVisibleFraction is the minimum fraction of non-transparent
	// pixels a body part must have to count as VISIBLE. A part that is more
	// than half transparent is effectively invisible to a viewer, so the
	// default requires a majority of a part's sampled pixels to be opaque.
	DefaultMinVisibleFraction = 0.5

	// DefaultMinGeometrySize is the minimum world-space size a geometry bone
	// must reach on every axis to be considered non-tiny.
	DefaultMinGeometrySize = 0.5

	// DefaultMinVisibleParts is the minimum number of standard body parts that
	// must be meaningfully visible for the skin to pass. This catches
	// half-invisible skins where a player hides one or more body parts while
	// keeping the rest opaque.
	DefaultMinVisibleParts = 4
)

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
}

// SkinVisibilityResult holds the result of ValidateSkinVisibility.
//
// IsInvisible is true when no body part is visible. Suspicious flags a
// half-invisible skin: at least one part is visible, but fewer than
// DefaultMinVisibleParts of the standard body parts are visible.
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
// 64-wide texture, scaled proportionally for128x128 etc. Each region is the
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
// Persona geometry (bones but no cubes, custom meshes) has no usable box UVs
// to cross-reference and comes from Minecraft's curated skin market, so it is
// always treated as visible and never flagged suspicious.
//
// A skin is invisible if every body part has fewer than minVisibleFraction
// non-transparent pixels at its rendered UV regions.
func ValidateSkinVisibility(texture image.Image, geomData []byte, minVisibleFraction float64) SkinVisibilityResult {
	if texture == nil {
		return SkinVisibilityResult{Pass: false}
	}
	if minVisibleFraction <= 0 {
		minVisibleFraction = DefaultMinVisibleFraction
	}

	bounds := texture.Bounds()
	texW := float64(bounds.Dx())
	texH := float64(bounds.Dy())
	if texW <= 0 || texH <= 0 {
		return SkinVisibilityResult{Pass: false}
	}

	geomProvided := len(geomData) > 0
	bones := getGeometry(geomData)
	var results []SkinPartResult

	// When geometry has bones but none have cubes (persona skins), fall
	// back to the standard UV layout — there is nothing to cross-reference.
	hasCubes := false
	for _, b := range bones {
		if len(b.Cubes) > 0 {
			hasCubes = true
			break
		}
	}

	if hasCubes {
		results = checkFromGeometry(bones, texture, texW, texH)
	} else if geomProvided {
		// Persona skin: no box UVs to sample, always trusted visible.
		return SkinVisibilityResult{
			Pass:        true,
			IsInvisible: false,
			Parts:       visibleStandardParts(),
		}
	} else {
		results = checkFromStandardUV(texture, texW, texH)
	}

	// Box-UV geometry gives authoritative part regions, so a tiny "only one
	// limb" skin is definitively invisible. The texture-only fallback does
	// not know the true layout (persona textures diverge from vanilla), so it
	// stays lenient and only calls a fully-transparent skin invisible.
	return classifyVisibility(results, minVisibleFraction, hasCubes)
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
	// needs no scaling; a geometry declaring 64 on a128 texture scales 2x.
	scaleX, scaleY := uvScale(texW, texH)

	seen := map[string]bool{}
	var results []SkinPartResult

	for _, name := range standardPartNames {
		bone, ok := bones[name]
		if !ok || len(bone.Cubes) == 0 {
			continue
		}
		seen[name] = true

		results = append(results, SkinPartResult{
			Name:        name,
			Visible:     true,
			Fraction:    boneTextureFraction(bone, texture, scaleX, scaleY),
			Pixels:      boneTexturePixels(bone, texture, scaleX, scaleY),
			Transparent: boneTextureTransparent(bone, texture, scaleX, scaleY),
			FromGeo:     true,
		})
	}

	for name, bone := range bones {
		if seen[name] || len(bone.Cubes) == 0 {
			continue
		}

		results = append(results, SkinPartResult{
			Name:        name,
			Visible:     true,
			Fraction:    boneTextureFraction(bone, texture, scaleX, scaleY),
			Pixels:      boneTexturePixels(bone, texture, scaleX, scaleY),
			Transparent: boneTextureTransparent(bone, texture, scaleX, scaleY),
			FromGeo:     true,
		})
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

// partFraction computesthe visible-pixel fraction of a bone across all its
// cube faces. A bone with no sampleable pixels reports 0.
func boneTextureFraction(bone Bone, texture image.Image, scaleX, scaleY float64) float64 {
	totalAll, transAll := countBoneTexture(bone, texture, scaleX, scaleY)
	if totalAll <= 0 {
		return 0
	}
	return float64(totalAll-transAll) / float64(totalAll)
}

func boneTexturePixels(bone Bone, texture image.Image, scaleX, scaleY float64) int {
	total, _ := countBoneTexture(bone, texture, scaleX, scaleY)
	return total
}

func boneTextureTransparent(bone Bone, texture image.Image, scaleX, scaleY float64) int {
	_, trans := countBoneTexture(bone, texture, scaleX, scaleY)
	return trans
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
// opaque — so a mostly-transparent part counts as invisible. Accessory bones
// (e.g. capes) are never counted, so an opaque cape cannot mask an invisible
// body. The skin is INVISIBLE when no body part meets that bar; it is
// SUSPICIOUS (possibly half-invisible) when fewer than DefaultMinVisibleParts
// body parts are visible.
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
func classifyVisibility(results []SkinPartResult, minVisibleFraction float64, strict bool) SkinVisibilityResult {
	out := SkinVisibilityResult{}
	visParent := map[string]bool{}
	for i := range results {
		name := results[i].Name
		results[i].Visible = results[i].Fraction >= minVisibleFraction
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
	out.Suspicious = out.VisibleParts >= 1 && out.VisibleParts < DefaultMinVisibleParts && !out.IsInvisible
	out.Pass = !out.IsInvisible
	return out
}

// ValidateGeometrySize checks whether the geometry defines body parts that are
// large enough to be visible. Every bone with cubes is checked; the summed
// inflated size on each axis must meet minSize. Bones with no cubes are
// ignored (they produce no rendered geometry).
func ValidateGeometrySize(geomData []byte, minSize float64) GeometrySizeResult {
	if minSize <= 0 {
		minSize = DefaultMinGeometrySize
	}

	bones := getGeometry(geomData)
	if _, hasHead := bones["head"]; !hasHead {
		return GeometrySizeResult{
			Pass: false,
			Violations: []GeometryViolation{
				{Bone: "head", Size: 0, Minimum: minSize},
			},
		}
	}

	var violations []GeometryViolation
	for name, bone := range bones {
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
	vr := ValidateSkinVisibility(texture, geomData, DefaultMinVisibleFraction)

	// The geometry-size check only makes sense when geometry was actually
	// supplied. With nil geometry there are no bones to judge as too small,
	// so skip it rather than flagging a missing head on an ordinary skin.
	if len(geomData) == 0 {
		return vr
	}

	geomResult := ValidateGeometrySize(geomData, DefaultMinGeometrySize)
	tiny := map[string]float64{}
	for _, v := range geomResult.Violations {
		tiny[v.Bone] = v.Size
	}

	for i := range vr.Parts {
		if _, ok := tiny[vr.Parts[i].Name]; ok {
			vr.Parts[i].Visible = false
			vr.Parts[i].Fraction = 0
		}
	}

	return classifyVisibility(vr.Parts, DefaultMinVisibleFraction, true)
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

// boneWorldSize computes the approximate world-space size of a bone's geometry
// on each axis, accounting for inflate. A cube-level inflate overrides the
// bone's inflate.
func boneWorldSize(b Bone) float64 {
	var sizes [3]float64
	for _, c := range b.Cubes {
		inflate := b.Inflate
		if c.Inflate != nil {
			inflate = *c.Inflate
		}
		sizes[0] += c.Size[0] + 2*inflate
		sizes[1] += c.Size[1] + 2*inflate
		sizes[2] += c.Size[2] + 2*inflate
	}
	max := sizes[0]
	if sizes[1] > max {
		max = sizes[1]
	}
	if sizes[2] > max {
		max = sizes[2]
	}
	return max
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
