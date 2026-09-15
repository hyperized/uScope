package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"math"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
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
	defaultScene   = "radar"
	defaultTheme   = "night"
	defaultColour  = "altitude"
	defaultOn      = "on"

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

	// The coordinate limits --lat and --lon are checked against. internal/source
	// checks them again, because it is a library entry point and a caller that
	// skipped the flags would otherwise centre the scope on nowhere.
	minLatitude, maxLatitude   = -90.0, 90.0
	minLongitude, maxLongitude = -180.0, 180.0

	// linuxGOOS is the only platform with a radio driver behind it, so it is
	// the only one where "no source given" means the local SDR.
	linuxGOOS = "linux"
)

// sourceKind is where the aircraft come from. The set is closed for the same
// reason backend.Kind's is: a typo that fell through to a default would draw
// something nobody asked for.
type sourceKind uint8

// The sources, in the order --replay-iq, --beast and --demo beat each other.
const (
	// sourceAuto means nobody chose: the local radio on Linux, the demo
	// fleet anywhere else.
	sourceAuto sourceKind = iota

	// sourceDemo is the invented fleet, which is what makes the radar
	// developable on a machine with no receiver.
	sourceDemo

	// sourceBeast consumes Mode S frames from a remote demodulator.
	sourceBeast

	// sourceReplay plays a captured IQ file back through the demodulator.
	sourceReplay
)

// Flag validation errors. Each is a sentinel so a test can assert which rule
// rejected the input rather than matching on a message.
var (
	errEmptyFB    = errors.New(appName + ": --fb must not be empty")
	errFPSRange   = errors.New(appName + ": --fps out of range")
	errRotate     = errors.New(appName + ": --rotate must be auto, 0, 1, 2 or 3")
	errSize       = errors.New(appName + ": --size must be WxH")
	errFrames     = errors.New(appName + ": --frames out of range")
	errBackend    = errors.New(appName + ": --backend must be auto, fb, kitty, blocks or png")
	errScene      = errors.New(appName + ": --scene must be radar, pattern or specimen")
	errTheme      = errors.New(appName + ": --theme must be night or paper")
	errColour     = errors.New(appName + ": --colour must be altitude or airline")
	errAirports   = errors.New(appName + ": --airports must be on or off")
	errBattery    = errors.New(appName + ": --battery must not be empty")
	errLatitude   = errors.New(appName + ": --lat out of range")
	errLongitude  = errors.New(appName + ": --lon out of range")
	errLatLonPair = errors.New(appName + ": --lat and --lon must be given together")
	errBeastAddr  = errors.New(appName + ": --beast must be HOST:PORT")
	errPNGBoth    = errors.New(appName + ": --png and --backend disagree")
	errPNGPath    = errors.New(appName + ": --backend png needs --png PATH to write to")
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
	scene       app.SceneKind
	theme       theme.Kind
	colour      radar.ColourMode
	airports    radar.Toggle

	// battery is the power-supply file to read instead of looking one up. It
	// is empty for the normal case, which is autodiscovery.
	battery string

	// Where the aircraft come from, and where the receiver is if the operator
	// said. hasLocation is separate from the two coordinates because latitude
	// zero is the Gulf of Guinea, not "unset".
	source      sourceKind
	beast       string
	replay      string
	latitude    float64
	longitude   float64
	hasLocation bool
}

