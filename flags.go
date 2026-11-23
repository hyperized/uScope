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

	"github.com/hyperized/uScope/internal/affinity"
	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
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
	defaultTheme    = "auto"
	defaultLook     = "glass"
	defaultColour   = "altitude"
	defaultFilter   = "all"
	defaultOn       = "on"
	defaultOff      = "off"
	defaultRange    = "auto"
	defaultRecentre = "3m"
	defaultView     = "scope"

	// defaultCorridorHalfLife is source.DefaultCorridorHalfLife spelled the
	// way the flag is typed. A test holds the two together.
	defaultCorridorHalfLife = "2h"

	// autoRotate is the one non-numeric value --rotate accepts.
	autoRotate = "auto"

	// batteryOff is the one non-path value --battery accepts. A desktop has no
	// power supply to find, and without it uAirwaves' poller would warn on
	// stderr every thirty seconds about a battery that was never going to be
	// there.
	batteryOff = "off"

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

	// icaoLength is how long an airfield's ICAO code is.
	icaoLength = 4

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
	errTheme      = errors.New(appName + ": --theme must be night, day or auto")
	errLook       = errors.New(appName + ": --look must be glass, phosphor, mono or vector")
	errColour     = errors.New(appName + ": --colour must be altitude, airline or corridor")
	errFilter     = errors.New(appName + ": --filter must be all, low, mid, high, below-high, in, out or other")
	errAirports   = errors.New(appName + ": --airports must be on or off")
	errShore      = errors.New(appName + ": --shore must be on or off")
	errStars      = errors.New(appName + ": --stars must be on or off")
	errCorridors  = errors.New(appName + ": --corridors must be on or off")
	errCorrField  = errors.New(appName + ": --corridor-field must be an airfield's four-letter ICAO code, or empty")
	errHalfLife   = errors.New(appName + ": --corridor-half-life must be an interval from 10m to 24h")
	errMinShore   = errors.New(appName + ": --minimal-shore must be on or off")
	errMinAirport = errors.New(appName + ": --minimal-airports must be on or off")
	errMinBar     = errors.New(appName + ": --minimal-bar must be on or off")
	errMinBoard   = errors.New(appName + ": --minimal-board must be on or off")
	errLights     = errors.New(appName + ": --lights must be on, off or auto")
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
	errZoom       = errors.New(appName + ": --zoom out of range")
	errTilt       = errors.New(appName + ": --tilt out of range")
	errOrbit      = errors.New(appName + ": --orbit must be on or off")
	errAzimuth    = errors.New(appName + ": --azimuth out of range")
	errGPSD       = errors.New(appName + ": --gpsd must be off or HOST:PORT")
	errHistPath   = errors.New(appName + ": --history must be off or a path, and not -")
)

