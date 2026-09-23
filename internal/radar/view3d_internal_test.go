package radar

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// The fixed camera these tests project through: a square box the size of the
// scope at the panel's resolution, at the default elevation and the azimuth
// the view starts on.
const (
	cameraSide    = 600
	cameraRangeNm = 40.0
	cameraTopNm   = bowlTopFt * DefaultExaggerate / ftPerNm
)

// testCamera builds the camera every projection case below measures against.
func testCamera(azimuthDeg, elevationDeg float64) camera3 {
	cam, _ := newCamera3(image.Rect(0, 0, cameraSide, cameraSide), framing3{
		scopeNm:     cameraRangeNm,
		azimuth:     azimuthDeg,
		elevation:   elevationDeg,
		topNm:       cameraTopNm,
		topRadiusNm: bowlRadiusNm(bowlTopFt, cameraRangeNm),
	})

	return cam
}

// TestNewCamera3Refuses checks the three shapes of nonsense the camera will
// not be built from. Every one of them would otherwise divide by zero or
// produce a NaN focal length, and a NaN reaching an integer conversion is
// undefined in the spec rather than merely wrong.
func TestNewCamera3Refuses(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		box     image.Rectangle
		scopeNm float64
	}{
		{name: "an empty box", box: image.Rectangle{}, scopeNm: cameraRangeNm},
		{name: "a box with no width", box: image.Rect(0, 0, 0, cameraSide), scopeNm: cameraRangeNm},
		{name: "a range of zero", box: image.Rect(0, 0, cameraSide, cameraSide), scopeNm: 0},
		{name: "a negative range", box: image.Rect(0, 0, cameraSide, cameraSide), scopeNm: -1},
		{name: "a NaN range", box: image.Rect(0, 0, cameraSide, cameraSide), scopeNm: math.NaN()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			framing := framing3{
				scopeNm: testCase.scopeNm, azimuth: 0, elevation: defaultElevation,
				topNm: cameraTopNm, topRadiusNm: bowlRadiusNm(bowlTopFt, testCase.scopeNm),
			}
			if _, ok := newCamera3(testCase.box, framing); ok {
				t.Errorf("newCamera3 with %s reported ok, want refused", testCase.name)
			}
		})
	}
}

// TestCameraFramesTheRing checks the rule ringFraming3 solves for: the outer
// range ring spans about ringSpan of the box's width, when that rule is the
// one that binds.
//
// It only binds at a shallow envelope: framing the ring across the box then
// asks for more distance than framing the envelope down it does. A life-size
// envelope is that shallow, so the camera here is built at MinExaggerate
// rather than through testCamera, whose default exaggeration is deep enough
// that the vertical rule binds instead and the ring comes out narrower; see
// the note on newCamera3.
//
// It is measured between the ring's east and west points, which are its widest
// pair from any azimuth, and allowed a few per cent either way: the solved
// distance treats both as sitting at the orbit distance, and the near one is
// fractionally closer than that.
func TestCameraFramesTheRing(t *testing.T) {
	t.Parallel()

	cam, drawable := newCamera3(image.Rect(0, 0, cameraSide, cameraSide), framing3{
		scopeNm:     cameraRangeNm,
		azimuth:     0,
		elevation:   defaultElevation,
		topNm:       bowlTopFt * MinExaggerate / ftPerNm,
		topRadiusNm: bowlRadiusNm(bowlTopFt, cameraRangeNm),
	})
	if !drawable {
		t.Fatal("newCamera3 refused a life-size framing, want a camera")
	}

	eastX, _, eastOK := cam.at(point3{east: cameraRangeNm})
	westX, _, westOK := cam.at(point3{east: -cameraRangeNm})

	if !eastOK || !westOK {
		t.Fatalf("the outer ring's east/west points are not visible: east %v, west %v", eastOK, westOK)
	}

	const tolerance = 0.05

	got := float64(eastX-westX) / cameraSide
	if math.Abs(got-ringSpan) > tolerance {
		t.Errorf("the outer ring spans %.3f of the box width, want about %.2f", got, ringSpan)
	}
}

// TestCameraProjection checks the three things the picture depends on being
// true: east is to the right at azimuth zero, height goes up the screen, and
// anything behind the camera is culled rather than drawn mirrored.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func TestCameraProjection(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)
	centre := cameraSide / 2

	t.Run("the outer ring east lands right of centre", func(t *testing.T) {
		t.Parallel()

		x, _, ok := cam.at(point3{east: cameraRangeNm})
		if !ok {
			t.Fatal("the outer ring's east point is not visible")
		}

		if x <= centre {
			t.Errorf("east at the outer ring landed at x = %d, want right of %d", x, centre)
		}
	})

	t.Run("a point straight up lands above its own ground point", func(t *testing.T) {
		t.Parallel()

		const climbNm = 20.0

		// On the camera's own vertical plane, which at azimuth zero is the
		// north/south line through the receiver. A stalk off to one side of it
		// leans towards the vanishing point as it climbs, so only one on the
		// plane can be asked to keep its x.
		groundX, groundY, groundOK := cam.at(point3{north: 10})
		airX, airY, airOK := cam.at(point3{north: 10, up: climbNm})

		if !groundOK || !airOK {
			t.Fatalf("one end of the stalk is not visible: ground %v, air %v", groundOK, airOK)
		}

		if airY >= groundY {
			t.Errorf("the air point landed at y = %d, want above the ground point's %d", airY, groundY)
		}

		if airX != groundX {
			t.Errorf("the air point landed at x = %d, want the ground point's %d", airX, groundX)
		}
	})

	t.Run("a point behind the camera is culled", func(t *testing.T) {
		t.Parallel()

		// Azimuth zero puts the camera due south, so far enough south is
		// behind it. Without the depth test this would project to a mirrored
		// ghost somewhere on the picture.
		if _, _, ok := cam.at(point3{north: -1000}); ok {
			t.Error("a point behind the camera projected, want it culled")
		}
	})

	t.Run("a point on the lens is culled", func(t *testing.T) {
		t.Parallel()

		if _, _, ok := cam.at(cam.eye); ok {
			t.Error("the camera's own position projected, want it culled")
		}
	})

	t.Run("a NaN coordinate is culled", func(t *testing.T) {
		t.Parallel()

		if _, _, ok := cam.at(point3{east: math.NaN()}); ok {
			t.Error("a NaN point projected, want it culled")
		}
	})
}

// TestCameraWalksRoundTheTraffic checks that the azimuth turns the world
// under the camera rather than turning the camera in place.
//
// At azimuth zero the camera is due south and east is to the right of the
// screen, which is the orientation the flat scope has. A quarter turn round
// puts it due west looking east, so north has moved off to the left.
func TestCameraWalksRoundTheTraffic(t *testing.T) {
	t.Parallel()

	const quarterTurn = 90.0

	cam := testCamera(quarterTurn, defaultElevation)
	centre := cameraSide / 2

	northX, _, northOK := cam.at(point3{north: cameraRangeNm})
	if !northOK {
		t.Fatal("the outer ring's north point is not visible at a quarter turn")
	}

	if northX >= centre {
		t.Errorf("north landed at x = %d, want left of %d once the camera has walked a quarter turn",
			northX, centre)
	}
}

