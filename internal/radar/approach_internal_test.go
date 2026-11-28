package radar

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The stream's frame and the one the strip's cost is measured at.
const (
	approachTestWidth   = 1920
	approachTestHeight  = 1080
	approachBenchWidth  = 2560
	approachBenchHeight = 1440
	approachBenchPlanes = 12
)

// approachPlane is an arrival km east of boardCorridors' field at metres,
// descending towards it.
func approachPlane(icao, callsign string, km, metres float64) airplane.Snapshot {
	plane := windowPlane(icao, callsign, km/kmPerNm, windowDescentFpm)
	plane.Altitude = metres / metresPerFoot

	return plane
}

// approachScene is a scene in the bare 3D view with the strip on, and the
// window overlay as asked.
func approachScene(tb testing.TB, frame source.Frame, window bool) *Scene {
	tb.Helper()

	scene, _ := windowScene(tb, frame)
	scene.Apply(Settings{
		View: ViewMinimal3D, Window: window, Approach: true, Colour: ColourCorridor, RangeNm: waterProbeRangeNm,
	})

	return scene
}

// approachLay is a layout over a fresh canvas of this size, with the whole
// canvas as the scope box.
func approachLay(tb testing.TB, width, height int) layout {
	tb.Helper()

	canv, err := canvas.New(width, height)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	return layout{dst: canv, scope: canv.Bounds()}
}

// approachQueue is n arrivals in line on a three-degree path, every 2.4 km
// from 2 km out.
func approachQueue(count int) []airplane.Snapshot {
	planes := make([]airplane.Snapshot, 0, count)

	for index := range count {
		km := 2 + 2.4*float64(index)
		planes = append(planes, approachPlane(fmt.Sprintf("4840%02X", index), fmt.Sprintf("KLM%d", 100+index),
			km, km*52))
	}

	return planes
}

func TestApplyCarriesTheApproach(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	if !scene.approach || !scene.approachShown() {
		t.Fatal("Apply(Approach: true) did not put the strip on the bare 3D view")
	}

	scene.Apply(Settings{View: View3D, Approach: true})

	if scene.approachShown() {
		t.Error("the strip is shown in the full 3D view")
	}

	scene.Apply(Settings{View: ViewMinimal3D})

	if scene.approach {
		t.Error("Apply with Approach false left the strip on")
	}
}

func TestNewApproachLayout(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)

	for _, testCase := range []struct {
		name          string
		width, height int
		wantOK        bool
	}{
		{name: "stream", width: approachTestWidth, height: approachTestHeight, wantOK: true},
		{name: "1440p", width: approachBenchWidth, height: approachBenchHeight, wantOK: true},
		{name: "too narrow for the heading", width: 400, height: 300},
		{name: "no face", width: approachTestWidth, height: approachTestHeight},
	} {
		lay := approachLay(t, testCase.width, testCase.height)

		if testCase.name == "no face" {
			faceless := *scene
			faceless.faces.Small = nil

			if _, ok := faceless.newApproachLayout(&lay); ok {
				t.Error("a layout with no face to set the type in")
			}

			continue
		}

		strip, ok := scene.newApproachLayout(&lay)
		if ok != testCase.wantOK {
			t.Fatalf("%s: ok %v, want %v", testCase.name, ok, testCase.wantOK)
		}

		if !ok {
			continue
		}

		wantLeft := testCase.width * approachLeftPercent / percent
		wantWidth := testCase.width * approachWidthPercent / percent

		if strip.panel.Min.X != wantLeft || strip.panel.Dx() != wantWidth {
			t.Errorf("%s: panel %v, want left %d and width %d", testCase.name, strip.panel, wantLeft, wantWidth)
		}

		if wantHeight := wantWidth * tenths / approachAspectTenths; strip.panel.Dy() != wantHeight {
			t.Errorf("%s: panel height %d, want %d", testCase.name, strip.panel.Dy(), wantHeight)
		}

		if !strip.plot.In(strip.panel) {
			t.Errorf("%s: plot %v outside the panel %v", testCase.name, strip.plot, strip.panel)
		}
	}
}

