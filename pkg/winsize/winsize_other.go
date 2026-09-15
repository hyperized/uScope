//go:build !linux && !darwin

package winsize

import (
	"fmt"
	"runtime"
)

// Get always fails here. The caller falls back to a default canvas size,
// the same path it takes when a real descriptor turns out not to be a
// terminal.
func Get(_ uintptr) (Size, error) {
	return Size{}, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}
