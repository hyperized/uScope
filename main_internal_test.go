package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/battery"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/fbdev"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/shore"
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

	// midFrames sits halfway through the accepted --frames range, the same
	// way midDimension does for --size.
	midFrames = (minFrames + maxFrames) / 2

	// midExaggerate sits inside --exaggerate's accepted range, the same way
	// midFrames does for --frames.
	midExaggerate = (radar.MinExaggerate + radar.MaxExaggerate) / 2

	// aboveMaxExaggerate is one step past what --exaggerate accepts, the same
	// way aboveMaxDimension is for --size.
	aboveMaxExaggerate = radar.MaxExaggerate + 1

	// Repeated literals, named once so goconst has nothing to complain
	// about and a typo in one table cannot silently diverge from another.
	altFB          = "/dev/fb1"
	outPNG         = "out.png"
	flagRotate     = "--rotate"
	flagSize       = "--size"
	flagFPS        = "--fps"
	flagBackend    = "--backend"
	flagFrames     = "--frames"
	flagPNG        = "--png"
	flagScene      = "--scene"
	flagTheme      = "--theme"
	flagDemo       = "--demo"
	flagBeast      = "--beast"
	flagReplay     = "--replay-iq"
	flagLat        = "--lat"
	flagLon        = "--lon"
	flagColour     = "--colour"
	flagBattery    = "--battery"
	flagAirports   = "--airports"
	flagShore      = "--shore"
	flagRange      = "--range"
	flagMinimal    = "--minimal"
	flagNoDecay    = "--no-decay"
	flagRecenter   = "--recenter"
	flagDemoSector = "--demo-sector"
	flagView       = "--view"
	flagExaggerate = "--exaggerate"

	// patternValue is the one non-default --scene spelling, named because it
	// turns up in several tables. specimenValue is the spelling --scene no
	// longer accepts, named for the rejection tests that check it stays that
	// way.
	patternValue  = "pattern"
	specimenValue = "specimen"

	// paperValue is the one non-default --theme spelling, airlineValue the
	// one non-default --colour spelling, offValue the one non-default
	// --airports spelling.
	paperValue    = "paper"
	airlineValue  = "airline"
	offValue      = "off"
	demoValue     = "demo"
	demoLabel     = "DEMO"
	captureFile   = "capture.iq"
	kittyValue    = "kitty"
	blocksValue   = "blocks"
	pngValue      = "png"
	caseDefault   = "default"
	caseMaxEdge   = "maximum edge"
	caseWrongCase = "wrong case"
	caseEmpty     = "empty"
	caseAutoGiven = "auto explicit"

	// viewMinimalValue and view3DValue are --view's other two spellings; its
	// default, "scope", is already named as defaultView.
	viewMinimalValue = "minimal"
	view3DValue      = "3d"

	// nanText is the flag spelling of not-a-number. strconv.ParseFloat
	// accepts it, so every float64 flag parses it fine; each one's own range
	// check is what turns it away.
	nanText = "NaN"

	// batteryPathWithSpace is a --battery value that is not whitespace-only
	// despite containing some: only an all-whitespace value is refused, so a
	// real-looking path with a space inside it has to be accepted unchanged.
	batteryPathWithSpace = "/sys/class/power supply/BAT0/uevent"
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
		theme:      theme.KindNight,
		colour:     radar.ColourAltitude,
		airports:   radar.ToggleOn,
		shore:      radar.ToggleOn,
		recentre:   radar.DefaultRecentre,
		view:       radar.ViewScope,
		exaggerate: radar.DefaultExaggerate,
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
				args = []string{flagPNG, filepath.Join(t.TempDir(), outPNG)}
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
		{name: caseAutoGiven, args: []string{flagRotate, defaultRotate}, wantRot: rotate.None, wantAuto: true},
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
		// wantBackend is backend.Auto (the zero value) except when --png is
		// set: parseBackend resolves an unspecified --backend to PNG once a
		// path is given, so these rows have to expect that too.
		wantBackend backend.Kind
	}{
		{name: "test pattern", args: []string{"--test-pattern"}, testPattern: true},
		{name: "png dash form", args: []string{"-png", outPNG}, pngPath: outPNG, wantBackend: backend.PNG},
		{name: "png equals form", args: []string{"--png=out.png"}, pngPath: outPNG, wantBackend: backend.PNG},
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
			want.backend = testCase.wantBackend

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want backend.Kind
	}{
		{name: caseDefault, args: nil, want: backend.Auto},
		{name: caseAutoGiven, args: []string{flagBackend, defaultBackend}, want: backend.Auto},
		{name: "framebuffer", args: []string{flagBackend, "fb"}, want: backend.Framebuffer},
		{name: "kitty", args: []string{flagBackend, kittyValue}, want: backend.Kitty},
		{name: "blocks", args: []string{flagBackend, blocksValue}, want: backend.Blocks},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.backend = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsFrames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: caseDefault, args: nil, want: minFrames},
		{name: "one dash form", args: []string{"-frames", "1"}, want: 1},
		{name: caseMaxEdge, args: []string{"--frames=" + strconv.Itoa(maxFrames)}, want: maxFrames},
		{name: "mid-range equals form", args: []string{"--frames=" + strconv.Itoa(midFrames)}, want: midFrames},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.frames = testCase.want

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
		{name: "backend invalid", args: []string{flagBackend, "nope"}, sentinel: errBackend},
		{name: "frames below range", args: []string{flagFrames, "-1"}, sentinel: errFrames},
		{name: "frames above range", args: []string{flagFrames, "1001"}, sentinel: errFrames},
		{name: "backend png without a png path", args: []string{flagBackend, pngValue}, sentinel: errPNGPath},
		{
			// fb, kitty and blocks all disagree with --png the same way; fb
			// stands in for the group.
			name:     "png with an incompatible backend",
			args:     []string{flagPNG, outPNG, flagBackend, "fb"},
			sentinel: errPNGBoth,
		},
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

