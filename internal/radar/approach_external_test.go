package radar_test

import (
	"bytes"
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
)

// approachArrivals is how many arrivals the strip's frame carries.
const approachArrivals = 12

// approachTestFrame is the window overlay's test frame with a queue of
// arrivals east of the field, every 1.2 NM from 1 NM out on a three-degree
// path.
func approachTestFrame() source.Frame {
	const (
		ftPerNm       = 318.0
		descentFpm    = -800
		east, west    = 90, 270
		firstNm, step = 1.0, 1.2
	)

	frame := windowTestFrame()

	for index := range approachArrivals {
		nm := firstNm + step*float64(index)
		plane := scenePlane(fmt.Sprintf("4841%02X", index), fmt.Sprintf("TRA%d", 10+index), east, nm, nm*ftPerNm, west)
		plane.VertRate = descentFpm
		frame.Planes = append(frame.Planes, plane)
	}

	return frame
}

// approachPixels draws a frame at the stream's size and returns the pixels.
func approachPixels(t *testing.T, frame source.Frame, set radar.Settings) []byte {
	t.Helper()

	scene, canv, _ := sceneOn(t, windowWidth, windowHeight, frame)
	scene.Apply(set)
	scene.Draw(canv, 0)

	return bytes.Clone(canv.Image().Pix)
}

// TestApproachOnlyInTheBare3DView checks that Approach changes no picture but
// the bare 3D view's.
func TestApproachOnlyInTheBare3DView(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			off := radar.Settings{View: view, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor}
			on := off
			on.Approach = true

			if !bytes.Equal(approachPixels(t, approachTestFrame(), off), approachPixels(t, approachTestFrame(), on)) {
				t.Errorf("Approach changed the %s picture", view)
			}
		})
	}
}

// TestApproachStaysTopLeft checks that the strip draws along the top left,
// with or without the window overlay, and nowhere else; and that it
// draws its panel over a frame with no arrivals at all.
func TestApproachStaysTopLeft(t *testing.T) {
	t.Parallel()

	empty := windowTestFrame()
	empty.Planes = nil

	for _, testCase := range []struct {
		name   string
		frame  source.Frame
		window bool
	}{
		{name: "queue", frame: approachTestFrame()},
		{name: "queue under the overlay", frame: approachTestFrame(), window: true},
		{name: "no arrivals", frame: empty},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			off := radar.Settings{
				View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor, Window: testCase.window,
			}
			on := off
			on.Approach = true

			before, after := approachPixels(t, testCase.frame, off), approachPixels(t, testCase.frame, on)
			// The strip spans 65 percent of the width from a 3 percent inset
			// and a tenth of the width in height.
			corner := image.Rect(0, 0, windowWidth*68/100, windowHeight/3)

			if changedOutside(t, before, after, corner) == 0 {
				t.Error("nothing drawn for the strip")
			}
		})
	}
}

// changedOutside counts the pixels that differ between two pictures, and
// fails the test at the first one outside box.
func changedOutside(t *testing.T, before, after []byte, box image.Rectangle) int {
	t.Helper()

	changed := 0

	for pixelY := range windowHeight {
		for pixelX := range windowWidth {
			offset := (pixelY*windowWidth + pixelX) * 4
			if bytes.Equal(before[offset:offset+4], after[offset:offset+4]) {
				continue
			}

			if !image.Pt(pixelX, pixelY).In(box) {
				t.Fatalf("the strip changed pixel (%d, %d), outside %v", pixelX, pixelY, box)
			}

			changed++
		}
	}

	return changed
}

// TestApproachDotsFollowTheAircraft checks that moving one arrival nearer
// the field changes the strip's picture, so the dots are live.
func TestApproachDotsFollowTheAircraft(t *testing.T) {
	t.Parallel()

	set := radar.Settings{
		View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor, Approach: true,
	}
	frame := approachTestFrame()
	moved := approachTestFrame()
	moved.Planes = append([]airplane.Snapshot(nil), moved.Planes...)
	last := &moved.Planes[len(moved.Planes)-1]
	last.Longitude -= 0.02

	if bytes.Equal(approachPixels(t, frame, set), approachPixels(t, moved, set)) {
		t.Error("moving an arrival left the strip as it was")
	}
}

// TestApproachDrawAllocations prices the strip's frames at zero, with twelve
// arrivals and the window overlay on as well.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestApproachDrawAllocations(t *testing.T) {
	scene, canv, _ := sceneOn(t, windowWidth, windowHeight, approachTestFrame())
	scene.Apply(radar.Settings{
		View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor, Window: true, Approach: true,
	})

	var elapsed time.Duration

	scene.Draw(canv, elapsed)

	frameAt := func() {
		elapsed += time.Second / 6
		scene.Draw(canv, elapsed)
	}

	if got := testing.AllocsPerRun(allocRuns, frameAt); got != 0 {
		t.Errorf("Draw with the approach strip allocated %.1f times per frame, want 0", got)
	}
}
