package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxBatch is the Expo Push API's per-request message limit.
const maxBatch = 100

// ErrBadResponse marks a provider reply whose shape does not match the
// request, e.g. a wrong number of tickets.
var ErrBadResponse = errors.New("push: provider response does not match the request")

// ExpoOptions configures NewExpo; the zero value is usable.
type ExpoOptions struct {
	BaseURL     string                                           // default "https://exp.host"
	AccessToken string                                           // optional; sent as "Authorization: Bearer <t>" when non-empty
	HTTPClient  *http.Client                                     // default &http.Client{Timeout: 15 * time.Second}
	Sleep       func(ctx context.Context, d time.Duration) error // default: wait d or ctx.Done()
	MaxRetries  int                                              // default 3 (retries AFTER the first attempt)
	Logf        func(format string, args ...any)                 // default no-op
}

// Expo sends pushes through the Expo Push API (ADR-0040 §2): batches of at
// most maxBatch messages, exponential backoff on network errors, 429 and 5xx,
// no retry on other 4xx. It never logs or returns tokens, the access token,
// request bodies or response bodies — only batch sizes, status codes and
// attempt numbers.
type Expo struct {
	baseURL     string
	accessToken string
	httpc       *http.Client
	sleep       func(ctx context.Context, d time.Duration) error
	maxRetries  int
	logf        func(format string, args ...any)
}

// NewExpo returns an Expo sender with o's zero fields filled in; it makes no
// network calls.
func NewExpo(o ExpoOptions) *Expo {
	e := &Expo{
		baseURL:     o.BaseURL,
		accessToken: o.AccessToken,
		httpc:       o.HTTPClient,
		sleep:       o.Sleep,
		maxRetries:  o.MaxRetries,
		logf:        o.Logf,
	}
	if e.baseURL == "" {
		e.baseURL = "https://exp.host"
	}
	if e.httpc == nil {
		e.httpc = &http.Client{Timeout: 15 * time.Second}
	}
	if e.sleep == nil {
		e.sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if e.maxRetries <= 0 {
		e.maxRetries = 3
	}
	if e.logf == nil {
		e.logf = func(string, ...any) {}
	}
	return e
}

// Send delivers msgs, splitting into batches of at most maxBatch sent
// sequentially; the returned results line up with msgs by index. A failure on
// any batch fails the whole call: a partial push would wake some devices with
// an event the others miss, so the caller retries everything or nothing.
func (e *Expo) Send(ctx context.Context, msgs []Message) ([]Result, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	var out []Result
	for start := 0; start < len(msgs); start += maxBatch {
		batch := msgs[start:min(start+maxBatch, len(msgs))]
		results, err := e.sendBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, results...)
	}
	return out, nil
}

func (e *Expo) sendBatch(ctx context.Context, batch []Message) ([]Result, error) {
	body := make([]expoMessage, len(batch))
	for i, m := range batch {
		body[i] = buildExpo(m)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	delay := time.Second
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results, retry, err := e.attempt(ctx, batch, payload, attempt)
		if err == nil {
			return results, nil
		}
		if !retry || attempt > e.maxRetries {
			return nil, err
		}
		if err := e.sleep(ctx, delay); err != nil {
			return nil, err
		}
		delay *= 2
	}
}

// attempt posts one batch once; retry says whether the failure is transient
// (network error, HTTP 429, HTTP 5xx) and worth another attempt.
func (e *Expo) attempt(ctx context.Context, batch []Message, payload []byte, attempt int) ([]Result, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/--/api/v2/push/send", bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.accessToken)
	}

	e.logf("push: expo: posting batch of %d messages (attempt %d)", len(batch), attempt)
	resp, err := e.httpc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, fmt.Errorf("push: expo: post: %w", err)
	}
	defer resp.Body.Close()
	e.logf("push: expo: http %d (attempt %d)", resp.StatusCode, attempt)

	switch {
	case resp.StatusCode == http.StatusOK:
		return e.decodeTickets(resp, batch)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("push: expo: http %d (attempt %d)", resp.StatusCode, attempt)
	default:
		return nil, false, fmt.Errorf("push: expo: http %d (attempt %d)", resp.StatusCode, attempt)
	}
}

type expoTicket struct {
	Status  string `json:"status"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type expoResponse struct {
	Data []expoTicket `json:"data"`
}

// decodeTickets maps one ticket per message, in order. The response body is
// never included in errors: it echoes what we sent.
func (e *Expo) decodeTickets(resp *http.Response, batch []Message) ([]Result, bool, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("push: expo: read response: %w", err)
	}
	var er expoResponse
	if err := json.Unmarshal(data, &er); err != nil {
		return nil, false, fmt.Errorf("push: expo: malformed response: %w", err)
	}
	if len(er.Data) != len(batch) {
		return nil, false, fmt.Errorf("%w: %d tickets for %d messages", ErrBadResponse, len(er.Data), len(batch))
	}
	results := make([]Result, len(batch))
	for i, tk := range er.Data {
		if tk.Status == "ok" {
			results[i] = Result{Token: batch[i].Token, OK: true}
		} else {
			results[i] = Result{Token: batch[i].Token, ErrCode: tk.Details.Error}
		}
	}
	return results, true, nil
}
