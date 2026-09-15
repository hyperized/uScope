//go:build integration && (linux || darwin)

package winsize_test

import (
	"errors"
	"os"
	"testing"

	"github.com/hyperized/uScope/pkg/winsize"
)

// TestGetOnRealTTY exercises Get against the process's controlling
// terminal. It only proves anything when one exists, so it skips rather
// than fails over ssh or under a CI runner that has none.
func TestGetOnRealTTY(t *testing.T) {
	t.Parallel()

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no controlling terminal: %v", err)
	}

	t.Cleanup(func() {
		if err := tty.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	size, err := winsize.Get(tty.Fd())
	if err != nil {
		if errors.Is(err, winsize.ErrNotTerminal) {
			t.Skipf("not a terminal: %v", err)
		}

		t.Fatalf("Get() error = %v", err)
	}

	if size.Cols <= 0 || size.Rows <= 0 {
		t.Fatalf("Get() = %+v, want positive Cols and Rows", size)
	}
}