// TestParseBackend covers every branch of parseBackend directly, including
// the --png/--backend combinations that a plain --backend value never
// reaches on its own.
func TestParseBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		pngPath  string
		want     backend.Kind
		sentinel error
	}{
		{name: "bad backend name", text: "nope", sentinel: errBackend},
		{name: "png backend without a path", text: pngValue, sentinel: errPNGPath},
		{
			// fb, kitty and blocks all take this branch; fb stands in for
			// the group since the check does not depend on which one it is.
			name:     "png path with an incompatible backend",
			text:     "fb",
			pngPath:  outPNG,
			sentinel: errPNGBoth,
		},
		{name: "png path with auto becomes png", text: defaultBackend, pngPath: outPNG, want: backend.PNG},
		{name: "png path with explicit png stays png", text: pngValue, pngPath: outPNG, want: backend.PNG},
		{name: "auto with no png path", text: defaultBackend, want: backend.Auto},
		{name: "fb with no png path", text: "fb", want: backend.Framebuffer},
		{name: "kitty with no png path", text: kittyValue, want: backend.Kitty},
		{name: "blocks with no png path", text: blocksValue, want: backend.Blocks},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseBackend(testCase.text, testCase.pngPath)

			if testCase.sentinel != nil {
				if !errors.Is(err, testCase.sentinel) {
					t.Errorf("parseBackend(%q, %q) error = %v, want errors.Is(%v)",
						testCase.text, testCase.pngPath, err, testCase.sentinel)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseBackend(%q, %q) unexpected error: %v", testCase.text, testCase.pngPath, err)
			}

			if got != testCase.want {
				t.Errorf("parseBackend(%q, %q) = %v, want %v", testCase.text, testCase.pngPath, got, testCase.want)
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
		{name: "png", flagName: pngValue, wantDef: ""},
		{name: "size", flagName: "size", wantDef: defaultSize},
		{name: "backend", flagName: "backend", wantDef: defaultBackend},
		{name: "frames", flagName: "frames", wantDef: strconv.Itoa(minFrames)},
		{name: "theme", flagName: "theme", wantDef: defaultTheme},
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

	// --demo so the run does not fall back to the demo fleet with a warning,
	// which is what a machine with no receiver would otherwise print.
	got := run([]string{flagDemo, flagPNG, pngPath}, &stdout, &stderr)

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

	// --backend fb is what forces the failure reliably: auto no longer
	// does, since it now falls back to a terminal backend and would run the
	// live loop forever instead of failing. Off Linux fbdev.Open is a stub
	// that always fails, and on a Linux builder or container /dev/fb0 is
	// absent, so the two produce different messages and this only pins the
	// outcome. TestExplain pins the wording of the no-backend advice, and
	// does it on any platform by building the error itself.
	got := run([]string{flagBackend, "fb"}, &stdout, &stderr)

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
			want: "no framebuffer on " + runtime.GOOS +
				"; use --backend blocks or --backend kitty to draw in the terminal, or --png for a file",
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

func TestParseFlagsScene(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want app.SceneKind
	}{
		{name: caseDefault, args: nil, want: app.Radar},
		{name: "radar explicit", args: []string{flagScene, defaultScene}, want: app.Radar},
		{name: patternValue, args: []string{flagScene, patternValue}, want: app.Pattern},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.scene = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsSceneRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "unknown scene", args: []string{flagScene, "waterfall"}},
		{name: "empty scene", args: []string{flagScene, ""}},
		{name: "specimen no longer accepted", args: []string{flagScene, specimenValue}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errScene) {
				t.Fatalf("parseFlags(%v) error = %v, want errScene", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, app.ErrScene) {
				t.Errorf("parseFlags(%v) error = %v, want app.ErrScene wrapped in it", testCase.args, err)
			}
		})
	}
}

// TestSceneReachesConfig pins that --scene actually arrives in the app.Config
// main hands to app.Run, which is the one line of wiring no other test in
// this file covers.
func TestSceneReachesConfig(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags([]string{flagScene, patternValue})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if cfg.scene != app.Pattern {
		t.Errorf("config.scene = %v, want %v", cfg.scene, app.Pattern)
	}
}

func TestParseFlagsTheme(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want theme.Kind
	}{
		{name: caseDefault, args: nil, want: theme.KindNight},
		{name: "night explicit", args: []string{flagTheme, defaultTheme}, want: theme.KindNight},
		{name: paperValue, args: []string{flagTheme, paperValue}, want: theme.KindPaper},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.theme = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsThemeRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "unknown theme", args: []string{flagTheme, "sepia"}},
		{name: "empty theme", args: []string{flagTheme, ""}},
		{
			// --theme is an allow list, not free text: the exact spelling is
			// what is accepted, not a case-insensitive match of it.
			name: caseWrongCase, args: []string{flagTheme, "Night"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errTheme) {
				t.Fatalf("parseFlags(%v) error = %v, want errTheme", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, theme.ErrUnknown) {
				t.Errorf("parseFlags(%v) error = %v, want theme.ErrUnknown wrapped in it", testCase.args, err)
			}
		})
	}
}

