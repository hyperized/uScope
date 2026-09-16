package radar_test

import (
	"os"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// minimal3DSettings is the bare 3D view at the fixed range the rest of these
// tests use, so nothing here depends on where auto range happens to land.
func minimal3DSettings() radar.Settings {
	return radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm}
}

// minimal3DScene builds a scene in the bare 3D view over a frame, plus the
// canvas it draws on.
func minimal3DScene(tb testing.TB, frame source.Frame, set radar.Settings) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, frame)
	scene.Apply(set)
	scene.Draw(canv, 0)

	return scene, canv
}

// TestMinimal3DHasNoChrome checks that the bare 3D view really is bare: no
// header band, no key bar, no column and no ground furniture.
//
// Two colours say all of it. Ink is what the header, the key caps, the card
// and the rows are set in, and nothing the bare picture draws uses it: the
// stalks and the receiver marker are muted, the models and their trails carry
// the traffic's own colours. Rule is the header's hairline, the card's border,
// the range rings and the airfield markers, and the bare view draws none of
// them either.
func TestMinimal3DHasNoChrome(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	))

	_, fullCanvas := minimal3DScene(t, frame, view3DSettings())

	if countColour(fullCanvas, fullCanvas.Bounds(), theme.Night.Ink) == 0 {
		t.Fatal("the 3D view with its chrome set no type, so this comparison proves nothing")
	}

	if countColour(fullCanvas, fullCanvas.Bounds(), theme.Night.Rule) == 0 {
		t.Fatal("the 3D view with its chrome drew no rules or rings, so this comparison proves nothing")
	}

	_, canv := minimal3DScene(t, frame, minimal3DSettings())

	if got := countColour(canv, canv.Bounds(), theme.Night.Ink); got != 0 {
		t.Errorf("the bare 3D view set %d ink pixels, want none: no header, no key bar, no column", got)
	}

	if got := countColour(canv, canv.Bounds(), theme.Night.Rule); got != 0 {
		t.Errorf("the bare 3D view drew %d rule pixels, want none: no hairline, no rings, no airfields", got)
	}
}

// TestMinimal3DDrawsTheTraffic checks that what is left is the traffic: the
// models and their trails, in the colours the altitude bands give them.
func TestMinimal3DDrawsTheTraffic(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	_, canv := minimal3DScene(t, frame, minimal3DSettings())

	if countColour(canv, canv.Bounds(), theme.Night.AltLow) == 0 {
		t.Error("the bare 3D view drew no low-band pixels, want the first aircraft")
	}

	if countColour(canv, canv.Bounds(), theme.Night.AltHigh) == 0 {
		t.Error("the bare 3D view drew no high-band pixels, want the second aircraft")
	}
}

// TestMinimal3DDrawsNoEnvelope checks that the coverage a frame carries reaches
// nothing in the bare view.
//
// The envelope is the only thing in the scene the coverage snapshot feeds, so
// two frames that differ in nothing else have to draw the same canvas. That is
// a stronger claim than counting a colour: the measured envelope is the accent
// faded towards the field, and a count would have to know the exact mix.
func TestMinimal3DDrawsNoEnvelope(t *testing.T) {
	t.Parallel()

	planes := []airplane.Snapshot{scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)}

	_, bare := minimal3DScene(t, sceneFrame(planes...), minimal3DSettings())
	_, withCoverage := minimal3DScene(t, covered(sceneFrame(planes...)), minimal3DSettings())

	if !identical(bare, withCoverage) {
		t.Error("coverage changed the bare 3D picture, want the envelope kept off it")
	}

	// The same pair in the full view has to differ, or the fixture is not
	// carrying an envelope for the bare view to be leaving out.
	_, fullBare := minimal3DScene(t, sceneFrame(planes...), view3DSettings())
	_, fullCovered := minimal3DScene(t, covered(sceneFrame(planes...)), view3DSettings())

	if identical(fullBare, fullCovered) {
		t.Fatal("coverage changed nothing in the full 3D view either, so this proves nothing")
	}
}

// TestMinimal3DDrawsNothingForTheSelection checks that n and p keep working
// while the picture says nothing about what they landed on.
//
// That is minimal's own rule. The ring and the callsign refer to a card and a
// row table that a bare view does not have, so all they could point at is
// something that is not on screen.
func TestMinimal3DDrawsNothingForTheSelection(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	scene, canv := minimal3DScene(t, frame, minimal3DSettings())

	before, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(before, 0)

	if !press(scene, 'n') {
		t.Fatal("the n key was not handled, want the selection to move")
	}

	scene.Draw(canv, 0)

	if !identical(canv, before) {
		t.Error("the selection changed the bare 3D picture, want nothing drawn for it")
	}

	if got := countColour(canv, canv.Bounds(), theme.Night.Accent); got != 0 {
		t.Errorf("the bare 3D view drew %d accent pixels, want no selection marker", got)
	}
}

