//go:build !linux && !darwin

package term

import (
	"fmt"
	"runtime"
)

// Reader cannot read a terminal here; MakeRaw fails first, so the app never
// starts a key reader on this platform. It exists so callers compile.
type Reader struct{}

// NewReader returns a Reader whose every Read fails with ErrUnsupported.
func NewReader(_ uintptr) *Reader {
	return &Reader{}
}

// Read always fails.
func (*Reader) Read(_ []byte) (int, error) {
	return 0, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}
