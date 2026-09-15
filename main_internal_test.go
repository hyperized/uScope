package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/fbdev"
	"github.com/hyperized/uScope/pkg/rotate"
)

// Numbers built from flags.go's own constants, so the tests do not drift
// from the code they check.
const (
	widthLandscape  = 1280
	heightLandscape = 720

	midDimension      = (minDimension + maxDimension) / 2
	belowMinDimension = minDimension - 1
	aboveMaxDimension = maxDimension + 1

	fpsAboveRange = maxFPS + 1

	// Repeated literals, named once so goconst has nothing to complain
	// about and a typo in one table cannot silently diverge from another.
	altFB       = "/dev/fb1"
	outPNG      = "out.png"
	flagRotate  = "--rotate"
	flagSize    = "--size"
	flagFPS     = "--fps"
	caseDefault = "default"
	caseMaxEdge = "maximum edge"
)

// errUnrelated stands in for "some error that has nothing to do with the
// framebuffer", used to check that explain() leaves it untouched.
var errUnrelated = errors.New("test: unrelated error")

// checkConfig compares every field of a parsed config at once, so a wrong
// default on one field cannot hide behind an assertion on another.
func checkConfig(t *testing.T, got, want config) {
	t.Helper()

	if got != want {
		t.Errorf("config = %+v, want %+v", got, want)
	}
}

// defaultConfig is what parseFlags(nil) should produce. Individual tests
// copy it and override the one field they are exercising.
func defaultConfig() config {
	return config{
		fbPath:     defaultFB,
		rotation:   rotate.None,
		autoRotate: true,
		fps:        defaultFPS,
		size:       image.Pt(widthLandscape, heightLandscape),
	}
}

// runMainWith drives the real main() through the osExit seam so exercising
// it does not end the test binary. os.Args and osExit are process globals,
// so callers must not run this alongside anything else that touches them.
func runMainWith(t *testing.T, args []string) int {
	t.Helper()

	origArgs := os.Args
	origExit := osExit

	t.Cleanup(func() {
		os.Args = origArgs
		osExit = origExit
	})

	var exitCode int

	osExit = func(code int) { exitCode = code }

	os.Args = append([]string{"uScope"}, args...)

	main()

	return exitCode
}

// TestMainFunc exercises main() itself. Both cases mutate the process's
// os.Args and the osExit seam, so neither this test nor its subtests call
// t.Parallel() - a concurrent test could otherwise observe the swapped
// os.Exit or the rewritten argument list.
//
//nolint:paralleltest // deliberately serial: mutates os.Args and osExit.
func TestMainFunc(t *testing.T) {
	tests := []struct {
		name       string
		useTempPNG bool
		rawArgs    []string
		wantCode   int
	}{
		{name: "png into a temp dir exits clean", useTempPNG: true, wantCode: exitOK},
		{name: "unknown flag exits with failure", rawArgs: []string{"--nope"}, wantCode: exitFailure},
	}

	for _, testCase := range tests { //nolint:paralleltest // deliberately serial: mutates os.Args and osExit.
		t.Run(testCase.name, func(t *testing.T) {
			args := testCase.rawArgs
			if testCase.useTempPNG {
				args = []string{"--png", filepath.Join(t.TempDir(), outPNG)}
			}

			got := runMainWith(t, args)

			if got != testCase.wantCode {
				t.Errorf("main() exit code = %d, want %d", got, testCase.wantCode)
			}
		})
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	t.Parallel()

	got, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags(nil) unexpected error: %v", err)
	}

	checkConfig(t, got, defaultConfig())
}