// TestCameraElevationFlattensTheGround checks that the elevation is a tilt
// rather than a height: seen from nearly overhead a range ring is almost the
// circle it really is, and seen from low down it squashes into an ellipse.
//
// It is the property the brackets move and the one thing that would silently
// break if the camera's up axis were built wrong.
func TestCameraElevationFlattensTheGround(t *testing.T) {
	t.Parallel()

	depth := func(elevationDeg float64) int {
		cam := testCamera(0, elevationDeg)

		_, northY, northOK := cam.at(point3{north: cameraRangeNm})
		_, southY, southOK := cam.at(point3{north: -cameraRangeNm})

		if !northOK || !southOK {
			t.Fatalf("the outer ring is not visible at %g degrees of elevation", elevationDeg)
		}

		return southY - northY
	}

	if depth(maxElevation) <= depth(minElevation) {
		t.Error("the ground ring is no deeper from overhead than from low down, want it rounder")
	}
}

// TestCameraGuardsWildCoordinates checks the cull that keeps a line from being
// drawn to a point a million pixels away.
//
// The point sits just in front of the lens and far off to one side, so it
// passes the depth test and then projects to an enormous offset. Nothing in a
// real frame lands there; the guard exists because the cost of finding out is
// a million rejected Set calls rather than a wrong pixel.
func TestCameraGuardsWildCoordinates(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)

	wild := point3{
		east:  cam.eye.east + 500,
		north: cam.eye.north + minDepth*2,
		up:    cam.eye.up,
	}

	if _, _, ok := cam.at(wild); ok {
		t.Error("a point projecting far outside the box was drawn, want it culled")
	}
}

// TestCameraAzimuth checks the automatic orbit: stopped it stays put, running
// it winds forward on the render clock at one revolution per orbitPeriod.
func TestCameraAzimuth(t *testing.T) {
	t.Parallel()

	const quarterTurn = 90.0

	for _, testCase := range []struct {
		name     string
		orbiting bool
		azimuth  float64
		at       time.Duration
		elapsed  time.Duration
		want     float64
	}{
		{name: "stopped stays where it was put", azimuth: quarterTurn, elapsed: orbitPeriod, want: quarterTurn},
		{name: "a quarter period is a quarter turn", orbiting: true, elapsed: orbitPeriod / 4, want: quarterTurn},
		{name: "a whole period comes back round", orbiting: true, elapsed: orbitPeriod, want: 0},
		{
			name:     "it winds on from where the last keypress left it",
			orbiting: true, azimuth: quarterTurn, at: orbitPeriod, elapsed: orbitPeriod + orbitPeriod/4,
			want: 2 * quarterTurn,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{orbiting: testCase.orbiting, azimuth: testCase.azimuth, azimuthAt: testCase.at}

			const tolerance = 1e-9

			if got := scene.cameraAzimuth(testCase.elapsed); math.Abs(got-testCase.want) > tolerance {
				t.Errorf("cameraAzimuth(%s) = %g, want %g", testCase.elapsed, got, testCase.want)
			}
		})
	}
}

// TestWrapDegrees checks that an angle comes back into the compass rose from
// either side, which is what a nudge below zero and an orbit past a full turn
// both need.
func TestWrapDegrees(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "already in range", in: 90, want: 90},
		{name: caseZero, in: 0, want: 0},
		{name: "one turn is zero", in: degreesPerCircle, want: 0},
		{name: "past a turn", in: degreesPerCircle + 45, want: 45},
		{name: caseNegative, in: -15, want: 345},
		{name: "well below zero", in: -degreesPerCircle - 15, want: 345},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const tolerance = 1e-9

			if got := wrapDegrees(testCase.in); math.Abs(got-testCase.want) > tolerance {
				t.Errorf("wrapDegrees(%g) = %g, want %g", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestHorizonNm checks the radio-horizon formula at the bottom and the top of
// the bowl.
//
// The figures are the formula worked through by hand:
// 1.23 * (sqrt(5000) + sqrt(30)) and 1.23 * (sqrt(45000) + sqrt(30)). They are
// here rather than recomputed in the test so that changing the constant is a
// test failure rather than a silent agreement with itself.
func TestHorizonNm(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		altitudeFt float64
		want       float64
	}{
		{name: "the bottom of the bowl", altitudeFt: bowlStepFt, want: 93.711},
		{name: "the top of the bowl", altitudeFt: bowlTopFt, want: 267.660},
		{name: "on the ground it is the antenna's own horizon", altitudeFt: 0, want: 6.737},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const tolerance = 0.001

			if got := horizonNm(testCase.altitudeFt); math.Abs(got-testCase.want) > tolerance {
				t.Errorf("horizonNm(%g) = %.3f, want %.3f", testCase.altitudeFt, got, testCase.want)
			}
		})
	}
}

// TestBowlRadiusClampsToTheRange checks that the bowl never reaches past the
// picture: the lowest ring is already ninety-four nautical miles out, so at
// every range a scope normally runs at the clamp is what is drawn.
func TestBowlRadiusClampsToTheRange(t *testing.T) {
	t.Parallel()

	if got := bowlRadiusNm(bowlStepFt, cameraRangeNm); got != cameraRangeNm {
		t.Errorf("bowlRadiusNm at %g nm range = %g, want the range itself", cameraRangeNm, got)
	}

	// Open the range past the horizon and the curve appears on its own.
	const wideRangeNm = 300.0

	if got := bowlRadiusNm(bowlStepFt, wideRangeNm); got >= wideRangeNm {
		t.Errorf("bowlRadiusNm at %g nm range = %g, want the horizon rather than the range", wideRangeNm, got)
	}
}

// TestCellReachNm checks the one number the measured envelope is built from:
// how far one bearing sector reaches at one altitude band.
//
// The floor is a count of fixes per distance bin rather than a percentile of
// the cell, and the two middle cases are why. A percentile leaves a share of
// every cell's fixes outside the mesh by construction, which on a busy field
// is aircraft visibly flying outside their own envelope. A count floor keeps
// the whole tail and drops only the one or two fixes a mis-decode drops into a
// bin nothing else ever touched.
func TestCellReachNm(t *testing.T) {
	t.Parallel()

	const (
		sector = 3
		band   = 6

		// nearTop is where the bulk of the fixes sit, farBin where the stray
		// one lands, and quietBin a bin between two busy ones that nothing
		// happened to fly through.
		nearTop  = 5
		quietBin = 7
		farBin   = 20

		// busy is a cell's worth of real traffic, comfortably over the floor.
		busy = 40

		lastBin = coverage.DistanceBinCount - 1
	)

	for _, testCase := range []struct {
		name string
		bins map[int]uint32
		want float64
	}{
		{
			name: "an empty cell reaches nothing",
			want: 0,
		},
		{
			name: "a bin under the floor reaches nothing",
			bins: map[int]uint32{nearTop: minBinObservations - 1},
			want: 0,
		},
		{
			name: "one stray fix in a far bin is left outside",
			bins: map[int]uint32{0: busy, 1: busy, 2: busy, 3: busy, nearTop: busy, farBin: 1},
			want: float64(nearTop+1) * coverage.DistanceBinNm,
		},
		{
			name: "three fixes in the same far bin are an aircraft rather than a mis-decode",
			bins: map[int]uint32{0: busy, 1: busy, 2: busy, 3: busy, nearTop: busy, farBin: minBinObservations},
			want: float64(farBin+1) * coverage.DistanceBinNm,
		},
		{
			name: "a quiet bin between two busy ones is inside the reach",
			bins: map[int]uint32{0: busy, quietBin: busy},
			want: float64(quietBin+1) * coverage.DistanceBinNm,
		},
		{
			name: "a saturated far bin still counts",
			bins: map[int]uint32{lastBin: math.MaxUint32},
			want: float64(lastBin+1) * coverage.DistanceBinNm,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var grid source.CoverageGrid

			for bin, count := range testCase.bins {
				grid.Cells[sector][band][bin] = count
			}

			if got := cellReachNm(&grid, sector, band); got != testCase.want {
				t.Errorf("cellReachNm = %g, want %g", got, testCase.want)
			}

			// Nothing was written to either neighbour, so the indexing is
			// wrong if anything comes back out of one.
			if got := cellReachNm(&grid, sector+1, band); got != 0 {
				t.Errorf("the next bearing sector reaches %g, want 0", got)
			}

			if got := cellReachNm(&grid, sector, band+1); got != 0 {
				t.Errorf("the next altitude band reaches %g, want 0", got)
			}
		})
	}
}

