//go:build !linux

package fbdev //nolint:testpackage // matches the linux internal test file's package, per the brief for this file.

import (
	"errors"
	"image"
	"testing"

	"github.com/hyperized/uScope/pkg/rotate"
)

func TestOpen_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := Open("/dev/fb0")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open() error = %v, want ErrUnsupported", err)
	}
}

func TestStub_Getters(t *testing.T) {
	t.Parallel()

	dev := &Device{}

	if got := dev.Width(); got != 0 {
		t.Errorf("Width() = %d, want 0", got)
	}

	if got := dev.Height(); got != 0 {
		t.Errorf("Height() = %d, want 0", got)
	}

	if got := dev.BitsPerPixel(); got != 0 {
		t.Errorf("BitsPerPixel() = %d, want 0", got)
	}

	if got := dev.Stride(); got != 0 {
		t.Errorf("Stride() = %d, want 0", got)
	}

	if got, want := dev.String(), "no framebuffer"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStub_Blit(t *testing.T) {
	t.Parallel()

	dev := &Device{}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	err := dev.Blit(img, rotate.None)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Blit() error = %v, want ErrUnsupported", err)
	}
}

func TestStub_Close(t *testing.T) {
	t.Parallel()

	dev := &Device{}

	if err := dev.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