func TestParseFlagsFB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: caseDefault, args: nil, want: defaultFB},
		{name: "dash form", args: []string{"-fb", altFB}, want: altFB},
		{name: "double dash equals form", args: []string{"--fb=/dev/fb1"}, want: altFB},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.fbPath = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsFPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: caseDefault, args: nil, want: defaultFPS},
		{name: "lower edge dash form", args: []string{"-fps", strconv.Itoa(minFPS)}, want: minFPS},
		{name: "upper edge equals form", args: []string{"--fps=" + strconv.Itoa(maxFPS)}, want: maxFPS},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.fps = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsRotate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantRot  rotate.Rotation
		wantAuto bool
	}{
		{name: "auto default", args: nil, wantRot: rotate.None, wantAuto: true},
		{name: "auto explicit", args: []string{flagRotate, defaultRotate}, wantRot: rotate.None, wantAuto: true},
		{name: "upright", args: []string{"--rotate=0"}, wantRot: rotate.None, wantAuto: false},
		{name: "clockwise", args: []string{"-rotate", "1"}, wantRot: rotate.Clockwise, wantAuto: false},
		{name: "upside down", args: []string{flagRotate, "2"}, wantRot: rotate.UpsideDown, wantAuto: false},
		{name: "counter clockwise", args: []string{"--rotate=3"}, wantRot: rotate.CounterClockwise, wantAuto: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.rotation = testCase.wantRot
			want.autoRotate = testCase.wantAuto

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want image.Point
	}{
		{name: caseDefault, args: nil, want: image.Pt(widthLandscape, heightLandscape)},
		{
			name: "portrait dash form",
			args: []string{"-size", "720x1280"},
			want: image.Pt(heightLandscape, widthLandscape),
		},
		{
			name: "minimum edge equals form",
			args: []string{"--size=" + strconv.Itoa(minDimension) + "x" + strconv.Itoa(minDimension)},
			want: image.Pt(minDimension, minDimension),
		},
		{
			name: caseMaxEdge,
			args: []string{flagSize, strconv.Itoa(maxDimension) + "x" + strconv.Itoa(maxDimension)},
			want: image.Pt(maxDimension, maxDimension),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.size = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsMisc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		testPattern bool
		pngPath     string
	}{
		{name: "test pattern", args: []string{"--test-pattern"}, testPattern: true},
		{name: "png dash form", args: []string{"-png", outPNG}, pngPath: outPNG},
		{name: "png equals form", args: []string{"--png=out.png"}, pngPath: outPNG},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.testPattern = testCase.testPattern
			want.pngPath = testCase.pngPath

			checkConfig(t, got, want)
		})
	}
}

// TestParseFlagsRejections covers every way validated() can reject the
// command line, asserted against the sentinel that should have fired.
func TestParseFlagsRejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		sentinel error
	}{
		{name: "empty fb", args: []string{"--fb", ""}, sentinel: errEmptyFB},
		{name: "fps zero", args: []string{flagFPS, "0"}, sentinel: errFPSRange},
		{name: "fps negative", args: []string{flagFPS, "-1"}, sentinel: errFPSRange},
		{name: "fps just above range", args: []string{flagFPS, "121"}, sentinel: errFPSRange},
		{name: "fps way above range", args: []string{flagFPS, "1000"}, sentinel: errFPSRange},
		{name: "rotate out of range high", args: []string{flagRotate, "4"}, sentinel: errRotate},
		{name: "rotate out of range low", args: []string{flagRotate, "-1"}, sentinel: errRotate},
		{name: "rotate empty", args: []string{flagRotate, ""}, sentinel: errRotate},
		{name: "rotate not a number", args: []string{flagRotate, "yes"}, sentinel: errRotate},
		{
			// "auto" is the one accepted word, and it is matched literally:
			// upper case is not the same word as far as parseRotate cares.
			name:     "rotate wrong case is rejected",
			args:     []string{flagRotate, "AUTO"},
			sentinel: errRotate,
		},
		{name: "size missing height", args: []string{flagSize, "1280"}, sentinel: errSize},
		{name: "size missing width", args: []string{flagSize, "x720"}, sentinel: errSize},
		{name: "size trailing x", args: []string{flagSize, "1280x"}, sentinel: errSize},
		{name: "size not numbers", args: []string{flagSize, "axb"}, sentinel: errSize},
		{name: "size zero width", args: []string{flagSize, "0x720"}, sentinel: errSize},
		{name: "size zero height", args: []string{flagSize, "1280x0"}, sentinel: errSize},
		{name: "size width over max", args: []string{flagSize, "8193x720"}, sentinel: errSize},
		{name: "size height over max", args: []string{flagSize, "1280x8193"}, sentinel: errSize},
		{name: "size negative width", args: []string{flagSize, "-5x720"}, sentinel: errSize},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, testCase.sentinel) {
				t.Errorf("parseFlags(%v) error = %v, want errors.Is(%v)", testCase.args, err, testCase.sentinel)
			}
		})
	}
}

func TestParseFlagsRejectUnknownFlag(t *testing.T) {
	t.Parallel()

	// The flag set's output was hardcoded to os.Stderr in bind(), not to
	// whatever writer a caller of run() passes in, so this also prints flag
	// usage to the real test process's stderr. That is expected noise.
	_, err := parseFlags([]string{"--nope"})
	if err == nil {
		t.Error("parseFlags(--nope) error = nil, want non-nil")
	}
}