// TestThemeReachesConfig pins that --theme actually arrives in the
// app.Config main hands to app.Run, which is the one line of wiring no other
// test in this file covers.
func TestThemeReachesConfig(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags([]string{flagTheme, paperValue})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if cfg.theme != theme.KindPaper {
		t.Errorf("config.theme = %v, want %v", cfg.theme, theme.KindPaper)
	}
}

func TestParseFlagsColour(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want radar.ColourMode
	}{
		{name: caseDefault, args: nil, want: radar.ColourAltitude},
		{name: "altitude explicit", args: []string{flagColour, defaultColour}, want: radar.ColourAltitude},
		{name: airlineValue, args: []string{flagColour, airlineValue}, want: radar.ColourAirline},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.colour = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsColourRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "empty colour", args: []string{flagColour, ""}},
		{
			// --colour is an allow list, not free text: the exact spelling is
			// what is accepted, not a case-insensitive match of it.
			name: caseWrongCase, args: []string{flagColour, "Airline"},
		},
		{name: "unknown colour", args: []string{flagColour, "rainbow"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errColour) {
				t.Fatalf("parseFlags(%v) error = %v, want errColour", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, radar.ErrColour) {
				t.Errorf("parseFlags(%v) error = %v, want radar.ErrColour wrapped in it", testCase.args, err)
			}
		})
	}
}

func TestParseFlagsBattery(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want string
	}{
		{name: caseDefault, args: nil, want: ""},
		{
			// Only an all-whitespace value is refused; a real-looking path
			// with a space inside it lands in config.battery unchanged.
			name: "a path with spaces is accepted",
			args: []string{flagBattery, batteryPathWithSpace},
			want: batteryPathWithSpace,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.battery = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsBatteryRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "spaces only", args: []string{flagBattery, "   "}},
		{name: "a single tab", args: []string{flagBattery, "\t"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errBattery) {
				t.Errorf("parseFlags(%v) error = %v, want errBattery", testCase.args, err)
			}
		})
	}
}

func TestParseFlagsAirports(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want radar.Toggle
	}{
		{name: caseDefault, args: nil, want: radar.ToggleOn},
		{name: "on explicit", args: []string{flagAirports, defaultOn}, want: radar.ToggleOn},
		{name: offValue, args: []string{flagAirports, offValue}, want: radar.ToggleOff},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.airports = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsAirportsRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: caseEmpty, args: []string{flagAirports, ""}},
		{
			// --airports is an allow list, not free text, and not
			// strconv.ParseBool: only the two exact spellings are accepted.
			name: caseWrongCase, args: []string{flagAirports, "On"},
		},
		{name: "a bool spelling is not one of the two words", args: []string{flagAirports, "true"}},
		{name: "another bool spelling", args: []string{flagAirports, "1"}},
		{name: "yes is not one of the two words either", args: []string{flagAirports, "yes"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errAirports) {
				t.Fatalf("parseFlags(%v) error = %v, want errAirports", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, radar.ErrToggle) {
				t.Errorf("parseFlags(%v) error = %v, want radar.ErrToggle wrapped in it", testCase.args, err)
			}
		})
	}
}

// --- sources --------------------------------------------------------------

// beastAddr is a syntactically valid BEAST address. Nothing dials it: building
// a source opens no socket, so the host never has to exist.
const beastAddr = "127.0.0.1:30005"

// fakeIngest satisfies the unexported ingest interface internal/source's
// WithIngest takes, so a test can build a Live that never goes near a radio.
type fakeIngest struct {
	started chan struct{}
	once    sync.Once
}

func (f *fakeIngest) Stream(ctx context.Context, _ *airplanes.Airplanes) error {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()

	return fmt.Errorf("fake ingest: %w", ctx.Err())
}

func (*fakeIngest) Source() adsb.SourceInfo { return adsb.SourceInfo{Label: "FAKE"} }

func (*fakeIngest) Stats() adsb.Stats { return adsb.Stats{} }

func TestParseFlagsSource(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		args   []string
		want   sourceKind
		beast  string
		replay string
	}{
		{name: caseDefault, args: nil, want: sourceAuto},
		{name: demoValue, args: []string{flagDemo}, want: sourceDemo},
		{name: "beast", args: []string{flagBeast, beastAddr}, want: sourceBeast, beast: beastAddr},
		{name: "replay", args: []string{flagReplay, captureFile}, want: sourceReplay, replay: captureFile},
		{
			// Replay wins so a capture can always be played back on a host
			// that also has a feed configured, which is what uAirwaves does.
			name:   "replay beats beast and demo",
			args:   []string{flagReplay, captureFile, flagBeast, beastAddr, flagDemo},
			want:   sourceReplay,
			beast:  beastAddr,
			replay: captureFile,
		},
		{
			name:  "beast beats demo",
			args:  []string{flagBeast, beastAddr, flagDemo},
			want:  sourceBeast,
			beast: beastAddr,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.source = testCase.want
			want.beast = testCase.beast
			want.replay = testCase.replay

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsBeastRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		addr string
	}{
		{name: "no port", addr: "receiver.local"},
		{name: "nothing at all", addr: ":"},
		{name: "no host", addr: ":30005"},
		{name: "no port after the colon", addr: "receiver.local:"},
		{name: "too many colons", addr: "a:b:c"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags([]string{flagBeast, testCase.addr})
			if !errors.Is(err, errBeastAddr) {
				t.Errorf("parseFlags(--beast %q) error = %v, want errBeastAddr", testCase.addr, err)
			}
		})
	}
}

