// Package radar draws the scope.
//
// It is the scene DESIGN.md is the contract for: a header band, a square
// scope on the left with range rings and one thin trail per aircraft, a right
// column holding the selected-flight panel, the compact rows and the legend,
// and a key bar along the bottom.
//
// Aircraft are coloured by altitude band or by operator, which is what the c
// key and --colour pick between. Airline colours come from pkg/airlines and
// are adapted to whichever field the palette draws on.
//
// The aircraft arrive through internal/source, which is the only thing that
// knows whether they came off a radio or were invented. Everything here works
// on a source.Frame and would not notice the difference.
//
// The scope's furniture is drawn on a background layer of its own and copied
// under each frame, so the rings, the labels, the airfields and the coastline
// cost one memmove per frame instead of being drawn again. The layer is
// redrawn when the canvas size, the range, the receiver position, the palette
// or one of the overlay toggles changes, and not otherwise.
//
// Minimal mode is the aircraft and their trails on the bare field, edge to
// edge, with no header, no key bar, no column and no furniture at all. The
// keys keep working: the airfield and shore toggles still change state while
// it is on, they simply have nothing to draw.
//
// Every size comes from the canvas bounds and the metrics of the face it is
// set in. A block that does not fit is dropped rather than drawn over its
// neighbour, so the same scene renders at 1280x720 on the panel and on a
// canvas of a few dozen pixels. Below 640 pixels wide the right column goes;
// below 320 pixels tall the labels go.
//
// A Scene carries the selection, the range mode and the trail toggle, so it
// is not safe for concurrent use. The run loop calls Draw and Handle from one
// goroutine, which is the contract.
package radar

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/sprite"
)

// Spacing, in pixels at the panel's resolution. These are the only fixed
// numbers in the scene; everything else comes from a font metric.
const (
	baseMargin    = 16
	marginDivisor = 8

	blockGap = 16
	rowLead  = 4

	// headerPadY is the air above and below the header band's tallest face,
	// and ruleHeight is the hairline under it.
	headerPadY = 6
	ruleHeight = 1

	// The key caps along the bottom: padding inside the filled box, the gap
	// from a cap to its own label, and the gap on to the next cap.
	capPadX  = 5
	capPadY  = 3
	capGap   = 6
	entryGap = 18

	// Letter spacing for the small all-caps labels. Terminus is tight at 6
	// pixels wide, and a tracked-out label reads as a heading rather than as
	// text someone forgot to finish.
	labelTracking  = 1
	headerTracking = 2
)

// The responsive thresholds. Below these the scene drops a whole block rather
// than shrinking it: a bitmap face has one design size, and scaling it down
// does not make small text, it makes unreadable text.
const (
	// minColumnWidth is the canvas width below which the right column goes
	// and the scope takes the whole frame.
	minColumnWidth = 640

	// minLabelHeight is the canvas height below which every small label goes:
	// the cardinal letters, the range numbers, the airport names.
	minLabelHeight = 320
)

// The altitude bands, in feet. An aircraft is coloured by how high it is,
// which is the one thing a top-down scope cannot show by position.
const (
	lowCeiling = 10000.0
	midCeiling = 25000.0
)

// opaque is the alpha every colour the scene produces carries. The
// framebuffer has no alpha channel, so a translucent pixel would be flattened
// the moment it left the canvas.
const opaque = 0xFF

// Faces are the four console fonts the scene sets type in.
//
// A nil face is not an error: the block that would have used it skips itself,
// the same way a block that does not fit does. That is what lets the scene
// survive an 80x24 terminal of half blocks.
type Faces struct {
	Small    *psf.Font
	Body     *psf.Font
	BodyBold *psf.Font
	Large    *psf.Font
}

