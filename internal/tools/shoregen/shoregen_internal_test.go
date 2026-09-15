package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/shore"
)

// The testdata fixtures every test below reads. Naming them once here keeps a
// typo from silently pointing two tests at two different files.
const (
	coastlineFixture = "testdata/coastline.geojson"
	lakesFixture     = "testdata/lakes.geojson"
	brokenFixture    = "testdata/broken.geojson"
	missingFixture   = "testdata/does-not-exist.geojson"
)

// The three flag names, named once so goconst does not see a dozen repeated
// string literals across the table-driven cases below.
const (
	flagCoastline = "-coastline"
	flagLakes     = "-lakes"
	flagOut       = "-out"
)

// TestRunErrors drives run through every error branch that a single process
// can reach on its own: a bad flag, a missing input on either side, and an
// input that is not GeoJSON at all.
func TestRunErrors(t *testing.T) {
	t.Parallel()

	// None of the cases below ever reach write, so this path is never
	// actually created. It still lives outside the repository rather than
	// inside testdata, in case that ever stops being true.
	unusedOut := filepath.Join(t.TempDir(), "unused.bin.gz")

	for _, testCase := range []struct {
		name          string
		args          []string
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:          "an unknown flag is a parse failure before anything is opened",
			args:          []string{"-nope"},
			wantErrSubstr: "parsing flags",
		},
		{
			name:          "the default paths do not exist in the package's own directory",
			wantErrSubstr: "opening the coastline",
		},
		{
			name: "a missing coastline file is named in the error",
			args: []string{
				flagCoastline, missingFixture,
				flagLakes, lakesFixture,
				flagOut, unusedOut,
			},
			wantErrSubstr: "opening the coastline",
		},
		{
			name: "a missing lakes file is named in the error",
			args: []string{
				flagCoastline, coastlineFixture,
				flagLakes, missingFixture,
				flagOut, unusedOut,
			},
			wantErrSubstr: "opening the lakes",
		},
		{
			name: "a coastline file that is not json fails the build with ErrGeoJSON",
			args: []string{
				flagCoastline, brokenFixture,
				flagLakes, lakesFixture,
				flagOut, unusedOut,
			},
			wantErrIs: shore.ErrGeoJSON,
		},
		{
			name: "a lakes file that is not json fails the build with ErrGeoJSON too",
			args: []string{
				flagCoastline, coastlineFixture,
				flagLakes, brokenFixture,
				flagOut, unusedOut,
			},
			wantErrIs: shore.ErrGeoJSON,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			err := run(testCase.args, &stdout)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want non-nil", testCase.args)
			}

			if testCase.wantErrIs != nil && !errors.Is(err, testCase.wantErrIs) {
				t.Errorf("run(%v) error = %v, want errors.Is match for %v", testCase.args, err, testCase.wantErrIs)
			}

			if testCase.wantErrSubstr != "" && !strings.Contains(err.Error(), testCase.wantErrSubstr) {
				t.Errorf("run(%v) error = %q, want to contain %q", testCase.args, err.Error(), testCase.wantErrSubstr)
			}
		})
	}
}

// wantPolylines and wantPoints are what shore.Build produces from the two
// fixtures: three coastline lines of four, three and two points, plus the one
// lake ring big enough to survive the area filter, at five points. The other
// lake in lakes.geojson is well under the five square kilometre threshold and
// contributes nothing, which is the point of including it.
const (
	wantPolylines = 4
	wantPoints    = 14

	// The box below has to cover both cells the fixtures land in: the
	// coastline sits east of 4 degrees, the lakes east of 5, and a five
	// degree grid puts those in neighbouring cells.
	withinLatMin = 51.0
	withinLatMax = 54.0
	withinLonMin = 3.0
	withinLonMax = 6.5

	// fixtureLat and fixtureLon are the coastline LineString's first point,
	// which Within must hand back within the fixed point codec's rounding.
	fixtureLat = 52.0
	fixtureLon = 4.0

	// pointEpsilon is well past the codec's worst case: it packs coordinates
	// at 1e-5 degrees, several orders of magnitude finer than this.
	pointEpsilon = 1e-4
)

