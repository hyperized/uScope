//go:build linux

package fbdev

import (
	"fmt"
	"syscall"
	"unsafe"
)

// realSys is the production seam: three raw syscalls and nothing else.
//
// These are here rather than inline so the rest of the package stays free of
// unsafe and can be unit tested on any machine. There is no x/sys dependency
// on purpose; the whole of uScope is standard library.
type realSys struct{}

// ioctl issues a framebuffer ioctl. arg points at the struct the kernel
// fills in.
func (realSys) ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return fmt.Errorf("ioctl request 0x%x: %w", req, errno)
	}

	return nil
}

// mmap maps the whole framebuffer shared and writable.
func (realSys) mmap(fd int, length int) ([]byte, error) {
	mem, err := syscall.Mmap(fd, 0, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap %d bytes: %w", length, err)
	}

	return mem, nil
}

// munmap releases the mapping.
func (realSys) munmap(mem []byte) error {
	if err := syscall.Munmap(mem); err != nil {
		return fmt.Errorf("munmap: %w", err)
	}

	return nil
}
