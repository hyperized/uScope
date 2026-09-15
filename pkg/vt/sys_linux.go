//go:build linux

package vt

import (
	"fmt"
	"syscall"
	"unsafe"
)

// realSys is the production ioctl seam.
type realSys struct{}

// getMode reads the console's current mode. KDGETMODE writes a C int, so it
// takes an address; the conversion stays inside the syscall call because a
// uintptr that outlives the expression is not a valid pointer reference.
func (realSys) getMode(fd uintptr) (int32, error) {
	var mode int32

	//nolint:gosec // G103: the ioctl writes into mode, so it needs its address.
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, kdGetMode, uintptr(unsafe.Pointer(&mode)))
	if errno != 0 {
		return 0, fmt.Errorf("ioctl KDGETMODE: %w", errno)
	}

	return mode, nil
}

// setMode switches the console. KDSETMODE takes the mode by value, not by
// address, which is why this is a separate method.
func (realSys) setMode(fd uintptr, mode int32) error {
	//nolint:gosec // mode is one of the two KD_ constants, both small and positive.
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, kdSetMode, uintptr(mode)); errno != 0 {
		return fmt.Errorf("ioctl KDSETMODE %d: %w", mode, errno)
	}

	return nil
}