// TestRunHappyPath runs the whole generator over the testdata fixtures and
// checks what came out the other end: the packed file decodes, the shoreline
// it holds sits where the fixtures put it, and the counts run reports match
// what shore.Build actually produced.
func TestRunHappyPath(t *testing.T) {
	t.Parallel()

	outPath := filepath.Join(t.TempDir(), "shore.bin.gz")

	args := []string{
		flagCoastline, coastlineFixture,
		flagLakes, lakesFixture,
		flagOut, outPath,
	}

	var stdout bytes.Buffer

	if err := run(args, &stdout); err != nil {
		t.Fatalf("run(%v) error = %v, want nil", args, err)
	}

	wantStdout := fmt.Sprintf("wrote %s: %d polylines, %d points\n", outPath, wantPolylines, wantPoints)
	if got := stdout.String(); got != wantStdout {
		t.Errorf("run(%v) stdout = %q, want %q", args, got, wantStdout)
	}

	packed, err := os.Open(outPath) //nolint:gosec // outPath is built above from t.TempDir(), not attacker input.
	if err != nil {
		t.Fatalf("os.Open(%q) error = %v, want nil", outPath, err)
	}

	defer func() { _ = packed.Close() }()

	set, err := shore.Decode(packed)
	if err != nil {
		t.Fatalf("shore.Decode(%q) error = %v, want nil", outPath, err)
	}

	var (
		visited []shore.Polyline
		points  int
	)

	set.Within(withinLatMin, withinLatMax, withinLonMin, withinLonMax, func(line shore.Polyline) {
		kept := make(shore.Polyline, len(line))
		copy(kept, line)
		visited = append(visited, kept)
		points += len(kept)
	})

	if len(visited) != wantPolylines {
		t.Errorf("Within(...) visited %d polylines, want %d", len(visited), wantPolylines)
	}

	if points != wantPoints {
		t.Errorf("Within(...) visited %d points, want %d", points, wantPoints)
	}

	if !containsPoint(visited, fixtureLat, fixtureLon, pointEpsilon) {
		t.Errorf("Within(...) = %v, want it to contain the coastline fixture's first point", visited)
	}
}

// containsPoint reports whether any of the given polylines holds a point
// within epsilon degrees of lat and lon on both axes.
func containsPoint(lines []shore.Polyline, lat, lon, epsilon float64) bool {
	for _, line := range lines {
		for _, point := range line {
			if math.Abs(point.Lat-lat) < epsilon && math.Abs(point.Lon-lon) < epsilon {
				return true
			}
		}
	}

	return false
}

// TestWriteCreateFails covers write's own failure to create the output file.
//
// Encode failing on the writer, and the file's Close failing, are not
// exercised anywhere in this package. Both need the *os.File write hands to
// shore.Encode to fail after os.Create has already succeeded on the same
// call, and write opens that file itself from the path argument, so nothing
// in this package can hand it a writer that fails on demand. Reaching either
// branch for real would need something like a disk quota, or a file size
// rlimit paired with ignoring SIGXFSZ for the whole process, and both trade a
// reliable, parallel-safe suite for two more covered lines. That is not a
// good trade here, so those two branches are reported as uncovered rather
// than forced.
func TestWriteCreateFails(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "a directory in place of the output file cannot be created over",
			path: func(t *testing.T) string {
				t.Helper()

				return t.TempDir()
			},
		},
		{
			name: "a parent directory that does not exist cannot be created into",
			path: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "missing-parent", "out.bin.gz")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := testCase.path(t)

			var stdout bytes.Buffer

			err := write(path, nil, &stdout)
			if err == nil {
				t.Fatalf("write(%q) error = nil, want non-nil", path)
			}

			const wantSubstr = "creating"

			if !strings.Contains(err.Error(), wantSubstr) {
				t.Errorf("write(%q) error = %q, want to contain %q", path, err.Error(), wantSubstr)
			}
		})
	}
}

