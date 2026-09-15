// Package backend is the contract between uScope's run loop and whatever
// puts the frame in front of a human.
//
// The loop does not care whether a frame lands in /dev/fb0 on the uConsole,
// in a Kitty graphics escape sequence over ssh, or in half-block characters
// on a terminal that can manage nothing better. It draws into a canvas of
// whatever size the backend asks for and hands the canvas over.
//
// The interface sits in its own package so internal/app can depend on it
// without importing the framebuffer, which exists only on Linux, or the
// terminal encoders, which drag in the escape-sequence machinery. Nothing
// here touches a device, so the whole package is testable anywhere.
package backend

import (
	"errors"
	"fmt"
	"image"
	"slices"
)

// Backend is somewhere a frame can be put.
//
// Size reports the canvas the backend wants right now, width then height in
// pixels. The loop re-reads it every frame rather than trusting the answer
// it got at open, because a terminal window changes size while the program
// runs; when the answer changes the loop builds a new canvas.
//
// Blit displays an image that is exactly Size(). Close puts back whatever
// the backend changed and may be called more than once.
//
// An implementation owns its output stream, so it is not expected to be safe
// for concurrent use. One backend per run loop.
type Backend interface {
	Size() (int, int)
	Blit(img *image.RGBA) error
	Close() error
}

// Terminal names and TERM_PROGRAM values known to draw Kitty graphics.
const (
	termGhostty    = "xterm-ghostty"
	termKitty      = "xterm-kitty"
	programGhostty = "ghostty"
	programWezTerm = "WezTerm"

	// envKittyWindow is set by kitty itself in every child process, which
	// catches the case where TERM has been overridden by a shell profile.
	envKittyWindow = "KITTY_WINDOW_ID"
)

// ErrInvalid is returned for a --backend value that is not one of the five.
var ErrInvalid = errors.New("backend: unknown backend")

// Kind names the backends uScope knows how to build. It is what --backend
// parses into.
type Kind uint8

// The backends, in the order --backend lists them. Auto is zero so that the
// zero value of a config means "work it out", which is the safe default.
const (
	// Auto decides at run time: the framebuffer when this machine has one,
	// otherwise the best the terminal can do.
	Auto Kind = iota

	// Framebuffer is the Linux framebuffer, the uConsole's own screen.
	Framebuffer

	// Kitty is the terminal graphics protocol, which puts real pixels in a
	// terminal window and survives an ssh hop.
	Kitty

	// Blocks is half-block characters with 24-bit colour, which works on any
	// terminal that can do colour at all.
	Blocks

	// PNG writes a single frame to a file and exits.
	PNG
)

// Parse turns a --backend value into a Kind.
//
// The set is closed on purpose. A typo that fell through to a default would
// pick a backend nobody asked for, and on this program that means drawing
// into the wrong device.
func Parse(text string) (Kind, error) {
	switch text {
	case "auto":
		return Auto, nil
	case "fb":
		return Framebuffer, nil
	case "kitty":
		return Kitty, nil
	case "blocks":
		return Blocks, nil
	case "png":
		return PNG, nil
	default:
		return Auto, fmt.Errorf("%w: %q", ErrInvalid, text)
	}
}

// String renders the flag spelling, so a Kind round-trips through Parse and
// prints as itself in the startup line.
func (k Kind) String() string {
	switch k {
	case Auto:
		return "auto"
	case Framebuffer:
		return "fb"
	case Kitty:
		return "kitty"
	case Blocks:
		return "blocks"
	case PNG:
		return "png"
	default:
		return "invalid"
	}
}

// KittyCapable reports whether the terminal described by env draws Kitty
// graphics.
//
// This is an allow list of terminals confirmed to implement the protocol,
// not a probe. Probing means writing a query and waiting for a reply that
// never arrives on a terminal which does not understand the question, and a
// half-second stall at startup on an unknown terminal is a worse trade than
// falling back to half-blocks. Adding a name to the list is cheap when one
// turns up.
//
// env is os.Getenv in production and a map lookup in tests.
func KittyCapable(env func(string) string) bool {
	return env(envKittyWindow) != "" ||
		slices.Contains([]string{termGhostty, termKitty}, env("TERM")) ||
		slices.Contains([]string{programGhostty, programWezTerm}, env("TERM_PROGRAM"))
}
