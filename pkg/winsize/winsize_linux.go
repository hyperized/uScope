//go:build linux

package winsize

// ioctlGetWinsize is TIOCGWINSZ from <asm-generic/ioctls.h>. Linux numbers
// this one by hand rather than with the _IOR macro, which is why it is this
// small.
const ioctlGetWinsize = 0x5413
