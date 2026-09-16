package radar

import (
	"math"

	"github.com/hyperized/uAirwaves/pkg/coverage"
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
	for band := 1; band <= bowlBands; band++ {
		altitudeFt := float64(band) * bowlStepFt

		s.drawCircle3(dst, view,
			bowlRadiusNm(altitudeFt, view.scopeNm), view.height(altitudeFt), s.pal.Muted, bowlDash)
	}

	s.drawMeridians3(dst, view)
}

// drawMeridians3 draws the lines that run from the receiver up the outside of
// the bowl, one every forty-five degrees.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawMeridians3(dst *canvas.Canvas, view scene3) {
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
				dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y), s.pal.Muted)
			}

			prevX, prevY, prevOK = x, y, ok
		}
	}
}

// sectorWidthDeg is how much compass a coverage bearing sector covers. The
// count is the tracker's; the arithmetic is here because the tracker keeps its
// own copy unexported.
const sectorWidthDeg = degreesPerCircle / coverage.BearingSectorCount

// reachBySector is a measured envelope's radius in each bearing sector at one
// altitude band, in nautical miles, and zero where nothing has been heard.
type reachBySector [coverage.BearingSectorCount]float64

// sectorReach reads one altitude band of a coverage snapshot, reporting
// whether any sector in it has anything to draw.
//
// coverage.Snapshot has no bearing-by-altitude grid to read directly: Cells is
// altitude band by distance bin, and Sectors is one farthest distance per
// bearing sector over every altitude. The wireframe is the two put together,
// which is as much as the tracker can say. A band reaches as far as its
// farthest occupied distance bin, a sector as far as its own farthest
// observation, and a vertex is the nearer of the two. So the bands decide how
// the shape stacks up and the sectors decide its outline, and a sector nothing
// has ever been heard in stays at zero and draws no edge at all, which is what
// makes a directional antenna come out lopsided rather than round.
//
// Everything is clamped to the range on screen for the reason the bowl is: a
// wireframe two hundred and fifty nautical miles across on a forty mile scope
// is off the picture, and a shape nobody can see says less than a smaller one
// they can.
func sectorReach(view scene3, snapshot coverage.Snapshot, band int) (reachBySector, bool) {
	var reach reachBySector

	bandNm := bandReachNm(snapshot, band)
	if bandNm <= 0 {
		return reach, false
	}

	filled := false

	for sector := range coverage.BearingSectorCount {
		observed := snapshot.Sectors[sector]
		if observed <= 0 {
			continue
		}

		reach[sector] = min(observed, bandNm, view.scopeNm)
		filled = true
	}

	return reach, filled
}

// bandReachNm is the outer edge of the farthest distance bin holding an
// observation in one altitude band, or zero when the band is empty.
//
// The outer edge rather than the middle of the bin, because the farthest
// aircraft in it was somewhere in that ten nautical miles and the near edge
// would understate every band by up to a bin. The sector's own figure is an
// exact distance and caps it wherever it is the smaller of the two, so the
// overstatement only survives in sectors where the band itself is the limit.
func bandReachNm(snapshot coverage.Snapshot, band int) float64 {
	for bin := coverage.DistanceBinCount - 1; bin >= 0; bin-- {
		if snapshot.Cells[band][bin] > 0 {
			return float64(bin+1) * coverage.DistanceBinNm
		}
	}

	return 0
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
// claim reception the tracker never saw.
func (s *Scene) drawMeasured3(dst *canvas.Canvas, view scene3, snapshot coverage.Snapshot) {
	var below reachBySector

	haveBelow := false

	for band := range coverage.AltitudeBandCount {
		reach, filled := sectorReach(view, snapshot, band)
		if !filled {
			haveBelow = false

			continue
		}

		s.drawMeasuredRing3(dst, view, band, reach)

		if haveBelow {
			s.drawMeasuredEdges3(dst, view, band, reach, below)
		}

		below, haveBelow = reach, true
	}
}

// drawMeasuredRing3 joins one band's vertices into a closed ring, skipping an
// edge wherever either end of it has never been heard.
func (s *Scene) drawMeasuredRing3(dst *canvas.Canvas, view scene3, band int, reach reachBySector) {
	for sector := range coverage.BearingSectorCount {
		next := (sector + 1) % coverage.BearingSectorCount
		if reach[sector] <= 0 || reach[next] <= 0 {
			continue
		}

		s.drawEdge3(dst, view,
			view.sectorPoint(sector, band, reach[sector]),
			view.sectorPoint(next, band, reach[next]))
	}
}

// drawMeasuredEdges3 joins one band's vertices to the band below it, which is
// what turns a stack of rings into a surface.
func (s *Scene) drawMeasuredEdges3(
	dst *canvas.Canvas, view scene3, band int, upper, lower reachBySector,
) {
	for sector := range coverage.BearingSectorCount {
		if upper[sector] <= 0 || lower[sector] <= 0 {
			continue
		}

		s.drawEdge3(dst, view,
			view.sectorPoint(sector, band, upper[sector]),
			view.sectorPoint(sector, band-1, lower[sector]))
	}
}

// drawEdge3 draws one wireframe edge in the accent colour, dropping it when
// either end is off the picture.
func (s *Scene) drawEdge3(dst *canvas.Canvas, view scene3, from, to point3) {
	fromX, fromY, fromOK := view.cam.at(from)
	toX, toY, toOK := view.cam.at(to)

	if !fromOK || !toOK {
		return
	}

	dst.LineAA(float64(fromX), float64(fromY), float64(toX), float64(toY), s.pal.Accent)
}
