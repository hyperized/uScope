package app

import (
	"fmt"
	"image"

	"github.com/hyperized/uScope/pkg/rotate"
)

// fbBackend is the framebuffer wearing the Backend interface.
//
// fbdev takes the rotation on every Blit call, because a framebuffer has no
// opinion about which way up the panel is mounted and the caller does. The
// run loop stopped having that opinion in slice 2, so the rotation is bound
// here once, at open, and the loop just hands over frames.
type fbBackend struct {
	dev Blitter
	rot rotate.Rotation
}

// Size is the canvas size for the panel, which is the physical size with the
// axes swapped when the panel is turned a quarter turn either way.
func (f *fbBackend) Size() (int, int) {
	return f.rot.Logical(f.dev.Width(), f.dev.Height())
}

// Blit draws the frame through the rotation.
func (f *fbBackend) Blit(img *image.RGBA) error {
	if err := f.dev.Blit(img, f.rot); err != nil {
		return fmt.Errorf("fb backend: %w", err)
	}

	return nil
}

// Close releases the device.
func (f *fbBackend) Close() error {
	if err := f.dev.Close(); err != nil {
		return fmt.Errorf("fb backend: %w", err)
	}

	return nil
}
