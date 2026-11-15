package radar

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// tubeRoute is a route along the same north-easterly line exposureTrack
// flies, climbing through the three bands and ending on an altitude nobody
// decoded, at strength.
func tubeRoute(class source.TrackClass, strength float64) source.Route {
	stations := make([]source.Station, 0, 4)

	for index, altitude := range []float64{3000, 15000, 30000, 0} {
		stations = append(stations, source.Station{
			Latitude:  layerBaseLat + float64(index)*0.1,
			Longitude: layerBaseLon + float64(index)*0.13,
			Altitude:  altitude,
			SpreadNm:  float64(index),
			SpreadFt:  float64(index) * 500,
			Members:   2,
		})
	}

	return source.Route{Class: class, Strength: strength, Stations: stations}
}

// layerKeyBarTop is the first row of the key bar on the layer fixture's
// 1280x720 canvas.
const layerKeyBarTop = 682

// aboveKeyBar is the canvas's pixels down to the key bar. The flat views'
// U cap shows whether the corridors are on, so a picture with them on and one
// with them off always differ there, and the counts below are about the
// picture.
func aboveKeyBar(canv *canvas.Canvas) []byte {
	img := canv.Image()

	return img.Pix[:layerKeyBarTop*img.Stride]
}

// tubePixels counts what routes and trails change on the fixture scene in
// view under filter, against the same scene with the corridors off.
func tubePixels(t *testing.T, view View, routes []source.Route, trails []source.Trail, filter BandFilter) int {
	t.Helper()

	draw := func(corridors Toggle) []byte {
		scene, canv, src := exposedLayerScene(t)
		scene.Apply(Settings{View: view, Shore: ToggleOff, Filter: filter, Corridors: corridors})

		src.frame.Ghosts = trails
		src.frame.Routes = routes

		scene.Draw(canv, 0)

		return aboveKeyBar(canv)
	}

	tubed, bare := draw(ToggleOn), draw(ToggleOff)
	count := 0

	for index := range tubed {
		if tubed[index] != bare[index] {
			count++
		}
	}

	return count
}

// TestTubesDrawWhatTheFilterShows checks a route is drawn in every view, that
// a ceiling takes the top off it, that a route too faint to see, one with no
// decoded altitude and a transit route draw nothing, and that a trail a route has taken in is
// not drawn again as a hairline.
func TestTubesDrawWhatTheFilterShows(t *testing.T) {
	t.Parallel()

	tubed := exposureTrack(source.TrackInbound, 0)
	tubed.Tubed, tubed.TubeTo = true, len(tubed.Points)-1

	undecoded := tubeRoute(source.TrackOutbound, 1)
	for index := range undecoded.Stations {
		undecoded.Stations[index].Altitude = 0
	}

	for _, view := range []View{ViewScope, ViewMinimal, View3D, ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			// The trail is there so the corridors count as drawn at all.
			trails := []source.Trail{tubed}
			if got := tubePixels(t, view, nil, trails, FilterAll); got != 0 {
				t.Errorf("a tubed trail with no route drew %d pixels", got)
			}

			faint := []source.Route{
				tubeRoute(source.TrackInbound, 0.01), undecoded, tubeRoute(source.TrackTransit, 1),
			}
			if got := tubePixels(t, view, faint, trails, FilterAll); got != 0 {
				t.Errorf("a faint route, an undecoded one and a transit one drew %d pixels", got)
			}

			routes := []source.Route{tubeRoute(source.TrackInbound, 1)}
			all := tubePixels(t, view, routes, trails, FilterAll)
			under := tubePixels(t, view, routes, trails, FilterBelowHigh)

			if all == 0 || under == 0 || under >= all {
				t.Errorf("pixels: every band %d, under the ceiling %d; want both drawn and the ceiling fewer",
					all, under)
			}
		})
	}
}

