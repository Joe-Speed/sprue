package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
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

func TestProcessSquare(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for x := 0; x < 300; x++ {
		for y := 0; y < 100; y++ {
			c := color.RGBA{0, 0, 0, 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{255, 255, 255, 255}
			}
			source.Set(x, y, c)
		}
	}
	var in bytes.Buffer
	if err := png.Encode(&in, source); err != nil {
		t.Fatal(err)
	}
	out, err := ProcessSquare(in.Bytes(), 50)
	if err != nil {
		t.Fatal(err)
	}
	result, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds().Dx() != 50 || result.Bounds().Dy() != 50 {
		t.Fatalf("size %v", result.Bounds())
	}
	r, _, _, _ := result.At(25, 25).RGBA()
	if r < 0xf000 {
		t.Errorf("centre crop should keep the white middle band, got %d", r>>8)
	}
	if _, err := ProcessSquare(in.Bytes(), 8); err == nil {
		t.Error("tiny sizes should be refused")
	}
}

// A tiny file that declares a huge frame must be refused before the pixel
// buffer is allocated. The PNG header below claims 20000 by 20000.
func TestProcessRefusesHugeDeclaredSize(t *testing.T) {
	header := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 'I', 'H', 'D', 'R',
		0, 0, 0x4e, 0x20, 0, 0, 0x4e, 0x20, 8, 6, 0, 0, 0}
	if _, err := Process(header); err != ErrBadImage {
		t.Errorf("huge declared size: %v", err)
	}
	if _, err := ProcessSquare(header, 256); err != ErrBadImage {
		t.Errorf("huge declared size, square: %v", err)
	}
}