// TestBandReach checks that the envelope is read per bearing sector and per
// altitude band rather than from one figure per band: the same sector reaches
// a different distance at two heights, and a sector nothing was ever heard in
// has no vertex at either.
//
// That pair is what a chimney on one side of a rooftop antenna looks like, and
// it is the thing the two projections uAirwaves' tracker keeps cannot describe
// between them. Cells was altitude by distance over every bearing and Sectors
// one farthest distance per bearing over every altitude, so a vertex taken as
// the nearer of the two could be lopsided in outline or shrink with altitude
// and never both at once.
func TestBandReach(t *testing.T) {
	t.Parallel()

	const (
		heardSector = 3
		deafSector  = 11

		// The two cells: bin 11 ends at 120 nautical miles and bin 2 at 30, so
		// the same sector reaches four times as far up high as it does low
		// down.
		highBand = 6
		highBin  = 11
		highNm   = 120.0
		lowBand  = 1
		lowBin   = 2
		lowNm    = 30.0

		// wideScopeNm is well past the far cell, so the clamp is out of the
		// way in every case but the one that is about it.
		wideScopeNm = 300.0
	)

	var grid source.CoverageGrid

	grid.Cells[heardSector][highBand][highBin] = minBinObservations
	grid.Cells[heardSector][lowBand][lowBin] = minBinObservations

	wide := scene3{scopeNm: wideScopeNm, reachNm: wideScopeNm}

	for _, testCase := range []struct {
		name string
		band int
		want float64
	}{
		{name: "the high band reaches its own bin", band: highBand, want: highNm},
		{name: "the low band reaches a nearer one", band: lowBand, want: lowNm},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reach, filled := bandReach(wide, &grid, testCase.band)
			if !filled {
				t.Fatalf("band %d came back empty, want a vertex in sector %d", testCase.band, heardSector)
			}

			if reach[heardSector] != testCase.want {
				t.Errorf("sector %d at band %d reaches %g, want %g",
					heardSector, testCase.band, reach[heardSector], testCase.want)
			}

			if reach[deafSector] != 0 {
				t.Errorf("sector %d reaches %g, want 0 so the ring skips it", deafSector, reach[deafSector])
			}
		})
	}

	t.Run("a band nothing was heard in draws nothing", func(t *testing.T) {
		t.Parallel()

		if _, filled := bandReach(wide, &grid, highBand+1); filled {
			t.Error("a band with no cell over the floor came back filled, want empty")
		}
	})

	t.Run("a reach past the scope range is clamped to it", func(t *testing.T) {
		t.Parallel()

		near := scene3{scopeNm: cameraRangeNm, reachNm: cameraRangeNm}

		reach, filled := bandReach(near, &grid, highBand)
		if !filled {
			t.Fatal("the populated band came back empty")
		}

		if reach[heardSector] != cameraRangeNm {
			t.Errorf("a %g nm reach on a %g nm scope draws at %g, want the range",
				highNm, cameraRangeNm, reach[heardSector])
		}
	})
}

// TestSectorPoint checks that a measured vertex lands in the middle of its
// bearing sector and at the middle altitude of its band, rather than on either
// edge of either bin.
func TestSectorPoint(t *testing.T) {
	t.Parallel()

	const (
		radiusNm  = 30.0
		tolerance = 1e-9
	)

	view := scene3{scopeNm: cameraRangeNm, reachNm: cameraRangeNm, upScale: DefaultExaggerate / ftPerNm}

	// Sector 0 spans 0 to 22.5 degrees, so its centre bearing is 11.25.
	point := view.sectorPoint(0, 0, radiusNm)

	sin, cos := math.Sincos(sectorWidthDeg * sectorCentre * math.Pi / halfCircle)

	if math.Abs(point.east-radiusNm*sin) > tolerance || math.Abs(point.north-radiusNm*cos) > tolerance {
		t.Errorf("sector 0's vertex is at east %g north %g, want the middle of the sector",
			point.east, point.north)
	}

	wantUp := view.height(coverage.AltitudeBandFt * sectorCentre)
	if math.Abs(point.up-wantUp) > tolerance {
		t.Errorf("band 0's vertex is at %g nm up, want %g (the middle of the band)", point.up, wantUp)
	}
}

// TestSceneHeight checks the one guard in the altitude stretch: an altitude of
// zero is one nobody has decoded rather than sea level, so it sits on the
// ground instead of being multiplied into one.
func TestSceneHeight(t *testing.T) {
	t.Parallel()

	view := scene3{upScale: DefaultExaggerate / ftPerNm}

	if got := view.height(0); got != 0 {
		t.Errorf("height(0) = %g, want 0", got)
	}

	if got := view.height(-1); got != 0 {
		t.Errorf("height(-1) = %g, want 0", got)
	}

	want := bowlStepFt * DefaultExaggerate / ftPerNm
	if got := view.height(bowlStepFt); math.Abs(got-want) > 1e-9 {
		t.Errorf("height(%g) = %g, want %g", bowlStepFt, got, want)
	}
}

// TestProjectRefusals checks the two things scene3.project drops before it
// reaches the camera at all: an aircraft with no decoded position, and one
// outside the range the ground furniture is drawn to.
func TestProjectRefusals(t *testing.T) {
	t.Parallel()

	view := scene3{
		cam:     testCamera(0, defaultElevation),
		origin:  geo{lat: layerBaseLat, lon: layerBaseLon},
		cosLat0: math.Cos(layerBaseLat * math.Pi / halfCircle),
		scopeNm: cameraRangeNm,
		reachNm: cameraRangeNm,
		upScale: DefaultExaggerate / ftPerNm,
	}

	if _, _, ok := view.project(0, 0, 10000); ok {
		t.Error("an aircraft at the undecoded (0, 0) projected, want it refused")
	}

	// A degree of latitude is sixty nautical miles, so one degree north is
	// well outside the forty mile range.
	if _, _, ok := view.project(layerBaseLat+1, layerBaseLon, 10000); ok {
		t.Error("an aircraft outside the range projected, want it refused")
	}

	if _, _, ok := view.project(layerBaseLat+0.1, layerBaseLon, 10000); !ok {
		t.Error("an aircraft inside the range was refused, want it drawn")
	}
}