// TestTubedTrailKeepsItsLooseEnds checks a member of a route is drawn as a
// hairline wherever its tube does not reach: nothing when the tube covers it
// end to end, the part before the tube when it covers only the middle, and
// all of it when it is in no tube. The tilted views draw no hairlines, so
// only the flat ones are asked.
func TestTubedTrailKeepsItsLooseEnds(t *testing.T) {
	t.Parallel()

	for _, view := range []View{ViewScope, ViewMinimal} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			count := func(from, to int, tubed bool) int {
				trail := exposureTrack(source.TrackInbound, 0)
				trail.Tubed, trail.TubeFrom, trail.TubeTo = tubed, from, to

				return tubePixels(t, view, nil, []source.Trail{trail}, FilterAll)
			}

			covered, middle, loose := count(0, 3, true), count(1, 2, true), count(0, 0, false)
			if covered != 0 || middle == 0 || middle >= loose {
				t.Errorf("hairline pixels: covered %d, middle covered %d, loose %d; want 0 < middle < loose",
					covered, middle, loose)
			}
		})
	}
}

// TestTubesCutAtTheScopeEdge checks a route running off the range ring, or
// through a station with no position, is cut there rather than dropped.
func TestTubesCutAtTheScopeEdge(t *testing.T) {
	t.Parallel()

	route := tubeRoute(source.TrackInbound, 1)
	route.Stations = route.Stations[:3]

	far := route.Stations[2]
	far.Latitude += 5

	unplaced := route.Stations[1]
	unplaced.Latitude, unplaced.Longitude = 0, 0

	route.Stations = append(route.Stations, far, unplaced, route.Stations[0])
	trails := []source.Trail{exposureTrack(source.TrackInbound, 0)}
	trails[0].Tubed, trails[0].TubeTo = true, len(trails[0].Points)-1

	for _, view := range []View{ViewScope, View3D} {
		if got := tubePixels(t, view, []source.Route{route}, trails, FilterAll); got == 0 {
			t.Errorf("%s: a route cut in three drew nothing", view)
		}
	}
}

// TestTubeHeading checks the direction across a station's neighbours, and
// that a route standing on one spot has none.
func TestTubeHeading(t *testing.T) {
	t.Parallel()

	place := func(station *source.Station) (float64, float64) { return station.Longitude, station.Latitude }

	north := []source.Station{{Latitude: 0, Longitude: 1}, {Latitude: 2, Longitude: 1}, {Latitude: 4, Longitude: 1}}
	if dx, dy := tubeHeading(north, 0, place); dx != 0 || dy != 1 {
		t.Errorf("first station of a northbound route heads (%g, %g), want (0, 1)", dx, dy)
	}

	if dx, dy := tubeHeading(north, 2, place); dx != 0 || dy != 1 {
		t.Errorf("last station of a northbound route heads (%g, %g), want (0, 1)", dx, dy)
	}

	still := []source.Station{{Latitude: 1, Longitude: 1}, {Latitude: 1, Longitude: 1}}
	if dx, dy := tubeHeading(still, 1, place); dx != 0 || dy != 0 {
		t.Errorf("a route on one spot heads (%g, %g), want none", dx, dy)
	}
}

// sideSection is a cross-section of a northbound tube of radius one at north,
// laid the way section lays one, with every vertex placed and each vertex's
// depth its own index.
func sideSection(north float64) tubeSection {
	var section tubeSection

	for side, turn := range tubeAround {
		section.vertices[side] = point3{east: -turn[0], north: north, up: turn[1]}
		section.depths[side] = float64(side)
		section.corners[side] = image.Point{X: side}
	}

	section.centre = point3{north: north}
	section.whole = true

	return section
}

// testStyle is a route's style at full strength in the night palette.
func testStyle(scene *Scene) tubeStyle {
	return scene.tubeStyleOf(&source.Route{Class: source.TrackInbound, Strength: 1})
}

// facesSeen collects the faces between two cross-sections of a northbound
// tube one mile long, seen from eye.
func facesSeen(eye point3) (*Scene, tubeStyle) {
	start, end := sideSection(0), sideSection(1)

	scene := &Scene{}
	scene.SetPalette(theme.Night)

	style := testStyle(scene)
	scene.collectStrips(&start, &end, eye, &style)

	return scene, style
}

