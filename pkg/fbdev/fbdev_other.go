//go:build !linux

package fbdev

import (
	"fmt"
	"image"
	"runtime"

	"github.com/hyperized/uScope/pkg/rotate"
)

// Device is the framebuffer stub for platforms without one. It exists so the
// rest of uScope has one API to compile against; every call fails cleanly.
type Device struct{}

// Option configures a Device. No options ship in slice 1; the variadic is
// here so adding one later is not a breaking change.
type Option func(*Device)

// Open always fails here. main turns this into the advice to use --png.
func Open(_ string, _ ...Option) (*Device, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}

// Width reports the framebuffer width in pixels. Always 0 off Linux.
func (*Device) Width() int { return 0 }

// Height reports the framebuffer height in pixels. Always 0 off Linux.
func (*Device) Height() int { return 0 }

// BitsPerPixel reports the pixel depth. Always 0 off Linux.
func (*Device) BitsPerPixel() int { return 0 }

// Stride reports the bytes per scanline. Always 0 off Linux.
func (*Device) Stride() int { return 0 }

// String describes the device.
func (*Device) String() string { return "no framebuffer" }

// Blit always fails here.
func (*Device) Blit(_ *image.RGBA, _ rotate.Rotation) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}

// Close is a no-op here, so deferred cleanup needs no platform check.
func (*Device) Close() error { return nil }
