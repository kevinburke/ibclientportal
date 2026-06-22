package ibclientportal

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kevinburke/rest/restclient"
)

// RateLimitError is returned when the Client Portal gateway responds with HTTP
// 429 (Too Many Requests). The gateway throttles some endpoints (notably
// /pa/transactions, documented at one request per 15 minutes per account) and
// answers a throttled request with an empty body, so without this type the 429
// would surface as restclient's opaque "invalid response body:" error with no
// way to tell rate limiting apart from a real failure. Detect it with
// errors.As and back off.
//
// Note that exceeding a limit can put the caller in a penalty box for several
// minutes, so a short backoff often will not clear it; EnableRateLimits is the
// proactive way to stay under the limits in the first place.
type RateLimitError struct {
	// RetryAfter is the delay from the response's Retry-After header, or 0 when
	// absent. IB does not document or, as observed, send a Retry-After header
	// on its 429s, so in practice this is 0 and the caller's own backoff
	// governs; the field honors the header if a future gateway ever sends one.
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return "ibclientportal: rate limited (HTTP 429), retry after " + e.RetryAfter.String()
	}
	return "ibclientportal: rate limited (HTTP 429)"
}

// parseError is the client's ErrorParser (set in New). It surfaces HTTP 429 as
// a typed *RateLimitError and defers to restclient.DefaultErrorParser for every
// other status code.
func parseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		// Drain and close the body so the keep-alive connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return restclient.DefaultErrorParser(resp)
}

// parseRetryAfter interprets a Retry-After header value per RFC 9110: either a
// non-negative integer number of seconds or an HTTP date. It returns 0 when the
// value is absent or unparseable. IB is not known to send this header (see
// RateLimitError.RetryAfter); this is defensive forward-compat.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
