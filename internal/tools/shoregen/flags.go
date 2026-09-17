package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// errEmptyPath is returned for a path flag given as an empty string.
var errEmptyPath = errors.New(name + ": path must not be empty")

// parse declares the five paths and validates what landed in them.
//
// ContinueOnError rather than ExitOnError, so a bad flag comes back as an
// error the caller turns into an exit code instead of flag calling os.Exit
// from underneath a test.
func parse(args []string, output io.Writer) (paths, error) {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(output)

	chosen := paths{}
	set.StringVar(&chosen.coastline, "coastline", defaultCoastline,
		"Natural Earth ne_10m_coastline.geojson to read")
	set.StringVar(&chosen.lakes, "lakes", defaultLakes,
		"Natural Earth ne_10m_lakes.geojson to read")
	set.StringVar(&chosen.land, "land", defaultLand,
		"Natural Earth ne_10m_land.geojson to read")
	set.StringVar(&chosen.output, "out", defaultOutput,
		"packed shoreline file to write")
	set.StringVar(&chosen.landOut, "land-out", defaultLandOut,
		"packed land file to write")

	if err := set.Parse(args); err != nil {
		return paths{}, fmt.Errorf("%s: parsing flags: %w", name, err)
	}

	return chosen, chosen.check()
}

// check refuses an empty path. A flag given as "" is somebody's unset shell
// variable, not a request to open a file named nothing.
func (p paths) check() error {
	for _, field := range []struct {
		flag  string
		value string
	}{
		{flag: "-coastline", value: p.coastline},
		{flag: "-lakes", value: p.lakes},
		{flag: "-land", value: p.land},
		{flag: "-out", value: p.output},
		{flag: "-land-out", value: p.landOut},
	} {
		if field.value == "" {
			return fmt.Errorf("%w: %s", errEmptyPath, field.flag)
		}
	}

	return nil
}
