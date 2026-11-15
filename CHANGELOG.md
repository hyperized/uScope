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
  a cap; the bare 3D view's has a `Z STARS` one.
- Aircraft lights, on by default. Every aircraft with a known heading carries
  a red light on its left wingtip, a green one on its right and a white one
  on the tail. All three strobes double-flash white together, 200 ms apart,
  twenty times a minute, half the certified minimum, so they keep the look of
  a strobe without pulling the eye off the traffic. A red beacon flashes on
  the fuselage 46 times a minute in every view: a disc with a pale core in
  3D, so it shows on any fill colour, and a five-pixel plus on the flat
  sprite. Each aircraft gets its own rhythm from its ICAO address, so the
  fleet does not blink in step. A flash lights the frame it started in, so
  the uConsole at 8 fps still shows every one, and at 30 fps a strobe lasts
  two frames and the beacon three or four. By day the steady lights are
  dimmer and the flashes are not. The phosphor and mono looks draw them in
  their own hue at different strengths. `s` and `--lights on|off` turn them
  on and off, and the key bar has an `S LIGHTS` cap.
- A bar for the bare views. `h` in `minimal` or `minimal3d`, or
  `--minimal-bar on`, puts a thin band across the top with the wordmark, the
  feed's dot, the sun block and both clocks. The picture moves down by the
  bar's height. It is off by default.
- A key bar that hides itself in the bare views. Any key pressed in `minimal`
  or `minimal3d` shows the softkeys along the bottom for four seconds, and
  each press starts the four seconds again. The bar is drawn over the
  picture, so the horizon and the projection centre stay put. It lists only
  the keys that work in a bare view, so the select arrows, `w` and `e` are
  left off and `h` for the top bar is added.
- `--tilt` sets where the 3D camera's elevation starts, and `--orbit on|off`
  and `--azimuth DEG` say whether it turns and where it stands, in degrees
  clockwise from north.
- `--zoom N` lands the auto range N steps inside the fit (negative widens)
  without turning auto off, so the view keeps following the traffic.
- `--minimal-shore` and `--minimal-airports` set where minimal and minimal3d
  start for the coastline and the airfield markers. The coastline defaults to
  `off` and the airfields to `on`, and `m` and `a` still flip them.
- `--battery off` draws no battery indicator and starts no poller, so a
  machine with no battery stops logging a missing one every 30 seconds.

### Changed

- `--theme` defaults to `auto` rather than `night`.
- The bare views draw the airfield markers by default. `a` or
  `--minimal-airports off` takes them away.
- The key bar has no `Q QUIT` cap any more. `S LIGHTS` took its room, since
  with both the widest bar ran past the margin. `q` still quits.
- The radar's layout scales with the canvas height. A canvas 2160 pixels
  tall or more draws its type, softkeys, column, markers, sprites and line
  widths at a whole multiple of the 1080 layout, so `--size 3840x2160` is the
  1080 picture at twice the size. Anything shorter renders exactly as before.


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
