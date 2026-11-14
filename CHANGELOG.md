# Changelog

All notable changes to this project are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `pkg/sun`: the sun's elevation at a position, whether it is above the
  horizon, the twilight it is in (`ConditionOf`, with `CivilLimit`,
  `NauticalLimit` and `AstronomicalLimit`), and the day's sunrise and sunset
  (`Rise` and `Set`).
- `--theme auto` switches between the night and day palettes by itself at
  the horizon, using the receiver's position, and stays on night while there
  is no position. `l` cycles night, day and auto, and the cap shows
  `AUTO·DAY` or `AUTO·NIGHT`.
- The sun's state in the header. With the receiver's position known, the band
  shows `DAY`, one of the three twilights or `NIGHT`, then today's sunrise and
  sunset in local time (`DAY ↑ 08:09 ↓ 18:42`), left of the clocks. The times
  follow the almanac convention, so they agree with published tables to the
  minute. The block is dropped on a band too narrow to hold it beside the
  source label.
- A sky in the tilted views. With the receiver's position known, the 3D views
  paint a gradient above the horizon whose colours follow the sun's elevation
  there, through night, nautical and civil twilight, low sun and day. Without
  a position, and at night, the picture is unchanged.
- Stars in the 3D views' night sky: a fixed field that turns with the camera,
  fully on below six degrees of sun and gone by sunrise. On by default. `z`
  and `--stars off` turn them off. The full 3D view's key bar has no room for
  a cap.

### Changed

- `--theme` defaults to `auto` rather than `night`.

### Fixed

- The tilted views no longer draw a faint dashed line down the 5 degree east
  meridian. Land is stored in five degree cells, and two cells' pieces now
  walk their shared boundary in the same edges, so the fill closes over it.

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
