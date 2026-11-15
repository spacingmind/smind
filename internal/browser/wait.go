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
				return false, "", err
			}
			if found {
				return true, urlNow(), nil
			}
		} else {
			info, err := s.tabInfo(ctx, tab.targetID)
			if err != nil {
				return false, "", err
			}
			if strings.Contains(info.URL, urlContains) {
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
