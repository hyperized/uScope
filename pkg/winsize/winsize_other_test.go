//go:build !linux && !darwin

package winsize //nolint:testpackage // matches the unix internal test file's package, per the brief for this file.

import (
	"errors"
	"testing"
)

func TestGet_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := Get(0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Get() error = %v, want ErrUnsupported", err)
	}
}