func TestApproachDotsKeepTheNearest(t *testing.T) {
	t.Parallel()

	var table approachDots

	for index := range approachMaxDots {
		table.add(approachDot{km: float64(approachMaxDots - index)})
	}

	table.add(approachDot{km: approachMaxDots + 1})

	if table.count != approachMaxDots || table.dots[approachMaxDots-1].km != approachMaxDots {
		t.Fatalf("a dot further out than all of a full table was kept: last %v", table.dots[approachMaxDots-1].km)
	}

	table.add(approachDot{km: 0.5})

	if table.dots[0].km != 0.5 || table.dots[approachMaxDots-1].km != approachMaxDots-1 {
		t.Errorf("a nearer dot did not push the furthest out: first %v, last %v",
			table.dots[0].km, table.dots[approachMaxDots-1].km)
	}

	for index := 1; index < table.count; index++ {
		if table.dots[index-1].km > table.dots[index].km {
			t.Fatalf("dots out of order at %d", index)
		}
	}
}

func TestApproachLabelsTable(t *testing.T) {
	t.Parallel()

	var table approachLabels

	box := image.Rect(0, 0, 10, 10)
	for range len(table.boxes) + 1 {
		table.add(box)
	}

	if table.count != len(table.boxes) {
		t.Fatalf("count %d, want the table's size %d", table.count, len(table.boxes))
	}

	var one approachLabels

	one.add(box)

	if one.hits(box, 0) {
		t.Error("a box hit the entry it was told to skip")
	}

	if !one.hits(box.Add(image.Pt(5, 5)), -1) {
		t.Error("an overlapping box did not hit")
	}

	if one.hits(box.Add(image.Pt(20, 0)), -1) {
		t.Error("a clear box hit")
	}
}

func TestOverlayHeight(t *testing.T) {
	t.Parallel()

	var scene Scene

	field := boardCorridors().Field

	for _, testCase := range []struct {
		name       string
		altitudeFt float64
		wantMetres float64
		wantKnown  bool
	}{
		{name: "not reported", altitudeFt: 0},
		{name: "on the approach", altitudeFt: 2000, wantMetres: 2000 * metresPerFoot, wantKnown: true},
		{name: "below sea level", altitudeFt: -25, wantMetres: -25 * metresPerFoot, wantKnown: true},
	} {
		plane := approachPlane(lightICAO, "", 5, 0)
		plane.Altitude = testCase.altitudeFt

		metres, known := scene.overlayHeight(plane, field)
		if metres != testCase.wantMetres || known != testCase.wantKnown {
			t.Errorf("%s: %v %v, want %v %v", testCase.name, metres, known, testCase.wantMetres, testCase.wantKnown)
		}

		if testCase.wantKnown != (scene.captionMetres(plane, field) != 0) {
			t.Errorf("%s: captionMetres disagrees with overlayHeight", testCase.name)
		}
	}
}

func TestApproachDotFor(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	lay := approachLay(t, approachTestWidth, approachTestHeight)

	strip, ok := scene.newApproachLayout(&lay)
	if !ok {
		t.Fatal("no layout at the stream's size")
	}

	departure := windowPlane("484AC4", "EZY12", windowDepartureNm, windowClimbFpm)
	unknown := approachPlane("484AC5", "KLM9", 10, 0)
	high := approachPlane("484AC6", "KLM10", 20, 2500)
	low := approachPlane("484AC7", "KLM11", 3, 0)
	low.Altitude = -40
	unnamed := approachPlane("484AC8", "  ", 12, 600)
	far := approachPlane("484AC9", "KLM12", 32, 1500)

	for _, testCase := range []struct {
		name      string
		plane     airplane.Snapshot
		wantOK    bool
		wantY     int
		wantAlpha float64
		wantCall  string
	}{
		{name: "a departure", plane: departure},
		{name: "beyond the reach", plane: far},
		{name: "no height", plane: unknown},
		{
			name: "above the ceiling", plane: high, wantOK: true, wantY: strip.plot.Min.Y,
			wantAlpha: approachAboveAlpha, wantCall: "KLM10",
		},
		{name: "below the field", plane: low, wantOK: true, wantY: strip.plot.Max.Y, wantAlpha: 1, wantCall: "KLM11"},
		{
			name: "no callsign", plane: unnamed, wantOK: true,
			wantY: approachY(strip.plot, 600), wantAlpha: 1, wantCall: "484AC8",
		},
	} {
		dot, placed := scene.approachDotFor(&testCase.plane, boardCorridors(), &strip)
		if placed != testCase.wantOK {
			t.Errorf("%s: placed %v, want %v", testCase.name, placed, testCase.wantOK)

			continue
		}

		if placed && (dot.y != testCase.wantY || dot.alpha != testCase.wantAlpha || dot.callsign != testCase.wantCall) {
			t.Errorf("%s: y %d alpha %v %q, want %d %v %q", testCase.name, dot.y, dot.alpha, dot.callsign,
				testCase.wantY, testCase.wantAlpha, testCase.wantCall)
		}
	}
}