// BatteryReader is the part of a battery the header draws.
//
// It is two methods rather than uAirwaves' whole *battery.Status because that
// is all the scene reads, and because a test then hands over a struct of its
// own instead of a live poller. It is declared here, where it is consumed, for
// the same reason app.KeyHandler is declared in internal/app.
//
// An implementation is read on the draw path, so it has to be safe to call
// from the drawing goroutine while whatever fills it runs on another.
// uAirwaves' Status takes a mutex and satisfies that.
type BatteryReader interface {
	// GetPercentage is the charge left, 0 to 100, or negative when nothing has
	// been read yet.
	GetPercentage() int8

	// IsCharging reports whether the machine is on external power.
	IsCharging() bool
}

// Toggler is what the b key asks to flip the dongle's bias-tee.
//
// It is one method and it returns nothing, which is the whole design. Setting
// the bias-tee is a USB control transfer, and a dongle wedged by an unplug or
// a bus reset mid-write can take seconds to answer. Doing that on the
// goroutine that draws would freeze the scope; uAirwaves learned the same
// thing on its event loop and put the flip behind a worker there too.
//
// So an implementation returns at once and does the work somewhere else. It
// is also where the in-flight guard and the failure report belong: the scene
// has nothing useful to do with either, and a key that reports an error it
// cannot act on is a key that stutters.
//
// It is declared here, where it is consumed, for the same reason
// BatteryReader is.
type Toggler interface {
	// Toggle asks for the bias-tee to flip to whatever it is not. It must not
	// block: it is called from the key handler, between two frames.
	Toggle()
}

