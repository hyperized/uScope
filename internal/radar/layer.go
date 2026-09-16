package radar

import (
	"math"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The layer key's tolerances.
const (
	// fixPrecision is the grid the receiver position is snapped to before it
	// goes into the key: 1e-4 degrees, about eleven metres.
	//
	// A self-locate estimate moves in its last decimal on every frame. Without
	// the rounding the layer would be redrawn for eleven metres of imaginary
	// movement, which is the whole cost the layer exists to avoid.
	fixPrecision = 1e4

	// maxDegrees bounds a coordinate before it is converted to an integer.
	// Latitude and longitude both fit inside it with room to spare.
	maxDegrees = 360
)

// layerKey is everything the background layer's picture depends on.
//
// It is compared whole with ==, so every field has to be comparable and the
// list has to be complete: a setting that changes the picture and is missing
// here would leave a stale layer on screen until something else moved.
type layerKey struct {
	width    int
	height   int
	rangeNm  float64
	lat      int64
	lon      int64
	palette  theme.Palette
	shore    bool
	airports bool
	fix      source.FixMode

	// auto is in the key because the outer ring's label opens with AUTO while
	// auto range is on. Toggling r on a still scope changes no other field, so
	// without this the word would appear only once something else moved.
	auto bool

	// view is in the key because v changes the whole picture behind the
	// aircraft: the same canvas size, range and palette draw rings and a home
	// marker in one view and two overlays around a different centre in the
	// other. The 3D view never builds a key at all, for the reason layerless
	// gives.
	view View

	// centreLat and centreLon are minimal mode's own projection centre,
	// snapped onto the same grid the receiver's position is.
	//
	// The centre moves on its own, without a key being pressed, which is what
	// makes it the one field here that has to be read every frame. A glide
	// does rebuild the layer once per frame for its two seconds, and there is
	// nothing to be done about that: the shore genuinely is somewhere else on
	// each of those frames.
	centreLat int64
	centreLon int64
}

// paintBackground puts the background layer under the frame, redrawing it
// first if anything it depends on has moved.
//
// The copy is one memmove over the pixel slice. Redrawing the rings, the
// labels, the airports and a few thousand shore segments instead would be the
// most expensive thing in the frame, and none of it changes between two frames
// that share a key.
func (s *Scene) paintBackground(dst *canvas.Canvas, frame source.Frame) {
	if s.layerless() {
		s.layer = nil
		dst.Clear(s.pal.Field)

		return
	}

	key := s.layerKeyFor(dst, frame)
	if s.layer == nil || s.layerKey != key {
		s.renderLayer(dst, key, frame)
	}

	copy(dst.Image().Pix, s.layer.Image().Pix)
}

// layerless reports whether the view on screen draws straight into the frame
// rather than over a cached background.
//
// Two of the three do, for two different reasons. Minimal with both its
// overlays off has nothing behind the aircraft but the field, so holding a
// second canvas the size of the first to keep one colour in would be waste;
// turn either overlay on and it wants the layer like any other view, because a
// few thousand shore segments are not something to draw thirty times a second.
// The 3D view has plenty of furniture and can cache none of it: the camera
// moves on every frame the orbit is running, so a layer would be rebuilt on
// each one and cost the same drawing plus a copy of the whole canvas on top.
func (s *Scene) layerless() bool {
	if s.perspective() {
		return true
	}

	return s.minimal() && !s.shoreDrawn() && !s.airportsDrawn()
}

// layerKeyFor reads the current state of everything the layer depends on.
func (s *Scene) layerKeyFor(dst *canvas.Canvas, frame source.Frame) layerKey {
	bounds := dst.Bounds()

	return layerKey{
		width:    bounds.Dx(),
		height:   bounds.Dy(),
		rangeNm:  s.scopeRange.GetCurrent(),
		lat:      snap(frame.Receiver.Latitude),
		lon:      snap(frame.Receiver.Longitude),
		palette:  s.pal,
		shore:    s.shoreDrawn(),
		airports: s.airportsDrawn(),
		fix:      frame.Receiver.Mode,
		auto:     s.autoRange,

		view:      s.shown,
		centreLat: snap(s.centre.lat),
		centreLon: snap(s.centre.lon),
	}
}

// renderLayer draws the background afresh.
//
// It carves the frame exactly the way Draw does, taking the key bar off the
// bottom and the header off the top before splitting what is left, because the
// rings it draws have to land under the aircraft Draw puts on top of them. The
// two measure rather than share a layout, so neither can be handed one the
// other has already spent.
func (s *Scene) renderLayer(dst *canvas.Canvas, key layerKey, frame source.Frame) {
	bounds := dst.Bounds()
	if s.layer == nil || s.layer.Bounds() != bounds {
		// A canvas the size of another canvas cannot fail. canvas.New refuses
		// only a dimension of zero or less, and dst came from that same
		// constructor, so its bounds are positive by construction.
		s.layer, _ = canvas.New(bounds.Dx(), bounds.Dy()) //nolint:errcheck // see above.
	}

	s.layer.Clear(s.pal.Field)

	lay := s.newLayout(s.layer)

	// Minimal takes the whole canvas, so there is no key bar or header to
	// make room for and nothing to split off for a column. measureScope
	// ignores lay.scope in that mode anyway; skipping the carving keeps it
	// from being measured twice for an answer nothing reads.
	if !s.minimal() {
		lay.bottom -= s.keyBarHeight(&lay)
		lay.top += s.headerHeight(&lay)
		lay.split()
	}

	s.drawField(&lay, frame)

	s.layerKey = key
	s.layerRuns++
}

// snap rounds a coordinate onto the grid the layer key compares on.
//
// A NaN folds to zero rather than being converted. Converting a NaN to an
// integer is undefined in the spec, and a key field that compared unequal to
// itself would rebuild the layer on every frame for as long as the bad
// coordinate lasted.
func snap(degrees float64) int64 {
	if math.IsNaN(degrees) {
		return 0
	}

	return int64(math.Round(min(max(degrees, -maxDegrees), maxDegrees) * fixPrecision))
}
