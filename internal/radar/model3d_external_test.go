package radar_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// noHeading is uAirwaves' sentinel for an aircraft nobody has decoded a
// velocity message from yet. It starts an Airplane there and leaves it, so
// this is the value that arrives on a real feed rather than one invented for
// the test.
const noHeading = -1.0

// shapeExtent is the box every pixel inside box that is not the field occupies,
// or false when the whole box is field.
//
// It measures the whole of a shape rather than one colour of it. The model's
// far wing is mixed part-way into the field, so counting the aircraft's own
// colour would measure the near half of an aeroplane and call it the shape.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func shapeExtent(canv *canvas.Canvas, box image.Rectangle, field color.RGBA) (image.Rectangle, bool) {
	found := image.Rectangle{}
	drawn := false

	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == field {
				continue
			}

			pixel := image.Rect(x, y, x+1, y+1)
			if !drawn {
				found, drawn = pixel, true

				continue
			}

			found = found.Union(pixel)
		}
	}

	return found, drawn
}

// TestRowAttitudeDrawsTheHeading checks the little aeroplane at the end of a
// strip's ident field puts a different picture in the cell for every heading
// it is given.
//
// Which way round each of those pictures is, is TestCellModelFacesTheHeading's
// subject: it reads the projected nose and tail straight out of the model,
// which is a sharper question than a cell of pixels can answer. What this one
// adds is that the cell is wired to the heading at all, through the strip
// plan, the cell camera and the shared shape routine.
//
// The fixture flies a filler aircraft ahead of the one under test, so the one
// being read lands on a half strip rather than the selected one: the selected
// strip's own lifted fill sits behind everything in it, which would make a
// bare "was anything painted" comparison true whatever the heading, and a
// half strip carries no such fill.
func TestRowAttitudeDrawsTheHeading(t *testing.T) {
	t.Parallel()

	cell := func(tb testing.TB, heading float64) *canvas.Canvas {
		tb.Helper()

		plane := scenePlane(icaoSample, callsignSample, 45, 12, 2400, heading)

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, sceneFrame(stripFillerPlane(), plane))
		scene.Apply(bareScope())
		scene.Draw(canv, 0)

		if painted(canv, stripAttitudeCell) == 0 {
			tb.Fatalf("the attitude cell drew nothing at heading %g, want an aircraft in it", heading)
		}

		return canv
	}

	headings := []float64{0, 90, 180, 270}
	drawn := make([]*canvas.Canvas, 0, len(headings))

	for _, heading := range headings {
		drawn = append(drawn, cell(t, heading))
	}

	for left := range drawn {
		for right := left + 1; right < len(drawn); right++ {
			if identicalIn(drawn[left], drawn[right], stripAttitudeCell) {
				t.Errorf("headings %g and %g drew the same cell, want one turned to each",
					headings[left], headings[right])
			}
		}
	}
}

// TestRowAttitudeTakesTheAircraftColour checks the cell is painted the way the
// aeroplane on the field beside it is.
//
// It is the aircraft's own colour in both colour modes, and the selected
// strip is no exception: both the filler ahead of it and the aircraft under
// test fly the same altitude, so the check holds on the full strip's own
// attitude cell as well as on the half strip below it. The point of the cell
// is that reading the board and reading the scope are the same act of
// recognition, which a strip that changed colour when it was selected would
// break for the one strip it matters most on.
func TestRowAttitudeTakesTheAircraftColour(t *testing.T) {
	t.Parallel()

	// A cruising altitude, so the cell is in the high band rather than in the
	// low one the rest of these fixtures use.
	const high = 36000.0

	filler := scenePlane("AAA111", "FILLER1", 0, 5, high, 10)
	plane := scenePlane(icaoSample, callsignSample, 45, 12, high, 41)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(filler, plane))
	scene.Apply(bareScope())
	scene.Draw(canv, 0)

	for _, testCase := range []struct {
		name string
		box  image.Rectangle
	}{
		{name: "the selected strip", box: firstStripAttitudeCell},
		{name: "an unselected strip", box: stripAttitudeCell},
	} {
		if got := countColour(canv, testCase.box, theme.Night.AltHigh); got == 0 {
			t.Errorf("%s drew no %v pixels, want the aircraft's own band colour", testCase.name, theme.Night.AltHigh)
		}

		if got := countColour(canv, testCase.box, theme.Night.Ink); got != 0 {
			t.Errorf("%s drew %d ink pixels, want the band colour instead", testCase.name, got)
		}
	}
}

// TestUnknownHeadingDrawsTheDisc checks what stands in for the model when
// there is no attitude to pose one in.
//
// uAirwaves leaves an aircraft's heading at -1 until a velocity message
// arrives, which on a real feed is most of the first minute of a new contact.
// A model drawn pointing north anyway would be the one thing on the screen
// stating a fact nobody has, so both places that draw a model draw a small
// filled disc instead.
//
// The disc is round, so the test reads its extent: a model at any heading is
// longer in one direction than the other, and a disc is not. Measuring the
// extent against the field only works away from the selected strip's own
// lifted fill, which is why the aircraft under test flies behind a filler and
// lands on a half strip; see TestRowAttitudeDrawsTheHeading.
func TestUnknownHeadingDrawsTheDisc(t *testing.T) {
	t.Parallel()

	plane := scenePlane(icaoSample, callsignSample, 45, 12, 2400, noHeading)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(stripFillerPlane(), plane))
	scene.Apply(bareScope())
	scene.Draw(canv, 0)

	box, drawn := shapeExtent(canv, stripAttitudeCell, theme.Night.Field)
	if !drawn {
		t.Fatal("the attitude cell drew nothing for an aircraft with no heading, want the disc")
	}

	if box.Dx() != box.Dy() {
		t.Errorf("the shape measured %dx%d, want a round disc", box.Dx(), box.Dy())
	}

	// A filled disc of radius 3 is seven pixels across. Anything much larger
	// is a model that slipped through; anything smaller is not a disc.
	const wantSide = 7

	if box.Dx() != wantSide {
		t.Errorf("the disc measured %d px across, want %d", box.Dx(), wantSide)
	}
}

