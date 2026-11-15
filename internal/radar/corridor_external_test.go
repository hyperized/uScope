package radar_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// corridorFrame is sceneFrame with the receiver's own position as the
// corridor field, so the bearings scenePlane places aircraft on are the
// bearings Classify measures against.
func corridorFrame(planes ...airplane.Snapshot) source.Frame {
	frame := sceneFrame(planes...)
	frame.Corridors = source.Corridors{
		Field: source.Airfield{ICAO: "EHAM", Latitude: receiverLat, Longitude: receiverLon},
	}

	return frame
}

// corridorPlane is scenePlane with no history and a vertical rate. The
// history scenePlane invents runs past the field from the south west, which
// would read as a departure to the ends test and is not what these cases are
// about.
func corridorPlane(icao, callsign string, bearing, distanceNm, altitude, heading, vertRate float64) airplane.Snapshot {
	plane := scenePlane(icao, callsign, bearing, distanceNm, altitude, heading)
	plane.PositionHistory = nil
	plane.VertRate = vertRate

	return plane
}

// The three aircraft the corridor tests draw: one pointed at the field low
// and level, one climbing out of the terminal area, one crossing at cruise.
func corridorArrival() airplane.Snapshot {
	return corridorPlane("484AC1", "KLM123", 45, 12, 2400, 225, 0)
}

func corridorDeparture() airplane.Snapshot {
	return corridorPlane("3C6745", "EZY456", 90, 20, 6000, 90, 1500)
}

func corridorOverflight() airplane.Snapshot {
	return corridorPlane("4CA2D3", "DLH789", 135, 30, 35000, 45, 0)
}

// TestCorridorModeColoursByClass draws one aircraft of each class in altitude
// and in corridor colouring and checks the class colour gains pixels on the
// scope and on the card, where the callsign is set in it.
func TestCorridorModeColoursByClass(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  color.RGBA
	}{
		{name: "an arrival in the data colour", plane: corridorArrival(), want: theme.Night.Data},
		{name: "a departure in the caution colour", plane: corridorDeparture(), want: theme.Night.Caution},
		{name: "an overflight muted", plane: corridorOverflight(), want: theme.Night.Muted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := corridorFrame(testCase.plane)

			altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
			altitude.Draw(altitudeCanvas, 0)

			corridor, corridorCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame,
				radar.WithColour(radar.ColourCorridor))
			corridor.Draw(corridorCanvas, 0)

			for _, box := range []struct {
				name string
				area image.Rectangle
			}{
				{name: "scope", area: scopeBox},
				{name: "card", area: cardBox},
			} {
				got := countColour(corridorCanvas, box.area, testCase.want)
				base := countColour(altitudeCanvas, box.area, testCase.want)

				if got <= base {
					t.Errorf("%s: %d pixels in the class colour, want more than altitude mode's %d",
						box.name, got, base)
				}
			}
		})
	}
}

// TestCorridorLegend checks the right column's legend carries the arrival and
// departure colours, which the altitude legend has no use for.
func TestCorridorLegend(t *testing.T) {
	t.Parallel()

	frame := corridorFrame(corridorArrival())

	altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	altitude.Draw(altitudeCanvas, 0)

	corridor, corridorCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourCorridor))
	corridor.Draw(corridorCanvas, 0)

	// OTHER's muted swatch is not counted: the labels of both legends are set
	// in the muted ink, so the count measures the words as much as the box.
	for _, col := range []color.RGBA{theme.Night.Data, theme.Night.Caution} {
		if countColour(corridorCanvas, legendStrip, col) <= countColour(altitudeCanvas, legendStrip, col) {
			t.Errorf("the corridor legend carries no more %v than the altitude legend", col)
		}
	}
}

// TestCorridorFilterHidesTheOtherClasses checks set filter out over the
// settings block takes the arrival off the scope and leaves the departure.
func TestCorridorFilterHidesTheOtherClasses(t *testing.T) {
	t.Parallel()

	frame := corridorFrame(corridorArrival(), corridorDeparture())

	all, allCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourCorridor))
	all.Draw(allCanvas, 0)

	out, outCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	out.Apply(radar.Settings{Colour: radar.ColourCorridor, Filter: radar.FilterOut})
	out.Draw(outCanvas, 0)

	if got := out.Current().Filter; got != radar.FilterOut {
		t.Fatalf("Current().Filter = %q, want %q", got, radar.FilterOut)
	}

	if countColour(outCanvas, scopeBox, theme.Night.Data) >= countColour(allCanvas, scopeBox, theme.Night.Data) {
		t.Error("OUT left the arrival's data-coloured pixels on the scope")
	}

	if countColour(outCanvas, scopeBox, theme.Night.Caution) == 0 {
		t.Error("OUT took the departure off the scope as well")
	}
}