// The off spelling of --history and the one path it refuses: - reads as
// stdout everywhere else on a command line, and stdout is not a file
// anything can be kept in.
const (
	stateOff   = "off"
	stdoutPath = "-"
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
	filter      radar.BandFilter
	airports    radar.Toggle
	shore       radar.Toggle

	// minimalShore, minimalAirports, minimalBar and minimalBoard are the bare
	// views' own switches, already read from on/off into what the scene keeps.
	minimalShore    bool
	minimalAirports bool
	minimalBar      bool
	minimalBoard    bool
	lights          radar.LightsMode
	stars           radar.Toggle
	corridors       radar.Toggle
	rangeNm         float64
	recentre        time.Duration
	view            radar.View
	exaggerate      float64
	zoom            int
	tilt            float64
	orbit           radar.Toggle
	azimuth         float64

	// cpus is the --cpus set, empty to leave the threads where they are.
	cpus affinity.Set

	// battery is the power-supply file to read instead of looking one up. It
	// is empty for the normal case, which is autodiscovery, and for --battery
	// off, which noBattery says apart.
	battery string

	// noBattery is --battery off: no poller and no indicator. It is a field of
	// its own because empty already means "find one".
	noBattery bool

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

	// corridorField is the airfield --corridor-field pinned, upper-cased and
	// four letters long, and empty to let the source find the nearest one.
	// Whether the list knows it is the source's question, answered when it is
	// built; see sourceError.
	corridorField string

	// corridorHalfLife is how long a lost track takes to count for half as
	// much, and a route to fade out once nothing flies it, already inside the
	// source's bounds.
	corridorHalfLife time.Duration

	// history is the --history file, already validated, and empty for off.
	// Only a live source reads it; see sourceFor.
	history string

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
	filter      string
	airports    string
	shore       string
	minShore    string
	minAirports string
	minBar      string
	minBoard    string
	lights      string
	stars       string
	corridors   string
	corrField   string
	halfLife    string
	history     string
	scopeRange  string
	battery     string
	cpus        string
	beast       string
	replay      string
	gpsd        string
	latitude    string
	longitude   string
	recentre    string
	view        string
	exaggerate  float64
	zoom        int
	tilt        float64
	azimuth     float64
	orbit       string
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

	cfg, err := raw.validated()
	if err != nil {
		return config{}, err
	}

	cfg.cpus, err = parseCPUs(raw.cpus)
	if err != nil {
		return config{}, err
	}

	return cfg, nil
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
		"colour theme: night, day or auto. auto follows the sun at the receiver, night with no position. "+
			"l cycles them while it runs")
	set.StringVar(&raw.look, "look", defaultLook,
		"which palette to wear: glass, phosphor, mono or vector. k cycles them while it runs")
	set.StringVar(&raw.colour, "colour", defaultColour,
		"what an aircraft's colour means: altitude, airline or corridor, which is arriving at or leaving "+
			"the corridor airfield")
	set.StringVar(&raw.filter, "filter", defaultFilter,
		"which aircraft to draw: all, low, mid or high while colouring by altitude, in, out or other "+
			"while colouring by corridor, and below-high in either for everything under 25,000 ft. "+
			"f cycles them while it runs")
	set.StringVar(&raw.airports, "airports", defaultOn,
		"draw the airfield markers on the scope: on or off")
	set.StringVar(&raw.shore, "shore", defaultOn,
		"draw the coastline and the water fill under the scope: on or off")
	set.StringVar(&raw.minShore, "minimal-shore", defaultOff,
		"draw the coastline in minimal and minimal3d: on or off. m flips it while it runs")
	set.StringVar(&raw.minAirports, "minimal-airports", defaultOn,
		"draw the airfield markers in minimal and minimal3d: on or off. a flips it while it runs")
	set.StringVar(&raw.minBar, "minimal-bar", defaultOff,
		"draw a thin bar with the clocks and the sun across the top of minimal and minimal3d: on or off. "+
			"h flips it while it runs")
	set.StringVar(&raw.minBoard, "minimal-board", defaultOff,
		"draw the flight strip board in a column down the right quarter of minimal3d: on or off. "+
			"j flips it while it runs")
	set.StringVar(&raw.lights, "lights", defaultOn,
		"draw the aircraft's navigation lights, strobes and beacon: on, off, or auto to leave them off "+
			"in daylight and fade them in as the sun sets. s cycles them while it runs")
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
	bindCamera(set, raw)
	bindCPUs(set, &raw.cpus)
	set.StringVar(&raw.battery, "battery", "",
		"power-supply uevent file to read the battery from, or off for no indicator; empty finds one, Linux only")
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
	bindCorridors(set, raw)
	set.IntVar(&raw.frames, "frames", 0,
		"stop after this many frames, 0 to run until quit, up to 1000")

	return raw
}

// bindCorridors declares the corridor flags. The switch is the radar's and
// the other two are the source's, but they are one feature and read better
// together in --help.
func bindCorridors(set *flag.FlagSet, raw *rawFlags) {
	set.StringVar(&raw.corridors, "corridors", defaultOn,
		"draw the routes lost aircraft flew as tubes, fading with age and coloured by arrival or departure: "+
			"on or off. u flips it while it runs")
	set.StringVar(&raw.corrField, "corridor-field", "",
		"the airfield lost tracks are sorted by, as an ICAO code; empty takes the nearest within 150 nm")
	set.StringVar(&raw.halfLife, "corridor-half-life", defaultCorridorHalfLife,
		"how long a lost track takes to count for half as much, and how long a route is drawn once nothing flies it, "+
			"10m to 24h")
	set.StringVar(&raw.history, "history", stateOff,
		"keep the lost tracks in this file across a restart, live sources only; off keeps nothing")
}

