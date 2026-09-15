// Package winsize asks the kernel how big a terminal is.
//
// uScope draws pixels into a terminal, so it needs two things the kernel
// alone can answer: how many character cells the terminal has, and, when
// the terminal is willing to say, how many pixels those cells cover. The
// cell count sizes the canvas; the pixel count, when present, is what a
// caller uses to work out how big one cell is.
//
// This is deliberately not golang.org/x/term. The whole implementation is
// one TIOCGWINSZ ioctl and uScope has no third-party dependencies.
package winsize

import "errors"

// Size is what the kernel reports for a terminal.
//
// XPixels and YPixels are frequently zero. Plenty of terminal emulators
// never fill in the pixel fields of TIOCGWINSZ even though the ioctl
// succeeds and Cols and Rows come back correct. Every caller must have a
// fallback for that case and must never divide by XPixels or YPixels
// without checking they are non-zero first.
type Size struct {
	Cols, Rows       int // character cells
	XPixels, YPixels int // pixels, 0 when the terminal does not report them
}

// Sentinel errors.
var (
	// ErrNotTerminal means the descriptor is not a terminal, which happens
	// whenever the descriptor is a pipe or a file. Callers fall back to a
	// default size rather than failing outright.
	ErrNotTerminal = errors.New("winsize: not a terminal")

	// ErrUnsupported means this platform has no TIOCGWINSZ implementation
	// here.
	ErrUnsupported = errors.New("winsize: getting the window size is not supported on this platform")
)