// Scene is the radar.
type Scene struct {
	faces      Faces
	pal        theme.Palette
	src        source.Source
	scopeRange *scope.Scope
	icon       *sprite.Bitmap
	now        func() time.Time

	// trail is how much of each aircraft's track is drawn, which the t key
	// cycles, and autoRange is what the r key toggles.
	trail     trailMode
	autoRange bool

	// colour is what an aircraft's colour means, which the c key cycles.
	colour ColourMode

	// airports is whether the airfield markers are drawn, which the a key
	// toggles.
	airports bool

	// shoreOn is whether the coastline is drawn, which the m key toggles, and
	// shoreSet is the data behind it. A nil set draws nothing whatever the
	// toggle says, which is the state on a run that was never handed the data.
	shoreOn  bool
	shoreSet *shore.Set

	// shown is which of the three views is on screen, which the v key cycles
	// and --view picks the start of. The minimal and perspective helpers in
	// view.go are how everything else asks.
	shown View

	// minimalShore and minimalAirports are minimal mode's own copies of the
	// two overlay toggles, and they are what m and a flip while it is on.
	//
	// Both start off, so minimal opens on a bare field, which is what it is
	// for. They are separate from shoreOn and airports rather than shared
	// because the two views want opposite defaults: the scope is a map with
	// aircraft on it and minimal is aircraft with nothing behind them, so a
	// shared pair would mean every trip into minimal started by turning two
	// things off and every trip back started by turning them on again.
	minimalShore    bool
	minimalAirports bool

	// Minimal mode's own projection centre and the glide that moves it, all
	// of it inert while recentre is zero. recentre is the cadence --recenter
	// asked for. centre is where minimal mode projects from on this frame;
	// glideFrom and glideTo are the ends of the move in progress and glideAt
	// the elapsed reading it started at. lastFit is the frame clock the last
	// centring happened on, which is what the cadence is measured against,
	// and haveCentre separates "nothing chosen yet" from "centred on the
	// equator".
	//
	// follow.go works all of it out. It lives on the Scene rather than in a
	// struct of its own because the draw path has to reach it without an
	// allocation, and because it is state of the same kind as the toggles
	// above it.
	recentre   time.Duration
	centre     geo
	glideFrom  geo
	glideTo    geo
	glideAt    time.Duration
	gliding    bool
	haveCentre bool
	lastFit    time.Time

	// The background layer: everything on the scope that does not move
	// between frames, kept on a canvas of its own and copied under each frame
	// rather than drawn again. layerRuns counts how many times it has been
	// drawn, which is what lets a test prove it is not once per frame.
	layer     *canvas.Canvas
	layerKey  layerKey
	layerRuns int

	// clip is the 3D view's own window onto the frame: a canvas sharing the
	// frame's pixels but bounded by the scope box, so a bowl taller than the
	// box or an envelope wider than the range is cut at the edge instead of
	// being drawn across the column beside it. clipOf and clipBox are what it
	// was built for, so it is rebuilt when the canvas or the box moves and not
	// once per frame.
	clip    *canvas.Canvas
	clipOf  *canvas.Canvas
	clipBox image.Rectangle

	// posed is the 3D view's scratch for one aircraft's model: where each of
	// its vertices landed on the canvas and which way round it is to the
	// camera. It is a field for the reason the format buffers below are, and
	// one is enough because an aircraft is projected, drawn and done with
	// before the next one is started.
	posed posed

	// light is whether the palette draws on a light field. It is kept beside
	// the palette rather than worked out per aircraft because an airline's
	// colour is adapted once per draw call and the answer cannot change
	// between two of them.
	light bool

	// The 3D view's camera. azimuth and elevation are in degrees, and
	// azimuth is where the last keypress left it rather than where the camera
	// is pointing now: orbiting winds it forward from azimuthAt, which is the
	// render clock the current revolution started on. elapsed is the reading
	// the last drawn frame was given, and it is here because a key press
	// arrives between two frames and has to freeze the orbit where the picture
	// actually is.
	azimuth   float64
	elevation float64
	azimuthAt time.Duration
	elapsed   time.Duration
	orbiting  bool

	// envelope is whether the receiving envelope is drawn, which the e key
	// toggles. It starts on: the envelope is most of the reason the view
	// exists, and a 3D scope without it is a tilted scope.
	envelope bool

	// exaggerate is how far altitude is stretched into height, which
	// --exaggerate sets. See DefaultExaggerate for why it is not 1.
	exaggerate float64

	// counts is the legend's per-frame operator tally in airline mode. It is a
	// field rather than a local so the table it holds outlives the frame that
	// filled it and the draw path never allocates one.
	counts tally

	// rangeLabelRects and rangeLabelCount are where the background layer put
	// its range labels the last time it drew them, so the airport overlay
	// drawn right after it can skip a marker that would sit on top of one.
	// Both are reset and refilled at the start of every drawRings call rather
	// than grown into, which is what keeps a fixed array the right container
	// for them: ringCount never draws more than three, so three is all the
	// room they ever need.
	rangeLabelRects [ringCount]image.Rectangle
	rangeLabelCount int

	// battery is what the header's indicator reads, or nil on a machine with
	// no battery to read. Nil is the normal state on a desktop, so it is a
	// state rather than a failure and nothing is drawn for it.
	battery BatteryReader

	// biasTee is what the b key flips, or nil on a run nobody wired one to,
	// which is every run that is not driving the radio itself.
	biasTee Toggler

	// biasSupported and biasEnabled are the last frame's bias-tee, copied out
	// of it in Draw the way elapsed is.
	//
	// They are read off the frame rather than out of the source because the
	// key bar draws the cap and the key handler reads the same two bits, and
	// neither may reach through the seam: the answer would be a USB transfer
	// on the draw path in one case and on the key path in the other. The
	// source caches the pair and the frame carries the cache.
	//
	// A b press before the first frame therefore does nothing. That is the
	// right way round: until a frame has arrived nothing knows whether there
	// is a dongle to talk to.
	biasSupported bool
	biasEnabled   bool

	// The selection is keyed by ICAO so it survives the list being re-sorted
	// when an aircraft overtakes another. selIndex and icaos are what the
	// n and p keys step through, refreshed from the list every frame.
	selICAO  string
	selIndex int
	icaos    []string

	// rowStart is the first compact row on screen. The window follows the
	// selection so the selected aircraft is always one of the rows.
	rowStart int

	// pinned says the operator chose this aircraft. Until they do, the
	// selection follows the nearest contact rather than sticking to whichever
	// aeroplane happened to be first in the list when the program started.
	// n, p, Up and Down pin it; Esc lets go again.
	pinned bool

	// The scratch buffers every number in the scene is formatted into.
	// Reusing them is what keeps the draw path free of allocations; the note
	// at the top of format.go explains why they are byte arrays and not
	// strings. digits holds one plain number, grouped the same number with
	// thousands separators, coords one half of a position, and clock the
	// header's time, appended by time.AppendFormat because Time.Format
	// allocates a string on every frame.
	digits  [32]byte
	grouped [40]byte
	coords  [24]byte
	clock   [16]byte
}