// faceAlpha is the alpha the face from side was collected at. sideSection
// numbers its corners by side, so a face's first corner says which it is.
func faceAlpha(scene *Scene, side int) float64 {
	for _, patch := range scene.tubePatches {
		if scene.tubePoints[patch.from].X == side {
			return patch.alpha
		}
	}

	return 0
}

// wallSides is the sides of the faces in tubePatches[from:to].
func wallSides(scene *Scene, from, to int) []int {
	sides := make([]int, 0, to-from)

	for _, patch := range scene.tubePatches[from:to] {
		sides = append(sides, scene.tubePoints[patch.from].X)
	}

	return sides
}

// checkCompounded checks every near wall face facesSeen collected from eye
// carries its own alpha compounded with the far wall's behind it.
func checkCompounded(t *testing.T, scene *Scene, style *tubeStyle, eye point3) {
	t.Helper()

	start, end := sideSection(0), sideSection(1)

	for side := 3; side <= 8; side++ {
		cos := faceCos(start.vertices[side], start.vertices[side+1], end.vertices[side], end.vertices[side+1], eye)
		near, far := rimAlpha(style.nearBase, tubeNearFloor, cos), rimAlpha(style.farBase, tubeFarFloor, cos)

		if got, want := faceAlpha(scene, side), 1-(1-near)*(1-far); math.Abs(got-want) > 1e-12 || got <= near {
			t.Errorf("face %d alpha %g, want the near wall's %g compounded with the far wall's %g, %g",
				side, got, near, far, want)
		}
	}
}

// TestCollectStripsDrawsBothWalls checks a tube seen side on fills the near
// wall's six faces in one group, one face to a patch, each at its own alpha
// compounded with the far wall's behind it, with an edge either side; that an
// eye inside the tube, which sees no near wall, fills nothing; and that a ring
// of one point has no faces.
func TestCollectStripsDrawsBothWalls(t *testing.T) {
	t.Parallel()

	fromEast, style := facesSeen(point3{east: 100, north: 0.5})

	if len(fromEast.tubeGroups) != 1 || len(fromEast.tubeOrders) != 1 {
		t.Fatalf("groups %d, orders %d; want one of each", len(fromEast.tubeGroups), len(fromEast.tubeOrders))
	}

	// Faces 3 to 8 are the ones on the east half.
	group := fromEast.tubeGroups[0]
	if got := wallSides(fromEast, group.faces, group.facesEnd); !slices.Equal(got, []int{3, 4, 5, 6, 7, 8}) {
		t.Errorf("faces filled %v, want the east half", got)
	}

	if got := len(fromEast.tubeOver); got != 2 || group.overEnd != 2 || len(fromEast.tubeUnder) != 0 {
		t.Errorf("silhouette edges = %d over the wall, %d under; want 2 over", got, len(fromEast.tubeUnder))
	}

	// Faces 3 and 8 are on the rim and face 5 looks at the eye.
	if rim, middle := faceAlpha(fromEast, 3), faceAlpha(fromEast, 5); rim <= middle {
		t.Errorf("alpha %g at the rim, %g facing the eye; want the rim denser", rim, middle)
	}

	checkCompounded(t, fromEast, &style, point3{east: 100, north: 0.5})

	for _, patch := range fromEast.tubePatches {
		if patch.to-patch.from != 4 {
			t.Errorf("a face has %d corners, want 4", patch.to-patch.from)
		}
	}

	inside, _ := facesSeen(point3{north: 0.5})
	if len(inside.tubePatches) != 0 || len(inside.tubeOver) != 0 {
		t.Errorf("from inside: %d faces, %d edges; want none", len(inside.tubePatches), len(inside.tubeOver))
	}

	var point tubeSection

	scene, _ := facesSeen(point3{})
	scene.collectStrips(&point, &point, point3{east: 1}, &style)

	if len(scene.tubePatches)+len(scene.tubeOver) != 0 {
		t.Error("a ring of one point drew faces")
	}

	if farthestFirst(tubeOrder{depth: 2}, tubeOrder{depth: 1}) >= 0 {
		t.Error("farthestFirst put the nearer group first")
	}
}

