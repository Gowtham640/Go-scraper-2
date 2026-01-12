# SRM Academia Go Scraper

A server-side Go scraper for SRM Academia portal that handles authentication, scrapes student data (user info, courses, timetable, calendar), and stores everything in Supabase with comprehensive logging and token management.

## Features

- ✅ **Multi-step Zoho Authentication**: User lookup, password auth, optional CAPTCHA handling
- ✅ **Session Management**: Automatic token storage with expiry tracking and auto-relogin
- ✅ **HTML Parsing**: Server-side scraping using goquery (no browser automation)
- ✅ **Data Storage**: Seamless integration with Supabase for users, tokens, and cached data
- ✅ **Structured Logging**: JSON-formatted logs for all operations
- ✅ **Rate Limiting**: Built-in rate limiting to prevent SRM portal overload
- ✅ **Health Checks**: Monitor service availability

## Project Structure

```
scraper/
├── main.go                 # Entry point, HTTP server setup
├── go.mod                  # Go module definition
├── .env                    # Environment variables
├── config/
│   └── config.go           # Load env vars, define constants
├── handlers/
│   ├── login.go            # POST /login
│   ├── user.go             # GET /user
│   ├── courses.go          # GET /courses
│   ├── timetable.go        # GET /timetable
│   ├── calendar.go         # GET /calendar
│   └── health.go           # GET /health
├── auth/
│   ├── auth.go             # SRM authentication flow
│   └── session.go          # Token management, auto-relogin
├── scraper/
│   ├── client.go           # HTTP client with SRM headers
│   ├── user.go             # Parse user info table
│   ├── courses.go          # Parse courses table
│   ├── timetable.go        # Generate timetable from courses
│   └── calendar.go         # Parse calendar table
├── storage/
│   └── supabase.go         # Supabase client, CRUD operations
├── models/
│   └── models.go           # All data structures
├── logger/
│   └── logger.go           # Structured JSON logging
└── middleware/
    └── ratelimit.go        # Rate limiting for requests
```

## Installation

1. **Install Go** (version 1.21 or higher)
2. **Clone the repository**
3. **Install dependencies:**
   ```bash
   go mod download
   ```

4. **Configure environment variables** (`.env` file):
   ```
   SUPABASE_URL=https://qndsumtuimqtdyxnvmqv.supabase.co
   ENCRYPTION_KEY=your_service_role_key
   SUPABASE_KEY=your_anon_key
   PORT=8080
   URL=http://localhost:3000
   CRON_SECRET=your_secret
   ```

## Usage

### Running the Server

```bash
go run main.go
```

Or build and run:
```bash
go build -o scraper.exe .
./scraper.exe
```

The server will start on port 8080 (or PORT specified in .env).

## API Endpoints

### 1. POST `/login`
Authenticate user and fetch initial user info.

**Request:**
```json
{
  "account": "email@srmist.edu.in",
  "password": "user_password",
  "cdigest": "optional_captcha_digest",
  "captcha": "optional_captcha_solution"
}
```

**Success Response:**
```json
{
  "success": true,
  "userInfo": {
    "name": "John Doe",
    "mobile": "9876543210",
    "program": "B.Tech Computer Science",
    "semester": 4,
    "regNumber": "RAXXXXXXXXXXXX",
    "batch": "2",
    "year": 2,
    "department": "Computer Science",
    "section": "A"
  }
}
```

**CAPTCHA Required Response:**
```json
{
  "success": false,
  "captcha": "base64_encoded_image",
  "cdigest": "captcha_digest"
}
```

### 2. GET `/user`
Fetch and update user information.

**Headers:**
- `X-User-Id`: User UUID from auth.users
- `X-Email`: User email address

**Response:**
```json
{
  "success": true
}
```

### 3. GET `/courses`
Fetch course data and cache in Supabase.

**Headers:**
- `X-User-Id`: User UUID
- `X-Email`: User email

**Response:**
```json
{
  "success": true
}
```

### 4. GET `/timetable`
Generate timetable from courses and cache.

**Headers:**
- `X-User-Id`: User UUID
- `X-Email`: User email

**Response:**
```json
{
  "success": true
}
```

### 5. GET `/calendar`
Fetch academic calendar and store in calendar table.

**Headers:**
- `X-User-Id`: User UUID
- `X-Email`: User email

**Response:**
```json
{
  "success": true
}
```

### 6. GET `/health`
Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2026-01-11T10:30:00Z",
  "services": {
    "supabase": "connected",
    "srm_api": "accessible"
  }
}
```

## Authentication Flow

1. **User Lookup**: POST to SRM lookup endpoint with email
2. **Password Auth**: POST with password, receive session cookies
3. **CAPTCHA** (if required): Fetch image, retry with solution
4. **Token Storage**: Store cookies in Supabase with expiry

## Token Management

- Tokens stored in `public.tokens` table with expiry timestamp
- Auto-relogin when token expires
- Frontend must send `X-User-Id` and `X-Email` headers for authenticated requests

## Data Storage

### Tables Used

1. **auth.users** - Created via Supabase Admin API during login
2. **public.users** - User profile data
3. **public.tokens** - Session cookies with expiry
4. **public.user_cache** - Cached data (courses, timetable) with data_type field
5. **public.calendar** - Academic calendar (semester=0, course="Default")

## Rate Limiting

- 1 request per second per IP address
- Configurable burst size (default: 3)
- Automatic cleanup of old rate limiters every 10 minutes

## Logging

Structured JSON logs with fields:
```json
{
  "timestamp": "2026-01-11T10:30:00Z",
  "level": "INFO|ERROR|WARN|DEBUG",
  "user": "user@srmist.edu.in",
  "action": "login|scrape_courses|supabase_insert",
  "message": "Detailed message",
  "data": {"additional_context": "..."}
}
```

## Error Handling

- **Login fails**: Returns specific error message
- **HTML parsing fails**: Logs error, returns partial data or error response
- **Supabase fails**: Detailed error logging
- **Session expired**: Automatic relogin (requires password in secure store)

## Development

### Running Tests
```bash
go test ./...
```

### Linting
```bash
golangci-lint run
```

### Building for Production
```bash
go build -ldflags="-s -w" -o scraper .
```

## Security Considerations

- Store `.env` file securely (never commit to version control)
- Use service role key for Supabase operations
- Implement proper password handling for auto-relogin
- Enable HTTPS in production
- Add authentication middleware for API endpoints

## License

MIT License

## Support

For issues and questions, please contact the development team.
