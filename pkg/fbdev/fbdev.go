// Package fbdev blits a canvas straight into a Linux framebuffer device.
//
// This is the layer that makes uScope a pixel program rather than a
// character one: it mmaps /dev/fb0 and writes packed pixels into it. There
// is no X server, no DRM master, and no terminal involved. The uConsole user
// is in the video group, so none of it needs root.
//
// The panel is mounted portrait, so the buffer is 720x1280 while the user
// sees 1280x720. Blit takes the rotation and walks the logical image into
// physical buffer positions, which is why pkg/rotate sits underneath.
//
// Only Linux has a framebuffer. Every other platform gets the same API back
// with ErrUnsupported, so the rest of uScope compiles and tests on a Mac.
package fbdev

import "errors"

// Sentinel errors. Callers separate "this machine has no framebuffer" from
// "this frame is the wrong shape", because the first is a reason to fall
// back to --png and the second is a bug.
var (
	// ErrUnsupported means the platform has no framebuffer device at all.
	ErrUnsupported = errors.New("fbdev: no framebuffer backend on this platform")

	// ErrSize means the image handed to Blit does not match the logical
	// size the device wants for that rotation.
	ErrSize = errors.New("fbdev: image size does not match the framebuffer")

	// ErrDepth means the framebuffer reports a pixel depth we cannot pack.
	ErrDepth = errors.New("fbdev: unsupported pixel depth")

	// ErrClosed means Blit was called after Close.
	ErrClosed = errors.New("fbdev: device is closed")
)