// Option adjusts a Scene at construction.
type Option func(*Scene)

// WithPalette replaces the colours at construction.
func WithPalette(pal theme.Palette) Option {
	return func(s *Scene) { s.SetPalette(pal) }
}

// SetPalette replaces the colours on a scene that is already built. This is
// what the l key uses to cycle the theme at run time: internal/app calls it
// on every scene that implements it, not only the one on screen, so
// switching scenes later still shows the theme that was chosen.
//
// It takes the light-field flag off the palette rather than being told
// separately. One setter means the two can never disagree, and a palette that
// is not theme.Paper reads as dark, which is the rule theme.Kind already
// applies everywhere else.
func (s *Scene) SetPalette(pal theme.Palette) {
	s.pal = pal
	s.light = pal.Light()
}

// WithColour picks what an aircraft's colour means at construction.
func WithColour(mode ColourMode) Option {
	return func(s *Scene) { s.colour = mode }
}

// WithClock replaces the fallback clock. It is only consulted when the source
// hands back a frame with no timestamp on it, which a real source never does.
// A nil function leaves time.Now in place.
func WithClock(now func() time.Time) Option {
	return func(s *Scene) {
		if now != nil {
			s.now = now
		}
	}
}

// WithBattery wires the header's battery indicator to a reader.
//
// Without it the header has no indicator at all, which is the right answer on
// a machine with no battery: an empty glyph would say the battery is flat.
func WithBattery(reader BatteryReader) Option {
	return func(s *Scene) { s.battery = reader }
}

// WithBiasTee wires the b key to whatever flips the dongle's LNA power.
//
// Without it the key does nothing and the cap is never drawn, which is the
// right answer for a run with no radio behind it. The cap also stays away
// when a toggler is wired but the frame says the source does not support one:
// internal/app wires the same toggler whichever source was chosen, and the
// source is what knows whether there is a dongle.
func WithBiasTee(toggler Toggler) Option {
	return func(s *Scene) { s.biasTee = toggler }
}

// WithShore supplies the coastlines the scope draws under everything else.
//
// Without it, or with a nil set, no shore is drawn however the toggle and the
// flag are set. That is a run that was never handed the data rather than a
// failure, so there is nothing to report and nothing to refuse: the rest of
// the scope is unaffected.
func WithShore(set *shore.Set) Option {
	return func(s *Scene) { s.shoreSet = set }
}

// WithSprite replaces the aircraft silhouette. A nil bitmap leaves the
// built-in one alone.
func WithSprite(icon *sprite.Bitmap) Option {
	return func(s *Scene) {
		if icon != nil {
			s.icon = icon
		}
	}
}

// New builds the radar around its four faces, its data and its range control.
//
// Those three are parameters rather than options because a radar without them
// has nothing to draw; the options are the things that have a useful default.
// Auto range, the airfield markers and the shore all start on, the trails
// start on trailLong and the colour mode on altitude, which is the state the
// scope is most useful in when nobody has touched a key yet. The view starts on the scope: the
// other two are views to switch to, not ones to explain on first sight. The
// camera starts orbiting with its envelope drawn, because a 3D view arrived at
// by pressing v twice should be doing the thing it was added for.
func New(faces Faces, src source.Source, scopeRange *scope.Scope, opts ...Option) *Scene {
	scene := &Scene{
		faces:      faces,
		pal:        theme.Night,
		src:        src,
		scopeRange: scopeRange,
		icon:       sprite.Airplane(),
		now:        time.Now,
		trail:      trailLong,
		autoRange:  true,
		airports:   true,
		shoreOn:    true,
		colour:     ColourAltitude,
		selIndex:   -1,
		shown:      ViewScope,
		elevation:  defaultElevation,
		orbiting:   true,
		envelope:   true,
		exaggerate: DefaultExaggerate,
	}

	for _, opt := range opts {
		opt(scene)
	}

	return scene
}

