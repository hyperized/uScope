//go:build linux

package affinity

import (
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"
)

// TestTaskError checks the errno mapping: zero is success, ESRCH is a thread
// that has gone, and anything else is passed through for errors.Is.
func TestTaskError(t *testing.T) {
	t.Parallel()

	if err := taskError(0); err != nil {
		t.Errorf("taskError(0) = %v, want nil", err)
	}

	if err := taskError(syscall.ESRCH); !errors.Is(err, errGone) {
		t.Errorf("taskError(ESRCH) = %v, want errGone", err)
	}

	if err := taskError(syscall.EINVAL); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("taskError(EINVAL) = %v, want EINVAL", err)
	}
}

// TestPlatformCalls runs the real calls against this test process: the main
// thread is listed, its mask reads back non-empty, setting the mask it
// already has is accepted, and a thread that does not exist is gone.
//
// It pins to the mask the process already has, so it moves nothing and is
// safe in a shared test binary.
func TestPlatformCalls(t *testing.T) {
	t.Parallel()

	tasks, err := platformListTasks()
	if err != nil {
		t.Fatalf("platformListTasks: %v", err)
	}

	if !slices.Contains(tasks, os.Getpid()) {
		t.Fatalf("platformListTasks() = %v, want the main thread %d in it", tasks, os.Getpid())
	}

	held, err := platformGetTask(os.Getpid())
	if err != nil || held.IsEmpty() {
		t.Fatalf("platformGetTask = %s, %v; want a non-empty mask", held, err)
	}

	if err := platformSetTask(os.Getpid(), held); err != nil {
		t.Fatalf("platformSetTask(own mask): %v", err)
	}

	// PID_MAX_LIMIT is 1<<22, so nothing can have this thread ID.
	const noSuchTask = 1 << 23

	if _, err := platformGetTask(noSuchTask); !errors.Is(err, errGone) {
		t.Errorf("platformGetTask(%d) error = %v, want errGone", noSuchTask, err)
	}
}