func TestParseFlagsLocation(t *testing.T) {
	t.Parallel()

	t.Run("both given", func(t *testing.T) {
		t.Parallel()

		got, err := parseFlags([]string{flagLat, "52.3105", flagLon, "4.7683"})
		if err != nil {
			t.Fatalf("parseFlags: %v", err)
		}

		want := defaultConfig()
		want.latitude, want.longitude, want.hasLocation = 52.3105, 4.7683, true

		checkConfig(t, got, want)
	})

	for _, testCase := range []struct {
		name     string
		args     []string
		sentinel error
	}{
		{name: "latitude alone", args: []string{flagLat, "52"}, sentinel: errLatLonPair},
		{name: "longitude alone", args: []string{flagLon, "4"}, sentinel: errLatLonPair},
		{name: "latitude too high", args: []string{flagLat, "91", flagLon, "0"}, sentinel: errLatitude},
		{name: "latitude too low", args: []string{flagLat, "-91", flagLon, "0"}, sentinel: errLatitude},
		{name: "longitude too high", args: []string{flagLat, "0", flagLon, "181"}, sentinel: errLongitude},
		{name: "longitude too low", args: []string{flagLat, "0", flagLon, "-181"}, sentinel: errLongitude},
		{name: "latitude is not a number", args: []string{flagLat, "north", flagLon, "0"}, sentinel: errLatitude},
		{name: "longitude is not a number", args: []string{flagLat, "0", flagLon, "east"}, sentinel: errLongitude},
		{name: "latitude is NaN", args: []string{flagLat, nanText, flagLon, "0"}, sentinel: errLatitude},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, testCase.sentinel) {
				t.Errorf("parseFlags(%v) error = %v, want %v", testCase.args, err, testCase.sentinel)
			}
		})
	}
}

func TestSourceFor(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		cfg        config
		goos       string
		wantLabel  string
		wantWarn   bool
		wantSector bool
	}{
		{
			name:      "replay",
			cfg:       config{source: sourceReplay, replay: "/tmp/capture.iq"},
			goos:      linuxGOOS,
			wantLabel: "REPLAY capture.iq",
		},
		{
			name:      "beast",
			cfg:       config{source: sourceBeast, beast: beastAddr},
			goos:      linuxGOOS,
			wantLabel: "BEAST " + beastAddr,
		},
		{
			name:      demoValue,
			cfg:       config{source: sourceDemo},
			goos:      linuxGOOS,
			wantLabel: demoLabel,
		},
		{
			name:      "nothing asked for on linux opens the radio",
			cfg:       config{source: sourceAuto},
			goos:      linuxGOOS,
			wantLabel: "SDR",
		},
		{
			name:      "nothing asked for elsewhere flies the demo fleet",
			cfg:       config{source: sourceAuto},
			goos:      "darwin",
			wantLabel: demoLabel,
			wantWarn:  true,
		},
		{
			name:      "a position is passed through to the demo fleet",
			cfg:       config{source: sourceDemo, hasLocation: true, latitude: 51.5, longitude: -0.45},
			goos:      linuxGOOS,
			wantLabel: demoLabel,
		},
		{
			name: "a position is passed through to the live source",
			cfg: config{
				source: sourceBeast, beast: beastAddr,
				hasLocation: true, latitude: 51.5, longitude: -0.45,
			},
			goos:      linuxGOOS,
			wantLabel: "BEAST " + beastAddr,
		},
		{
			name:       "demo sector crowds the fleet into the north-west quadrant",
			cfg:        config{source: sourceDemo, demoSector: true},
			goos:       linuxGOOS,
			wantLabel:  demoLabel,
			wantSector: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			src, err := sourceFor(testCase.cfg, testCase.goos, &stderr)
			if err != nil {
				t.Fatalf("sourceFor: %v", err)
			}

			defer func() { _ = src.Close() }()

			frame := src.Frame()

			if got := frame.Source.Label; got != testCase.wantLabel {
				t.Errorf("source label = %q, want %q", got, testCase.wantLabel)
			}

			if warned := stderr.Len() > 0; warned != testCase.wantWarn {
				t.Errorf("warned = %v (%q), want %v", warned, stderr.String(), testCase.wantWarn)
			}

			if testCase.wantSector {
				checkSector(t, frame)
			}
		})
	}
}

// checkSector asserts that --demo-sector actually reached the fleet: every
// aircraft sits north and west of the receiver, which is what the north-west
// quadrant means.
func checkSector(t *testing.T, frame source.Frame) {
	t.Helper()

	if len(frame.Planes) == 0 {
		t.Fatal("frame has no aircraft to check")
	}

	for _, plane := range frame.Planes {
		if plane.Latitude <= frame.Receiver.Latitude {
			t.Errorf("aircraft %s latitude %g, want it north of the receiver (%g)",
				plane.ICAO, plane.Latitude, frame.Receiver.Latitude)
		}

		if plane.Longitude >= frame.Receiver.Longitude {
			t.Errorf("aircraft %s longitude %g, want it west of the receiver (%g)",
				plane.ICAO, plane.Longitude, frame.Receiver.Longitude)
		}
	}
}

func TestSourceForRejectsABadPosition(t *testing.T) {
	t.Parallel()

	// The flag layer rejects these before sourceFor ever sees them. The
	// branch is here because internal/source validates its own input, and
	// this is the only way to reach it.
	for _, testCase := range []struct {
		name string
		cfg  config
	}{
		{name: "live", cfg: config{source: sourceBeast, beast: beastAddr, hasLocation: true, latitude: 91}},
		{name: "demo", cfg: config{source: sourceDemo, hasLocation: true, latitude: 91}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			src, err := sourceFor(testCase.cfg, linuxGOOS, io.Discard)
			if !errors.Is(err, source.ErrCoordinate) {
				t.Fatalf("sourceFor error = %v, want source.ErrCoordinate", err)
			}

			if src != nil {
				t.Error("sourceFor returned a source alongside an error")
			}
		})
	}
}

