package radar

import (
	"image/color"
	"math"

	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The theoretical radio-horizon bowl.
const (
	// bowlStepFt is the altitude between two rings of the bowl and bowlTopFt
	// the highest one drawn. Five thousand feet a step matches the coverage
	// tracker's own altitude bands, so the theoretical shape and the measured
	// one are quoted at the same heights, and forty-five thousand is above
	// anything with a transponder that is not a balloon.
	bowlStepFt = 5000.0
	bowlTopFt  = 45000.0

	// bowlBands is how many rings that comes to.
	bowlBands = int(bowlTopFt / bowlStepFt)

	// antennaFt is how high the antenna is assumed to be above the ground.
	// uScope has no way to ask, and thirty feet is a rooftop or an attic
	// window, which is where most of them are. It contributes about six
	// nautical miles to every ring, so being wrong by a storey changes the
	// bowl by less than a pixel.
	antennaFt = 30.0

	// horizonFactor turns the square roots of two heights in feet into a
	// line-of-sight distance in nautical miles:
	//
	//	d = 1.23 * (sqrt(h_antenna) + sqrt(h_aircraft))
	//
	// It is the standard VHF and radar horizon approximation, which folds
	// atmospheric refraction in by pretending the earth's radius is four
	// thirds of the real one. It is the formula every ADS-B range chart is
	// drawn with, and it is why an antenna thirty feet up hears an airliner
	// two hundred and fifty nautical miles away when the geometric horizon is
	// under seven.
	horizonFactor = 1.23

	// bowlMeridians is how many lines run from the receiver out along the
	// bowl. Eight is one every forty-five degrees, which is enough for the
	// rings to read as one surface rather than as a stack of loose hoops.
	bowlMeridians = 8
)

// How far each wireframe is mixed towards the field before it is drawn.
//
// Both shapes used to be drawn in their palette colour at full strength, and
// the measured mesh in particular came out brighter than the aircraft inside
// it: the picture read as a wireframe with some dots caught in it rather than
// as traffic inside a measured envelope. The envelope is context, and context
// that outshines the subject is in the way.
//
// They are mixed into the field rather than composited per pixel, which is
// what Scene.fade already does for trails and for the same reason: the scope
// is a near-uniform field, so blending once and drawing the result solid gives
// the same picture as blending every pixel and costs one operation instead of
// thousands.
const (
	// measuredFade keeps the data colour recognisable as itself while dropping
	// it below the aircraft it surrounds.
	measuredFade = 0.35

	// bowlFade is applied to the muted colour, which is already the quietest in
	// the palette; at 0.60 the bowl still outshone the measured mesh by
	// luminance, so both sit at the same fade and the hue alone separates them.
	bowlFade = 0.35
)

// measuredInk is the colour the measured wireframe is drawn in, and bowlInk
// the theoretical bowl's.
//
// The mesh is Data rather than the accent it used to be. It is a record of what
// the antenna heard, which is a reading about the machine and so cyan by the
// same rule the source label and the clocks are, and the accent now means the
// selected aircraft and nothing else. The two shapes were the one place the
// accent appeared twice in a picture, which is exactly what it must not do.
//
// Both are worked out from the palette on every call rather than cached on the
// Scene, for the reason SetPalette takes the light flag off the palette: two
// fields can disagree about which theme is on and one cannot. The callers
// hoist them out of their loops, so a frame does this arithmetic twice.
func (s *Scene) measuredInk() color.RGBA { return s.fade(s.pal.Data, measuredFade) }

func (s *Scene) bowlInk() color.RGBA { return s.fade(s.pal.Muted, bowlFade) }

// horizonNm is how far an aircraft at altitudeFt can be heard by an antenna
// antennaFt above the ground, in nautical miles. See horizonFactor.
func horizonNm(altitudeFt float64) float64 {
	return horizonFactor * (math.Sqrt(altitudeFt) + math.Sqrt(antennaFt))
}

// bowlRadiusNm is one ring of the bowl, clamped to the range on screen.
//
// Without the clamp the bowl leaves the picture at every range a scope
// normally runs at: the lowest ring is already ninety-four nautical miles out.
// Clamped, it reads as a cylinder at close range, which is the honest shape,
// because at forty miles every altitude above five thousand feet is over the
// horizon and the bowl has nothing to say. Open the range past a hundred and
// the curve appears on its own.
func bowlRadiusNm(altitudeFt, scopeNm float64) float64 {
	return min(horizonNm(altitudeFt), scopeNm)
}

// drawBowl3 draws the theoretical envelope: one dashed ring per altitude band,
// and the meridians up the outside of them.
func (s *Scene) drawBowl3(dst *canvas.Canvas, view scene3) {
	ink := s.bowlInk()

	for band := 1; band <= bowlBands; band++ {
		altitudeFt := float64(band) * bowlStepFt

		s.drawCircle3(dst, view,
			point3{up: view.height(altitudeFt)}, bowlRadiusNm(altitudeFt, view.scopeNm), ink, bowlDash)
	}

	s.drawMeridians3(dst, view, ink)
}

// drawMeridians3 draws the lines that run from the receiver up the outside of
// the bowl, one every forty-five degrees.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (*Scene) drawMeridians3(dst *canvas.Canvas, view scene3, ink color.RGBA) {
	for spoke := range bowlMeridians {
		sin, cos := math.Sincos(2 * math.Pi * float64(spoke) / bowlMeridians)
		prevX, prevY, prevOK := view.cam.at(point3{})

		for band := 1; band <= bowlBands; band++ {
			altitudeFt := float64(band) * bowlStepFt
			radius := bowlRadiusNm(altitudeFt, view.scopeNm)

			x, y, ok := view.cam.at(point3{
				east: radius * sin, north: radius * cos, up: view.height(altitudeFt),
			})

			if ok && prevOK {
				dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y), ink)
			}

			prevX, prevY, prevOK = x, y, ok
		}
	}
}

