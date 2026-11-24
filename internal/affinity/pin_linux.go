//go:build linux

package affinity

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// taskDir lists one entry per thread of the calling process, named by its
// thread ID.
const taskDir = "/proc/self/task"

// maskBytes is the size handed to the affinity calls: the whole Set.
const maskBytes = words * wordBits / 8

func platformListTasks() ([]int, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("listing threads: %w", err)
	}

	tasks := make([]int, 0, len(entries))

	for _, entry := range entries {
		task, err := strconv.Atoi(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("listing threads: %s: %w", entry.Name(), err)
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func platformGetTask(task int) (Set, error) {
	var set Set

	// The kernel copies its own cpumask, nr_cpu_ids bits rounded up to a
	// long, and leaves the rest of the buffer alone, so the zero value is
	// what the CPUs it does not know about read as.
	//
	//nolint:gosec // G103: the kernel writes the mask, so it needs the address.
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_GETAFFINITY,
		uintptr(task), maskBytes, uintptr(unsafe.Pointer(&set.mask)))

	return set, taskError(errno)
}

func platformSetTask(task int, set Set) error {
	//nolint:gosec // G103: the kernel reads the mask from this address.
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY,
		uintptr(task), maskBytes, uintptr(unsafe.Pointer(&set.mask)))

	return taskError(errno)
}

// taskError turns an errno into nil, errGone for a thread that exited, or
// the errno itself.
func taskError(errno syscall.Errno) error {
	switch {
	case errno == 0:
		return nil
	case errors.Is(errno, syscall.ESRCH):
		return errGone
	default:
		return errno
	}
}
