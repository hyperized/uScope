//go:build integration && linux

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/affinity"
)

// buildBinary builds uScope into the test's temp directory, since what is
// under test is the process as an operator starts it, threads and all.
func buildBinary(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "uScope")

	//nolint:gosec // fixed arguments and the test's own temp path.
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	return binary
}

// allowedList reads one Cpus_allowed_list out of a /proc status file.
func allowedList(t *testing.T, path string) (string, bool) {
	t.Helper()

	text, err := os.ReadFile(path) //nolint:gosec // a /proc path the test built.
	if err != nil {
		return "", false // the thread exited between the listing and the read
	}

	for line := range strings.Lines(string(text)) {
		if value, ok := strings.CutPrefix(line, "Cpus_allowed_list:"); ok {
			return strings.TrimSpace(value), true
		}
	}

	t.Fatalf("%s has no Cpus_allowed_list", path)

	return "", false
}

// subsetOfOwn picks the upper half of the CPUs this test may use, so the
// pin is a real move away from where the kernel would put the threads.
func subsetOfOwn(t *testing.T) affinity.Set {
	t.Helper()

	own, _ := allowedList(t, "/proc/self/status")

	set, err := affinity.Parse(own)
	if err != nil {
		t.Fatalf("own Cpus_allowed_list %q: %v", own, err)
	}

	if set.Count() < 2 {
		t.Skipf("only %s is allowed here; pinning needs two CPUs to show anything", own)
	}

	keep := set.Count() / 2
	list := make([]string, 0, keep)

	for cpu := affinity.MaxCPU; cpu >= 0 && len(list) < keep; cpu-- {
		if set.Has(cpu) {
			list = append(list, strconv.Itoa(cpu))
		}
	}

	subset, err := affinity.Parse(strings.Join(list, ","))
	if err != nil {
		t.Fatalf("subset %v: %v", list, err)
	}

	return subset
}

// TestCPUsPinEveryThread starts the binary drawing half blocks into a pipe with --cpus
// on half the CPUs, lets it run until the render workers and the rest have
// started, and checks every thread under /proc/PID/task is on the subset,
// including the ones that did not exist when the pin was made.
//
//nolint:paralleltest // builds and runs a binary; slow enough without company.
func TestCPUsPinEveryThread(t *testing.T) {
	binary := buildBinary(t)
	subset := subsetOfOwn(t)

	//nolint:gosec // the binary the test just built, fixed flags.
	cmd := exec.CommandContext(t.Context(), binary, "--cpus", subset.String(), "--demo", "--view", "3d",
		"--backend", "blocks", "--fps", "30", "--frames", "1000")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	line, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the first stderr line: %v", err)
	}

	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	t.Logf("uScope said: %s", strings.TrimSpace(line))

	if !strings.Contains(line, "pinned") || !strings.HasSuffix(strings.TrimSpace(line), " to "+subset.String()) {
		t.Fatalf("first stderr line %q, want the pin announced on %s", line, subset)
	}

	// Long enough for the render pool, the frame writer and the GC workers to have
	// started threads of their own.
	time.Sleep(3 * time.Second)

	tasks, err := filepath.Glob("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/task/*/status")
	if err != nil || len(tasks) == 0 {
		t.Fatalf("no threads under /proc/%d: %v", cmd.Process.Pid, err)
	}

	seen := map[string]int{}

	for _, status := range tasks {
		if list, ok := allowedList(t, status); ok {
			seen[list]++
		}
	}

	t.Logf("%d threads after 3 s, by Cpus_allowed_list: %v", len(tasks), seen)

	if len(seen) != 1 || seen[subset.String()] == 0 {
		t.Errorf("threads on %v, want every one on %s", seen, subset)
	}
}

// TestCPUsRefused checks the binary stops at start when the kernel cannot
// keep the list: none of it exists, or only some of it does.
//
//nolint:paralleltest // builds and runs a binary.
func TestCPUsRefused(t *testing.T) {
	binary := buildBinary(t)

	for _, testCase := range []struct{ list, want string }{
		{list: "1023", want: affinity.ErrRefused.Error()},
		{list: "0,1023", want: affinity.ErrNotHeld.Error()},
	} {
		//nolint:gosec // the binary the test just built, fixed flags.
		out, err := exec.CommandContext(t.Context(), binary, "--cpus", testCase.list, "--demo",
			"--backend", "blocks", "--frames", "1").CombinedOutput()

		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != exitFailure {
			t.Errorf("--cpus %s: %v, want exit %d", testCase.list, err, exitFailure)
		}

		t.Logf("--cpus %s: %s", testCase.list, strings.TrimSpace(string(out)))

		if !strings.Contains(string(out), testCase.want) {
			t.Errorf("--cpus %s said %q, want %q in it", testCase.list, out, testCase.want)
		}
	}
}
