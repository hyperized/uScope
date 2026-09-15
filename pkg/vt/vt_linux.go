//go:build linux

package vt

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Console ioctls from <linux/kd.h>.
const (
	kdSetMode = 0x4B3A
	kdGetMode = 0x4B3B
)

// KD_TEXT and KD_GRAPHICS. The kernel reads and writes these as a C int.
const (
	kdText     int32 = 0
	kdGraphics int32 = 1
)

// sys is the ioctl seam. Tests supply a fake, because a unit test has no
// controlling terminal to switch.
//
// Two methods rather than one generic ioctl, so the unsafe.Pointer to uintptr
// conversion stays inside the syscall call expression. That is the only place
// the unsafe rules allow it: a uintptr holds no pointer semantics, so passing
// one across an interface call leaves the kernel with an address the garbage
// collector does not know is live. The race detector's checkptr catches it.
type sys interface {
	getMode(fd uintptr) (int32, error)
	setMode(fd uintptr, mode int32) error
}

// Graphics puts tty into graphics mode and returns a function that puts it
// back the way it was.
//
// The previous mode is read first rather than assumed to be KD_TEXT, so
// restoring does not fight whatever else set the mode. On failure the
// returned function is nil and the terminal is untouched.
func Graphics(tty *os.File) (func() error, error) {
	return enterGraphics(tty, realSys{})
}

// enterGraphics is Graphics with the seam exposed.
func enterGraphics(tty *os.File, seam sys) (func() error, error) {
	if tty == nil {
		return nil, fmt.Errorf("%w: no terminal given", ErrNotConsole)
	}

	descriptor := tty.Fd()

	previous, err := seam.getMode(descriptor)
	if err != nil {
		return nil, classify(err, "KDGETMODE")
	}

	// Only the two documented modes get restored. A driver reporting
	// anything else would otherwise be converted straight into a wild
	// uintptr, and text is the safe console to hand back.
	if previous != kdText && previous != kdGraphics {
		previous = kdText
	}

	if err := seam.setMode(descriptor, kdGraphics); err != nil {
		return nil, classify(err, "KDSETMODE")
	}

	restore := func() error {
		if err := seam.setMode(descriptor, previous); err != nil {
			return fmt.Errorf("vt: restoring console mode %d: %w", previous, err)
		}

		return nil
	}

	return restore, nil
}

// classify separates "you are not on a console" from a real failure.
//
// ENOTTY is the plain not-a-terminal case, EINVAL comes back from a
// terminal that is not a VT (a pty, so any ssh session), and EPERM from a VT
// this process does not own. All three mean degrade, not abort.
func classify(err error, what string) error {
	if errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("%w: %s: %w", ErrNotConsole, what, err)
	}

	return fmt.Errorf("vt: %s: %w", what, err)
}