// view3DScene builds a scene already in the 3D view, over a frame the caller
// supplies, plus the canvas it draws on.
func view3DScene(tb testing.TB, frame source.Frame) (*Scene, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := New(layerTestFaces(tb), &stubSource{frame: frame},
		scope.New(scope.WithCurrent(cameraRangeNm)))
	scene.shown = View3D

	return scene, canv
}

// view3DPicture is the part of the canvas the 3D scene is drawn in: the scope
// box and the margin around it, but not the column beside it.
//
// The accent colour marks the selected flight's rule in that column as well as
// the measured envelope, so a count taken over the whole canvas would be
// reading the panel rather than the picture.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var view3DPicture = image.Rect(0, 0, 620, layerCanvasHeight)

// coveredFrame is a frame carrying a synthetic coverage grid: the altitude
// bands named, heard in every bearing sector except gapSector.
//
// Both knobs are the point of the fixture. A missing sector is what a
// directional antenna looks like and is the case the wireframe has to leave an
// edge out of; the band list is how a stack with a hole in it gets built. A
// gapSector of -1 is a sector nothing matches, so the envelope closes all the
// way round. Every cell named carries minBinObservations in one bin, which is
// exactly the floor cellReachNm draws a vertex at.
func coveredFrame(gapSector int, bands ...int) source.Frame {
	frame := source.Frame{
		Receiver: source.Receiver{
			Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual,
		},
		Now: time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC),
	}

	// Bin 1 ends twenty nautical miles out, which is half the fixture's range
	// and so a shape with room around it in the picture.
	const bin = 1

	for _, band := range bands {
		for sector := range coverage.BearingSectorCount {
			if sector == gapSector {
				continue
			}

			frame.Grid.Cells[sector][band][bin] = minBinObservations
		}
	}

	return frame
}

// TestMeasuredEnvelopeSkipsAnEmptySector checks the wireframe against a
// synthetic snapshot: a sector nothing was ever heard in produces no edge, so
// the shape drawn with a gap in it carries fewer accent pixels than the one
// drawn all the way round.
func TestMeasuredEnvelopeSkipsAnEmptySector(t *testing.T) {
	t.Parallel()

	const (
		noGap   = -1
		lowBand = 1
	)

	full, fullCanvas := view3DScene(t, coveredFrame(noGap, lowBand, lowBand+1))
	full.Draw(fullCanvas, 0)

	gapped, gappedCanvas := view3DScene(t, coveredFrame(0, lowBand, lowBand+1))
	gapped.Draw(gappedCanvas, 0)

	fullInk := countInk(fullCanvas, full.measuredInk())
	gappedInk := countInk(gappedCanvas, gapped.measuredInk())

	if fullInk == 0 {
		t.Fatal("the full envelope drew no pixels at all, so this comparison proves nothing")
	}

	if gappedInk >= fullInk {
		t.Errorf("the gapped envelope drew %d pixels against the full one's %d, want fewer",
			gappedInk, fullInk)
	}
}

// TestMeasuredEnvelopeIsNotRoundWhenTheGridIsNot checks the property the whole
// grid exists for: an antenna that hears one half of the sky four times as far
// as the other draws a mesh that is further out on that half.
//
// The reach used to be one figure per altitude band crossed with one per
// bearing sector, and once the band figure became the smaller of the two it
// decided every vertex, so the mesh came out round however lopsided the
// antenna was. Reading a bearing by altitude by distance cell puts the outline
// back.
func TestMeasuredEnvelopeIsNotRoundWhenTheGridIsNot(t *testing.T) {
	t.Parallel()

	const (
		band    = 1
		nearBin = 0
		farBin  = 3

		// eastSectors is the half of the compass the camera at azimuth zero
		// shows to the right of the receiver: bearings 0 through 180.
		eastSectors = coverage.BearingSectorCount / 2
	)

	scene, view := envelopeFixture()

	var round, lopsided source.CoverageGrid

	for sector := range coverage.BearingSectorCount {
		round.Cells[sector][band][farBin] = minBinObservations

		bin := nearBin
		if sector < eastSectors {
			bin = farBin
		}

		lopsided.Cells[sector][band][bin] = minBinObservations
	}

	roundCanvas, lopsidedCanvas := blankCanvas(t), blankCanvas(t)

	scene.drawMeasured3(roundCanvas, view, &round)
	scene.drawMeasured3(lopsidedCanvas, view, &lopsided)

	if identicalPixels(roundCanvas, lopsidedCanvas) {
		t.Fatal("a lopsided grid drew the same mesh as a round one")
	}

	// roundSlack is how far the two halves of a round mesh may differ and
	// still be round: a ring is rasterised a pixel either way, not to the
	// pixel.
	const roundSlack = 4

	roundWest, roundEast := meshSpread(roundCanvas)
	if diff := roundWest - roundEast; diff > roundSlack || diff < -roundSlack {
		t.Errorf("a round grid drew %d pixels west of the receiver and %d east, want the two within %d",
			roundWest, roundEast, roundSlack)
	}

	// The east half of the lopsided grid reaches four bins, the west half one,
	// so anything short of twice as far east is the outline being averaged
	// away rather than drawn.
	lopsidedWest, lopsidedEast := meshSpread(lopsidedCanvas)
	if lopsidedEast <= 2*lopsidedWest {
		t.Errorf("a lopsided grid drew %d pixels west of the receiver and %d east, want the east half much further out",
			lopsidedWest, lopsidedEast)
	}
}

// TestMeasuredEnvelopeNeedsObservations checks that an antenna that has heard
// nothing draws no envelope at all, rather than a flat ring at the origin.
func TestMeasuredEnvelopeNeedsObservations(t *testing.T) {
	t.Parallel()

	frame := source.Frame{
		Receiver: source.Receiver{
			Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual,
		},
	}

	scene, canv := view3DScene(t, frame)
	scene.Draw(canv, 0)

	if got := countInk(canv, scene.measuredInk()); got != 0 {
		t.Errorf("an empty coverage snapshot drew %d envelope pixels, want none", got)
	}
}

// TestMeasuredEnvelopeBreaksOverAnEmptyBand checks that two bands with an
// empty one between them are not bridged: an edge drawn through a band nobody
// heard anything in would claim reception the tracker never saw.
//
// It compares the whole envelope against the two rings drawn on their own, so
// a single bridging edge is a pixel difference rather than something that has
// to be inferred from a count. The adjacent pair is the other half of the
// check: there the edges have to be there, or the test would pass on a
// drawMeasured3 that drew no edges at all.
func TestMeasuredEnvelopeBreaksOverAnEmptyBand(t *testing.T) {
	t.Parallel()

	const (
		lowBand  = 0
		nextBand = 1
		gapBand  = 2
	)

	for _, testCase := range []struct {
		name      string
		upper     int
		wantEdges bool
	}{
		{name: "an empty band between two breaks the surface", upper: gapBand},
		{name: "two bands next to each other are joined", upper: nextBand, wantEdges: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, view := envelopeFixture()
			grid := coveredFrame(-1, lowBand, testCase.upper).Grid

			whole := blankCanvas(t)
			scene.drawMeasured3(whole, view, &grid)

			ringsOnly := blankCanvas(t)

			for _, band := range []int{lowBand, testCase.upper} {
				reach, filled := bandReach(view, &grid, band)
				if !filled {
					t.Fatalf("band %d came back empty, so the fixture proves nothing", band)
				}

				scene.drawMeasuredRing3(ringsOnly, view, band, reach, scene.measuredInk())
			}

			if identicalPixels(whole, ringsOnly) == testCase.wantEdges {
				t.Errorf("the envelope matched its rings alone = %v, want %v",
					identicalPixels(whole, ringsOnly), !testCase.wantEdges)
			}
		})
	}
}

