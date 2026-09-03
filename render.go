package skinapi

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/fogleman/fauxgl"
)

// View selects which part of the model to render. Bone inclusion is
// ancestry-based rather than a fixed list, so a custom skin's extra bones
// (ears, tails, wings, party hats) are picked up automatically as long as
// they are parented somewhere under a standard anchor.
//
// See docs/views-and-cameras.md.
type View string

const (
	ViewBody   View = "body"   // full figure
	ViewChest  View = "chest"  // waist-up / bust, arms included
	ViewHead   View = "head"   // head only (+ descendants: hat, custom ear/horn/helmet bones, etc.)
	ViewAvatar View = "avatar" // square head icon, closer-framed than ViewHead
)

func boneMap(geo Geometry) map[string]Bone {
	m := make(map[string]Bone, len(geo.Bones))
	for _, b := range geo.Bones {
		m[b.Name] = b
	}
	return m
}

func isDescendant(byName map[string]Bone, name, ancestor string) bool {
	seen := map[string]bool{}
	cur := name
	for cur != "" && !seen[cur] {
		if cur == ancestor {
			return true
		}
		seen[cur] = true
		cur = byName[cur].Parent
	}
	return false
}

func includeForView(geo Geometry, view View) func(name string) bool {
	byName := boneMap(geo)
	switch view {
	case ViewHead, ViewAvatar:
		return func(name string) bool { return isDescendant(byName, name, "head") }
	case ViewChest:
		// A bust: head, torso and both arms with their own descendants
		// (sleeves, held-item locators, arm-mounted custom bones).
		return func(name string) bool {
			return isDescendant(byName, name, "head") ||
				isDescendant(byName, name, "leftArm") ||
				isDescendant(byName, name, "rightArm") ||
				name == "body" || name == "waist"
		}
	default: // ViewBody
		return nil // include everything
	}
}

// includeForParts builds an inclusion filter from a caller-supplied bone
// list. Each named bone and everything parented under it is included, the
// same way includeForView works. An empty list means everything.
func includeForParts(geo Geometry, parts []string) func(name string) bool {
	if len(parts) == 0 {
		return nil
	}
	byName := boneMap(geo)
	return func(name string) bool {
		for _, p := range parts {
			if isDescendant(byName, name, p) {
				return true
			}
		}
		return false
	}
}

