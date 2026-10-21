package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Default transport limits per SPECS.md §3.2.
const (
	SilenceTimeout      = 3 * time.Minute
	TotalDeadline       = 15 * time.Minute
	MaxTransientRetries = 3
)

// Transport is an HTTP client with kabby's timeout and retry policy.
type Transport struct {
	client *http.Client
}

// NewTransport creates a Transport with kabby's default timeouts.
func NewTransport() *Transport {
	return &Transport{
		client: &http.Client{Timeout: TotalDeadline},
	}
}

// Do executes an HTTP request with context-based cancellation.
// Transient failures (network errors, 5xx) are retried up to
// MaxTransientRetries times with cancellable exponential backoff.
func (t *Transport) Do(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxTransientRetries; attempt++ {
		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < MaxTransientRetries {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(backoffDuration(attempt)):
				}
				continue
			}
			return nil, err
		}
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			body := drainBody(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(body))
			if attempt < MaxTransientRetries {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(backoffDuration(attempt)):
				}
				continue
			}
			return nil, lastErr
		}
		return resp, nil
	}
	return nil, lastErr
}

func drainBody(body io.ReadCloser) []byte {
	data, _ := io.ReadAll(body)
	return data
}

func backoffDuration(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

// DecodeJSON reads an HTTP response body and decodes JSON into v.
func DecodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode json: %v (body: %.500s)", err, data)
	}
	return nil
}

// SSEReader reads a server-sent events stream line by line.
type SSEReader struct {
	scanner *bufio.Scanner
	ctx     context.Context
}

// NewSSEReader wraps an HTTP response body as an SSE reader bound to ctx.
func NewSSEReader(ctx context.Context, body io.ReadCloser) *SSEReader {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB max line
	return &SSEReader{scanner: scanner, ctx: ctx}
}

// NextData returns the next SSE "data: ..." payload.
// Returns ("", false) when the stream ends or ctx is cancelled.
func (r *SSEReader) NextData() (string, bool) {
	select {
	case <-r.ctx.Done():
		return "", false
	default:
	}

	for r.scanner.Scan() {
		line := r.scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: "), true
		}
	}
	return "", false
}

// ErrStreamCut reports a response stream that ended before the backend
// marked it complete. Any tool calls seen on such a stream are discarded.
var ErrStreamCut = errors.New("response stream ended before completion")