func TestStart(t *testing.T) {
	t.Parallel()

	t.Run("a live source is started", func(t *testing.T) {
		t.Parallel()

		ingest := &fakeIngest{started: make(chan struct{})}

		src, err := source.NewLive(source.WithIngest(ingest), source.WithStderr(io.Discard))
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		start(ctx, src)

		select {
		case <-ingest.started:
		case <-time.After(time.Second):
			t.Fatal("start did not run the ingest")
		}

		cancel()

		if err := src.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	t.Run("a demo source has nothing to start", func(t *testing.T) {
		t.Parallel()

		src, err := source.NewDemo()
		if err != nil {
			t.Fatalf("NewDemo: %v", err)
		}

		// The point is that this does not panic on a source with no Start.
		start(t.Context(), src)

		if err := src.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

// Not parallel on purpose: it swaps the package-level newSource seam, and
// parallel tests only resume once every sequential test has finished, so
// this is the one window in which rewriting it races with nobody.
func TestRunReportsASourceItCannotBuild(t *testing.T) { //nolint:paralleltest // rewrites the newSource seam
	original := newSource
	newSource = func(config, string, io.Writer) (source.Source, error) { return nil, errUnrelated }

	t.Cleanup(func() { newSource = original })

	var stdout, stderr bytes.Buffer

	if got := run([]string{flagDemo}, &stdout, &stderr); got != exitFailure {
		t.Errorf("run() exit code = %d, want %d", got, exitFailure)
	}

	if !strings.Contains(stderr.String(), errUnrelated.Error()) {
		t.Errorf("run() stderr = %q, want it to name the failure", stderr.String())
	}
}

// --- battery --------------------------------------------------------------

// testPercentage stands in for a battery reading a fake watcher hands back.
// The exact figure carries no meaning beyond being a value awaitCharge and
// startBattery both treat as "arrived", so it is reused across every test
// that needs one.
const testPercentage = 84

// TestAwaitChargeReturnsAsSoonAsAReadingArrives holds awaitCharge to its
// fastest promise: a status that already carries a reading needs no waiting
// at all, not even a closed done channel to fall back on.
func TestAwaitChargeReturnsAsSoonAsAReadingArrives(t *testing.T) {
	t.Parallel()

	status := battery.NewStatus(battery.WithPercentage(testPercentage))
	done := make(chan struct{}) // deliberately never closed

	awaitCharge(status, done)
}

// TestAwaitChargeReturnsWhenTheDoneChannelCloses covers the watcher-gave-up
// exit on its own: with no reading ever arriving, a closed done channel has
// to end the wait well before the settle deadline would.
func TestAwaitChargeReturnsWhenTheDoneChannelCloses(t *testing.T) {
	t.Parallel()

	status := battery.NewStatus(battery.WithPercentage(unknownCharge))
	done := make(chan struct{})
	close(done)

	start := time.Now()

	awaitCharge(status, done)

	if elapsed := time.Since(start); elapsed >= batterySettle {
		t.Errorf("awaitCharge took %v, want it to return on the closed done channel, well under batterySettle (%v)",
			elapsed, batterySettle)
	}
}

// TestAwaitChargeKeepsPollingUntilAReadingArrives holds the loop-and-retry
// branch to its promise: with neither the done channel nor the settle
// deadline ready, a poll tick must not end the wait, only bring awaitCharge
// back around to check the percentage again.
func TestAwaitChargeKeepsPollingUntilAReadingArrives(t *testing.T) {
	t.Parallel()

	status := battery.NewStatus(battery.WithPercentage(unknownCharge))
	done := make(chan struct{}) // never closed: only the delayed reading may end this

	go func() {
		time.Sleep(3 * batteryPoll)
		status.Update(battery.WithPercentage(testPercentage))
	}()

	awaitCharge(status, done)

	if got := status.GetPercentage(); got < 0 {
		t.Errorf("status percentage = %d, want a reading to have arrived", got)
	}
}

// TestAwaitChargeReturnsWhenTheSettleWindowRunsOut is not parallel: it
// rewrites the package-level batterySettle seam, and parallel tests only
// resume once every sequential test has finished, so this is the one window
// in which rewriting it races with nobody.
func TestAwaitChargeReturnsWhenTheSettleWindowRunsOut(t *testing.T) { //nolint:paralleltest // rewrites batterySettle
	original := batterySettle
	batterySettle = time.Millisecond

	t.Cleanup(func() { batterySettle = original })

	status := battery.NewStatus(battery.WithPercentage(unknownCharge))
	done := make(chan struct{}) // left open: only the deadline may end this call

	start := time.Now()

	awaitCharge(status, done)

	if elapsed := time.Since(start); elapsed >= original {
		t.Errorf("awaitCharge took %v, want it to return once the shortened settle window ran out, well under "+
			"the unmodified batterySettle of %v", elapsed, original)
	}
}

// TestStartBatteryReturnsAsSoonAsAReadingArrives drives startBattery through
// the watchBattery seam with a fake that reports a reading immediately and
// then blocks, the way the real watcher keeps polling after its first read.
// startBattery must not wait for it to finish.
//
// Not parallel: it rewrites the package-level watchBattery seam.
func TestStartBatteryReturnsAsSoonAsAReadingArrives(t *testing.T) { //nolint:paralleltest // rewrites watchBattery
	original := watchBattery

	t.Cleanup(func() { watchBattery = original })

	watchBattery = func(ctx context.Context, status *battery.Status, _ string) error {
		status.Update(battery.WithPercentage(testPercentage))
		<-ctx.Done()

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	var stderr bytes.Buffer

	start := time.Now()
	status, wait := startBattery(ctx, config{}, &stderr)
	elapsed := time.Since(start)

	cancel()
	wait()

	if elapsed >= batterySettle {
		t.Errorf("startBattery took %v, want it to return as soon as the reading arrived, well under "+
			"batterySettle (%v)", elapsed, batterySettle)
	}

	if got := status.GetPercentage(); got != testPercentage {
		t.Errorf("status percentage = %d, want %d", got, testPercentage)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing: the watcher never failed", stderr.String())
	}
}

// TestStartBatteryReportsAFailingWatcher covers both ways a watcher can end
// without ever producing a reading: giving up because the platform has none
// to read, and failing for some other reason. Both reach stderr through the
// same line, so both are checked the same way.
//
// Not parallel: it rewrites the package-level watchBattery seam.
func TestStartBatteryReportsAFailingWatcher(t *testing.T) { //nolint:paralleltest // rewrites watchBattery
	original := watchBattery

	t.Cleanup(func() { watchBattery = original })

	for _, testCase := range []struct { //nolint:paralleltest // deliberately serial: rewrites watchBattery
		name string
		err  error
	}{
		{name: "the platform has no battery to watch", err: battery.ErrUnsupported},
		{name: "some other failure", err: errUnrelated},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			watchBattery = func(context.Context, *battery.Status, string) error { return testCase.err }

			ctx, cancel := context.WithCancel(context.Background())

			var stderr bytes.Buffer

			start := time.Now()
			status, wait := startBattery(ctx, config{}, &stderr)
			elapsed := time.Since(start)

			cancel()
			wait()

			if elapsed >= batterySettle {
				t.Errorf("startBattery took %v, want it to return once the watcher gave up, well under "+
					"batterySettle (%v)", elapsed, batterySettle)
			}

			if got := status.GetPercentage(); got != unknownCharge {
				t.Errorf("status percentage = %d, want %d (the watcher never reported one)", got, unknownCharge)
			}

			if !strings.Contains(stderr.String(), testCase.err.Error()) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), testCase.err.Error())
			}
		})
	}
}

// shortWait bounds the "wait must not already be done" check in
// TestStartBatteryWaitsForTheContextWhenNothingArrives. It only has to be
// short next to a human, not next to the shortened batterySettle the test
// sets, so a plain time.Millisecond multiple is as good as any other choice.
const shortWaitMillis = 50

// TestStartBatteryWaitsForTheContextWhenNothingArrives covers the deadline
// path end to end: with the settle window shortened and a watcher that never
// produces a reading, startBattery still returns quickly, but the wait
// function it hands back only completes once the caller's own context is
// cancelled, because the watcher goroutine is still out there polling.
//
// Not parallel: it rewrites the package-level watchBattery and batterySettle
// seams.
func TestStartBatteryWaitsForTheContextWhenNothingArrives(t *testing.T) { //nolint:paralleltest // rewrites seams
	originalWatch := watchBattery
	originalSettle := batterySettle
	batterySettle = time.Millisecond

	t.Cleanup(func() {
		watchBattery = originalWatch
		batterySettle = originalSettle
	})

	watchBattery = func(ctx context.Context, _ *battery.Status, _ string) error {
		<-ctx.Done()

		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer

	start := time.Now()
	status, wait := startBattery(ctx, config{}, &stderr)
	elapsed := time.Since(start)

	if elapsed >= originalSettle {
		t.Errorf("startBattery took %v, want it to return once the shortened settle window ran out", elapsed)
	}

	if got := status.GetPercentage(); got != unknownCharge {
		t.Errorf("status percentage = %d, want %d (nothing ever arrived)", got, unknownCharge)
	}

	waitDone := make(chan struct{})

	go func() {
		wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("wait() returned before the context was cancelled")
	case <-time.After(shortWaitMillis * time.Millisecond):
	}

	cancel()

	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("wait() did not return after the context was cancelled")
	}
}

// TestPollBattery drives the production watcher directly against a context
// that is already cancelled, so it returns immediately rather than shelling
// out to pmset or waiting on a real interval, empty override path and an
// explicit one alike.
//
// On this platform an already-cancelled context makes the first pmset read
// fail, which is a transient error rather than battery.ErrUnsupported, so
// Watch's own loop falls through to its select - and there ctx.Done() is
// already ready, so it returns nil before ever ticking again. pollBattery
// therefore reports no error at all here rather than a wrapped one; that is
// checked rather than assumed, since a reader with a different failure mode
// could behave differently.
func TestPollBattery(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "no override path", path: ""},
		{name: "an override path", path: "/nonexistent/uevent"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			status := battery.NewStatus(battery.WithPercentage(unknownCharge))

			if err := pollBattery(ctx, status, testCase.path); err != nil {
				t.Errorf("pollBattery(%q) = %v, want nil on this platform", testCase.path, err)
			}
		})
	}
}

