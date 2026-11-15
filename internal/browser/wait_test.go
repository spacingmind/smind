package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// waitHook answers the isolated-world setup and returns the nth evaluate's
// includes() answer. onCancel runs before the first evaluate answer.
func waitHook(eval func(n int) bool, onCancel func()) func(f *fakeCDP, c fakeCall) (any, string) {
	return func(f *fakeCDP, c fakeCall) (any, string) {
		switch c.Method {
		case "Page.getFrameTree":
			return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1"}}}, ""
		case "Page.createIsolatedWorld":
			return map[string]any{"executionContextId": 77}, ""
		case "Runtime.evaluate":
			n := len(f.callsFor("Runtime.evaluate"))
			if n == 1 && onCancel != nil {
				onCancel()
			}
			return map[string]any{"result": map[string]any{"type": "boolean", "value": eval(n)}}, ""
		}
		return defaultHook(f, c)
	}
}

func lowerPollInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := waitPollInterval
	waitPollInterval = d
	t.Cleanup(func() { waitPollInterval = old })
}

func TestWaitFoundOnThirdPoll(t *testing.T) {
	s, f := startHookedSession(t, waitHook(func(n int) bool { return n >= 3 }, nil))
	lowerPollInterval(t, 5*time.Millisecond)

	found, url, err := s.WaitFor(context.Background(), "", "ready", "", 2*time.Second)
	if err != nil || !found || url != "http://localhost:3000/" {
		t.Fatalf("found=%v url=%q err=%v", found, url, err)
	}
	if n := len(f.callsFor("Runtime.evaluate")); n != 3 {
		t.Fatalf("the third poll must find it, %d evaluates", n)
	}
	if n := len(f.callsFor("Page.createIsolatedWorld")); n != 1 {
		t.Fatalf("the isolated context is created once, not per poll: %d", n)
	}
}

func TestWaitTimesOutSoftly(t *testing.T) {
	s, f := startHookedSession(t, waitHook(func(int) bool { return false }, nil))
	lowerPollInterval(t, 10*time.Millisecond)

	found, url, err := s.WaitFor(context.Background(), "", "gone", "", 80*time.Millisecond)
	if err != nil || found {
		t.Fatalf("a timeout is soft: found=%v err=%v", found, err)
	}
	if url != "http://localhost:3000/" {
		t.Fatalf("the current url is still reported: %q", url)
	}
	if n := len(f.callsFor("Runtime.evaluate")); n < 2 {
		t.Fatalf("it must have polled more than once: %d", n)
	}
}

func TestWaitTimeoutIsCappedAt15s(t *testing.T) {
	for d, want := range map[time.Duration]time.Duration{
		0:               MaxWait,
		-time.Second:    MaxWait,
		time.Hour:       MaxWait,
		MaxWait + 1:     MaxWait,
		MaxWait:         MaxWait,
		5 * time.Second: 5 * time.Second,
		time.Minute:     MaxWait,
	} {
		if got := clampWait(d); got != want {
			t.Errorf("clampWait(%v) = %v, want %v", d, got, want)
		}
	}
	// A 1h wait is not rejected: it polls under the 15 s cap until the
	// caller's context is cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, f := startHookedSession(t, waitHook(func(int) bool { return false }, cancel))
	lowerPollInterval(t, 5*time.Millisecond)

	_, _, err := s.WaitFor(ctx, "", "x", "", time.Hour)
	if err == nil {
		t.Fatal("the cancelled wait must surface the context error")
	}
	if n := len(f.callsFor("Runtime.evaluate")); n < 1 {
		t.Fatal("a 1h timeout must still start polling")
	}
}

func TestWaitTextIsJSONEncoded(t *testing.T) {
	text := "he said \"hi\"\nback\\slash </script>"
	s, f := startHookedSession(t, waitHook(func(int) bool { return true }, nil))

	found, _, err := s.WaitFor(context.Background(), "", text, "", time.Second)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	calls := f.callsFor("Runtime.evaluate")
	if len(calls) != 1 {
		t.Fatalf("evaluate calls: %d", len(calls))
	}
	enc, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	want := `(document.body ? document.body.innerText : "").includes(` + string(enc) + `)`
	if got := calls[0].Params["expression"]; got != want {
		t.Fatalf("expression:\n got %q\nwant %q", got, want)
	}
	for _, esc := range []string{`\"`, `\\`, `\n`} {
		if !strings.Contains(want, esc) {
			t.Fatalf("the needle must be JSON-escaped (%s missing): %s", esc, want)
		}
	}
	if calls[0].Params["contextId"] != float64(77) || calls[0].Params["returnByValue"] != true {
		t.Fatalf("evaluate params: %+v", calls[0].Params)
	}
	cw := f.callsFor("Page.createIsolatedWorld")
	if len(cw) != 1 || cw[0].Params["frameId"] != "F1" || cw[0].Params["worldName"] != "smind-read" || cw[0].Params["grantUniveralAccess"] != false {
		t.Fatalf("isolated world params: %+v", cw)
	}
}

func TestWaitURLModeDoesNotEvaluate(t *testing.T) {
	s, f := startHookedSession(t, waitHook(func(int) bool { return false }, nil))

	found, url, err := s.WaitFor(context.Background(), "", "", "localhost:3000", time.Second)
	if err != nil || !found || url != "http://localhost:3000/" {
		t.Fatalf("found=%v url=%q err=%v", found, url, err)
	}
	if n := len(f.callsFor("Runtime.evaluate")); n != 0 {
		t.Fatalf("url mode must not run page script, %d evaluates", n)
	}
	if n := len(f.callsFor("Page.createIsolatedWorld")); n != 0 {
		t.Fatalf("url mode needs no isolated world: %d", n)
	}
}

