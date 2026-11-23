package affinity

import (
	"errors"
	"fmt"
)

// maxRounds is how many passes over the thread list Pin makes before giving
// up. Each pass leaves every thread it saw pinned, so only a thread cloned
// during the pass from one not yet reached can need another. The Go runtime
// starts a handful of threads at most while main is starting up; two passes
// are the norm and eight is far past anything seen.
const maxRounds = 8

// errGone is what a task seam returns for a thread that exited between
// being listed and being touched. Pin skips it: a thread that no longer
// exists runs nowhere.
var errGone = errors.New("affinity: thread has exited")

// The seams between Pin's bookkeeping and the system calls. The Linux file
// fills them with the real thing and every other platform with a refusal;
// tests swap in fakes, which is why a test that does must not run in
// parallel with another.
//
//nolint:gochecknoglobals // test seams; production holds the platform's own.
var (
	listTasks = platformListTasks
	getTask   = platformGetTask
	setTask   = platformSetTask
)

// Pin moves every thread of the process onto the CPUs in set, and returns
// how many threads it found on its last pass, all of them by then on set.
// Threads started afterwards inherit the mask from the thread that starts
// them.
//
// It errors with ErrEmpty for an empty set, ErrUnsupported off Linux,
// ErrRefused when the kernel turns the mask down, ErrNotHeld when the kernel
// accepts it but narrows it (a CPU the machine lacks or the cgroup's cpuset
// excludes), and ErrUnsettled if new threads outrun it. On any error some
// threads may already be pinned; the caller is expected to stop.
//
// Pin is safe to call from any goroutine, but it is meant to run once at
// start, before the process has much to say about where its threads are.
func Pin(set Set) (int, error) {
	if set.IsEmpty() {
		return 0, ErrEmpty
	}

	for range maxRounds {
		tasks, err := listTasks()
		if err != nil {
			return 0, err
		}

		moved, err := pinTasks(tasks, set)
		if err != nil {
			return 0, err
		}

		if moved == 0 {
			return len(tasks), nil
		}
	}

	return 0, fmt.Errorf("%w after %d passes", ErrUnsettled, maxRounds)
}

// pinTasks gives each task the mask it does not have yet and reports how
// many it had to move.
func pinTasks(tasks []int, set Set) (int, error) {
	moved := 0

	for _, task := range tasks {
		changed, err := pinTask(task, set)
		if err != nil {
			return 0, err
		}

		if changed {
			moved++
		}
	}

	return moved, nil
}

// pinTask moves one thread and reads the mask back, since the kernel's word
// that it took the mask is not the same as it holding the whole of it.
func pinTask(task int, set Set) (bool, error) {
	held, err := getTask(task)
	if errors.Is(err, errGone) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("reading thread %d: %w", task, err)
	}

	if held == set {
		return false, nil
	}

	err = setTask(task, set)
	if errors.Is(err, errGone) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("%w: %s on thread %d: %w", ErrRefused, set, task, err)
	}

	held, err = getTask(task)
	if errors.Is(err, errGone) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("reading thread %d: %w", task, err)
	}

	if held != set {
		return false, fmt.Errorf("%w: asked for %s, thread %d is on %s", ErrNotHeld, set, task, held)
	}

	return true, nil
}
