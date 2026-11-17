# Changelog

All notable changes to this project are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `--lights auto` leaves the aircraft lights off in daylight and brings them
  in as the sun goes down: none with the sun ten degrees up or more, a fade
  from there to the horizon, and full strength from sunset to sunrise, at the
  receiver's position and whatever the theme. With no position known they
  stay on. On a display left running all day the beacons and strobes were
  the busiest thing on the screen at noon. `s` now cycles on, off and auto;
  `on` is still the default, and `on` and `off` draw exactly what they did.

### Fixed

- Under an altitude filter, the trail of an aircraft heard again after a gap
  no longer vanishes until its altitude is decoded and then fades back in.
  The filter now judges an aircraft with no altitude yet by the last one on
  its track, which is what kept the far traffic's trails blinking.
- Arrival tubes no longer go missing under a busy sky. Overflights were
  bundled into routes of their own that are never drawn, and they filled the
  64 routes uScope keeps: on one busy evening the routes were full within 75
  minutes, 48 of the 64 were overflights, and 79 of 86 arrivals were refused
  a route and drawn as thin lines only. Overflights now stay out of the
  routes, and the newest tracks are bundled first, so on a display left
  running all day the tracks left out when the routes run out are the oldest
  rather than the evening's arrivals. On the flat views an overflight's thin
  grey line is drawn whole again; the stretch that sat inside an undrawn
  route used to be missing.

## [0.2.0] - 2026-11-15

### Added

- `--filter all|low|mid|high|below-high|in|out|other` starts the run on one
  of the stops `f` cycles, so a display nobody is at the keyboard of can use
  it, and `f` carries on from there. The band stops belong to altitude
  colouring, the class stops to corridor colouring and `below-high` to both;
  a stop the colouring does not have, or any stop under airline colouring,
  starts on all. The operator filter in airline colouring has no spelling and
  stays on `f`.
- `--history PATH` keeps the lost tracks the corridors are drawn from across
  a restart. The file is read before the first frame, written once a minute
  while the tracks change and once more on shutdown, when aircraft still in
  the sky are written as lost at that moment and revived as soon as the feed
  hears them again. Tracks older than six half-lives are dropped on load. A
  file that will not read is one line on stderr and an empty start; a failed
  write is one line per distinct cause. Live sources only.
- Corridors: the tracks of aircraft that have gone out of range are bundled
  into routes, and every arrival and departure route is drawn as a tube, the
  bundle's average path with its spread as the width, so arrivals narrow onto
  the runway and departures open out. Each track is sorted when it is lost by
  its two ends against one airfield: cyan if it ended within 10 nm of the
  field, amber if it began there, grey otherwise. Grey routes get no tube; the
  flat views draw their tracks as thin lines. Arrivals and departures share a
  route when their bearings from the field agree within 30 degrees at 2, 12,
  30 and 50 nm. A track counts for half as much every `--corridor-half-life`
  (2h by default, 10m to 24h), and a track in no route is drawn on its own as
  a thin line on the flat views. The airfield is the nearest within 150 nm of
  the receiver, or the one `--corridor-field ICAO` names; an ICAO code the
  airport list lacks stops the start with an error. The flat views draw a
  translucent band on the ground. The 3D views draw a tube at the route's
  average altitude, shaded like glass: dense at the outline, thin where it
  faces the camera, with the back wall showing through, a hairline down each
  side and one ring at the mouth by the field. Its far end, and any end the
  filter or the edge of the view cuts, fades out over four stations. A tube
  runs out from the field only while at least three tracks have flown each
  2 nm station and their spread stays within 4 nm, with position, altitude
  and spread averaged over five stations. The altitude filter on `f` cuts a
  tube where it leaves the band on show. `u` and `--corridors off` turn the
  corridors off in every view. The `U TUBES` cap sits at the end of the flat
  views' key bar and after `M` in the bare views' reveal; the 3D view's bar
  has no room for it. `--demo` starts with seventy invented tracks around
  Schiphol from the last two hours.
- Colouring by corridor, third on the `c` cycle and `--colour corridor`. An
  aircraft that looks to be arriving at the corridor airfield is cyan, one
  leaving it amber, and the rest grey, by the same guess and in the same
  colours as the strip board's `DIR` column. Trails, coasting aircraft,
  callsigns and the trails of lost aircraft follow it; the level column and
  the panel's altitude figure keep the altitude colours. The legend reads
  `IN`, `OUT` and `OTHER`, and the cap reads `C CORRIDOR`. With no airfield
  placed everything is grey.
