package ibclientportal

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func mkResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestParseError429(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	err := parseError(mkResponse(http.StatusTooManyRequests, h, ""))
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 30*time.Second {
		t.Fatalf("RetryAfter = %s, want 30s", rle.RetryAfter)
	}
}

func TestParseError429NoRetryAfter(t *testing.T) {
	err := parseError(mkResponse(http.StatusTooManyRequests, nil, ""))
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %s, want 0", rle.RetryAfter)
	}
}

func TestParseErrorNon429FallsThrough(t *testing.T) {
	err := parseError(mkResponse(http.StatusInternalServerError, nil, `{"error":"boom"}`))
	var rle *RateLimitError
	if errors.As(err, &rle) {
		t.Fatalf("500 should not produce a RateLimitError, got %v", err)
	}
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"0", 0},
		{"-3", 0},
		{"  10 ", 10 * time.Second},
		{"garbage", 0},
	}
	for _, tc := range tests {
		if got := parseRetryAfter(tc.in); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
