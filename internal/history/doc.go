// Package history keeps the corridor tracks of aircraft that have gone quiet
// across a restart of the program.
//
// internal/source holds those tracks in a ring and converts it to and from
// the Snapshot here; nothing in this package knows about the ring, the
// corridors or the radar. The file is a cache and the feed is the truth, so
// every way a file can be wrong ends in an error the caller reports and
// ignores, never in a run that will not start.
//
// The one thing to know before reading on is that the file is read as
// untrusted bytes. It is encoding/gob behind a magic and a version byte, read
// through a size cap, and every gob message length is checked against the
// bytes actually present before gob is allowed near them: gob sizes its
// buffer from a message's length prefix before it reads the message, so a
// ten-byte file claiming a gigabyte would otherwise allocate one.
package history
