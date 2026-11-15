package radar

import (
	"image/color"
	"math"

	"github.com/hyperized/uScope/pkg/canvas"
)

// coastInks3 is the colour a coastline is drawn in for each band of the haze,
// indexed by haze3.band: the shore colour at zero and a shade more of the
// horizon colour at every band out.
type coastInks3 [hazeBands + 1]color.RGBA

// drawShore3 paints the water, the land and the coastlines on the floor of
// the world, in that order.
//
// It is drawShore in perspective, down to the order: the fill goes down first
// and the outlines over it, so the join between sea and land is covered by the
// line that describes it and the jagged edge the fill leaves never shows. Both
// halves measure the haze once between them, so a line and the ground under it
// fade by the same distance. A nil set draws nothing.
func (s *Scene) drawShore3(dst *canvas.Canvas, view scene3) {
	if s.shoreSet == nil {
		return
	}

	haze := view.haze()

	s.fillGround3(dst, view, haze)
	s.drawCoast3(dst, view, haze)
}

// drawCoast3 draws every coastline the camera can see, out to the far edge of
// the haze.
//
// Inside the reach a coastline is what it always was, the shore colour at the
// view's weight. Past it the line fades out over the ground under it, band by
// band, and is gone where the ground has finished fading; see coastInks3 for
// the rate. It used to stop at the reach, and over a ground that now fades out
// instead of ending, a dark line that stopped dead read as a fault in the
// picture. Each segment beyond the reach takes its colour from the band its
// middle lies in, so nothing is worked out per pixel.
//
// The lines come from a cache that keeps the land's rules, for the land's
// reason: the far edge of the haze is up to nine hundred miles out, and what
// costs out there is projecting points, not finding them. A line whose box is
// past the haze, or wholly off the picture, is skipped before any of it is
// projected. The cull's margin takes in the line's weight, since a stroke
// reaches that far either side of the line it draws.
func (s *Scene) drawCoast3(dst *canvas.Canvas, view scene3, haze haze3) {
	s.cacheLines3(&s.coast3, coastLines, view, haze.reachNm())

	sight := view.cam.frustum(dst.Bounds().Inset(-cullMarginPx - view.weight))
	inks := s.coastInks3()

	for _, line := range s.coast3.lines {
		if !haze.past(line) && !sight.misses(view.cam, line.low, line.high) {
			s.drawShoreLine3(dst, view, haze, &inks, line)
		}
	}
}

// coastInks3 is this frame's coastline colour for every band.
//
// Each band's ink is that band's bare ground, the field carried towards the
// horizon, plus the shore colour's lead over the field scaled by the square of
// the band's strength. It is built on the bare ground rather than on the land
// so that it sits the same distance off the land and off the sea, which carry
// different tints. The ground keeps
// strength itself, so the line fades faster than the ground does, and that is
// deliberate. The line stands three to four times further off the field than
// the sea does (115 levels against 33 by day, 127 against 43 at night),
// so at the same share it was still a crisp line by the time the sea and land
// had merged into the haze, and it disappeared only in the last few bands. On
// a shallow tilt those bands are a few rows each, which made the
// shore look as if it stopped rather than faded. With the square, line and
// ground drop out of sight at about the same distance. Squaring keeps the
// shore's weight at or below strength, which hazed needs to stay a mix.
func (s *Scene) coastInks3() coastInks3 {
	inks := coastInks3{s.pal.Shore}

	for band := 1; band <= hazeBands; band++ {
		strength := hazeStrength(band)
		inks[band] = s.hazed(s.pal.Shore, strength*strength, strength)
	}

	return inks
}

// drawShoreLine3 projects one coastline and draws it.
//
// The thinning is the scope view's moved a step earlier in the pipeline, to
// nautical miles on the ground rather than pixels on the canvas. A piece of
// ground shrinks on the screen as one over its distance, so the threshold
// grows with the distance of the nearest corner of the line's box beyond the
// full tint. Inside it nothing changes; out in the haze a coastline of a
// thousand points is drawn as a few dozen, which is what keeps the far coast
// from costing more than the near one.
func (s *Scene) drawShoreLine3(dst *canvas.Canvas, view scene3, haze haze3, inks *coastInks3, line boxedLine3) {
	points := line.line
	if len(points) < 2 {
		return
	}

	thin := view.minSegNm * max(1, haze.nearest(line)/haze.near)
	ring := circle{radius: view.reachNm}
	from := view.ground(points[0].Lat, points[0].Lon)

	for index := 1; index < len(points); index++ {
		next := view.ground(points[index].Lat, points[index].Lon)

		// The last point is always drawn, however short the run to it, so a
		// coastline reaches its own end rather than stopping one segment early.
		if index < len(points)-1 &&
			math.Abs(next.east-from.east) < thin && math.Abs(next.north-from.north) < thin {
			continue
		}

		s.drawShoreSegment3(dst, view, haze, inks, ring, from, next)

		from = next
	}
}

// drawShoreSegment3 draws one piece of coast: the part inside the range in the
// shore colour, exactly as it was cut before the haze, and whatever runs on
// either side of it in the colour of the band it lies in.
func (s *Scene) drawShoreSegment3(dst *canvas.Canvas, view scene3, haze haze3, inks *coastInks3,
	ring circle, first, last point3,
) {
	enter, leave, inside := ring.span(segment{fromX: first.east, fromY: first.north, toX: last.east, toY: last.north})
	if !inside {
		drawHazedSegment3(dst, view, haze, inks, first, last)

		return
	}

	entry, exit := first.along(last, enter), first.along(last, leave)

	if enter > 0 {
		drawHazedSegment3(dst, view, haze, inks, first, entry)
	}

	view.strokeGround(dst, entry, exit, s.pal.Shore)

	if leave < 1 {
		drawHazedSegment3(dst, view, haze, inks, exit, last)
	}
}

// drawHazedSegment3 draws a piece of coast outside the range in the colour of
// the band its middle lies in, or not at all past the haze.
//
// The middle, rather than either end, is what keeps a long segment that
// crosses a band edge from taking the colour of the band it barely touches.
// The edge of the range always lies inside the full tint, so the piece either
// side of the cut starts in the shore colour and there is no step where the
// two meet.
func drawHazedSegment3(dst *canvas.Canvas, view scene3, haze haze3, inks *coastInks3, first, last point3) {
	band := haze.band(first.along(last, sectorCentre))
	if band == hazePast {
		return
	}

	view.strokeGround(dst, first, last, inks[band])
}

// strokeGround draws a line between two points on the floor, or nothing when
// the camera cannot place either end.
func (v scene3) strokeGround(dst *canvas.Canvas, first, last point3, col color.RGBA) {
	startX, startY, startOK := v.cam.at(first)
	endX, endY, endOK := v.cam.at(last)

	if startOK && endOK {
		dst.StrokeLineAA(float64(startX), float64(startY), float64(endX), float64(endY), v.weight, col)
	}
}
