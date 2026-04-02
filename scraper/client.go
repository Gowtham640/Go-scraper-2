package scraper

import (
	"fmt"
	"io"
	"net/http"
	"srm-academia-scraper/logger"
	"strings"
	"time"
)

const (
	UserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	ContentType   = "application/x-www-form-urlencoded; charset=UTF-8"
	SRMBaseURL    = "https://academia.srmist.edu.in"
	TimeTableURL  = SRMBaseURL + "/srm_university/academia-academic-services/page/My_Time_Table_2023_24"
	CalendarURL   = SRMBaseURL + "/srm_university/academia-academic-services/page/Academic_Planner_2025_26_EVEN"
	AttendanceURL = SRMBaseURL + "/srm_university/academia-academic-services/page/My_Attendance"
)

// HTTPClient wraps http.Client with SRM-specific headers
type HTTPClient struct {
	client  *http.Client
	cookies string
}

// NewHTTPClient creates a new HTTP client for SRM requests
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				logger.Info("http_redirect", fmt.Sprintf("Following redirect to %s", req.URL.String()), map[string]interface{}{
					"redirect_url": req.URL.String(),
					"redirect_count": len(via),
					"original_url": via[0].URL.String(),
				})
				return nil
			},
		},
	}
}

// SetCookies sets the session cookies for authenticated requests
func (c *HTTPClient) SetCookies(cookies string) {
	c.cookies = cookies
}

// MakeRequest makes an HTTP request with SRM-specific headers
func (c *HTTPClient) MakeRequest(method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		logger.Error("http_request", "Failed to create request", err, nil)
		return nil, err
	}

	// Add SRM-required headers
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", ContentType)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Win32"`)
	req.Header.Set("sec-gpc", "1")
	req.Header.Set("dnt", "1")
	req.Header.Set("Referer", "https://academia.srmist.edu.in/")

	// Add cookies if available (never log raw Cookie header: contains session values).
	if c.cookies != "" {
		req.Header.Set("Cookie", c.cookies)
	}

	// Read request body if present
	if body != nil {
		if bodyBytes, err := io.ReadAll(body); err == nil {
			// Reset body for the actual request
			req.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
		}
	}

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error("http_request", "Failed to execute request", err, nil)
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("http_request", "Failed to read response body", err, nil)
		return nil, err
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		logger.Error("http_request", fmt.Sprintf("Request failed with status %d", resp.StatusCode), nil, map[string]interface{}{
			"status_code": resp.StatusCode,
			"url":         url,
			"body_length": len(bodyBytes),
		})
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	return bodyBytes, nil
}

// GetWithCookies makes a GET request with session cookies
func (c *HTTPClient) GetWithCookies(url, cookies string) ([]byte, error) {
	logger.Info("http_request", "GET", map[string]interface{}{
		"url":         url,
		"cookie_len":  len(cookies),
		"has_cookies": cookies != "",
	})
	c.SetCookies(cookies)
	return c.MakeRequest("GET", url, nil)
}

// PostWithCookies makes a POST request with session cookies
func (c *HTTPClient) PostWithCookies(url, cookies string, body io.Reader) ([]byte, error) {
	logger.Info("http_request", "POST", map[string]interface{}{
		"url":         url,
		"cookie_len":  len(cookies),
		"has_cookies": cookies != "",
	})
	c.SetCookies(cookies)
	return c.MakeRequest("POST", url, body)
}

// Post makes a POST request
func (c *HTTPClient) Post(url string, body io.Reader) ([]byte, error) {
	return c.MakeRequest("POST", url, body)
}

// Get makes a GET request
func (c *HTTPClient) Get(url string) ([]byte, error) {
	return c.MakeRequest("GET", url, nil)
}

// ExtractCookies extracts cookies from HTTP response headers
func ExtractCookies(resp *http.Response) string {
	cookies := []string{}
	for _, cookie := range resp.Cookies() {
		cookieStr := fmt.Sprintf("%s=%s", cookie.Name, cookie.Value)
		cookies = append(cookies, cookieStr)
	}

	result := strings.Join(cookies, "; ")
	return result
}

// RateLimit adds a delay between requests to avoid rate limiting
func RateLimit(duration time.Duration) {
	time.Sleep(duration)
}
