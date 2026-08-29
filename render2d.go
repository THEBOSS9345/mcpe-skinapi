package skinapi

import (
	"image"

	"golang.org/x/image/draw" // Draw + NearestNeighbor scaling, keeps Minecraft's pixelated look
)

// Render2D composites a flat front-view "paper doll" by cropping the standard
// vanilla box-UV regions straight out of the texture. It needs no geometry at
// all, which is why Render falls back to it for persona skins: they carry
// bones but no cubes, so there is nothing to rasterize.
//
// Coordinates are relative to a 64-wide texture and scaled proportionally for
// other widths. See docs/rendering-pipeline.md#the-2d-fallback.
func Render2D(texture image.Image, view View, size int) image.Image {
	scale := float64(texture.Bounds().Dx()) / 64.0

	type part struct{ ux, uy, w, h, d float64 }
	head := part{0, 0, 8, 8, 8}
	body := part{16, 16, 8, 12, 4}
	rightArm := part{40, 16, 4, 12, 4}
	leftArm := part{32, 48, 4, 12, 4}
	rightLeg := part{0, 16, 4, 12, 4}
	leftLeg := part{16, 48, 4, 12, 4}

	frontCrop := func(p part) image.Image {
		r := boxUVRects(p.ux*scale, p.uy*scale, p.w*scale, p.h*scale, p.d*scale)["north"]
		rect := image.Rect(int(r.x), int(r.y), int(r.x+r.w), int(r.y+r.h))
		sub, ok := texture.(interface {
			SubImage(r image.Rectangle) image.Image
		})
		if !ok {
			return texture
		}
		return sub.SubImage(rect)
	}

	var canvas *image.NRGBA
	switch view {
	case ViewHead, ViewAvatar:
		canvas = compose(frontCrop(head))
	case ViewChest:
		canvas = composeParts(frontCrop(head), frontCrop(body), frontCrop(leftArm), frontCrop(rightArm), nil, nil)
	default: // ViewBody
		canvas = composeParts(frontCrop(head), frontCrop(body), frontCrop(leftArm), frontCrop(rightArm), frontCrop(leftLeg), frontCrop(rightLeg))
	}

	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.NearestNeighbor.Scale(out, out.Bounds(), canvas, canvas.Bounds(), draw.Over, nil)
	return out
}

func compose(img image.Image) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// composeParts stacks head above body above arms/legs into a flat paper doll.
func composeParts(head, body, leftArm, rightArm, leftLeg, rightLeg image.Image) *image.NRGBA {
	bw := body.Bounds().Dx()
	totalW := leftArm.Bounds().Dx() + bw + rightArm.Bounds().Dx()
	totalH := head.Bounds().Dy() + body.Bounds().Dy()
	if leftLeg != nil {
		totalH += leftLeg.Bounds().Dy()
	}

	out := image.NewNRGBA(image.Rect(0, 0, totalW, totalH))
	midX := leftArm.Bounds().Dx()
	y := 0

	drawAt := func(img image.Image, x, yy int) {
		b := img.Bounds()
		draw.Draw(out, image.Rect(x, yy, x+b.Dx(), yy+b.Dy()), img, b.Min, draw.Over)
	}

	hb := head.Bounds()
	drawAt(head, midX+(bw-hb.Dx())/2, y)
	y += hb.Dy()

	drawAt(leftArm, 0, y)
	drawAt(body, midX, y)
	drawAt(rightArm, midX+bw, y)
	y += body.Bounds().Dy()

	if leftLeg != nil && rightLeg != nil {
		lw := leftLeg.Bounds().Dx()
		drawAt(leftLeg, midX+bw/2-lw, y)
		drawAt(rightLeg, midX+bw/2, y)
	}

	return out
}