// sectorWidthDeg is how much compass one bearing sector of the grid covers.
// The count is uAirwaves'; the arithmetic is here because that package keeps
// its own copy unexported.
const sectorWidthDeg = degreesPerCircle / coverage.BearingSectorCount

// reachBySector is a measured envelope's radius in each bearing sector at one
// altitude band, in nautical miles, and zero where nothing has been heard.
type reachBySector [coverage.BearingSectorCount]float64

// minBinObservations is the fewest fixes one distance bin of one bearing
// sector at one altitude band has to hold before the envelope reaches out to
// it.
//
// The envelope is a claim about what the antenna has heard, so it has to
// contain everything that was really heard. A percentile cannot do that. At
// the 98th, two fixes in every hundred are outside the mesh by construction,
// and on a busy field that is aircraft visibly flying outside their own
// envelope, which reads as a broken picture rather than as a tail. A count
// floor keeps the whole tail and drops only what was never a tail at all: a
// mis-decoded position lands one fix, occasionally two, in a bin nothing else
// ever touches, while an aircraft genuinely tracked out there is heard every
// few seconds for as long as it is in view, which is dozens. Three sits above
// the first and far below the second.
const minBinObservations = 3

// cellReachNm is how far one bearing sector reaches at one altitude band: the
// outer edge of the farthest distance bin holding at least
// minBinObservations fixes, and zero when no bin in that cell does.
//
// It walks inward from the far bin and stops at the first one over the floor,
// so everything nearer is inside the mesh whether or not it was busy. A gap
// between two occupied bins is a stretch of sky nothing happened to fly
// through, not a hole in the antenna's reception.
func cellReachNm(grid *source.CoverageGrid, sector, band int) float64 {
	bins := &grid.Cells[sector][band]

	for bin := len(bins) - 1; bin >= 0; bin-- {
		if bins[bin] >= minBinObservations {
			return float64(bin+1) * coverage.DistanceBinNm
		}
	}

	return 0
}

