package radar

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
)

// The recentring cadence.
const (
	// DefaultRecentre is what --recenter holds when nobody sets it. Three
	// minutes is long enough that the field reads as still rather than as
	// something sliding about, and short enough that traffic drifting along a
	// directional antenna's beam is caught well before it leaves the canvas.
	DefaultRecentre = 3 * time.Minute

	// MinRecentre and MaxRecentre bound the flag. Under ten seconds the glide
	// would be most of the interval and the picture would never settle; past
	// an hour the flag is off in all but name, and zero is the spelling for
	// that.
	MinRecentre = 10 * time.Second
	MaxRecentre = time.Hour

	// glideSpan is how long the centre takes to travel from where it was to
	// where the traffic now is. Two seconds reads as one picture moving
	// rather than as a cut, and nobody waits for it.
	glideSpan = 2 * time.Second
)

// ErrRecentre is returned for a --recenter value that is neither zero nor an
// interval the cadence can work at. One sentinel covers both, because from
// the operator's side "3 fortnights" and "1s" are the same mistake: a value
// the flag will not take.
var ErrRecentre = errors.New("radar: not a recentring interval")

// ParseRecentre reads a --recenter value.
//
// Zero is accepted and means off, which leaves minimal mode centred on the
// receiver the way it was before the flag existed. Everything else has to
// land inside MinRecentre to MaxRecentre, and is refused rather than clamped
// while there is still somebody reading.
func ParseRecentre(text string) (time.Duration, error) {
	every, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a duration", ErrRecentre, text)
	}

	if every == 0 {
		return 0, nil
	}

	if every < MinRecentre || every > MaxRecentre {
		return 0, fmt.Errorf("%w: got %s, want 0 or %s to %s", ErrRecentre, text, MinRecentre, MaxRecentre)
	}

	return every, nil
}

// geo is a point on the ground.
//
// It is a type rather than a pair of float64s passed about because the centre,
// the two ends of a glide and a centroid are all the same kind of thing, and
// four functions taking (latitude, longitude) side by side is four chances to
// swap them.
type geo struct {
	lat float64
	lon float64
}

// following reports whether minimal mode is chasing the traffic rather than
// sitting on the receiver.
//
// Both halves have to be true. --recenter 0 keeps the old behaviour, and the
// ordinary scope is never recentred at all: it has a home marker in the
// middle, range rings measured from it and a row table of bearings off it, so
// moving the centre would make three other blocks lie.
func (s *Scene) following() bool {
	return s.minimal() && s.recentre > 0
}

// follow moves minimal mode's centre and its range onto the traffic.
//
// Two clocks drive it and they are deliberately not the same clock. The
// cadence is measured on the frame's own timestamp, because a refit is about
// the picture going stale rather than about how long the program has been up.
// The glide is measured on elapsed, the render clock, because it is an
// animation and has to advance once per drawn frame whatever the feed is
// doing.
func (s *Scene) follow(frame source.Frame, elapsed time.Duration) {
	if s.due(frame.Now) {
		if centroid, found := centroidOf(frame.Planes); found {
			s.lastFit = frame.Now
			s.aim(centroid, elapsed)
			s.fitRangeAround(frame, centroid)
		}
	}

	s.glide(elapsed)
}

// due reports whether the cadence has come round again.
//
// The first centring is due the moment anything has a position, so a scope
// that has just started does not sit on the receiver for three minutes with
// all the traffic in one corner. A frame with nothing on it does not count as
// a centring, which is what leaves this true until the first aircraft arrives.
func (s *Scene) due(now time.Time) bool {
	if !s.haveCentre {
		return true
	}

	return !now.Before(s.lastFit.Add(s.recentre))
}

// aim points the centre at a new centroid.
//
// The first one snaps rather than gliding. There is nothing on screen yet for
// a glide to keep continuous, and a still frame is drawn exactly once, so a
// first centring that glided would render --png at the start of the path
// instead of at the end of it.
func (s *Scene) aim(centroid geo, elapsed time.Duration) {
	if !s.haveCentre {
		s.centre, s.haveCentre = centroid, true

		return
	}

	s.glideFrom, s.glideTo = s.centre, centroid
	s.glideAt, s.gliding = elapsed, true
}