// TestRimAlpha checks the rim shading thins away from the silhouette, reads
// the same either side of the wall, keeps its floor on the face looking at
// the camera and stops at the cap.
func TestRimAlpha(t *testing.T) {
	t.Parallel()

	const base = 0.075

	last := math.Inf(1)

	for step := range 11 {
		cos := float64(step) / 10

		got := rimAlpha(base, tubeNearFloor, cos)
		if got > last {
			t.Errorf("rimAlpha at |cos| %g = %g, above %g a step nearer the rim", cos, got, last)
		}

		if mirror := rimAlpha(base, tubeNearFloor, -cos); mirror != got {
			t.Errorf("rimAlpha(%g) = %g but rimAlpha(%g) = %g", cos, got, -cos, mirror)
		}

		last = got
	}

	if got, want := rimAlpha(base, tubeNearFloor, 1), base*tubeNearFloor*tubeRimGain; math.Abs(got-want) > 1e-12 {
		t.Errorf("rimAlpha facing the camera = %g, want the floor %g", got, want)
	}

	if got, want := rimAlpha(base, tubeNearFloor, 0), base*tubeRimGain; math.Abs(got-want) > 1e-12 {
		t.Errorf("rimAlpha edge on = %g, want %g", got, want)
	}

	if got := rimAlpha(1, tubeNearFloor, 0); got != tubeRimCap {
		t.Errorf("rimAlpha on a solid wall = %g, want the cap %g", got, tubeRimCap)
	}
}

// TestFaceCos checks the cosine's sign on both walls and that a face with no
// area is edge on.
func TestFaceCos(t *testing.T) {
	t.Parallel()

	start, end := sideSection(0), sideSection(1)
	eye := point3{east: 100, north: 0.5}

	// Vertex 0 is on the west side, so the face from it faces away from an
	// eye in the east, and the face from vertex 6 towards it.
	if got := faceCos(start.vertices[0], start.vertices[1], end.vertices[0], end.vertices[1], eye); got >= 0 {
		t.Errorf("the west face's cosine = %g, want negative", got)
	}

	if got := faceCos(start.vertices[6], start.vertices[7], end.vertices[6], end.vertices[7], eye); got <= 0 {
		t.Errorf("the east face's cosine = %g, want positive", got)
	}

	var point point3
	if got := faceCos(point, point, point, point, eye); got != 0 {
		t.Errorf("a face with no area has cosine %g, want 0", got)
	}
}

// TestCollectRingSplitsTheSides checks a ring seen side on is twelve
// segments added to the group before it, the six facing the eye strong and
// over the near wall, the six behind faint and under it.
func TestCollectRingSplitsTheSides(t *testing.T) {
	t.Parallel()

	scene, style := facesSeen(point3{east: 100, north: 0.5})
	section := sideSection(0)
	edges := len(scene.tubeOver)

	scene.collectRing(&section, point3{east: 100}, &style)

	group := scene.tubeGroups[0]
	over, under := scene.tubeOver[edges:group.overEnd], scene.tubeUnder[group.under:group.underEnd]

	if len(over) != tubeSides/2 || len(under) != tubeSides/2 {
		t.Fatalf("segments over the wall %d, under it %d; want %d each", len(over), len(under), tubeSides/2)
	}

	for index := range over {
		if over[index].ink != style.ringNear || under[index].ink != style.ringFar {
			t.Errorf("segment %d inks %v over, %v under; want %v and %v",
				index, over[index].ink, under[index].ink, style.ringNear, style.ringFar)
		}
	}
}

