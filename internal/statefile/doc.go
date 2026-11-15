// Package statefile keeps the settings an operator changed over the control
// port across a restart: it reads them back on start and writes them out
// after every change.
//
// The file is the control port's own language with the verb left off, one
// "name value" per line, so cat shows it and a hand edit works. Reading it
// back goes through control.ParseSetting, the parser behind set, which means
// a line is accepted or refused for exactly the reasons the same set would be
// and the two cannot drift.
//
// The one thing worth knowing before reading on is where the writes happen.
// The run loop owns the scene and must never wait on a disk, so it only hands
// the state to a Saver, which keeps the latest one and writes it from a
// goroutine of its own once the changes have stopped arriving for a moment.
package statefile
