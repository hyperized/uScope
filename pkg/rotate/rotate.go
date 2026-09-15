// Package rotate holds the fbcon rotation numbering and the pixel mapping
// that belongs to it.
//
// The uConsole panel is mounted portrait, so the framebuffer is 720x1280.
// fbcon turns the console a quarter turn to give the user a 1280x720
// landscape screen, and publishes which way it turned in
// /sys/class/graphics/fbcon/rotate. uScope reads the same file so its
// canvas lands the same way up as the console it replaces.
//
// Nothing here touches a device beyond reading that one sysfs file, so the
// whole package is testable on any machine.
package rotate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Rotation is an fbcon rotation angle. The numbering is the kernel's, not
// ours: 0 is upright, and every step is a further quarter turn clockwise.
type Rotation uint8

// The four fbcon rotations. A uConsole with a portrait panel normally
// reports Clockwise.
const (
	None             Rotation = 0
	Clockwise        Rotation = 1
	UpsideDown       Rotation = 2
	CounterClockwise Rotation = 3
)

// SysfsPath is where fbcon publishes the console rotation. Reading it is how
// uScope matches the orientation the user already sees on tty1.
const SysfsPath = "/sys/class/graphics/fbcon/rotate"

// ErrInvalid is returned for anything that is not one of the four fbcon
// rotation numbers. Rotation is a uint8, so an unchecked conversion would
// silently accept 4 and then map pixels nowhere.
var ErrInvalid = errors.New("rotate: invalid rotation")

// Parse turns the fbcon numeral into a Rotation. Only "0" through "3" are
// accepted; the caller deals with "auto" before getting here.
func Parse(text string) (Rotation, error) {
	switch text {
	case "0":
		return None, nil
	case "1":
		return Clockwise, nil
	case "2":
		return UpsideDown, nil
	case "3":
		return CounterClockwise, nil
	default:
		return None, fmt.Errorf("%w: %q", ErrInvalid, text)
	}
}

// FromSysfs reads a rotation from an fbcon sysfs file. The kernel writes a
// numeral plus a newline, so the content is trimmed before parsing.
func FromSysfs(path string) (Rotation, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is a flag-controlled sysfs file, not user content.
	if err != nil {
		return None, fmt.Errorf("rotate: read %s: %w", path, err)
	}

	return Parse(strings.TrimSpace(string(b)))
}

// String renders the fbcon numeral, so it round-trips through Parse and
// prints as "rotate=1" in the startup line.
func (r Rotation) String() string {
	switch r {
	case None, Clockwise, UpsideDown, CounterClockwise:
		return strconv.FormatUint(uint64(r), 10)
	default:
		return "invalid"
	}
}

// Valid reports whether r is one of the four fbcon rotations.
func (r Rotation) Valid() bool {
	return r <= CounterClockwise
}

// Logical returns the canvas size for a physical framebuffer of pw by ph.
// A quarter turn either way swaps the axes; the other two leave them alone.
//
// makes the two signatures disagree.
//
//nolint:varnamelen // pw, ph pairs with Map; spelling them out here only
func (r Rotation) Logical(pw, ph int) (int, int) {
	if r == Clockwise || r == CounterClockwise {
		return ph, pw
	}

	return pw, ph
}

// Map converts a logical canvas pixel to its physical framebuffer pixel,
// given the physical size pw by ph.
//
// The two quarter turns are mirror images of each other, and picking the
// wrong one puts the image on the wrong edge rather than producing garbage,
// which makes it easy to miss. Clockwise is the one confirmed on the device:
// run the test pattern and the red square sits top-left with the cyan
// triangle pointing up. If it does not, --rotate overrides the sysfs guess.
//
// None shares the default branch: Rotation is a uint8, so a caller can hand
// us a 4, and treating that as upright beats mapping the frame off-buffer.
//
//nolint:varnamelen // x, y, pw, ph is the universal idiom for a coordinate map.
func (r Rotation) Map(x, y, pw, ph int) (int, int) {
	switch r {
	case Clockwise:
		return pw - 1 - y, x
	case CounterClockwise:
		return y, ph - 1 - x
	case UpsideDown:
		return pw - 1 - x, ph - 1 - y
	case None:
		fallthrough
	default:
		return x, y
	}
}
