# Auth Browser Service

Node.js + Playwright service for browser-based authentication to SRM Academia portal.

## Purpose

When SRM portal authentication endpoints are unavailable, this service performs real browser login to obtain session cookies for the Go scraper.

## Usage

```bash
# Set environment variables
export SRM_EMAIL="student@srmist.edu.in"
export SRM_PASSWORD="password123"
export TIMEOUT_SECONDS="40"

# Run login
cd auth-browser
npm start
```

## Login Flow

1. **Navigate**: `https://academia.srmist.edu.in/`
2. **Fill Email**: `#login_id` input field
3. **Click Next**: `button#nextbtn` with text "Next"
4. **Wait**: For password field to appear
5. **Fill Password**: `#password` input field
6. **Click Sign In**: `button#nextbtn` with text "Sign In"
7. **Handle Rate Limit**: If redirected to block page, click "I Understand"
8. **Extract Cookies**: All browser cookies
9. **Return JSON**: Via stdout

## Output Format

### Success
```json
{
  "status": "success",
  "timestamp": "2026-01-11T10:30:00Z",
  "cookies": [
    {
      "name": "session_token",
      "value": "abc123...",
      "domain": ".srmist.edu.in",
      "path": "/",
      "httpOnly": true,
      "secure": true,
      "expiry": 1673548800
    }
  ]
}
```

### Error
```json
{
  "status": "error",
  "timestamp": "2026-01-11T10:30:00Z",
  "reason": "LOGIN_FAILED | RATE_LIMIT_BLOCKED | SELECTOR_NOT_FOUND | TIMEOUT",
  "details": "Additional error context"
}
```

## Exit Codes

- `0`: Success
- `1`: Failure

## Dependencies

- Node.js 16+
- Playwright
- Chromium browser (auto-installed)

## Installation

```bash
npm install
npx playwright install chromium
```

## Integration with Go

The Go scraper spawns this service as a subprocess when login is required. The service outputs JSON to stdout which Go parses to extract cookies.