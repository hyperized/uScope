package control

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
)

// MaxLine is the longest command line the port reads, in bytes, not counting
// the line ending. The longest real command is well under fifty; the limit is
// there so a client cannot make the server buffer whatever it sends.
const MaxLine = 256

// tenths is the precision the reply prints angles and ranges to.
const tenths = 10

// Errors a command line can be refused with. The reply carries the message
// after the package prefix, so "err unknown command" is ErrUnknownCommand.
var (
	ErrUnknownCommand = errors.New("control: unknown command")
	ErrUsage          = errors.New("control: usage")
	ErrUnknownSetting = errors.New("control: unknown setting")
	ErrValue          = errors.New("control: bad value")
	ErrKey            = errors.New("control: not a key")
	ErrQuit           = errors.New("control: quitting is not available over the control port")
	ErrBiasTee        = errors.New("control: the bias-tee is not available over the control port")
	ErrLineTooLong    = errors.New("control: line too long")
	ErrNotUTF8        = errors.New("control: line is not UTF-8")
)

// Verb is what a command asks for.
type Verb uint8

// The four commands.
const (
	// VerbGet reports the state and changes nothing.
	VerbGet Verb = iota + 1

	// VerbSet changes one setting; Command.Change carries it.
	VerbSet

	// VerbKey presses a key; Command.Key carries it.
	VerbKey

	// VerbHelp lists the commands. The server answers it itself, so it never
	// reaches the run loop.
	VerbHelp
)

// State is what the reply line reports: the radar's settings as the scene
// reads them back, and the theme and look the run loop holds.
type State struct {
	Radar radar.Settings
	Theme theme.Kind
	Look  theme.Look
}

// Change is a parsed and validated set command, applied to a State the run
// loop has just read back. It holds only the value it sets, so it is safe to
// build on one goroutine and run on another.
type Change func(state *State)

// Command is one parsed command line.
type Command struct {
	Verb   Verb
	Change Change
	Key    input.Key
}

// setter is one name set accepts and how its value is read.
type setter struct {
	name  string
	parse func(text string) (Change, error)
}

// setters is the allow list behind set, in the order help lists it. Every
// value goes through the same parser its command-line flag does, so the two
// cannot accept different things.
//
// shore and airports set both pairs, the scope's and the bare views'. An
// operator typing "set shore on" means the coastline on the picture they are
// looking at, and setting both means the answer is the same after v.
func setters() []setter {
	return []setter{
		{"view", changes(radar.ParseView, func(s *State, v radar.View) { s.Radar.View = v })},
		{"shore", changes(radar.ParseToggle, func(s *State, v radar.Toggle) {
			s.Radar.Shore, s.Radar.MinimalShore = v, v.On()
		})},
		{"airports", changes(radar.ParseToggle, func(s *State, v radar.Toggle) {
			s.Radar.Airports, s.Radar.MinimalAirports = v, v.On()
		})},
		{"bar", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.MinimalBar = v.On() })},
		{"board", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.Board = v.On() })},
		{"lights", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.Lights = v.On() })},
		{"stars", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.Stars = v })},
		{"zoom", changes(radar.ParseZoom, func(s *State, v int) { s.Radar.Zoom = v })},
		{"tilt", changes(radar.ParseTilt, func(s *State, v float64) { s.Radar.Tilt = v })},
		{"orbit", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.Orbit = v })},
		{"azimuth", changes(radar.ParseAzimuth, func(s *State, v float64) { s.Radar.Azimuth = v })},
		{"exaggerate", changes(radar.ParseExaggerate, func(s *State, v float64) { s.Radar.Exaggerate = v })},
		{"range", changes(radar.ParseRange, func(s *State, v float64) { s.Radar.RangeNm = v })},
		{"theme", changes(theme.Parse, func(s *State, v theme.Kind) { s.Theme = v })},
		{"look", changes(theme.ParseLook, func(s *State, v theme.Look) { s.Look = v })},
		{"colour", changes(radar.ParseColour, func(s *State, v radar.ColourMode) { s.Radar.Colour = v })},
		{"recenter", changes(radar.ParseRecentre, func(s *State, v time.Duration) { s.Radar.Recentre = v })},
		{"corridors", changes(radar.ParseToggle, func(s *State, v radar.Toggle) { s.Radar.Corridors = v })},
		{"filter", changes(radar.ParseFilter, func(s *State, v radar.BandFilter) { s.Radar.Filter = v })},
	}
}

