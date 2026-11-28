package radar

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The two depths the scale is checked at, in nautical miles north of the
// receiver. Both are well inside cameraRangeNm, so the camera draws them
// both, and they are far enough apart that a scale that quietly depended on
// distance would be obvious.
const (
	modelNearNm = 10.0
	modelFarNm  = 30.0
)

// posedAt projects the model for one aircraft and hands back the scene the
// vertices landed on, so a case can read them out of scene.posed.
//
// The pose goes in as three angles rather than as a Snapshot because most of
// what is checked here is the geometry: a case that wanted to drive the
// vertical rate or the trail through the rules that read them says so by
// calling modelPitch or modelBank itself.
func posedAt(cam camera3, centre point3, headingDeg, pitchDeg, bankDeg float64) *Scene {
	scene := &Scene{}
	scene.projectModel3(cam, centre, newOrient3(headingDeg, pitchDeg, bankDeg, cam.spanScale(centre)))

	return scene
}

// TestModelNoseLeadsTheTail checks the one thing the shape has to get right
// before anything else is worth checking: which end of it is the front.
//
// The camera is the view's own at azimuth zero, which looks north from due
// south, so an aircraft heading north is pointing away from it and its nose
// draws further up the screen than its tail. Turned through 180 degrees the
// same aircraft is coming towards the camera and the two swap over. Nothing
// about the model, the projection or the yaw can be right if this is wrong,
// and a sign error in newOrient3 would show up here and almost nowhere else.
func TestModelNoseLeadsTheTail(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)
	centre := point3{north: modelNearNm}

	for _, testCase := range []struct {
		name       string
		headingDeg float64
		wantAbove  bool
	}{
		{name: "heading north, flying away from the camera", headingDeg: 0, wantAbove: true},
		{name: "heading south, flying towards it", headingDeg: 180},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := posedAt(cam, centre, testCase.headingDeg, 0, 0)

			if !scene.posed.seen[vertNose] || !scene.posed.seen[vertTail] {
				t.Fatalf("the camera refused the nose or the tail: nose %v, tail %v",
					scene.posed.seen[vertNose], scene.posed.seen[vertTail])
			}

			noseY, tailY := scene.posed.atY[vertNose], scene.posed.atY[vertTail]

			// Screen y grows downward, so "above" is the smaller number.
			if above := noseY < tailY; above != testCase.wantAbove {
				t.Errorf("nose at y=%d, tail at y=%d: nose above tail = %v, want %v",
					noseY, tailY, above, testCase.wantAbove)
			}
		})
	}
}

// TestModelSpanIsConstantOnScreen checks the rule that makes the model
// readable at all: it is drawn at a fixed size in pixels, not at a fixed size
// in the world.
//
// An airliner sixty metres across is a fraction of a pixel at thirty nautical
// miles, so a model drawn to scale would say nothing at all. spanScale divides
// the fixed pixel span by the projection's own scale at the aircraft's depth,
// which should leave the wingtips the same distance apart however far away the
// aeroplane is.
//
// A pixel of tolerance is allowed and no more. The two wingtips sit a little
// aft of the aircraft's centre, so they are fractionally nearer the camera
// than the point the scale was computed at, and the rounding to whole pixels
// costs half of one at each end.
func TestModelSpanIsConstantOnScreen(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)

	spanAt := func(tb testing.TB, northNm float64) int {
		tb.Helper()

		scene := posedAt(cam, point3{north: northNm}, 0, 0, 0)

		if !scene.posed.seen[vertWingTipLeft] || !scene.posed.seen[vertWingTipRight] {
			tb.Fatalf("the camera refused a wingtip at %g nm", northNm)
		}

		return scene.posed.atX[vertWingTipRight] - scene.posed.atX[vertWingTipLeft]
	}

	near, far := spanAt(t, modelNearNm), spanAt(t, modelFarNm)

	const tolerance = 1

	if diff := near - far; diff > tolerance || diff < -tolerance {
		t.Errorf("span at %g nm = %d px, at %g nm = %d px, want within %d",
			modelNearNm, near, modelFarNm, far, tolerance)
	}

	if diff := float64(near) - modelSpanPx; math.Abs(diff) > tolerance {
		t.Errorf("span at %g nm = %d px, want %g within %d", modelNearNm, near, modelSpanPx, tolerance)
	}
}