// TestMinimal3DMarksTheReceiver checks the one thing the bare 3D view does put
// on the ground.
//
// The sky is empty, so the marker is the only thing that can be drawn at all
// and the canvas is either it or nothing. A receiver with no position decoded
// is the other half: there is nowhere to put the marker, and nothing is drawn.
func TestMinimal3DMarksTheReceiver(t *testing.T) {
	t.Parallel()

	_, canv := minimal3DScene(t, sceneFrame(), minimal3DSettings())

	if painted(canv, canv.Bounds()) == 0 {
		t.Error("the bare 3D view drew nothing over an empty sky, want the receiver's own marker")
	}

	frame := sceneFrame()
	frame.Receiver.Latitude, frame.Receiver.Longitude = 0, 0

	_, blank := minimal3DScene(t, frame, minimal3DSettings())

	if got := painted(blank, blank.Bounds()); got != 0 {
		t.Errorf("the bare 3D view drew %d pixels with no receiver position, want an empty field", got)
	}
}

// TestMinimal3DOverlaysFollowTheBarePair checks that a and m reach the bare
// views' own pair of toggles from the tilted one, the way they do from the
// flat one.
//
// The airfield markers are drawn in the rule colour, which nothing else in the
// bare picture uses: there are no rings there for them to be confused with.
func TestMinimal3DOverlaysFollowTheBarePair(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv := minimal3DScene(t, frame, minimal3DSettings())

	if got := countColour(canv, canv.Bounds(), theme.Night.Rule); got != 0 {
		t.Fatalf("the bare 3D view drew %d airfield pixels before a, want the pair to start off", got)
	}

	if !press(scene, 'a') {
		t.Fatal("the a key was not handled, want the airfield toggle to take it")
	}

	scene.Draw(canv, 0)

	if countColour(canv, canv.Bounds(), theme.Night.Rule) == 0 {
		t.Error("the bare 3D view drew no airfield pixels after a, want the markers")
	}
}

// TestMinimal3DFollowsTheTraffic checks that the cadence moves the bare 3D
// view's centre, which is what it shares with minimal.
//
// The fleet is well off the receiver, so a picture centred on the traffic and
// one centred on the antenna cannot be the same canvas.
func TestMinimal3DFollowsTheTraffic(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	set := minimal3DSettings()
	_, still := minimal3DScene(t, frame, set)

	set.Recentre = radar.DefaultRecentre

	_, moved := minimal3DScene(t, frame, set)

	if identical(still, moved) {
		t.Error("the cadence changed nothing in the bare 3D view, want it centred on the traffic")
	}
}

// TestMinimal3DRenderPNG writes the demo fleet in the bare 3D view to a
// directory an operator names, for looking at over a picture rather than
// proving anything a pixel count could check on its own.
//
// It is skipped unless USCOPE_PNG_DIR is set, the same as the other two render
// tests here.
func TestMinimal3DRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	writeViewPNG(t, dir, "view-minimal3d.png", radar.Settings{
		View: radar.ViewMinimal3D, Recentre: radar.DefaultRecentre,
	})
}

// TestMinimal3DDrawsNoStalks pins the user's call that the bare 3D view
// carries no vertical under each aircraft: the trails say where it has been.
// Stalks are Muted, and so is the receiver marker, so the check compares the
// bare view against the 3D view of the same frame rather than demanding zero:
// two aircraft high above the ground put hundreds of Muted pixels into the
// full view, and the bare view must keep only the marker's few dozen.
func TestMinimal3DDrawsNoStalks(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	_, fullCanvas := minimal3DScene(t, frame, view3DSettings())
	withStalks := countColour(fullCanvas, fullCanvas.Bounds(), theme.Night.Muted)

	_, canv := minimal3DScene(t, frame, minimal3DSettings())
	bare := countColour(canv, canv.Bounds(), theme.Night.Muted)

	const markerBudget = 64

	if withStalks <= markerBudget {
		t.Fatalf("the 3D view drew only %d muted pixels, so it has no stalks to compare against", withStalks)
	}

	if bare > markerBudget {
		t.Errorf("the bare 3D view drew %d muted pixels, want at most %d: only the receiver marker", bare, markerBudget)
	}
}
