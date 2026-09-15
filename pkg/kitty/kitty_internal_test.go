package kitty

import "testing"

// testFirstID and testSecondID are fixed ids used across this file's table
// tests. They only need to be distinct and nonzero; the values themselves
// carry no meaning.
const (
	testFirstID  uint32 = 10
	testSecondID uint32 = 20
)

// TestDefaultImageIDs checks the two built-in ids are distinct and nonzero,
// the same property WithIDs enforces on a caller-supplied pair, so New
// never starts an Encoder in a state its own option would refuse to
// produce.
func TestDefaultImageIDs(t *testing.T) {
	t.Parallel()

	if defaultFirstImageID == 0 || defaultSecondImageID == 0 {
		t.Fatalf("default image ids must be nonzero: first=%d second=%d", defaultFirstImageID, defaultSecondImageID)
	}

	if defaultFirstImageID == defaultSecondImageID {
		t.Fatalf("default image ids must differ, both are %d", defaultFirstImageID)
	}
}

// TestDefaultChunkSize checks the default chunk size is itself already a
// value WithChunkSize would accept unchanged.
func TestDefaultChunkSize(t *testing.T) {
	t.Parallel()

	if defaultChunkSize < minChunkSize || defaultChunkSize > maxChunkSize {
		t.Fatalf("defaultChunkSize %d is outside [%d, %d]", defaultChunkSize, minChunkSize, maxChunkSize)
	}

	if defaultChunkSize%base64Quantum != 0 {
		t.Fatalf("defaultChunkSize %d is not a multiple of %d", defaultChunkSize, base64Quantum)
	}
}

// TestNextID exercises the id-alternation rule directly against the
// encoder's state, rather than through a round trip of encoding a frame.
func TestNextID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		displayed bool
		lastID    uint32
		want      uint32
	}{
		{name: "nothing displayed yet", displayed: false, lastID: 0, want: testFirstID},
		{name: "first was last displayed", displayed: true, lastID: testFirstID, want: testSecondID},
		{name: "second was last displayed", displayed: true, lastID: testSecondID, want: testFirstID},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			enc := &Encoder{
				firstID:   testFirstID,
				secondID:  testSecondID,
				displayed: tcase.displayed,
				lastID:    tcase.lastID,
			}

			if got := enc.nextID(); got != tcase.want {
				t.Fatalf("nextID() = %d, want %d", got, tcase.want)
			}
		})
	}
}

// TestWithIDsAppliesOrRejects checks the option moves both ids together on
// a valid pair and leaves both fields untouched on an invalid one, rather
// than partially applying a broken pair.
func TestWithIDsAppliesOrRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		first   uint32
		second  uint32
		applied bool
	}{
		{name: "valid distinct nonzero pair", first: 100, second: 200, applied: true},
		{name: "first is zero", first: 0, second: 200, applied: false},
		{name: "second is zero", first: 100, second: 0, applied: false},
		{name: "equal ids", first: 100, second: 100, applied: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			enc := &Encoder{firstID: testFirstID, secondID: testSecondID}
			WithIDs(tcase.first, tcase.second)(enc)

			wantFirst, wantSecond := testFirstID, testSecondID
			if tcase.applied {
				wantFirst, wantSecond = tcase.first, tcase.second
			}

			if enc.firstID != wantFirst || enc.secondID != wantSecond {
				t.Fatalf("after WithIDs(%d, %d): firstID=%d secondID=%d, want %d, %d",
					tcase.first, tcase.second, enc.firstID, enc.secondID, wantFirst, wantSecond)
			}
		})
	}
}

// TestWithChunkSizeAppliesOrRejects checks the option rounds an accepted
// size down to a multiple of 4 and leaves the current chunk size untouched
// outside the accepted range.
func TestWithChunkSizeAppliesOrRejects(t *testing.T) {
	t.Parallel()

	const startingChunkSize = 64

	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "already a multiple of 4", size: 128, want: 128},
		{name: "rounds down to a multiple of 4", size: 130, want: 128},
		{name: "minimum accepted", size: minChunkSize, want: minChunkSize},
		{name: "maximum accepted", size: maxChunkSize, want: maxChunkSize},
		{name: "below minimum is rejected", size: minChunkSize - 1, want: startingChunkSize},
		{name: "above maximum is rejected", size: maxChunkSize + 1, want: startingChunkSize},
		{name: "zero is rejected", size: 0, want: startingChunkSize},
		{name: "negative is rejected", size: -8, want: startingChunkSize},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			enc := &Encoder{chunkSize: startingChunkSize}
			WithChunkSize(tcase.size)(enc)

			if enc.chunkSize != tcase.want {
				t.Fatalf("after WithChunkSize(%d): chunkSize=%d, want %d", tcase.size, enc.chunkSize, tcase.want)
			}
		})
	}
}