// bindCamera declares the 3D views' flags: the camera's placement and the
// sky it looks at, and the zoom that frames the picture. They are split from
// bind to keep it under funlen.
func bindCamera(set *flag.FlagSet, raw *rawFlags) {
	set.Float64Var(&raw.exaggerate, "exaggerate", radar.DefaultExaggerate,
		"how far the 3d view stretches altitude into height, 1 to 20")
	set.IntVar(&raw.zoom, "zoom", 0,
		"range increments inside the auto fit, -5 to 5, negative widens; the view still follows the traffic. "+
			"Ignored with --range")
	set.Float64Var(&raw.tilt, "tilt", radar.DefaultTilt,
		"the 3d camera's starting elevation in degrees, 10 to 80. [ and ] step from it while it runs")
	set.StringVar(&raw.orbit, "orbit", defaultOn,
		"whether the 3d camera turns on its own: on or off. o flips it while it runs")
	set.Float64Var(&raw.azimuth, "azimuth", 0,
		"where the 3d camera stands, in degrees clockwise from north, 0 up to 360; "+
			"the start of the orbit, or where it stays with --orbit off")
	set.StringVar(&raw.stars, "stars", defaultOn,
		"draw stars in the night sky of the 3d views: on or off. z flips it while it runs")
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

	colouring, err := raw.colouring()
	if err != nil {
		return display{}, err
	}

	overlays, err := raw.overlays()
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

	camera, err := raw.camera()
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
		look:       look,
		radar: radar.Settings{
			Colour:   colouring.Colour,
			Filter:   colouring.Filter,
			Airports: overlays.Airports,
			Shore:    overlays.Shore,
			RangeNm:  rangeNm,
			Recentre: recentre,

			MinimalShore:    overlays.MinimalShore,
			MinimalAirports: overlays.MinimalAirports,
			MinimalBar:      overlays.MinimalBar,
			Board:           overlays.Board,
			Lights:          overlays.Lights,
			Stars:           overlays.Stars,
			Corridors:       overlays.Corridors,

			View:       view,
			Exaggerate: camera.Exaggerate,
			Zoom:       camera.Zoom,
			Tilt:       camera.Tilt,
			Orbit:      camera.Orbit,
			Azimuth:    camera.Azimuth,
		},
	}, nil
}

// colouring reads --colour and --filter, the two flags that say what the
// aircraft are painted by and which of them are drawn. They are one helper so
// display stays under the complexity cap.
func (raw rawFlags) colouring() (radar.Settings, error) {
	colour, err := parseColour(raw.colour)
	if err != nil {
		return radar.Settings{}, err
	}

	filter, err := radar.ParseFilter(raw.filter)
	if err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errFilter, err)
	}

	return radar.Settings{Colour: colour, Filter: filter}, nil
}

// camera checks the flags that place the 3D camera and frame the picture,
// rather than name a choice, and hands them back as the part of a settings
// block they fill. They are one helper so display stays under the complexity
// cap.
//
// The bounds are radar's own Check functions, so the flag and the scene cannot
// disagree about what a tilt of 85 means. Refused rather than clamped, unlike
// the bracket keys, because there is somebody reading. The float flags keep
// their parsed value rather than a zero for "unset": each default is a real
// setting, so the scene's zero-means-default rule never fires from the
// command line.
func (raw rawFlags) camera() (radar.Settings, error) {
	if err := radar.CheckExaggerate(raw.exaggerate); err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errExaggerate, err)
	}

	if err := radar.CheckZoom(raw.zoom); err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errZoom, err)
	}

	if err := radar.CheckTilt(raw.tilt); err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errTilt, err)
	}

	if err := radar.CheckAzimuth(raw.azimuth); err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errAzimuth, err)
	}

	orbit, err := radar.ParseToggle(raw.orbit)
	if err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errOrbit, err)
	}

	return radar.Settings{
		Exaggerate: raw.exaggerate, Zoom: raw.zoom, Tilt: raw.tilt, Orbit: orbit, Azimuth: raw.azimuth,
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

	if err := checkBattery(raw.battery); err != nil {
		return err
	}

	return checkKept(raw.history, errHistPath)
}

// checkKept refuses a --history value that cannot be a file to keep: -,
// which reads like stdout, and a path with a NUL in it. off is the default,
// and empty means off too. invalid is the flag's own sentinel.
func checkKept(path string, invalid error) error {
	if path == stdoutPath || strings.ContainsRune(path, 0) {
		return fmt.Errorf("%w: got %q", invalid, path)
	}

	return nil
}

// statePath is the --history file, or empty for off.
func statePath(text string) string {
	if text == stateOff {
		return ""
	}

	return text
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

	field, halfLife, err := raw.corridorSource()
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
		filter:      show.radar.Filter,
		airports:    show.radar.Airports,
		shore:       show.radar.Shore,

		minimalShore:    show.radar.MinimalShore,
		minimalAirports: show.radar.MinimalAirports,
		minimalBar:      show.radar.MinimalBar,
		minimalBoard:    show.radar.Board,
		lights:          show.radar.Lights,
		stars:           show.radar.Stars,
		corridors:       show.radar.Corridors,
		rangeNm:         show.radar.RangeNm,
		recentre:        show.radar.Recentre,
		view:            show.radar.View,
		exaggerate:      show.radar.Exaggerate,
		zoom:            show.radar.Zoom,
		tilt:            show.radar.Tilt,
		orbit:           show.radar.Orbit,
		azimuth:         show.radar.Azimuth,

		battery:     batteryPath(raw.battery),
		noBattery:   raw.battery == batteryOff,
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

		corridorField:    field,
		corridorHalfLife: halfLife,
		history:          statePath(raw.history),
	}, nil
}

