package radar

import (
	"image"
	"math"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
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

	top := posedAt(newCellCamera3(first), point3{}, 41, modelPitchDeg, 0)
	bottom := posedAt(newCellCamera3(second), point3{}, 41, modelPitchDeg, 0)

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

			scene := posedAt(newCellCamera3(centre), point3{}, testCase.headingDeg, 0, 0)

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

			scene := posedAt(newCellCamera3(centre), point3{}, 0, 0, testCase.bankDeg)

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

	scene := posedAt(newCellCamera3(image.Pt(100, 100)), point3{}, 0, 0, 0)

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