func TestParseRotate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		wantRot  rotate.Rotation
		wantAuto bool
		wantErr  bool
	}{
		{name: "auto", text: defaultRotate, wantRot: rotate.None, wantAuto: true},
		{name: "upright", text: "0", wantRot: rotate.None},
		{name: "clockwise", text: "1", wantRot: rotate.Clockwise},
		{name: "upside down", text: "2", wantRot: rotate.UpsideDown},
		{name: "counter clockwise", text: "3", wantRot: rotate.CounterClockwise},
		{name: "junk wraps both sentinels", text: "junk", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotRot, gotAuto, err := parseRotate(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, errRotate) {
					t.Errorf("parseRotate(%q) error = %v, want errors.Is(errRotate)", testCase.text, err)
				}

				if !errors.Is(err, rotate.ErrInvalid) {
					t.Errorf("parseRotate(%q) error = %v, want errors.Is(rotate.ErrInvalid)", testCase.text, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseRotate(%q) unexpected error: %v", testCase.text, err)
			}

			if gotRot != testCase.wantRot || gotAuto != testCase.wantAuto {
				t.Errorf("parseRotate(%q) = (%v, %v), want (%v, %v)",
					testCase.text, gotRot, gotAuto, testCase.wantRot, testCase.wantAuto)
			}
		})
	}
}

func TestParseSizeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want image.Point
	}{
		{name: "landscape", text: defaultSize, want: image.Pt(widthLandscape, heightLandscape)},
		{name: "portrait", text: "720x1280", want: image.Pt(heightLandscape, widthLandscape)},
		{name: "minimum edge", text: "1x1", want: image.Pt(minDimension, minDimension)},
		{name: caseMaxEdge, text: "8192x8192", want: image.Pt(maxDimension, maxDimension)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSize(testCase.text)
			if err != nil {
				t.Fatalf("parseSize(%q) unexpected error: %v", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("parseSize(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

func TestParseSizeInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{name: "no x at all", text: "1280"},
		{name: "missing width", text: "x720"},
		{name: "missing height", text: "1280x"},
		{name: "not numbers", text: "axb"},
		{name: "zero width", text: "0x720"},
		{name: "zero height", text: "1280x0"},
		{name: "width over max", text: "8193x720"},
		{name: "height over max", text: "1280x8193"},
		{name: "negative width", text: "-5x720"},
		{
			// strings.Cut splits on the first "x" only, so "1x2x3" becomes
			// width "1" and height "2x3" - it fails as a bad height, not as
			// an ambiguous three-part size.
			name: "two x's cuts at the first one",
			text: "1x2x3",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseSize(testCase.text)
			if !errors.Is(err, errSize) {
				t.Errorf("parseSize(%q) error = %v, want errors.Is(errSize)", testCase.text, err)
			}
		})
	}
}

func TestDimension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		want    int
		wantErr bool
	}{
		{name: "valid mid-range", text: strconv.Itoa(midDimension), want: midDimension},
		{name: "not a number", text: "abc", wantErr: true},
		{name: "minimum edge", text: strconv.Itoa(minDimension), want: minDimension},
		{name: caseMaxEdge, text: strconv.Itoa(maxDimension), want: maxDimension},
		{name: "below minimum", text: strconv.Itoa(belowMinDimension), wantErr: true},
		{name: "above maximum", text: strconv.Itoa(aboveMaxDimension), wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := dimension(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, errSize) {
					t.Errorf("dimension(%q) error = %v, want errors.Is(errSize)", testCase.text, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("dimension(%q) unexpected error: %v", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("dimension(%q) = %d, want %d", testCase.text, got, testCase.want)
			}
		})
	}
}

// TestBind pins the documented defaults by reading them back off a fresh
// flag set, the same one an operator would see from --help.
func TestBind(t *testing.T) {
	t.Parallel()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	bind(set)

	tests := []struct {
		name     string
		flagName string
		wantDef  string
	}{
		{name: "fb", flagName: "fb", wantDef: defaultFB},
		{name: "rotate", flagName: "rotate", wantDef: defaultRotate},
		{name: "fps", flagName: "fps", wantDef: strconv.Itoa(defaultFPS)},
		{name: "test-pattern", flagName: "test-pattern", wantDef: "false"},
		{name: "png", flagName: "png", wantDef: ""},
		{name: "size", flagName: "size", wantDef: defaultSize},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := set.Lookup(testCase.flagName)
			if got == nil {
				t.Fatalf("flag %q not registered", testCase.flagName)
			}

			if got.DefValue != testCase.wantDef {
				t.Errorf("flag %q DefValue = %q, want %q", testCase.flagName, got.DefValue, testCase.wantDef)
			}
		})
	}
}

