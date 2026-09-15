// Package term puts a terminal into raw mode and puts it back.
//
// uScope needs keystrokes the moment they are typed, without the line
// discipline echoing them over the frame or waiting for Enter. That is the
// whole job: clear the cooking flags, set an 8-bit character size, and give
// reads a short timeout so the reader goroutine can notice a cancelled
// context instead of blocking in the kernel forever.
//
// This is deliberately not golang.org/x/term. It is thirty lines of termios
// and uScope has no third-party dependencies.
package term

import "errors"

// Sentinel errors.
var (
	// ErrNotTerminal means the descriptor is not a terminal, which happens
	// whenever stdin is a pipe or a file. Callers warn and run without
	// keyboard control rather than failing.
	ErrNotTerminal = errors.New("term: not a terminal")

	// ErrUnsupported means this platform has no termios implementation here.
	ErrUnsupported = errors.New("term: raw mode is not supported on this platform")
)