// ringView is a tilted view through testCamera over the layer fixture's
// receiver, everything inside its reach.
func ringView() scene3 {
	return scene3{
		cam:     testCamera(0, defaultElevation),
		origin:  geo{lat: layerBaseLat, lon: layerBaseLon},
		cosLat0: math.Cos(layerBaseLat * math.Pi / halfCircle),
		scopeNm: cameraRangeNm,
		reachNm: cameraRangeNm,
		upScale: DefaultExaggerate / ftPerNm,
		weight:  3,
	}
}

// ringRoute is an inbound route of count stations a mile apart, running
// north across the receiver at 5,000 ft with a spread of radius.
func ringRoute(count int, radius float64) source.Route {
	stations := make([]source.Station, 0, count)

	for index := range count {
		stations = append(stations, source.Station{
			Latitude:  layerBaseLat + float64(index-count/2)/nmPerDegree,
			Longitude: layerBaseLon,
			Altitude:  5000,
			SpreadNm:  radius,
			SpreadFt:  radius * 1000,
			Members:   2,
		})
	}

	return source.Route{Class: source.TrackInbound, Strength: 1, Stations: stations}
}

// ringsIn counts the rings a skinned route collected, as its ring segments
// over the twelve a ring has.
func ringsIn(scene *Scene, style tubeStyle) int {
	segments := 0

	for _, line := range slices.Concat(scene.tubeOver, scene.tubeUnder) {
		if line.ink == style.ringNear || line.ink == style.ringFar {
			segments++
		}
	}

	return segments / tubeSides
}

// skinned is the scene and style after skinning route on ringView with the
// stations in filtered given an undecoded altitude.
func skinned(t *testing.T, count int, filtered ...int) (*Scene, tubeStyle, source.Route) {
	t.Helper()

	scene := &Scene{}
	scene.SetPalette(theme.Night)
	style := testStyle(scene)

	if style.edge == style.ringNear || style.edge == style.ringFar {
		t.Fatal("the edge and ring inks match, so a ring count cannot tell them apart")
	}

	route := ringRoute(count, 1)
	for _, index := range filtered {
		route.Stations[index].Altitude = 0
	}

	scene.skinRoute(ringView(), &route, allBands, true)

	return scene, style, route
}

// TestSkinRouteRings checks the only ring a route gets is the mouth at its
// field end, whatever breaks the rest of it, and that a station standing
// alone gets none.
func TestSkinRouteRings(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		count    int
		filtered []int
		want     int
	}{
		{name: "ten stations", count: 10, want: 1},
		{name: "a run broken at station five", count: 10, filtered: []int{5}, want: 1},
		{name: "two runs broken twice", count: 12, filtered: []int{3, 8}, want: 1},
		{name: "the field end standing alone", count: 3, filtered: []int{1}, want: 0},
	} {
		scene, style, _ := skinned(t, testCase.count, testCase.filtered...)

		if got := ringsIn(scene, style); got != testCase.want {
			t.Errorf("%s: %d rings, want %d", testCase.name, got, testCase.want)
		}
	}
}

// TestRunCutGetsNoMouth checks a run that starts away from the field gets no
// ring at either end: neither where the filter cut it nor where the route
// runs out.
func TestRunCutGetsNoMouth(t *testing.T) {
	t.Parallel()

	for _, filtered := range [][]int{{0}, {0, 5}, {1}} {
		scene, style, _ := skinned(t, 10, filtered...)

		if got := ringsIn(scene, style); got != 0 {
			t.Errorf("cut at %v: %d rings, want none", filtered, got)
		}

		if len(scene.tubePatches) == 0 {
			t.Errorf("cut at %v: no walls, want the runs past the cut skinned", filtered)
		}
	}
}