// TestModelFarWingIsTheOneAwayFromTheCamera checks the rule the painter's
// order and the shading both hang off.
//
// Shading one wing is the only cue a shape this small has for which way up
// and which way round it is, so getting the wrong wing would not merely look
// odd, it would say the opposite of the truth. An aircraft heading north away
// from a camera at azimuth zero has its right wing to the east, which is
// across the picture rather than away from it; turning the camera a quarter
// turn puts that same wing away from it.
func TestModelFarWingIsTheOneAwayFromTheCamera(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		azimuthDeg   float64
		headingDeg   float64
		wantFarRight bool
	}{
		{name: "nose-on, the left wing is fractionally further", azimuthDeg: 0, headingDeg: 0},
		{name: "seen from the east, the right wing is away", azimuthDeg: 90, headingDeg: 0, wantFarRight: true},
		{name: "seen from the west, the left wing is", azimuthDeg: 270, headingDeg: 0},
		{name: "turn the aircraft instead and it swaps back", azimuthDeg: 90, headingDeg: 180},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cam := testCamera(testCase.azimuthDeg, defaultElevation)
			scene := posedAt(cam, point3{}, testCase.headingDeg, 0, 0)

			if got := scene.posed.farRight; got != testCase.wantFarRight {
				t.Errorf("farRight = %v, want %v", got, testCase.wantFarRight)
			}
		})
	}
}

// TestModelPitch checks the three states the nose is drawn in and the exact
// rates they change at.
//
// The boundaries are tested on both sides because they are a plain comparison
// and a > that should have been a >= is the whole of what could go wrong. A
// rate of exactly modelClimbFpm is level: below three hundred feet a minute a
// barometric rate is noise, and the boundary belongs on the noise side of the
// line rather than the climbing one.
func TestModelPitch(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		rate float64
		want float64
	}{
		{name: "sitting still", rate: 0},
		{name: "a rate too small to mean anything", rate: modelClimbFpm - 1},
		{name: "exactly at the boundary is still level", rate: modelClimbFpm},
		{name: "one foot a minute past it is a climb", rate: modelClimbFpm + 1, want: modelPitchDeg},
		{name: "a real climb", rate: 2200, want: modelPitchDeg},
		{name: "exactly at the negative boundary is level", rate: -modelClimbFpm},
		{name: "one past it is a descent", rate: -modelClimbFpm - 1, want: -modelPitchDeg},
		{name: "a real descent", rate: -1800, want: -modelPitchDeg},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := modelPitch(testCase.rate); got != testCase.want {
				t.Errorf("modelPitch(%g) = %g, want %g", testCase.rate, got, testCase.want)
			}
		})
	}
}

// leg builds a run of fixes from a list of east and north offsets in degrees,
// so a case below can write the shape of a track rather than a table of
// coordinates.
func leg(offsets ...[2]float64) []airplane.PositionEntry {
	fixes := make([]airplane.PositionEntry, 0, len(offsets))
	for _, offset := range offsets {
		fixes = append(fixes, airplane.PositionEntry{Longitude: offset[0], Latitude: offset[1]})
	}

	return fixes
}

// TestModelBank checks the roll worked out from the last two legs of a trail.
//
// The turns are written as right angles because a right angle is the one
// heading change nobody has to check the arithmetic on: north then east is 90
// degrees to the right, which is three times modelTurnDeg and so well past the
// cap, and the same the other way round is a left turn of the same size.
//
// The cases that produce nothing matter as much as the ones that do. Two fixes
// is one leg and one leg is no turn; a repeated fix is an aircraft that did
// not move, which is a track with no bearing rather than a bearing of zero.
func TestModelBank(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		fixes []airplane.PositionEntry
		want  float64
	}{
		{name: "no history at all", fixes: nil},
		{name: "two fixes is one leg and no turn", fixes: leg([2]float64{0, 0}, [2]float64{0, 1})},
		{
			name:  "flying straight",
			fixes: leg([2]float64{0, 0}, [2]float64{0, 1}, [2]float64{0, 2}),
		},
		{
			name:  "a right angle to the right is past the cap",
			fixes: leg([2]float64{0, 0}, [2]float64{0, 1}, [2]float64{1, 1}),
			want:  modelBankDeg,
		},
		{
			name:  "a right angle to the left is past it the other way",
			fixes: leg([2]float64{0, 0}, [2]float64{0, 1}, [2]float64{-1, 1}),
			want:  -modelBankDeg,
		},
		{
			name:  "a fix that did not move leaves the leg into it with no bearing",
			fixes: leg([2]float64{0, 0}, [2]float64{0, 0}, [2]float64{0, 1}),
		},
		{
			name:  "a fix that did not move leaves the leg out of it with none either",
			fixes: leg([2]float64{0, 0}, [2]float64{0, 1}, [2]float64{0, 1}),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := modelBank(testCase.fixes); got != testCase.want {
				t.Errorf("modelBank(%v) = %g, want %g", testCase.fixes, got, testCase.want)
			}
		})
	}
}

