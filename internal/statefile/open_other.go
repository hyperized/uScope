//go:build js || wasip1

package statefile

// nonBlocking is zero where syscall has no O_NONBLOCK. Neither platform has
// FIFOs a uScope could be pointed at.
const nonBlocking = 0
