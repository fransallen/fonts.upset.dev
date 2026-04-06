package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
)

const (
	backendCSS   = "https://fonts.googleapis.com"
	backendFonts = "https://fonts.gstatic.com/s"
	userAgent    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
)

var (
	version string

	// Shared HTTP clients with connection pooling. Two clients are used so that
	// slow font-file transfers (longer timeout) don't affect CSS fetches.
	cssClient  = &http.Client{Timeout: 10 * time.Second}
	fontClient = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	// Compute version once at startup.
	version = os.Getenv("GITHUB_TAG")
	if version == "" {
		version = "1.0"
	}
	if hostname, err := os.Hostname(); err == nil {
		version += "-" + hostname
	}
	if popID := os.Getenv("POP_ID"); popID != "" {
		version += "-" + popID
	}

	http.HandleFunc("/", handleRequest)
	log.Println("fonts.upset.dev is running on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		// Redirect to https://upset.dev/fonts if the path is /
		http.Redirect(w, r, "https://upset.dev/fonts", http.StatusMovedPermanently)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/f/") {
		// Pass the font requests through to the origin font server
		proxyRequest(w, r, backendFonts+r.URL.Path[2:])
		return
	}

	if strings.HasPrefix(r.URL.Path, "/css") {
		// Proxy stylesheet request
		url := backendCSS + r.URL.Path
		if r.URL.RawQuery != "" {
			url += "?" + r.URL.RawQuery
		}
		proxyStylesheet(w, r, url)
		return
	}

	// Respond with a 404 if the path doesn't match any of the conditions
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("The requested resource was not found. To learn how to use fonts.upset.dev, please visit https://upset.dev/fonts"))
}

// fetchCSS fetches the font CSS from Google using a fixed browser user-agent string
// to make sure the correct CSS is returned and rewrites the font URLs to proxy them
// through the worker (on the same origin to avoid a new connection).
// No user request headers (IP, cookies, etc.) are forwarded to Google.
func fetchCSS(url string) (string, error) {
	if strings.HasPrefix(url, "/") {
		url = "https:" + url
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := cssClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	fontCSS := string(body)

	// Minify CSS using tdewolff/minify with custom options
	m := minify.New()
	cssMinifier := &css.Minifier{
		KeepCSS2: false,
	}
	m.Add("text/css", cssMinifier)

	minified, err := m.String("text/css", fontCSS)
	if err != nil {
		// If minification fails, use original CSS
		minified = fontCSS
	}

	// Keep font-family names as they come from Google (with quotes and original case)
	// This preserves the exact font names without lowercasing or removing quotes
	minified = preserveFontFamilies(fontCSS, minified)

	// Rewrite all of the font URLs to come through the worker
	minified = regexp.MustCompile(`(?i)(https?:)?//fonts\.gstatic\.com/s/`).ReplaceAllString(minified, "/f/")

	return minified, nil
}

// proxyRequest generates a new request based on the original. Filters the request
// headers to prevent leaking user data (cookies, etc) and filters the response
// headers to prevent the origin setting policy on our origin.
func proxyRequest(w http.ResponseWriter, r *http.Request, url string) {
	req, err := http.NewRequest(r.Method, url, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Set custom request headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := fontClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch resource", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Only include a strict subset of response headers
	responseHeaders := []string{
		"Content-Type",
		"Cache-Control",
		"Expires",
		"Access-Control-Allow-Origin",
		"Accept-Ranges",
		"Date",
		"Last-Modified",
		"ETag",
	}

	for _, name := range responseHeaders {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}

	// Add security header
	w.Header().Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// proxyStylesheet handles a proxied stylesheet request
func proxyStylesheet(w http.ResponseWriter, _ *http.Request, url string) {
	css, err := fetchCSS(url)
	if err != nil {
		http.Error(w, "Failed to fetch stylesheet", http.StatusBadGateway)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Fonts-ID", version)
	w.Header().Set("Link", "<https://upset.dev/fonts>; rel=\"help\"")

	w.Write([]byte(css))
}

// preserveFontFamilies extracts font-family declarations from original CSS
// and replaces them in the minified version to keep original casing and quotes
func preserveFontFamilies(original, minified string) string {
	// Extract all font-family values from original CSS
	fontFamilyRegex := regexp.MustCompile(`font-family:\s*([^;}]+)`)
	originalMatches := fontFamilyRegex.FindAllStringSubmatch(original, -1)
	minifiedMatches := fontFamilyRegex.FindAllStringSubmatch(minified, -1)

	if len(originalMatches) != len(minifiedMatches) {
		// If counts don't match, return minified as-is to be safe
		return minified
	}

	// Replace each minified font-family with its original version
	result := minified
	for i := range originalMatches {
		if i < len(minifiedMatches) {
			originalValue := strings.TrimSpace(originalMatches[i][1])
			minifiedValue := strings.TrimSpace(minifiedMatches[i][1])
			// Replace the minified version with original
			result = strings.Replace(result, "font-family:"+minifiedValue, "font-family:"+originalValue, 1)
		}
	}

	return result
}
