package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleRequestRedirect checks that the root path redirects to the help page.
func TestHandleRequestRedirect(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handleRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("expected 301, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "https://upset.dev/fonts" {
		t.Errorf("unexpected redirect location: %s", loc)
	}
}

// TestHandleRequestNotFound checks that unknown paths return 404.
func TestHandleRequestNotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/unknown", nil)
	w := httptest.NewRecorder()
	handleRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestHandleRequestCSSProxied checks that /css routes are handled and not 404'd.
// The upstream fetch will fail in tests, so we just verify the route is recognised
// (returns 502, not 404).
func TestHandleRequestCSSProxied(t *testing.T) {
	req := httptest.NewRequest("GET", "/css2?family=Inter", nil)
	w := httptest.NewRecorder()
	handleRequest(w, req)

	resp := w.Result()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("expected /css2 to be routed, got 404")
	}
}

// TestHandleRequestFontProxied checks that /f/ routes are handled (routed to upstream,
// not rejected with our own 404). The upstream may return any status; we just verify
// the response is not our own "resource not found" 404 page.
func TestHandleRequestFontProxied(t *testing.T) {
	req := httptest.NewRequest("GET", "/f/inter/v1/abc.woff2", nil)
	w := httptest.NewRecorder()
	handleRequest(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound && strings.Contains(string(body), "The requested resource was not found") {
		t.Error("expected /f/ to be routed to upstream, but got our own 404 handler")
	}
}

// TestFetchCSSRewritesFontURLs checks that font file URLs in the stylesheet are
// rewritten to go through the proxy (/f/) instead of pointing at fonts.gstatic.com.
func TestFetchCSSRewritesFontURLs(t *testing.T) {
	// Serve a fake Google Fonts CSS response.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `@font-face {
  font-family: 'Inter';
  src: url(https://fonts.gstatic.com/s/inter/v1/abc.woff2) format('woff2');
}`)
	}))
	defer upstream.Close()

	result, _, err := fetchCSS(upstream.URL)
	if err != nil {
		t.Fatalf("fetchCSS returned error: %v", err)
	}

	if strings.Contains(result, "fonts.gstatic.com") {
		t.Error("font URL was not rewritten: still contains fonts.gstatic.com")
	}
	if !strings.Contains(result, "/f/") {
		t.Error("font URL was not rewritten to /f/ path")
	}
}

// TestFetchCSSPreservesFontFamily checks that font-family names retain their
// original casing and quoting after minification.
func TestFetchCSSPreservesFontFamily(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `@font-face {
  font-family: 'Roboto Condensed';
  src: url(https://fonts.gstatic.com/s/roboto/v1/abc.woff2) format('woff2');
  font-weight: 400;
}`)
	}))
	defer upstream.Close()

	result, _, err := fetchCSS(upstream.URL)
	if err != nil {
		t.Fatalf("fetchCSS returned error: %v", err)
	}

	if !strings.Contains(result, "'Roboto Condensed'") {
		t.Errorf("font-family name not preserved; got: %s", result)
	}
}

// TestFetchCSSUpstreamError checks that a non-200 response from upstream returns an error.
func TestFetchCSSUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	_, _, err := fetchCSS(upstream.URL)
	if err == nil {
		t.Error("expected error for non-200 upstream response, got nil")
	}
}

// TestProxyStylesheetHeaders checks that the stylesheet response includes the
// expected headers.
func TestProxyStylesheetHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `@font-face { font-family: 'Inter'; src: url(https://fonts.gstatic.com/s/a.woff2); }`)
	}))
	defer upstream.Close()

	req := httptest.NewRequest("GET", "/css2?family=Inter", nil)
	w := httptest.NewRecorder()
	proxyStylesheet(w, req, upstream.URL)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("unexpected Content-Type: %s", ct)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected Access-Control-Allow-Origin: *")
	}
	if resp.Header.Get("Cache-Control") == "" {
		t.Error("expected Cache-Control header to be set")
	}
}

// TestPreserveFontFamilies checks the font-family preservation helper directly.
func TestPreserveFontFamilies(t *testing.T) {
	original := `@font-face { font-family: 'Noto Sans'; font-weight: 400; }`
	// Simulate minifier lowercasing the font name.
	minified := `@font-face{font-family:'noto sans';font-weight:400}`

	result := preserveFontFamilies(original, minified)

	if !strings.Contains(result, "'Noto Sans'") {
		t.Errorf("original font-family not restored; got: %s", result)
	}
}

// TestPreserveFontFamiliesMismatch checks that mismatched counts return minified as-is.
func TestPreserveFontFamiliesMismatch(t *testing.T) {
	original := `@font-face { font-family: 'A'; } @font-face { font-family: 'B'; }`
	minified := `@font-face{font-family:'a'}`

	result := preserveFontFamilies(original, minified)
	if result != minified {
		t.Errorf("expected minified to be returned unchanged on mismatch; got: %s", result)
	}
}
