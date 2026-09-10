// Package images validates uploaded photos and re-encodes them as bounded
// JPEGs so nothing a browser sent is ever stored or served verbatim.
package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

// Limits sized for phone cameras: a 48 megapixel iPhone frame is about
// 8064 by 6048 pixels and up to 15 megabytes as a JPEG.
const (
	MaxUploadBytes  = 15 << 20
	MaxSourcePixels = 50_000_000
	MaxEdge         = 1600
	jpegQuality     = 85
)

var ErrTooLarge = errors.New("images: upload too large")
var ErrBadImage = errors.New("images: not a decodable image")

// decodeSlots bounds how many uploads decode at once. Go's decoders allocate
// the whole frame up front, so a few 50 megapixel photos arriving together
// would otherwise stack their buffers.
var decodeSlots = make(chan struct{}, 2)

// decode reads only the header first and refuses anything whose declared
// size is over the cap, so a tiny file claiming huge dimensions never gets
// its pixel buffer allocated. Then it decodes for real, one of a few at a time.
func decode(data []byte) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > MaxSourcePixels {
		return nil, ErrBadImage
	}
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	return source, nil
}

// Process decodes an upload, downscales it so no edge exceeds MaxEdge, and
// returns a fresh JPEG. The re-encode strips metadata and anything hostile.
func Process(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	source, err := decode(data)
	if err != nil {
		return nil, err
	}
	scaled := Downscale(source, MaxEdge)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, scaled, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ProcessSquare decodes an upload, crops it to a centred square, scales it to
// size pixels on each side, and returns a fresh JPEG. Used for profile pictures.
func ProcessSquare(data []byte, size int) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes || size < 16 || size > MaxEdge {
		return nil, ErrTooLarge
	}
	source, err := decode(data)
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	side := min(width, height)
	square := image.Rect(0, 0, side, side).Add(image.Pt(bounds.Min.X+(width-side)/2, bounds.Min.Y+(height-side)/2))
	scaled := Downscale(crop{source, square}, size)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, scaled, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pixelReader returns a function giving 8 bit red, green, and blue at a point.
// JPEGs decode to YCbCr and PNGs to RGBA or NRGBA; reading those directly is
// several times faster than going through the colour interface, which
// matters for a 48 megapixel phone photo. Anything else takes the slow path.
func pixelReader(source image.Image) func(x, y int) (uint8, uint8, uint8) {
	switch img := source.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint8, uint8, uint8) {
			c := img.YCbCrAt(x, y)
			return color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
		}
	case *image.RGBA:
		return func(x, y int) (uint8, uint8, uint8) {
			i := img.PixOffset(x, y)
			return img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		}
	case *image.NRGBA:
		return func(x, y int) (uint8, uint8, uint8) {
			i := img.PixOffset(x, y)
			return img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		}
	case crop:
		inner := pixelReader(img.Image)
		return inner
	}
	return func(x, y int) (uint8, uint8, uint8) {
		r, g, b, _ := source.At(x, y).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
	}
}

// crop presents a window of another image without copying pixels.
type crop struct {
	image.Image
	window image.Rectangle
}

func (c crop) Bounds() image.Rectangle { return c.window }

// Downscale box-filters an image so its longest edge is at most maxEdge.
// Images already small enough come back untouched.
func Downscale(source image.Image, maxEdge int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if maxEdge < 1 || (width <= maxEdge && height <= maxEdge) {
		return source
	}
	scale := (max(width, height) + maxEdge - 1) / maxEdge
	outWidth, outHeight := max(width/scale, 1), max(height/scale, 1)
	pixel := pixelReader(source)
	out := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := 0; y < outHeight; y++ {
		for x := 0; x < outWidth; x++ {
			var sumRed, sumGreen, sumBlue, samples uint64
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					sourceX, sourceY := bounds.Min.X+x*scale+dx, bounds.Min.Y+y*scale+dy
					if sourceX >= bounds.Max.X || sourceY >= bounds.Max.Y {
						continue
					}
					r, g, b := pixel(sourceX, sourceY)
					sumRed += uint64(r)
					sumGreen += uint64(g)
					sumBlue += uint64(b)
					samples++
				}
			}
			if samples == 0 {
				samples = 1
			}
			offset := out.PixOffset(x, y)
			out.Pix[offset] = uint8(sumRed / samples)
			out.Pix[offset+1] = uint8(sumGreen / samples)
			out.Pix[offset+2] = uint8(sumBlue / samples)
			out.Pix[offset+3] = 255
		}
	}
	return out
}