// rawFlags is the command line before validation: whatever the flag package
// managed to parse, in the shapes it parses into.
type rawFlags struct {
	fb          string
	rotate      string
	png         string
	size        string
	backend     string
	scene       string
	theme       string
	colour      string
	airports    string
	battery     string
	beast       string
	replay      string
	latitude    string
	longitude   string
	fps         int
	frames      int
	testPattern bool
	demo        bool
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
		"draw one still frame of the selected scene and exit, leaving console and terminal untouched")
	set.StringVar(&raw.png, "png", "",
		"render the scene to this PNG file instead of drawing it on a screen")
	set.StringVar(&raw.size, "size", defaultSize,
		"canvas size as WxH, used by --png and by the kitty backend")
	set.StringVar(&raw.backend, "backend", defaultBackend,
		"where to draw: auto, fb, kitty, blocks or png")
	set.StringVar(&raw.scene, "scene", defaultScene,
		"what to draw: radar, pattern or specimen")
	set.StringVar(&raw.theme, "theme", defaultTheme,
		"colour theme: night or paper")
	set.StringVar(&raw.colour, "colour", defaultColour,
		"what an aircraft's colour means: altitude or airline")
	set.StringVar(&raw.airports, "airports", defaultOn,
		"draw the airfield markers on the scope: on or off")
	set.StringVar(&raw.battery, "battery", "",
		"power-supply uevent file to read the battery from; empty finds one, Linux only")
	set.BoolVar(&raw.demo, "demo", false,
		"fly an invented fleet instead of decoding one, for a machine with no receiver")
	set.StringVar(&raw.beast, "beast", "",
		"consume Mode S frames from a remote demodulator at HOST:PORT")
	set.StringVar(&raw.replay, "replay-iq", "",
		"replay a captured IQ file through the demodulator")
	set.StringVar(&raw.latitude, "lat", "",
		"receiver latitude in degrees, -90 to 90; needs --lon as well")
	set.StringVar(&raw.longitude, "lon", "",
		"receiver longitude in degrees, -180 to 180; needs --lat as well")
	set.IntVar(&raw.frames, "frames", 0,
		"stop after this many frames, 0 to run until quit, up to 1000")

	return raw
}

// display is the half of the command line that decides what gets drawn and
// where it lands.
//
// It is a struct rather than eight return values because validated would
// otherwise carry one branch per flag, and the flags keep arriving. Grouping
// them by what they do beats grouping them by which function happened to parse
// them first.
type display struct {
	rotation   rotate.Rotation
	autoRotate bool
	size       image.Point
	backend    backend.Kind
	scene      app.SceneKind
	theme      theme.Kind
	radar      radar.Settings
}

// display parses every flag that is an allow list or a shape, in the order
// they appear in --help.
func (raw rawFlags) display() (display, error) {
	rotation, auto, err := parseRotate(raw.rotate)
	if err != nil {
		return display{}, err
	}

	size, err := parseSize(raw.size)
	if err != nil {
		return display{}, err
	}

	kind, err := parseBackend(raw.backend, raw.png)
	if err != nil {
		return display{}, err
	}

	scene, err := parseScene(raw.scene)
	if err != nil {
		return display{}, err
	}

	themeKind, err := parseTheme(raw.theme)
	if err != nil {
		return display{}, err
	}

	colour, err := parseColour(raw.colour)
	if err != nil {
		return display{}, err
	}

	airports, err := parseAirports(raw.airports)
	if err != nil {
		return display{}, err
	}

	return display{
		rotation:   rotation,
		autoRotate: auto,
		size:       size,
		backend:    kind,
		scene:      scene,
		theme:      themeKind,
		radar:      radar.Settings{Colour: colour, Airports: airports},
	}, nil
}

// limits range-checks the three numeric flags, which are the only ones that
// are a number rather than a name.
func (raw rawFlags) limits() error {
	if raw.fb == "" {
		return errEmptyFB
	}

	if raw.fps < minFPS || raw.fps > maxFPS {
		return fmt.Errorf("%w: got %d, want %d to %d", errFPSRange, raw.fps, minFPS, maxFPS)
	}

	if raw.frames < minFrames || raw.frames > maxFrames {
		return fmt.Errorf("%w: got %d, want %d to %d", errFrames, raw.frames, minFrames, maxFrames)
	}

	return checkBattery(raw.battery)
}

// validated range-checks everything and produces the config.
func (raw rawFlags) validated() (config, error) {
	if err := raw.limits(); err != nil {
		return config{}, err
	}

	show, err := raw.display()
	if err != nil {
		return config{}, err
	}

	place, err := raw.location()
	if err != nil {
		return config{}, err
	}

	chosen, err := raw.sourceChoice()
	if err != nil {
		return config{}, err
	}

	return config{
		fbPath:      raw.fb,
		rotation:    show.rotation,
		autoRotate:  show.autoRotate,
		fps:         raw.fps,
		frames:      raw.frames,
		testPattern: raw.testPattern,
		pngPath:     raw.png,
		size:        show.size,
		backend:     show.backend,
		scene:       show.scene,
		theme:       show.theme,
		colour:      show.radar.Colour,
		airports:    show.radar.Airports,
		battery:     raw.battery,
		source:      chosen,
		beast:       raw.beast,
		replay:      raw.replay,
		latitude:    place.latitude,
		longitude:   place.longitude,
		hasLocation: place.given,
	}, nil
}

// location is the receiver position the operator typed in, if any.
type location struct {
	latitude  float64
	longitude float64
	given     bool
}