// ParseParts splits a comma-separated bone list into trimmed, non-empty
// names. Blank input returns nil, which Options.Parts reads as "everything".
func ParseParts(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.Split(raw, ",")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ParseView resolves a view name, as it would arrive in a query string or a
// config file, to a View. Matching ignores case and surrounding space, and
// blank input returns ViewBody.
//
// An unrecognised name is an error wrapping ErrUnknownView rather than a
// silent fallback. Options.View is a bare string type that accepts anything,
// so a request for "avatr" would otherwise render a full body and look like
// the service ignoring its caller.
func ParseView(raw string) (View, error) {
	switch v := View(strings.ToLower(strings.TrimSpace(raw))); v {
	case "":
		return ViewBody, nil
	case ViewBody, ViewChest, ViewHead, ViewAvatar:
		return v, nil
	default:
		return "", fmt.Errorf("%w %q", ErrUnknownView, raw)
	}
}

// ParseAngle resolves an angle name to an Angle, the same way ParseView
// resolves a view. Blank input returns the zero Angle, which Options reads as
// "the default for the chosen view". An unrecognised name wraps
// ErrUnknownAngle.
func ParseAngle(raw string) (Angle, error) {
	switch a := Angle(strings.ToLower(strings.TrimSpace(raw))); a {
	case "":
		return "", nil
	case AngleFront, AngleIso:
		return a, nil
	default:
		return "", fmt.Errorf("%w %q", ErrUnknownAngle, raw)
	}
}

// boundingBoxOf returns the min/max corners spanning every vertex position
// across triangles.
func boundingBoxOf(triangles []*fauxgl.Triangle) (min, max fauxgl.Vector) {
	first := true
	extend := func(p fauxgl.Vector) {
		if first {
			min, max = p, p
			first = false
			return
		}
		min.X, max.X = math.Min(min.X, p.X), math.Max(max.X, p.X)
		min.Y, max.Y = math.Min(min.Y, p.Y), math.Max(max.Y, p.Y)
		min.Z, max.Z = math.Min(min.Z, p.Z), math.Max(max.Z, p.Z)
	}
	for _, t := range triangles {
		extend(t.V1.Position)
		extend(t.V2.Position)
		extend(t.V3.Position)
	}
	return
}

// Angle selects one of the two named camera presets. Options.Camera bypasses
// these entirely and takes explicit yaw/pitch instead.
type Angle string

const (
	AngleFront Angle = "front"
	AngleIso   Angle = "iso"
)

// defaultAngleFor returns the angle used when the caller doesn't pick one.
// Head defaults to iso because a front portrait hides the top of the head,
// which is exactly where custom bones tend to sit; everything else reads
// better straight on. See docs/views-and-cameras.md#angles.
func defaultAngleFor(view View) Angle {
	if view == ViewHead {
		return AngleIso
	}
	return AngleFront
}

// isoYawDegrees/isoPitchDegrees show three faces at once (front, top, one
// side) without foreshortening any of them away to nothing.
const (
	isoYawDegrees   = 35.0
	isoPitchDegrees = 25.0
)

func angleToYawPitch(angle Angle) (yawDegrees, pitchDegrees float64) {
	if angle == AngleIso {
		return isoYawDegrees, isoPitchDegrees
	}
	return 0, 0
}

// cameraForYawPitch frames the camera from the triangles' actual bounding
// box, never a hardcoded distance - a fixed distance works only for models
// whose extent you already assumed.
//
// yaw=0,pitch=0 sits the camera on the -Z side looking toward +Z, up=+Y.
// Positive yaw swings the eye toward +X; positive pitch raises it to look
// down. See docs/rendering-pipeline.md#stage-4--framing-the-camera for how
// that baseline was established.
func cameraForYawPitch(triangles []*fauxgl.Triangle, fovDegrees, marginFactor, yawDegrees, pitchDegrees float64) (eye, center fauxgl.Vector) {
	min, max := boundingBoxOf(triangles)
	center = fauxgl.Vector{X: (min.X + max.X) / 2, Y: (min.Y + max.Y) / 2, Z: (min.Z + max.Z) / 2}
	halfExtent := math.Max((max.X-min.X)/2, math.Max((max.Y-min.Y)/2, (max.Z-min.Z)/2))
	if halfExtent <= 0 {
		halfExtent = 1
	}
	halfFovRad := fovDegrees * math.Pi / 360
	distance := halfExtent / math.Tan(halfFovRad) * marginFactor

	yaw, pitch := yawDegrees*math.Pi/180, pitchDegrees*math.Pi/180
	offset := fauxgl.Vector{
		X: distance * math.Sin(yaw) * math.Cos(pitch),
		Y: distance * math.Sin(pitch),
		Z: -distance * math.Cos(yaw) * math.Cos(pitch),
	}
	eye = fauxgl.Vector{X: center.X + offset.X, Y: center.Y + offset.Y, Z: center.Z + offset.Z}
	return eye, center
}

// rasterize does the GPU-free drawing, given resolved triangles and camera
// parameters. capeTriangles/capeTexture may be nil to skip the cape.
func rasterize(triangles, capeTriangles []*fauxgl.Triangle, texture, capeTexture image.Image, eye, center fauxgl.Vector, fovDegrees float64, size int) image.Image {
	dc := fauxgl.NewContext(size, size)
	dc.Cull = fauxgl.CullNone // see shader.go / mesh.go: winding order is not guaranteed consistent
	dc.ClearColorBufferWith(fauxgl.Transparent)

	// Clip space only - do NOT chain .Viewport(...) here. fauxgl applies the
	// NDC->screen mapping itself after the perspective divide; adding one
	// before the divide renders a fully blank image.
	// See docs/design-decisions.md#why-no-viewport-in-the-shader-matrix.
	matrix := fauxgl.LookAt(eye, center, fauxgl.Vector{Y: 1}).
		Perspective(fovDegrees, 1.0, 1, 500)

	// Deliberately the singular DrawTriangle in a loop, NOT the plural
	// DrawTriangles: the plural form spawns goroutines that race on fauxgl's
	// depth buffer, which trips the race detector in any downstream test
	// suite. It is also slower under concurrent load. Do not "optimize" this
	// back. See docs/design-decisions.md#why-rasterization-is-single-threaded.
	tex := newFastImageTexture(texture)
	dc.Shader = newAlphaTestTextureShader(matrix, tex)
	for _, t := range triangles {
		dc.DrawTriangle(t)
	}

	if capeTexture != nil && len(capeTriangles) > 0 {
		capeTex := newFastImageTexture(capeTexture)
		dc.Shader = newAlphaTestTextureShader(matrix, capeTex)
		for _, t := range capeTriangles {
			dc.DrawTriangle(t)
		}
	}

	return dc.Image()
}

// buildCapeTriangles builds the cape mesh. A cape entry is its own
// self-contained bone chain (body -> waist -> cape), so it positions itself
// from parent names within capeGeo alone.
func buildCapeTriangles(capeGeo Geometry) []*fauxgl.Triangle {
	include := func(name string) bool { return name == "cape" }
	return buildTriangles(capeGeo, include)
}
