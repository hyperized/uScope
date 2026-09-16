package radar_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// view3DBox is the part of the panel-sized canvas the perspective scene is
// drawn in: the scope square and the air around it, but not the column beside
// it. It matches scopeBox's left edge and width; the height is the whole
// canvas because the camera puts the top of the envelope well above the square
// the flat scope keeps to.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var view3DBox = image.Rect(0, 0, 620, panelHeight)

// orbitQuarter is a quarter of the automatic orbit's two-minute revolution,
// which is far enough round for the picture to be obviously different.
const orbitQuarter = 30 * time.Second

// view3DSettings starts a scene in the perspective view at the tests' fixed
// range, so nothing depends on where auto range happened to land.
func view3DSettings() radar.Settings {
	return radar.Settings{View: radar.View3D, RangeNm: sceneRangeNm}
}

// covered adds a synthetic coverage snapshot to a frame: one altitude band
// heard twenty nautical miles out in every bearing sector.
//
// It is the smallest snapshot that draws a complete measured envelope, which
// is what the cases below need to see whether the envelope was drawn at all.
func covered(frame source.Frame) source.Frame {
	const (
		band    = 2
		bin     = 1
		heardNm = 20.0
	)

	frame.Coverage.Cells[band][bin] = 1

	for sector := range coverage.BearingSectorCount {
		frame.Coverage.Sectors[sector] = heardNm
	}

	return frame
}

// TestParseView checks the --view allow list. It is a list rather than free
// text, so a spelling that is nearly right is refused rather than guessed at.
func TestParseView(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   string
		want radar.View
	}{
		{name: "the scope", in: "scope", want: radar.ViewScope},
		{name: "minimal", in: "minimal", want: radar.ViewMinimal},
		{name: "the 3D view", in: "3d", want: radar.View3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := radar.ParseView(testCase.in)
			if err != nil {
				t.Fatalf("ParseView(%q) unexpected error: %v", testCase.in, err)
			}

			if got != testCase.want {
				t.Errorf("ParseView(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestParseViewRejections checks that ParseView refuses everything that is not
// one of the three exact spellings, the default included.
func TestParseViewRejections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "wrong case", in: "3D"},
		{name: "another wrong case", in: "Minimal"},
		{name: "a word for the same thing", in: "perspective"},
		{name: "a near miss", in: "3-d"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := radar.ParseView(testCase.in)
			if !errors.Is(err, radar.ErrView) {
				t.Fatalf("ParseView(%q) error = %v, want radar.ErrView", testCase.in, err)
			}

			// The refused value still comes back as the scope, so a caller
			// that reports the error and carries on has a usable view.
			if got != radar.ViewScope {
				t.Errorf("ParseView(%q) = %q on failure, want %q", testCase.in, got, radar.ViewScope)
			}
		})
	}
}