func TestWaitRejectsBadArguments(t *testing.T) {
	s, f := startHookedSession(t, waitHook(func(int) bool { return true }, nil))
	cases := []struct{ text, url string }{
		{"both", "set"},
		{"", ""},
		{strings.Repeat("a", MaxWaitText+1), ""},
	}
	for _, c := range cases {
		found, _, err := s.WaitFor(context.Background(), "", c.text, c.url, time.Second)
		if err == nil || found {
			t.Errorf("WaitFor(text=%q, url=%q) must be rejected: %v", c.text, c.url, err)
		}
	}
	if n := len(f.callsFor("Runtime.evaluate")) + len(f.callsFor("Page.createIsolatedWorld")); n != 0 {
		t.Fatalf("a rejected wait must touch nothing: %d calls", n)
	}
}

// navWaitHook answers the isolated-world dance with a new context id per
// world (worlds []int64), optional recreate failures (worldErrs, by call
// number), and per-evaluate answers (evals, by call number).
func navWaitHook(worlds map[int]int64, worldErrs map[int]string, evals map[int]any) func(f *fakeCDP, c fakeCall) (any, string) {
	return func(f *fakeCDP, c fakeCall) (any, string) {
		switch c.Method {
		case "Page.getFrameTree":
			return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1"}}}, ""
		case "Page.createIsolatedWorld":
			n := len(f.callsFor("Page.createIsolatedWorld"))
			if msg, ok := worldErrs[n]; ok {
				return nil, msg
			}
			return map[string]any{"executionContextId": worlds[n]}, ""
		case "Runtime.evaluate":
			n := len(f.callsFor("Runtime.evaluate"))
			switch v := evals[n].(type) {
			case string:
				return nil, v
			case bool:
				return map[string]any{"result": map[string]any{"value": v}}, ""
			}
			return nil, "unexpected evaluate"
		}
		return defaultHook(f, c)
	}
}

func TestWaitSurvivesNavigationDuringWait(t *testing.T) {
	s, f := startHookedSession(t, navWaitHook(
		map[int]int64{1: 5, 2: 6},
		nil,
		map[int]any{1: "Cannot find context with specified id", 2: true},
	))
	lowerPollInterval(t, 5*time.Millisecond)

	found, _, err := s.WaitFor(context.Background(), "", "after nav", "", 2*time.Second)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if n := len(f.callsFor("Page.createIsolatedWorld")); n != 2 {
		t.Fatalf("the world must be recreated once after the navigation, not %d times", n)
	}
	evals := f.callsFor("Runtime.evaluate")
	if len(evals) != 2 {
		t.Fatalf("evaluate calls: %d", len(evals))
	}
	if got := evals[1].Params["contextId"]; got != float64(6) {
		t.Fatalf("the evaluate after the recreate must run in the new world: contextId=%v", got)
	}
}

func TestWaitKeepsPollingWhenTheWorldCannotBeRecreatedYet(t *testing.T) {
	s, f := startHookedSession(t, navWaitHook(
		map[int]int64{1: 5, 3: 6},
		map[int]string{2: "No frame with given id found"},
		map[int]any{1: "Execution context was destroyed", 2: "Execution context was destroyed", 3: true},
	))
	lowerPollInterval(t, 5*time.Millisecond)

	found, _, err := s.WaitFor(context.Background(), "", "slow nav", "", 2*time.Second)
	if err != nil || !found {
		t.Fatalf("a mid-navigation recreate failure is not an error: found=%v err=%v", found, err)
	}
	if n := len(f.callsFor("Page.createIsolatedWorld")); n != 3 {
		t.Fatalf("it must keep trying to recreate the world, calls=%d", n)
	}
}

func TestWaitOtherEvaluateErrorsStillFail(t *testing.T) {
	s, _ := startHookedSession(t, navWaitHook(
		map[int]int64{1: 5},
		nil,
		map[int]any{1: "Internal error"},
	))
	lowerPollInterval(t, 5*time.Millisecond)

	found, _, err := s.WaitFor(context.Background(), "", "x", "", time.Second)
	if err == nil || found || !strings.Contains(err.Error(), "Internal error") {
		t.Fatalf("an unrelated evaluate error must surface: found=%v err=%v", found, err)
	}
}

func TestWaitURLMatchesTheFullURL(t *testing.T) {
	long := "http://localhost:3000/" + strings.Repeat("a", 700) + "/needle"
	s, f := startHookedSession(t, func(f *fakeCDP, c fakeCall) (any, string) {
		if c.Method == "Target.getTargetInfo" {
			return map[string]any{"targetInfo": map[string]any{"targetId": "T1", "url": long, "title": "Home"}}, ""
		}
		return defaultHook(f, c)
	})

	found, url, err := s.WaitFor(context.Background(), "", "", "/needle", time.Second)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	// The needle sits past the display copy's 500-rune cut: it can only have
	// matched the raw URL.
	if n := len([]rune(url)); n > 501 {
		t.Fatalf("the caller still gets the cleaned display copy, got %d runes", n)
	}
	if strings.Contains(url, "needle") {
		t.Fatalf("the raw url must never leave the package: %q", url)
	}
	if n := len(f.callsFor("Runtime.evaluate")); n != 0 {
		t.Fatalf("url mode must not run page script, %d evaluates", n)
	}
}