// layout is the frame carved into blocks, worked out once per Draw.
//
// The key bar takes its room off the bottom and the header off the top,
// because both belong to an edge whatever else is on screen. What is left is
// split into the scope, which is square and as large as the height allows,
// and the column beside it.
type layout struct {
	dst    *canvas.Canvas
	scope  image.Rectangle
	column image.Rectangle
	left   int
	right  int
	top    int
	bottom int
	labels bool
}

// Draw paints one frame.
//
// Everything on screen comes from the data and the clock in the frame. The
// one thing elapsed is read for is minimal mode's glide, which is an
// animation rather than a reading: it has to advance once per drawn frame
// whatever the feed is doing, and the run loop's own clock is the only thing
// that measures that.
func (s *Scene) Draw(dst *canvas.Canvas, elapsed time.Duration) {
	frame := s.src.Frame()
	if frame.Now.IsZero() {
		frame.Now = s.now()
	}

	// Kept for the camera keys, which arrive between two frames and have to
	// know where the orbit had got to when the last one was drawn.
	s.elapsed = elapsed

	// Same reason: the b key arrives between two frames and decides what to
	// ask for from the state the last one carried.
	s.biasSupported, s.biasEnabled = frame.BiasTee.Supported, frame.BiasTee.Enabled

	s.syncSelection(frame)

	// Minimal mode following the traffic fits its own range, around the
	// centroid and at the cadence rather than around the receiver and on
	// every frame. Running both would have the two pull against each other
	// once a frame, which is exactly the breathing the cadence is for.
	if s.following() {
		s.follow(frame, elapsed)
	} else {
		s.fitRange(frame)
	}

	// The field, the rings, the labels, the home marker, the airports and the
	// shore all come from the background layer, which is redrawn only when
	// something it depends on moves. Everything below is what changes.
	s.paintBackground(dst, frame)

	// The layout is a local whose address is handed down rather than a value
	// returned by pointer, because a pointer returned from newLayout escapes
	// to the heap and that is the one allocation a frame would otherwise make.
	lay := s.newLayout(dst)

	// Minimal is the aircraft and nothing else, edge to edge. It skips the
	// three blocks that would take room off the canvas, so the traffic gets
	// the whole frame rather than the square the column left behind.
	if s.minimal() {
		s.drawTraffic(&lay, frame)

		return
	}

	s.drawKeyBar(&lay)
	s.drawHeader(&lay, frame)
	lay.split()
	s.drawScope(&lay, frame, elapsed)
	s.drawColumn(&lay, frame)
}

// drawScope paints whatever the box beside the column holds in the view on
// screen: the flat scope, or the perspective one.
//
// The header, the column and the key bar are the same furniture either way,
// which is the whole reason the 3D view slots in here rather than taking the
// canvas the way minimal does. Only the picture changes.
func (s *Scene) drawScope(lay *layout, frame source.Frame, elapsed time.Duration) {
	if s.perspective() {
		s.draw3D(lay, frame, elapsed)

		return
	}

	s.drawTraffic(lay, frame)
}

// newLayout measures the frame and takes the margin out of it.
//
// The margin is an eighth of the short edge up to 16 pixels, so a small canvas
// is not all border. Taking an eighth also means two margins can never take
// more than a quarter of the width, so the box left over is always at least a
// pixel wide and there is nothing to guard against.
func (*Scene) newLayout(dst *canvas.Canvas) layout {
	bounds := dst.Bounds()
	margin := min(baseMargin, min(bounds.Dx(), bounds.Dy())/marginDivisor)

	return layout{
		dst:    dst,
		left:   bounds.Min.X + margin,
		right:  bounds.Max.X - margin,
		top:    bounds.Min.Y + margin,
		bottom: bounds.Max.Y - margin,
		labels: bounds.Dy() >= minLabelHeight,
	}
}

