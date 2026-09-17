package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"math"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

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
	defaultFB       = "/dev/fb0"
	defaultRotate   = "auto"
	defaultFPS      = 30
	defaultSize     = "1280x720"
	defaultBackend  = "auto"
	defaultScene    = "radar"
	defaultTheme    = "night"
	defaultLook     = "glass"
	defaultColour   = "altitude"
	defaultOn       = "on"
	defaultRange    = "auto"
	defaultRecentre = "3m"
	defaultView     = "scope"

	// autoRotate is the one non-numeric value --rotate accepts.
	autoRotate = "auto"

	// defaultGPSDAddress is where gpsd listens, and gpsdOff the one
	// non-address value --gpsd accepts. The address is spelled with a hostname
	// rather than 127.0.0.1 so a run against a daemon on the other end of a
	// tunnel only needs the host swapped.
	defaultGPSDAddress = "localhost:2947"
	gpsdOff            = "off"

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
	errScene      = errors.New(appName + ": --scene must be radar or pattern")
	errTheme      = errors.New(appName + ": --theme must be night or day")
	errLook       = errors.New(appName + ": --look must be glass, phosphor or mono")
	errColour     = errors.New(appName + ": --colour must be altitude or airline")
	errAirports   = errors.New(appName + ": --airports must be on or off")
	errShore      = errors.New(appName + ": --shore must be on or off")
	errRange      = errors.New(appName + ": --range must be auto or a range the scope can show")
	errBattery    = errors.New(appName + ": --battery must not be empty")
	errLatitude   = errors.New(appName + ": --lat out of range")
	errLongitude  = errors.New(appName + ": --lon out of range")
	errLatLonPair = errors.New(appName + ": --lat and --lon must be given together")
	errBeastAddr  = errors.New(appName + ": --beast must be HOST:PORT")
	errPNGBoth    = errors.New(appName + ": --png and --backend disagree")
	errPNGPath    = errors.New(appName + ": --backend png needs --png PATH to write to")
	errRecentre   = errors.New(appName + ": --recenter must be 0 or an interval from 10s to 1h")
	errView       = errors.New(appName + ": --view must be scope, 3d, minimal or minimal3d")
	errExaggerate = errors.New(appName + ": --exaggerate out of range")
	errGPSD       = errors.New(appName + ": --gpsd must be off or HOST:PORT")
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
	look        theme.Look
	colour      radar.ColourMode
	airports    radar.Toggle
	shore       radar.Toggle
	rangeNm     float64
	recentre    time.Duration
	view        radar.View
	exaggerate  float64

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

	// gpsd is where to watch for a real fix, already validated, and empty when
	// there is to be no watcher at all. Empty covers both --gpsd off and the
	// default on a platform that has no gpsd on it.
	gpsd string

	// demoSector is inert unless the demo fleet is what ends up running, the
	// same way --beast alongside --demo is not an error: the operator asked
	// for something that only matters if a later choice makes it apply.
	demoSector bool

	// biasTee powers an LNA over the coax and autoSweep walks the gain grid
	// once before the first frame. Both only apply when uScope is driving the
	// radio itself, and both are inert rather than an error anywhere else,
	// for the reason demoSector is: the operator asked for something a later
	// choice made irrelevant. sourceFor says so once on stderr, because a
	// silently ignored --bias-t is an LNA the operator believes is powered.
	biasTee   bool
	autoSweep bool
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
	look        string
	colour      string
	airports    string
	shore       string
	scopeRange  string
	battery     string
	beast       string
	replay      string
	gpsd        string
	latitude    string
	longitude   string
	recentre    string
	view        string
	exaggerate  float64
	fps         int
	frames      int
	testPattern bool
	demo        bool
	demoSector  bool
	biasTee     bool
	autoSweep   bool
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
		"what to draw: radar or pattern. pattern is a flags-only diagnostic with no key back to it")
	set.StringVar(&raw.theme, "theme", defaultTheme,
		"colour theme: night or day. l cycles them while it runs")
	set.StringVar(&raw.look, "look", defaultLook,
		"which palette to wear: glass, phosphor or mono. k cycles them while it runs")
	set.StringVar(&raw.colour, "colour", defaultColour,
		"what an aircraft's colour means: altitude or airline")
	set.StringVar(&raw.airports, "airports", defaultOn,
		"draw the airfield markers on the scope: on or off")
	set.StringVar(&raw.shore, "shore", defaultOn,
		"draw the coastline under the scope: on or off")
	set.StringVar(&raw.scopeRange, "range", defaultRange,
		"scope range in nautical miles, or auto to fit the aircraft on the field")
	// The flag is spelled the American way and the Go identifiers behind it
	// are spelled the British one, which is deliberate rather than a slip:
	// "recenter" is what anyone reaching for this flag will type, and
	// internal/radar spells everything else the way the rest of this repo
	// does. Do not "fix" either half into the other.
	set.StringVar(&raw.recentre, "recenter", defaultRecentre,
		"how often minimal mode recentres on the traffic, 10s to 1h, or 0 to stay on the receiver")
	set.StringVar(&raw.view, "view", defaultView,
		"which view the radar starts on: scope, 3d, minimal or minimal3d. v cycles them while it runs")
	set.Float64Var(&raw.exaggerate, "exaggerate", radar.DefaultExaggerate,
		"how far the 3d view stretches altitude into height, 1 to 20")
	set.StringVar(&raw.battery, "battery", "",
		"power-supply uevent file to read the battery from; empty finds one, Linux only")
	set.BoolVar(&raw.demo, "demo", false,
		"fly an invented fleet instead of decoding one, for a machine with no receiver")
	set.BoolVar(&raw.demoSector, "demo-sector", false,
		"place the whole demo fleet in the north-west quadrant, as a directional antenna would")
	set.BoolVar(&raw.biasTee, "bias-t", false,
		"power an external LNA over the coax from the dongle's bias-tee; local SDR only")
	set.BoolVar(&raw.autoSweep, "auto-sweep", false,
		"walk the gain grid once before the first frame and keep the best cell; local SDR only")
	set.StringVar(&raw.beast, "beast", "",
		"consume Mode S frames from a remote demodulator at HOST:PORT")
	set.StringVar(&raw.gpsd, "gpsd", defaultGPSD(runtime.GOOS),
		"watch a gpsd daemon at HOST:PORT for the receiver's own position, or off to work it out from the aircraft")
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
	look       theme.Look
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

	look, err := parseLook(raw.look)
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

	shoreToggle, err := parseShore(raw.shore)
	if err != nil {
		return display{}, err
	}

	rangeNm, err := parseRange(raw.scopeRange)
	if err != nil {
		return display{}, err
	}

	recentre, err := parseRecentre(raw.recentre)
	if err != nil {
		return display{}, err
	}

	view, err := parseView(raw.view)
	if err != nil {
		return display{}, err
	}

	if err := checkExaggerate(raw.exaggerate); err != nil {
		return display{}, err
	}

	return display{
		rotation:   rotation,
		autoRotate: auto,
		size:       size,
		backend:    kind,
		scene:      scene,
		theme:      themeKind,
		look:       look,
		radar: radar.Settings{
			Colour:   colour,
			Airports: airports,
			Shore:    shoreToggle,
			RangeNm:  rangeNm,
			Recentre: recentre,

			View:       view,
			Exaggerate: raw.exaggerate,
		},
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

	gpsd, err := parseGPSD(raw.gpsd)
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
		look:        show.look,
		colour:      show.radar.Colour,
		airports:    show.radar.Airports,
		shore:       show.radar.Shore,
		rangeNm:     show.radar.RangeNm,
		recentre:    show.radar.Recentre,
		view:        show.radar.View,
		exaggerate:  show.radar.Exaggerate,
		battery:     raw.battery,
		source:      chosen,
		beast:       raw.beast,
		replay:      raw.replay,
		gpsd:        gpsd,
		latitude:    place.latitude,
		longitude:   place.longitude,
		hasLocation: place.given,
		demoSector:  raw.demoSector,
		biasTee:     raw.biasTee,
		autoSweep:   raw.autoSweep,
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
		if err := hostPort(raw.beast, errBeastAddr); err != nil {
			return sourceAuto, err
		}

		return sourceBeast, nil
	}

	if raw.demo {
		return sourceDemo, nil
	}

	return sourceAuto, nil
}

