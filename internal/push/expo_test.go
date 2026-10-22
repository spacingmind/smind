package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeTickets(w http.ResponseWriter, n int) {
	tickets := make([]expoTicket, n)
	for i := range tickets {
		tickets[i].Status = "ok"
		tickets[i].ID = fmt.Sprintf("id-%d", i)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(expoResponse{Data: tickets}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// expoFake records every request and answers through reply, which gets the
// 1-based request number and the message count of the batch.
type expoFake struct {
	mu     sync.Mutex
	bodies [][]byte
	auths  []string
	lens   []int
	reply  func(w http.ResponseWriter, reqNo, msgs int)
}

func newExpoFake(t *testing.T, reply func(w http.ResponseWriter, reqNo, msgs int)) (*expoFake, *httptest.Server) {
	t.Helper()
	f := &expoFake{reply: reply}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var arr []json.RawMessage
		_ = json.Unmarshal(body, &arr)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.lens = append(f.lens, len(arr))
		reqNo := len(f.bodies)
		f.mu.Unlock()
		f.reply(w, reqNo, len(arr))
	}))
	t.Cleanup(ts.Close)
	return f, ts
}

func (f *expoFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *expoFake) body(i int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[i]
}

func (f *expoFake) lengths() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.lens)
}

func (f *expoFake) auth() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.auths)
}

func instantSleep(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
}

func TestExpoSendsAndMapsTickets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"status":"ok","id":"t1"},`+
			`{"status":"error","message":"device gone","details":{"error":"DeviceNotRegistered"}},`+
			`{"status":"ok","id":"t3"}]}`)
	}))
	defer ts.Close()
	msgs := []Message{
		{Token: "tok-a", Platform: PlatformAndroid, Data: Blob{V: 1, N: "n", C: "c"}},
		{Token: "tok-b", Platform: PlatformAndroid, Data: Blob{V: 1, N: "n", C: "c"}},
		{Token: "tok-c", Platform: PlatformIOS, Data: Blob{V: 1, N: "n", C: "c"}},
	}
	got, err := NewExpo(ExpoOptions{BaseURL: ts.URL}).Send(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Token: "tok-a", OK: true},
		{Token: "tok-b", ErrCode: "DeviceNotRegistered"},
		{Token: "tok-c", OK: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("results = %+v, want %+v", got, want)
	}
}

func TestExpoBatching100(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, _, msgs int) { writeTickets(w, msgs) })
	msgs := make([]Message, 250)
	for i := range msgs {
		msgs[i] = Message{Token: fmt.Sprintf("tok-%d", i), Platform: PlatformAndroid, Data: Blob{V: 1, N: "n", C: "c"}}
	}
	got, err := NewExpo(ExpoOptions{BaseURL: ts.URL}).Send(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if lengths := f.lengths(); !slices.Equal(lengths, []int{100, 100, 50}) {
		t.Fatalf("batch sizes = %v, want [100 100 50]", lengths)
	}
	if len(got) != 250 {
		t.Fatalf("got %d results, want 250", len(got))
	}
	for i, res := range got {
		if !res.OK || res.Token != msgs[i].Token {
			t.Fatalf("result %d = %+v, want ok for %s", i, res, msgs[i].Token)
		}
	}
}

func TestExpoRequestHasNoPlaintext(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, _, msgs int) { writeTickets(w, msgs) })
	nonce := make([]byte, 12)
	for i := range nonce {
		nonce[i] = byte(0xa0 + i)
	}
	ev := Event{
		ID:     "evt-1",
		Kind:   "permission",
		TaskID: 424242,
		RunID:  "SECRET-RUN-XYZ",
		Title:  "SECRET-TITLE-XYZ",
		Body:   "SECRET-BODY-XYZ",
		At:     1790000000,
	}
	blob, err := sealWithNonce(testKey(), "dev-1", ev, nonce)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []Message{
		{Token: "ExponentPushToken[android]", Platform: PlatformAndroid, Data: blob},
		{Token: "ExponentPushToken[ios]", Platform: PlatformIOS, Data: blob},
	}
	if _, err := NewExpo(ExpoOptions{BaseURL: ts.URL}).Send(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}

	body := string(f.body(0))
	for _, leak := range []string{"SECRET-TITLE-XYZ", "SECRET-BODY-XYZ", "424242", "SECRET-RUN-XYZ", "permission", "kind", "taskId"} {
		if strings.Contains(body, leak) {
			t.Fatalf("request body leaks %q", leak)
		}
	}
	var arr []map[string]any
	if err := json.Unmarshal(f.body(0), &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 2 {
		t.Fatalf("request carries %d messages, want 2", len(arr))
	}
	allowed := []string{"to", "data", "priority", "ttl", "channelId", "title", "body", "mutableContent"}
	for i, m := range arr {
		for k := range m {
			if !slices.Contains(allowed, k) {
				t.Fatalf("message %d has unexpected key %q", i, k)
			}
		}
	}
}

func TestExpoRetriesThenSucceeds(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, reqNo, msgs int) {
		if reqNo <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeTickets(w, msgs)
	})
	var delays []time.Duration
	e := NewExpo(ExpoOptions{BaseURL: ts.URL, Sleep: instantSleep(&delays)})
	if _, err := e.Send(context.Background(), []Message{{Token: "tok", Platform: PlatformAndroid, Data: Blob{}}}); err != nil {
		t.Fatal(err)
	}
	if got := f.count(); got != 3 {
		t.Fatalf("%d requests, want 3", got)
	}
	if !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("backoff delays = %v, want [1s 2s]", delays)
	}
}

func TestExpoRetryBackoffThenDrop(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, _, _ int) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	var delays []time.Duration
	e := NewExpo(ExpoOptions{BaseURL: ts.URL, Sleep: instantSleep(&delays)})
	_, err := e.Send(context.Background(), []Message{{Token: "tok", Platform: PlatformAndroid, Data: Blob{}}})
	if err == nil {
		t.Fatal("Send succeeded, want an error after exhausting retries")
	}
	if got := f.count(); got != 4 {
		t.Fatalf("%d requests, want 1+3", got)
	}
	if !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Fatalf("backoff delays = %v, want [1s 2s 4s]", delays)
	}
}

func TestExpo429IsRetried(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, reqNo, msgs int) {
		if reqNo == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeTickets(w, msgs)
	})
	var delays []time.Duration
	e := NewExpo(ExpoOptions{BaseURL: ts.URL, Sleep: instantSleep(&delays)})
	if _, err := e.Send(context.Background(), []Message{{Token: "tok", Platform: PlatformAndroid, Data: Blob{}}}); err != nil {
		t.Fatal(err)
	}
	if got := f.count(); got != 2 {
		t.Fatalf("%d requests, want 2", got)
	}
	if !slices.Equal(delays, []time.Duration{time.Second}) {
		t.Fatalf("backoff delays = %v, want [1s]", delays)
	}
}

func TestExpo400NotRetried(t *testing.T) {
	f, ts := newExpoFake(t, func(w http.ResponseWriter, _, _ int) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errors":[{"code":"VALIDATION_ERROR","message":"LEAKME: the payload was bad"}]}`)
	})
	e := NewExpo(ExpoOptions{BaseURL: ts.URL, Sleep: instantSleep(&[]time.Duration{})})
	_, err := e.Send(context.Background(), []Message{{Token: "tok", Platform: PlatformAndroid, Data: Blob{}}})
	if err == nil {
		t.Fatal("Send succeeded, want an error")
	}
	if strings.Contains(err.Error(), "LEAKME") {
		t.Fatalf("error leaks the response body: %v", err)
	}
	if got := f.count(); got != 1 {
		t.Fatalf("%d requests, want 1 (400 is not retried)", got)
	}
}