// The placeholder paths TestParseSuccess and TestParseErrors feed through the
// flags. parse and check never touch a filesystem, so any non-empty string
// will do; naming them once keeps goconst from seeing the same literal
// repeated across both tables.
const (
	placeholderCoastline = "coast.geojson"
	placeholderLakes     = "lakes.geojson"
	placeholderOut       = "out.bin.gz"
)

// TestParseSuccess checks what parse hands back when there is nothing to
// refuse: the three defaults with no flags, and all three overridden. A
// bytes.Buffer takes the place of the caller's output stream throughout, so a
// bad flag's usage text never lands on this test's own output.
func TestParseSuccess(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		args      []string
		wantPaths paths
	}{
		{
			name:      "no flags at all keeps the three defaults",
			wantPaths: paths{coastline: defaultCoastline, lakes: defaultLakes, output: defaultOutput},
		},
		{
			name: "all three flags override their defaults",
			args: []string{
				flagCoastline, placeholderCoastline,
				flagLakes, placeholderLakes,
				flagOut, placeholderOut,
			},
			wantPaths: paths{coastline: placeholderCoastline, lakes: placeholderLakes, output: placeholderOut},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			got, err := parse(testCase.args, &output)
			if err != nil {
				t.Fatalf("parse(%v) error = %v, want nil", testCase.args, err)
			}

			if got != testCase.wantPaths {
				t.Errorf("parse(%v) = %+v, want %+v", testCase.args, got, testCase.wantPaths)
			}
		})
	}
}

// TestParseErrors exercises every way parse can refuse its input: an unknown
// flag, and check's three empty-path rejections.
func TestParseErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name          string
		args          []string
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:          "an unknown flag is reported as a parse failure",
			args:          []string{"-nope"},
			wantErrSubstr: "parsing flags",
		},
		{
			name: "an empty coastline path is refused and named",
			args: []string{
				flagCoastline, "",
				flagLakes, placeholderLakes,
				flagOut, placeholderOut,
			},
			wantErrIs:     errEmptyPath,
			wantErrSubstr: flagCoastline,
		},
		{
			name: "an empty lakes path is refused and named",
			args: []string{
				flagCoastline, placeholderCoastline,
				flagLakes, "",
				flagOut, placeholderOut,
			},
			wantErrIs:     errEmptyPath,
			wantErrSubstr: flagLakes,
		},
		{
			name: "an empty output path is refused and named",
			args: []string{
				flagCoastline, placeholderCoastline,
				flagLakes, placeholderLakes,
				flagOut, "",
			},
			wantErrIs:     errEmptyPath,
			wantErrSubstr: flagOut,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			_, err := parse(testCase.args, &output)
			if err == nil {
				t.Fatalf("parse(%v) error = nil, want non-nil", testCase.args)
			}

			if testCase.wantErrIs != nil && !errors.Is(err, testCase.wantErrIs) {
				t.Errorf("parse(%v) error = %v, want errors.Is match for %v", testCase.args, err, testCase.wantErrIs)
			}

			if !strings.Contains(err.Error(), testCase.wantErrSubstr) {
				t.Errorf("parse(%v) error = %q, want to contain %q", testCase.args, err.Error(), testCase.wantErrSubstr)
			}
		})
	}
}

// helperProcessEnv and helperArgsEnv pass a subprocess invocation's intent to
// TestHelperMain, the only test allowed to call main: main ends in os.Exit,
// and calling it from the process running the real suite would end that too.
const (
	helperProcessEnv = "SHOREGEN_HELPER_PROCESS"
	helperArgsEnv    = "SHOREGEN_HELPER_ARGS"
	helperArgSep     = "\x1f"
)

