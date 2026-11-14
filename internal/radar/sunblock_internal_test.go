package radar

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sun"
	"github.com/hyperized/uScope/pkg/text"
)

func TestResolveDaylight(t *testing.T) {
	t.Parallel()

	solstice := time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name      string
		receiver  source.Receiver
		condition sun.Condition
		times     string
	}{
		{
			name:      "schiphol at noon",
			receiver:  layerFrame(layerBaseLat).Receiver,
			condition: sun.Day,
			times:     "↑ 03:19 ↓ 20:07",
		},
		{
			name:      "the midnight sun has neither time",
			receiver:  source.Receiver{Latitude: 80, Longitude: 0, HasFix: true, Mode: source.FixManual},
			condition: sun.Day,
		},
		{name: "no position", condition: sun.Night},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.resolveDaylight(source.Frame{Now: solstice, Receiver: testCase.receiver})

			if scene.daylight.condition != testCase.condition {
				t.Errorf("condition = %v, want %v", scene.daylight.condition, testCase.condition)
			}

			if got := string(scene.daylight.times); got != testCase.times {
				t.Errorf("times = %q, want %q", got, testCase.times)
			}
		})
	}
}

// TestResolveDaylightCadence checks the answer stands for daylightEvery and
// is worked out again after it, and that losing the position clears the times.
func TestResolveDaylightCadence(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)
	frame := layerFrame(layerBaseLat)
	frame.Now = start

	scene := &Scene{}
	scene.resolveDaylight(frame)

	frame.Now = start.Add(daylightEvery - time.Second)
	scene.resolveDaylight(frame)

	if !scene.daylight.at.Equal(start) {
		t.Errorf("answer worked out again at %v, inside the cadence", scene.daylight.at)
	}

	frame.Now = start.Add(daylightEvery)
	scene.resolveDaylight(frame)

	if !scene.daylight.at.Equal(frame.Now) {
		t.Errorf("answer from %v still standing after the cadence", scene.daylight.at)
	}

	frame.Receiver = source.Receiver{}
	scene.resolveDaylight(frame)

	if len(scene.daylight.times) != 0 || scene.daylight.known {
		t.Errorf("times = %q, known = %v after losing the position", scene.daylight.times, scene.daylight.known)
	}
}

// TestDrawSunBlockWordOnly checks a day with neither time draws the word and
// nothing after it.
func TestDrawSunBlockWordOnly(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(640, 120)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := New(layerTestFaces(t), &stubSource{}, scope.New())
	scene.resolveDaylight(source.Frame{
		Now:      time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC),
		Receiver: source.Receiver{Latitude: 80, HasFix: true, Mode: source.FixManual},
	})

	lay := scene.newLayout(canv)

	left, drawn := scene.drawSunBlock(&lay, 600, 60, 0)
	if !drawn {
		t.Fatal("drawSunBlock drew nothing")
	}

	word, _ := text.Measure(scene.faces.Small, sun.Day.String(), text.WithSpacing(labelTracking))
	if want := 600 - word; left != want {
		t.Errorf("left = %d, want %d, the width of DAY alone", left, want)
	}
}

// TestResolveDaylightAllocations prices the frame the answer is due on, which
// TestDrawAllocations cannot: its clock never moves, so it only ever measures
// the frames between two answers.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestResolveDaylightAllocations(t *testing.T) {
	frame := layerFrame(layerBaseLat)
	frame.Now = time.Date(2026, time.June, 21, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))

	scene := &Scene{}
	scene.resolveDaylight(frame)

	got := testing.AllocsPerRun(50, func() {
		frame.Now = frame.Now.Add(daylightEvery)
		scene.resolveDaylight(frame)
	})

	if got != 0 {
		t.Errorf("resolveDaylight allocated %.1f times per answer, want 0", got)
	}
}
