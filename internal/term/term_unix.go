//go:build linux || darwin

package term

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// readTimeoutTenths is VTIME, in tenths of a second. With VMIN at 0 a read
// returns after at most this long with whatever arrived, including nothing.
// That timeout is what bounds how long the reader goroutine takes to notice
// a cancelled context, so it doubles as the shutdown latency.
const readTimeoutTenths = 1

// sys is the ioctl seam, so the flag arithmetic can be tested without a
// terminal.
type sys interface {
	ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error
}

// realSys is the production seam.
type realSys struct{}

// MakeRaw switches the terminal on fd into raw mode and returns a function
// that restores the settings that were there before.
//
// Restoring matters more than it looks: a process that exits without it
// leaves the user's shell with no echo, which reads as a hung machine.
func MakeRaw(fd uintptr) (func() error, error) {
	return rawMode(fd, realSys{})
}

// ioctl issues a terminal ioctl against a termios struct.
func (realSys) ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return fmt.Errorf("ioctl request 0x%x: %w", req, errno)
	}

	return nil
}

// rawMode is MakeRaw with the seam exposed.
func rawMode(descriptor uintptr, seam sys) (func() error, error) {
	var original syscall.Termios

	//nolint:gosec // G103: the ioctl fills this struct, so it needs its address.
	if err := seam.ioctl(descriptor, ioctlGetAttr, unsafe.Pointer(&original)); err != nil {
		return nil, classify(err, "read terminal attributes")
	}

	raw := original
	applyRaw(&raw)

	//nolint:gosec // G103: as above.
	if err := seam.ioctl(descriptor, ioctlSetAttr, unsafe.Pointer(&raw)); err != nil {
		return nil, classify(err, "set raw mode")
	}

	restore := func() error {
		saved := original

		//nolint:gosec // G103: as above.
		if err := seam.ioctl(descriptor, ioctlSetAttr, unsafe.Pointer(&saved)); err != nil {
			return fmt.Errorf("term: restoring terminal attributes: %w", err)
		}

		return nil
	}

	return restore, nil
}

// applyRaw clears the line discipline's cooking.
//
// Lflag drops signal generation, line buffering, echo and the editing
// extensions. Iflag drops flow control and CR translation, because ^S must
// reach the program and Enter must stay distinguishable. Oflag drops output
// post-processing so a lone newline does not become CR LF over the frame.
// Cflag is forced to 8 bits, no parity.
func applyRaw(attr *syscall.Termios) {
	attr.Lflag &^= syscall.ISIG | syscall.ICANON | syscall.ECHO | syscall.IEXTEN
	attr.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	attr.Oflag &^= syscall.OPOST
	attr.Cflag &^= syscall.CSIZE | syscall.PARENB
	attr.Cflag |= syscall.CS8

	attr.Cc[syscall.VMIN] = 0
	attr.Cc[syscall.VTIME] = readTimeoutTenths
}

// classify turns the not-a-terminal case into a sentinel the caller can
// degrade on, and leaves anything else as a real failure.
func classify(err error, what string) error {
	if notTerminal(err) {
		return fmt.Errorf("%w: %s: %w", ErrNotTerminal, what, err)
	}

	return fmt.Errorf("term: %s: %w", what, err)
}

// notTerminal reports whether an errno means "this descriptor is not a
// terminal".
//
// There are two answers to the same question. A pipe gives ENOTTY on both
// kernels, but /dev/null gives ENODEV on Darwin, which is what a program
// started with stdin redirected from /dev/null hits. Treating only ENOTTY as
// degradable made uScope refuse to start there instead of running on without
// a keyboard.
func notTerminal(err error) bool {
	return errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.ENODEV)
}