// --- shore, range and minimal ---------------------------------------------

// midRangeNm is a --range value comfortably inside the scope's limits. The
// limits themselves are read off a scope rather than written here, so this is
// the only figure in these tests that has to be picked by hand.
const midRangeNm = 200

// rangeText spells a range the way an operator would type it, with no
// trailing zeros to make the flag look like something it is not.
func rangeText(nauticalMiles float64) string {
	return strconv.FormatFloat(nauticalMiles, 'f', -1, 64)
}

func TestParseFlagsShore(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want radar.Toggle
	}{
		{name: caseDefault, args: nil, want: radar.ToggleOn},
		{name: "on explicit", args: []string{flagShore, defaultOn}, want: radar.ToggleOn},
		{name: offValue, args: []string{flagShore, offValue}, want: radar.ToggleOff},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.shore = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsShoreRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: caseEmpty, args: []string{flagShore, ""}},
		{
			// --shore is the same allow list --airports is, so the same
			// near-misses have to be refused rather than guessed at.
			name: caseWrongCase, args: []string{flagShore, "Off"},
		},
		{name: "a bool spelling is not one of the two words", args: []string{flagShore, "false"}},
		{name: "coast is not one of the two words either", args: []string{flagShore, "coast"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errShore) {
				t.Fatalf("parseFlags(%v) error = %v, want errShore", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, radar.ErrToggle) {
				t.Errorf("parseFlags(%v) error = %v, want radar.ErrToggle wrapped in it", testCase.args, err)
			}
		})
	}
}

