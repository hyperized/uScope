package radar_test

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// pictureHash is the SHA-256 of a canvas's pixels, which is how a test pins a
// whole frame to the one drawn before a change without checking a PNG in.
func pictureHash(t *testing.T, set radar.Settings) string {
	t.Helper()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, groupedCorridorFrame())
	scene.Apply(set)
	scene.Draw(canv, 0)
	scene.Draw(canv, groupedSettle)

	sum := sha256.Sum256(canv.Image().Pix)

	return hex.EncodeToString(sum[:])
}

// TestNoEmergencyLeavesTheGroupedListAlone checks a grouped list with nobody
// squawking an emergency draws the same pixels it drew before the EMERGENCY
// section existed. The hashes were taken from Draw before that section was
// added, and come out the same on arm64 and amd64.
func TestNoEmergencyLeavesTheGroupedListAlone(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		set  radar.Settings
		want string
	}{
		{
			name: "scope column",
			set:  radar.Settings{Colour: radar.ColourCorridor},
			want: "2f29da2640d93d67d1453e81a94c87ee8e7e347f50baad8254cddcc3b67656cd",
		},
		{
			name: "bare 3D board",
			set:  radar.Settings{Colour: radar.ColourCorridor, View: radar.ViewMinimal3D, Board: true},
			want: "39e59239224776848b7d021731f96578e42cf8625eaa595b58d3739c2cb0adc2",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := pictureHash(t, testCase.set); got != testCase.want {
				t.Errorf("frame hash = %s, want %s", got, testCase.want)
			}
		})
	}
}

// The grouped list's slots with the arrival squawking 7600, counted down from
// the first strip as in groups_external_test.go: EMERGENCY's heading and the
// arrival, then IN's heading with nobody under it, OUT's heading and the
// departure, OTHER's heading and the overflight.
const (
	slotEmergencyHead = 0
	slotDistressed    = 1
	slotEmptyIn       = 2
	slotOutHead       = 3
	slotLateDeparture = 4
)

// distressedCorridorFrame is groupedCorridorFrame with the arrival
// squawking 7600.
func distressedCorridorFrame() source.Frame {
	frame := groupedCorridorFrame()

	for index, plane := range frame.Planes {
		if plane.ICAO == corridorArrival().ICAO {
			frame.Planes[index] = squawking(plane, "7600")
		}
	}

	return frame
}

// squawking is plane squawking code, with the flag uAirwaves sets from it.
func squawking(plane airplane.Snapshot, code string) airplane.Snapshot {
	plane.Squawk, plane.Emergency = code, true

	return plane
}

// TestEmergencySectionOpensTheList draws the scope's column with the arrival
// squawking 7600 and checks EMERGENCY's heading and the arrival's strip are
// the first two lines, both carrying the warning red, and that IN's heading
// follows with nobody under it, so OUT and the departure move down one line.
func TestEmergencySectionOpensTheList(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, distressedCorridorFrame())
	scene.Apply(radar.Settings{Colour: radar.ColourCorridor})
	scene.Draw(canv, 0)
	scene.Draw(canv, groupedSettle)

	for _, slot := range []int{slotEmergencyHead, slotDistressed} {
		if countColour(canv, stripRowBox(slot), theme.Night.Warn) == 0 {
			t.Errorf("no warning red on line %d", slot)
		}
	}

	if countColour(canv, stripRowBox(slotEmptyIn), theme.Night.Data) == 0 {
		t.Error("line 2 is not IN's heading")
	}

	if countColour(canv, stripRowBox(slotOutHead), theme.Night.Caution) == 0 ||
		countColour(canv, stripRowBox(slotLateDeparture), theme.Night.Caution) == 0 {
		t.Error("OUT's heading and the departure are not on lines 3 and 4")
	}
}

// TestCeilingKeepsAnEmergencyOnTheBoard puts the overflight at 35,000 feet on
// 7500 and holds the scope under the ceiling, which hides that band. Its
// strip, and the red box on it, stay on the board.
func TestCeilingKeepsAnEmergencyOnTheBoard(t *testing.T) {
	t.Parallel()

	board := image.Rect(boardColumnLeft, boardFirstStripTop, boardColumnRight, boardLegendTop)

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  bool
	}{
		{name: "squawking 7500", plane: squawking(corridorOverflight(), "7500"), want: true},
		{name: "squawking nothing", plane: corridorOverflight(), want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, corridorFrame(corridorArrival(), testCase.plane))
			scene.Apply(radar.Settings{Colour: radar.ColourAltitude})
			pressFilter(t, scene, pressFilterCeiling)
			scene.Draw(canv, 0)
			scene.Draw(canv, groupedSettle)

			if got := countColour(canv, board, theme.Night.Warn) > 0; got != testCase.want {
				t.Errorf("warning red on the board = %v, want %v", got, testCase.want)
			}
		})
	}
}
