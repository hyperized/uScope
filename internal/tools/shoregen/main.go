// Command shoregen packs Natural Earth's coastline, lakes and land into the
// two files pkg/shore embeds.
//
// It is deliberately thin. Reading the GeoJSON, filtering it, cutting the
// polylines at cell boundaries, clipping the land rings to them and encoding
// both all live in pkg/shore, where they are tested; this is the flags, the
// files and the two lines of output that say what came out.
//
// Run it through `make shore-data`, which downloads the three inputs to a
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
	defaultLand      = "ne_10m_land.geojson"
	defaultOutput    = "pkg/shore/shore.bin.gz"
	defaultLandOut   = "pkg/shore/land.bin.gz"

	// The nouns the two tallies are reported with. One file holds open lines
	// and the other closed rings, and a run that said "polylines" twice would
	// leave a reader no way to tell which of the two had come out wrong.
	nounLines = "polylines"
	nounRings = "rings"

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
	land      string
	output    string
	landOut   string
}

// job is one output file: where it goes, what packs it, and what to call the
// shapes in it when the tally is printed.
type job struct {
	path string
	noun string
	pack func(io.Writer, []shore.Polyline) error
}

// run converts the inputs into the two packed files and reports what it wrote.
//
// It takes its arguments and its output stream rather than reading os.Args
// and printing, which is what lets a test drive the whole generator over a
// fixture in testdata.
func run(args []string, stdout io.Writer) error {
	chosen, err := parse(args, stdout)
	if err != nil {
		return err
	}

	lines, err := chosen.outlines()
	if err != nil {
		return err
	}

	if err := write(job{path: chosen.output, noun: nounLines, pack: shore.Encode}, lines, stdout); err != nil {
		return err
	}

	rings, err := chosen.rings()
	if err != nil {
		return err
	}

	return write(job{path: chosen.landOut, noun: nounRings, pack: shore.EncodeLand}, rings, stdout)
}

// outlines reads the coastline and the lakes into the polylines the shoreline
// file carries.
func (p paths) outlines() ([]shore.Polyline, error) {
	coastline, err := os.Open(p.coastline)
	if err != nil {
		return nil, fmt.Errorf("%s: opening the coastline: %w", name, err)
	}

	defer func() { _ = coastline.Close() }()

	lakes, err := os.Open(p.lakes)
	if err != nil {
		return nil, fmt.Errorf("%s: opening the lakes: %w", name, err)
	}

	defer func() { _ = lakes.Close() }()

	lines, err := shore.Build(coastline, lakes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return lines, nil
}

// rings reads the land and the lakes into the closed rings the land file
// carries.
//
// The lakes file is opened a second time rather than kept from the first pass.
// It is 5 MB of JSON in a generator that runs when Natural Earth publishes a
// release, and holding it in memory to save one read would be a saving that
// costs more to explain than it earns.
func (p paths) rings() ([]shore.Polyline, error) {
	land, err := os.Open(p.land)
	if err != nil {
		return nil, fmt.Errorf("%s: opening the land: %w", name, err)
	}

	defer func() { _ = land.Close() }()

	lakes, err := os.Open(p.lakes)
	if err != nil {
		return nil, fmt.Errorf("%s: opening the lakes: %w", name, err)
	}

	defer func() { _ = lakes.Close() }()

	rings, err := shore.BuildLand(land, lakes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return rings, nil
}

// write encodes the shapes to the job's file and reports the tally.
//
// The file is created rather than written in place through a temporary and a
// rename: this is a generator run by hand into a working tree, so a failed
// run leaving a short file is a `git checkout` away from fixed, and the extra
// dance would only hide which of the two happened.
func write(target job, lines []shore.Polyline, stdout io.Writer) error {
	// The path is whatever the operator passed to -out or -land-out. Picking
	// it is the point of those flags.
	file, err := os.Create(target.path) //nolint:gosec // operator-supplied output path.
	if err != nil {
		return fmt.Errorf("%s: creating %s: %w", name, target.path, err)
	}

	return encode(file, target, lines, stdout)
}

// encode packs the shapes into an open sink, closes it, and reports the tally
// on stdout. The job is carried through for the path in the error messages and
// for the noun in the tally.
//
// It is split from write so a test can hand it a sink that fails. write opens
// a real file from a path, and there is no path that succeeds at os.Create and
// then refuses the write that follows, so the two failures below would be
// unreachable from the other side of it.
func encode(out io.WriteCloser, target job, lines []shore.Polyline, stdout io.Writer) error {
	if err := target.pack(out, lines); err != nil {
		_ = out.Close()

		return fmt.Errorf("%s: encoding %s: %w", name, target.path, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("%s: closing %s: %w", name, target.path, err)
	}

	points := 0
	for _, line := range lines {
		points += len(line)
	}

	_, _ = fmt.Fprintf(stdout, "wrote %s: %d %s, %d points\n", target.path, len(lines), target.noun, points)

	return nil
}