// TestFieldEndKeepsItsMouth checks the one ring a whole route gets stands on
// station 0, by the centre of its segments on screen.
func TestFieldEndKeepsItsMouth(t *testing.T) {
	t.Parallel()

	scene, style, route := skinned(t, 10)
	view := ringView()

	var sumX, sumY float64

	segments := 0

	for _, line := range slices.Concat(scene.tubeOver, scene.tubeUnder) {
		if line.ink == style.ringNear || line.ink == style.ringFar {
			sumX += line.x0 + line.x1
			sumY += line.y0 + line.y1
			segments++
		}
	}

	if segments != tubeSides {
		t.Fatalf("%d ring segments, want one ring of %d", segments, tubeSides)
	}

	ringX, ringY := sumX/float64(2*segments), sumY/float64(2*segments)
	nearest, best := -1, math.Inf(1)

	for index := range route.Stations {
		point := &route.Stations[index]
		x, y, _ := view.cam.at(view.world(point.Latitude, point.Longitude, point.Altitude))

		if distance := math.Hypot(float64(x)-ringX, float64(y)-ringY); distance < best {
			nearest, best = index, distance
		}
	}

	if nearest != 0 {
		t.Errorf("the ring stands on station %d, want the field end", nearest)
	}
}

// TestSkinStationEndsTheRunOffThePicture checks a cross-section the camera
// cannot place whole ends the run without a mouth.
func TestSkinStationEndsTheRunOffThePicture(t *testing.T) {
	t.Parallel()

	scene := &Scene{}
	scene.SetPalette(theme.Night)
	style := testStyle(scene)

	route := ringRoute(1, 1)
	walk := tubeWalk{length: 3, last: 2}
	blind := scene3{upScale: DefaultExaggerate / ftPerNm}

	scene.skinStation(&walk, blind, 3, point3{}, 0, 1, &route.Stations[0], &style)

	if strokes := len(scene.tubeOver) + len(scene.tubeUnder); walk.length != 0 || strokes != 0 {
		t.Errorf("run length %d, %d strokes; want the run ended with no mouth", walk.length, strokes)
	}
}