// glide advances the centre along the path aim set up.
//
// It reads the elapsed the run loop hands the scene rather than accumulating
// a per-frame delta, so a frame that arrives late lands where it belongs on
// the path instead of pushing the whole move out behind it. An elapsed that
// goes backwards, which is what a test redrawing an old frame looks like,
// pins to the start rather than running the curve in reverse.
func (s *Scene) glide(elapsed time.Duration) {
	if !s.gliding {
		return
	}

	progress := float64(elapsed-s.glideAt) / float64(glideSpan)
	if progress >= 1 {
		s.centre, s.gliding = s.glideTo, false

		return
	}

	s.centre = between(s.glideFrom, s.glideTo, ease(max(progress, 0)))
}

// ease is the curve the centre travels on: still at both ends, quickest
// through the middle.
//
// Smoothstep rather than a cosine. It is two multiplies, it is exact at 0,
// 0.5 and 1 in float64 so the endpoints cannot drift, and nobody watching a
// scope could tell the two apart.
func ease(progress float64) float64 {
	return progress * progress * (3 - 2*progress)
}

// between interpolates one point towards another.
//
// The two coordinates move independently, which over the tens of nautical
// miles a scope covers is the same straight line the projection already
// assumes. It is not a great-circle path and does not need to be.
func between(start, target geo, along float64) geo {
	return geo{
		lat: start.lat + (target.lat-start.lat)*along,
		lon: start.lon + (target.lon-start.lon)*along,
	}
}

// centroidOf is the mean position of every aircraft that has one.
//
// The sentinel skipped here is the one uAirwaves' own distance function uses:
// an aircraft at exactly (0, 0) has had no position decoded yet rather than
// being in the Gulf of Guinea. Counting one of those would drag the centre a
// third of the way to Africa.
//
// The two means are taken separately and the antimeridian is not corrected
// for. A fleet spread across it would average onto the wrong side of the
// world, and it is not a fleet any single scope range holds anyway.
func centroidOf(planes airplanes.List) (geo, bool) {
	var sum geo

	count := 0

	for _, plane := range planes {
		if !positioned(plane.Latitude, plane.Longitude) {
			continue
		}

		sum.lat += plane.Latitude
		sum.lon += plane.Longitude
		count++
	}

	if count == 0 {
		return geo{}, false
	}

	return geo{lat: sum.lat / float64(count), lon: sum.lon / float64(count)}, true
}

// positioned reports whether a pair of coordinates is a position at all.
//
// Exactly (0, 0) is the undecoded state rather than a spot in the Gulf of
// Guinea: a Snapshot starts there and stays until a position message
// resolves, uAirwaves' own distance function reads it the same way, and so
// does the receiver's own fix. NaN is refused for the reason every other
// figure here is checked: it came off the air.
//
// It is the one rule, in one place. hasPosition asks it about an aircraft,
// newProjector about the point the scope is centred on, and drawReceiver
// about the antenna.
func positioned(latitude, longitude float64) bool {
	if latitude == 0 && longitude == 0 {
		return false
	}

	return !math.IsNaN(latitude) && !math.IsNaN(longitude)
}

// minimalOrigin is the point minimal mode projects from: the traffic's own
// centre while the cadence is on and has chosen one, the receiver otherwise.
//
// Falling back rather than refusing is what makes --recenter 0 exactly the
// behaviour minimal mode had before the flag existed, and it is also the
// first frame of every run, before anything with a position has arrived.
func (s *Scene) minimalOrigin(receiver source.Receiver) geo {
	if s.following() && s.haveCentre {
		return s.centre
	}

	return geo{lat: receiver.Latitude, lon: receiver.Longitude}
}

// applyRecentre sets the cadence and forgets whatever the last one worked out.
//
// A scene handed a fresh settings block should not carry on gliding towards a
// centre the new cadence never chose, and a block that turns recentring off
// has to put the projection back on the receiver rather than leaving it
// parked wherever the traffic was.
func (s *Scene) applyRecentre(every time.Duration) {
	s.recentre = every
	s.centre, s.haveCentre, s.gliding = geo{}, false, false
	s.lastFit = time.Time{}
}
