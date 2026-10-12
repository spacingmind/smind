package quota

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// requestTimeout bounds one usage request, end to end.
	requestTimeout = 15 * time.Second
	// maxBodyBytes caps how much of a usage response is read.
	maxBodyBytes = 1 << 20
)

// FetchError is a transient fetch failure (network, timeout, 429, 5xx,
// unparseable body). Its message is fixed vocabulary: it never contains a
// token, a header or any part of a response body. The poller (PR-A2) backs off
// on it and uses RetryAfter for a 429.
type FetchError struct {
	// Status is the HTTP status, 0 when the request got no response.
	Status int
	// RetryAfter is the provider's Retry-After on a 429, 0 when absent.
	RetryAfter time.Duration
	msg        string
}

func (e *FetchError) Error() string { return e.msg }

func httpStatusError(status int, retryAfter time.Duration) *FetchError {
	msg := fmt.Sprintf("http %d", status)
	if status == http.StatusTooManyRequests {
		msg = errRateLimited
	}
	return &FetchError{Status: status, RetryAfter: retryAfter, msg: msg}
}

// malformedResult is the (Report, error) pair for a body that is not a usable
// usage response (HTML, not JSON, no windows).
func malformedResult(now time.Time) (Report, error) {
	return errorReport(errMalformed, now), &FetchError{msg: errMalformed}
}

// failureResult turns a *FetchError into the (Report, error) pair Fetch
// returns for transient failures: an error-status Report carrying the fixed
// message, and the error itself.
func failureResult(err *FetchError, now time.Time) (Report, error) {
	return errorReport(err.msg, now), err
}

// newUsageClient derives a client for usage calls from base: same transport
// (the uTLS one the proxy uses), a 15 s timeout, and redirects never followed
// (a 3xx is returned as the final response).
func newUsageClient(base *http.Client) *http.Client {
	return &http.Client{
		Transport: base.Transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// getBody performs one GET and returns the body of a 2xx response.
//
//   - 401/403: rejected is the status, body and err are zero (no retry: a
//     forced refresh is how a rotating-token login gets lost).
//   - 429: *FetchError carrying Retry-After.
//   - any other non-2xx (including an unfollowed 3xx), a timeout, a network
//     error, a body over 1 MiB: *FetchError in fixed vocabulary.
//   - a cancelled context: ctx.Err().
//
// Errors from net/http are never wrapped or returned (they embed the URL).
func getBody(ctx context.Context, client *http.Client, url string, header http.Header, now func() time.Time) (body []byte, rejected int, err error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if reqErr != nil {
		return nil, 0, &FetchError{msg: errNetwork}
	}
	req.Header = header.Clone()

	resp, doErr := client.Do(req)
	if doErr != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		var ne net.Error
		if errors.As(doErr, &ne) && ne.Timeout() {
			return nil, 0, &FetchError{msg: errTimeout}
		}
		return nil, 0, &FetchError{msg: errNetwork}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, resp.StatusCode, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, 0, httpStatusError(resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After"), now()))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, 0, httpStatusError(resp.StatusCode, 0)
	}

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if readErr != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		var ne net.Error
		if errors.As(readErr, &ne) && ne.Timeout() {
			return nil, 0, &FetchError{msg: errTimeout}
		}
		return nil, 0, &FetchError{msg: errNetwork}
	}
	if len(data) > maxBodyBytes {
		return nil, 0, &FetchError{msg: errTooLarge}
	}
	return data, 0, nil
}

// parseRetryAfter reads a Retry-After value: delta-seconds or an HTTP-date.
// Missing, unparseable or negative values give 0 (the poller applies its own
// floor).
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if math.IsNaN(secs) || secs < 0 {
			return 0
		}
		// An absurd value means "a very long time": hand back the cap, not
		// the shortest wait.
		if secs >= retryAfterCap.Seconds() {
			return retryAfterCap
		}
		return time.Duration(secs * float64(time.Second))
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := at.Sub(now); d > retryAfterCap {
			return retryAfterCap
		} else if d > 0 {
			return d
		}
	}
	return 0
}
