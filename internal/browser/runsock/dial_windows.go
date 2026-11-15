//go:build windows

package runsock

import (
	"context"
	"time"
)

// Dial connects to a run's pipe (the child's side). The returned connection has
// the net.Conn subset the child uses (Read, Write, Close, deadlines).
func Dial(ctx context.Context, name string) (Conn, error) {
	wait := 2 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < wait {
			wait = d
		}
	}
	c, err := dialPipe(name, wait)
	if err != nil {
		return nil, err
	}
	return c, nil
}
