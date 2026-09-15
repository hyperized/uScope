//go:build integration && linux

package vt_test

import (
	"errors"
	"os"
	"testing"

	"github.com/hyperized/uScope/pkg/vt"
)

// TestGraphicsOnRealConsole exercises Graphics against the process's actual
// controlling terminal. It only proves anything on a real Linux console, so
// it skips rather than fails whenever that is not the case.
func TestGraphicsOnRealConsole(t *testing.T) {
	t.Parallel()

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("opening /dev/tty: %v", err)
	}

	t.Cleanup(func() {
		if err := tty.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	restore, err := vt.Graphics(tty)
	if err != nil {
		// Over ssh /dev/tty opens fine but is a pty, not a VT, so this is
		// the expected outcome rather than a failure.
		if errors.Is(err, vt.ErrNotConsole) {
			t.Skipf("not a console this process can switch: %v", err)
		}

		t.Fatalf("Graphics() error = %v", err)
	}

	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
}
