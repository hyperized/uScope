package radar

import (
	"math"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The high airways are drawn as dashed lines in the airway ink. Lengths are
// on the ground in nautical miles, so a dash keeps its size against the map
// whatever the range and shrinks with distance in the tilted views like
// everything else.
//
// A ribbon as wide as the bundle, chevrons pointing the way the traffic
// flies, and a chart look with designator boxes and flight levels were all
// tried and dropped; see .claude/notes/radar-tracks.md.
const (
	// airwayEndShare is how much of the airway at each end fades in and out.
	// The ends are only where the antenna stopped hearing the traffic, so the
	// line should not claim the airway stops there.
	airwayEndShare = 0.2

	// airwayDashNm and airwayGapNm are the dash and the gap.
	airwayDashNm = 2.5
	airwayGapNm  = 3.0

	// airwayLight, airwayDark and airwayTilted are the dashes' opacity on a
	// light flat field, a dark flat field and in the tilted views. On the
	// dark field a one-pixel violet dash at the light field's half came out
	// grey and sank into the transit hairlines, so there the dashes are drawn
	// twice as wide as well. They stay well under the tubes, which are what
	// the picture is for.
	airwayLight  = 0.5
	airwayDark   = 0.7
	airwayTilted = 0.6
)

// airwayPen maps a place on one airway to a pixel: a share t of the way from
// its upstream end. It is one struct for both kinds of view so the dashes are
// laid out once.
type airwayPen struct {
	lengthNm float64

	// The flat views: the ends in pixels and the clip circle.
	flat                     bool
	ax, ay, bx, by           float64
	centreX, centreY, radius float64

	// The tilted views: the ends in the world, and whether the range clips.
	view  *scene3
	a, b  point3
	reach bool
}

// at is the pixel for a share of the way along the airway, and false where
// it is clipped or the camera cannot place it.
func (p *airwayPen) at(share float64) (float64, float64, bool) {
	if p.flat {
		x := p.ax + (p.bx-p.ax)*share
		y := p.ay + (p.by-p.ay)*share

		return x, y, math.Hypot(x-p.centreX, y-p.centreY) <= p.radius
	}

	point := point3{
		east:  p.a.east + (p.b.east-p.a.east)*share,
		north: p.a.north + (p.b.north-p.a.north)*share,
		up:    p.a.up,
	}

	if p.reach && !p.view.inRange(point) {
		return 0, 0, false
	}

	x, y, placed := p.view.cam.at(point)

	return float64(x), float64(y), placed
}

// airwayEnds is how much of the airway is left at a share of the way along
// it by the end fades.
func airwayEnds(share float64) float64 {
	left := min(max(min(share, 1-share)/airwayEndShare, 0), 1)

	return left * left * (3 - 2*left)
}

// airwayShown reports whether an airway is drawn under the filter and the
// fade. An airway's altitude is at or above the floor, so the filter keeps
// it exactly when it keeps the high band.
func airwayShown(airway *source.Airway, bands bandMask) bool {
	return airway.Strength >= corridorMinStrength && bands.has(bandOf(airway.Altitude))
}

// drawAirways paints the airways on the flat views' background layer, before
// the tubes, so everything else on the ground is drawn over them.
func (s *Scene) drawAirways(dst *canvas.Canvas, proj projector, bands bandMask, weight int) {
	if !s.airways {
		return
	}

	opacity, width := airwayDark, 2*max(weight, 1)
	if s.light {
		opacity, width = airwayLight, max(weight, 1)
	}

	for index := range s.exposure.airways {
		airway := &s.exposure.airways[index]
		if !airwayShown(airway, bands) {
			continue
		}

		if pen, ok := flatPen(airway, proj); ok {
			s.airwayDashes(dst, &pen, airway.Strength*opacity, width)
		}
	}
}

// drawAirways3 paints the airways in the tilted views at their mean
// altitude. It runs before the tubes, so a tube crossing under an airway is
// drawn over it: the arrivals and departures are what the picture is for.
func (s *Scene) drawAirways3(dst *canvas.Canvas, view scene3, bands bandMask) {
	if !s.airways {
		return
	}

	for index := range s.exposure.airways {
		airway := &s.exposure.airways[index]
		if !airwayShown(airway, bands) {
			continue
		}

		if pen, ok := s.tiltedPen(airway, &view); ok {
			s.airwayDashes(dst, &pen, airway.Strength*airwayTilted, max(view.weight, 1))
		}
	}
}

// flatPen is the pen for one airway in the flat views, and false when it is
// under a pixel long.
func flatPen(airway *source.Airway, proj projector) (airwayPen, bool) {
	fromX, fromY := proj.offset(airway.FromLatitude, airway.FromLongitude)
	toX, toY := proj.offset(airway.ToLatitude, airway.ToLongitude)

	lengthPx := math.Hypot(toX-fromX, toY-fromY)
	if lengthPx < 1 {
		return airwayPen{}, false
	}

	return airwayPen{
		flat: true, ax: fromX, ay: fromY, bx: toX, by: toY,
		lengthNm: lengthPx / proj.scale,
		centreX:  float64(proj.centerX), centreY: float64(proj.centerY), radius: proj.limitNm * proj.scale,
	}, true
}

// tiltedPen is the pen for one airway in the tilted views at its mean
// altitude, and false when it is under a mile long. The bare view has no
// range ring, so only the full one clips to the range.
func (s *Scene) tiltedPen(airway *source.Airway, view *scene3) (airwayPen, bool) {
	pen := airwayPen{
		view:  view,
		a:     view.world(airway.FromLatitude, airway.FromLongitude, airway.Altitude),
		b:     view.world(airway.ToLatitude, airway.ToLongitude, airway.Altitude),
		reach: !s.bare(),
	}

	pen.lengthNm = math.Hypot(pen.b.east-pen.a.east, pen.b.north-pen.a.north)

	return pen, pen.lengthNm >= 1
}

// airwayDashes draws the dashes along the centreline, each faded by where it
// sits against the ends. A dash with either end clipped is left out rather
// than cut at the ring, which at a few pixels long does not show.
func (s *Scene) airwayDashes(dst *canvas.Canvas, pen *airwayPen, strength float64, weight int) {
	period := airwayDashNm + airwayGapNm

	for from := 0.0; from < pen.lengthNm; from += period {
		start, stop := from/pen.lengthNm, min(from+airwayDashNm, pen.lengthNm)/pen.lengthNm

		alpha := strength * airwayEnds((start+stop)/2)
		if alpha < corridorMinStrength {
			continue
		}

		x0, y0, in0 := pen.at(start)
		x1, y1, in1 := pen.at(stop)

		if in0 && in1 {
			dst.BlendLineAA(x0, y0, x1, y1, weight, s.pal.Airway, alpha)
		}
	}
}
