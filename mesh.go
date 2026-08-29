package skinapi

import (
	"encoding/json"
	"math"

	"github.com/fogleman/fauxgl"
)

// uvRect is a texture-pixel-space rectangle for one cube face.
type uvRect struct{ x, y, w, h float64 }

// boxUVRects computes the standard Bedrock "unwrapped box" layout for a cube
// of size (w,h,d) with UV origin (u,v).
//
// See docs/geometry-format.md#box-uv-the-common-case for the layout diagram.
func boxUVRects(u, v, w, h, d float64) map[string]uvRect {
	return map[string]uvRect{
		"up":    {u + d, v, w, d},
		"down":  {u + d + w, v, w, d},
		"west":  {u, v + d, d, h},
		"north": {u + d, v + d, w, h},
		"east":  {u + d + w, v + d, d, h},
		"south": {u + d + w + d, v + d, w, h},
	}
}

// perFaceUVRect is Bedrock's alternative per-face uv object form:
// {"north":{"uv":[u,v],"uv_size":[w,h]}, ...}.
type perFaceUVEntry struct {
	UV     []float64 `json:"uv"`
	UVSize []float64 `json:"uv_size"`
}

func perFaceUVRects(raw json.RawMessage) map[string]uvRect {
	var faces map[string]perFaceUVEntry
	if err := json.Unmarshal(raw, &faces); err != nil {
		return nil
	}
	out := map[string]uvRect{}
	for name, f := range faces {
		if len(f.UV) < 2 {
			continue
		}
		w, h := 0.0, 0.0
		if len(f.UVSize) >= 2 {
			w, h = f.UVSize[0], f.UVSize[1]
		}
		out[name] = uvRect{f.UV[0], f.UV[1], w, h}
	}
	return out
}

func cubeUVRects(c Cube) map[string]uvRect {
	if len(c.UV) == 0 {
		return nil
	}
	var arr []float64
	if err := json.Unmarshal(c.UV, &arr); err == nil && len(arr) >= 2 {
		return boxUVRects(arr[0], arr[1], c.Size[0], c.Size[1], c.Size[2])
	}
	return perFaceUVRects(c.UV)
}

// faceCorner maps the parametric corner loop [-1,-1]->[1,-1]->[1,1]->[-1,1]
// to a local 3D offset from the cube's centre, for each of the 6 faces, given
// half-extents hx,hy,hz.
//
// See docs/rendering-pipeline.md#face-geometry - note in particular that the
// bottom of a face pairs with the texture's bottom row; pairing them the
// other way renders every side face upside down.
func faceCorner(face string, u, v, hx, hy, hz float64) fauxgl.Vector {
	switch face {
	case "up":
		return fauxgl.Vector{X: u * hx, Y: hy, Z: v * hz}
	case "down":
		return fauxgl.Vector{X: u * hx, Y: -hy, Z: -v * hz}
	case "north":
		return fauxgl.Vector{X: -u * hx, Y: v * hy, Z: -hz}
	case "south":
		return fauxgl.Vector{X: u * hx, Y: v * hy, Z: hz}
	case "east":
		return fauxgl.Vector{X: hx, Y: v * hy, Z: -u * hz}
	case "west":
		return fauxgl.Vector{X: -hx, Y: v * hy, Z: u * hz}
	}
	return fauxgl.Vector{}
}

var faceOrder = []string{"up", "down", "north", "south", "east", "west"}

