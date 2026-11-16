package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// waitPollInterval is the delay between WaitFor polls (a var so tests can
// lower it).
var waitPollInterval = 250 * time.Millisecond

const (
	MaxWait     = 15 * time.Second
	MaxWaitText = 200
)

// clampWait bounds a caller-chosen wait timeout to (0, MaxWait].
func clampWait(d time.Duration) time.Duration {
	if d <= 0 || d > MaxWait {
		return MaxWait
	}
	return d
}

// isolatedContext creates (and returns) an isolated world on the tab's main
// frame. Page scripts cannot see or tamper with what runs there.
func (s *Session) isolatedContext(ctx context.Context, t *tabState) (int64, error) {
	var ft struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := s.tabCall(ctx, t, "Page.getFrameTree", nil, &ft); err != nil {
		return 0, fmt.Errorf("browser: isolated world: %w", err)
	}
	var w struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	// grantUniveralAccess is the protocol's own (mis)spelling.
	if err := s.tabCall(ctx, t, "Page.createIsolatedWorld", map[string]any{"frameId": ft.FrameTree.Frame.ID, "worldName": "smind-read", "grantUniveralAccess": false}, &w); err != nil {
		return 0, fmt.Errorf("browser: isolated world: %w", err)
	}
	return w.ExecutionContextID, nil
}

// isContextGone reports whether err is the browser saying the execution
// context a Runtime.evaluate targeted no longer exists: a page-driven
// navigation destroys the isolated world the wait was polling in.
func isContextGone(err error) bool {
	var ce *CDPError
	if !errors.As(err, &ce) {
		return false
	}
	m := strings.ToLower(ce.Message)
	return strings.Contains(m, "cannot find context") ||
		strings.Contains(m, "execution context was destroyed") ||
		strings.Contains(m, "context with specified id")
}

// rawTabURL returns the tab's URL exactly as the browser reports it. Waits
// match needles against it, not against tabInfo's display copy (cleaned and
// cut to 500 runes, which can hide the very text being waited for); callers
// are still only ever handed the display copy.
func (s *Session) rawTabURL(ctx context.Context, t *tabState) (string, error) {
	var r struct {
		TargetInfo struct {
			URL string `json:"url"`
		} `json:"targetInfo"`
	}
	if err := s.tabCall(ctx, t, "Target.getTargetInfo", map[string]any{"targetId": t.targetID}, &r); err != nil {
		return "", err
	}
	return r.TargetInfo.URL, nil
}

// evalIncludes runs the text search in the isolated world. The needle is
// JSON-encoded, never concatenated into the expression.
func (s *Session) evalIncludes(ctx context.Context, t *tabState, evalCtx int64, text string) (bool, error) {
	enc, err := json.Marshal(text)
	if err != nil {
		return false, err
	}
	expr := `(document.body ? document.body.innerText : "").includes(` + string(enc) + `)`
	var out struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	if err := s.tabCall(ctx, t, "Runtime.evaluate", map[string]any{"contextId": evalCtx, "expression": expr, "returnByValue": true}, &out); err != nil {
		return false, err
	}
	return out.Result.Value, nil
}

// WaitFor polls a tab until text appears in the page body or the tab's URL
// contains urlContains (exactly one of the two). A timeout is soft: it
// returns found=false, not an error.
func (s *Session) WaitFor(ctx context.Context, tabID, text, urlContains string, timeout time.Duration) (found bool, url string, err error) {
	if (text == "") == (urlContains == "") {
		return false, "", errors.New("browser: wait needs exactly one of text or urlContains")
	}
	if len(text) > MaxWaitText {
		return false, "", fmt.Errorf("browser: wait text is longer than %d characters", MaxWaitText)
	}
	timeout = clampWait(timeout)
	ctx, cancel, err := s.begin(ctx) // the call timeout also bounds the wait: MaxWait < CallTimeout
	if err != nil {
		return false, "", err
	}
	defer cancel()
	tab, err := s.tabFor(tabID)
	if err != nil {
		return false, "", err
	}
	urlNow := func() string {
		info, err := s.tabInfo(ctx, tab.targetID)
		if err != nil {
			return ""
		}
		return info.URL
	}
	var evalCtx int64
	if text != "" {
		evalCtx, err = s.isolatedContext(ctx, tab)
		if err != nil {
			return false, "", err
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		if text != "" {
			found, err = s.evalIncludes(ctx, tab, evalCtx, text)
			if err != nil {
				if !isContextGone(err) {
					return false, "", err
				}
				// The isolated world died with a navigation: recreate it and
				// keep polling. While the page is still mid-navigation even
				// the recreate can fail; that only means "not yet" — the
				// deadline or a cancelled context ends the wait, nothing else.
				if n, nerr := s.isolatedContext(ctx, tab); nerr == nil {
					evalCtx = n
				} else if ctx.Err() != nil {
					return false, "", ctx.Err()
				}
			} else if found {
				return true, urlNow(), nil
			}
		} else {
			raw, err := s.rawTabURL(ctx, tab)
			if err != nil {
				return false, "", err
			}
			if strings.Contains(raw, urlContains) {
				info, err := s.tabInfo(ctx, tab.targetID)
				if err != nil {
					return false, "", err
				}
				return true, info.URL, nil
			}
		}
		if !time.Now().Before(deadline) {
			return false, urlNow(), nil
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
}