// changes builds a setter's parser out of the flag's own parser and the one
// field it writes.
func changes[T any](parse func(string) (T, error), assign func(*State, T)) func(string) (Change, error) {
	return func(text string) (Change, error) {
		value, err := parse(text)
		if err != nil {
			return nil, err
		}

		return func(state *State) { assign(state, value) }, nil
	}
}

// namedKey is the allow list of keys key takes by name. Esc is not among
// them: on the radar it unpins the selection, but on every other scene it
// quits, and a scope with nobody at its keyboard must not be one typo away
// from ending.
func namedKey(name string) (input.Kind, bool) {
	switch name {
	case "up":
		return input.Up, true
	case "down":
		return input.Down, true
	case "left":
		return input.Left, true
	case "right":
		return input.Right, true
	case "enter":
		return input.Enter, true
	default:
		return input.Rune, false
	}
}

// Parse reads one command line, without its line ending.
//
// Everything is checked here, on the connection's goroutine, so what reaches
// the run loop is a command that cannot fail. Words are separated by runs of
// spaces or tabs and matched exactly: "GET" is refused the way "--view 3D"
// is.
func Parse(line string) (Command, error) {
	if !utf8.ValidString(line) {
		return Command{}, ErrNotUTF8
	}

	if len(line) > MaxLine {
		return Command{}, ErrLineTooLong
	}

	words := strings.Fields(line)
	if len(words) == 0 {
		return Command{}, ErrUnknownCommand
	}

	switch words[0] {
	case "get":
		return bare(VerbGet, words)
	case "help":
		return bare(VerbHelp, words)
	case "set":
		return parseSet(words[1:])
	case "key":
		return parseKey(words[1:])
	default:
		return Command{}, ErrUnknownCommand
	}
}

// bare is a command that takes no arguments.
func bare(verb Verb, words []string) (Command, error) {
	if len(words) != 1 {
		return Command{}, fmt.Errorf("%w: %s takes no arguments", ErrUsage, words[0])
	}

	return Command{Verb: verb}, nil
}

// parseSet reads the name and value after set.
func parseSet(args []string) (Command, error) {
	if len(args) != 2 {
		return Command{}, fmt.Errorf("%w: set <name> <value>", ErrUsage)
	}

	change, err := ParseSetting(args[0], args[1])
	if err != nil {
		return Command{}, err
	}

	return Command{Verb: VerbSet, Change: change}, nil
}

// ParseSetting is set without the verb: one name from the allow list and its
// value, checked by the same parser the flag uses. It is what a saved state
// file is read back through, so a line in that file is refused for exactly
// the reasons the same set would be.
//
// The error wraps ErrUnknownSetting for a name set does not take and ErrValue
// for a value its parser refuses.
func ParseSetting(name, text string) (Change, error) {
	for _, known := range setters() {
		if known.name != name {
			continue
		}

		change, err := known.parse(text)
		if err != nil {
			return nil, fmt.Errorf("%w for %s: %w", ErrValue, name, err)
		}

		return change, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrUnknownSetting, name)
}

// parseKey reads the key after key: one printable character or one of the
// named keys, less the ones that do more than change the picture.
func parseKey(args []string) (Command, error) {
	if len(args) != 1 {
		return Command{}, fmt.Errorf("%w: key <k>", ErrUsage)
	}

	name := args[0]

	if name == "esc" || name == "q" || name == "Q" {
		return Command{}, ErrQuit
	}

	// b switches DC power onto the antenna coax. The port is for changing the
	// picture, and an LNA powered or cut by a stray script is a fault in the
	// receive chain that nobody at the screen can see the cause of.
	if name == "b" || name == "B" {
		return Command{}, ErrBiasTee
	}

	if kind, ok := namedKey(name); ok {
		return Command{Verb: VerbKey, Key: input.Key{Kind: kind}}, nil
	}

	value, size := utf8.DecodeRuneInString(name)
	if size != len(name) || !unicode.IsPrint(value) {
		return Command{}, fmt.Errorf("%w: %q", ErrKey, name)
	}

	return Command{Verb: VerbKey, Key: input.Key{Kind: input.Rune, Rune: value}}, nil
}