// corridorSource reads the two corridor flags that go to the source rather
// than to the scene: which airfield, and how fast the tracks fade.
//
// The ICAO code is upper-cased first, because airport codes are written both
// ways and the list stores them in capitals, and then held to four letters.
// That is the shape of every code in uAirwaves' list, so anything else is a
// typo the operator should hear about now rather than as an unknown airfield
// from the source a moment later.
func (raw rawFlags) corridorSource() (string, time.Duration, error) {
	field := strings.ToUpper(raw.corrField)
	if field != "" && !icaoCode(field) {
		return "", 0, fmt.Errorf("%w: got %q", errCorrField, raw.corrField)
	}

	halfLife, err := source.ParseCorridorHalfLife(raw.halfLife)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %w", errHalfLife, err)
	}

	return field, halfLife, nil
}

// icaoCode reports whether text is four ASCII capitals.
func icaoCode(text string) bool {
	if len(text) != icaoLength {
		return false
	}

	for index := range len(text) {
		if text[index] < 'A' || text[index] > 'Z' {
			return false
		}
	}

	return true
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

// overlays parses the nine on/off switches, the scope's pair, the bare
// views' pair, bar and board, the aircraft lights, the stars and the
// corridors, into the Settings fields they fill. They are one helper so display stays a flat list of
// parses rather than growing past funlen.
func (raw rawFlags) overlays() (radar.Settings, error) {
	airports, err := parseAirports(raw.airports)
	if err != nil {
		return radar.Settings{}, err
	}

	shoreToggle, err := parseShore(raw.shore)
	if err != nil {
		return radar.Settings{}, err
	}

	minimal, err := raw.minimalSwitches()
	if err != nil {
		return radar.Settings{}, err
	}

	lights, err := parseLights(raw.lights)
	if err != nil {
		return radar.Settings{}, err
	}

	stars, err := radar.ParseToggle(raw.stars)
	if err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errStars, err)
	}

	corridors, err := radar.ParseToggle(raw.corridors)
	if err != nil {
		return radar.Settings{}, fmt.Errorf("%w: %w", errCorridors, err)
	}

	minimal.Airports, minimal.Shore, minimal.Lights, minimal.Stars = airports, shoreToggle, lights, stars
	minimal.Corridors = corridors

	return minimal, nil
}

// minimalSwitches reads the bare views' four switches into the Settings
// fields they fill.
func (raw rawFlags) minimalSwitches() (radar.Settings, error) {
	minShore, err := parseMinimal(raw.minShore, errMinShore)
	if err != nil {
		return radar.Settings{}, err
	}

	minAirports, err := parseMinimal(raw.minAirports, errMinAirport)
	if err != nil {
		return radar.Settings{}, err
	}

	minBar, err := parseMinimal(raw.minBar, errMinBar)
	if err != nil {
		return radar.Settings{}, err
	}

	minBoard, err := parseMinimal(raw.minBoard, errMinBoard)
	if err != nil {
		return radar.Settings{}, err
	}

	return radar.Settings{
		MinimalShore: minShore, MinimalAirports: minAirports, MinimalBar: minBar, Board: minBoard,
	}, nil
}

// parseLights reads --lights against internal/radar's allow list.
func parseLights(text string) (radar.LightsMode, error) {
	mode, err := radar.ParseLights(text)
	if err != nil {
		return radar.LightsOn, fmt.Errorf("%w: %w", errLights, err)
	}

	return mode, nil
}

// parseMinimal reads one of the bare views' switches against the
// same on/off allow list --shore uses, wrapping a refusal in the flag's own
// sentinel. It answers a bool because the scene's own default for each is off
// and a Toggle's zero reads as on; see radar.Settings.MinimalShore.
func parseMinimal(text string, sentinel error) (bool, error) {
	toggle, err := radar.ParseToggle(text)
	if err != nil {
		return false, fmt.Errorf("%w: %w", sentinel, err)
	}

	return toggle.On(), nil
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

// batteryPath is the file --battery names, or empty for off, so the poller is
// never handed the word "off" as a path.
func batteryPath(text string) string {
	if text == batteryOff {
		return ""
	}

	return text
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
