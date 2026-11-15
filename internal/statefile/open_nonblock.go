//go:build !js && !wasip1

package statefile

import "syscall"

// nonBlocking keeps the open of a FIFO from waiting for a writer.
const nonBlocking = syscall.O_NONBLOCK
