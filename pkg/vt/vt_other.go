//go:build !linux

package vt

import (
	"fmt"
	"os"
	"runtime"
)

// Graphics always reports ErrNotConsole off Linux, since no other platform
// has a Linux virtual terminal to switch. Callers already degrade on this
// error, so nothing else has to know the difference.
func Graphics(_ *os.File) (func() error, error) {
	return nil, fmt.Errorf("%w: %s has no Linux VT", ErrNotConsole, runtime.GOOS)
}