func TestApproachFocus(t *testing.T) {
	t.Parallel()

	near := approachPlane("484AD1", "KLM1", 6, 300)
	farther := approachPlane("484AD2", "KLM2", 14, 700)
	departure := windowPlane("484AD3", "EZY3", windowDepartureNm, windowClimbFpm)

	for _, testCase := range []struct {
		name   string
		window bool
		planes []airplane.Snapshot
		want   string
	}{
		{name: "overlay off, nearest arrival", planes: []airplane.Snapshot{farther, near}, want: "484AD1"},
		{name: "overlay off, only a departure", planes: []airplane.Snapshot{departure}},
		{
			name: "overlay on, caption on an arrival", window: true,
			planes: []airplane.Snapshot{farther, near}, want: "484AD1",
		},
		{name: "overlay on, caption on a departure", window: true, planes: []airplane.Snapshot{departure}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := windowFrame(testCase.planes...)
			scene := approachScene(t, frame, testCase.window)
			lay := approachLay(t, approachTestWidth, approachTestHeight)
			scene.drawScope(&lay, frame, 0)
			scene.drawWindow(&lay, frame, 0)

			if got := scene.approachFocus(frame); got != testCase.want {
				t.Errorf("focus %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestCollectApproachWithoutAField(t *testing.T) {
	t.Parallel()

	frame := windowFrame(approachQueue(3)...)
	scene := approachScene(t, frame, false)
	lay := approachLay(t, approachTestWidth, approachTestHeight)
	strip, _ := scene.newApproachLayout(&lay)

	scene.collectApproach(frame, &strip)

	if scene.approachDots.count != 3 {
		t.Fatalf("%d dots for three arrivals, want 3", scene.approachDots.count)
	}

	frame.Corridors = source.Corridors{}
	scene.collectApproach(frame, &strip)

	if scene.approachDots.count != 0 {
		t.Errorf("%d dots with no field placed, want none", scene.approachDots.count)
	}
}

// TestApproachLabelsNeverCollide draws queues of growing length and checks
// that no two callsigns overlap, that each stays in the panel, and that the
// focus always has one.
func TestApproachLabelsNeverCollide(t *testing.T) {
	t.Parallel()

	for count := 1; count <= approachBenchPlanes; count++ {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()

			frame := windowFrame(approachQueue(count)...)
			scene := approachScene(t, frame, false)
			lay := approachLay(t, approachTestWidth, approachTestHeight)
			scene.drawApproach(&lay, frame)

			strip, _ := scene.newApproachLayout(&lay)
			labels := &scene.approachLabels
			first := labels.dotsFrom + scene.approachDots.count

			if labels.count <= first {
				t.Fatal("not even the focus got a callsign")
			}

			for index := first; index < labels.count; index++ {
				checkCallsign(t, labels.boxes[first:index], labels.boxes[index], strip.panel)
			}
		})
	}
}

// checkCallsign fails the test when box leaves the panel or overlaps one of
// the callsigns set before it.
func checkCallsign(t *testing.T, before []image.Rectangle, box, panel image.Rectangle) {
	t.Helper()

	if !box.In(panel) {
		t.Errorf("callsign %v leaves the panel %v", box, panel)
	}

	for _, other := range before {
		if box.Overlaps(other) {
			t.Errorf("callsigns %v and %v overlap", box, other)
		}
	}
}

// TestLabelApproachDotWithNoRoom checks the two ways a dot with every place
// taken goes: the focus is centred over it inside the panel, and any other
// dot goes without.
func TestLabelApproachDotWithNoRoom(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	lay := approachLay(t, approachTestWidth, approachTestHeight)
	strip, _ := scene.newApproachLayout(&lay)

	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	pen := scene.windowPen(&lay, &win, scene.faces.Small, 0)

	for _, focus := range []bool{false, true} {
		scene.approachLabels = approachLabels{}
		scene.approachLabels.add(strip.panel)

		dot := approachDot{x: strip.plot.Max.X, y: strip.plot.Min.Y, callsign: "KLM1452", alpha: 1, focus: focus}
		scene.labelApproachDot(&pen, &strip, &dot, -1)

		placed := scene.approachLabels.count == 2
		if placed != focus {
			t.Fatalf("focus %v: placed %v", focus, placed)
		}

		if focus && !scene.approachLabels.boxes[1].In(strip.panel) {
			t.Errorf("the forced callsign %v leaves the panel", scene.approachLabels.boxes[1])
		}
	}
}

func TestDrawApproachDrawsNothingWhenItCannot(t *testing.T) {
	t.Parallel()

	frame := windowFrame(approachQueue(4)...)

	for _, testCase := range []struct {
		name          string
		on            bool
		width, height int
	}{
		{name: "with the flag off", width: approachTestWidth, height: approachTestHeight},
		{name: "no room", on: true, width: 400, height: 300},
	} {
		scene := approachScene(t, frame, false)
		scene.approach = testCase.on
		lay := approachLay(t, testCase.width, testCase.height)
		before := append([]byte(nil), lay.dst.Image().Pix...)

		scene.drawApproach(&lay, frame)

		if string(before) != string(lay.dst.Image().Pix) {
			t.Errorf("%s: the strip drew", testCase.name)
		}
	}
}

func TestApproachPlacesOrder(t *testing.T) {
	t.Parallel()

	dot := approachDot{x: 100, y: 50}
	places := approachPlaces(&dot, 20, 10, 5)

	want := [...]image.Point{{105, 40}, {75, 40}, {105, 50}, {75, 50}, {90, 35}, {90, 55}}
	if places != want {
		t.Errorf("places %v, want %v", places, want)
	}
}

// BenchmarkDrawApproach prices the strip alone with twelve arrivals at 1440p.
func BenchmarkDrawApproach(b *testing.B) {
	frame := windowFrame(approachQueue(approachBenchPlanes)...)
	scene := approachScene(b, frame, false)
	lay := approachLay(b, approachBenchWidth, approachBenchHeight)

	b.ReportAllocs()

	for b.Loop() {
		scene.drawApproach(&lay, frame)
	}
}

// TestApproachPanelWash checks the panel's strength on both kinds of
// palette: a third of the field colour on a dark one, more on a light one.
func TestApproachPanelWash(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		pal   theme.Palette
		alpha float64
	}{
		{name: caseNight, pal: theme.VectorNight, alpha: approachDarkPanel},
		{name: caseDay, pal: theme.VectorDay, alpha: approachLightPanel},
	} {
		scene := approachScene(t, windowFrame(), false)
		scene.SetPalette(testCase.pal)

		lay := approachLay(t, approachTestWidth, approachTestHeight)
		lay.dst.Clear(color.RGBA{A: 0xff})

		scene.drawApproach(&lay, windowFrame())

		strip, _ := scene.newApproachLayout(&lay)
		probe := strip.panel.Max.Sub(image.Pt(2, 2))
		got := lay.dst.Image().RGBAAt(probe.X, probe.Y)
		want := float64(testCase.pal.Field.G) * testCase.alpha

		if math.Abs(float64(got.G)-want) > 2 {
			t.Errorf("%s: panel green %d, want about %.0f", testCase.name, got.G, want)
		}
	}
}
