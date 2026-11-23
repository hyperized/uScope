package affinity

import (
	"errors"
	"syscall"
	"testing"
)

// fakeTasks stands in for the kernel: a mask per thread, plus hooks a case
// can use to make a call fail or a thread appear.
type fakeTasks struct {
	held    map[int]Set
	order   []int
	listErr error

	// getErr and setErr fail the call for one thread. getAfter fails only
	// the read that follows a set, which is the read-back.
	getErr   map[int]error
	getAfter map[int]error
	setErr   map[int]error

	// narrow is what the kernel leaves a thread on instead of the mask it
	// was given, for the cpuset case.
	narrow map[int]Set

	// spawn is called on every set, so a case can clone new threads with
	// the old mask while the pass is running.
	spawn func(*fakeTasks)

	wasSet map[int]bool
	lists  int
}

func newFake(tasks ...int) *fakeTasks {
	fake := &fakeTasks{
		held: map[int]Set{}, getErr: map[int]error{}, getAfter: map[int]error{},
		setErr: map[int]error{}, narrow: map[int]Set{}, wasSet: map[int]bool{},
	}

	for _, task := range tasks {
		fake.start(task)
	}

	return fake
}

func (fake *fakeTasks) start(task int) {
	fake.held[task] = mustParse("0-7")
	fake.order = append(fake.order, task)
}

func (fake *fakeTasks) list() ([]int, error) {
	fake.lists++

	return append([]int(nil), fake.order...), fake.listErr
}

func (fake *fakeTasks) get(task int) (Set, error) {
	if err := fake.getAfter[task]; err != nil && fake.wasSet[task] {
		return Set{}, err
	}

	if err := fake.getErr[task]; err != nil {
		return Set{}, err
	}

	return fake.held[task], nil
}

func (fake *fakeTasks) set(task int, set Set) error {
	if err := fake.setErr[task]; err != nil {
		return err
	}

	fake.wasSet[task] = true
	fake.held[task] = set

	if narrowed, ok := fake.narrow[task]; ok {
		fake.held[task] = narrowed
	}

	if fake.spawn != nil {
		fake.spawn(fake)
	}

	return nil
}

// install swaps the seams for the fake and puts them back when the test
// ends. Callers must not be parallel.
func (fake *fakeTasks) install(t *testing.T) {
	t.Helper()

	list, get, set := listTasks, getTask, setTask

	t.Cleanup(func() { listTasks, getTask, setTask = list, get, set })

	listTasks, getTask, setTask = fake.list, fake.get, fake.set
}

func mustParse(text string) Set {
	set, err := Parse(text)
	if err != nil {
		panic(err)
	}

	return set
}

// TestPinMovesEveryThread checks a normal start: every thread moves on the
// first pass and a second pass confirms nothing is left.
func TestPinMovesEveryThread(t *testing.T) { //nolint:paralleltest // swaps the task seams.
	fake := newFake(10, 11, 12)
	fake.install(t)

	want := mustParse("4-7")

	count, err := Pin(want)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}

	if count != 3 || fake.lists != 2 {
		t.Errorf("Pin = %d threads in %d passes, want 3 in 2", count, fake.lists)
	}

	for task, held := range fake.held {
		if held != want {
			t.Errorf("thread %d on %s, want %s", task, held, want)
		}
	}
}

// TestPinCatchesLateThreads checks a thread cloned during a pass, with the
// mask of a thread not yet reached, is found and moved on the next one.
func TestPinCatchesLateThreads(t *testing.T) { //nolint:paralleltest // swaps the task seams.
	fake := newFake(10, 11)
	spawned := false
	fake.spawn = func(fake *fakeTasks) {
		if !spawned {
			spawned = true

			fake.start(99)
		}
	}
	fake.install(t)

	want := mustParse("4-7")

	count, err := Pin(want)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}

	if count != 3 || fake.held[99] != want || fake.lists != 3 {
		t.Errorf("Pin = %d threads in %d passes, late thread on %s", count, fake.lists, fake.held[99])
	}
}

