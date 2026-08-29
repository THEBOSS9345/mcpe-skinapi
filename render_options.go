package skinapi

import (
	"errors"
	"image"

	"github.com/fogleman/fauxgl"
)

// DefaultSize is the output edge length used when Options.Size is zero.
const DefaultSize = 512

// Camera positions the view explicitly, instead of letting View and Angle
// pick a framing. Set Options.Camera only when one of the presets won't do —
// the presets already handle the common cases.
type Camera struct {
	// Yaw rotates the camera around the vertical axis, in degrees. 0 is
	// straight-on front; positive values swing the camera around to bring
	// more of the subject's left side into view.
	Yaw float64
	// Pitch raises the camera, in degrees. 0 is level with the subject's
	// centre; positive values look down and reveal top-facing surfaces.
	Pitch float64
	// FOV is the field of view in degrees. Zero means 35. Small values
	// (~15-20) read as flat and zoomed in; large ones (~50+) add perspective
	// distortion, which suits dramatic close framing.
	FOV float64
	// Margin is how much room to leave around the subject. Zero means 1.5.
	// 1.0 frames as tightly as possible without clipping; larger values pull
	// the camera back.
	Margin float64
}

// Options describes one render. Only Texture is required.
type Options struct {
	// Texture is the decoded skin image. Required. Bedrock skins are
	// normally 64x64 or 128x128, but any size works.
	Texture image.Image

	// Geometry is the skin's model, as returned by ParseGeometry. Leave it
	// nil for a skin that uses a built-in model — DefaultGeometry stands in,
	// which is what a Bedrock client would render. See DefaultGeometry for
	// why this is the common case rather than a convenience.
	Geometry []Geometry

	// Identifier picks which entry of Geometry to render when it holds more
	// than one, which is usual: a bundle typically carries a cape entry plus
	// both arm variants. It matches the identifier named in the skin's
	// resource patch, e.g. "geometry.humanoid.customSlim".
	//
	// The resource patch is the authoritative wide-vs-slim selector. The
	// login packet's ArmSize field is not: real captures show the two
	// disagreeing, with ArmSize reporting "wide" for a skin whose patch
	// names customSlim.
	//
	// Empty, or naming an entry that isn't present, falls back to the entry
	// with the most cubes. See SelectGeometry.
	Identifier string

	// Cape is an equipped cape texture, or nil. It renders only if Geometry
	// also defines a bone named "cape" that has a cube — capes live in their
	// own entry, never merged into the body.
	Cape image.Image

	// View selects the framing: body, chest, head or avatar. Zero means
	// ViewBody. Ignored when Parts is set.
	View View

	// Angle picks the camera preset for View: straight-on or the angled
	// 3-quarter "head icon" look. Zero means the default for the chosen
	// View, which is AngleIso for ViewHead and AngleFront elsewhere.
	// Ignored when Camera is set.
	Angle Angle

	// Parts names exactly which bones to render, e.g.
	// []string{"head", "leftArm"}. Each name pulls in everything parented
	// under it, so "head" also brings a hat or hair bone. Empty means use
	// View instead.
	Parts []string

	// Camera overrides the View/Angle framing with an explicit position.
	Camera *Camera

	// Size is the output edge length in pixels; the image is always square.
	// Zero means DefaultSize.
	Size int
}

// ErrNoTexture is returned by Render when Options.Texture is nil.
var ErrNoTexture = errors.New("skinapi: texture is required")

// Render rasterizes a skin into a square image.
//
// The zero-ish case is the common one: with only a Texture set, this renders
// the full body of a standard humanoid, straight on, at 512x512.
//
//	img, err := skinapi.Render(skinapi.Options{Texture: tex})
//
// Persona skins are handled rather than rejected. Their geometry has real
// bones but no cubes at all — Bedrock never sends mesh data for
// avatar-builder skins — so there is nothing to rasterize, and Render falls
// back to a flat crop of the texture (see Render2D). That check is the
// reason to prefer Render over driving the mesh functions directly.
func Render(opts Options) (image.Image, error) {
	if opts.Texture == nil {
		return nil, ErrNoTexture
	}

	size := opts.Size
	if size <= 0 {
		size = DefaultSize
	}

	geos := opts.Geometry
	if len(geos) == 0 {
		geos = DefaultGeometry()
	}
	geo, ok := SelectGeometry(geos, opts.Identifier)
	if !ok {
		return nil, errors.New("skinapi: geometry has no usable entries")
	}

	view := opts.View
	if view == "" {
		view = ViewBody
	}

	// No cubes anywhere means a persona-style skin: real bones, no mesh. A
	// flat texture crop is the only meaningful output, and it's what the
	// client itself shows.
	if geo.TotalCubes() == 0 {
		return Render2D(opts.Texture, view, size), nil
	}

	var (
		triangles []*fauxgl.Triangle
		fov       = 35.0
		margin    = 1.5
		yaw       float64
		pitch     float64
	)

	if len(opts.Parts) > 0 {
		triangles = buildTriangles(geo, includeForParts(geo, opts.Parts))
		if len(triangles) == 0 {
			return nil, errors.New("skinapi: no bones matched the requested parts")
		}
	} else {
		triangles = buildTriangles(geo, includeForView(geo, view))
		if len(triangles) == 0 {
			return nil, errors.New("skinapi: nothing to render for this view (no cubes matched)")
		}
		fov, margin = framingFor(view)
	}

	if opts.Camera != nil {
		yaw, pitch = opts.Camera.Yaw, opts.Camera.Pitch
		if opts.Camera.FOV > 0 {
			fov = opts.Camera.FOV
		}
		if opts.Camera.Margin > 0 {
			margin = opts.Camera.Margin
		}
	} else {
		angle := opts.Angle
		if angle == "" {
			angle = defaultAngleFor(view)
		}
		if angle == AngleIso {
			// The iso camera is offset diagonally rather than pulled
			// straight back, so it needs extra margin not to clip a corner.
			margin *= 1.25
		}
		yaw, pitch = angleToYawPitch(angle)
	}

	eye, center := cameraForYawPitch(triangles, fov, margin, yaw, pitch)

	var capeTriangles []*fauxgl.Triangle
	if opts.Cape != nil {
		if capeGeo, found := FindCape(geos); found {
			capeTriangles = buildCapeTriangles(capeGeo)
		}
	}

	return rasterize(triangles, capeTriangles, opts.Texture, opts.Cape, eye, center, fov, size), nil
}

// framingFor returns the field of view and margin that suit a given view:
// avatar is a tight zoomed-in crop, head leaves a little more headroom, and
// chest/body need a wider field and more margin to fit a much taller subject.
func framingFor(view View) (fov, margin float64) {
	switch view {
	case ViewAvatar:
		return 25.0, 1.15
	case ViewHead:
		return 30.0, 1.4
	case ViewChest:
		return 35.0, 1.5
	default:
		return 35.0, 1.6
	}
}
