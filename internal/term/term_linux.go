//go:build linux

package term

// TCGETS and TCSETS from <asm-generic/ioctls.h>. Linux numbers these two by
// hand rather than with the _IOR macros, which is why they are this small.
const (
	ioctlGetAttr = 0x5401
	ioctlSetAttr = 0x5402
)