// TestView3DUnknownHeadingDrawsTheDisc is the same rule in the perspective
// view, which shares the shape routine with the table.
//
// It is checked separately because the two reach it by different routes: the
// table hands it the middle of a cell and a camera that never moves, and the
// view hands it a projected fix and a camera that orbits.
func TestView3DUnknownHeadingDrawsTheDisc(t *testing.T) {
	t.Parallel()

	known := scenePlane(icaoSample, callsignSample, 45, 12, 2400, 41)

	unknown := known
	unknown.Heading = noHeading

	draw := func(tb testing.TB, plane airplane.Snapshot) int {
		tb.Helper()

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, sceneFrame(plane))
		scene.Apply(radar.Settings{View: radar.View3D, RangeNm: sceneRangeNm})
		scene.Draw(canv, 0)

		return countColour(canv, view3DBox, theme.Night.AltLow)
	}

	withModel := draw(t, known)
	withDisc := draw(t, unknown)

	if withDisc == 0 {
		t.Fatal("the 3D view drew nothing for an aircraft with no heading, want the disc")
	}

	if withDisc == withModel {
		t.Errorf("an aircraft with no heading drew the same %d pixels as one with, want the disc instead",
			withDisc)
	}
}

// inkShades is how many different shades of ink, mixed part of the way
// towards field, the canvas holds: ink itself and the field are not counted.
//
// A colour is a shade when one share of ink explains all three channels to
// within a step of rounding. The twin jet paints every face in such a shade,
// a score of them for one aircraft, where the dart has its far wing and at
// most one more where a stroked edge happened to blend onto the line; so the
// count says which of the two models is on the canvas without having to know
// where it landed.
func inkShades(canv *canvas.Canvas, ink, field color.RGBA) int {
	seen := map[color.RGBA]bool{}
	img := canv.Image()

	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			got := img.RGBAAt(x, y)
			if got != ink && got != field && isShade(got, ink, field) {
				seen[got] = true
			}
		}
	}

	return len(seen)
}

// isShade reports whether got is field mixed towards ink by one share, to
// within a step of rounding on every channel.
func isShade(got, ink, field color.RGBA) bool {
	channels := [3][3]float64{
		{float64(got.R), float64(ink.R), float64(field.R)},
		{float64(got.G), float64(ink.G), float64(field.G)},
		{float64(got.B), float64(ink.B), float64(field.B)},
	}

	// The share is read off the channel where ink and field are furthest
	// apart, which is where rounding moves it least.
	widest := channels[0]
	for _, channel := range channels[1:] {
		if math.Abs(channel[1]-channel[2]) > math.Abs(widest[1]-widest[2]) {
			widest = channel
		}
	}

	share := (widest[0] - widest[2]) / (widest[1] - widest[2])
	if share <= 0 || share >= 1 {
		return false
	}

	for _, channel := range channels {
		if math.Abs(channel[2]+share*(channel[1]-channel[2])-channel[0]) > 1.5 {
			return false
		}
	}

	return true
}

// TestModelIsTheTwinJetOnlyWhenItIsLargeEnough checks the switch between the
// two models, by the shades each paints.
//
// At 1280x720 the model is sixteen pixels across and has to be the dart, to
// the byte, so the panel's picture does not move: its fills are the aircraft's
// colour and the far wing's one fade of it. At 3840x2160 the layout's scale is
// two, the model thirty-two pixels across, and the twin jet's faces each take
// their own shade by how they face the light.
func TestModelIsTheTwinJetOnlyWhenItIsLargeEnough(t *testing.T) {
	t.Parallel()

	plane := scenePlane(icaoSample, callsignSample, 45, 12, 2400, 41)
	plane.PositionHistory = nil

	for _, testCase := range []struct {
		name          string
		width, height int
		atLeast       int
		atMost        int
	}{
		{name: "the dart on the panel", width: panelWidth, height: panelHeight, atLeast: 1, atMost: 2},
		{name: "the twin jet at 4K", width: 3840, height: 2160, atLeast: 8, atMost: math.MaxInt},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, testCase.width, testCase.height, sceneFrame(plane))
			scene.Apply(radar.Settings{
				View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Lights: radar.LightsOff,
				Stars: radar.ToggleOff, Corridors: radar.ToggleOff, Orbit: radar.ToggleOff,
			})
			scene.Draw(canv, 0)

			got := inkShades(canv, theme.Night.AltLow, theme.Night.Field)
			if got < testCase.atLeast || got > testCase.atMost {
				t.Errorf("the model drew %d shades of its colour, want %d to %d",
					got, testCase.atLeast, testCase.atMost)
			}
		})
	}
}