// split divides what the header and the key bar left into the scope square
// and the column beside it.
//
// The scope is square because a range ring has to be a circle; a scope
// stretched to fill an oblong box would put the same number of nautical miles
// at different pixel distances depending on the bearing.
func (l *layout) split() {
	width, height := l.right-l.left, l.bottom-l.top
	if width <= 0 || height <= 0 {
		return
	}

	side := min(width, height)

	// Narrow canvases lose the column. Two half-width blocks side by side are
	// worse than one that works. With nothing beside it the scope is centred
	// in what is left, because a square pinned to the left edge of a wide box
	// reads as a mistake.
	if l.dst.Bounds().Dx() < minColumnWidth {
		left := l.left + (width-side)/2
		l.scope = image.Rect(left, l.top, left+side, l.top+side)

		return
	}

	l.scope = image.Rect(l.left, l.top, l.left+side, l.top+side)
	l.column = image.Rect(l.scope.Max.X+blockGap, l.top, l.right, l.bottom)

	if l.column.Dx() <= 0 {
		l.column = image.Rectangle{}
	}
}

// fits reports whether a block of this height still has room at the top.
func (l *layout) fits(height int) bool { return l.top+height <= l.bottom }

// lineHeight is how tall one line of a face is, or zero when the face is
// missing, which is how a block decides it has nothing to draw with.
func lineHeight(face *psf.Font) int {
	if face == nil {
		return 0
	}

	return face.Height()
}

// glyphWidth is how wide one glyph of a face is, or zero when the face is
// missing. The compact rows size their columns from it.
func glyphWidth(face *psf.Font) int {
	if face == nil {
		return 0
	}

	return face.Width()
}

// bandColour picks an aircraft's colour from its altitude.
//
// An altitude of zero means nobody has decoded one yet, not sea level: a
// Snapshot starts at zero and stays there until an altitude message arrives.
// Those are drawn muted so they read as "unknown" rather than as "very low".
func (s *Scene) bandColour(altitude float64) color.RGBA {
	switch {
	case altitude <= 0:
		return s.pal.Muted
	case altitude < lowCeiling:
		return s.pal.AltLow
	case altitude < midCeiling:
		return s.pal.AltMid
	default:
		return s.pal.AltHigh
	}
}

// fade mixes a colour towards the field by alpha, where 1 is the colour
// itself and 0 is the field.
//
// Trails fade this way rather than through the canvas alpha channel because
// the scope is a near-uniform field: mixing once per segment and drawing the
// result solid gives the same picture as compositing every pixel, and costs
// one blend instead of hundreds.
func (s *Scene) fade(col color.RGBA, alpha float64) color.RGBA {
	return color.RGBA{
		R: mix(s.pal.Field.R, col.R, alpha),
		G: mix(s.pal.Field.G, col.G, alpha),
		B: mix(s.pal.Field.B, col.B, alpha),
		A: opaque,
	}
}

// mix interpolates one channel between the field and the ink, where an alpha
// of 1 is all ink and 0 is all field.
//
// The clamp lives here rather than in fade so mix is safe whatever it is
// handed. NaN is folded to the field explicitly rather than left to the
// clamp, because min and max propagate a NaN instead of pinning it, and
// converting a NaN to uint8 is undefined in the spec: one bad alpha would
// otherwise produce whatever the compiler felt like that day. Once alpha is
// in range the result cannot leave the byte, so there is nothing to clamp on
// the way out.
func mix(field, ink uint8, alpha float64) uint8 {
	if math.IsNaN(alpha) {
		return field
	}

	clamped := min(max(alpha, 0), 1)

	return uint8(math.Round(float64(field) + (float64(ink)-float64(field))*clamped)) //nolint:gosec // in range.
}
