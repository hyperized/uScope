//go:build darwin

package winsize

// ioctlGetWinsize is TIOCGWINSZ from <sys/ttycom.h>: _IOR('t', 104, struct
// winsize).
const ioctlGetWinsize = 0x40087468
