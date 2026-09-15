//go:build darwin

package term

// TIOCGETA and TIOCSETA from <sys/ttycom.h>. These are _IOR('t', 19, struct
// termios) and _IOW('t', 20, struct termios), so the 0x48 in the middle is
// sizeof(struct termios) and changes if the struct ever does.
const (
	ioctlGetAttr = 0x40487413
	ioctlSetAttr = 0x80487414
)