- In corridor colouring the aircraft list is three sections, `IN`, `OUT` and
  `OTHER`, each under a heading with its count, nearest first inside each and
  numbered from 01 again. The lines are shared a third each, spare lines go to
  the sections that need them, and a section that runs out of room ends on
  `+N MORE`. `n`, `p` and the arrows step through the list in the order it is
  drawn, the selected section scrolls to keep its strip in view, and the
  `SEL` number is the strip's own. Altitude and airline colouring list as
  before.
- `f` has a fifth stop while colouring by altitude: after `>25K` comes `<25K`,
  which keeps the low and mid bands together and hides the overflights, and
  rings both swatches on the legend. In corridor colouring `f` cycles `IN`,
  `OUT`, `OTHER` and `<25K`; holding it to one class shows that section of the
  list alone and leaves every corridor tube on screen.
- A flight strip board for `minimal3d`. `j` or `--minimal-board on` gives the
  right quarter of the bare 3D view to a column with the aircraft count, a
  strip per aircraft and a legend. The strips keep the callsign, the level,
  the ground speed and the distance at 1280x720, plus a `DIR` column that
  reads `IN`, `OUT` or `---`, and an emergency still shows as `EMG` beside the
  callsign. The camera moves in so the picture keeps its size in the three
  quarters left. Coloured by corridor, the board is grouped like the list
  and drops `DIR`. It is off by default, and `j` does nothing in the other
  views.
- The tilted views' ground no longer stops at the outer range ring. The sea,
  the land and the coastline run on past it and fade in 32 bands into the
  sky's horizon colour, or into the field when there is no sky. Inside the
  ring nothing changes.

- Aircraft no longer blink out at the edge of coverage. When uAirwaves drops
  one, the scope keeps drawing it for a minute where it was last heard, at its
  last heading, with its trail, fading to nothing, in every view and every
  trail mode. It has no lights or selection and is left out of the rows, the
  strips and the aircraft count. Heard again, it carries its old track in
  front of the new one, so the trail does not regrow from a dot.
- New aircraft fade in over two seconds. The first frame, `--png` included,
  and the first frame after the filter changes draw everything at full
  strength. Lights come on when the fade is done.
- The altitude filter stops hold 500 ft either side of each edge, so an
  aircraft cruising at FL250 under `>25K` stays put instead of switching on
  and off with every altitude report.
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
- A fourth look, `vector`. Its night keeps glass's hues on a field lifted off
  black, with the band, rules, softkeys, muted chrome and coast lifted with it.
  The sea is a blue and the land a low-chroma grey-green, strong enough to stay
  apart under the coloured route tubes. Its day is glass's day with that
  stronger ground and a darker coast. `--look vector` and the `k` cycle
  (glass, phosphor, mono, vector) both reach it, and the K cap reads VECTOR.

### Changed

- `--theme` defaults to `auto` rather than `night`.
- The bare views draw the airfield markers by default. `a` or
  `--minimal-airports off` takes them away.
- The bare 3D view's key bar folds `←→ TURN` and `[ ] TILT` into one
  `←→[ ] CAMERA` cap, which makes room for `J BOARD`. The full 3D bar keeps
  both.
- The key bar has no `Q QUIT` cap any more. `S LIGHTS` took its room, since
  with both the widest bar ran past the margin. `q` still quits.
- The radar's layout scales with the canvas height. A canvas 2160 pixels
  tall or more draws its type, softkeys, column, markers, sprites and line
  widths at a whole multiple of the 1080 layout, so `--size 3840x2160` is the
  1080 picture at twice the size. Anything shorter renders exactly as before.
- The land has a tint of its own where it used to be the bare field, which on
  the day palettes was a white page. Glass, phosphor and mono mix 8% of their
  green into the field, the share the sea takes of Data, so the land is green
  in those two and grey in mono; vector has a grey-green of its own.
  The sea and land colours now live on the palette rather than being mixed in
  the scene. In the 3D views the land fades into the horizon by the same bands
  as the sea. The flat scope paints the field back over the corners between
  its range ring and the square the land is filled in, so no land shows
  outside the ring.

### Fixed

- `--help` exits with status 0. It used to print the usage and then exit 1
  with "flag: help requested" on stderr, as if asking were an error.
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
