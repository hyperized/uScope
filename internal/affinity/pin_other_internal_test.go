//go:build !linux

package affinity

import (
	"errors"
	"testing"
)

// TestPinUnsupported checks every platform call refuses off Linux, and that
// Pin passes the refusal on rather than reporting success.
func TestPinUnsupported(t *testing.T) {
	t.Parallel()

	if _, err := platformListTasks(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("platformListTasks error = %v, want ErrUnsupported", err)
	}

	if _, err := platformGetTask(1); !errors.Is(err, ErrUnsupported) {
		t.Errorf("platformGetTask error = %v, want ErrUnsupported", err)
	}

	if err := platformSetTask(1, mustParse("0")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("platformSetTask error = %v, want ErrUnsupported", err)
	}

	if _, err := Pin(mustParse("0")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Pin error = %v, want ErrUnsupported", err)
	}
}
