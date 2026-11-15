package radar

import (
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The corridor exposure's measurements.
const (
	// corridorTintMax is how far a track lost a moment ago is pulled off the
	// field. A quarter is about three times the water tint, which is what it
	// takes for an approach to read over the sea while the live traffic
	// still stands out above two hours of old tracks.
	corridorTintMax = 0.25

	// corridorMinStrength is the faintest track worth drawing. Under it the
	// colour rounds to the field or one step off it, so a track a little over
	// four half-lives old is skipped rather than drawn invisibly.
	corridorMinStrength = 0.015

	// exposureTick is how coarsely the fade is keyed on the flat views'
	// background layer. Once a minute is often enough for a fade measured in
	// hours, and the layer is rebuilt that often rather than every frame.
	exposureTick = time.Minute
)

// bandMask is a set of altitude bands, one bit per band at the band's own
// index.
type bandMask uint8

// allBands is every band, which is what an unfiltered scope shows.
const allBands bandMask = 1<<bandCount - 1

// has reports whether the mask includes a band. bandUnknown is in no mask,
// which is what keeps a stretch of track at an undecoded altitude off a
// filtered picture and an unfiltered one alike.
func (m bandMask) has(band int) bool {
	return band >= 0 && band < bandCount && m&(1<<band) != 0
}

// exposure is what the last frame carried for the corridor exposure, copied
// off it in Draw so the layer, which is drawn from Scene state rather than
// from the frame, sees the same tracks the frame did.
//
// The trails alias the source's own slice, which holds still until the next
// Frame call on this goroutine, so copying the header is enough.
type exposure struct {
	trails          []source.Trail
	generation      uint64
	routes          []source.Route
	routeGeneration uint64
	corridors       source.Corridors
	now             time.Time
}

// exposureDrawn reports whether there is an exposure on screen: the setting
// is on, the source has an airfield and a half-life, and it has lost at least
// one track. Before the first track is lost there is nothing to draw, and
// minimal mode gets to stay layerless until there is.
func (s *Scene) exposureDrawn() bool {
	return s.corridors && s.exposure.corridors.Placed() && s.exposure.corridors.HalfLife > 0 &&
		len(s.exposure.trails) > 0
}

// corridorBands is the altitude bands the exposure draws, which are the bands
// the filter lets aircraft through in.
//
// It walks the filter's own value rather than the colour mode, so it answers
// what visible answers: a band keeps itself and anything else keeps them all.
// The operator values are airline mode's and land in the last case. A lost
// track carries a callsign, but a filter on one airline's live traffic that
// also thinned two hours of its history would be answering a question nobody
// asked of the corridors.
func (s *Scene) corridorBands() bandMask {
	switch s.filter.kind {
	case filterBand:
		return bandMask(1) << s.filter.band
	case filterAll, filterOperator, filterOther:
		fallthrough
	default:
		return allBands
	}
}

// exposureKey is the part of the background layer's key the corridors decide:
// the ring's generation, the routes' generation, the fade tick and the bands.
// All four are zero while nothing is drawn, so a lost track, a minute passing
// or a press of f with the corridors off rebuilds nothing.
func (s *Scene) exposureKey() exposureKey {
	if !s.exposureDrawn() {
		return exposureKey{}
	}

	return exposureKey{
		generation: s.exposure.generation,
		routes:     s.exposure.routeGeneration,
		tick:       s.exposure.now.Truncate(exposureTick).Unix(),
		bands:      s.corridorBands(),
	}
}

// exposureKey is what the corridors add to the background layer's key. The
// ring's generation moves when a track is lost or ages out, the routes'
// whenever they are recomputed, which is that or a minute passing, and the
// tick once a minute for the hairlines' own fade. Without the bands a press
// of f would leave the last filter's corridors on screen until the next track
// was lost.
type exposureKey struct {
	generation uint64
	routes     uint64
	tick       int64
	bands      bandMask
}

// classColour is the colour a class of track is drawn in before it fades.
// Arrivals take the data colour and departures the amber, so the two
// directions of one runway read apart; transit takes the quiet grey, because
// an airway overhead is context for the corridors rather than one of them.
func (s *Scene) classColour(class source.TrackClass) color.RGBA {
	switch class {
	case source.TrackInbound:
		return s.pal.Data
	case source.TrackOutbound:
		return s.pal.Caution
	case source.TrackTransit:
		fallthrough
	default:
		return s.pal.Muted
	}
}

// exposureInk is the colour one track is drawn in, or false when it has faded
// past drawing.
//
// The strength halves every half-life from the moment the track was lost.
// Fading towards the field rather than blending over what is under it is the
// trails' trade again: the ground is near uniform, and one mix per track
// costs nothing per pixel. A track lost after the frame's clock, which a
// clock stepped backwards produces, is drawn at full strength rather than
// stronger than that.
func (s *Scene) exposureInk(trail *source.Trail) (color.RGBA, bool) {
	age := max(s.exposure.now.Sub(trail.LostAt), 0)
	strength := corridorTintMax * math.Exp2(-age.Seconds()/s.exposure.corridors.HalfLife.Seconds())

	if strength < corridorMinStrength {
		return color.RGBA{}, false
	}

	return s.fade(s.classColour(trail.Class), strength), true
}

// drawExposure draws the corridors on the flat views' ground: every route as
// a tube, then every lost track as a hairline wherever no tube covers it,
// oldest first, so the newest and strongest lie on top. A member of a route
// still has its hairline past the end of the tube, where its route fanned
// out or thinned and the source stopped drawing one.
//
// It runs on the background layer, between the water and land fill and the
// coastline, so the coast stays legible over a corridor that crosses it. The
// layer is rebuilt when a track is lost or ages out and once a minute for the
// fade, so a steady frame pays one copy for all of it. The projection's own
// cut-off is the clip: the range ring on the full scope, the canvas corner
// on the bare one, the same as the live trails.
func (s *Scene) drawExposure(dst *canvas.Canvas, proj projector, weight int) {
	if !s.exposureDrawn() {
		return
	}

	bands := s.corridorBands()
	s.drawTubes(dst, proj, bands, weight)

	for index := range s.exposure.trails {
		trail := &s.exposure.trails[index]

		ink, strong := s.exposureInk(trail)
		if !strong {
			continue
		}

		before, after := trail.Loose()
		drawExposurePath(dst, proj, before, ink, bands)
		drawExposurePath(dst, proj, after, ink, bands)
	}
}

// drawExposurePath draws one track as one-pixel segments in one colour.
//
// The filter is applied a segment at a time, by the band of the fix it ends
// on, so the low band's filter takes the top off a departure's climb and
// leaves the part under it. A segment ending at an undecoded altitude is in no band and is
// never drawn.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func drawExposurePath(
	dst *canvas.Canvas, proj projector, fixes []airplane.PositionEntry, ink color.RGBA, bands bandMask,
) {
	if len(fixes) < 2 {
		return
	}

	prevX, prevY, prevInside := proj.at(fixes[0].Latitude, fixes[0].Longitude)

	for index := 1; index < len(fixes); index++ {
		fix := &fixes[index]
		x, y, inside := proj.at(fix.Latitude, fix.Longitude)

		if inside && prevInside && bands.has(bandOf(fix.Altitude)) {
			dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y), ink)
		}

		prevX, prevY, prevInside = x, y, inside
	}
}

// drawExposure3 draws the corridors in the tilted views as skinned tubes and
// nothing else. The flat views' hairlines are left out here: at the 3D view's
// exaggeration a lost track floats above the ground in a tangle that hid the
// tubes rather than adding to them.
//
// Neither tilted view has a layer, so this is per frame. It runs straight
// after the ground and before the envelope and the live traffic, which keeps
// it under everything that moves.
func (s *Scene) drawExposure3(dst *canvas.Canvas, view scene3) {
	if !s.exposureDrawn() {
		return
	}

	s.drawTubes3(dst, view, s.corridorBands())
}