// TestModelBankIsProportionalUnderTheCap checks the half of the rule the right
// angles above run straight past: a gentler turn banks less.
//
// A turn of exactly modelTurnDeg is the standard-rate one the cap is sized
// from, and half of it has to come out at half the bank. The tolerance is
// there because the track is worked out from positions through a cosine, not
// read off a heading field.
func TestModelBankIsProportionalUnderTheCap(t *testing.T) {
	t.Parallel()

	// Three fixes a degree apart: north, then a leg turned by the angle under
	// test. Working in degrees of latitude and longitude at the equator keeps
	// the cosine correction at one, so the track is the angle it looks like.
	turned := func(byDeg float64) []airplane.PositionEntry {
		sin, cos := math.Sincos(byDeg * math.Pi / halfCircle)

		return leg([2]float64{0, 0}, [2]float64{0, 1}, [2]float64{sin, 1 + cos})
	}

	for _, testCase := range []struct {
		name   string
		turnBy float64
		want   float64
	}{
		{name: "half a standard-rate turn", turnBy: modelTurnDeg / 2, want: modelBankDeg / 2},
		{name: "a standard-rate turn reaches the cap", turnBy: modelTurnDeg, want: modelBankDeg},
		{name: "the same to the left", turnBy: -modelTurnDeg / 2, want: -modelBankDeg / 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const tolerance = 0.5

			got := modelBank(turned(testCase.turnBy))
			if math.Abs(got-testCase.want) > tolerance {
				t.Errorf("a %g degree turn banked %g, want %g within %g",
					testCase.turnBy, got, testCase.want, tolerance)
			}
		})
	}
}

// TestSignedTurn checks the fold that stops an aircraft crossing north from
// reading as a turn most of the way round the compass.
func TestSignedTurn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "no change", in: 0},
		{name: "a small right turn", in: 20, want: 20},
		{name: "a small left turn", in: -20, want: -20},
		{name: "past the half circle folds the other way", in: 350, want: -10},
		{name: "and so does its negative", in: -350, want: 10},
		{name: "exactly half a circle stays positive", in: halfCircle, want: halfCircle},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := signedTurn(testCase.in); got != testCase.want {
				t.Errorf("signedTurn(%g) = %g, want %g", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestCellCameraIsTheSameEveryRow checks the property the attitude column is
// built on: two aircraft in the same attitude draw the same picture wherever
// their cells are on the page.
//
// It is the reason the cell has a camera of its own. The view's camera orbits,
// so an aircraft in it is seen from wherever the orbit has got to; a column of
// cells read down the page has to be seen from one angle throughout or the
// column says nothing.
func TestCellCameraIsTheSameEveryRow(t *testing.T) {
	t.Parallel()

	const (
		firstRow  = 100
		secondRow = 400
		column    = 1138
	)

	first := image.Pt(column, firstRow)
	second := image.Pt(column, secondRow)

	top := posedAt(newCellCamera3(first, 1), point3{}, 41, modelPitchDeg, 0)
	bottom := posedAt(newCellCamera3(second, 1), point3{}, 41, modelPitchDeg, 0)

	for index := range modelVertexCount {
		if !top.posed.seen[index] || !bottom.posed.seen[index] {
			t.Fatalf("vertex %d was refused: top %v, bottom %v",
				index, top.posed.seen[index], bottom.posed.seen[index])
		}

		wantX := top.posed.atX[index]
		wantY := top.posed.atY[index] + (secondRow - firstRow)

		if bottom.posed.atX[index] != wantX || bottom.posed.atY[index] != wantY {
			t.Errorf("vertex %d landed at (%d, %d) in the second cell, want (%d, %d)",
				index, bottom.posed.atX[index], bottom.posed.atY[index], wantX, wantY)
		}
	}
}

// TestCellModelFacesTheHeading is the attitude cell's own version of
// TestModelNoseLeadsTheTail, and the check the column exists to pass.
//
// The cell's camera is fixed with north up the page and east to the right, so
// a northbound aircraft draws its nose above its tail and an eastbound one
// draws it to the right. Every one of the four is worth a case: the camera is
// hand-built rather than solved by newCamera3, and a sign wrong in any of its
// three axes would leave one direction right and another mirrored.
func TestCellModelFacesTheHeading(t *testing.T) {
	t.Parallel()

	centre := image.Pt(120, 120)

	for _, testCase := range []struct {
		name       string
		headingDeg float64
		wantOffX   int
		wantOffY   int
	}{
		{name: "north puts the nose above the tail", headingDeg: 0, wantOffY: -1},
		{name: "east puts it to the right", headingDeg: 90, wantOffX: 1},
		{name: "south puts it below", headingDeg: 180, wantOffY: 1},
		{name: "west puts it to the left", headingDeg: 270, wantOffX: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := posedAt(newCellCamera3(centre, 1), point3{}, testCase.headingDeg, 0, 0)

			offX := sign(scene.posed.atX[vertNose] - scene.posed.atX[vertTail])
			offY := sign(scene.posed.atY[vertNose] - scene.posed.atY[vertTail])

			if offX != testCase.wantOffX || offY != testCase.wantOffY {
				t.Errorf("nose at (%d, %d), tail at (%d, %d): offset sign (%d, %d), want (%d, %d)",
					scene.posed.atX[vertNose], scene.posed.atY[vertNose],
					scene.posed.atX[vertTail], scene.posed.atY[vertTail],
					offX, offY, testCase.wantOffX, testCase.wantOffY)
			}
		})
	}
}

// sign reduces a pixel offset to -1, 0 or 1, so a case can say which side of
// something a vertex landed on without pinning how far.
func sign(value int) int {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	default:
		return 0
	}
}