// TestTubeScale checks the taper: full strength inside a run, a quarter on
// the route's last station, rising by a quarter a station back from a cut,
// the nearer cut winning, and the field end left alone.
func TestTubeScale(t *testing.T) {
	t.Parallel()

	shownBut := func(count int, hidden ...int) []bool {
		shown := make([]bool, count)
		for index := range shown {
			shown[index] = !slices.Contains(hidden, index)
		}

		return shown
	}

	for _, testCase := range []struct {
		name    string
		shown   []bool
		station int
		want    float64
	}{
		{name: "an interior station", shown: shownBut(12), station: 5, want: 1},
		{name: "the last station", shown: shownBut(12), station: 11, want: 0.25},
		{name: "one back from a cut", shown: shownBut(12, 8), station: 7, want: 0.25},
		{name: "two back from a cut", shown: shownBut(12, 8), station: 6, want: 0.5},
		{name: "three back from a cut", shown: shownBut(12, 8), station: 5, want: 0.75},
		{name: "one past a cut", shown: shownBut(12, 3), station: 4, want: 0.25},
		{name: "a cut both sides", shown: shownBut(12, 3, 6), station: 5, want: 0.25},
		{name: "a cut both sides, the far one nearer", shown: shownBut(12, 2, 6), station: 4, want: 0.5},
		{name: "the field end with station 1 hidden", shown: shownBut(12, 1), station: 0, want: 0.25},
		{name: "the field end", shown: shownBut(12), station: 0, want: 1},
	} {
		if got := tubeScale(testCase.shown, testCase.station); got != testCase.want {
			t.Errorf("%s: scale %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// TestTaperScalesTheStyle checks a scale of 1 hands the style back as it was,
// and a half halves the walls and fades the edge and the centreline again
// from the ink, leaving the rings alone.
func TestTaperScalesTheStyle(t *testing.T) {
	t.Parallel()

	scene := &Scene{}
	scene.SetPalette(theme.Night)
	style := testStyle(scene)

	if got := scene.taper(&style, 1); got != style {
		t.Errorf("scale 1 gave %+v, want the style unchanged", got)
	}

	half := scene.taper(&style, halfway)
	want := style
	want.nearBase, want.farBase = style.nearBase/2, style.farBase/2
	want.edge = scene.fade(style.ink, tubeEdgeStrength*halfway)
	want.line = scene.fade(style.ink, tubeLineStrength*halfway)

	if half != want {
		t.Errorf("scale 0.5 gave %+v, want %+v", half, want)
	}

	if half.edge == style.edge || half.line == style.line {
		t.Error("the tapered edge or line is the full-strength ink")
	}
}

// TestTubeDepthOrder checks the near wall goes over the lines behind it and
// under the lines in front of it: the middle of a ring's farthest segment is
// tinted by the wall in front of it, and the middle of its nearest segment is
// drawn in the ring's own ink.
func TestTubeDepthOrder(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(cameraSide, cameraSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := &Scene{corridors: true}
	scene.SetPalette(theme.Night)
	style := testStyle(scene)

	// A fat tube, so its rings stand clear of the centreline on screen,
	// reversed so its field end, the one ring it gets, is the end away from
	// the camera and the wall lies over that ring's far side.
	route := ringRoute(9, 4)
	slices.Reverse(route.Stations)
	scene.exposure.routes = []source.Route{route}
	scene.drawTubes3(canv, ringView(), allBands)

	nearest, farthest := -1, -1

	for index, line := range scene.tubeOver {
		if line.ink == style.ringNear && (nearest < 0 || line.y0 > scene.tubeOver[nearest].y0) {
			nearest = index
		}
	}

	for index, line := range scene.tubeUnder {
		if farthest < 0 || line.y0 < scene.tubeUnder[farthest].y0 {
			farthest = index
		}
	}

	if nearest < 0 || farthest < 0 {
		t.Fatal("the tube drew no rings")
	}

	middle := func(line tubeLine) color.RGBA {
		return canv.Image().RGBAAt(int(math.Round((line.x0+line.x1)/2)), int(math.Round((line.y0+line.y1)/2)))
	}

	if got := middle(scene.tubeOver[nearest]); got != style.ringNear {
		t.Errorf("the nearest ring segment reads %v, want its own ink %v over the wall", got, style.ringNear)
	}

	if got := middle(scene.tubeUnder[farthest]); got == style.ringFar {
		t.Errorf("the farthest ring segment reads its own ink %v, want the near wall over it", got)
	}
}

// TestTubesKeyTheLayer checks a new set of routes rebuilds the flat views'
// layer.
func TestTubesKeyTheLayer(t *testing.T) {
	t.Parallel()

	scene, canv, src := exposedLayerScene(t)
	src.frame.Routes = []source.Route{tubeRoute(source.TrackInbound, 1)}
	src.frame.RouteGeneration = 1

	scene.Draw(canv, 0)
	scene.Draw(canv, 0)

	src.frame.RouteGeneration++

	scene.Draw(canv, 0)

	if scene.layerRuns != 2 {
		t.Errorf("layerRuns = %d, want 2", scene.layerRuns)
	}
}

// TestFlushBandNeedsTwoStations checks a run of one station draws nothing
// and leaves the run empty for the next.
func TestFlushBandNeedsTwoStations(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(16, 16)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := &Scene{}
	scene.tubeLeft = append(scene.tubeLeft, image.Point{X: 2, Y: 2})
	scene.tubeRight = append(scene.tubeRight, image.Point{X: 4, Y: 4})
	scene.tubeCentre = append(scene.tubeCentre, [2]float64{3, 3})

	scene.flushBand(canv, canv.Bounds(), theme.Night.Data, 1, 1)

	for _, value := range canv.Image().Pix {
		if value != 0 {
			t.Fatal("a one-station run drew something")
		}
	}

	if len(scene.tubeLeft)+len(scene.tubeRight)+len(scene.tubeCentre) != 0 {
		t.Error("the run was not emptied")
	}
}

// TestTubesCap checks the U cap's light follows the corridors and its word is
// its own.
func TestTubesCap(t *testing.T) {
	t.Parallel()

	scene := &Scene{corridors: true}

	if !scene.capEngaged(capCorridors) {
		t.Error("the corridors are on and the cap says off")
	}

	scene.corridors = false
	if scene.capEngaged(capCorridors) {
		t.Error("the corridors are off and the cap says on")
	}

	if got := scene.capLabel(flatCaps[0]); got != tubesBarLabel {
		t.Errorf("the cap reads %q, want %q", got, tubesBarLabel)
	}
}

// TestTubesKeyEveryView checks u flips the one corridor switch in all four
// views. There is no bare pair the way a and m have one.
func TestTubesKeyEveryView(t *testing.T) {
	t.Parallel()

	for _, view := range []View{ViewScope, View3D, ViewMinimal, ViewMinimal3D} {
		scene := &Scene{shown: view, corridors: true}

		if !scene.handleRune('u') || scene.corridors {
			t.Errorf("%s: u left the corridors on", view)
		}

		if !scene.handleRune('U') || !scene.corridors {
			t.Errorf("%s: U left the corridors off", view)
		}
	}
}

// widestBarScene is the scene state every cycling cap is on its longest word
// in: corridor colour, the short trail, the mid-band filter, the phosphor look
// and the auto theme at night, on a source with a bias-tee.
func widestBarScene(t *testing.T, view View) (*Scene, layout) {
	t.Helper()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := &Scene{
		faces:         layerTestFaces(t),
		shown:         view,
		biasSupported: true,
		colour:        ColourCorridor,
		trail:         trailShort,
		filter:        filterState{kind: filterBand, band: bandMid},
	}
	scene.SetPalette(theme.PhosphorNight)
	scene.sunTheme.on = true

	return scene, layout{dst: canv, right: layerCanvasWidth - baseMargin}
}

// TestTubesCapFitsTheBarsItIsOn measures the two bars U TUBES is drawn on at
// their widest, the flat scope's with U on its tail and the bare 3D reveal's
// with U after M, and checks both stop inside the margin with the cap drawn.
// The full 3D bar has no U; TestDrawCapsFitsAtItsWidestState holds that one.
func TestTubesCapFitsTheBarsItIsOn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		view  View
		caps  []keyCap
		tail  []keyCap
		reach int
	}{
		{name: "flat scope", view: ViewScope, caps: keyCaps[:], tail: flatCaps[:], reach: 1045},
		{name: "bare 3D", view: ViewMinimal3D, caps: bareCaps[:], tail: bare3DCaps[:], reach: 1242},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, lay := widestBarScene(t, testCase.view)

			pen := scene.drawCaps(&lay, baseMargin, 0, testCase.caps)
			pen = scene.drawCaps(&lay, pen, 0, biasCap[:])
			pen = scene.drawCaps(&lay, pen, 0, testCase.tail)

			if pen >= lay.right {
				t.Errorf("the widest bar reached x=%d, want it to stop before the %d margin", pen, lay.right)
			}

			t.Logf("the widest %s bar ends at x=%d", testCase.name, pen)

			if pen < testCase.reach {
				t.Errorf("the widest bar ended at x=%d, before its last cap at %d", pen, testCase.reach)
			}
		})
	}
}

// TestTubeDrawn checks which routes get a tube: arrivals and departures
// strong enough to see, and never through traffic.
func TestTubeDrawn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		class    source.TrackClass
		strength float64
		want     bool
	}{
		{name: "inbound", class: source.TrackInbound, strength: 1, want: true},
		{name: "outbound", class: source.TrackOutbound, strength: 1, want: true},
		{name: "transit", class: source.TrackTransit, strength: 1, want: false},
		{name: "inbound too faint to see", class: source.TrackInbound, strength: 0.01, want: false},
	} {
		route := source.Route{Class: testCase.class, Strength: testCase.strength}
		if got := tubeDrawn(&route); got != testCase.want {
			t.Errorf("%s: tubeDrawn = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}
