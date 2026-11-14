package radar

import (
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// TestThemeLabel pins the L cap's four words. Auto puts both its own name
// and the sun's answer on the cap, since auto alone would leave the operator
// guessing which palette it picked.
func TestThemeLabel(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		auto bool
		want string
	}{
		{name: "night", pal: theme.Night, want: "NIGHT"},
		{name: "day", pal: theme.Day, want: "DAY"},
		{name: "auto on night", pal: theme.Night, auto: true, want: "AUTO·NIGHT"},
		{name: "auto on day", pal: theme.MonoDay, auto: true, want: "AUTO·DAY"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)
			scene.SetAutoTheme(testCase.auto)

			if got := scene.capLabel(keyCap{key: "L", state: capTheme}); got != testCase.want {
				t.Errorf("L cap = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestAutoThemeDue pins every reason the sun gets asked again, and the one
// case it does not: the same receiver inside the interval.
func TestAutoThemeDue(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	here := source.Receiver{Latitude: 52.3105, Longitude: 4.7683, Mode: source.FixManual}

	moved := func(lat, lon float64) source.Receiver {
		receiver := here
		receiver.Latitude, receiver.Longitude = lat, lon

		return receiver
	}

	for _, testCase := range []struct {
		name     string
		fresh    bool
		now      time.Time
		receiver source.Receiver
		want     bool
	}{
		{name: "no answer yet", fresh: true, now: start, receiver: here, want: true},
		{name: "same place inside the interval", now: start.Add(autoThemeEvery - time.Second), receiver: here},
		{name: "same place at the interval", now: start.Add(autoThemeEvery), receiver: here, want: true},
		{name: "the clock went back", now: start.Add(-time.Second), receiver: here, want: true},
		{name: "latitude moved", now: start, receiver: moved(52.4, 4.7683), want: true},
		{name: "longitude moved", now: start, receiver: moved(52.3105, 4.8), want: true},
		{name: "position lost", now: start, receiver: source.Receiver{}, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var auto autoTheme
			if !testCase.fresh {
				auto.remember(start, here)
			}

			if got := auto.due(testCase.now, testCase.receiver); got != testCase.want {
				t.Errorf("due = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestSetAutoThemeForgetsTheAnswer checks that switching auto on, even when it
// already is, throws the last answer away, so the next frame asks the sun
// however recently it was last asked.
func TestSetAutoThemeForgetsTheAnswer(t *testing.T) {
	t.Parallel()

	scene := &Scene{}
	scene.SetAutoTheme(true)
	scene.sunTheme.remember(time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC), source.Receiver{})

	scene.SetAutoTheme(true)

	if !scene.sunTheme.at.IsZero() {
		t.Errorf("last answer at %v after SetAutoTheme, want none", scene.sunTheme.at)
	}
}

// TestAutoThemeEveryIsTenSeconds pins the interval the black-box tests assume
// for the auto theme.
func TestAutoThemeEveryIsTenSeconds(t *testing.T) {
	t.Parallel()

	if autoThemeEvery != 10*time.Second {
		t.Errorf("autoThemeEvery = %v, want 10s", autoThemeEvery)
	}
}
