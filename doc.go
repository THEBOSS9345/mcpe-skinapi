// Package skinapi renders Minecraft Bedrock skins to images.
//
// It takes a skin texture and, optionally, the skin's geometry.json, and
// rasterizes them into a square PNG-ready image.Image — no GPU, no headless
// browser, no external process.
//
// # Rendering
//
// Render is the entry point. Only a texture is required:
//
//	tex, _, err := image.Decode(f)
//	if err != nil {
//		return err
//	}
//	img, err := skinapi.Render(skinapi.Options{Texture: tex})
//
// That renders the full body of a standard humanoid, straight on, at
// 512x512. Options selects a different framing, angle, size, bone subset or
// explicit camera.
//
// Callers holding encoded bytes can skip the decode and encode entirely with
// RenderBytes, which takes BytesOptions and returns PNG bytes. Both paths run
// the same renderer and produce identical output. Options and BytesOptions
// also render themselves, via Render, RenderPNG and BytesOptions.RenderPNG.
//
// Skins taken off the wire arrive as raw RGBA rather than an encoded image;
// TextureFromRGBA wraps those without copying.
//
// # Geometry is optional
//
// Leaving Options.Geometry nil is not a shortcut — it is the correct input
// for most real skins. A Bedrock client sends no mesh at all for a skin that
// uses one of the built-in models: its login packet carries the literal JSON
// "null" in SkinGeometryData and names the model only in the skin's resource
// patch. Geometry travels the wire only for skins with a genuinely custom
// mesh. So a caller relaying a real skin very often has nothing to pass, and
// DefaultGeometry stands in with the model the client itself would use.
//
// When a skin does carry geometry, parse it with ParseGeometry, which accepts
// both of Bedrock's on-the-wire formats — the modern "minecraft:geometry"
// array and the pre-1.12 flat top-level-key form — and pass the result:
//
//	geos, err := skinapi.ParseGeometry(raw)
//	if err != nil {
//		return err
//	}
//	img, err := skinapi.Render(skinapi.Options{
//		Texture:    tex,
//		Geometry:   geos,
//		Identifier: "geometry.humanoid.customSlim",
//		View:       skinapi.ViewAvatar,
//		Angle:      skinapi.AngleIso,
//		Size:       256,
//	})
//
// Use IsEmpty to tell "the skin has no custom mesh" apart from "the upload
// was broken" before parsing: it reports true for empty input and for the
// literal "null" a client actually sends.
//
// # Custom models and persona skins
//
// Skins with extra bones — ears, tails, wings, party hats — render as a real
// 3D mesh with no special-casing; bone scoping is ancestry-based, so a custom
// bone parented under "head" is included by ViewHead automatically.
//
// Persona (avatar-builder) skins have real bones but no cubes at all, because
// Bedrock never sends mesh data for them. Render detects this and falls back
// to a flat crop of the texture rather than failing.
//
// # Untrusted input
//
// The library enforces no size or complexity limits of its own, since what
// counts as too large is policy. It does supply the two measurements to set
// those limits from: Complexity bounds a geometry document before rendering,
// and ImageDimensions reads a texture's dimensions from its header before
// decoding — a small PNG can declare enormous dimensions and force a huge
// allocation.
//
// Malformed input is survivable rather than fatal. Cubes whose size or origin
// arrays are too short are skipped rather than indexed, and every parser is
// fuzzed; see fuzz_test.go.
//
// # Reading a login packet
//
// A skin arrives as several fields that each need decoding. ParseResourcePatch
// reads the one that names the model — it is authoritative for wide-vs-slim,
// where the packet's ArmSize field is not — and ParseView/ParseAngle turn
// request parameters into the corresponding options, rejecting names they do
// not recognise instead of quietly rendering something else.
//
// # Detecting invisible skins
//
// Separately from rendering, Skin answers whether a skin is invisible or only
// half-visible, cross-referencing geometry against the texture's alpha. See
// NewSkin, or NewSkinWithOptions to judge by your own thresholds.
//
// # Further reading
//
// The docs directory covers the subject in depth, for anyone extending the
// library or debugging a render:
//
//	docs/skin-data.md           what a Bedrock client actually sends
//	docs/geometry-format.md     the geometry.json format, both versions
//	docs/rendering-pipeline.md  how a bone tree becomes pixels
//	docs/views-and-cameras.md   bone scoping, framing and camera math
//	docs/api-reference.md       every exported symbol
//	docs/recipes.md             worked examples
//	docs/design-decisions.md    why the library works the way it does
//
// Comments in this package stay brief and point into those files rather than
// repeating them.
package skinapi
