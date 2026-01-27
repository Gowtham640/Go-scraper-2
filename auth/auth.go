package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"strings"
	"time"
)

const (
	LookupURLTemplate   = "https://academia.srmist.edu.in/accounts/p/40-10002227248/signin/v2/lookup/%s@srmist.edu.in"
	PasswordURLTemplate = "https://academia.srmist.edu.in/accounts/p/40-10002227248/signin/v2/primary/%s/password?digest=%s&cli_time=%d&servicename=ZohoCreator&service_language=en&serviceurl=https://academia.srmist.edu.in/portal/academia-academic-services/redirectFromLogin"
	CaptchaURLTemplate  = "https://academia.srmist.edu.in/accounts/p/40-10002227248/webclient/v1/captcha/%s?darkmode=false"
)

// AuthService handles SRM authentication
type AuthService struct {
	httpClient *scraper.HTTPClient
}

// NewAuthService creates a new authentication service
func NewAuthService() *AuthService {
	return &AuthService{
		httpClient: scraper.NewHTTPClient(),
	}
}

// UserLookup performs the first step of authentication (user lookup)
func (a *AuthService) UserLookup(email string) (*models.LookupResponse, error) {
	fmt.Println("=== STARTING USER LOOKUP ===")
	fmt.Printf("Email: %s\n", email)

	logger.InfoWithUser(email, "auth_lookup", "Starting user lookup", nil)

	// Extract username from email
	username := strings.Split(email, "@")[0]
	fmt.Printf("Extracted username: %s\n", username)

	// Build lookup URL
	lookupURL := fmt.Sprintf(LookupURLTemplate, username)
	fmt.Printf("Lookup URL: %s\n", lookupURL)

	// Build form data
	timestamp := time.Now().Unix() * 1000
	formData := url.Values{}
	formData.Set("mode", "primary")
	formData.Set("cli_time", fmt.Sprintf("%d", timestamp))
	formData.Set("orgtype", "40")
	formData.Set("service_language", "en")
	formData.Set("serviceurl", "https://academia.srmist.edu.in/portal/academia-academic-services/redirectFromLogin")

	fmt.Printf("Form data: %s\n", formData.Encode())
	fmt.Printf("Current timestamp: %d\n", timestamp)

	// Make the request
	fmt.Println("Making HTTP POST request to lookup URL...")
	scraper.RateLimit(1 * time.Second)
	body, err := a.httpClient.Post(lookupURL, strings.NewReader(formData.Encode()))
	if err != nil {
		fmt.Printf("ERROR: HTTP request failed: %v\n", err)
		logger.ErrorWithUser(email, "auth_lookup", "User lookup failed", err, nil)
		return nil, err
	}

	fmt.Printf("Raw response body: %s\n", string(body))

	// Parse response
	var lookupResp models.LookupResponse
	if err := json.Unmarshal(body, &lookupResp); err != nil {
		fmt.Printf("ERROR: Failed to parse JSON response: %v\n", err)
		logger.ErrorWithUser(email, "auth_lookup", "Failed to parse lookup response", err, nil)
		return nil, err
	}

	fmt.Printf("Parsed response - Identifier: %s, Digest: %s\n", lookupResp.Identifier, lookupResp.Digest)
	fmt.Println("=== USER LOOKUP COMPLETED SUCCESSFULLY ===")

	logger.InfoWithUser(email, "auth_lookup", "User lookup successful", map[string]interface{}{
		"identifier": lookupResp.Identifier,
	})
	return &lookupResp, nil
}

// PasswordAuth performs the second step of authentication (password verification)
func (a *AuthService) PasswordAuth(identifier, digest, password string) (string, error) {
	fmt.Println("=== STARTING PASSWORD AUTHENTICATION ===")
	fmt.Printf("Identifier: %s\n", identifier)
	fmt.Printf("Digest: %s\n", digest)
	fmt.Printf("Password: %s\n", strings.Repeat("*", len(password)))

	logger.Info("auth_password", "Starting password authentication", nil)

	// Build password URL
	timestamp := time.Now().Unix() * 1000
	passwordURL := fmt.Sprintf(PasswordURLTemplate, identifier, digest, timestamp)
	fmt.Printf("Password URL: %s\n", passwordURL)
	fmt.Printf("Current timestamp: %d\n", timestamp)

	// Build JSON body
	passwordData := map[string]interface{}{
		"passwordauth": map[string]string{
			"password": password,
		},
	}
	jsonData, err := json.Marshal(passwordData)
	if err != nil {
		fmt.Printf("ERROR: Failed to marshal password data: %v\n", err)
		logger.Error("auth_password", "Failed to marshal password data", err, nil)
		return "", err
	}

	fmt.Printf("Request JSON body: %s\n", string(jsonData))

	// Create request with custom headers
	req, err := http.NewRequest("POST", passwordURL, strings.NewReader(string(jsonData)))
	if err != nil {
		fmt.Printf("ERROR: Failed to create password request: %v\n", err)
		logger.Error("auth_password", "Failed to create password request", err, nil)
		return "", err
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-zcsrf-token", "iamcsrcoo="+digest)
	req.Header.Set("User-Agent", scraper.UserAgent)
	req.Header.Set("Accept", "application/json")

	fmt.Printf("Request headers:\n")
	fmt.Printf("  Content-Type: %s\n", req.Header.Get("Content-Type"))
	fmt.Printf("  x-zcsrf-token: %s\n", req.Header.Get("x-zcsrf-token"))
	fmt.Printf("  User-Agent: %s\n", req.Header.Get("User-Agent"))
	fmt.Printf("  Accept: %s\n", req.Header.Get("Accept"))

	// Execute request
	fmt.Println("Making HTTP POST request to password URL...")
	scraper.RateLimit(1 * time.Second)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("ERROR: HTTP request failed: %v\n", err)
		logger.Error("auth_password", "Password authentication failed", err, nil)
		return "", err
	}
	defer resp.Body.Close()

	fmt.Printf("Response status code: %d\n", resp.StatusCode)
	fmt.Printf("Response status: %s\n", resp.Status)

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ERROR: Failed to read response body: %v\n", err)
	} else {
		fmt.Printf("Response body: %s\n", string(respBody))
	}

	// Extract cookies from response
	cookies := scraper.ExtractCookies(resp)
	fmt.Printf("Cookies received: %s\n", cookies)

	// Check all response headers
	fmt.Println("All response headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("ERROR: Password auth failed with status %d\n", resp.StatusCode)
		logger.Error("auth_password", fmt.Sprintf("Password auth failed with status %d", resp.StatusCode), nil, map[string]interface{}{
			"status_code": resp.StatusCode,
		})
		return "", fmt.Errorf("password authentication failed with status %d", resp.StatusCode)
	}

	fmt.Println("=== PASSWORD AUTHENTICATION COMPLETED SUCCESSFULLY ===")
	logger.Info("auth_password", "Password authentication successful", nil)
	return cookies, nil
}