func TestParseFlagsRange(t *testing.T) {
	t.Parallel()

	limits := scope.New()

	for _, testCase := range []struct {
		name string
		args []string
		want float64
	}{
		{name: caseDefault, args: nil, want: 0},
		{name: caseAutoGiven, args: []string{flagRange, defaultRange}, want: 0},
		{
			name: "the closest the scope can show",
			args: []string{flagRange, rangeText(limits.GetMin())}, want: limits.GetMin(),
		},
		{name: "a range in between", args: []string{flagRange, rangeText(midRangeNm)}, want: midRangeNm},
		{
			name: "the furthest the scope can show",
			args: []string{flagRange, rangeText(limits.GetMax())}, want: limits.GetMax(),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.rangeNm = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsRangeRejections(t *testing.T) {
	t.Parallel()

	limits := scope.New()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: caseEmpty, args: []string{flagRange, ""}},
		{name: "not a number", args: []string{flagRange, "forty"}},
		{
			// ParseFloat is happy to read "NaN", so the guard that refuses it
			// is the range check rather than the parse.
			name: "not a number the scope could show", args: []string{flagRange, nanText},
		},
		{name: "below the closest the scope can show", args: []string{flagRange, rangeText(limits.GetMin() - 1)}},
		{name: "past the furthest the scope can show", args: []string{flagRange, rangeText(limits.GetMax() + 1)}},
		{name: "negative", args: []string{flagRange, "-40"}},
		{
			// auto is the only word the flag takes, so a synonym is refused
			// the same way a misspelling would be.
			name: caseWrongCase, args: []string{flagRange, "Auto"},
		},
		{name: "a synonym for auto", args: []string{flagRange, "fit"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errRange) {
				t.Fatalf("parseFlags(%v) error = %v, want errRange", testCase.args, err)
			}

			if !errors.Is(err, radar.ErrRange) {
				t.Errorf("parseFlags(%v) error = %v, want radar.ErrRange wrapped in it", testCase.args, err)
			}
		})
	}
}

func TestParseFlagsRecenter(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: caseDefault, args: nil, want: radar.DefaultRecentre},
		{name: "explicit default", args: []string{flagRecenter, defaultRecentre}, want: radar.DefaultRecentre},
		{name: "zero stays on the receiver", args: []string{flagRecenter, "0"}, want: 0},
		{name: "lower edge", args: []string{flagRecenter, "10s"}, want: radar.MinRecentre},
		{name: "upper edge", args: []string{flagRecenter, "1h"}, want: radar.MaxRecentre},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.recentre = testCase.want

			checkConfig(t, got, want)
		})
	}
}

func TestParseFlagsRecenterRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "just under the floor", args: []string{flagRecenter, "9s"}},
		{name: "just over the ceiling", args: []string{flagRecenter, "2h"}},
		{name: "negative", args: []string{flagRecenter, "-1m"}},
		{name: "not a duration", args: []string{flagRecenter, "forever"}},
		{name: caseEmpty, args: []string{flagRecenter, ""}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errRecentre) {
				t.Errorf("parseFlags(%v) error = %v, want errors.Is(errRecentre)", testCase.args, err)
			}
		})
	}
}

// TestParseFlagsView covers --view's three accepted spellings, plus the
// default it falls back to when nobody sets it.
func TestParseFlagsView(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want radar.View
	}{
		{name: caseDefault, args: nil, want: radar.ViewScope},
		{name: "scope explicit", args: []string{flagView, defaultView}, want: radar.ViewScope},
		{name: "minimal", args: []string{flagView, viewMinimalValue}, want: radar.ViewMinimal},
		{name: "3d", args: []string{flagView, view3DValue}, want: radar.View3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.view = testCase.want

			checkConfig(t, got, want)
		})
	}
}

// TestParseFlagsViewRejections covers every way --view can be turned away: an
// empty value, either of the two wrong-case near-misses, and an unknown word.
func TestParseFlagsViewRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: caseEmpty, args: []string{flagView, ""}},
		{
			// --view is an allow list, not free text: the exact spelling is
			// what is accepted, not a case-insensitive match of it.
			name: caseWrongCase, args: []string{flagView, "Scope"},
		},
		{name: "wrong case for 3d", args: []string{flagView, "3D"}},
		{name: "unknown view", args: []string{flagView, "birdseye"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errView) {
				t.Fatalf("parseFlags(%v) error = %v, want errView", testCase.args, err)
			}

			// The wrapped cause travels with it, so a reader sees both the
			// flag that was wrong and the value that was rejected.
			if !errors.Is(err, radar.ErrView) {
				t.Errorf("parseFlags(%v) error = %v, want radar.ErrView wrapped in it", testCase.args, err)
			}
		})
	}
}

