//go:build integration && linux

package fbdev_test

import (
	"image"
	"os"
	"testing"

	"github.com/hyperized/uScope/pkg/fbdev"
	"github.com/hyperized/uScope/pkg/rotate"
)

const (
	fbDevicePath = "/dev/fb0"
	maxSaneDim   = 10000
	depth16      = 16
	depth32      = 32
	bitsPerByte  = 8
)

// TestIntegration_RealFramebuffer opens the real framebuffer device and
// blits one black frame into it. It needs actual hardware, so it is gated
// behind the integration tag and skips cleanly when there is nothing to
// open.
func TestIntegration_RealFramebuffer(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(fbDevicePath); err != nil {
		if os.IsNotExist(err) {
			t.Skip("no /dev/fb0 on this machine")
		}

		if os.IsPermission(err) {
			t.Skip("no permission to stat /dev/fb0")
		}

		t.Fatalf("stat %s: %v", fbDevicePath, err)
	}

	dev, err := fbdev.Open(fbDevicePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() {
		if err := dev.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	width, height := dev.Width(), dev.Height()
	if width <= 0 || width >= maxSaneDim || height <= 0 || height >= maxSaneDim {
		t.Fatalf("geometry out of sane range: %dx%d", width, height)
	}

	bpp := dev.BitsPerPixel()
	if bpp != depth16 && bpp != depth32 {
		t.Fatalf("BitsPerPixel() = %d, want 16 or 32", bpp)
	}

	if minStride := width * bpp / bitsPerByte; dev.Stride() < minStride {
		t.Fatalf("Stride() = %d, want at least %d", dev.Stride(), minStride)
	}

	t.Logf("device: %s", dev.String())

	logicalW, logicalH := rotate.None.Logical(width, height)
	img := image.NewRGBA(image.Rect(0, 0, logicalW, logicalH))

	if err := dev.Blit(img, rotate.None); err != nil {
		t.Fatalf("Blit: %v", err)
	}
}
