# fonts.upset.dev

A privacy-preserving proxy for Google Fonts. It fetches font stylesheets and font files on behalf of visitors so their IP addresses and browser fingerprints are never sent to Google.

## Features

- Proxies Google Fonts stylesheets and font files without exposing visitor IP addresses or browser fingerprints to Google.
- Rewrites font URLs in stylesheets to route through the proxy automatically.
- Minifies CSS stylesheets before serving them.
- Built-in cache via local disk to avoid redundant upstream requests.

## How it works

When a browser requests a Google Fonts stylesheet through this proxy, the server:

1. Fetches the CSS from `fonts.googleapis.com` using a fixed user-agent string
2. Minifies the CSS and rewrites all font file URLs to point back through the proxy
3. Returns the rewritten stylesheet to the browser

Font file requests (`.woff2`, etc.) are proxied directly from `fonts.gstatic.com`. In both cases, only a strict set of safe headers is forwarded so no cookies, IP addresses, or other user-identifying information ever reaches Google.

## Usage

Replace your Google Fonts `<link>` tag with one pointing at this server. The path and query string stay the same.

**Before:**

```html
<link
  rel="stylesheet"
  href="https://fonts.googleapis.com/css2?family=Inter:wght@400;700&display=swap"
/>
```

**After:**

```html
<link
  rel="stylesheet"
  href="https://fonts.upset.dev/css2?family=Inter:wght@400;700&display=swap"
/>
```

That is all. Font files are automatically served through the proxy as well because the stylesheet URLs are rewritten server-side.

## Running locally

**Requirements:** Go 1.24 or later

```sh
git clone https://github.com/fransallen/fonts.upset.dev
cd fonts.upset.dev
go run .
```

## Run with Docker

```sh
docker run -d --name fonts.upset.dev -p 8080:8080 ghcr.io/fransallen/fonts.upset.dev
```

The server listens on `http://localhost:8080`.

## Self-Hosting

**fonts.upset.dev** can be self-hosted using Docker or deployed to your preferred platform, like Fly, Render, or Railway.

## Environment variables

| Variable        | Description                                       | Default        |
| --------------- | ------------------------------------------------- | -------------- |
| `CACHE_ENABLED` | Set to `true` to enable the local disk cache      | `false`        |
| `CACHE_DIR`     | Directory used to store cached CSS and font files | `.fonts-cache` |

When the cache is enabled, responses include an `X-Fonts-Cache` header set to `HIT` or `MISS`.

## Routes

| Path    | Description                                            |
| ------- | ------------------------------------------------------ |
| `/`     | Redirects to `https://upset.dev/fonts`                 |
| `/css*` | Proxies Google Fonts CSS stylesheets                   |
| `/f/*`  | Proxies individual font files from `fonts.gstatic.com` |

## Privacy

This proxy is designed so that Google never sees your visitors:

- All requests to Google are made server-side with a fixed user-agent string.
- No request headers from the browser (IP, cookies, `Referer`, etc.) are forwarded.
- Only a safe subset of response headers is passed back to the browser.

## Running tests

```sh
go test ./...
```

## License

MIT
