# fonts.upset.dev

A privacy-preserving proxy for Google Fonts. It fetches font stylesheets and font files on behalf of visitors so their IP addresses and browser fingerprints are never sent to Google.

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

## Deployment

**fonts.upset.dev** can be self-hosted using Docker or deployed to your preferred platform, like Fly, Render, or Railway.

## Environment variables

| Variable     | Description                                                       | Default  |
| ------------ | ----------------------------------------------------------------- | -------- |
| `GITHUB_TAG` | Release version tag, included in the `X-Fonts-ID` response header | `1.0`    |
| `POP_ID`     | Point-of-presence identifier appended to `X-Fonts-ID`             | _(none)_ |

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