// fixtureMinSegNm is the coastline thinning threshold the fixture view
// carries, in nautical miles. It is about what a forty mile range comes to at
// the panel's resolution, and it is set rather than left zero so the case that
// folds a too-short segment into the next one is reachable.
const fixtureMinSegNm = 0.17

// envelopeFixture is a scene and a 3D view built without a layout, for the
// cases below that call one drawing function rather than drawing a frame.
func envelopeFixture() (*Scene, scene3) {
	view := scene3{
		cam:      testCamera(0, defaultElevation),
		origin:   geo{lat: layerBaseLat, lon: layerBaseLon},
		cosLat0:  math.Cos(layerBaseLat * math.Pi / halfCircle),
		scopeNm:  cameraRangeNm,
		reachNm:  cameraRangeNm,
		upScale:  DefaultExaggerate / ftPerNm,
		minSegNm: fixtureMinSegNm,
	}

	return &Scene{
		pal:        theme.Night,
		scopeRange: scope.New(scope.WithCurrent(cameraRangeNm)),
		elevation:  defaultElevation,
		exaggerate: DefaultExaggerate,
	}, view
}

// blankCanvas is a square canvas the size of the scope box, cleared to the
// field so a drawn pixel is the only thing on it.
func blankCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(cameraSide, cameraSide)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	return canv
}

// identicalPixels reports whether two canvases of the same size hold the same
// picture, byte for byte.
func identicalPixels(first, second *canvas.Canvas) bool {
	return bytes.Equal(first.Image().Pix, second.Image().Pix)
}

// TestDrawEdge3CullsOffPicture checks that a wireframe edge with an end the
// camera cannot see is dropped rather than drawn to wherever the projection
// happened to land.
func TestDrawEdge3CullsOffPicture(t *testing.T) {
	t.Parallel()

	canv := blankCanvas(t)
	scene, view := envelopeFixture()

	behind := point3{north: -1000}

	scene.drawEdge3(canv, view, scene.measuredInk(), behind, point3{east: 10})
	scene.drawEdge3(canv, view, scene.measuredInk(), point3{east: 10}, behind)

	if got := countInk(canv, scene.measuredInk()); got != 0 {
		t.Errorf("an edge with an end behind the camera drew %d pixels, want none", got)
	}
}

// countInk counts the pixels of one exact colour on a canvas, which is how
// these tests ask whether a piece of furniture was drawn without pinning a
// whole picture.
//
// It is the internal twin of the external tests' countColour, narrowed to
// view3DPicture: the wireframe lands wherever the camera is pointing, which is
// anywhere in the box, and never in the column beside it.
func countInk(canv *canvas.Canvas, want color.RGBA) int {
	bounds := canv.Bounds().Intersect(view3DPicture)
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == want {
				count++
			}
		}
	}

	return count
}

// meshSpread is how far left and right of the receiver a drawn shape reaches,
// in pixels, which is how a test asks whether that shape is lopsided without
// pinning a whole picture. The camera puts the receiver in the middle of its
// square box.
//
// It counts any pixel that is not the field rather than the exact ink colour,
// because LineAA blends a line's edges into the field and only the pixels at
// full coverage come back as the ink itself. Those are a sparse sample of the
// shape; its outline is not.
//
//nolint:nonamedreturns // (west, east) reads clearer named at this signature.
func meshSpread(canv *canvas.Canvas) (west, east int) {
	bounds := canv.Bounds().Intersect(view3DPicture)
	middle := cameraSide / 2

	// A pixel on the wrong side of the middle gives a negative distance, which
	// max drops on the floor, so neither half needs a branch of its own.
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) != theme.Night.Field {
				west, east = max(west, middle-x), max(east, x-middle+1)
			}
		}
	}

	return west, east
}

// TestCameraKeysBelongToTheView checks that the camera keys are claimed by the
// 3D view and by nothing else.
//
// In the scope and minimal views there is no camera to move and no envelope to
// toggle, so they have to fall through to the run loop rather than quietly
// changing state that nothing on screen could show.
func TestCameraKeysBelongToTheView(t *testing.T) {
	t.Parallel()

	for _, value := range []rune{'e', 'E', 'o', 'O', '[', ']'} {
		t.Run(string(value), func(t *testing.T) {
			t.Parallel()

			for _, view := range []View{ViewScope, ViewMinimal} {
				scene := &Scene{shown: view}
				if scene.handleRune(value) {
					t.Errorf("%q was taken in the %s view, want it to fall through", value, view)
				}
			}

			scene := &Scene{shown: View3D}
			if !scene.handleRune(value) {
				t.Errorf("%q was not taken in the 3D view, want the camera to have it", value)
			}
		})
	}
}

// TestHandle3DRuneIgnoresTheRest checks that a key the camera does not bind
// still falls through while the 3D view is on screen.
func TestHandle3DRuneIgnoresTheRest(t *testing.T) {
	t.Parallel()

	scene := &Scene{shown: View3D}
	if scene.handleRune('z') {
		t.Error("an unbound key was taken in the 3D view, want it to fall through")
	}
}

// TestEnvelopeToggle checks that e flips the envelope and that it starts on:
// the envelope is most of the reason the view exists, so a 3D scope without it
// is a tilted scope.
func TestEnvelopeToggle(t *testing.T) {
	t.Parallel()

	scene := New(Faces{}, &stubSource{}, scope.New())
	if !scene.envelope {
		t.Fatal("the envelope starts off, want it on")
	}

	scene.shown = View3D

	scene.handleRune('e')

	if scene.envelope {
		t.Error("e did not turn the envelope off")
	}

	scene.handleRune('E')

	if !scene.envelope {
		t.Error("E did not turn the envelope back on")
	}
}

// TestTiltClamps checks that the brackets move the elevation by one step and
// stop at both ends of its travel rather than running past them.
//
// Clamping is right here where ParseRecentre refuses: this is a key held down
// against the end of its travel, not a value somebody typed.
func TestTiltClamps(t *testing.T) {
	t.Parallel()

	scene := &Scene{shown: View3D, elevation: defaultElevation}

	scene.handleRune(']')

	if got := scene.elevation; got != defaultElevation+tiltStep {
		t.Errorf("] moved the elevation to %g, want %g", got, defaultElevation+tiltStep)
	}

	scene.handleRune('[')

	if got := scene.elevation; got != defaultElevation {
		t.Errorf("[ moved the elevation to %g, want %g", got, defaultElevation)
	}

	// Far more presses than the travel holds, from either end.
	for range 1 + int((maxElevation-minElevation)/tiltStep) {
		scene.handleRune(']')
	}

	if got := scene.elevation; got != maxElevation {
		t.Errorf("] past the top left the elevation at %g, want %g", got, maxElevation)
	}

	for range 1 + int((maxElevation-minElevation)/tiltStep) {
		scene.handleRune('[')
	}

	if got := scene.elevation; got != minElevation {
		t.Errorf("[ past the bottom left the elevation at %g, want %g", got, minElevation)
	}
}

