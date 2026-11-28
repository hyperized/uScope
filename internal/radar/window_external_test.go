package radar_test

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The stream's frame, which the overlay is laid out for, and the corridor
// airfield the test frame places on the receiver.
const (
	windowWidth  = 1920
	windowHeight = 1080
	windowField  = "EHAM"
)

// windowTestFrame is the receiver's frame with its corridor airfield placed
// on the receiver, an arrival eight miles east descending towards it, and two
// tracks lost in the last hour for the count.
func windowTestFrame() source.Frame {
	arrival := scenePlane("484AC1", "KLM1452", 90, 8, 3000, 270)
	arrival.VertRate = -800

	frame := sceneFrame(arrival, scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268))
	frame.Corridors = source.Corridors{
		Field: source.Airfield{ICAO: windowField, Latitude: receiverLat, Longitude: receiverLon},
	}
	frame.Ghosts = []source.Trail{
		{ICAO: "484AC9", Class: source.TrackInbound, LostAt: sceneClock.Add(-10 * time.Minute)},
		{ICAO: "484ACA", Class: source.TrackOutbound, LostAt: sceneClock.Add(-20 * time.Minute)},
	}

	return frame
}

// windowPixels draws the test frame at the stream's size and returns the
// pixels.
func windowPixels(t *testing.T, set radar.Settings) []byte {
	t.Helper()

	scene, canv, _ := sceneOn(t, windowWidth, windowHeight, windowTestFrame())
	scene.Apply(set)
	scene.Draw(canv, 0)

	return bytes.Clone(canv.Image().Pix)
}

// TestWindowOnlyInTheBare3DView checks that Window changes no picture but the
// bare 3D view's.
func TestWindowOnlyInTheBare3DView(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			off := radar.Settings{View: view, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor}
			on := off
			on.Window = true

			if !bytes.Equal(windowPixels(t, off), windowPixels(t, on)) {
				t.Errorf("Window changed the %s picture", view)
			}
		})
	}
}

// TestWindowStaysInItsCorners checks that the overlay draws in all three of
// its places, the clock at the top right and the caption and the count along
// the bottom, and leaves the middle of the picture alone.
func TestWindowStaysInItsCorners(t *testing.T) {
	t.Parallel()

	off := radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor}
	on := off
	on.Window = true

	before, after := windowPixels(t, off), windowPixels(t, on)

	clock := image.Rect(windowWidth/2, 0, windowWidth, windowHeight/4)
	caption := image.Rect(0, windowHeight*3/4, windowWidth/2, windowHeight)
	count := image.Rect(windowWidth/2, windowHeight*3/4, windowWidth, windowHeight)
	changed := map[string]int{}

	for pixelY := range windowHeight {
		for pixelX := range windowWidth {
			offset := (pixelY*windowWidth + pixelX) * 4
			if bytes.Equal(before[offset:offset+4], after[offset:offset+4]) {
				continue
			}

			point := image.Pt(pixelX, pixelY)

			switch {
			case point.In(clock):
				changed["clock"]++
			case point.In(caption):
				changed["caption"]++
			case point.In(count):
				changed["count"]++
			default:
				t.Fatalf("the overlay changed pixel %v, outside its three places", point)
			}
		}
	}

	for _, place := range []string{"clock", "caption", "count"} {
		if changed[place] == 0 {
			t.Errorf("nothing drawn for the %s", place)
		}
	}
}

// TestWindowDrawAllocations prices the overlay's frames at zero: the clock,
// the count and the caption fading in, held and faded out, with the render
// clock stepping a sixth of a second a frame.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestWindowDrawAllocations(t *testing.T) {
	frame := windowTestFrame()
	scene, canv, _ := sceneOn(t, windowWidth, windowHeight, frame)
	scene.Apply(radar.Settings{
		View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor, Window: true,
	})

	var elapsed time.Duration

	scene.Draw(canv, elapsed)

	frameAt := func() {
		elapsed += time.Second / 6
		scene.Draw(canv, elapsed)
	}

	if got := testing.AllocsPerRun(allocRuns, frameAt); got != 0 {
		t.Errorf("Draw with the window overlay allocated %.1f times per frame, want 0", got)
	}
}

// TestWindowWithNothingToCaption checks the overlay over a frame with no
// aircraft near the field: the clock and the count, and no caption.
func TestWindowWithNothingToCaption(t *testing.T) {
	t.Parallel()

	frame := windowTestFrame()
	frame.Planes = frame.Planes[1:]

	scene, canv, _ := sceneOn(t, windowWidth, windowHeight, frame)
	scene.Apply(radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Window: true})
	scene.Draw(canv, 0)

	bare, bareCanvas, _ := sceneOn(t, windowWidth, windowHeight, frame)
	bare.Apply(radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm})
	bare.Draw(bareCanvas, 0)

	caption := image.Rect(0, windowHeight*3/4, windowWidth/2, windowHeight)
	if !regionEqual(canv, bareCanvas, caption) {
		t.Error("a caption was drawn with nothing near the field")
	}
}

// regionEqual reports whether two canvases hold the same pixels in box.
func regionEqual(one, other *canvas.Canvas, box image.Rectangle) bool {
	for pixelY := box.Min.Y; pixelY < box.Max.Y; pixelY++ {
		for pixelX := box.Min.X; pixelX < box.Max.X; pixelX++ {
			if one.Image().RGBAAt(pixelX, pixelY) != other.Image().RGBAAt(pixelX, pixelY) {
				return false
			}
		}
	}

	return true
}