// exaggerateText spells an --exaggerate value the way an operator would type
// it, mirroring rangeText for the same reason: no trailing zeros.
func exaggerateText(factor float64) string {
	return strconv.FormatFloat(factor, 'f', -1, 64)
}

// TestParseFlagsExaggerate covers --exaggerate's default, both edges of its
// range, and a value in between.
func TestParseFlagsExaggerate(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
		want float64
	}{
		{name: caseDefault, args: nil, want: radar.DefaultExaggerate},
		{
			name: "lower edge",
			args: []string{flagExaggerate, exaggerateText(radar.MinExaggerate)},
			want: radar.MinExaggerate,
		},
		{
			name: "a value in between",
			args: []string{flagExaggerate, exaggerateText(midExaggerate)},
			want: midExaggerate,
		},
		{
			name: caseMaxEdge,
			args: []string{flagExaggerate, exaggerateText(radar.MaxExaggerate)},
			want: radar.MaxExaggerate,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseFlags(testCase.args)
			if err != nil {
				t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
			}

			want := defaultConfig()
			want.exaggerate = testCase.want

			checkConfig(t, got, want)
		})
	}
}

// TestParseFlagsExaggerateRejections covers every way --exaggerate can be
// turned away: below the floor, above the ceiling, and NaN.
func TestParseFlagsExaggerateRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "zero", args: []string{flagExaggerate, "0"}},
		{name: "a negative value", args: []string{flagExaggerate, "-1"}},
		{name: "just above the maximum", args: []string{flagExaggerate, exaggerateText(aboveMaxExaggerate)}},
		{
			// strconv.ParseFloat, and therefore the flag package behind
			// --exaggerate, accepts "NaN" outright; checkExaggerate is the
			// check that turns it away.
			name: "NaN", args: []string{flagExaggerate, nanText},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags(testCase.args)
			if !errors.Is(err, errExaggerate) {
				t.Errorf("parseFlags(%v) error = %v, want errors.Is(errExaggerate)", testCase.args, err)
			}
		})
	}
}

// TestParseFlagsBooleans covers the two flags that are a bare switch: each
// in its default state, given on its own, and given with an explicit value,
// because the flag package accepts all three spellings and a switch that only
// worked as --flag would be a surprise to anyone scripting it.
func TestParseFlagsBooleans(t *testing.T) {
	t.Parallel()

	for _, flagCase := range []struct {
		flag  string
		apply func(*config, bool)
	}{
		{flag: flagNoDecay, apply: func(cfg *config, on bool) { cfg.noDecay = on }},
		{flag: flagDemoSector, apply: func(cfg *config, on bool) { cfg.demoSector = on }},
	} {
		for _, testCase := range []struct {
			name string
			args []string
			want bool
		}{
			{name: caseDefault, args: nil, want: false},
			{name: "given", args: []string{flagCase.flag}, want: true},
			{name: "given as true", args: []string{flagCase.flag + "=true"}, want: true},
			{name: "given as false", args: []string{flagCase.flag + "=false"}, want: false},
		} {
			t.Run(flagCase.flag+" "+testCase.name, func(t *testing.T) {
				t.Parallel()

				got, err := parseFlags(testCase.args)
				if err != nil {
					t.Fatalf("parseFlags(%v) unexpected error: %v", testCase.args, err)
				}

				want := defaultConfig()
				flagCase.apply(&want, testCase.want)

				checkConfig(t, got, want)
			})
		}
	}
}

// TestMinimalFlagRemoved guards against --minimal reappearing by accident. It
// used to seed the radar's minimal view at startup; a run now always starts
// on the full scope, and v, inside the radar's own Handle, is the only way
// into minimal.
func TestMinimalFlagRemoved(t *testing.T) {
	t.Parallel()

	if _, err := parseFlags([]string{flagMinimal}); err == nil {
		t.Fatalf("parseFlags([%s]) = nil error, want a parse failure: the flag no longer exists", flagMinimal)
	}
}

// TestRunReportsShoreDataItCannotDecode covers the branch that cannot happen
// in a shipped binary: the embedded coastline is a fixed file that decodes or
// does not, and it does. The branch still has to be there, because a build
// whose data was corrupted on the way into the binary is exactly the run
// somebody needs a clear line on stderr from.
//
// Not parallel on purpose, for the same reason the newSource test is not: it
// rewrites a package-level seam, and parallel tests only resume once every
// sequential test has finished.
func TestRunReportsShoreDataItCannotDecode(t *testing.T) { //nolint:paralleltest // rewrites the loadShore seam
	original := loadShore
	loadShore = func() (*shore.Set, error) { return nil, errUnrelated }

	t.Cleanup(func() { loadShore = original })

	var stdout, stderr bytes.Buffer

	if got := run([]string{flagDemo}, &stdout, &stderr); got != exitFailure {
		t.Errorf("run() exit code = %d, want %d", got, exitFailure)
	}

	if !strings.Contains(stderr.String(), errUnrelated.Error()) {
		t.Errorf("run() stderr = %q, want it to name the failure", stderr.String())
	}
}