// TestOrbitKeys checks the three things Left, Right and o do between them: a
// nudge turns the camera by one step, a nudge stops the orbit, and o sets it
// going again from wherever the nudges left it.
func TestOrbitKeys(t *testing.T) {
	t.Parallel()

	scene := New(Faces{}, &stubSource{}, scope.New())
	if !scene.orbiting {
		t.Fatal("the camera starts still, want it orbiting")
	}

	scene.shown = View3D

	// Half a period in, so the running azimuth is somewhere the stopped one
	// could not have reached on its own.
	scene.elapsed = orbitPeriod / 2
	running := scene.cameraAzimuth(scene.elapsed)

	if !scene.Handle(input.Key{Kind: input.Right}) {
		t.Fatal("Right was not taken in the 3D view")
	}

	if scene.orbiting {
		t.Error("Right left the camera orbiting, want it stopped")
	}

	const tolerance = 1e-9

	if got := scene.cameraAzimuth(scene.elapsed); math.Abs(got-wrapDegrees(running+azimuthStep)) > tolerance {
		t.Errorf("Right turned the camera to %g, want %g", got, wrapDegrees(running+azimuthStep))
	}

	// A stopped camera stays where it is however much time passes.
	if got := scene.cameraAzimuth(scene.elapsed + orbitPeriod); math.Abs(got-scene.azimuth) > tolerance {
		t.Errorf("a stopped camera drifted to %g, want %g", got, scene.azimuth)
	}

	if !scene.Handle(input.Key{Kind: input.Left}) {
		t.Fatal("Left was not taken in the 3D view")
	}

	if got := scene.cameraAzimuth(scene.elapsed); math.Abs(got-wrapDegrees(running)) > tolerance {
		t.Errorf("Left did not undo Right: the camera is at %g, want %g", got, wrapDegrees(running))
	}

	stopped := scene.azimuth

	scene.handleRune('o')

	if !scene.orbiting {
		t.Error("o did not set the camera orbiting again")
	}

	if math.Abs(scene.azimuth-stopped) > tolerance {
		t.Errorf("o restarted the orbit from %g, want the azimuth on screen, %g", scene.azimuth, stopped)
	}

	if got := scene.cameraAzimuth(scene.elapsed + orbitPeriod/4); math.Abs(got-wrapDegrees(stopped+90)) > tolerance {
		t.Errorf("a quarter period after o the camera is at %g, want %g", got, wrapDegrees(stopped+90))
	}
}

// TestArrowsFallThroughOutsideTheView checks that Left and Right still reach
// the run loop in the two flat views, which is what they did before there was
// a camera to turn.
func TestArrowsFallThroughOutsideTheView(t *testing.T) {
	t.Parallel()

	for _, view := range []View{ViewScope, ViewMinimal} {
		scene := &Scene{shown: view}

		for _, key := range []input.Kind{input.Left, input.Right} {
			if scene.Handle(input.Key{Kind: key}) {
				t.Errorf("an arrow was taken in the %s view, want it to fall through", view)
			}
		}
	}
}

// TestApplyExaggerate checks that a Settings block left unset reads as the
// scene's own default rather than as a flat world.
func TestApplyExaggerate(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "unset falls back to the default", in: 0, want: DefaultExaggerate},
		{name: caseNegative, in: -4, want: DefaultExaggerate},
		{name: "the floor", in: MinExaggerate, want: MinExaggerate},
		{name: "the ceiling", in: MaxExaggerate, want: MaxExaggerate},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{scopeRange: scope.New()}
			scene.Apply(Settings{Exaggerate: testCase.in})

			if scene.exaggerate != testCase.want {
				t.Errorf("Apply(Exaggerate: %g) left %g, want %g", testCase.in, scene.exaggerate, testCase.want)
			}
		})
	}
}

// TestView3DGuards exercises the guards on the drawing functions directly.
//
// Every one of them refuses an input a whole frame cannot produce: a circle of
// no radius, a coastline of one point, a trail of one fix, a scope box with no
// room in it, and geometry the camera cannot see. They are called here rather
// than through Draw because there is no canvas size or camera angle that
// reaches them, and a guard nothing ever tests is a guard nobody can trust.
func TestView3DGuards(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	scene.faces = Faces{}

	t.Run("a circle of no radius draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		scene.drawCircle3(canv, view, point3{}, 0, theme.Night.Rule, solidDash)
		scene.drawCircle3(canv, view, point3{}, -10, theme.Night.Rule, solidDash)

		if !identicalPixels(canv, blank) {
			t.Error("a circle of no radius drew something, want nothing")
		}
	})

	t.Run("a coastline of one point draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		scene.drawShoreLine3(canv, view, shore.Polyline{{Lat: layerBaseLat, Lon: layerBaseLon}})

		if !identicalPixels(canv, blank) {
			t.Error("a one-point coastline drew something, want nothing")
		}
	})

	t.Run("a coastline outside the range draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		const wayOut = 1000.0

		ring := circle{radius: view.scopeNm}
		scene.drawShoreSegment3(canv, view, ring,
			point3{east: wayOut, north: wayOut}, point3{east: wayOut + 1, north: wayOut})

		if !identicalPixels(canv, blank) {
			t.Error("a coastline outside the range drew something, want nothing")
		}
	})

	t.Run("a coastline behind the camera draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		// A ring wide enough to keep a segment the camera is standing in
		// front of. Nothing sets a range like this, which is exactly why the
		// projection has to refuse the piece rather than the clip.
		const wideRingNm = 1e6

		ring := circle{radius: wideRingNm}
		scene.drawShoreSegment3(canv, view, ring, point3{north: -wideRingNm / 2}, point3{north: -wideRingNm / 3})

		if !identicalPixels(canv, blank) {
			t.Error("a coastline behind the camera drew something, want nothing")
		}
	})
}

// TestView3DThinsADenseCoastline checks the one thing drawShoreLine3 does
// beyond projecting: points closer together than a pixel's worth of ground are
// folded into the next segment rather than each costing a line of their own.
//
// Natural Earth carries a point every few hundred metres, which at any range
// uScope shows is dozens of points inside one pixel.
func TestView3DThinsADenseCoastline(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	scene.faces = Faces{}

	t.Run("a dense coastline is thinned rather than drawn point by point", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		dense := blankCanvas(t)

		// Ten points inside one thinning threshold, then one far enough away
		// to be worth its own segment. Drawn point by point they would be the
		// same line at ten times the cost.
		const (
			crowded = 10
			tinyDeg = 0.0005
		)

		line := make(shore.Polyline, 0, crowded+1)

		for step := range crowded {
			line = append(line, shore.Point{Lat: layerBaseLat + float64(step)*tinyDeg, Lon: layerBaseLon})
		}

		line = append(line, shore.Point{Lat: layerBaseLat + 0.2, Lon: layerBaseLon})

		scene.drawShoreLine3(canv, view, line)
		scene.drawShoreLine3(dense, view, shore.Polyline{line[0], line[len(line)-1]})

		if identicalPixels(canv, blankCanvas(t)) {
			t.Fatal("the thinned coastline drew nothing at all, so this proves nothing")
		}

		if !identicalPixels(canv, dense) {
			t.Error("thinning changed the line, want the crowded points folded into the long segment")
		}
	})
}

