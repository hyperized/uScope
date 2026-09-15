package vt_test

import (
	"errors"
	"testing"

	"github.com/hyperized/uScope/pkg/vt"
)

// TestErrNotConsole checks the sentinel itself: non-nil, with a message
// that explains what happened.
func TestErrNotConsole(t *testing.T) {
	t.Parallel()

	if vt.ErrNotConsole == nil {
		t.Fatal("ErrNotConsole is nil")
	}

	if vt.ErrNotConsole.Error() == "" {
		t.Fatal("ErrNotConsole.Error() is empty")
	}
}

// TestGraphicsNilIsNotConsole is the one assertion that holds on every
// platform: Graphics(nil) fails with ErrNotConsole everywhere, on Linux
// because of the explicit nil check and elsewhere because the stub always
// returns it.
func TestGraphicsNilIsNotConsole(t *testing.T) {
	t.Parallel()

	_, err := vt.Graphics(nil)
	if !errors.Is(err, vt.ErrNotConsole) {
		t.Fatalf("Graphics(nil) error = %v, want ErrNotConsole", err)
	}
}