// TestModelRollDropsTheInsideWing checks that the bank angle actually rolls
// the shape, and rolls it the way an aeroplane does.
//
// The sign is the whole of what could be wrong here and it is the half that
// matters: an aircraft banked into a right turn drops its right wing, and one
// drawn with the left wing down would be saying it was turning the other way.
// The cell's camera is used because it has north up and no orbit, so which
// wingtip is higher on the page is a question with one answer.
func TestModelRollDropsTheInsideWing(t *testing.T) {
	t.Parallel()

	centre := image.Pt(120, 120)

	for _, testCase := range []struct {
		name      string
		bankDeg   float64
		wantLower int
	}{
		{name: "banked right, the right wing goes down", bankDeg: modelBankDeg, wantLower: 1},
		{name: "banked left, the left wing does", bankDeg: -modelBankDeg, wantLower: -1},
		{name: "wings level, neither", bankDeg: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := posedAt(newCellCamera3(centre, 1), point3{}, 0, 0, testCase.bankDeg)

			rightY, leftY := scene.posed.atY[vertWingTipRight], scene.posed.atY[vertWingTipLeft]

			// Screen y grows downward, so the lower wing is the larger number.
			if got := sign(rightY - leftY); got != testCase.wantLower {
				t.Errorf("right wingtip at y=%d, left at y=%d: sign %d, want %d",
					rightY, leftY, got, testCase.wantLower)
			}
		})
	}
}

// TestCellCameraDrawsAtTheSpanItWasAsked checks the cell's model comes out at
// attSpanPx rather than at the perspective view's wider modelSpanPx.
//
// The span is the camera's rather than the model's, which is what lets one set
// of vertices serve a 24-pixel table cell and a whole picture. This is the
// check that says the field is actually read.
func TestCellCameraDrawsAtTheSpanItWasAsked(t *testing.T) {
	t.Parallel()

	scene := posedAt(newCellCamera3(image.Pt(100, 100), 1), point3{}, 0, 0, 0)

	got := scene.posed.atX[vertWingTipRight] - scene.posed.atX[vertWingTipLeft]

	const tolerance = 1

	if diff := float64(got) - attSpanPx; math.Abs(diff) > tolerance {
		t.Errorf("the cell drew a span of %d px, want %g within %d", got, attSpanPx, tolerance)
	}

	if float64(got) >= modelSpanPx {
		t.Errorf("the cell drew a span of %d px, want less than the view's %g", got, modelSpanPx)
	}
}

// TestProjectModelDropsWhatTheCameraRefuses checks the rule every polyline in
// the 3D view already follows, applied to the model's vertices: a vertex the
// camera will not have is marked unseen rather than clamped onto the edge of
// the picture.
//
// A clamped vertex would drag a face or an edge to a place no part of the
// aeroplane is, which is worse than the gap dropping it leaves. The aircraft
// here is put behind the camera, where at() refuses on depth.
func TestProjectModelDropsWhatTheCameraRefuses(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)

	// The camera sits south of the receiver looking north, so a point far
	// south of it is behind the lens.
	behind := point3{north: -10 * cameraRangeNm}

	scene := &Scene{}
	scene.projectModel3(cam, behind, newOrient3(0, 0, 0, 1))

	for index := range modelVertexCount {
		if scene.posed.seen[index] {
			t.Errorf("vertex %d behind the camera was marked seen, want it dropped", index)
		}
	}
}

