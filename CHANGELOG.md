# Changelog

All notable changes to this project are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-09-23

First public release.

### Added

- A pixel canvas blitted to the uConsole's framebuffer with fbcon's rotation
  applied, or to a terminal through the Kitty graphics protocol or half
  blocks, or to a PNG file with `--png`.
- The radar scope: silhouettes turned to heading, trails in four modes,
  range rings, cardinals, the nearest airfields, and the coastline and water
  from Natural Earth embedded in the binary.
- A right column with the selected flight, a row table with an attitude
  cell per aircraft, and a legend. `w` hides it.
- Aircraft coloured by altitude band or by airline, read off the callsign
  prefix, and a filter over either legend with `f`.
- A 3D view with low polygon aircraft models, an orbiting camera, and the
  receiving envelope: the theoretical radio horizon and the measured one
  from a bearing by altitude grid of what the antenna has heard.
- Minimal mode, which follows the traffic's own centre for a directional
  antenna, and a bare 3D view.
- Three looks, glass, phosphor and mono, each in night and day.
- Aircraft from the local RTL-SDR, a Mode S BEAST feed, a captured IQ file,
  or an invented fleet for a machine with no receiver.
- The receiver's position from gpsd, from `--lat` and `--lon`, or worked
  out from the aircraft by uAirwaves' self-locator, with the home marker
  drawn as a dashed ring at the estimate's spread.
- Bias-tee and gain sweep on the local SDR, and a battery indicator in the
  header.
- Four embedded Terminus faces through a PSF parser of its own.