// TestHelperMain is not a test in its own right. Run inside the normal suite,
// with the marker environment variable unset, it returns at once and passes
// trivially. TestMainSubprocess below re-executes this same test binary with
// the marker set and the intended arguments packed into helperArgsEnv, and it
// is that second process, not this one, whose exit code the assertions there
// check.
//
//nolint:paralleltest // this is a re-exec target, not a normal test case, and must run alone.
func TestHelperMain(_ *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}

	var args []string
	if raw := os.Getenv(helperArgsEnv); raw != "" {
		args = strings.Split(raw, helperArgSep)
	}

	os.Args = append([]string{os.Args[0]}, args...)

	main()
}

// TestMainSubprocess is the only way to reach main. It calls os.Exit, so
// calling it in process would end the real test binary instead of reporting
// a result; re-executing the compiled test binary as a child process is what
// lets both of main's endings be checked against a real exit code.
func TestMainSubprocess(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		args     func(t *testing.T) []string
		wantExit int
	}{
		{
			name: "a successful run exits zero",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{
					flagCoastline, coastlineFixture,
					flagLakes, lakesFixture,
					flagOut, filepath.Join(t.TempDir(), "shore.bin.gz"),
				}
			},
			wantExit: exitOK,
		},
		{
			name: "a failing run exits non-zero",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{
					flagCoastline, missingFixture,
					flagLakes, lakesFixture,
					flagOut, filepath.Join(t.TempDir(), "shore.bin.gz"),
				}
			},
			wantExit: exitFailure,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			args := testCase.args(t)

			//nolint:gosec // os.Args[0] is this test binary re-executing itself, not user input.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHelperMain$")

			cmd.Env = append(os.Environ(),
				helperProcessEnv+"=1",
				helperArgsEnv+"="+strings.Join(args, helperArgSep),
			)

			runErr := cmd.Run()

			exitCode := exitOK

			if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
				exitCode = exitErr.ExitCode()
			} else if runErr != nil {
				t.Fatalf("subprocess(%v) error = %v, want *exec.ExitError or nil", args, runErr)
			}

			if exitCode != testCase.wantExit {
				t.Errorf("subprocess(%v) exit = %d, want %d", args, exitCode, testCase.wantExit)
			}
		})
	}
}

// errSink is what a failing output looks like to encode. writeErr and
// closeErr are returned by the matching method, so one fake covers both of
// encode's failure branches.
type errSink struct {
	writeErr error
	closeErr error
}

func (s errSink) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}

	return len(p), nil
}

func (s errSink) Close() error { return s.closeErr }

// errSinkFailed is the cause an errSink reports. It carries no meaning beyond
// being a value the assertions can find again inside the wrapped error.
var errSinkFailed = errors.New("the sink refused it")

// TestEncodeReportsASinkThatFails covers the two failures write cannot reach
// on its own. gzip buffers, so a short body only reaches the writer when the
// stream is closed, which is why the Close case exercises the same fake with
// the error moved to the other method.
func TestEncodeReportsASinkThatFails(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		sink errSink
		want string
	}{
		{name: "the write fails", sink: errSink{writeErr: errSinkFailed}, want: "encoding"},
		{name: "the close fails", sink: errSink{closeErr: errSinkFailed}, want: "closing"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			lines := []shore.Polyline{{{Lat: 52, Lon: 4}, {Lat: 52.1, Lon: 4.1}}}

			err := encode(testCase.sink, placeholderOut, lines, &stdout)
			if !errors.Is(err, errSinkFailed) {
				t.Fatalf("encode(a sink that fails) error = %v, want errSinkFailed wrapped in it", err)
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("encode(a sink that fails) error = %q, want to contain %q", err.Error(), testCase.want)
			}

			if stdout.Len() != 0 {
				t.Errorf("encode(a sink that fails) wrote %q to stdout, want nothing", stdout.String())
			}
		})
	}
}
