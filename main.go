package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	// Local disk cache.
	cacheEnabled bool
	cacheDir     string
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

	// Initialise local cache.
	cacheEnabled = os.Getenv("CACHE_ENABLED") == "true"
	cacheDir = os.Getenv("CACHE_DIR")
	if cacheDir == "" {
		cacheDir = ".fonts-cache"
	}
	if cacheEnabled {
		for _, sub := range []string{"css", "fonts"} {
			if err := os.MkdirAll(filepath.Join(cacheDir, sub), 0755); err != nil {
				log.Printf("cache: failed to create %s dir: %v — disabling cache", sub, err)
				cacheEnabled = false
				break
			}
		}
		if cacheEnabled {
			log.Printf("cache: enabled, directory=%s", cacheDir)
		}
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

// cssCachePath returns the cache file path for a CSS URL.
func cssCachePath(url string) string {
	h := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, "css", fmt.Sprintf("%x.css", h))
}

// fontCacheBase returns the base cache path for a font URL.
// The body is stored at this path; the Content-Type is stored at this path + ".ct".
func fontCacheBase(url string) string {
	h := sha256.Sum256([]byte(url))
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".font"
	}
	return filepath.Join(cacheDir, "fonts", fmt.Sprintf("%x%s", h, ext))
}

// fetchCSS fetches the font CSS from Google using a fixed browser user-agent string
// to make sure the correct CSS is returned and rewrites the font URLs to proxy them
// through the worker (on the same origin to avoid a new connection).
// No user request headers (IP, cookies, etc.) are forwarded to Google.
func fetchCSS(url string) (string, bool, error) {
	if strings.HasPrefix(url, "/") {
		url = "https:" + url
	}

	if cacheEnabled {
		if data, err := os.ReadFile(cssCachePath(url)); err == nil {
			return string(data), true, nil
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", false, err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := cssClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
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

	if cacheEnabled {
		if err := os.WriteFile(cssCachePath(url), []byte(minified), 0644); err != nil {
			log.Printf("cache: failed to write CSS: %v", err)
		}
	}

	return minified, false, nil
}

// proxyRequest generates a new request based on the original. Filters the request
// headers to prevent leaking user data (cookies, etc) and filters the response
// headers to prevent the origin setting policy on our origin.
func proxyRequest(w http.ResponseWriter, r *http.Request, url string) {
	if cacheEnabled {
		base := fontCacheBase(url)
		if body, err := os.ReadFile(base); err == nil {
			ct, _ := os.ReadFile(base + ".ct")
			if len(ct) > 0 {
				w.Header().Set("Content-Type", string(ct))
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Fonts-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(body)
			return
		}
	}

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
	if cacheEnabled {
		w.Header().Set("X-Fonts-Cache", "MISS")
	}

	w.WriteHeader(resp.StatusCode)

	if cacheEnabled && resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Failed to read resource", http.StatusBadGateway)
			return
		}
		base := fontCacheBase(url)
		if err := os.MkdirAll(filepath.Dir(base), 0755); err == nil {
			if err := os.WriteFile(base, body, 0644); err != nil {
				log.Printf("cache: failed to write font: %v", err)
			} else if ct := resp.Header.Get("Content-Type"); ct != "" {
				if err := os.WriteFile(base+".ct", []byte(ct), 0644); err != nil {
					log.Printf("cache: failed to write font content-type: %v", err)
				}
			}
		}
		w.Write(body)
		return
	}

	io.Copy(w, resp.Body)
}

// proxyStylesheet handles a proxied stylesheet request
func proxyStylesheet(w http.ResponseWriter, _ *http.Request, url string) {
	css, hit, err := fetchCSS(url)
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
	if cacheEnabled {
		if hit {
			w.Header().Set("X-Fonts-Cache", "HIT")
		} else {
			w.Header().Set("X-Fonts-Cache", "MISS")
		}
	}

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
