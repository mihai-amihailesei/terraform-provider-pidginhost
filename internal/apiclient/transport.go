// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// retryTransport retries requests that are safe to repeat:
//
//   - any request answered 429 Too Many Requests (it was not processed);
//   - idempotent requests (GET, HEAD, PUT, DELETE) answered 502, 503 or 504,
//     or that failed with a transport error.
//
// POST requests are never retried after a 5xx or transport error, because the
// API may already have acted on them (for example allocated an IP).
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
}

func newRetryTransport(base http.RoundTripper, maxRetries int) *retryTransport {
	return &retryTransport{base: base, maxRetries: maxRetries}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	idempotent := req.Method == http.MethodGet || req.Method == http.MethodHead ||
		req.Method == http.MethodPut || req.Method == http.MethodDelete

	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}

		resp, err := t.base.RoundTrip(req)

		retry := false
		switch {
		case err != nil:
			retry = idempotent
		case resp.StatusCode == http.StatusTooManyRequests:
			retry = true
		case resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout:
			retry = idempotent
		}
		if !retry || attempt >= t.maxRetries || req.Context().Err() != nil {
			return resp, err
		}
		if req.Body != nil && req.GetBody == nil {
			// The body was consumed and cannot be replayed.
			return resp, err
		}

		wait := backoff(attempt, resp)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		tflog.Debug(req.Context(), "retrying Pidgin Host API request", map[string]any{
			"method": req.Method, "url": req.URL.String(), "attempt": attempt + 1, "wait": wait.String(),
		})

		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

// backoff honours Retry-After when present and otherwise uses exponential
// backoff with jitter, capped at 30 seconds.
func backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if s := resp.Header.Get("Retry-After"); s != "" {
			if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
				return min(time.Duration(secs)*time.Second, 60*time.Second)
			}
		}
	}
	d := time.Duration(1<<attempt) * time.Second
	d += time.Duration(rand.Int64N(int64(500 * time.Millisecond)))
	return min(d, 30*time.Second)
}

// loggingTransport writes one debug log line per request. It never logs
// headers or bodies, so tokens and credentials cannot leak into TF_LOG output.
type loggingTransport struct {
	base http.RoundTripper
}

func newLoggingTransport(base http.RoundTripper) *loggingTransport {
	return &loggingTransport{base: base}
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	fields := map[string]any{
		"method":   req.Method,
		"url":      req.URL.String(),
		"duration": time.Since(start).String(),
	}
	if err != nil {
		fields["error"] = err.Error()
	} else {
		fields["status"] = resp.StatusCode
	}
	tflog.Debug(req.Context(), "Pidgin Host API request", fields)
	return resp, err
}