// benchFleet3 is the fleet BenchmarkModels3 draws: 120 aircraft spread over a
// 40 nm scope at every heading and a spread of heights, in the world the
// camera looks at, with the pixel each one lands on.
type benchFleet3 struct {
	planes  [benchModels]airplane.Snapshot
	centres [benchModels]point3
	at      [benchModels]image.Point
}

// benchModels is how many aircraft a busy stream frame carries.
const benchModels = 120

// newBenchFleet3 places the fleet on a grid inside the ring, eleven across,
// so none of them is refused by the camera and every heading is drawn.
func newBenchFleet3(tb testing.TB, cam camera3) benchFleet3 {
	tb.Helper()

	const (
		across  = 11
		spacing = 6.0
		headDeg = 3.0
		climb   = 1500.0
	)

	var fleet benchFleet3

	for index := range benchModels {
		row, col := index/across, index%across
		altFt := 2000 + float64(index%13)*3000

		fleet.centres[index] = point3{
			east:  (float64(col) - across/2) * spacing,
			north: (float64(row) - across/2) * spacing,
			up:    altFt * DefaultExaggerate / ftPerNm,
		}
		fleet.planes[index] = airplane.Snapshot{
			Heading:  float64(index) * headDeg,
			VertRate: float64(index%3-1) * climb,
		}

		x, y, ok := cam.at(fleet.centres[index])
		if !ok {
			tb.Fatalf("aircraft %d at %+v is off the camera", index, fleet.centres[index])
		}

		fleet.at[index] = image.Pt(x, y)
	}

	return fleet
}

// BenchmarkModels3 prices the aircraft models alone, 120 of them on a
// 2560x1440 canvas, at the span the panel draws (scale 1) and the span the
// stream draws (scale 2).
func BenchmarkModels3(b *testing.B) {
	const width, height = 2560, 1440

	for _, bench := range []struct {
		name  string
		scale int
	}{
		{name: "scale 1", scale: 1},
		{name: "scale 2", scale: 2},
	} {
		b.Run(bench.name, func(b *testing.B) {
			cam, built := newCamera3(image.Rect(0, 0, width, height), framing3{
				scopeNm:     cameraRangeNm,
				azimuth:     200,
				elevation:   defaultElevation,
				topNm:       cameraTopNm,
				topRadiusNm: bowlRadiusNm(bowlTopFt, cameraRangeNm),
			})
			if !built {
				b.Fatal("newCamera3 refused the bench box")
			}

			cam.modelSpan = modelSpanPx * float64(bench.scale)

			fleet := newBenchFleet3(b, cam)

			canv, err := canvas.New(width, height)
			if err != nil {
				b.Fatal(err)
			}

			scene := &Scene{}
			scene.SetPalette(theme.VectorNight)

			ink := theme.VectorNight.Ink

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				for index := range benchModels {
					scene.drawShape3(canv, cam, fleet.planes[index], fleet.centres[index], fleet.at[index],
						ink, bench.scale)
				}
			}
		})
	}
}

// TestJetMeshFillsItsArrays checks the hand-counted sizes of the twin jet
// against what the build actually made. The counts size Scene.posed and the
// mesh's own arrays, so a table edited without them would either panic at
// init or leave a run of zero faces drawn at the origin.
func TestJetMeshFillsItsArrays(t *testing.T) {
	t.Parallel()

	if jet.vertexCount != jetVertexCount {
		t.Errorf("the build made %d vertices, jetVertexCount is %d", jet.vertexCount, jetVertexCount)
	}

	if jet.faceCount != jetFaceCount {
		t.Errorf("the build made %d faces, jetFaceCount is %d", jet.faceCount, jetFaceCount)
	}

	covered := make([]int, jetFaceCount)

	for index, part := range jet.parts {
		if part.end <= part.first {
			t.Errorf("part %d has no faces", index)
		}

		for face := part.first; face < part.end; face++ {
			covered[face]++
		}
	}

	for face, count := range covered {
		if count != 1 {
			t.Errorf("face %d is in %d parts, want exactly one", face, count)
		}
	}
}

