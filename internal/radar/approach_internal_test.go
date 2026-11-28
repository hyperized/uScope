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

	// glideFtPerNm puts the test queues on a three-degree path.
	glideFtPerNm = 318

	// approachFocusCall is the callsign the test queues open with.
	approachFocusCall = "KLM1452"
)

// approachPlane is an arrival nm east of boardCorridors' field at feet,
// descending towards it.
func approachPlane(icao, callsign string, nm, feet float64) airplane.Snapshot {
	plane := windowPlane(icao, callsign, nm, windowDescentFpm)
	plane.Altitude = feet

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

// approachQueue is n arrivals in line on a three-degree path, every 1.2 NM
// from 1 NM out.
func approachQueue(count int) []airplane.Snapshot {
	planes := make([]airplane.Snapshot, 0, count)

	for index := range count {
		nm := 1 + 1.2*float64(index)
		planes = append(planes, approachPlane(fmt.Sprintf("4840%02X", index), fmt.Sprintf("KLM%d", 100+index),
			nm, nm*glideFtPerNm))
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

	for _, testCase := range []struct {
		name          string
		width, height int
		window        bool
		wantOK        bool
	}{
		{name: "stream", width: approachTestWidth, height: approachTestHeight, wantOK: true},
		{
			name: "stream under the overlay", width: approachTestWidth, height: approachTestHeight,
			window: true, wantOK: true,
		},
		{name: "1440p", width: approachBenchWidth, height: approachBenchHeight, wantOK: true},
		{name: "2160p", width: 3840, height: 2160, wantOK: true},
		{name: "too small", width: 400, height: 300},
		{name: "too short for two rows", width: 1100, height: 1080, window: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := approachScene(t, windowFrame(), testCase.window)
			lay := approachLay(t, testCase.width, testCase.height)

			strip, ok := scene.newApproachLayout(&lay)
			if ok != testCase.wantOK {
				t.Fatalf("ok %v, want %v", ok, testCase.wantOK)
			}

			if !ok {
				return
			}

			win := newWindowLayout(&lay)
			wantLeft := testCase.width * approachLeftPercent / percent
			wantRight := min(wantLeft+testCase.width*approachWidthPercent/percent, scene.approachRightLimit(&lay, &win))
			wantHeight := testCase.width * approachHeightPercent / percent

			if strip.panel.Min.X != wantLeft || strip.panel.Max.X != wantRight || strip.panel.Dy() != wantHeight {
				t.Errorf("panel %v, want left %d, right %d and height %d", strip.panel, wantLeft, wantRight, wantHeight)
			}

			if !strip.plot.In(strip.panel) || strip.plot.Dy() < approachRows*strip.pitch {
				t.Errorf("plot %v outside the panel %v or under %d rows", strip.plot, strip.panel, approachRows)
			}
		})
	}
}

func TestNewApproachLayoutWithNoFace(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	scene.faces.Small = nil
	lay := approachLay(t, approachTestWidth, approachTestHeight)

	if _, ok := scene.newApproachLayout(&lay); ok {
		t.Error("a layout with no face to set the type in")
	}
}

// TestApproachRightLimit checks the strip keeps clear of the clock: no limit
// with the overlay off, and the clock's left edge less the overlay's gap
// with it on.
func TestApproachRightLimit(t *testing.T) {
	t.Parallel()

	lay := approachLay(t, approachTestWidth, approachTestHeight)
	win := newWindowLayout(&lay)

	if got := approachScene(t, windowFrame(), false).approachRightLimit(&lay, &win); got != math.MaxInt {
		t.Errorf("limit %d with the overlay off, want none", got)
	}

	scene := approachScene(t, windowFrame(), true)
	clock := scene.windowPen(&lay, &win, scene.faces.Large, 0)
	want := win.box.Max.X - clock.measure("19:36") - win.scale*windowCountGap

	if got := scene.approachRightLimit(&lay, &win); got != want {
		t.Errorf("limit %d with the overlay on, want %d", got, want)
	}
}

func TestApproachDotsKeepTheNearest(t *testing.T) {
	t.Parallel()

	var table approachDots

	for index := range approachMaxDots {
		table.add(approachDot{nm: float64(approachMaxDots - index)})
	}

	table.add(approachDot{nm: approachMaxDots + 1})

	if table.count != approachMaxDots || table.dots[approachMaxDots-1].nm != approachMaxDots {
		t.Fatalf("a dot further out than all of a full table was kept: last %v", table.dots[approachMaxDots-1].nm)
	}

	table.add(approachDot{nm: 0.5})

	if table.dots[0].nm != 0.5 || table.dots[approachMaxDots-1].nm != approachMaxDots-1 {
		t.Errorf("a nearer dot did not push the furthest out: first %v, last %v",
			table.dots[0].nm, table.dots[approachMaxDots-1].nm)
	}

	for index := 1; index < table.count; index++ {
		if table.dots[index-1].nm > table.dots[index].nm {
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

	if _, hit := one.firstHit(box, 0, 0); hit {
		t.Error("a box hit the entry it was told to skip")
	}

	if got, hit := one.firstHit(box.Add(image.Pt(5, 5)), -1, 0); !hit || got != box {
		t.Error("an overlapping box did not hit")
	}

	if _, hit := one.firstHit(box.Add(image.Pt(20, 0)), -1, 0); hit {
		t.Error("a clear box hit")
	}

	if _, hit := one.firstHit(box, -1, 1); hit {
		t.Error("an entry before from was looked at")
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
	unknown := approachPlane("484AC5", "KLM9", 5, 0)
	high := approachPlane("484AC6", "KLM10", 10, 8000)
	low := approachPlane("484AC7", "KLM11", 2, 0)
	low.Altitude = 1
	unnamed := approachPlane("484AC8", "  ", 6, 2000)
	far := approachPlane("484AC9", "KLM12", 17, 5000)

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
		{name: "a foot up", plane: low, wantOK: true, wantY: strip.plot.Max.Y, wantAlpha: 1, wantCall: "KLM11"},
		{
			name: "no callsign", plane: unnamed, wantOK: true,
			wantY: approachY(strip.plot, 2000), wantAlpha: 1, wantCall: "484AC8",
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

	near := approachPlane("484AD1", "KLM1", 3, 1000)
	farther := approachPlane("484AD2", "KLM2", 8, 2500)
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
// that no two callsigns overlap, that each stays in the panel, and that each
// dot is either labelled or counted in the "+N".
func TestApproachLabelsNeverCollide(t *testing.T) {
	t.Parallel()

	for count := 1; count <= approachMaxDots; count++ {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()

			checkApproachLabels(t, approachTestWidth, approachTestHeight, approachQueue(count))
		})
	}
}

// TestEveryArrivalIsLabelled is the owner's rule: eight arrivals spread over
// the 15 NM, or four within 1.5 NM of each other, all carry their callsigns,
// in two rows at most.
func TestEveryArrivalIsLabelled(t *testing.T) {
	t.Parallel()

	sizes := []image.Point{{approachTestWidth, approachTestHeight}, {approachBenchWidth, approachBenchHeight}}

	for _, size := range sizes {
		for name, planes := range map[string][]airplane.Snapshot{
			"eight spread": approachSpread(),
			"four bunched": approachBunched(),
			"bunched mid":  approachBunchedAt(8),
		} {
			t.Run(fmt.Sprintf("%s at %dp", name, size.Y), func(t *testing.T) {
				t.Parallel()

				if left := checkApproachLabels(t, size.X, size.Y, planes); left != 0 {
					t.Errorf("%d of %d arrivals without a callsign", left, len(planes))
				}
			})
		}
	}
}

// TestApproachBunchedAtTheEnds checks the two places a bunch cannot all be
// labelled, against the field and against the ceiling: every callsign that
// is set keeps the rules, and the rest are counted for the "+N".
func TestApproachBunchedAtTheEnds(t *testing.T) {
	t.Parallel()

	for name, planes := range map[string][]airplane.Snapshot{
		"at the field":   approachBunchedAt(1),
		"at the ceiling": approachBunchedAt(13),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checkApproachLabels(t, approachTestWidth, approachTestHeight, planes)
		})
	}
}

// approachSpread is eight arrivals evenly over the strip's 15 NM, on a
// three-degree path.
func approachSpread() []airplane.Snapshot {
	planes := make([]airplane.Snapshot, 0, 8)
	calls := []string{approachFocusCall, "CND093P", "TRA6721", "EZY77KL", "KLM88", "BAW432", "DLH4EA", "RJA151"}

	for index, call := range calls {
		nm := 1 + 1.9*float64(index)
		planes = append(planes, approachPlane(fmt.Sprintf("4842%02X", index), call, nm, nm*glideFtPerNm))
	}

	return planes
}

// approachBunched is four arrivals within 1.5 NM of each other, 5 NM out.
func approachBunched() []airplane.Snapshot { return approachBunchedAt(5) }

// approachBunchedAt is four arrivals half a nautical mile apart from fromNm
// out.
func approachBunchedAt(fromNm float64) []airplane.Snapshot {
	planes := make([]airplane.Snapshot, 0, 4)

	for index, call := range []string{approachFocusCall, "CND093P", "TRA6721", "EZY77KL"} {
		nm := fromNm + 0.5*float64(index)
		planes = append(planes, approachPlane(fmt.Sprintf("4843%02X", index), call, nm, nm*glideFtPerNm))
	}

	return planes
}

// checkApproachLabels draws the strip over these arrivals and checks every
// callsign stays in the panel and clear of every other, and that each dot is
// labelled or counted. It returns how many went without.
func checkApproachLabels(t *testing.T, width, height int, planes []airplane.Snapshot) int {
	t.Helper()

	frame := windowFrame(planes...)
	scene := approachScene(t, frame, false)
	lay := approachLay(t, width, height)
	scene.drawApproach(&lay, frame)

	strip, _ := scene.newApproachLayout(&lay)
	labels := &scene.approachLabels
	first := labels.dotsFrom + scene.approachDots.count

	for index := first; index < labels.count; index++ {
		checkCallsign(t, labels.boxes[first:index], labels.boxes[index], strip.panel)
	}

	checkApproachOrder(t, scene)

	if labelled := labels.count - first; labelled+labels.unlabelled != scene.approachDots.count {
		t.Errorf("%d labelled and %d counted, for %d dots", labelled, labels.unlabelled, scene.approachDots.count)
	}

	return labels.unlabelled
}

// checkApproachOrder fails the test when the callsigns are not in the order
// of their dots, sit below their dots, or when a leader runs through any
// callsign.
func checkApproachOrder(t *testing.T, scene *Scene) {
	t.Helper()

	labels := &scene.approachLabels
	dots := scene.approachDots.dots[:scene.approachDots.count]
	lastCentre := math.MaxInt

	for index := range dots {
		dot := &dots[index]
		if !dot.labelled {
			continue
		}

		centre := (dot.label.box.Min.X + dot.label.box.Max.X) / 2
		if centre >= lastCentre {
			t.Errorf("callsign %q is out of its dot's order", dot.callsign)
		}

		if dot.label.box.Max.Y > dot.y || dot.label.row >= approachRows {
			t.Errorf("callsign %q is not above its dot within %d rows", dot.callsign, approachRows)
		}

		lastCentre = centre
	}

	first := labels.dotsFrom + len(dots)

	for index := range dots {
		dot := &dots[index]
		if !dot.labelled || dot.label.row == 0 {
			continue
		}

		leader := image.Rect(dot.x, dot.label.box.Max.Y, dot.x+1, dot.y)

		for box := first; box < labels.count; box++ {
			if labels.boxes[box] != dot.label.box && labels.boxes[box].Overlaps(leader) {
				t.Errorf("the leader of %q runs through callsign %v", dot.callsign, labels.boxes[box])
			}
		}
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

// TestApproachMoreCount checks the "+N": drawn when a crowd leaves dots
// without a callsign, and absent when every dot has one.
func TestApproachMoreCount(t *testing.T) {
	t.Parallel()

	crowd := make([]airplane.Snapshot, 0, approachMaxDots)
	for index := range approachMaxDots {
		nm := 5 + 0.05*float64(index)
		crowd = append(crowd, approachPlane(fmt.Sprintf("4844%02X", index), approachFocusCall, nm, nm*glideFtPerNm))
	}

	for _, testCase := range []struct {
		name     string
		planes   []airplane.Snapshot
		wantMore bool
	}{
		{name: "a crowd", planes: crowd, wantMore: true},
		{name: "one arrival", planes: approachQueue(1)},
	} {
		frame := windowFrame(testCase.planes...)
		scene := approachScene(t, frame, false)
		lay := approachLay(t, approachTestWidth, approachTestHeight)
		scene.drawApproach(&lay, frame)

		strip, _ := scene.newApproachLayout(&lay)
		win := windowLayout{scale: strip.scale, shadow: strip.shadow}
		pen := scene.windowPen(&lay, &win, scene.faces.Small, 0)
		more := approachMoreBox(&pen, &strip)

		drawn := !uniform(lay.dst, more)
		if drawn != testCase.wantMore || (scene.approachLabels.unlabelled > 0) != drawn {
			t.Errorf("%s: +N drawn %v with %d unlabelled, want %v", testCase.name, drawn,
				scene.approachLabels.unlabelled, testCase.wantMore)
		}
	}
}

// uniform reports whether every pixel of box is the same colour.
func uniform(canv *canvas.Canvas, box image.Rectangle) bool {
	img := canv.Image()
	first := img.RGBAAt(box.Min.X, box.Min.Y)

	for pixelY := box.Min.Y; pixelY < box.Max.Y; pixelY++ {
		for pixelX := box.Min.X; pixelX < box.Max.X; pixelX++ {
			if img.RGBAAt(pixelX, pixelY) != first {
				return false
			}
		}
	}

	return true
}

// TestFindApproachSpot checks the ordered rule on a bare panel: centred
// over the dot on the lowest row, a row up before sliding, slid when every
// row is taken at the centre, and nothing when the order or the barriers
// leave no room.
func TestFindApproachSpot(t *testing.T) {
	t.Parallel()

	strip := approachLayout{panel: image.Rect(0, 0, 1000, 1000), scale: 1, pitch: 11}
	dot := approachDot{x: 500, y: 500}
	bottom := 500 - approachDotRadius - approachLabelGap
	open := func() approachRun {
		run := approachRun{rightmost: math.MaxInt, gap: approachLabelGap}
		for row := range run.barriers {
			run.barriers[row] = 1000 + approachLabelGap
		}

		return run
	}

	for _, testCase := range []struct {
		name      string
		blocked   []image.Rectangle
		lean      approachLean
		next      int
		edit      func(*approachRun)
		want      approachSpot
		wantFound bool
	}{
		{name: "centred", want: approachSpot{box: image.Rect(490, bottom-10, 510, bottom)}, wantFound: true},
		{
			name: "leaning right", lean: leanRight,
			want: approachSpot{box: image.Rect(500, bottom-10, 520, bottom)}, wantFound: true,
		},
		{
			name: "a row up", blocked: []image.Rectangle{image.Rect(505, bottom-10, 600, bottom)},
			want: approachSpot{box: image.Rect(490, bottom-21, 510, bottom-11), row: 1}, wantFound: true,
		},
		{
			name: "slid left", blocked: []image.Rectangle{image.Rect(505, 0, 600, 1000)},
			want: approachSpot{box: image.Rect(483, bottom-10, 503, bottom)}, wantFound: true,
		},
		{
			name: "slid past a blocker", blocked: []image.Rectangle{image.Rect(495, 0, 600, 1000)},
			want: approachSpot{}, wantFound: false,
		},
		{
			name: "leader shut by a barrier", blocked: []image.Rectangle{image.Rect(480, bottom-10, 520, bottom)},
			edit: func(run *approachRun) { run.barriers[0] = 499 },
		},
		{name: "kept left of the last centre", edit: func(run *approachRun) { run.rightmost = 480 }},
		{
			name: "staggered clear of the next column", lean: leanStagger, next: 480,
			want: approachSpot{box: image.Rect(500, bottom-10, 520, bottom)}, wantFound: true,
		},
		{
			name: "staggered up a row off the next column", lean: leanStagger, next: 499,
			want: approachSpot{box: image.Rect(500, bottom-21, 520, bottom-11), row: 1}, wantFound: true,
		},
	} {
		scene := &Scene{}
		for _, box := range testCase.blocked {
			scene.approachLabels.add(box)
		}

		run := open()
		if testCase.edit != nil {
			testCase.edit(&run)
		}

		probe := approachProbe{
			strip: &strip, dot: &dot, width: 20, height: 10, own: -1,
			next: testCase.next, stagger: testCase.lean == leanStagger,
		}

		got, found := scene.findApproachSpot(&probe, &run, testCase.lean)
		if found != testCase.wantFound || got != testCase.want {
			t.Errorf("%s: %v %v, want %v %v", testCase.name, got, found, testCase.want, testCase.wantFound)
		}
	}
}

// TestDrawApproachLeader checks the leader drops straight from the box to
// the top of the dot.
func TestDrawApproachLeader(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	strip := approachLayout{scale: 1}
	dot := approachDot{x: 100, y: 100}
	lay := approachLay(t, 200, 200)

	scene.drawApproachLeader(&lay, &strip, &dot, image.Rect(60, 30, 140, 50))

	if lay.dst.Image().RGBAAt(100, 70) == (color.RGBA{}) {
		t.Error("no leader between the box and the dot")
	}

	if lay.dst.Image().RGBAAt(110, 70) != (color.RGBA{}) {
		t.Error("the leader is not straight down")
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

// TestPlaceApproachLabelsKeepsTheBetterLean builds a table where leaning
// right does worse than centring: a thin post just right of the centred
// place catches the rightward slide, and a second post just left of it
// stops the slide there. A dot that nothing can label makes the centred
// lay-out leave one bare, so both are tried and the centred one is kept.
func TestPlaceApproachLabelsKeepsTheBetterLean(t *testing.T) {
	t.Parallel()

	scene := approachScene(t, windowFrame(), false)
	lay := approachLay(t, approachTestWidth, approachTestHeight)
	win := windowLayout{scale: 1, shadow: 1}
	pen := scene.windowPen(&lay, &win, scene.faces.Small, 0)
	pen.scale = 1

	strip := approachLayout{panel: image.Rect(0, 0, 1000, 1000), scale: 1, pitch: pen.height() + 1}
	dots := []approachDot{{x: 500, y: 500, callsign: "AB"}, {x: 100, y: 500, callsign: "AB"}}
	half := pen.measure("AB") / 2

	scene.approachLabels = approachLabels{}
	scene.approachLabels.add(image.Rect(500+half+1, 0, 500+half+3, 1000))
	scene.approachLabels.add(image.Rect(500-half-1, 0, 500-half, 1000))
	scene.approachLabels.add(image.Rect(0, 0, 300, 1000))
	scene.approachLabels.dotsFrom = 100

	if got := scene.placeApproachLabels(&pen, &strip, dots); got != 1 {
		t.Fatalf("%d bare, want 1", got)
	}

	if !dots[0].labelled || dots[0].label.box.Min.X != 500-half {
		t.Errorf("the first dot's callsign is at %v, want centred", dots[0].label.box)
	}
}
