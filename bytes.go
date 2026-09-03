package skinapi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	_ "image/jpeg" // registered so DecodeImage accepts JPEG as well as PNG
)

// BytesOptions is Options with encoded byte slices in place of decoded
// images, for callers who already hold file or wire bytes and want PNG bytes
// back. It is the same render with the decode and encode steps folded in.
//
// Every field behaves exactly as its Options counterpart; see Options for
// what each one means.
type BytesOptions struct {
	// Texture is an encoded PNG or JPEG. Required.
	//
	// Bedrock sends skins as raw RGBA rather than an encoded image; use
	// TextureFromRGBA for those and the Options/Render path instead.
	Texture []byte

	// Geometry is a raw geometry.json. Nil, empty, or the literal "null" a
	// Bedrock client sends for a built-in model all fall back to
	// DefaultGeometry, so a skin's field can be passed straight through.
	Geometry []byte

	// Cape is an encoded PNG or JPEG cape texture, or nil.
	Cape []byte

	Identifier string
	View       View
	Angle      Angle
	Parts      []string
	Camera     *Camera
	Size       int
}

// RenderBytes renders from encoded bytes and returns encoded PNG bytes.
//
//	out, err := skinapi.RenderBytes(skinapi.BytesOptions{
//		Texture: textureBytes,
//		View:    skinapi.ViewAvatar,
//	})
//
// It is exactly Render with decoding and PNG encoding folded in, so it
// behaves identically for geometry defaults, persona fallback and identifier
// selection.
//
// Note that this decodes whatever it is given. A service accepting untrusted
// uploads should bound image dimensions first — see
// docs/recipes.md#handling-untrusted-uploads.
func RenderBytes(opts BytesOptions) ([]byte, error) {
	if len(opts.Texture) == 0 {
		return nil, ErrNoTexture
	}

	texture, err := DecodeImage(opts.Texture)
	if err != nil {
		return nil, fmt.Errorf("texture: %w", err)
	}

	var geos []Geometry
	if !IsEmpty(opts.Geometry) {
		if geos, err = ParseGeometry(opts.Geometry); err != nil {
			return nil, fmt.Errorf("geometry: %w", err)
		}
	}

	var cape image.Image
	if len(opts.Cape) > 0 {
		if cape, err = DecodeImage(opts.Cape); err != nil {
			return nil, fmt.Errorf("cape: %w", err)
		}
	}

	return Options{
		Texture:    texture,
		Geometry:   geos,
		Identifier: opts.Identifier,
		Cape:       cape,
		View:       opts.View,
		Angle:      opts.Angle,
		Parts:      opts.Parts,
		Camera:     opts.Camera,
		Size:       opts.Size,
	}.RenderPNG()
}

// Render renders these options, as the package-level Render function does.
// It is the method form, for callers who prefer to build options and render
// them in one expression.
func (o Options) Render() (image.Image, error) {
	return Render(o)
}

// RenderPNG renders these options and encodes the result as PNG bytes, for
// callers holding decoded images who nonetheless want bytes back.
func (o Options) RenderPNG() ([]byte, error) {
	img, err := Render(o)
	if err != nil {
		return nil, err
	}
	return EncodePNG(img)
}

// RenderPNG renders these options and returns encoded PNG bytes. It is the
// method form of RenderBytes.
func (o BytesOptions) RenderPNG() ([]byte, error) {
	return RenderBytes(o)
}

// DecodeImage decodes PNG or JPEG bytes into an image.
//
// It applies no size limit. Decoding is where a malicious image does its
// damage: a few-KB file can declare enormous dimensions and force a huge
// allocation. Callers handling untrusted input should call ImageDimensions
// first, which reads only the header.
func DecodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("no image data")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a valid image: %w", err)
	}
	return img, nil
}

// EncodePNG encodes an image as PNG bytes.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TextureFromRGBA wraps raw non-premultiplied RGBA pixel data as an image,
// which is the form Bedrock actually sends a skin in: SkinData decodes to
// width*height*4 bytes with no header, and the dimensions arrive separately
// in SkinImageWidth and SkinImageHeight.
//
//	raw, err := base64.StdEncoding.DecodeString(data.SkinData)
//	if err != nil {
//		return err
//	}
//	tex, err := skinapi.TextureFromRGBA(raw, data.SkinImageWidth, data.SkinImageHeight)
//
// The byte slice backs the image directly rather than being copied, so it
// must not be modified afterwards. A length that disagrees with the declared
// dimensions is an error rather than a silently garbled image.
//
// See docs/skin-data.md#the-texture-is-not-an-image-file.
func TextureFromRGBA(pix []byte, width, height int) (image.Image, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid dimensions %dx%d", width, height)
	}
	if want := width * height * 4; len(pix) != want {
		return nil, fmt.Errorf("got %d bytes of pixel data, expected %d for %dx%d", len(pix), want, width, height)
	}
	return &image.NRGBA{
		Pix:    pix,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}, nil
}

// ImageDimensions reports an encoded image's pixel dimensions by reading only
// its header, without decoding the pixels.
//
// This is the check DecodeImage's documentation asks callers handling
// untrusted uploads to make first, and the reason it matters: decoding is
// where a malicious image does its damage. A few-KB PNG can declare enormous
// dimensions and force a multi-gigabyte allocation the moment it is decoded.
// Bounding the header first costs nothing.
//
//	w, h, err := skinapi.ImageDimensions(data)
//	if err != nil {
//		return err
//	}
//	if w > 512 || h > 512 {
//		return errors.New("skin texture too large")
//	}
//	tex, err := skinapi.DecodeImage(data)
//
// It is to a texture what Complexity is to geometry: the measurement, with the
// ceiling left to the caller. See docs/recipes.md#handling-untrusted-uploads.
func ImageDimensions(data []byte) (width, height int, err error) {
	if len(data) == 0 {
		return 0, 0, errors.New("no image data")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("not a valid image: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}
