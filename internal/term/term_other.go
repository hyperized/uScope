//go:build !linux && !darwin

package term

import (
	"fmt"
	"runtime"
)

// MakeRaw always fails here. The caller warns and runs without keyboard
// control, which is the same path it takes when stdin is a pipe.
func MakeRaw(_ uintptr) (func() error, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}
