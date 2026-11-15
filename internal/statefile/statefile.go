package statefile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/hyperized/uScope/internal/control"
)

// maxFile is how much of a state file Load reads. A real one is about 200
// bytes; the cap is there so a file somebody pointed --state at by mistake
// cannot be read into memory whole.
const maxFile = 64 << 10

// header opens every file Encode writes. Decode skips it like any comment.
const header = "# uScope state, rewritten after every control-port change. Edit it while uScope is stopped.\n"

// ErrLine is wrapped by every error Decode hands its skip function for a line
// that is not one name and one value.
var ErrLine = errors.New("statefile: want one name and one value")

// Encode renders the state as a file: the header, then one "name value" line
// per field in control.State.Fields order.
func Encode(state control.State) []byte {
	var out bytes.Buffer

	out.WriteString(header)

	for _, field := range state.Fields() {
		out.WriteString(field.Name)
		out.WriteByte(' ')
		out.WriteString(field.Value)
		out.WriteByte('\n')
	}

	return out.Bytes()
}

// Decode reads a state file from r and applies each line to base, in file
// order, returning the result.
//
// Blank lines and lines starting with # are skipped quietly. Any other line
// that is not exactly one name and one value set accepts is handed to skip,
// with its line number, and left out; the rest of the file still applies. The
// error is only for r itself failing, or a line longer than control.MaxLine,
// and the state returned with it holds every line read before that.
func Decode(r io.Reader, base control.State, skip func(line int, err error)) (control.State, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, control.MaxLine+1), control.MaxLine+1)

	state := base
	number := 0

	for scanner.Scan() {
		number++

		change, err := parseLine(scanner.Text())
		if err != nil {
			skip(number, err)

			continue
		}

		if change != nil {
			change(&state)
		}
	}

	if err := scanner.Err(); err != nil {
		return state, fmt.Errorf("statefile: after line %d: %w", number, err)
	}

	return state, nil
}

// parseLine reads one line. A nil change with a nil error is a line with
// nothing on it to apply.
func parseLine(line string) (control.Change, error) {
	text := strings.TrimSpace(line)
	if text == "" || strings.HasPrefix(text, "#") {
		return nil, nil //nolint:nilnil // nothing to apply is not an error, and there is no change to hand back.
	}

	words := strings.Fields(text)
	if len(words) != 2 { //nolint:mnd // a name and a value.
		return nil, fmt.Errorf("%w: %q", ErrLine, text)
	}

	change, err := control.ParseSetting(words[0], words[1])
	if err != nil {
		return nil, fmt.Errorf("statefile: %w", err)
	}

	return change, nil
}

// Load reads the state file at path over base and returns the result, so
// the file wins for every field it holds and base stands for the rest.
//
// Nothing here stops a run. A missing file is the first start and returns
// base without a word. A line Decode skips, a file that cannot be read and a
// file that breaks off are each one warning on warn, and whatever was read
// before the trouble still applies.
func Load(path string, base control.State, warn io.Writer) control.State {
	// Opened non-blocking, because a plain open of a FIFO waits until
	// something writes to it, and a mistyped --state would hang the start with
	// nothing on screen to say why. Checking the type before opening would
	// leave a window for the path to change in between; checking the open
	// file does not. Reads from a regular file never block either way.
	//nolint:gosec // operator-supplied --state path, like --png.
	file, err := os.OpenFile(path, os.O_RDONLY|nonBlocking, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return base
	}

	if err != nil {
		warnf(warn, "warning: state file: %v; starting from the flags\n", err)

		return base
	}

	defer func() { _ = file.Close() }()

	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		warnf(warn, "warning: state file %s is not a regular file; starting from the flags\n", path)

		return base
	}

	state, err := Decode(io.LimitReader(file, maxFile), base, func(line int, err error) {
		warnf(warn, "warning: state file %s line %d: %v; skipped\n", path, line, err)
	})
	if err != nil {
		warnf(warn, "warning: state file %s: %v; the rest is skipped\n", path, err)
	}

	return state
}

// warnf writes one warning. Its error is dropped: a warning that cannot be
// printed has nowhere better to go, and the picture matters more.
func warnf(warn io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(warn, format, args...)
}
