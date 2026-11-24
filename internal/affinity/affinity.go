// Package affinity parses a CPU list such as "4-7" or "0,2,4-7" and pins the
// whole process to it.
//
// The parsing is pure and works on every platform, so a flag can be checked
// on a Mac. The pinning is Linux only, and it has to visit every thread:
// sched_setaffinity moves one thread, and by the time main runs the Go
// runtime already has several. Pin sets each task under /proc/self/task and
// goes round again until a pass finds nothing left to move, because a thread
// cloned from one that was not yet pinned inherits the old mask.
package affinity

import (
	"errors"
	"fmt"
	"math/bits"
	"strconv"
)

const (
	// MaxCPU is the highest CPU number a list may name. It is glibc's
	// CPU_SETSIZE less one, which is also the mask the syscalls are handed.
	// Nothing this runs on comes close; the bound is there so a typo is a
	// refused flag rather than a mask the kernel reads past.
	MaxCPU = 1023

	// maxListLength caps the text before it is read. Every CPU up to MaxCPU
	// written out one by one would be longer, but nobody types that, and the
	// value comes from a manifest anyone with commit access can edit.
	maxListLength = 256

	wordBits = 64
	words    = (MaxCPU + 1) / wordBits

	// maxDigits is how many digits MaxCPU has.
	maxDigits = 4

	decimal = 10
)

// Parsing errors. Each says which rule the list broke.
var (
	ErrEmpty  = errors.New("affinity: empty CPU list")
	ErrSyntax = errors.New("affinity: a CPU list is numbers and ascending ranges separated by commas")
	ErrRange  = errors.New("affinity: CPU number above " + strconv.Itoa(MaxCPU))
	ErrOrder  = errors.New("affinity: range runs backwards")
)

// Pinning errors.
var (
	// ErrUnsupported is returned by Pin on any platform but Linux.
	ErrUnsupported = errors.New("affinity: pinning to CPUs is Linux only")

	// ErrRefused means the kernel turned the mask down, with the errno
	// wrapped alongside: EINVAL when none of the CPUs is one the process may
	// use, EPERM when it is not allowed to change affinity at all.
	ErrRefused = errors.New("affinity: the kernel refused the CPU list")

	// ErrNotHeld means the kernel accepted the mask but kept the thread on a
	// different one. sched_setaffinity quietly drops CPUs that are offline
	// or outside the cgroup's cpuset as long as one of them is left, so a
	// list naming a CPU the machine does not have succeeds without this
	// check.
	ErrNotHeld = errors.New("affinity: the kernel did not keep the CPU list")

	// ErrUnsettled means threads kept appearing faster than Pin could move
	// them for every round it is willing to make.
	ErrUnsettled = errors.New("affinity: threads kept starting on other CPUs")
)

// Set is a set of CPU numbers from 0 to MaxCPU. The zero value is empty. It
// is a value type with no pointers in it, so copies are independent and two
// sets compare with ==.
type Set struct {
	mask [words]uint64
}

// Parse reads a CPU list in the kernel's cpulist format: decimal numbers and
// ranges like 4-7, separated by commas. Ranges run upwards; overlaps are
// allowed and merge. Nothing else is accepted: no spaces, no strides, no
// leading zeros, no empty items. An empty string is ErrEmpty.
func Parse(text string) (Set, error) {
	var set Set

	if text == "" {
		return set, ErrEmpty
	}

	if len(text) > maxListLength {
		return set, fmt.Errorf("%w: %d characters, want at most %d", ErrSyntax, len(text), maxListLength)
	}

	for start := 0; start <= len(text); {
		end := start
		for end < len(text) && text[end] != ',' {
			end++
		}

		low, high, err := parseItem(text[start:end])
		if err != nil {
			return Set{}, fmt.Errorf("%w in %q", err, text)
		}

		for cpu := low; cpu <= high; cpu++ {
			set.mask[cpu/wordBits] |= 1 << (cpu % wordBits)
		}

		start = end + 1
	}

	return set, nil
}

// parseItem reads one comma-separated item: a number, or two joined by a
// dash.
func parseItem(item string) (int, int, error) {
	for index := range len(item) {
		if item[index] != '-' {
			continue
		}

		low, err := parseCPU(item[:index])
		if err != nil {
			return 0, 0, err
		}

		high, err := parseCPU(item[index+1:])
		if err != nil {
			return 0, 0, err
		}

		if high < low {
			return 0, 0, fmt.Errorf("%w: %s", ErrOrder, item)
		}

		return low, high, nil
	}

	cpu, err := parseCPU(item)

	return cpu, cpu, err
}

// parseCPU reads one CPU number. It walks the bytes itself rather than
// calling strconv.Atoi, which would take a sign and, in a range, a second
// dash for a negative number.
func parseCPU(text string) (int, error) {
	if text == "" || len(text) > maxDigits || (text[0] == '0' && len(text) > 1) {
		return 0, fmt.Errorf("%w: %q is not a CPU number", ErrSyntax, text)
	}

	cpu := 0

	for index := range len(text) {
		digit := text[index]
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("%w: %q is not a CPU number", ErrSyntax, text)
		}

		cpu = cpu*decimal + int(digit-'0')
	}

	if cpu > MaxCPU {
		return 0, fmt.Errorf("%w: %d", ErrRange, cpu)
	}

	return cpu, nil
}

// Has reports whether cpu is in the set. A number outside 0 to MaxCPU is
// never in it.
func (s Set) Has(cpu int) bool {
	if cpu < 0 || cpu > MaxCPU {
		return false
	}

	return s.mask[cpu/wordBits]&(1<<(cpu%wordBits)) != 0
}

// Count is how many CPUs the set holds.
func (s Set) Count() int {
	total := 0
	for _, word := range s.mask {
		total += bits.OnesCount64(word)
	}

	return total
}

// IsEmpty reports whether the set holds no CPU.
func (s Set) IsEmpty() bool {
	return s == Set{}
}

// String is the set in canonical cpulist form, runs folded into ranges, the
// way the kernel prints Cpus_allowed_list: "0,2,4-7". The empty set is "".
func (s Set) String() string {
	text := make([]byte, 0, wordBits)

	for cpu := 0; cpu <= MaxCPU; cpu++ {
		if !s.Has(cpu) {
			continue
		}

		last := cpu
		for last < MaxCPU && s.Has(last+1) {
			last++
		}

		if len(text) > 0 {
			text = append(text, ',')
		}

		text = strconv.AppendInt(text, int64(cpu), decimal)
		if last > cpu {
			text = append(text, '-')
			text = strconv.AppendInt(text, int64(last), decimal)
		}

		cpu = last
	}

	return string(text)
}