// hostPort makes sure an address is something that could be dialled.
//
// SplitHostPort alone is not enough: it is happy with ":" and with a bare
// port, and a half-written address is more likely a typo than an intent to
// dial localhost on port nothing. --beast and --gpsd share the check and
// differ only in which sentinel comes back, because they want the same shape
// and two copies of it would drift.
func hostPort(address string, sentinel error) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", sentinel, address, err)
	}

	if host == "" || port == "" {
		return fmt.Errorf("%w: %q has no %s", sentinel, address, missingPart(host))
	}

	return nil
}

// defaultGPSD is where --gpsd points when nobody says.
//
// Linux is the only platform uScope decodes on and the only one the uConsole's
// gpsd runs on, so it is the only one where looking for a daemon is worth the
// connection attempt. Everywhere else the default is off: a Mac developing the
// layout has no gpsd, and a watcher retrying a refused connection would put a
// warning on stderr that answers a question nobody asked.
func defaultGPSD(goos string) string {
	if goos == linuxGOOS {
		return defaultGPSDAddress
	}

	return gpsdOff
}

// parseGPSD reads --gpsd. It hands back the empty string for a run with no
// watcher, which is what internal/source reads as off.
func parseGPSD(text string) (string, error) {
	if text == gpsdOff {
		return "", nil
	}

	if err := hostPort(text, errGPSD); err != nil {
		return "", err
	}

	return text, nil
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

// parseLook reads --look against internal/theme's allow list, the same way
// parseTheme reads the other half of the pair.
func parseLook(text string) (theme.Look, error) {
	look, err := theme.ParseLook(text)
	if err != nil {
		return theme.LookGlass, fmt.Errorf("%w: %w", errLook, err)
	}

	return look, nil
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

// parseShore reads --shore against internal/radar's on/off allow list.
func parseShore(text string) (radar.Toggle, error) {
	toggle, err := radar.ParseToggle(text)
	if err != nil {
		return radar.ToggleOn, fmt.Errorf("%w: %w", errShore, err)
	}

	return toggle, nil
}

// parseRange reads --range against internal/radar's allow list, which takes
// its limits from the scope control rather than from numbers written here.
func parseRange(text string) (float64, error) {
	value, err := radar.ParseRange(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errRange, err)
	}

	return value, nil
}

// parseRecentre reads --recenter against internal/radar's allow list, which
// owns the limits for the same reason parseRange does not write them here.
func parseRecentre(text string) (time.Duration, error) {
	every, err := radar.ParseRecentre(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errRecentre, err)
	}

	return every, nil
}

// parseView reads --view against internal/radar's allow list.
func parseView(text string) (radar.View, error) {
	view, err := radar.ParseView(text)
	if err != nil {
		return radar.ViewScope, fmt.Errorf("%w: %w", errView, err)
	}

	return view, nil
}

// checkExaggerate range-checks --exaggerate.
//
// It is a float64 flag rather than a string, unlike --lat and --range, because
// there is no value it could be handed that means "nobody said": one is life
// size and zero is a flat world, so both ends are real settings and the flag
// simply has a default like --fps does.
func checkExaggerate(factor float64) error {
	if math.IsNaN(factor) || factor < radar.MinExaggerate || factor > radar.MaxExaggerate {
		return fmt.Errorf("%w: got %g, want %g to %g",
			errExaggerate, factor, radar.MinExaggerate, radar.MaxExaggerate)
	}

	return nil
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
