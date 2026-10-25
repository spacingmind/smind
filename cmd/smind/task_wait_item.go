package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// queueItemPollInterval is how often waitForQueueItem asks the daemon whether
// a queued prompt has started.
const queueItemPollInterval = 500 * time.Millisecond

// waitForQueueItem blocks until queue item itemID is delivered and returns the
// run it became, so `task wait item:<id>` can then follow that run. When there
// is no run to follow it returns "" and the exit code to use: 3 when the item
// was cancelled, 1 when the lookup failed (unknown item, daemon error), 5 on
// --timeout, 0 on Ctrl+C.
//
// Notices go to errw.
//
// ctx carries the --timeout deadline; sigCtx only the signal, to tell the two
// apart when ctx ends.
func waitForQueueItem(ctx, sigCtx context.Context, errw io.Writer, client *wsclient.Client, itemID int64, timeout time.Duration) (runID string, exit int) {
	announced := false
	ticker := time.NewTicker(queueItemPollInterval)
	defer ticker.Stop()
	for {
		var res struct {
			Item queueItemRow `json:"item"`
		}
		err := client.Call(ctx, "chat.queueGet", map[string]any{"itemId": itemID}, &res)
		switch {
		case ctx.Err() != nil:
			// fall through to the ctx handling below
		case err != nil:
			fmt.Fprintf(errw, "task wait: %v\n", err)
			return "", 1
		case res.Item.Status == "cancelled":
			reason := res.Item.CancelReason
			if reason == "" {
				reason = "no reason given"
			}
			fmt.Fprintf(errw, "queue item %d was cancelled: %s\n", itemID, reason)
			return "", 3
		case res.Item.RunID != nil && *res.Item.RunID != "":
			return *res.Item.RunID, 0
		case !announced:
			announced = true
			fmt.Fprintf(errw, "queue item %d is %s; waiting for it to start (its chat is busy or the task's setup is running)\n", itemID, res.Item.Status)
		}

		if ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-ticker.C:
				continue
			}
		}
		if sigCtx.Err() != nil {
			fmt.Fprintf(errw, "detached -- queue item %d is still queued\n", itemID)
			return "", 0
		}
		fmt.Fprintf(errw, "timed out after %s; queue item %d is still queued\n", timeout, itemID)
		return "", 5
	}
}
