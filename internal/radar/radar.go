// Package radar draws the scope.
//
// It is the scene DESIGN.md is the contract for: a header band, a square
// scope on the left with range rings and one thin trail per aircraft, a right
// column holding the selected-flight card, the compact rows, the legend and
// the stats, and a key bar along the bottom.
//
// The aircraft arrive through internal/source, which is the only thing that
// knows whether they came off a radio or were invented. Everything here works
// on a source.Frame and would not notice the difference.
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

// Scene is the radar.
type Scene struct {
	faces      Faces
	pal        theme.Palette
	src        source.Source
	scopeRange *scope.Scope
	icon       *sprite.Bitmap
	now        func() time.Time

	// trails and autoRange are what the t and a keys toggle.
	trails    bool
	autoRange bool

	// The selection is keyed by ICAO so it survives the list being re-sorted
	// when an aircraft overtakes another. selIndex and icaos are what the
	// n and p keys step through, refreshed from the list every frame.
	selICAO  string
	selIndex int
	icaos    []string

	// rowStart is the first compact row on screen. The window follows the
	// selection so the selected aircraft is always one of the rows.
	rowStart int

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
	return func(s *Scene) { s.pal = pal }
}

// SetPalette replaces the colours on a scene that is already built. This is
// what the l key uses to cycle the theme at run time: internal/app calls it
// on every scene that implements it, not only the one on screen, so
// switching scenes later still shows the theme that was chosen.
func (s *Scene) SetPalette(pal theme.Palette) { s.pal = pal }

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
// Trails and auto range both start on, which is the state the scope is most
// useful in when nobody has touched a key yet.
func New(faces Faces, src source.Source, scopeRange *scope.Scope, opts ...Option) *Scene {
	scene := &Scene{
		faces:      faces,
		pal:        theme.Night,
		src:        src,
		scopeRange: scopeRange,
		icon:       sprite.Airplane(),
		now:        time.Now,
		trails:     true,
		autoRange:  true,
		selIndex:   -1,
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

// Draw paints one frame. elapsed is unused: the scene is driven by the data
// and the clock in the frame, not by how long the program has been running.
func (s *Scene) Draw(dst *canvas.Canvas, _ time.Duration) {
	dst.Clear(s.pal.Field)

	frame := s.src.Frame()
	if frame.Now.IsZero() {
		frame.Now = s.now()
	}

	s.syncSelection(frame)
	s.fitRange(frame)

	// The layout is a local whose address is handed down rather than a value
	// returned by pointer, because a pointer returned from newLayout escapes
	// to the heap and that is the one allocation a frame would otherwise make.
	lay := s.newLayout(dst)

	s.drawKeyBar(&lay)
	s.drawHeader(&lay, frame)
	lay.split()
	s.drawScope(&lay, frame)
	s.drawColumn(&lay, frame)
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
