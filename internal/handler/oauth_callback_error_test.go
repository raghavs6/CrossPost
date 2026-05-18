package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func assertOAuthErrorRedirect(t *testing.T, w *httptest.ResponseRecorder, expectedError string) {
	t.Helper()

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d (body: %s)", resp.StatusCode, w.Body.String())
	}

	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/dashboard?oauth_error="+expectedError) {
		t.Fatalf("expected oauth error redirect for %q, got %q", expectedError, location)
	}
}
