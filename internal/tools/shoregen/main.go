// Command shoregen packs Natural Earth's coastline and lakes into the file
// pkg/shore embeds.
//
// It is deliberately thin. Reading the GeoJSON, filtering it, cutting the
// polylines at cell boundaries and encoding them all live in pkg/shore, where
// they are tested; this is the flags, the files and the one line of output
// that says what came out.
//
// Run it through `make shore-data`, which downloads the two inputs to a
// temporary directory first. See pkg/shore/README.md for the provenance.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/hyperized/uScope/pkg/shore"
)

const (
	name = "shoregen"

	// The default paths match what `make shore-data` downloads, so running
	// the generator by hand after a make run needs no arguments.
	defaultCoastline = "ne_10m_coastline.geojson"
	defaultLakes     = "ne_10m_lakes.geojson"
	defaultOutput    = "pkg/shore/shore.bin.gz"

	exitOK      = 0
	exitFailure = 1
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(exitFailure)
	}

	os.Exit(exitOK)
}

// paths is the validated command line.
type paths struct {
	coastline string
	lakes     string
	output    string
}

// run converts the two inputs into the packed file and reports what it wrote.
//
// It takes its arguments and its output stream rather than reading os.Args
// and printing, which is what lets a test drive the whole generator over a
// fixture in testdata.
func run(args []string, stdout io.Writer) error {
	chosen, err := parse(args, stdout)
	if err != nil {
		return err
	}

	coastline, err := os.Open(chosen.coastline)
	if err != nil {
		return fmt.Errorf("%s: opening the coastline: %w", name, err)
	}

	defer func() { _ = coastline.Close() }()

	lakes, err := os.Open(chosen.lakes)
	if err != nil {
		return fmt.Errorf("%s: opening the lakes: %w", name, err)
	}

	defer func() { _ = lakes.Close() }()

	lines, err := shore.Build(coastline, lakes)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return write(chosen.output, lines, stdout)
}

// write encodes the polylines to the output file and reports the tally.
//
// The file is created rather than written in place through a temporary and a
// rename: this is a generator run by hand into a working tree, so a failed
// run leaving a short file is a `git checkout` away from fixed, and the extra
// dance would only hide which of the two happened.
func write(path string, lines []shore.Polyline, stdout io.Writer) error {
	// The path is whatever the operator passed to -out. Picking it is the
	// point of the flag.
	file, err := os.Create(path) //nolint:gosec // operator-supplied output path.
	if err != nil {
		return fmt.Errorf("%s: creating %s: %w", name, path, err)
	}

	return encode(file, path, lines, stdout)
}

// encode packs the polylines into an open sink, closes it, and reports the
// tally on stdout. path is carried through for the error messages alone.
//
// It is split from write so a test can hand it a sink that fails. write opens
// a real file from a path, and there is no path that succeeds at os.Create and
// then refuses the write that follows, so the two failures below would be
// unreachable from the other side of it.
func encode(out io.WriteCloser, path string, lines []shore.Polyline, stdout io.Writer) error {
	if err := shore.Encode(out, lines); err != nil {
		_ = out.Close()

		return fmt.Errorf("%s: encoding %s: %w", name, path, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("%s: closing %s: %w", name, path, err)
	}

	points := 0
	for _, line := range lines {
		points += len(line)
	}

	_, _ = fmt.Fprintf(stdout, "wrote %s: %d polylines, %d points\n", path, len(lines), points)

	return nil
}