func TestExpoAccessTokenHeader(t *testing.T) {
	token := "ExponentPushToken[recorder]"
	var logs []string
	send := func(opts ExpoOptions) []string {
		f, ts := newExpoFake(t, func(w http.ResponseWriter, _, msgs int) { writeTickets(w, msgs) })
		opts.BaseURL = ts.URL
		if _, err := NewExpo(opts).Send(context.Background(), []Message{{Token: token, Platform: PlatformAndroid, Data: Blob{}}}); err != nil {
			t.Fatal(err)
		}
		return f.auth()
	}

	if got := send(ExpoOptions{
		AccessToken: "tok-123",
		Logf:        func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	}); got[0] != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want %q", got[0], "Bearer tok-123")
	}
	if got := send(ExpoOptions{}); got[0] != "" {
		t.Fatalf("Authorization = %q, want none", got[0])
	}
	if len(logs) == 0 {
		t.Fatal("Logf was never called")
	}
	for _, line := range logs {
		if strings.Contains(line, "tok-123") || strings.Contains(line, token) {
			t.Fatalf("log leaks a secret: %s", line)
		}
	}
}

func TestExpoBadTicketCount(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"status":"ok"}]}`)
	}))
	defer ts.Close()
	msgs := []Message{
		{Token: "tok-a", Platform: PlatformAndroid, Data: Blob{}},
		{Token: "tok-b", Platform: PlatformAndroid, Data: Blob{}},
	}
	_, err := NewExpo(ExpoOptions{BaseURL: ts.URL}).Send(context.Background(), msgs)
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("error = %v, want ErrBadResponse", err)
	}
}

func TestExpoContextCancelled(t *testing.T) {
	unblock := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	t.Cleanup(func() {
		close(unblock)
		ts.Close()
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := NewExpo(ExpoOptions{BaseURL: ts.URL}).Send(ctx, []Message{{Token: "tok", Platform: PlatformAndroid, Data: Blob{}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Send took %v to return after cancellation", elapsed)
	}
}

func TestExpoNeverDefaultsToNetworkInTests(t *testing.T) {
	if got := NewExpo(ExpoOptions{}).baseURL; got != "https://exp.host" {
		t.Fatalf("baseURL = %q, want https://exp.host", got)
	}
}
