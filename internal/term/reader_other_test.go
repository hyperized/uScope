//go:build !linux && !darwin

package term

import (
	"errors"
	"testing"
)

func TestReaderUnsupported(t *testing.T) {
	t.Parallel()

	_, err := NewReader(0).Read(make([]byte, 1))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