// location reads --lat and --lon.
//
// They are strings rather than float64 flags because latitude zero is a real
// place off the coast of Ghana, so "unset" cannot be a number. A string also
// keeps the --help line readable: a float64 flag has to carry some sentinel as
// its default, and the sentinel is what gets printed.
//
// Both or neither: a latitude without a longitude is half an answer, and
// guessing the other half would put the scope somewhere nobody asked for.
func (raw rawFlags) location() (location, error) {
	if raw.latitude == "" && raw.longitude == "" {
		return location{}, nil
	}

	if raw.latitude == "" || raw.longitude == "" {
		return location{}, errLatLonPair
	}

	latitude, err := degrees(raw.latitude, minLatitude, maxLatitude, errLatitude)
	if err != nil {
		return location{}, err
	}

	longitude, err := degrees(raw.longitude, minLongitude, maxLongitude, errLongitude)
	if err != nil {
		return location{}, err
	}

	return location{latitude: latitude, longitude: longitude, given: true}, nil
}

// degrees parses and range-checks one coordinate.
func degrees(text string, low, high float64, sentinel error) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", sentinel, text)
	}

	if math.IsNaN(value) || value < low || value > high {
		return 0, fmt.Errorf("%w: got %s, want %g to %g", sentinel, text, low, high)
	}

	return value, nil
}

// sourceChoice settles where the aircraft come from.
//
// The order is replay, then beast, then demo, then whatever the platform has.
// Replay wins so a developer can always play a capture back on a host that
// also has a feed configured, which is the same precedence uAirwaves uses.
// Giving two of them is not an error: the more specific one is obviously what
// was meant, and refusing would only get in the way of a scripted run.
func (raw rawFlags) sourceChoice() (sourceKind, error) {
	if raw.replay != "" {
		return sourceReplay, nil
	}

	if raw.beast != "" {
		if err := checkBeastAddress(raw.beast); err != nil {
			return sourceAuto, err
		}

		return sourceBeast, nil
	}

	if raw.demo {
		return sourceDemo, nil
	}

	return sourceAuto, nil
}

// checkBeastAddress makes sure --beast is something that could be dialled.
//
// SplitHostPort alone is not enough: it is happy with ":" and with a bare
// port, and a half-written address is more likely a typo than an intent to
// dial localhost on port nothing.
func checkBeastAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", errBeastAddr, address, err)
	}

	if host == "" || port == "" {
		return fmt.Errorf("%w: %q has no %s", errBeastAddr, address, missingPart(host))
	}

	return nil
}

// missingPart names whichever half of a host:port pair is empty.
func missingPart(host string) string {
	if host == "" {
		return "host"
	}

	return "port"
}

// parseScene reads --scene against the closed set internal/app knows how to
// build. The list lives there because that is where the scenes are wired up,
// and two lists would only drift.
func parseScene(text string) (app.SceneKind, error) {
	kind, err := app.ParseScene(text)
	if err != nil {
		return app.Radar, fmt.Errorf("%w: %w", errScene, err)
	}

	return kind, nil
}

// parseTheme reads --theme against internal/theme's allow list.
func parseTheme(text string) (theme.Kind, error) {
	kind, err := theme.Parse(text)
	if err != nil {
		return theme.KindNight, fmt.Errorf("%w: %w", errTheme, err)
	}

	return kind, nil
}

// parseColour reads --colour against internal/radar's allow list.
func parseColour(text string) (radar.ColourMode, error) {
	mode, err := radar.ParseColour(text)
	if err != nil {
		return radar.ColourAltitude, fmt.Errorf("%w: %w", errColour, err)
	}

	return mode, nil
}

// parseAirports reads --airports against internal/radar's on/off allow list.
func parseAirports(text string) (radar.Toggle, error) {
	toggle, err := radar.ParseToggle(text)
	if err != nil {
		return radar.ToggleOn, fmt.Errorf("%w: %w", errAirports, err)
	}

	return toggle, nil
}

// checkBattery rejects an override that was given as an empty string.
//
// Leaving --battery off means autodiscovery, so an empty value is the operator
// asking for a file named nothing rather than asking for the default. The path
// itself is not checked: picking it is the point of the flag, and the reader
// below reports a file it cannot open.
func checkBattery(path string) error {
	if path == "" {
		return nil
	}

	if strings.TrimSpace(path) == "" {
		return errBattery
	}

	return nil
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
