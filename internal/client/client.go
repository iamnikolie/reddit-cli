// Package client is a small JSON-over-HTTP client with retries and
// rate-limit awareness shared by every source.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// APIError is a non-2xx answer.
type APIError struct {
	Source  string
	Status  int
	Message string
	// RetryIn is the server's advice (seconds) when Status is 429.
	RetryIn int
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	s := fmt.Sprintf("%s error: HTTP %d: %s", e.Source, e.Status, msg)
	if e.Status == http.StatusTooManyRequests {
		if e.RetryIn > 0 {
			s += fmt.Sprintf("\nrate limited; retry in %ds", e.RetryIn)
		} else {
			s += "\nrate limited; retry shortly"
		}
	}
	return s
}

// Client talks to one base URL.
type Client struct {
	Source    string // label used in errors and --verbose output
	BaseURL   string
	UserAgent string
	Verbose   bool
	// RetryWait is the initial backoff for 5xx when the server gives no hint.
	RetryWait time.Duration
	// MaxWait bounds how long a single 429 is waited out; longer resets error.
	MaxWait time.Duration
	// MaxRetries bounds retries of 429 and 502/503/504.
	MaxRetries int
	http       *http.Client
}

// New returns a client for baseURL.
func New(source, baseURL, userAgent string) *Client {
	return &Client{
		Source:     source,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		UserAgent:  userAgent,
		RetryWait:  time.Second,
		MaxWait:    30 * time.Second,
		MaxRetries: 3,
		http:       &http.Client{},
	}
}

// Request describes one call.
type Request struct {
	Method  string
	Path    string
	Query   url.Values
	Body    []byte
	Headers map[string]string
}

func (c *Client) newHTTPRequest(ctx context.Context, r Request) (*http.Request, error) {
	u := c.BaseURL + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	var rd io.Reader
	if r.Body != nil {
		rd = bytes.NewReader(r.Body)
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func retryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryIn reads the server's reset advice in seconds from Retry-After or
// X-RateLimit-Reset (Arctic Shift and Reddit both send the latter).
func retryIn(h http.Header) int {
	for _, k := range []string{"Retry-After", "X-RateLimit-Reset", "X-Ratelimit-Reset"} {
		if v := h.Get(k); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				return int(f + 0.999)
			}
		}
	}
	return 0
}

// Do sends the request and returns the body and headers of a 2xx answer. A
// 429 is waited out when the reset is within MaxWait; 5xx answers retry with
// backoff. Anything else returns an *APIError.
func (c *Client) Do(ctx context.Context, r Request) ([]byte, http.Header, error) {
	wait := c.RetryWait
	if wait <= 0 {
		wait = time.Second
	}
	for attempt := 0; ; attempt++ {
		req, err := c.newHTTPRequest(ctx, r)
		if err != nil {
			return nil, nil, err
		}
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "→ %s %s\n", req.Method, req.URL.String())
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, nil, err
		}
		b, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "← HTTP %d (%d bytes)\n", resp.StatusCode, len(b))
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if rerr != nil {
				return nil, nil, fmt.Errorf("read %s response: %w", c.Source, rerr)
			}
			return b, resp.Header, nil
		}
		apiErr := &APIError{Source: c.Source, Status: resp.StatusCode, Message: parseMessage(b), RetryIn: retryIn(resp.Header)}
		if !retryable(resp.StatusCode) || attempt >= c.MaxRetries {
			return nil, nil, apiErr
		}
		d := wait
		if apiErr.RetryIn > 0 {
			d = time.Duration(apiErr.RetryIn) * time.Second
		}
		if resp.StatusCode == http.StatusTooManyRequests && d > c.MaxWait {
			return nil, nil, apiErr
		}
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "… retrying in %s\n", d)
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(d):
		}
		wait *= 2
	}
}

// parseMessage extracts an error message from {"error":"..."} or plain text.
func parseMessage(b []byte) string {
	var head struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(b, &head) == nil {
		var s string
		if json.Unmarshal(head.Error, &s) == nil && s != "" {
			return s
		}
		if head.Message != "" {
			return head.Message
		}
	}
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "<") {
		return "" // HTML error page; the status text is more useful
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// Get is Do for a GET returning the body.
func (c *Client) Get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	b, _, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: q})
	return b, err
}