// bandReach reads one altitude band out of the grid as a radius per bearing
// sector, reporting whether any sector in it has anything to draw.
//
// This is the whole of the measured envelope's arithmetic. Every vertex is one
// cell of a bearing by altitude by distance grid, so a sector that hears far at
// low level and nothing at high comes out that shape rather than being averaged
// with its neighbours. uScope used to read this off uAirwaves' tracker, which
// keeps altitude by distance over every bearing and one farthest distance per
// bearing over every altitude, and took the nearer of the two: that could draw
// a lopsided outline or a cone but never both at once, because neither
// projection knows what the other is looking at.
//
// A sector nothing has been heard in stays at zero and draws no edge at all,
// which is what a deaf quarter of an antenna looks like. Everything else is
// clamped to the range on screen for the reason the bowl is: a wireframe two
// hundred and fifty nautical miles across on a forty mile scope is off the
// picture, and a shape nobody can see says less than a smaller one they can.
func bandReach(view scene3, grid *source.CoverageGrid, band int) (reachBySector, bool) {
	var reach reachBySector

	filled := false

	for sector := range coverage.BearingSectorCount {
		reachNm := cellReachNm(grid, sector, band)
		if reachNm <= 0 {
			continue
		}

		reach[sector] = min(reachNm, view.scopeNm)
		filled = true
	}

	return reach, filled
}

// sectorPoint is one vertex of the measured envelope: the middle of a bearing
// sector at the radius given, at the middle altitude of a band.
//
// Both are middles rather than edges because a bin is where observations
// landed, not a boundary they respect: putting the vertex in the centre of the
// bin is the least wrong single place for everything that fell in it.
func (v scene3) sectorPoint(sector, band int, radiusNm float64) point3 {
	sin, cos := math.Sincos((float64(sector) + sectorCentre) * sectorWidthDeg * math.Pi / halfCircle)

	return point3{
		east:  radiusNm * sin,
		north: radiusNm * cos,
		up:    v.height((float64(band) + sectorCentre) * coverage.AltitudeBandFt),
	}
}

// drawMeasured3 draws the envelope the antenna has actually heard: a ring
// through the sectors at each altitude band, and an edge up each sector
// between two bands.
//
// The vertical edges only join bands that are next to each other and both have
// something in them. A band with no observations breaks the surface rather
// than being bridged across, because an edge drawn through an empty band would
// claim reception nothing ever saw.
func (s *Scene) drawMeasured3(dst *canvas.Canvas, view scene3, grid *source.CoverageGrid) {
	var below reachBySector

	ink := s.measuredInk()
	haveBelow := false

	for band := range coverage.AltitudeBandCount {
		reach, filled := bandReach(view, grid, band)
		if !filled {
			haveBelow = false

			continue
		}

		s.drawMeasuredRing3(dst, view, band, reach, ink)

		if haveBelow {
			s.drawMeasuredEdges3(dst, view, band, reach, below, ink)
		}

		below, haveBelow = reach, true
	}
}

// drawMeasuredRing3 joins one band's vertices into a closed ring, skipping an
// edge wherever either end of it has never been heard.
func (s *Scene) drawMeasuredRing3(
	dst *canvas.Canvas, view scene3, band int, reach reachBySector, ink color.RGBA,
) {
	for sector := range coverage.BearingSectorCount {
		next := (sector + 1) % coverage.BearingSectorCount
		if reach[sector] <= 0 || reach[next] <= 0 {
			continue
		}

		s.drawEdge3(dst, view, ink,
			view.sectorPoint(sector, band, reach[sector]),
			view.sectorPoint(next, band, reach[next]))
	}
}

// drawMeasuredEdges3 joins one band's vertices to the band below it, which is
// what turns a stack of rings into a surface.
func (s *Scene) drawMeasuredEdges3(
	dst *canvas.Canvas, view scene3, band int, upper, lower reachBySector, ink color.RGBA,
) {
	for sector := range coverage.BearingSectorCount {
		if upper[sector] <= 0 || lower[sector] <= 0 {
			continue
		}

		s.drawEdge3(dst, view, ink,
			view.sectorPoint(sector, band, upper[sector]),
			view.sectorPoint(sector, band-1, lower[sector]))
	}
}

// drawEdge3 draws one wireframe edge, dropping it when either end is off the
// picture.
//
// The colour is handed in rather than worked out here, so the mix happens once
// a frame instead of once an edge. A busy grid draws a couple of hundred
// of these.
func (*Scene) drawEdge3(dst *canvas.Canvas, view scene3, ink color.RGBA, from, to point3) {
	fromX, fromY, fromOK := view.cam.at(from)
	toX, toY, toOK := view.cam.at(to)

	if !fromOK || !toOK {
		return
	}

	dst.LineAA(float64(fromX), float64(fromY), float64(toX), float64(toY), ink)
}