// TestView3DMoreGuards is TestView3DGuards continued. The two are one subject
// split in half because a single function of a dozen subtests runs past the
// length the linter allows.
func TestView3DMoreGuards(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	scene.faces = Faces{}

	t.Run("a trail of one fix draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		scene.trail = trailAll
		scene.drawTrail3(canv, view, []airplane.PositionEntry{{Latitude: layerBaseLat, Longitude: layerBaseLon}},
			theme.Night.AltLow)

		if !identicalPixels(canv, blank) {
			t.Error("a one-fix trail drew something, want nothing")
		}
	})

	t.Run("a stalk whose base is behind the camera draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		// The base is projected from the aircraft's own position, so an
		// aircraft far enough south of an azimuth-zero camera has its shadow
		// behind the lens while the caller has already found a pixel for the
		// aircraft itself.
		behind := airplane.Snapshot{Latitude: layerBaseLat - 100, Longitude: layerBaseLon}

		scene.drawStalk3(canv, view, behind, cameraSide/2, cameraSide/2)

		if !identicalPixels(canv, blank) {
			t.Error("a stalk with its base behind the camera drew something, want nothing")
		}
	})
}

// TestView3DCardinals covers the two ways a cardinal letter is left off the
// ground: no face to set it in, and a projection that cannot place it.
func TestView3DCardinals(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	scene.faces = Faces{}

	t.Run("no face means no cardinal letters", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		lay := layout{dst: canv, labels: true}

		scene.drawCardinals3(&lay, view)

		if !identicalPixels(canv, blank) {
			t.Error("a scene with no body face drew cardinal letters, want none")
		}
	})

	t.Run("a cardinal the camera cannot see is skipped", func(t *testing.T) {
		t.Parallel()

		canv := blankCanvas(t)
		blank := blankCanvas(t)

		// A range far wider than the camera was built for, so the letters on
		// the ring land behind the lens. Nothing sets a range like this; the
		// skip exists because a projection that cannot answer must not be
		// asked to guess.
		wide := view
		wide.scopeNm = 1e6

		lay := layout{dst: canv, labels: true}
		lettered := *scene
		lettered.faces = layerTestFaces(t)

		lettered.drawCardinals3(&lay, wide)

		if !identicalPixels(canv, blank) {
			t.Error("a cardinal behind the camera was drawn, want it skipped")
		}
	})
}

// TestView3DRefusesABoxItCannotDrawIn covers the two shapes of scope box the
// view will not draw in at all: one with no room in it, and one the canvas has
// no pixels under.
func TestView3DRefusesABoxItCannotDrawIn(t *testing.T) {
	t.Parallel()

	scene, _ := envelopeFixture()
	scene.faces = Faces{}

	for _, testCase := range []struct {
		name string
		box  image.Rectangle
	}{
		{name: "a scope box with no room in it"},
		{name: "a scope box off the canvas", box: image.Rect(10000, 10000, 10600, 10600)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := blankCanvas(t)
			blank := blankCanvas(t)

			// The second box is one the camera is happy to be built for and
			// the canvas has no pixels under. Nothing produces one today; the
			// guard is there because the window is the only thing between the
			// picture and the block beside it, and a window onto nothing must
			// not be drawn through.
			lay := layout{dst: canv, scope: testCase.box}

			scene.draw3D(&lay, source.Frame{}, 0)

			if !identicalPixels(canv, blank) {
				t.Errorf("%s drew something, want nothing", testCase.name)
			}
		})
	}
}

// The two theme names, used as case names wherever a table runs once per
// palette. They are constants because the same pair turns up in four tables
// across this package's tests.
const (
	caseNight = "night"
	caseDay   = "day"
)

// TestEnvelopeInkIsFadedIntoTheField pins the two wireframe colours to exact
// values.
//
// Both shapes used to be drawn in their palette colour at full strength, and
// the measured mesh came out brighter than the aircraft it surrounds. The
// envelope is context, so it has to sit behind the traffic rather than in
// front of it, and the only way a pixel test elsewhere in this package can
// count envelope pixels is if it knows exactly which colour they are. These
// literals are what the external tests compute independently, so a change to
// either fade or to mix fails here and there rather than silently passing a
// test that is now counting nothing.
func TestEnvelopeInkIsFadedIntoTheField(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		pal          theme.Palette
		wantMeasured color.RGBA
		wantBowl     color.RGBA
	}{
		{
			name: caseNight, pal: theme.Night,
			wantMeasured: color.RGBA{R: 22, G: 79, B: 89, A: opaque},
			wantBowl:     color.RGBA{R: 49, G: 52, B: 55, A: opaque},
		},
		{
			name: caseDay, pal: theme.Day,
			wantMeasured: color.RGBA{R: 159, G: 201, B: 217, A: opaque},
			wantBowl:     color.RGBA{R: 192, G: 198, B: 203, A: opaque},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)

			if got := scene.measuredInk(); got != testCase.wantMeasured {
				t.Errorf("measuredInk() = %v, want %v", got, testCase.wantMeasured)
			}

			if got := scene.bowlInk(); got != testCase.wantBowl {
				t.Errorf("bowlInk() = %v, want %v", got, testCase.wantBowl)
			}
		})
	}
}

// TestEnvelopeInkSitsBetweenTheFieldAndItsSource checks the property the exact
// values above are an instance of: a faded wireframe is nearer the field than
// the palette colour it came from, in every channel and in both themes.
//
// The literals pin today's arithmetic; this pins what the arithmetic is for,
// so a future change that moves the numbers still has to keep the shapes
// receding rather than advancing.
func TestEnvelopeInkSitsBetweenTheFieldAndItsSource(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
	}{
		{name: caseNight, pal: theme.Night},
		{name: caseDay, pal: theme.Day},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)

			assertBetween(t, "measured", testCase.pal.Field, scene.measuredInk(), testCase.pal.Data)
			assertBetween(t, "bowl", testCase.pal.Field, scene.bowlInk(), testCase.pal.Muted)
		})
	}
}

// assertBetween reports whether mixed lies between field and source in every
// channel, which is what "faded towards the field" means channel by channel
// without assuming which of the two ends is the brighter one. Paper's field is
// lighter than its ink and night's is darker, so a test written as "dimmer"
// would only hold on one theme.
func assertBetween(tb testing.TB, what string, field, mixed, toward color.RGBA) {
	tb.Helper()

	for _, channel := range []struct {
		name          string
		from, got, to uint8
	}{
		{name: "red", from: field.R, got: mixed.R, to: toward.R},
		{name: "green", from: field.G, got: mixed.G, to: toward.G},
		{name: "blue", from: field.B, got: mixed.B, to: toward.B},
	} {
		low, high := min(channel.from, channel.to), max(channel.from, channel.to)
		if channel.got < low || channel.got > high {
			tb.Errorf("%s %s = %d, want between the field's %d and the palette's %d",
				what, channel.name, channel.got, channel.from, channel.to)
		}
	}
}