// TestJetFacesAreConvexAndOutward checks the two things the painter's order
// rests on. Every face is filled as a fan, which is only right for a convex
// polygon, so every corner of every face has to turn the same way. And a
// closed part's normals have to point out of it, or culling would draw the
// inside of the tube and hide the outside.
func TestJetFacesAreConvexAndOutward(t *testing.T) {
	t.Parallel()

	const tolerance = 1e-9

	for index := range jetFaceCount {
		face := &jet.faces[index]

		if length := face.normal.dot(face.normal); math.Abs(length-1) > tolerance {
			t.Errorf("face %d's normal has a squared length of %g, want 1", index, length)
		}

		// faceOutwards may have turned the normal against the winding, so
		// what matters is that every corner turns the same way as the first.
		var first float64

		for corner := range face.count {
			here := jet.vertices[face.corners[corner]]
			next := jet.vertices[face.corners[(corner+1)%face.count]]
			after := jet.vertices[face.corners[(corner+2)%face.count]]

			turn := cross3(next.minus(here), after.minus(next)).dot(face.normal)
			if corner == 0 {
				first = turn
			}

			if turn*first < -tolerance {
				t.Errorf("face %d turns the other way at corner %d, so it is not convex", index, corner)
			}
		}
	}

	for index, part := range jet.parts {
		if !part.closed {
			continue
		}

		middle := partMiddle(part)

		for face := part.first; face < part.end; face++ {
			if out := jet.faces[face].normal.dot(faceMiddle(&jet.faces[face]).minus(middle)); out <= 0 {
				t.Errorf("part %d face %d points into the part", index, face)
			}
		}
	}
}

// cross3 is the vector product, which the convexity check needs and the
// draw path does not.
func cross3(first, second point3) point3 {
	return point3{
		east:  first.north*second.up - first.up*second.north,
		north: first.up*second.east - first.east*second.up,
		up:    first.east*second.north - first.north*second.east,
	}
}

// faceMiddle is the average of a face's corners.
func faceMiddle(face *jetFace) point3 {
	var sum point3

	for _, corner := range face.corners[:face.count] {
		sum = point3{
			east:  sum.east + jet.vertices[corner].east,
			north: sum.north + jet.vertices[corner].north,
			up:    sum.up + jet.vertices[corner].up,
		}
	}

	count := float64(face.count)

	return point3{east: sum.east / count, north: sum.north / count, up: sum.up / count}
}

// partMiddle is the average of a part's face middles, which is inside a
// convex part.
func partMiddle(part jetPart) point3 {
	var sum point3

	for face := part.first; face < part.end; face++ {
		middle := faceMiddle(&jet.faces[face])
		sum = point3{east: sum.east + middle.east, north: sum.north + middle.north, up: sum.up + middle.up}
	}

	count := float64(part.end - part.first)

	return point3{east: sum.east / count, north: sum.north / count, up: sum.up / count}
}

// TestJetLampsSitWhereTheLightsGo checks the lights land on the twin jet's
// own wingtips, the end of its tail and the top of its body, which are not
// where the dart's are.
func TestJetLampsSitWhereTheLightsGo(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		vertex int
		want   point3
	}{
		{name: "port wingtip", vertex: jet.lamps.port, want: point3{east: -0.5, north: -0.2, up: jetWingUp(0.5)}},
		{
			name: "starboard wingtip", vertex: jet.lamps.starboard,
			want: point3{east: 0.5, north: -0.2, up: jetWingUp(0.5)},
		},
		{name: "tail", vertex: jet.lamps.tail, want: point3{north: -0.51, up: 0.042}},
		{name: "beacon", vertex: jet.lamps.beacon, want: jetBeaconAt},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const tolerance = 1e-12

			got := jet.vertices[testCase.vertex]
			if off := got.minus(testCase.want); off.dot(off) > tolerance {
				t.Errorf("vertex %d is at %+v, want %+v", testCase.vertex, got, testCase.want)
			}
		})
	}
}

// TestJetOrder checks the painter's order for each answer to the three
// questions it asks of the camera: which wing is further, whether the camera
// is above, and whether the nose points at it.
func TestJetOrder(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		look point3
		want [jetPartCount]int
	}{
		{
			name: "from above and behind, left wing away",
			look: point3{east: -0.5, north: 0.5, up: -0.5},
			want: [jetPartCount]int{
				jetEngineLeft, jetWingLeft, jetTailLeft, jetFuselage, jetFin,
				jetEngineRight, jetWingRight, jetTailRight,
			},
		},
		{
			name: "from above and in front, right wing away",
			look: point3{east: 0.5, north: -0.5, up: -0.5},
			want: [jetPartCount]int{
				jetTailRight, jetEngineRight, jetWingRight, jetFuselage, jetFin,
				jetTailLeft, jetEngineLeft, jetWingLeft,
			},
		},
		{
			name: "from below and behind, right wing away",
			look: point3{east: 0.5, north: 0.5, up: 0.5},
			want: [jetPartCount]int{
				jetWingRight, jetEngineRight, jetTailRight, jetFin, jetFuselage,
				jetWingLeft, jetEngineLeft, jetTailLeft,
			},
		},
		{
			name: "from below and in front, left wing away",
			look: point3{east: -0.5, north: -0.5, up: 0.5},
			want: [jetPartCount]int{
				jetTailLeft, jetWingLeft, jetEngineLeft, jetFin, jetFuselage,
				jetTailRight, jetWingRight, jetEngineRight,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, far := jetOrder(testCase.look)
			if got != testCase.want {
				t.Errorf("jetOrder = %v, want %v", got, testCase.want)
			}

			if far != 3 {
				t.Errorf("far = %d, want the three parts of one side", far)
			}
		})
	}
}