// FetchCaptcha fetches CAPTCHA image when required
func (a *AuthService) FetchCaptcha(cdigest string) (string, error) {
	logger.Info("auth_captcha", "Fetching CAPTCHA image", nil)

	captchaURL := fmt.Sprintf(CaptchaURLTemplate, cdigest)

	scraper.RateLimit(1 * time.Second)
	body, err := a.httpClient.Get(captchaURL)
	if err != nil {
		logger.Error("auth_captcha", "Failed to fetch CAPTCHA", err, nil)
		return "", err
	}

	// Parse CAPTCHA response
	var captchaResp models.CaptchaResponse
	if err := json.Unmarshal(body, &captchaResp); err != nil {
		logger.Error("auth_captcha", "Failed to parse CAPTCHA response", err, nil)
		return "", err
	}

	logger.Info("auth_captcha", "CAPTCHA fetched successfully", nil)
	return captchaResp.Captcha.ImageBytes, nil
}

// Login performs the complete authentication flow
func (a *AuthService) Login(email, password, cdigest, captcha string) (string, string, error) {
	fmt.Println("=========================================")
	fmt.Printf("STARTING COMPLETE LOGIN FLOW FOR: %s\n", email)
	fmt.Printf("Password length: %d characters\n", len(password))
	fmt.Printf("CDigest provided: %s\n", cdigest)
	fmt.Printf("Captcha provided: %s\n", captcha)
	fmt.Println("=========================================")

	logger.InfoWithUser(email, "auth_login", "Starting complete login flow", nil)

	// Step 1: User lookup
	fmt.Println("\n--- STEP 1: USER LOOKUP ---")
	lookupResp, err := a.UserLookup(email)
	if err != nil {
		fmt.Printf("USER LOOKUP FAILED: %v\n", err)
		// Check if CAPTCHA is required
		if strings.Contains(err.Error(), "HIP") || cdigest != "" {
			if cdigest == "" {
				// Need to fetch CAPTCHA
				fmt.Println("CAPTCHA is required but not provided")
				logger.InfoWithUser(email, "auth_login", "CAPTCHA required, but not provided", nil)
				return "", "", fmt.Errorf("CAPTCHA_REQUIRED")
			}
			// CAPTCHA provided, include in lookup
			// TODO: Implement CAPTCHA retry logic
			fmt.Println("CAPTCHA provided, attempting retry...")
		}
		fmt.Printf("LOGIN FAILED AT USER LOOKUP STEP\n")
		return "", "", err
	}

	fmt.Printf("USER LOOKUP SUCCESSFUL - Identifier: %s, Digest: %s\n", lookupResp.Identifier, lookupResp.Digest)

	// Step 2: Password authentication
	fmt.Println("\n--- STEP 2: PASSWORD AUTHENTICATION ---")
	cookies, err := a.PasswordAuth(lookupResp.Identifier, lookupResp.Digest, password)
	if err != nil {
		fmt.Printf("PASSWORD AUTHENTICATION FAILED: %v\n", err)
		fmt.Printf("LOGIN FAILED AT PASSWORD STEP\n")
		logger.ErrorWithUser(email, "auth_login", "Login failed at password step", err, nil)
		return "", "", err
	}

	fmt.Printf("PASSWORD AUTHENTICATION SUCCESSFUL\n")
	fmt.Printf("COOKIES RECEIVED: %s\n", cookies)
	fmt.Println("\n=========================================")
	fmt.Println("LOGIN COMPLETED SUCCESSFULLY")
	fmt.Println("=========================================")

	logger.InfoWithUser(email, "auth_login", "Login successful", nil)
	return cookies, lookupResp.Identifier, nil
}

// ExtractExpiryDays extracts expiry days from cookie string
func ExtractExpiryDays(cookies string) int {
	// Default to 35 days if not found
	defaultExpiry := 35

	// Look for Max-Age or expires in cookies
	parts := strings.Split(cookies, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "max-age=") {
			// Extract max-age value
			maxAge := strings.TrimPrefix(strings.ToLower(part), "max-age=")
			var seconds int
			fmt.Sscanf(maxAge, "%d", &seconds)
			if seconds > 0 {
				return seconds / (24 * 3600) // Convert seconds to days
			}
		}
	}

	return defaultExpiry
}