// TestViewNext checks the cycle v walks, including that the zero value moves
// the way the scope does. A View nobody set has to behave like the view a run
// starts on, or a Settings block built by a caller who only cared about the
// colour would start somewhere nobody asked for.
func TestViewNext(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   radar.View
		want radar.View
	}{
		{name: "the scope goes to minimal", in: radar.ViewScope, want: radar.ViewMinimal},
		{name: "minimal goes to 3D", in: radar.ViewMinimal, want: radar.View3D},
		{name: "3D comes back to the scope", in: radar.View3D, want: radar.ViewScope},
		{name: "the zero value moves like the scope", in: "", want: radar.ViewMinimal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.in.Next(); got != testCase.want {
				t.Errorf("View(%q).Next() = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestView3DDrawsTheFurniture checks that the perspective view puts its ground,
// its envelope and its traffic on the canvas, each in the colour it is meant
// to use.
func TestView3DDrawsTheFurniture(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	if got := countColour(canv, view3DBox, theme.Night.Rule); got == 0 {
		t.Error("the 3D view drew no rule-coloured pixels, want the range rings")
	}

	if got := countColour(canv, view3DBox, measuredWireInk); got == 0 {
		t.Error("the 3D view drew no measured-envelope pixels, want the wireframe")
	}

	if got := countColour(canv, view3DBox, bowlWireInk); got == 0 {
		t.Error("the 3D view drew no bowl pixels, want the theoretical envelope")
	}

	if got := countColour(canv, view3DBox, theme.Night.Muted); got == 0 {
		t.Error("the 3D view drew no muted pixels, want the aircraft stalks")
	}

	if got := countColour(canv, view3DBox, theme.Night.AltHigh); got == 0 {
		t.Error("the 3D view drew no high-altitude pixels, want the aircraft above 25,000 feet")
	}
}

// TestView3DEnvelopeToggle checks that e takes the envelope off the picture
// and puts it back, leaving everything else alone.
func TestView3DEnvelopeToggle(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	// The envelope has a colour of its own now that the wireframe is faded
	// into the field, where it used to share the accent with the selection
	// ring. Counting its own colour means e has to take every one of these
	// pixels away rather than merely some of them, which is a sharper check
	// than the comparison this test used to make.
	withEnvelope := countColour(canv, view3DBox, measuredWireInk)
	if withEnvelope == 0 {
		t.Fatal("the 3D view drew no envelope to toggle, so this proves nothing")
	}

	if !press(scene, 'e') {
		t.Fatal("Handle('e') = false, want the 3D view to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, view3DBox, measuredWireInk); got != 0 {
		t.Errorf("e left %d envelope pixels, want none", got)
	}

	// The selection ring is still drawn in the accent itself, so turning the
	// envelope off must not have taken the rest of the picture with it.
	if got := countColour(canv, view3DBox, theme.Night.Accent); got == 0 {
		t.Error("e took the accent off the picture as well, want the selection ring left alone")
	}

	press(scene, 'e')
	scene.Draw(canv, 0)

	if got := countColour(canv, view3DBox, measuredWireInk); got != withEnvelope {
		t.Errorf("e put %d envelope pixels back, want the original %d", got, withEnvelope)
	}
}

// TestView3DShoreToggle checks that the coastline is drawn on the ground of
// the perspective view, off the scope's own toggle rather than minimal's.
//
// The 3D view is a map with aircraft above it, which is what the scope is;
// minimal is aircraft with nothing behind them, which is why it keeps a pair
// of toggles of its own.
func TestView3DShoreToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	// Counted as painted pixels rather than as exact shore-coloured ones. In
	// perspective a north/south coastline converges towards the vanishing
	// point instead of running straight down the canvas, so every pixel of it
	// is a partial blend and almost none land on the palette colour exactly.
	withShore := painted(canv, view3DBox)

	press(scene, 'm')
	scene.Draw(canv, 0)

	if got := painted(canv, view3DBox); got >= withShore {
		t.Errorf("m left %d painted pixels against the original %d, want the coastline gone", got, withShore)
	}
}

// TestView3DAirportsToggle is TestView3DShoreToggle for a.
func TestView3DAirportsToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	withMarkers := countColour(canv, view3DBox, theme.Night.Rule)

	press(scene, 'a')
	scene.Draw(canv, 0)

	// The range rings are drawn in the same ink, so the markers going leaves
	// fewer rule pixels rather than none.
	if got := countColour(canv, view3DBox, theme.Night.Rule); got >= withMarkers {
		t.Errorf("a left %d rule pixels against the original %d, want fewer", got, withMarkers)
	}
}

// TestView3DTrailsToggle checks that t takes the trails and the ghosts off the
// perspective picture the same way it does on the flat one. A ghost is a
// trail, so the key that turns trails off has to take them with it.
func TestView3DTrailsToggle(t *testing.T) {
	t.Parallel()

	plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)
	ghost := ghostOf(scenePlane("3C6745", "DLH4EA", 200, 30, 36000, 268))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, ghostFrame([]source.Trail{ghost}, plane))
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	withTrails := painted(canv, view3DBox)

	press(scene, 't')
	scene.Draw(canv, 0)

	if got := painted(canv, view3DBox); got >= withTrails {
		t.Errorf("t left %d painted pixels against the original %d, want fewer", got, withTrails)
	}
}

// TestView3DSelection checks that the selected aircraft keeps its ring and its
// callsign in the perspective view.
//
// Minimal drops both because it has no panel for them to refer to. The 3D view
// keeps the column, so the marker still has something to point at.
func TestView3DSelection(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	flat, flatCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	flat.Apply(radar.Settings{RangeNm: sceneRangeNm})
	flat.Draw(flatCanvas, 0)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	// No coverage snapshot on this frame, so the only accent in the picture is
	// the selection ring and its label.
	if got := countColour(canv, view3DBox, theme.Night.Accent); got == 0 {
		t.Error("the 3D view drew no accent pixels, want the selection ring and its callsign")
	}
}

// TestView3DWithoutAPosition checks the rule scopeFrame.plottable carries on
// the flat scope: with no receiver position there is nowhere to put the ground
// overlays or the traffic, but the rings and the envelope are measured from
// the receiver rather than from a coordinate and are still drawn.
func TestView3DWithoutAPosition(t *testing.T) {
	t.Parallel()

	frame := covered(source.Frame{
		Planes:   []airplane.Snapshot{scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)},
		Receiver: source.Receiver{Label: source.LabelNone, Mode: source.FixNone},
		Now:      sceneClock,
	})

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	if got := countColour(canv, view3DBox, theme.Night.Rule); got == 0 {
		t.Error("the 3D view drew no rings without a position, want them drawn anyway")
	}

	if got := countColour(canv, view3DBox, measuredWireInk); got == 0 {
		t.Error("the 3D view drew no envelope without a position, want it drawn anyway")
	}

	if got := countColour(canv, view3DBox, theme.Night.AltLow); got != 0 {
		t.Errorf("the 3D view drew %d aircraft pixels without a position, want none", got)
	}
}