// TestJetInk checks the two ends of the shade scale on each kind of palette:
// on a night one the lit faces carry the full ink and the dim ones fade
// towards the field, on a day one it runs the other way round.
func TestJetInk(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		pal   theme.Palette
		shade float64
		alpha float64
	}{
		{name: "night, brightest", pal: theme.Night, shade: 1, alpha: 1},
		{name: "night, dimmest", pal: theme.Night, shade: jetDarkest, alpha: jetDarkest},
		{name: "day, brightest", pal: theme.Day, shade: 1, alpha: jetDayFloor},
		{name: "day, dimmest", pal: theme.Day, shade: jetDarkest, alpha: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)

			got := scene.jetInk(testCase.pal.Ink, testCase.shade)
			if want := scene.fade(testCase.pal.Ink, testCase.alpha); got != want {
				t.Errorf("jetInk(%g) = %v, want %v", testCase.shade, got, want)
			}
		})
	}
}

// jetTestCanvas is a canvas the size of the test camera's box, cleared to
// the night field.
func jetTestCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(cameraSide, cameraSide)
	if err != nil {
		tb.Fatal(err)
	}

	canv.Clear(theme.Night.Field)

	return canv
}

// nightPainted counts the pixels that are not the night field.
func nightPainted(canv *canvas.Canvas) int {
	img := canv.Image()
	count := 0

	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			if img.RGBAAt(x, y) != theme.Night.Field {
				count++
			}
		}
	}

	return count
}

// TestDrawModelPicksTheModelBySpan checks the threshold: the dart below
// jetMinSpanPx and the twin jet from it, told apart by the lamps each leaves
// in Scene.posed for the lights to read.
func TestDrawModelPicksTheModelBySpan(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		span float64
		want modelLamps
	}{
		{name: "the panel's sixteen pixels", span: modelSpanPx, want: dartLamps},
		{name: "just under the threshold", span: jetMinSpanPx - 0.5, want: dartLamps},
		{name: "at the threshold", span: jetMinSpanPx, want: jet.lamps},
		{name: "the stream's thirty-two", span: 2 * modelSpanPx, want: jet.lamps},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cam := testCamera(0, defaultElevation)
			cam.modelSpan = testCase.span

			scene := &Scene{}
			scene.SetPalette(theme.Night)

			canv := jetTestCanvas(t)
			scene.drawModel3(canv, cam, airplane.Snapshot{Heading: 60}, point3{north: modelNearNm},
				theme.Night.Ink, 1)

			if scene.posed.lamps != testCase.want {
				t.Errorf("lamps = %+v, want %+v", scene.posed.lamps, testCase.want)
			}

			if nightPainted(canv) == 0 {
				t.Error("drawModel3 painted nothing")
			}
		})
	}
}

// TestJetLampsLandOnTheWingtips checks the lights follow the twin jet's
// projected wingtips: a wing's span apart across the screen, the right wing
// to the right of a northbound aircraft seen from the south.
func TestJetLampsLandOnTheWingtips(t *testing.T) {
	t.Parallel()

	const span = 64.0

	cam := testCamera(0, defaultElevation)
	cam.modelSpan = span

	scene := &Scene{}
	scene.SetPalette(theme.Night)

	centre := point3{north: modelNearNm}
	scale := cam.spanScale(centre)
	scene.drawJet3(jetTestCanvas(t), cam, centre, newOrient3(0, 0, 0, scale), scale, theme.Night.Ink)

	left, right := scene.posed.lamps.port, scene.posed.lamps.starboard

	const tolerance = 2

	if got := scene.posed.atX[right] - scene.posed.atX[left]; math.Abs(float64(got)-span) > tolerance {
		t.Errorf("the wingtip lights are %d px apart, want %g within %d", got, span, tolerance)
	}
}

