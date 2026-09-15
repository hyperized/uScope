//go:build linux || darwin

package winsize

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// sys is the ioctl seam, so the field mapping can be tested without a
// terminal.
type sys interface {
	ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error
}

// realSys is the production seam.
type realSys struct{}

// ioctl issues the window-size ioctl against a winsizeIoctl struct.
//
// Coverage note: the success return needs a real terminal, so a unit test on
// any developer machine can only reach the failure path. The integration
// test in this package covers the success path on a machine with a tty.
func (realSys) ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return fmt.Errorf("ioctl request 0x%x: %w", req, errno)
	}

	return nil
}

// winsizeIoctl is struct winsize from <sys/ioctl.h>: four unsigned shorts,
// same layout on Linux and Darwin.
type winsizeIoctl struct{ Row, Col, Xpixel, Ypixel uint16 }

// Get reports the size of the terminal on fd, in character cells and, when
// the terminal fills them in, pixels.
//
// See Size's doc comment: XPixels and YPixels are often zero, and callers
// must treat them as optional.
func Get(fd uintptr) (Size, error) {
	return get(fd, realSys{})
}

// get is Get with the seam exposed.
//
//nolint:revive // confusing-naming: Get/get is the exported/unexported seam pair, as with term's MakeRaw/rawMode.
func get(fd uintptr, seam sys) (Size, error) {
	var raw winsizeIoctl

	//nolint:gosec // G103: the ioctl fills this struct, so it needs its address.
	if err := seam.ioctl(fd, ioctlGetWinsize, unsafe.Pointer(&raw)); err != nil {
		return Size{}, classify(err)
	}

	return Size{
		Cols:    int(raw.Col),
		Rows:    int(raw.Row),
		XPixels: int(raw.Xpixel),
		YPixels: int(raw.Ypixel),
	}, nil
}

// classify turns the not-a-terminal case into a sentinel the caller can
// degrade on, and leaves anything else as a real failure.
func classify(err error) error {
	// Two errnos for one answer: a pipe reports ENOTTY on both kernels,
	// while /dev/null reports ENODEV on Darwin. Both mean there is no
	// window to measure, and the caller falls back to pipe mode for either.
	if errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.ENODEV) {
		return fmt.Errorf("%w: %w", ErrNotTerminal, err)
	}

	return fmt.Errorf("winsize: get window size: %w", err)
}
