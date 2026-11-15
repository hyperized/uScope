// Package control is the live control port: a line protocol over TCP that
// changes a running uScope's settings, for a run that has nobody at its
// keyboard.
//
// One command per line, one reply per command. The reply is "ok" and the
// state after the command, or "err" and the reason:
//
//	get
//	set <name> <value>
//	key <k>
//	help
//
// The one design choice worth knowing before reading on is where commands
// run. The scene is not safe for concurrent use and does not need to be: it is
// owned by the run loop's goroutine. So the server only reads, parses and
// validates on its own goroutines, and what it hands over is a typed Request
// on a channel the run loop selects on beside its frame ticker and its key
// channel. The loop executes it between two frames and answers on the
// request's own reply channel. Nothing in this package touches a scene.
//
// There is no authentication. The port is for an operator on the same machine
// or on a private network, and it must be bound to loopback or to an address
// only that network can reach.
package control
