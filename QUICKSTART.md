# Quick Start Guide - SRM Academia Go Scraper

## Prerequisites

- Go 1.21 or higher installed
- Supabase account with database set up
- Access to SRM Academia portal

## Setup Steps

### 1. Verify Environment Variables

Ensure your `.env` file contains:
```
SUPABASE_URL=https://qndsumtuimqtdyxnvmqv.supabase.co
ENCRYPTION_KEY=your_service_role_key
SUPABASE_KEY=your_anon_key
PORT=8080
URL=http://localhost:3000
CRON_SECRET=your_secret
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Build the Application

```bash
go build -o scraper.exe .
```

### 4. Run the Server

```bash
./scraper.exe
```

Or run directly:
```bash
go run main.go
```

The server will start on `http://localhost:8080`

## Testing the API

### Test Health Check
```bash
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2026-01-11T...",
  "services": {
    "supabase": "connected",
    "srm_api": "accessible"
  }
}
```

### Test Login
```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "account": "student@srmist.edu.in",
    "password": "password123"
  }'
```

Expected response:
```json
{
  "success": true,
  "userInfo": {
    "name": "Student Name",
    "mobile": "1234567890",
    "program": "B.Tech",
    "semester": 4,
    "regNumber": "RA...",
    "batch": "2",
    "year": 2,
    "department": "Computer Science",
    "section": "A"
  }
}
```

### Test User Data Fetch
```bash
curl http://localhost:8080/user \
  -H "X-User-Id: user-uuid-here" \
  -H "X-Email: student@srmist.edu.in"
```

### Test Courses Fetch
```bash
curl http://localhost:8080/courses \
  -H "X-User-Id: user-uuid-here" \
  -H "X-Email: student@srmist.edu.in"
```

### Test Timetable Generation
```bash
curl http://localhost:8080/timetable \
  -H "X-User-Id: user-uuid-here" \
  -H "X-Email: student@srmist.edu.in"
```

### Test Calendar Fetch
```bash
curl http://localhost:8080/calendar \
  -H "X-User-Id: user-uuid-here" \
  -H "X-Email: student@srmist.edu.in"
```

## Common Issues

### Issue: "Failed to initialize Supabase client"
**Solution:** Check your `.env` file has correct `SUPABASE_URL` and `ENCRYPTION_KEY`

### Issue: "Authentication failed"
**Solution:** 
- Verify SRM credentials are correct
- Check if SRM portal is accessible
- May require CAPTCHA (will be returned in response)

### Issue: "Rate limit exceeded"
**Solution:** Wait 1 second between requests or adjust rate limit in `middleware/ratelimit.go`

### Issue: "Token expired"
**Solution:** The system will auto-relogin. Ensure password is stored securely for auto-relogin.

## Data Flow

1. **Login** → Creates auth user → Fetches user info → Stores tokens
2. **User Fetch** → Gets token → Scrapes HTML → Parses data → Updates database
3. **Courses Fetch** → Gets token → Scrapes HTML → Parses courses → Caches data
4. **Timetable Generation** → Gets courses → Maps to hardcoded slots → Caches data
5. **Calendar Fetch** → Gets token → Scrapes calendar HTML → Parses → Stores in calendar table

## Monitoring

Logs are output to stdout in JSON format:
```json
{
  "timestamp": "2026-01-11T10:30:00Z",
  "level": "INFO",
  "user": "student@srmist.edu.in",
  "action": "login",
  "message": "Login successful",
  "data": {}
}
```

### Log Levels
- **INFO**: Normal operations
- **WARN**: Warning conditions (rate limits, missing data)
- **ERROR**: Error conditions (auth failures, parse errors)
- **DEBUG**: Detailed debugging information

## Production Deployment

1. Set `PORT` environment variable for desired port
2. Use reverse proxy (nginx/apache) for HTTPS
3. Set up log aggregation (ELK stack, Datadog, etc.)
4. Configure firewall rules
5. Set up monitoring and alerting
6. Use systemd or similar for process management

## Next Steps

- Implement attendance scraping (when page becomes available)
- Implement marks scraping (when page becomes available)
- Add caching strategy for reduced SRM portal load
- Implement WebSocket for real-time updates
- Add API authentication/authorization
- Set up automated testing

## Support

For issues, check:
1. Logs for detailed error messages
2. Supabase dashboard for database issues
3. SRM portal accessibility
4. Network connectivity
