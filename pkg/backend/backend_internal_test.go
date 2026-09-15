package backend

import "testing"

// TestKindNumbering pins the constant values rather than only their names.
//
// Auto being zero is load bearing: app.Config's zero value has to mean "work
// the backend out", so a reordering of the block would silently turn an
// unset config into the framebuffer.
func TestKindNumbering(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind Kind
		want uint8
	}{
		{name: "auto is zero", kind: Auto, want: 0},
		{name: "fb", kind: Framebuffer, want: 1},
		{name: "kitty", kind: Kitty, want: 2},
		{name: "blocks", kind: Blocks, want: 3},
		{name: "png", kind: PNG, want: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := uint8(testCase.kind); got != testCase.want {
				t.Errorf("%v = %d, want %d", testCase.kind, got, testCase.want)
			}
		})
	}
}

// TestParseRoundTrip walks the closed set from the inside, so a sixth Kind
// added without a Parse case or a String case fails here.
func TestParseRoundTrip(t *testing.T) {
	t.Parallel()

	for kind := Auto; kind <= PNG; kind++ {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			if kind.String() == "invalid" {
				t.Fatalf("Kind(%d) has no String case", uint8(kind))
			}

			got, err := Parse(kind.String())
			if err != nil {
				t.Fatalf("Parse(%q): %v", kind.String(), err)
			}

			if got != kind {
				t.Errorf("Parse(%q) = %v, want %v", kind.String(), got, kind)
			}
		})
	}
}
