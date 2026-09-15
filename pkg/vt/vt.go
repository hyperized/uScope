// Package vt switches a Linux virtual terminal between text and graphics
// mode.
//
// In text mode the kernel console driver owns the screen and will repaint it
// over anything uScope draws, most visibly when the cursor blinks. KD_GRAPHICS
// tells it to keep its hands off until we put it back.
//
// The ioctl only works on the controlling terminal of the session, so it
// fails over ssh. That is expected rather than fatal: writing to /dev/fb0
// works regardless, so the caller warns and carries on.
package vt

import "errors"

// ErrNotConsole means the file is not a virtual terminal this process may
// drive. Over ssh, or with output redirected, this is the normal outcome and
// callers should degrade rather than exit.
var ErrNotConsole = errors.New("vt: not a console this process can switch")
