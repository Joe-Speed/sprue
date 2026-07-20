package images

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func solidImage(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestProcessReencodesAndBounds(t *testing.T) {
	out, err := Process(solidImage(3200, 1000))
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() > MaxEdge || decoded.Bounds().Dy() > MaxEdge {
		t.Fatalf("still too big: %v", decoded.Bounds())
	}
}

func TestProcessKeepsSmallImages(t *testing.T) {
	out, err := Process(solidImage(300, 200))
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 300 || decoded.Bounds().Dy() != 200 {
		t.Fatalf("size changed: %v", decoded.Bounds())
	}
}

func TestProcessRejectsGarbage(t *testing.T) {
	if _, err := Process([]byte("not an image at all")); err == nil {
		t.Fatal("expected error for garbage input")
	}
	if _, err := Process(nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}