// TestJetShadesItsFaces checks the twin jet comes out in a spread of shades of
// one ink rather than flat, which is what the light and the tones are for.
func TestJetShadesItsFaces(t *testing.T) {
	t.Parallel()

	const span = 128.0

	cam := testCamera(0, defaultElevation)
	cam.modelSpan = span

	scene := &Scene{}
	scene.SetPalette(theme.Night)

	centre := point3{north: modelNearNm}
	scale := cam.spanScale(centre)
	canv := jetTestCanvas(t)
	scene.drawJet3(canv, cam, centre, newOrient3(70, 0, 0, scale), scale, theme.Night.Ink)

	shades := map[color.RGBA]bool{}
	img := canv.Image()

	for y := range cameraSide {
		for x := range cameraSide {
			if got := img.RGBAAt(x, y); got != theme.Night.Field {
				shades[got] = true
			}
		}
	}

	const atLeast = 8

	if len(shades) < atLeast {
		t.Errorf("the twin jet came out in %d colours, want at least %d shades", len(shades), atLeast)
	}
}

// TestJetDropsWhatTheCameraRefuses checks a twin jet behind the camera paints
// nothing, as the dart does: every face touches a refused vertex.
func TestJetDropsWhatTheCameraRefuses(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)
	cam.modelSpan = 2 * modelSpanPx

	scene := &Scene{}
	scene.SetPalette(theme.Night)

	canv := jetTestCanvas(t)
	scene.drawJet3(canv, cam, point3{north: -10 * cameraRangeNm}, newOrient3(0, 0, 0, 1), 1, theme.Night.Ink)

	if got := nightPainted(canv); got != 0 {
		t.Errorf("a twin jet behind the camera painted %d pixels, want none", got)
	}
}

// levelCamera is a cell camera at the height of its subject, so a wings-level
// aircraft's wings are seen almost exactly edge on.
func levelCamera(centre image.Point, span float64) camera3 {
	return camera3{
		eye:       point3{north: -attDepthNm},
		forward:   point3{north: 1},
		right:     point3{east: 1},
		above:     point3{up: 1},
		focal:     attDepthNm,
		centerX:   centre.X,
		centerY:   centre.Y,
		limitX:    cameraSide,
		limitY:    cameraSide,
		modelSpan: span,
	}
}

// TestJetEdgeOnWingStaysALine checks a wing seen edge on is drawn unbroken
// from the body to the tip. Filled by pixel centres alone it would be a dotted
// line or nothing, since its width on screen is a fraction of a pixel.
func TestJetEdgeOnWingStaysALine(t *testing.T) {
	t.Parallel()

	const span = 64

	centre := image.Pt(cameraSide/2, cameraSide/2)
	cam := levelCamera(centre, span)

	scene := &Scene{}
	scene.SetPalette(theme.Night)

	canv := jetTestCanvas(t)
	scale := cam.spanScale(point3{})
	scene.drawJet3(canv, cam, point3{}, newOrient3(0, 0, 0, scale), scale, theme.Night.Ink)

	img := canv.Image()

	// From the edge of the body to just short of the tip on the right, every
	// column holds something within a couple of rows of the wing's height.
	for column := centre.X + span/8; column < centre.X+span/2-1; column++ {
		found := false

		for row := centre.Y - 4; row <= centre.Y+4; row++ {
			if img.RGBAAt(column, row) != theme.Night.Field {
				found = true

				break
			}
		}

		if !found {
			t.Fatalf("column %d of the edge-on wing is empty, want an unbroken line to the tip", column)
		}
	}
}

// TestJetAllocations checks the twin jet keeps the draw path's rule: nothing
// is allocated per aircraft, at the stream's span or a close shot's.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestJetAllocations(t *testing.T) {
	cam := testCamera(200, defaultElevation)
	canv := jetTestCanvas(t)

	scene := &Scene{}
	scene.SetPalette(theme.Day)

	plane := airplane.Snapshot{Heading: 120, VertRate: 1500}
	centre := point3{north: modelNearNm}

	atX, atY, seen := cam.at(centre)
	if !seen {
		t.Fatal("the camera refused the test aircraft")
	}

	for _, span := range []float64{2 * modelSpanPx, 8 * modelSpanPx} {
		cam.modelSpan = span

		draw := func() { scene.drawShape3(canv, cam, plane, centre, image.Pt(atX, atY), theme.Day.Ink, 1) }
		if got := testing.AllocsPerRun(allocRuns3, draw); got != 0 {
			t.Errorf("drawing a twin jet %g px across allocated %.1f times, want 0", span, got)
		}
	}
}

// allocRuns3 is how many draws TestJetAllocations averages over.
const allocRuns3 = 20