// TestRawFlagsValidatedOrder pins the order validated() checks fields in,
// which is awkward to observe through the command line since a single bad
// flag there only ever shows one failure at a time.
func TestRawFlagsValidatedOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      rawFlags
		sentinel error
	}{
		{
			name: "empty fb wins over an also-invalid fps",
			raw: rawFlags{
				rotate: defaultRotate,
				fps:    fpsAboveRange,
				size:   defaultSize,
			},
			sentinel: errEmptyFB,
		},
		{
			name: "rotate is checked before size",
			raw: rawFlags{
				fb:   defaultFB,
				fps:  defaultFPS,
				size: "bad",
			},
			sentinel: errRotate,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.raw.validated()
			if !errors.Is(err, testCase.sentinel) {
				t.Errorf("validated() error = %v, want errors.Is(%v)", err, testCase.sentinel)
			}
		})
	}
}

func TestRunSuccess(t *testing.T) {
	t.Parallel()

	pngPath := filepath.Join(t.TempDir(), outPNG)

	var stdout, stderr bytes.Buffer

	got := run([]string{"--png", pngPath}, &stdout, &stderr)

	if got != exitOK {
		t.Fatalf("run() exit code = %d, want %d", got, exitOK)
	}

	if stderr.Len() != 0 {
		t.Errorf("run() stderr = %q, want empty", stderr.String())
	}

	if !strings.Contains(stdout.String(), "wrote ") {
		t.Errorf("run() stdout = %q, want it to contain %q", stdout.String(), "wrote ")
	}

	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("expected %s to exist: %v", pngPath, err)
	}
}

func TestRunFlagError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	got := run([]string{flagFPS, "0"}, &stdout, &stderr)

	if got != exitFailure {
		t.Fatalf("run() exit code = %d, want %d", got, exitFailure)
	}

	if stdout.Len() != 0 {
		t.Errorf("run() stdout = %q, want empty", stdout.String())
	}

	if !strings.Contains(stderr.String(), errFPSRange.Error()) {
		t.Errorf("run() stderr = %q, want it to contain %q", stderr.String(), errFPSRange.Error())
	}
}

func TestRunAppFailure(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	// With no --png this opens a framebuffer, and no machine running tests
	// has one: off Linux fbdev.Open is a stub that always fails, and on a
	// Linux builder or container /dev/fb0 is absent. The two produce
	// different messages, so this only pins the outcome. TestExplain pins
	// the wording of the no-backend advice, and does it on any platform by
	// building the error itself.
	got := run(nil, &stdout, &stderr)

	if got != exitFailure {
		t.Fatalf("run() exit code = %d, want %d", got, exitFailure)
	}

	if stderr.Len() == 0 {
		t.Error("run() wrote nothing to stderr, want a reason for the failure")
	}

	if stdout.Len() != 0 {
		t.Errorf("run() stdout = %q, want nothing", stdout.String())
	}
}

func TestExplain(t *testing.T) {
	t.Parallel()

	wrappedUnsupported := fmt.Errorf("opening /dev/fb0: %w", fbdev.ErrUnsupported)

	tests := []struct {
		name string
		err  error
		cfg  config
		want string
	}{
		{
			name: "unsupported without png gets advice",
			err:  wrappedUnsupported,
			want: "no framebuffer backend on " + runtime.GOOS + "; use --png to render the pattern to a file",
		},
		{
			// This is the branch that must NOT show the --png advice: the
			// operator already passed --png, so the advice would be wrong.
			name: "unsupported with png set falls through to the raw error",
			err:  wrappedUnsupported,
			cfg:  config{pngPath: outPNG},
			want: wrappedUnsupported.Error(),
		},
		{
			name: "unrelated error is unchanged",
			err:  errUnrelated,
			want: errUnrelated.Error(),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := explain(testCase.err, testCase.cfg)

			if got != testCase.want {
				t.Errorf("explain() = %q, want %q", got, testCase.want)
			}
		})
	}
}