// TestView3DAircraftWithoutAHeading checks that an aircraft nobody has decoded
// a heading for is a bare circle rather than a silhouette, which is the same
// rule the flat scope applies: a silhouette would be claiming to know which
// way it is pointing.
func TestView3DAircraftWithoutAHeading(t *testing.T) {
	t.Parallel()

	turning := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)
	blind := sentinelPlane(-1, -1)
	blind.Latitude, blind.Longitude = turning.Latitude, turning.Longitude

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(blind))
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	withCircle := painted(canv, view3DBox)

	other, otherCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(turning))
	other.Apply(view3DSettings())
	other.Draw(otherCanvas, 0)

	// A fifteen-pixel silhouette covers more ground than a five-pixel circle,
	// which is the cheapest way to tell the two shapes apart without pinning
	// an exact picture.
	if withCircle >= painted(otherCanvas, view3DBox) {
		t.Error("the aircraft with no heading drew at least as much as one with a silhouette, want less")
	}
}

// TestView3DDropsWhatIsOutOfRange checks that an aircraft beyond the outer
// ring is left off the perspective picture, the same way the flat scope drops
// one past its own ring.
func TestView3DDropsWhatIsOutOfRange(t *testing.T) {
	t.Parallel()

	inside := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	outside := sceneFrame(scenePlane("484AC1", "KLM123", 45, sceneRangeNm*3, 2400, 41))

	near, nearCanvas, _ := sceneOn(t, panelWidth, panelHeight, inside)
	near.Apply(view3DSettings())
	near.Draw(nearCanvas, 0)

	far, farCanvas, _ := sceneOn(t, panelWidth, panelHeight, outside)
	far.Apply(view3DSettings())
	far.Draw(farCanvas, 0)

	if got := painted(farCanvas, view3DBox); got >= painted(nearCanvas, view3DBox) {
		t.Error("an aircraft outside the range drew as much as one inside it, want it dropped")
	}
}

// TestView3DAtEverySize checks that the perspective view survives the same
// range of canvases the flat one does, down to sizes with no room for a
// camera, no room for labels and no room for anything at all.
func TestView3DAtEverySize(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	for _, testCase := range []struct {
		name          string
		width, height int
	}{
		{name: "the panel", width: panelWidth, height: panelHeight},
		{name: "no column", width: 480, height: 320},
		{name: "no labels", width: panelWidth, height: 300},
		{name: "a terminal of half blocks", width: 160, height: 96},
		{name: "no room for a camera at all", width: 8, height: 8},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, testCase.width, testCase.height, frame)
			scene.Apply(view3DSettings())

			// The promise is that it renders rather than panicking or drawing
			// over its neighbour. What lands on a canvas of a few dozen pixels
			// is not something to pin.
			scene.Draw(canv, 0)
		})
	}
}

