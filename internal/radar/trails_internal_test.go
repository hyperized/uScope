package radar

import "testing"

// The four words the trail cap carries, named once here because both this file
// and the key-bar table in radar_internal_test.go read them back.
const (
	labelTrailOff   = "OFF"
	labelTrailShort = "SHORT"
	labelTrailLong  = "LONG"
	labelTrailAll   = "ALL"
)

// TestTrailModeCycle walks the t key all the way round and checks it comes
// back where it started.
//
// The order is the one the cap reads out, and it is an order rather than a
// set: off, then a short trail, then the long one, then everything including
// the ghosts. It goes from least to most on purpose, so a hand on the key is
// walking one way along a scale rather than jumping between unrelated states.
func TestTrailModeCycle(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		from trailMode
		want trailMode
	}{
		{name: "off goes to short", from: trailOff, want: trailShort},
		{name: "short goes to long", from: trailShort, want: trailLong},
		{name: "long goes to all", from: trailLong, want: trailAll},
		{name: "all wraps back to off", from: trailAll, want: trailOff},
		{name: "the zero value cycles the way long does", from: "", want: trailAll},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.from.next(); got != testCase.want {
				t.Errorf("trailMode(%q).next() = %q, want %q", testCase.from, got, testCase.want)
			}
		})
	}

	// Four presses from any starting point have to land back on it, which is
	// what makes the key reversible without a second one to go the other way.
	mode := trailLong
	for range 4 {
		mode = mode.next()
	}

	if mode != trailLong {
		t.Errorf("four presses from long left %q, want long back", mode)
	}
}

// TestTrailModeLabel checks the word the key cap carries for each mode.
//
// The cap names the mode rather than the setting, the way the colour and theme
// caps already do: a cap reading TRAILS says there is a trail setting without
// saying which of four states it is in, which is the question it is being
// asked.
func TestTrailModeLabel(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		mode trailMode
		want string
	}{
		{name: "the off mode", mode: trailOff, want: labelTrailOff},
		{name: "the short mode", mode: trailShort, want: labelTrailShort},
		{name: "the long mode", mode: trailLong, want: labelTrailLong},
		{name: "the all mode", mode: trailAll, want: labelTrailAll},
		{name: "the zero value reads as long", mode: "", want: labelTrailLong},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.label(); got != testCase.want {
				t.Errorf("trailMode(%q).label() = %q, want %q", testCase.mode, got, testCase.want)
			}
		})
	}
}

// TestTrailModeGhostsDrawn checks that exactly one mode puts a lost contact on
// the field.
func TestTrailModeGhostsDrawn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		mode trailMode
		want bool
	}{
		{mode: trailOff},
		{mode: trailShort},
		{mode: trailLong},
		{mode: trailAll, want: true},
		{mode: ""},
	} {
		t.Run(string(testCase.mode)+"/"+testCase.mode.label(), func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.ghostsDrawn(); got != testCase.want {
				t.Errorf("trailMode(%q).ghostsDrawn() = %v, want %v", testCase.mode, got, testCase.want)
			}
		})
	}
}

// TestTrailPlan is the whole of what the four modes mean, read off in one
// table: how far back into a history each one reaches, and how faint the
// oldest segment it draws comes out.
//
// The long history is deliberately longer than shortTrailFixes so the short
// mode has something to cut, and the short one is deliberately shorter so the
// same mode has nothing to cut and falls back to drawing all of it.
func TestTrailPlan(t *testing.T) {
	t.Parallel()

	const (
		longHistory  = 40
		shortHistory = 5
	)

	for _, testCase := range []struct {
		name      string
		mode      trailMode
		count     int
		wantDrawn bool
		wantFrom  int
		wantFloor float64
	}{
		{name: "off draws nothing however long the history", mode: trailOff, count: longHistory},
		{
			name: "long runs the whole history from the quarter-strength floor",
			mode: trailLong, count: longHistory,
			wantDrawn: true, wantFloor: trailMinAlpha,
		},
		{
			name: "the zero value plans the way long does",
			mode: "", count: longHistory,
			wantDrawn: true, wantFloor: trailMinAlpha,
		},
		{
			name: "all runs the whole history at full strength",
			mode: trailAll, count: longHistory,
			wantDrawn: true, wantFloor: trailMaxAlpha,
		},
		{
			name: "short keeps the last twelve fixes and fades to nothing",
			mode: trailShort, count: longHistory,
			wantDrawn: true, wantFrom: longHistory - shortTrailFixes,
		},
		{
			name: "short with less history than that keeps all of it",
			mode: trailShort, count: shortHistory,
			wantDrawn: true,
		},
		{
			name: "exactly twelve fixes is the boundary short keeps whole",
			mode: trailShort, count: shortTrailFixes,
			wantDrawn: true,
		},
		{
			name: "thirteen is one more than it keeps",
			mode: trailShort, count: shortTrailFixes + 1,
			wantDrawn: true, wantFrom: 1,
		},
		{name: "one fix is a point and not a line", mode: trailLong, count: 1},
		{name: "no fixes at all", mode: trailAll, count: 0},
		{
			name: "two fixes is the shortest line there is",
			mode: trailLong, count: 2,
			wantDrawn: true, wantFloor: trailMinAlpha,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, drawn := testCase.mode.plan(testCase.count)
			if drawn != testCase.wantDrawn {
				t.Fatalf("trailMode(%q).plan(%d) drawn = %v, want %v",
					testCase.mode, testCase.count, drawn, testCase.wantDrawn)
			}

			if !drawn {
				return
			}

			if plan.from != testCase.wantFrom {
				t.Errorf("plan.from = %d, want %d", plan.from, testCase.wantFrom)
			}

			if plan.floor != testCase.wantFloor {
				t.Errorf("plan.floor = %v, want %v", plan.floor, testCase.wantFloor)
			}

			if kept := testCase.count - plan.from; testCase.mode == trailShort && kept > shortTrailFixes {
				t.Errorf("the short mode kept %d fixes, want at most %d", kept, shortTrailFixes)
			}
		})
	}
}

// TestTrailPlanAlphaAtBothEnds reads each mode's plan back through the fade
// the draw path applies, which is where the floor actually turns into a
// colour.
//
// The head is full strength in every mode that draws at all: it is where the
// aeroplane is, and a track whose head was dimmer than its tail would point
// backwards. What separates the modes is the other end.
func TestTrailPlanAlphaAtBothEnds(t *testing.T) {
	t.Parallel()

	const history = 40

	for _, testCase := range []struct {
		name     string
		mode     trailMode
		wantTail float64
	}{
		{name: "short fades to nothing", mode: trailShort, wantTail: 0},
		{name: "long fades to a quarter", mode: trailLong, wantTail: trailMinAlpha},
		{name: "all does not fade", mode: trailAll, wantTail: trailMaxAlpha},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, drawn := testCase.mode.plan(history)
			if !drawn {
				t.Fatalf("trailMode(%q).plan(%d) drew nothing", testCase.mode, history)
			}

			span := float64(history - plan.from - 1)

			// Segment zero is the tail end of the run: it is the alpha the
			// first drawn segment climbs away from.
			if got := segmentAlpha(plan.floor, 0, span); got != testCase.wantTail {
				t.Errorf("alpha at the tail = %v, want %v", got, testCase.wantTail)
			}

			if got := segmentAlpha(plan.floor, int(span), span); got != trailMaxAlpha {
				t.Errorf("alpha at the head = %v, want %v", got, trailMaxAlpha)
			}
		})
	}
}
