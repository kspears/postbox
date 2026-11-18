// Package clock is the one source of the current time for everything that expires: sessions
// and sign-in throttling. Test builds can move it forward; release builds never do.
package clock

import (
	"sync/atomic"
	"time"
)

// Clock is the wall clock plus an offset that only test builds change.
type Clock struct {
	offset atomic.Int64
}

// Now returns the current time as Postbox sees it.
func (c *Clock) Now() time.Time {
	return time.Now().Add(time.Duration(c.offset.Load()))
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.offset.Add(int64(d))
}
