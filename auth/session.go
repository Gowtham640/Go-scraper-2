package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"srm-academia-scraper/config"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// SessionManager manages session tokens and handles auto-relogin
type SessionManager struct {
	authService *AuthService
	storage     *storage.SupabaseClient
}

// NewSessionManager creates a new session manager
func NewSessionManager(storage *storage.SupabaseClient) *SessionManager {
	return &SessionManager{
		authService: NewAuthService(),
		storage:     storage,
	}
}

// GetValidToken retrieves a valid token for a user, login triggered only on expired + 401
func (s *SessionManager) GetValidToken(userID, email string) (string, bool, error) {
	logger.InfoWithUser(email, "session_get_token", "Retrieving token from database", nil)

	// Get token from database
	tokenData, err := s.storage.GetToken(userID)
	if err != nil {
		logger.WarnWithUser(email, "session_get_token", "No token found", nil)
		return "", false, err
	}

	// Return token and whether it's expired
	isExpired := time.Now().After(tokenData.ExpiryTimestamp)
	logger.InfoWithUser(email, "session_get_token", fmt.Sprintf("Token found, expired: %v", isExpired), nil)
	return tokenData.Tokens, isExpired, nil
}

// PerformBrowserLogin executes the Node.js browser login service
func (s *SessionManager) PerformBrowserLogin(userID, email, password string) (string, error) {
	logger.InfoWithUser(email, "session_browser_login", "Starting browser-based login", nil)

	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		logger.ErrorWithUser(email, "session_browser_login", "Failed to get working directory", err, nil)
		return "", err
	}

	// Path to auth-browser directory
	authBrowserDir := filepath.Join(wd, "auth-browser")
	logger.InfoWithUser(email, "session_browser_login", fmt.Sprintf("Auth browser directory: %s", authBrowserDir), nil)

	// Set up environment variables
	env := os.Environ()
	env = append(env, fmt.Sprintf("SRM_EMAIL=%s", email))
	env = append(env, fmt.Sprintf("SRM_PASSWORD=%s", password))
	env = append(env, "TIMEOUT_SECONDS=40")

	logger.InfoWithUser(email, "session_browser_login", "Environment variables configured", map[string]interface{}{
		"email_configured":    email != "",
		"password_configured": password != "",
		"timeout_seconds":     30,
	})

	// HTTP service started by auth-browser/login.js (run start-stack.ps1 or start.sh)
	authPort := "3001"
	if config.AppConfig != nil && config.AppConfig.AuthServicePort != "" {
		authPort = config.AppConfig.AuthServicePort
	}
	loginURL := fmt.Sprintf("http://127.0.0.1:%s/login", authPort)
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	resp, err := http.Post(loginURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.ErrorWithUser(email, "session_browser_login", "Failed to reach auth browser service", err, nil)
		return "", err
	}
	defer resp.Body.Close()

	var response struct {
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
		Cookies []struct {
			Name   string  `json:"name"`
			Value  string  `json:"value"`
			Domain string  `json:"domain"`
			Path   string  `json:"path"`
			Expiry float64 `json:"expiry"`
		} `json:"cookies"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logger.ErrorWithUser(email, "session_browser_login", "Failed to decode auth browser response", err, nil)
		return "", err
	}

	if response.Status != "success" {
		err := fmt.Errorf("auth browser service error: %s", response.Reason)
		logger.ErrorWithUser(email, "session_browser_login", "Auth browser service reported failure", err, nil)
		return "", err
	}

	var cookiePairs []string
	for _, cookie := range response.Cookies {
		cookiePairs = append(cookiePairs, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}

	cookiesStr := strings.Join(cookiePairs, "; ")
	logger.InfoWithUser(email, "session_browser_login", "Received cookies from auth browser service", map[string]interface{}{
		"cookie_count": len(response.Cookies),
	})

	return cookiesStr, nil
}

// PerformLogin performs authentication and stores the new token
func (s *SessionManager) PerformLogin(userID, email, password string) (string, error) {
	logger.InfoWithUser(email, "session_login", "Performing authentication", nil)

	// Perform login
	cookies, _, err := s.authService.Login(email, password, "", "")
	if err != nil {
		logger.ErrorWithUser(email, "session_login", "Authentication failed", err, nil)
		return "", err
	}

	// Extract expiry days from cookies
	expiryDays := ExtractExpiryDays(cookies)

	// Store token in database
	err = s.storage.UpsertToken(userID, email, cookies, expiryDays)
	if err != nil {
		logger.ErrorWithUser(email, "session_login", "Failed to store token", err, nil)
		return "", err
	}

	logger.InfoWithUser(email, "session_login", "Token stored successfully", map[string]interface{}{
		"expiry_days": expiryDays,
	})
	return cookies, nil
}

// LoginAndCreateUser performs login and creates user in auth.users if not exists, stores token
func (s *SessionManager) LoginAndCreateUser(email, password string) (string, error) {
	logger.InfoWithUser(email, "session_create_user", "Creating user and logging in", nil)

	// Perform browser login to get cookies
	cookies, err := s.PerformBrowserLogin(email, email, password)
	if err != nil {
		logger.ErrorWithUser(email, "session_create_user", "Login failed", err, nil)
		return "", err
	}

	// Check if user exists in auth.users
	userID, err := s.storage.GetUserByEmail(email)
	if err != nil {
		// User doesn't exist, create in auth.users
		logger.InfoWithUser(email, "session_create_user", "Creating auth user", nil)
		userID, err = s.storage.CreateAuthUser(email, password)
		if err != nil {
			logger.ErrorWithUser(email, "session_create_user", "Failed to create auth user", err, nil)
			return "", err
		}
	}

	// Store tokens in public.tokens table
	expiryDays := ExtractExpiryDays(cookies)
	err = s.storage.UpsertToken(userID, email, cookies, expiryDays)
	if err != nil {
		logger.ErrorWithUser(email, "session_create_user", "Failed to store token", err, nil)
		return "", err
	}

	logger.InfoWithUser(email, "session_create_user", "User created and logged in successfully", map[string]interface{}{
		"user_id": userID,
	})
	return userID, nil
}

// FetchUserInfo fetches user info using stored token and updates users.html
func (s *SessionManager) FetchUserInfo(userID, email string) (*models.UserInfo, error) {
	logger.InfoWithUser(email, "session_fetch_user_info", "Fetching user info using stored token", nil)

	// Get stored token
	tokenData, err := s.storage.GetToken(userID)
	if err != nil {
		logger.ErrorWithUser(email, "session_fetch_user_info", "Failed to get token", err, nil)
		return nil, err
	}

	// Fetch user info from SRM portal using stored token
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.TimeTableURL, tokenData.Tokens)
	if err != nil {
		logger.ErrorWithUser(email, "session_fetch_user_info", "Failed to fetch user data", err, nil)
		return nil, err
	}

	// Update users.html with fetched HTML content
	err = os.WriteFile("users.html", []byte(htmlContent), 0644)
	if err != nil {
		logger.ErrorWithUser(email, "session_fetch_user_info", "Failed to save HTML content", err, nil)
		// Continue processing even if saving fails
	}

	// Parse user info from HTML
	userInfo, err := scraper.ParseUserInfo(string(htmlContent))
	if err != nil {
		logger.ErrorWithUser(email, "session_fetch_user_info", "Failed to parse user info", err, nil)
		return nil, err
	}

	// Update user data in public.users table
	err = s.storage.UpsertUser(userID, email, userInfo)
	if err != nil {
		logger.ErrorWithUser(email, "session_fetch_user_info", "Failed to update user data", err, nil)
		return nil, err
	}

	logger.InfoWithUser(email, "session_fetch_user_info", "User info fetched and updated successfully", nil)
	return userInfo, nil
}

// ValidateAndRefreshToken checks token validity and refreshes if needed
func (s *SessionManager) ValidateAndRefreshToken(userID, email, password string) (string, bool, error) {
	logger.InfoWithUser(email, "session_validate", "Validating token", nil)

	tokenData, err := s.storage.GetToken(userID)
	if err != nil {
		// No token found, need fresh login
		logger.WarnWithUser(email, "session_validate", "No token found", nil)
		cookies, err := s.PerformLogin(userID, email, password)
		return cookies, true, err
	}

	// Check expiry
	isExpired := time.Now().After(tokenData.ExpiryTimestamp)

	if isExpired {
		logger.InfoWithUser(email, "session_validate", "Token expired, refreshing", nil)
		cookies, err := s.PerformLogin(userID, email, password)
		return cookies, true, err
	}

	logger.InfoWithUser(email, "session_validate", "Token is valid", nil)
	return tokenData.Tokens, false, nil
}
