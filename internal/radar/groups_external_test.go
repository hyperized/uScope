package radar_test

import (
	"image"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// groupedSettle is a render clock past the fade-in, so the second frame of a
// test draws every aircraft at full strength.
const groupedSettle = 3 * time.Second

// The grouped list's slots at panel size, counted down from the first strip
// in strips of boardStripHeight: IN's heading, the arrival, OUT's heading, the
// departure, OTHER's heading, the overflight.
const (
	slotArrival    = 1
	slotDeparture  = 3
	slotOverflight = 5
)

// groupedCorridorFrame is the three corridor aircraft with the overflight
// moved in to five miles, so the nearest aircraft is the last one the grouped
// list draws.
func groupedCorridorFrame() source.Frame {
	near := corridorPlane("4CA2D3", "DLH789", 135, 5, 35000, 45, 0)

	return corridorFrame(near, corridorArrival(), corridorDeparture())
}

// groupedDraw builds a scene over the grouped frame in the scope view and
// draws it twice, the second time past the fade-in.
func groupedDraw(t *testing.T, set radar.Settings) (*radar.Scene, *canvas.Canvas) {
	t.Helper()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, groupedCorridorFrame())
	scene.Apply(set)
	scene.Draw(canv, 0)
	scene.Draw(canv, groupedSettle)

	return scene, canv
}

// TestGroupedListPutsEachClassUnderItsHeading checks corridor colouring lists
// the arrival first and the departure under it, each strip's callsign in its
// class's colour, although the overflight is the nearest of the three.
func TestGroupedListPutsEachClassUnderItsHeading(t *testing.T) {
	t.Parallel()

	_, canv := groupedDraw(t, radar.Settings{Colour: radar.ColourCorridor})

	if countColour(canv, stripRowBox(slotArrival), theme.Night.Data) == 0 {
		t.Error("the strip under IN's heading carries no cyan callsign: the arrival is not first")
	}

	if countColour(canv, stripRowBox(slotDeparture), theme.Night.Caution) == 0 {
		t.Error("the strip under OUT's heading carries no amber callsign: the departure is not there")
	}

	if countColour(canv, stripRowBox(slotOverflight), theme.Night.Caution) != 0 {
		t.Error("the overflight's strip carries amber: OTHER's section holds a departure")
	}
}

// TestGroupedSelectionWalksTheSections checks the unpinned selection stays on
// the nearest aircraft in OTHER, and that n then wraps to the top of IN and
// goes on into OUT, the order the list is drawn in.
func TestGroupedSelectionWalksTheSections(t *testing.T) {
	t.Parallel()

	scene, canv := groupedDraw(t, radar.Settings{Colour: radar.ColourCorridor})
	want := accentWidth * boardStripHeight

	for _, slot := range []int{slotOverflight, slotArrival, slotDeparture} {
		if got := countColour(canv, accentBox(slot), theme.Night.Accent); got != want {
			t.Fatalf("accent pixels in slot %d = %d, want %d", slot, got, want)
		}

		press(scene, 'n')
		scene.Draw(canv, groupedSettle)
	}
}

// TestGroupedFilterDrawsOneSection checks holding the filter to OUT draws
// OUT's heading and the departure straight under it, and nothing after.
func TestGroupedFilterDrawsOneSection(t *testing.T) {
	t.Parallel()

	_, canv := groupedDraw(t, radar.Settings{Colour: radar.ColourCorridor, Filter: radar.FilterOut})

	if countColour(canv, stripRowBox(0), theme.Night.Caution) == 0 {
		t.Error("the first line carries no amber: OUT's heading is not on top")
	}

	if countColour(canv, stripRowBox(1), theme.Night.Caution) == 0 {
		t.Error("the strip under OUT's heading carries no amber callsign")
	}

	below := image.Rect(boardColumnLeft, boardFirstStripTop+2*boardStripHeight, boardColumnRight, boardLegendTop)
	if got := painted(canv, below); got != 0 {
		t.Errorf("%d pixels drawn under the one section", got)
	}
}