// TestToggleOrbit checks that o is a switch rather than a restart, and that
// stopping freezes the camera where the picture actually has it.
//
// The second half is the subtle one. While the orbit runs, the azimuth on
// screen is wound forward from azimuthAt by the render clock, and s.azimuth
// holds only where the last keypress left it. Stopping by setting orbiting to
// false and nothing else would snap the view back to wherever the revolution
// began, which is a jump of up to a full turn on the frame after the press.
func TestToggleOrbit(t *testing.T) {
	t.Parallel()

	// A quarter of a revolution, so the wound-forward azimuth is a right
	// angle from where the orbit started and cannot be confused with it.
	const quarterTurn = 90.0

	scene := &Scene{shown: View3D, orbiting: true, elapsed: orbitPeriod / 4}

	if got := scene.cameraAzimuth(scene.elapsed); got != quarterTurn {
		t.Fatalf("cameraAzimuth = %v, want %v, so the fixture proves nothing", got, quarterTurn)
	}

	if !scene.handleRune('o') {
		t.Fatal("handleRune('o') = false, want the 3D view to take it")
	}

	if scene.orbiting {
		t.Error("o left the camera orbiting, want it stopped")
	}

	if got := scene.azimuth; got != quarterTurn {
		t.Errorf("o froze the camera at %v, want the %v the picture was showing", got, quarterTurn)
	}

	// Frozen means frozen: the render clock moving on must not move the view.
	if got := scene.cameraAzimuth(orbitPeriod); got != quarterTurn {
		t.Errorf("a stopped camera drifted to %v, want it held at %v", got, quarterTurn)
	}

	if !scene.handleRune('o') {
		t.Fatal("a second handleRune('o') = false, want the 3D view to take it")
	}

	if !scene.orbiting {
		t.Error("a second o left the camera stopped, want it turning again")
	}

	// Restarted from where it was rather than from where the last revolution
	// began, so the picture does not jump when the orbit picks up.
	if got := scene.cameraAzimuth(scene.elapsed); got != quarterTurn {
		t.Errorf("the restarted orbit began at %v, want the %v on screen", got, quarterTurn)
	}
}

// TestToggleOrbitIsThreeDOnly checks that o falls through in the other two
// views, the way every camera key does.
//
// There is no camera to stop in the scope or in minimal, and a key that
// quietly changed state nothing on screen could show would be a key whose
// effect turned up as a surprise the next time v was pressed.
func TestToggleOrbitIsThreeDOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		view View
	}{
		{name: "scope", view: ViewScope},
		{name: "minimal", view: ViewMinimal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view, orbiting: true}

			if scene.handleRune('o') {
				t.Error("handleRune('o') = true, want the key to fall through to the run loop")
			}

			if !scene.orbiting {
				t.Error("o stopped the orbit from a view with no camera in it")
			}
		})
	}
}

// The aircraft the stalk cases below fly: north-east of the receiver and well
// inside the range, so its stalk stands clear of the four cardinal letters out
// on the ring and of the range rings on the ground.
const (
	stalkNorthNm    = 9.0
	stalkEastNm     = 9.0
	stalkAltitudeFt = 30000.0

	// stalkEndSkip is how many samples at each end of the line are left out of
	// the count: the model sits on one end and the ground rings pass near the
	// other, and neither is the stalk.
	stalkEndSkip = 4

	// stalkSamples is how many points along the line are read. The line is
	// under two hundred pixels long at this camera, so one sample a pixel or
	// finer costs nothing and cannot step over a one-pixel stalk.
	stalkSamples = 256
)

// TestBare3DDrawsNoStalk checks that the bare 3D view leaves the air under an
// aircraft empty.
//
// The stalk is what the two perspective views differ by once the furniture is
// gone, so the full view is measured first over the same aircraft: a run where
// nothing was drawn between the ground and the aeroplane in either view would
// otherwise pass for the wrong reason.
func TestBare3DDrawsNoStalk(t *testing.T) {
	t.Parallel()

	if full := stalkRun(t, View3D); full == 0 {
		t.Fatal("the full 3D view drew nothing between the ground and the aircraft, so this proves nothing")
	}

	if got := stalkRun(t, ViewMinimal3D); got != 0 {
		t.Errorf("the bare 3D view drew %d pixels along the stalk, want the ground under it left empty", got)
	}
}

// stalkRun draws one aircraft in the view named and counts the pixels that are
// not bare field along the line from its shadow on the ground up to itself.
//
// The overlays, the envelope and the trail are all off, so the only thing that
// can land on that line is the stalk. The layout is measured the way Draw
// measures it rather than guessed at, because the box the camera frames against
// is the whole canvas in the bare view and the square beside the column in the
// full one.
func stalkRun(tb testing.TB, shown View) int {
	tb.Helper()

	plane := airplane.Snapshot{
		ICAO:      icaoSampleReal,
		Callsign:  "KLM123",
		Latitude:  layerBaseLat + stalkNorthNm/nmPerDegree,
		Longitude: layerBaseLon + stalkEastNm/(nmPerDegree*math.Cos(layerBaseLat*math.Pi/halfCircle)),
		Altitude:  stalkAltitudeFt,
		Heading:   45,
		Velocity:  420,
	}

	frame := source.Frame{
		Receiver: source.Receiver{
			Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual,
		},
		Planes: []airplane.Snapshot{plane},
		Now:    time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC),
	}

	scene, canv := view3DScene(tb, frame)
	scene.shown = shown
	scene.envelope, scene.airports, scene.shoreOn = false, false, false
	scene.Draw(canv, 0)

	view, ok := scene.measure3D(stalkLayout(scene, canv), frame, 0)
	if !ok {
		tb.Fatalf("measure3D in the %s view reported no room", shown)
	}

	groundX, groundY, groundOK := view.cam.at(view.ground(plane.Latitude, plane.Longitude))
	airX, airY, airOK := view.project(plane.Latitude, plane.Longitude, plane.Altitude)

	if !groundOK || !airOK {
		tb.Fatalf("one end of the stalk is not visible in the %s view: ground %v, air %v", shown, groundOK, airOK)
	}

	return paintedRun(canv, scene.pal.Field, groundX, groundY, airX, airY)
}

// stalkLayout carves the frame the way Draw carves it, so the camera measured
// here is the camera the picture was drawn through.
func stalkLayout(scene *Scene, canv *canvas.Canvas) *layout {
	lay := scene.newLayout(canv)

	if scene.bare() {
		lay.scope = canv.Bounds()

		return &lay
	}

	lay.bottom -= scene.keyBarHeight(&lay)
	lay.top += scene.headerHeight(&lay)
	lay.split()

	return &lay
}

// paintedRun counts the samples along a line that are anything other than the
// field colour, leaving stalkEndSkip samples out at each end.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func paintedRun(canv *canvas.Canvas, field color.RGBA, fromX, fromY, toX, toY int) int {
	bounds := canv.Bounds()
	count := 0

	for step := stalkEndSkip; step <= stalkSamples-stalkEndSkip; step++ {
		along := float64(step) / stalkSamples
		x := fromX + int(math.Round(float64(toX-fromX)*along))
		y := fromY + int(math.Round(float64(toY-fromY)*along))

		if !image.Pt(x, y).In(bounds) {
			continue
		}

		if canv.Image().RGBAAt(x, y) != field {
			count++
		}
	}

	return count
}