// addCube appends one cube's 6 faces as fauxgl triangles (2 per face) to
// triangles, with vertex positions transformed by worldMatrix (the owning
// bone's world transform) and UVs in 0..1 texture space.
func addCube(triangles *[]*fauxgl.Triangle, c Cube, bonePivot []float64, boneInflate float64, worldMatrix fauxgl.Matrix, texW, texH float64) {
	rects := cubeUVRects(c)
	if rects == nil {
		return
	}
	inflate := boneInflate
	if c.Inflate != nil {
		inflate = *c.Inflate
	}
	sx := c.Size[0] + 2*inflate
	sy := c.Size[1] + 2*inflate
	sz := c.Size[2] + 2*inflate
	hx, hy, hz := sx/2, sy/2, sz/2

	centerAbs := [3]float64{
		c.Origin[0] + c.Size[0]/2,
		c.Origin[1] + c.Size[1]/2,
		c.Origin[2] + c.Size[2]/2,
	}
	centerLocal := fauxgl.Vector{
		X: centerAbs[0] - at(bonePivot, 0),
		Y: centerAbs[1] - at(bonePivot, 1),
		Z: centerAbs[2] - at(bonePivot, 2),
	}

	corners := [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}

	for _, face := range faceOrder {
		rect, ok := rects[face]
		if !ok {
			continue
		}
		u0, v0, u1, v1 := rect.x, rect.y, rect.x+rect.w, rect.y+rect.h
		if c.Mirror && (face == "east" || face == "west") {
			u0, u1 = u1, u0
		}
		uvCorners := [4][2]float64{{u0, v1}, {u1, v1}, {u1, v0}, {u0, v0}}

		var verts [4]fauxgl.Vertex
		for i := 0; i < 4; i++ {
			local := faceCorner(face, corners[i][0], corners[i][1], hx, hy, hz)
			pos := centerLocal.Add(local)
			verts[i] = fauxgl.Vertex{
				Position: worldMatrix.MulPosition(pos),
				// V is pre-flipped to cancel fauxgl's internal v=1-v. Do not
				// "simplify" this away: the failure mode is a random-looking
				// transparent/opaque pattern, not a cleanly mirrored image.
				// See docs/rendering-pipeline.md#the-texture-coordinate-flip.
				Texture: fauxgl.Vector{X: uvCorners[i][0] / texW, Y: 1 - uvCorners[i][1]/texH},
			}
		}
		*triangles = append(*triangles,
			fauxgl.NewTriangle(verts[0], verts[1], verts[2]),
			fauxgl.NewTriangle(verts[0], verts[2], verts[3]),
		)
	}
}

func at(v []float64, i int) float64 {
	if i < len(v) {
		return v[i]
	}
	return 0
}

const degToRad = math.Pi / 180

// boneLocalMatrix builds a bone's local transform: rotate X, then Y, then Z
// about the bone's own origin, then translate by ownPivot-parentPivot.
//
// The rotation axis order and signs are the one part of this library not
// verified against real data - every skin captured so far has rotation==0 on
// every bone. See docs/geometry-format.md for the details.
func boneLocalMatrix(b Bone, parentPivot []float64) fauxgl.Matrix {
	own := b.Pivot
	offset := fauxgl.Vector{
		X: at(own, 0) - at(parentPivot, 0),
		Y: at(own, 1) - at(parentPivot, 1),
		Z: at(own, 2) - at(parentPivot, 2),
	}
	m := fauxgl.Identity()
	if len(b.Rotation) >= 3 {
		m = m.Rotate(fauxgl.Vector{X: 1}, at(b.Rotation, 0)*degToRad)
		m = m.Rotate(fauxgl.Vector{Y: 1}, at(b.Rotation, 1)*degToRad)
		m = m.Rotate(fauxgl.Vector{Z: 1}, at(b.Rotation, 2)*degToRad)
	}
	m = m.Translate(offset)
	return m
}

// boneWorldMatrices composes every bone's absolute transform up the parent
// chain, memoized. fauxgl has no scene graph, so the hierarchy is baked into
// vertex positions instead. The seen set makes a malformed parent cycle
// resolve to identity rather than recursing forever.
//
// See docs/rendering-pipeline.md for the stage-by-stage walkthrough.
func boneWorldMatrices(geo Geometry) map[string]fauxgl.Matrix {
	byName := map[string]Bone{}
	for _, b := range geo.Bones {
		byName[b.Name] = b
	}
	result := map[string]fauxgl.Matrix{}
	var resolve func(name string, seen map[string]bool) fauxgl.Matrix
	resolve = func(name string, seen map[string]bool) fauxgl.Matrix {
		if m, ok := result[name]; ok {
			return m
		}
		b, ok := byName[name]
		if !ok || seen[name] {
			return fauxgl.Identity()
		}
		seen[name] = true
		parentPivot := []float64{}
		parentWorld := fauxgl.Identity()
		if b.Parent != "" {
			if pb, ok := byName[b.Parent]; ok {
				parentPivot = pb.Pivot
				parentWorld = resolve(b.Parent, seen)
			}
		}
		local := boneLocalMatrix(b, parentPivot)
		world := parentWorld.Mul(local)
		result[name] = world
		return world
	}
	for _, b := range geo.Bones {
		resolve(b.Name, map[string]bool{})
	}
	return result
}

// buildTriangles builds fauxgl triangles for every cube in geo whose bone
// name passes includeBone (nil = include everything).
func buildTriangles(geo Geometry, includeBone func(name string) bool) []*fauxgl.Triangle {
	worlds := boneWorldMatrices(geo)
	var triangles []*fauxgl.Triangle
	for _, b := range geo.Bones {
		if includeBone != nil && !includeBone(b.Name) {
			continue
		}
		world := worlds[b.Name]
		for _, c := range b.Cubes {
			addCube(&triangles, c, b.Pivot, b.Inflate, world, geo.TextureWidth, geo.TextureHeight)
		}
	}
	return triangles
}