// TestView3DOrbitMovesThePicture checks the automatic orbit end to end: the
// same frame drawn at two different elapsed readings is a different picture,
// and stops being one once a nudge has stopped the camera.
func TestView3DOrbitMovesThePicture(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	scene, first, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(first, 0)

	second, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(second, orbitQuarter)

	if identicalIn(first, second, view3DBox) {
		t.Fatal("a quarter of a revolution later the picture is unchanged, want the camera to have moved")
	}

	// Right stops the orbit as well as nudging it, which is the point of the
	// key: a camera that walked away from where it was just put is not what
	// pressing an arrow meant.
	if !scene.Handle(input.Key{Kind: input.Right}) {
		t.Fatal("Right was not taken in the 3D view")
	}

	scene.Draw(first, orbitQuarter)
	scene.Draw(second, orbitQuarter*3)

	if !identicalIn(first, second, view3DBox) {
		t.Error("the picture moved after the orbit was stopped, want it still")
	}
}

// The two colours the 3D view draws its wireframes in: the measured envelope
// faded 35 percent of the way from the field towards the accent, and the
// theoretical bowl 60 percent of the way towards the muted colour.
//
// They are worked out here rather than read off the scene because the mix is
// unexported, and they are worked out rather than written down so a change to
// the scene's own arithmetic shows up as a failing count instead of a test
// that quietly starts matching nothing. The exact values are pinned on the
// other side by TestEnvelopeInkIsFadedIntoTheField in the internal file, so
// the two have to agree.
//
//nolint:gochecknoglobals // a colour is data, read-only after init.
var (
	measuredWireInk = fadeInto(theme.Night.Field, theme.Night.Accent, 0.35)
	bowlWireInk     = fadeInto(theme.Night.Field, theme.Night.Muted, 0.60)
)

// fadeInto mixes ink towards field by alpha, where 1 is all ink and 0 all
// field. It is the external twin of the scene's own fade, written out again
// rather than shared, which is the point: two independent implementations of
// the same arithmetic disagree when one of them changes.
func fadeInto(field, ink color.RGBA, alpha float64) color.RGBA {
	channel := func(from, to uint8) uint8 {
		return uint8(math.Round(float64(from) + (float64(to)-float64(from))*alpha))
	}

	return color.RGBA{
		R: channel(field.R, ink.R),
		G: channel(field.G, ink.G),
		B: channel(field.B, ink.B),
		A: 0xFF,
	}
}

// The 3D view's own cap boxes in the bottom bar, at the panel's resolution and
// with no bias-tee cap in front of them.
//
// They are written down rather than derived because the arithmetic that places
// them is the thing under test: a box worked out by re-running drawCaps' own
// sums would move whenever the bar did and never fail. These came off a
// rendered frame by scanning the bar row for runs of filled ink.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var (
	orbitCapBox    = image.Rect(734, 686, 750, 704)
	envelopeCapBox = image.Rect(804, 686, 820, 704)
	turnCapBox     = image.Rect(892, 686, 914, 704)
	tiltCapBox     = image.Rect(962, 686, 990, 704)

	// scopeBarTail is the part of the bar row the ten shared caps never reach,
	// which is where all four of the boxes above sit.
	scopeBarTail = image.Rect(724, 686, 1280, 704)
)

