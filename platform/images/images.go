// Package images validates uploaded photos and re-encodes them as bounded
// JPEGs so nothing a browser sent is ever stored or served verbatim.
package images

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

const (
	MaxUploadBytes  = 8 << 20
	MaxSourcePixels = 24_000_000
	MaxEdge         = 1600
	jpegQuality     = 85
)

var ErrTooLarge = errors.New("images: upload too large")
var ErrBadImage = errors.New("images: not a decodable image")

// Process decodes an upload, downscales it so no edge exceeds MaxEdge, and
// returns a fresh JPEG. The re-encode strips metadata and anything hostile.
func Process(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrBadImage
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width*height > MaxSourcePixels {
		return nil, ErrBadImage
	}
	scaled := Downscale(source, MaxEdge)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, scaled, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

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
					r, g, b, _ := source.At(sourceX, sourceY).RGBA()
					sumRed += uint64(r >> 8)
					sumGreen += uint64(g >> 8)
					sumBlue += uint64(b >> 8)
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