// Help is the text after "ok" in the reply to help.
func Help() string {
	known := setters()

	names := make([]string, 0, len(known))
	for _, setting := range known {
		names = append(names, setting.name)
	}

	return "commands: get | set <name> <value> | key <k> | help; set names: " +
		strings.Join(names, " ") +
		"; keys: one printable character, or up down left right enter"
}

// Field is one name and value of a State, spelled the way set takes them.
type Field struct {
	Name  string
	Value string
}

// Fields is the state as name and value pairs, always in the same order. Every
// name is one set accepts and every value is one set accepts for it, which is
// what lets the list be written out and read back through ParseSetting.
//
// shore and airports report the pair the view on screen draws, which is the
// pair m and a would flip there. bar is the bare views' bar whatever is on
// screen, because there is only one of it and the full views ignore it. bar,
// lights, stars, recenter, corridors, filter and board come last, in the order
// they were added, so the fields before them keep the positions scripts cut
// them by.
//
// filter is the altitude or corridor filter, and reads all while the colour
// is airline: the operator f picks there has no spelling set could take back.
func (s State) Fields() []Field {
	set := s.Radar

	shore, airports := set.Shore.On(), set.Airports.On()
	if set.View.Bare() {
		shore, airports = set.MinimalShore, set.MinimalAirports
	}

	rangeText := radar.RangeAuto
	if set.RangeNm > 0 {
		rangeText = number(set.RangeNm)
	}

	return []Field{
		{"view", string(orDefault(set.View, radar.ViewScope))},
		{"shore", string(radar.ToggleOf(shore))},
		{"airports", string(radar.ToggleOf(airports))},
		{"zoom", strconv.Itoa(set.Zoom)},
		{"tilt", number(set.Tilt)},
		{"orbit", string(radar.ToggleOf(set.Orbit.On()))},
		{"azimuth", number(compass(set.Azimuth))},
		{"exaggerate", number(set.Exaggerate)},
		{"range", rangeText},
		{"theme", string(orDefault(s.Theme, theme.KindNight))},
		{"look", string(orDefault(s.Look, theme.LookGlass))},
		{"colour", string(orDefault(set.Colour, radar.ColourAltitude))},
		{"bar", string(radar.ToggleOf(set.MinimalBar))},
		{"lights", string(radar.ToggleOf(set.Lights))},
		{"stars", string(radar.ToggleOf(set.Stars.On()))},
		{"recenter", interval(set.Recentre)},
		{"corridors", string(radar.ToggleOf(set.Corridors.On()))},
		{"filter", string(orDefault(set.Filter, radar.FilterAll))},
		{"board", string(radar.ToggleOf(set.Board))},
	}
}

// String renders the state as the reply line's fields, name=value separated
// by spaces in the order Fields gives them, so a script can cut a field out
// of it by position or by name.
func (s State) String() string {
	var line strings.Builder

	for index, field := range s.Fields() {
		if index > 0 {
			line.WriteByte(' ')
		}

		line.WriteString(field.Name)
		line.WriteByte('=')
		line.WriteString(field.Value)
	}

	return line.String()
}

// orDefault spells a zero allow-list value as the value it reads as.
//
//nolint:ireturn // T is one of the concrete string types, never an interface.
func orDefault[T ~string](value, fallback T) T {
	if value == "" {
		return fallback
	}

	return value
}

// number prints a value to one decimal place with no trailing zero, which is
// as fine as anybody reads a camera angle off a reply line.
func number(value float64) string {
	return strconv.FormatFloat(math.Round(value*tenths)/tenths, 'f', -1, 64)
}

// interval spells a recentring cadence the way --recenter is usually typed:
// 0, 3m, 1h. Duration.String would say 3m0s and 1h0m0s, which ParseRecentre
// takes just as well but nobody writes. A cadence that is not a whole number of
// minutes falls back to Duration.String, which reads back to the same value.
func interval(every time.Duration) string {
	switch {
	case every == 0:
		return "0"
	case every%time.Hour == 0:
		return strconv.FormatInt(int64(every/time.Hour), 10) + "h"
	case every%time.Minute == 0:
		return strconv.FormatInt(int64(every/time.Minute), 10) + "m"
	default:
		return every.String()
	}
}

// compass rounds an azimuth to the tenth number prints and wraps 359.96 onto
// zero, so the reply never says 360.
func compass(degrees float64) float64 {
	rounded := math.Round(degrees*tenths) / tenths
	if rounded >= radar.MaxAzimuth {
		return rounded - radar.MaxAzimuth
	}

	return rounded
}