// TestView3DKeyBarListsTheCameraKeys checks that all four of the view's keys
// are on the bar while it is up, and that none of them is on it otherwise.
//
// The camera keys were bound and undocumented on screen: the arrows turned the
// camera and the brackets tilted it, and the only way to find that out was to
// read the README, which is not where anyone looks while holding the device.
// The other half matters just as much. A cap for a key that does nothing in
// the view on screen is furniture pretending to be a control, so the scope's
// bar has to stop after VIEW.
func TestView3DKeyBarListsTheCameraKeys(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	flat, flatCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	flat.Apply(radar.Settings{RangeNm: sceneRangeNm})
	flat.Draw(flatCanvas, 0)

	perspective, perspectiveCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	perspective.Apply(view3DSettings())
	perspective.Draw(perspectiveCanvas, 0)

	for _, testCase := range []struct {
		name string
		box  image.Rectangle
	}{
		{name: "orbit", box: orbitCapBox},
		{name: "envelope", box: envelopeCapBox},
		{name: "turn", box: turnCapBox},
		{name: "tilt", box: tiltCapBox},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := painted(perspectiveCanvas, testCase.box); got == 0 {
				t.Errorf("the 3D bar drew nothing in the %s cap's box, want a cap there", testCase.name)
			}

			if got := painted(flatCanvas, testCase.box); got != 0 {
				t.Errorf("the scope bar drew %d pixels in the %s cap's box, want none", got, testCase.name)
			}
		})
	}

	// Nothing at all past VIEW on the flat scope, which is the stronger claim:
	// the four boxes above could each be blank while a fifth cap nobody asked
	// for sat between them.
	if got := painted(flatCanvas, scopeBarTail); got != 0 {
		t.Errorf("the scope bar drew %d pixels past VIEW, want the bar to stop there", got)
	}
}

// TestView3DKeyBarFitsAtPanelWidth checks that fourteen caps and their labels
// still stop short of the right margin.
//
// The bar has no wrapping and no eliding: drawCaps stops after the cap that
// reaches the margin, so a bar that outgrew the panel would silently lose its
// last control rather than complain. Airline mode is the wide case, because
// the colour cap is labelled with the mode it is on and AIRLINE is four
// characters longer than ALT.
func TestView3DKeyBarFitsAtPanelWidth(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
	scene.Apply(radar.Settings{View: radar.View3D, RangeNm: sceneRangeNm, Colour: radar.ColourAirline})
	scene.Draw(canv, 0)

	_, last := paintedExtent(canv, keyBarBox)
	if last < 0 {
		t.Fatal("the key bar drew nothing at all, so this proves nothing")
	}

	// baseMargin is 16, so the bar has to end before 1264 at this width.
	const rightMargin = panelWidth - 16

	if last >= rightMargin {
		t.Errorf("the 3D key bar reached x=%d, want it to stop before the %d margin", last, rightMargin)
	}

	// The tilt cap is the last one, so the bar must reach at least that far or
	// a cap has been dropped rather than merely fitting.
	if last < tiltCapBox.Max.X {
		t.Errorf("the 3D key bar ended at x=%d, before the tilt cap at %d", last, tiltCapBox.Max.X)
	}
}

// TestView3DOrbitCapFollowsTheKey checks that o reads as a switch.
//
// The orbit is on from the first frame, so before o toggled, the commonest
// press was one that rebased an azimuth already where it was: nothing on
// screen changed and the key read as dead. A filled cap that goes hollow is
// the bar saying which of the two states the camera is in.
func TestView3DOrbitCapFollowsTheKey(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	orbitingInk := countColour(canv, orbitCapBox, theme.Night.Ink)
	orbitingField := countColour(canv, orbitCapBox, theme.Night.Field)

	if orbitingInk <= orbitingField {
		t.Errorf("orbit on: %d ink and %d field pixels, want a filled cap", orbitingInk, orbitingField)
	}

	if !press(scene, 'o') {
		t.Fatal("Handle('o') = false, want the 3D view to take it")
	}

	scene.Draw(canv, 0)

	stoppedInk := countColour(canv, orbitCapBox, theme.Night.Ink)
	stoppedField := countColour(canv, orbitCapBox, theme.Night.Field)

	if stoppedField <= stoppedInk {
		t.Errorf("orbit off: %d ink and %d field pixels, want a hollow cap", stoppedInk, stoppedField)
	}

	if stoppedInk == 0 {
		t.Error("the hollow orbit cap drew no ink at all, want an outline and a letter")
	}

	press(scene, 'o')
	scene.Draw(canv, 0)

	if got := countColour(canv, orbitCapBox, theme.Night.Ink); got != orbitingInk {
		t.Errorf("a second o left %d ink pixels in the cap, want the original %d", got, orbitingInk)
	}
}
