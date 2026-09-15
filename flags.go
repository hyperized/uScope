package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"

	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/rotate"
)

const appName = "uScope"

// Flag defaults. The framebuffer path and the size match the uConsole, since
// that is the machine this is for.
const (
	defaultFB      = "/dev/fb0"
	defaultRotate  = "auto"
	defaultFPS     = 30
	defaultSize    = "1280x720"
	defaultBackend = "auto"

	// autoRotate is the one non-numeric value --rotate accepts.
	autoRotate = "auto"

	minFPS, maxFPS = 1, 120

	// --frames 0 means run until the user quits. The ceiling is there so a
	// mistyped argument cannot pin a terminal for a week; nothing needs more
	// than a thousand frames from a single invocation.
	minFrames, maxFrames = 0, 1000

	// maxDimension is a sanity ceiling for --size, not a hardware limit. It
	// exists so a typo allocates a rejected flag instead of 40 GB of canvas.
	minDimension, maxDimension = 1, 8192
)

// Flag validation errors. Each is a sentinel so a test can assert which rule
// rejected the input rather than matching on a message.
var (
	errEmptyFB  = errors.New(appName + ": --fb must not be empty")
	errFPSRange = errors.New(appName + ": --fps out of range")
	errRotate   = errors.New(appName + ": --rotate must be auto, 0, 1, 2 or 3")
	errSize     = errors.New(appName + ": --size must be WxH")
	errFrames   = errors.New(appName + ": --frames out of range")
	errBackend  = errors.New(appName + ": --backend must be auto, fb, kitty, blocks or png")
	errPNGBoth  = errors.New(appName + ": --png and --backend disagree")
	errPNGPath  = errors.New(appName + ": --backend png needs --png PATH to write to")
)

// config is the validated command line. Everything in it has already been
// range-checked, so nothing downstream has to check again.
type config struct {
	fbPath      string
	rotation    rotate.Rotation
	autoRotate  bool
	fps         int
	frames      int
	testPattern bool
	pngPath     string
	size        image.Point
	backend     backend.Kind
}

// rawFlags is the command line before validation: whatever the flag package
// managed to parse, in the shapes it parses into.
type rawFlags struct {
	fb          string
	rotate      string
	png         string
	size        string
	backend     string
	fps         int
	frames      int
	testPattern bool
}

// parseFlags turns an argument list into a validated config.
//
// ContinueOnError rather than ExitOnError, so a bad flag returns an error
// the caller can turn into an exit code instead of flag calling os.Exit from
// underneath it.
func parseFlags(args []string) (config, error) {
	set := flag.NewFlagSet(appName, flag.ContinueOnError)
	set.SetOutput(os.Stderr)

	raw := bind(set)

	if err := set.Parse(args); err != nil {
		return config{}, fmt.Errorf("%s: parsing flags: %w", appName, err)
	}

	return raw.validated()
}

// bind declares every flag against set and returns where the values land.
func bind(set *flag.FlagSet) *rawFlags {
	raw := &rawFlags{}

	set.StringVar(&raw.fb, "fb", defaultFB,
		"framebuffer device to draw on")
	set.StringVar(&raw.rotate, "rotate", defaultRotate,
		"panel rotation: auto reads "+rotate.SysfsPath+", or force 0, 1, 2 or 3 in fbcon's numbering")
	set.IntVar(&raw.fps, "fps", defaultFPS,
		"frames per second in live mode, 1 to 120")
	set.BoolVar(&raw.testPattern, "test-pattern", false,
		"paint one test frame and exit, leaving console and terminal untouched")
	set.StringVar(&raw.png, "png", "",
		"render the scene to this PNG file instead of drawing it on a screen")
	set.StringVar(&raw.size, "size", defaultSize,
		"canvas size as WxH, used by --png and by the kitty backend")
	set.StringVar(&raw.backend, "backend", defaultBackend,
		"where to draw: auto, fb, kitty, blocks or png")
	set.IntVar(&raw.frames, "frames", 0,
		"stop after this many frames, 0 to run until quit, up to 1000")

	return raw
}

// validated range-checks everything and produces the config.
func (raw rawFlags) validated() (config, error) {
	if raw.fb == "" {
		return config{}, errEmptyFB
	}

	if raw.fps < minFPS || raw.fps > maxFPS {
		return config{}, fmt.Errorf("%w: got %d, want %d to %d", errFPSRange, raw.fps, minFPS, maxFPS)
	}

	if raw.frames < minFrames || raw.frames > maxFrames {
		return config{}, fmt.Errorf("%w: got %d, want %d to %d", errFrames, raw.frames, minFrames, maxFrames)
	}

	rot, auto, err := parseRotate(raw.rotate)
	if err != nil {
		return config{}, err
	}

	size, err := parseSize(raw.size)
	if err != nil {
		return config{}, err
	}

	kind, err := parseBackend(raw.backend, raw.png)
	if err != nil {
		return config{}, err
	}

	return config{
		fbPath:      raw.fb,
		rotation:    rot,
		autoRotate:  auto,
		fps:         raw.fps,
		frames:      raw.frames,
		testPattern: raw.testPattern,
		pngPath:     raw.png,
		size:        size,
		backend:     kind,
	}, nil
}

// parseBackend reads --backend and reconciles it with --png.
//
// The two flags overlap, and guessing which one the operator meant is worse
// than saying they disagree: --png with --backend kitty could reasonably
// mean either "write a file" or "draw in the terminal and also write a
// file", and only one of those is implemented.
func parseBackend(text, pngPath string) (backend.Kind, error) {
	kind, err := backend.Parse(text)
	if err != nil {
		return backend.Auto, fmt.Errorf("%w: %w", errBackend, err)
	}

	if pngPath == "" {
		if kind == backend.PNG {
			return backend.Auto, errPNGPath
		}

		return kind, nil
	}

	if kind != backend.Auto && kind != backend.PNG {
		return backend.Auto, fmt.Errorf("%w: --png with --backend %s", errPNGBoth, kind)
	}

	return backend.PNG, nil
}

// parseRotate reads --rotate. The bool reports whether to autodetect.
func parseRotate(text string) (rotate.Rotation, bool, error) {
	if text == autoRotate {
		return rotate.None, true, nil
	}

	rot, err := rotate.Parse(text)
	if err != nil {
		return rotate.None, false, fmt.Errorf("%w: %w", errRotate, err)
	}

	return rot, false, nil
}

// parseSize reads --size in the WxH form.
func parseSize(text string) (image.Point, error) {
	widthText, heightText, found := strings.Cut(text, "x")
	if !found {
		return image.Point{}, fmt.Errorf("%w: %q has no x", errSize, text)
	}

	width, err := dimension(widthText)
	if err != nil {
		return image.Point{}, err
	}

	height, err := dimension(heightText)
	if err != nil {
		return image.Point{}, err
	}

	return image.Pt(width, height), nil
}

// dimension parses and range-checks one side of --size.
func dimension(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", errSize, text)
	}

	if value < minDimension || value > maxDimension {
		return 0, fmt.Errorf("%w: %d is outside %d to %d", errSize, value, minDimension, maxDimension)
	}

	return value, nil
}