// TestPinAlreadyThere checks a process already on the set is left alone.
func TestPinAlreadyThere(t *testing.T) { //nolint:paralleltest // swaps the task seams.
	fake := newFake(10)
	fake.held[10] = mustParse("4-7")
	fake.install(t)

	if count, err := Pin(mustParse("4-7")); err != nil || count != 1 || fake.wasSet[10] {
		t.Fatalf("Pin = %d, %v, set called %t; want 1, nil, false", count, err, fake.wasSet[10])
	}
}

// TestPinSkipsExitedThreads checks a thread that exits at any of the three
// points Pin touches it is skipped rather than reported.
func TestPinSkipsExitedThreads(t *testing.T) { //nolint:paralleltest // swaps the task seams.
	for _, testCase := range []struct { //nolint:paralleltest // swaps the task seams.
		name    string
		breakIt func(*fakeTasks)
	}{
		{name: "before the read", breakIt: func(fake *fakeTasks) { fake.getErr[11] = errGone }},
		{name: "before the set", breakIt: func(fake *fakeTasks) { fake.setErr[11] = errGone }},
		{name: "before the read-back", breakIt: func(fake *fakeTasks) { fake.getAfter[11] = errGone }},
	} {
		t.Run(testCase.name, func(t *testing.T) { //nolint:paralleltest // swaps the task seams.
			fake := newFake(10, 11)
			testCase.breakIt(fake)
			fake.install(t)

			if _, err := Pin(mustParse("4-7")); err != nil {
				t.Fatalf("Pin: %v", err)
			}

			if fake.held[10] != mustParse("4-7") {
				t.Errorf("thread 10 on %s, want 4-7", fake.held[10])
			}
		})
	}
}

// TestPinFailures checks every way the kernel can say no ends in an error
// naming the rule, and that nothing carries on after it.
func TestPinFailures(t *testing.T) { //nolint:paralleltest // swaps the task seams.
	for _, testCase := range []struct { //nolint:paralleltest // swaps the task seams.
		name    string
		breakIt func(*fakeTasks)
		want    error
	}{
		{
			name:    "listing fails",
			breakIt: func(fake *fakeTasks) { fake.listErr = ErrUnsupported },
			want:    ErrUnsupported,
		},
		{
			name:    "read fails",
			breakIt: func(fake *fakeTasks) { fake.getErr[10] = syscall.EFAULT },
			want:    syscall.EFAULT,
		},
		{
			name:    "no CPU usable",
			breakIt: func(fake *fakeTasks) { fake.setErr[10] = syscall.EINVAL },
			want:    ErrRefused,
		},
		{
			name:    "not allowed",
			breakIt: func(fake *fakeTasks) { fake.setErr[10] = syscall.EPERM },
			want:    syscall.EPERM,
		},
		{
			name:    "read-back fails",
			breakIt: func(fake *fakeTasks) { fake.getAfter[10] = syscall.EFAULT },
			want:    syscall.EFAULT,
		},
		{
			name:    "cpuset narrows it",
			breakIt: func(fake *fakeTasks) { fake.narrow[10] = mustParse("4-5") },
			want:    ErrNotHeld,
		},
		{
			name: "threads never stop starting",
			breakIt: func(fake *fakeTasks) {
				fake.spawn = func(fake *fakeTasks) { fake.start(len(fake.order) + 100) }
			},
			want: ErrUnsettled,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) { //nolint:paralleltest // swaps the task seams.
			fake := newFake(10)
			testCase.breakIt(fake)
			fake.install(t)

			count, err := Pin(mustParse("4-7"))
			if !errors.Is(err, testCase.want) || count != 0 {
				t.Fatalf("Pin = %d, %v; want 0, %v", count, err, testCase.want)
			}
		})
	}
}
