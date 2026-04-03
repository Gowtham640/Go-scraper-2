package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"srm-academia-scraper/cookiecheck"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/passcrypt"
	"strings"
	"time"

	"github.com/supabase-community/gotrue-go/types"
	"github.com/supabase-community/postgrest-go"
	supabase "github.com/supabase-community/supabase-go"
)

type SupabaseClient struct {
	client      *supabase.Client
	passwordKey []byte
}

var ErrQueueFull = errors.New("queue_full")

// ErrLoginRateLimited is returned when a new login job is rejected because a successful
// auth job completed within the configured cooldown window.
var ErrLoginRateLimited = errors.New("login rate limited")

// LoginAuthCooldown is the minimum time between enqueueing or claiming a new login job after a
// successful auth (done) job for the same user.
const LoginAuthCooldown = 10 * time.Minute

const (
	allowedCalendarEmail = "gr8790@srmist.edu.in"
	allowedCalendarID    = "9d7de386-5557-4df7-8148-2e70c0a904ee"
)

func NewSupabaseClient(url, key string, passwordKey []byte) (*SupabaseClient, error) {
	logger.Info("supabase_init", "Initializing Supabase client", nil)

	client, err := supabase.NewClient(url, key, nil)
	if err != nil {
		logger.Error("supabase_init", "Failed to create Supabase client", err, nil)
		return nil, err
	}

	logger.Info("supabase_init", "Supabase client initialized successfully", nil)
	return &SupabaseClient{client: client, passwordKey: passwordKey}, nil
}

// CreateAuthUser creates a user in auth.users table
func (s *SupabaseClient) CreateAuthUser(email, password string) (string, error) {
	logger.InfoWithUser(email, "create_auth_user", "Creating auth user", nil)

	// Use Supabase Admin API to create user
	user, err := s.client.Auth.Signup(types.SignupRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		logger.ErrorWithUser(email, "create_auth_user", "Failed to create auth user", err, nil)
		return "", err
	}

	logger.InfoWithUser(email, "create_auth_user", "Auth user created successfully", map[string]interface{}{
		"user_id": user.ID,
	})
	return user.ID.String(), nil
}

// UpsertUser upserts user data in public.users table
func (s *SupabaseClient) UpsertUser(userID, email string, userInfo *models.UserInfo) error {
	logger.InfoWithUser(email, "upsert_user", "Upserting user data", map[string]interface{}{
		"batch": userInfo.Batch,
	})

	data := map[string]interface{}{
		"id":             userID,
		"email":          email,
		"semester":       userInfo.Semester,
		"name":           userInfo.Name,
		"regnumber":      userInfo.RegNumber,
		"department":     userInfo.Department,
		"mobile":         userInfo.Mobile,
		"program":        userInfo.Program,
		"batch":          userInfo.Batch,
		"year":           userInfo.Year,
		"section":        userInfo.Section,
		"specialization": userInfo.Specialization,
	}

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").Upsert(data, "", "", "").ExecuteTo(&result)
	if err != nil {
		logger.ErrorWithUser(email, "upsert_user", "Failed to upsert user data", err, nil)
		return err
	}

	logger.InfoWithUser(email, "upsert_user", "User data upserted successfully", nil)
	return nil
}

// SaveUserEncryptedPassword encrypts plaintext with AES-GCM and upserts public.users (encrypted_password, password_iv, password_tag).
func (s *SupabaseClient) SaveUserEncryptedPassword(userID, email, plaintext string) error {
	logger.Info("save_user_encrypted_password", "flow start", map[string]interface{}{
		"user_id":                     userID,
		"email":                       email,
		"plaintext_password_nonempty": plaintext != "",
		"encryption_key_configured":   len(s.passwordKey) > 0,
	})
	if userID == "" {
		logger.Error("save_user_encrypted_password", "abort: user id empty", nil, nil)
		return fmt.Errorf("user id required")
	}
	if email == "" {
		logger.Error("save_user_encrypted_password", "abort: email empty", nil, map[string]interface{}{"user_id": userID})
		return fmt.Errorf("email required")
	}
	if plaintext == "" {
		logger.Error("save_user_encrypted_password", "abort: password plaintext empty", nil, map[string]interface{}{"user_id": userID})
		return fmt.Errorf("empty password")
	}
	if len(s.passwordKey) == 0 {
		logger.Error("save_user_encrypted_password", "abort: PASSWORD_KEY not loaded on client", nil, map[string]interface{}{"user_id": userID})
		return fmt.Errorf("password key not configured")
	}

	logger.Info("save_user_encrypted_password", "encrypt: invoking AES-GCM", map[string]interface{}{
		"user_id": userID,
	})
	encB64, ivB64, tagB64, err := passcrypt.EncryptAESGCM(plaintext, s.passwordKey)
	if err != nil {
		logger.Error("save_user_encrypted_password", "encrypt: AES-GCM failed", err, map[string]interface{}{
			"user_id": userID,
		})
		return err
	}
	logger.Info("save_user_encrypted_password", "encrypt: succeeded", map[string]interface{}{
		"user_id":            userID,
		"ciphertext_b64_len": len(encB64),
		"nonce_b64_len":      len(ivB64),
		"tag_b64_len":        len(tagB64),
	})

	// Upsert so a row exists even before profile fields are filled by FetchUserInfo.
	data := map[string]interface{}{
		"id":                 userID,
		"email":              email,
		"role":               "public",
		"encrypted_password": encB64,
		"password_iv":        ivB64,
		"password_tag":       tagB64,
	}

	logger.Info("save_user_encrypted_password", "upsert: posting to public.users", map[string]interface{}{
		"user_id": userID,
	})
	var result []map[string]interface{}
	_, err = s.client.From("users").Upsert(data, "", "", "").ExecuteTo(&result)

	if err != nil {
		logger.Error("save_user_encrypted_password", "upsert: Supabase request failed", err, map[string]interface{}{
			"user_id": userID,
		})
		return err
	}

	logger.Info("save_user_encrypted_password", "upsert: completed OK (encrypted_password, password_iv, password_tag)", map[string]interface{}{
		"user_id": userID,
	})
	return nil
}

// DecryptStoredPassword decrypts public.users password fields using the same PASSWORD_KEY as SaveUserEncryptedPassword.
func (s *SupabaseClient) DecryptStoredPassword(encryptedPasswordB64, passwordIVB64, passwordTagB64 string) (string, error) {
	logger.Info("decrypt_stored_password", "flow start", map[string]interface{}{
		"encryption_key_configured": len(s.passwordKey) > 0,
		"ciphertext_b64_nonempty":   encryptedPasswordB64 != "",
		"nonce_b64_nonempty":        passwordIVB64 != "",
		"tag_b64_nonempty":          passwordTagB64 != "",
	})
	if len(s.passwordKey) == 0 {
		logger.Error("decrypt_stored_password", "abort: password key not configured", nil, nil)
		return "", fmt.Errorf("password key not configured")
	}
	plain, err := passcrypt.DecryptAESGCM(encryptedPasswordB64, passwordIVB64, passwordTagB64, s.passwordKey)
	if err != nil {
		logger.Error("decrypt_stored_password", "decrypt: AES-GCM Open failed", err, nil)
		return "", err
	}
	logger.Info("decrypt_stored_password", "decrypt: succeeded", map[string]interface{}{
		"plaintext_len": len(plain),
	})
	return plain, nil
}

// UpsertToken stores or updates session tokens in public.tokens table
func (s *SupabaseClient) UpsertToken(userID, email, tokens string, expiryDays int) error {
	missing := cookiecheck.GetMissingCriticalCookies(tokens)
	missingJSON, err := json.Marshal(missing)
	if err != nil {
		return fmt.Errorf("marshal missing_tokens: %w", err)
	}
	missingStr := string(missingJSON)

	logger.InfoWithUser(email, "upsert_token", "Upserting token", map[string]interface{}{
		"expiry_days":   expiryDays,
		"missing_count": len(missing),
	})

	expiryTimestamp := time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour)

	data := map[string]interface{}{
		"user_id":          userID,
		"tokens":           tokens,
		"expiry_timestamp": expiryTimestamp.Format(time.RFC3339),
		"email":            email,
		"missing_tokens":   missingStr,
	}

	var result []map[string]interface{}

	var existing []map[string]interface{}
	_, err = s.client.From("tokens").
		Select("id", "", false).
		Eq("user_id", userID).
		ExecuteTo(&existing)
	if err != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to query existing token", err, nil)
		return err
	}

	if len(existing) > 0 {
		updateData := map[string]interface{}{
			"tokens":           tokens,
			"expiry_timestamp": expiryTimestamp.Format(time.RFC3339),
			"email":            email,
			"failure_count":    0,
			"missing_tokens":   missingStr,
		}

		logger.InfoWithUser(email, "upsert_token", "Updating existing token", map[string]interface{}{"token_id": existing[0]["id"]})
		_, err = s.client.From("tokens").
			Update(updateData, "", "").
			Eq("user_id", userID).
			ExecuteTo(&result)
	} else {
		logger.InfoWithUser(email, "upsert_token", "Inserting new token", nil)
		_, err = s.client.From("tokens").
			Insert(data, false, "", "", "").
			ExecuteTo(&result)
	}

	if err != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to upsert token", err, nil)
		return err
	}

	// Token upsert succeeded: mark the user as having a valid token.
	// This is intentionally placed after the token write succeeds.
	var userUpdateResult []map[string]interface{}
	_, userUpdateErr := s.client.From("users").
		Update(map[string]interface{}{"has_token": true}, "", "").
		Eq("id", userID).
		ExecuteTo(&userUpdateResult)
	if userUpdateErr != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to set users.has_token=true after token upsert", userUpdateErr, nil)
		return userUpdateErr
	}

	logger.InfoWithUser(email, "upsert_token", "Token upserted successfully", nil)
	return nil
}

// IncrementTokenFailureCount increments failure_count for a token row and returns the updated value.
func (s *SupabaseClient) IncrementTokenFailureCount(userID string, failureReason *string) (int, error) {
	logger.Info("increment_token_failure_count", "Incrementing token failure_count", map[string]interface{}{
		"user_id": userID,
	})

	var currentResult []map[string]interface{}
	_, err := s.client.From("tokens").
		Select("failure_count", "", false).
		Eq("user_id", userID).
		Limit(1, "").
		ExecuteTo(&currentResult)
	if err != nil {
		logger.Error("increment_token_failure_count", "Failed to fetch current failure_count", err, map[string]interface{}{
			"user_id": userID,
		})
		return 0, err
	}
	if len(currentResult) == 0 {
		return 0, fmt.Errorf("token not found for user")
	}

	currentCountFloat, ok := currentResult[0]["failure_count"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid failure_count type")
	}
	nextCount := int(currentCountFloat) + 1

	updateData := map[string]interface{}{
		"failure_count": nextCount,
	}
	if failureReason != nil {
		updateData["failure_reason"] = *failureReason
	}

	var updateResult []map[string]interface{}
	_, err = s.client.From("tokens").
		Update(updateData, "", "").
		Eq("user_id", userID).
		ExecuteTo(&updateResult)
	if err != nil {
		logger.Error("increment_token_failure_count", "Failed to update failure_count", err, map[string]interface{}{
			"user_id": userID,
		})
		return 0, err
	}

	logger.Info("increment_token_failure_count", "Token failure_count incremented", map[string]interface{}{
		"user_id":       userID,
		"failure_count": nextCount,
	})
	return nextCount, nil
}

// GetUserDecryptedPassword returns the stored decrypted portal password for a user.
func (s *SupabaseClient) GetUserDecryptedPassword(userID string) (string, error) {
	var result []map[string]interface{}
	_, err := s.client.From("users").
		Select("encrypted_password,password_iv,password_tag", "", false).
		Eq("id", userID).
		Limit(1, "").
		ExecuteTo(&result)
	if err != nil {
		return "", err
	}
	if len(result) == 0 {
		return "", fmt.Errorf("user not found")
	}

	encryptedPassword, ok1 := result[0]["encrypted_password"].(string)
	passwordIV, ok2 := result[0]["password_iv"].(string)
	passwordTag, ok3 := result[0]["password_tag"].(string)
	if !ok1 || !ok2 || !ok3 || encryptedPassword == "" || passwordIV == "" || passwordTag == "" {
		return "", fmt.Errorf("stored encrypted password not available")
	}

	return s.DecryptStoredPassword(encryptedPassword, passwordIV, passwordTag)
}

// GetToken retrieves token by user ID
func (s *SupabaseClient) GetToken(userID string) (*models.TokenData, error) {
	logger.Info("get_token", "Fetching token from database", map[string]interface{}{
		"user_id": userID,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("tokens").
		Select("*", "", false).
		Eq("user_id", userID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_token", "Failed to fetch token", err, nil)
		return nil, err
	}

	if len(result) == 0 {
		logger.Warn("get_token", "No token found for user", map[string]interface{}{
			"user_id": userID,
		})
		return nil, fmt.Errorf("no token found for user")
	}

	// Parse the result
	tokenData := &models.TokenData{
		UserID: result[0]["user_id"].(string),
		Tokens: result[0]["tokens"].(string),
		Email:  result[0]["email"].(string),
	}

	// Parse expiry timestamp
	if expiryStr, ok := result[0]["expiry_timestamp"].(string); ok {
		expiry, err := time.Parse(time.RFC3339, expiryStr)
		if err == nil {
			tokenData.ExpiryTimestamp = expiry
		}
	}

	logger.Info("get_token", "Token fetched successfully", nil)
	return tokenData, nil
}

// UpsertUserCache stores data in public.user_cache table
func (s *SupabaseClient) UpsertUserCache(userID, dataType string, data interface{}, expiresInHours int) error {
	logger.Info("upsert_cache", "Upserting user cache", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	expiresAt := time.Now().Add(time.Duration(expiresInHours) * time.Hour)

	// Check if record exists
	var existingResult []map[string]interface{}
	_, err := s.client.From("user_cache").
		Select("id", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("upsert_cache", "Failed to check existing cache", err, nil)
		return err
	}

	var result []map[string]interface{}
	if len(existingResult) > 0 {
		// Update existing record
		updateData := map[string]interface{}{
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
			"expires_at": expiresAt.Format(time.RFC3339),
		}

		_, err = s.client.From("user_cache").
			Update(updateData, "", "").
			Eq("user_id", userID).
			Eq("data_type", dataType).
			ExecuteTo(&result)
	} else {
		// Insert new record
		insertData := map[string]interface{}{
			"user_id":    userID,
			"data_type":  dataType,
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
			"expires_at": expiresAt.Format(time.RFC3339),
		}

		_, err = s.client.From("user_cache").Insert(insertData, false, "", "", "").ExecuteTo(&result)
	}

	if err != nil {
		logger.Error("upsert_cache", "Failed to upsert cache", err, nil)
		return err
	}

	logger.Info("upsert_cache", "Cache upserted successfully", nil)
	return nil
}

// GetTimetableModification returns modified_json for a user, or nil if no row exists.
func (s *SupabaseClient) GetTimetableModification(userID string) ([]byte, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("timetable_modification").
		Select("modified_json", "", false).
		Eq("user_id", userID).
		ExecuteTo(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	raw, ok := rows[0]["modified_json"]
	if !ok || raw == nil {
		return nil, nil
	}
	return json.Marshal(raw)
}

// UpsertCalendar updates only the fixed calendar row in public.calendar.
// No insert fallback is allowed.
func (s *SupabaseClient) UpsertCalendar(course string, semester int, data interface{}) error {
	logger.Info("upsert_calendar", "Upserting calendar", map[string]interface{}{
		"course":   course,
		"semester": semester,
		"id":       allowedCalendarID,
	})

	updateData := map[string]interface{}{
		"data":       data,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	var result []map[string]interface{}
	_, err := s.client.From("calendar").
		Update(updateData, "", "").
		Eq("id", allowedCalendarID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("upsert_calendar", "Failed to update fixed calendar row", err, map[string]interface{}{
			"id": allowedCalendarID,
		})
		return err
	}
	if len(result) == 0 {
		return fmt.Errorf("calendar row not found for id %s", allowedCalendarID)
	}

	logger.Info("upsert_calendar", "Calendar row updated successfully", map[string]interface{}{
		"id": allowedCalendarID,
	})
	return nil
}

// GetUserCredentialsByEmail returns user id and decrypted password for the exact allowed calendar email.
func (s *SupabaseClient) GetUserCredentialsByEmail(email string) (string, string, error) {
	if email != allowedCalendarEmail {
		return "", "", fmt.Errorf("email not allowed for calendar cron")
	}

	var result []map[string]interface{}
	_, err := s.client.From("users").
		Select("id,encrypted_password,password_iv,password_tag", "", false).
		Eq("email", email).
		Limit(1, "").
		ExecuteTo(&result)
	if err != nil {
		return "", "", err
	}
	if len(result) == 0 {
		return "", "", fmt.Errorf("user not found")
	}

	userID, ok := result[0]["id"].(string)
	if !ok || userID == "" {
		return "", "", fmt.Errorf("invalid user id for email")
	}

	encryptedPassword, ok1 := result[0]["encrypted_password"].(string)
	passwordIV, ok2 := result[0]["password_iv"].(string)
	passwordTag, ok3 := result[0]["password_tag"].(string)
	if !ok1 || !ok2 || !ok3 || encryptedPassword == "" || passwordIV == "" || passwordTag == "" {
		return "", "", fmt.Errorf("stored encrypted password not available")
	}

	password, err := s.DecryptStoredPassword(encryptedPassword, passwordIV, passwordTag)
	if err != nil {
		return "", "", err
	}

	return userID, password, nil
}

// GetUserByEmail retrieves user by email
func (s *SupabaseClient) GetUserByEmail(email string) (string, error) {
	logger.Info("get_user_by_email", "Fetching user by email", map[string]interface{}{
		"email": email,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").
		Select("id", "", false).
		Eq("email", email).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_by_email", "Failed to fetch user", err, nil)
		return "", err
	}

	if len(result) == 0 {
		logger.Warn("get_user_by_email", "User not found", map[string]interface{}{
			"email": email,
		})
		return "", fmt.Errorf("user not found")
	}

	userID := result[0]["id"].(string)
	logger.Info("get_user_by_email", "User found", map[string]interface{}{
		"user_id": userID,
	})
	return userID, nil
}

// GetUserBatch retrieves user's batch from public.users table
func (s *SupabaseClient) GetUserBatch(userID string) (string, error) {
	logger.Info("get_user_batch", "Fetching user batch", map[string]interface{}{
		"user_id": userID,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").
		Select("batch", "", false).
		Eq("id", userID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_batch", "Failed to fetch user batch", err, nil)
		return "", err
	}

	if len(result) == 0 {
		logger.Warn("get_user_batch", "User not found", map[string]interface{}{
			"user_id": userID,
		})
		return "", fmt.Errorf("user not found")
	}

	rawBatch, exists := result[0]["batch"]
	if !exists || rawBatch == nil {
		logger.Warn("get_user_batch", "Batch is missing or null for user", map[string]interface{}{
			"user_id":   userID,
			"batch_raw": rawBatch,
		})
		return "", fmt.Errorf("batch is null")
	}

	var batch string
	switch value := rawBatch.(type) {
	case string:
		batch = strings.TrimSpace(value)
	case float64:
		batch = strings.TrimSpace(fmt.Sprintf("%.0f", value))
	case int:
		batch = strings.TrimSpace(fmt.Sprintf("%d", value))
	default:
		logger.Warn("get_user_batch", "Batch has unsupported type", map[string]interface{}{
			"user_id":    userID,
			"batch_type": fmt.Sprintf("%T", rawBatch),
			"batch_raw":  rawBatch,
		})
		return "", fmt.Errorf("batch has unsupported type")
	}

	if batch == "" {
		logger.Warn("get_user_batch", "Batch is empty after normalization", map[string]interface{}{
			"user_id":    userID,
			"batch_type": fmt.Sprintf("%T", rawBatch),
			"batch_raw":  rawBatch,
		})
		return "", fmt.Errorf("batch is empty")
	}

	logger.Info("get_user_batch", "User batch found", map[string]interface{}{
		"user_id":    userID,
		"batch":      batch,
		"batch_type": fmt.Sprintf("%T", rawBatch),
		"batch_raw":  rawBatch,
	})
	return batch, nil
}

// GetUserEmail retrieves the registered email for a user ID from the users table
func (s *SupabaseClient) GetUserEmail(userID string) (string, error) {
	logger.Info("get_user_email", "Fetching user email", map[string]interface{}{
		"user_id": userID,
	})

	var result []map[string]interface{}
	_, err := s.client.From("users").
		Select("email", "", false).
		Eq("id", userID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_email", "Failed to fetch user email", err, nil)
		return "", err
	}

	if len(result) == 0 {
		logger.Warn("get_user_email", "User not found when fetching email", map[string]interface{}{
			"user_id": userID,
		})
		return "", fmt.Errorf("user not found")
	}

	email, ok := result[0]["email"].(string)
	if !ok {
		return "", fmt.Errorf("email field missing")
	}

	logger.Info("get_user_email", "User email retrieved", map[string]interface{}{
		"user_id": userID,
	})
	return email, nil
}

// GetUserCache retrieves cached data for a user by data_type
func (s *SupabaseClient) GetUserCache(userID, dataType string) (interface{}, error) {
	logger.Info("get_user_cache", "Fetching user cache", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("user_cache").
		Select("data", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_cache", "Failed to fetch user cache", err, nil)
		return nil, err
	}

	if len(result) == 0 {
		logger.Info("get_user_cache", "No cached data found", map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return nil, fmt.Errorf("no cached data found")
	}

	rawData := result[0]["data"]

	// Unmarshal based on data type
	var unmarshaledData interface{}
	switch dataType {
	case "courses":
		var coursesData models.CoursesData
		if err := s.unmarshalData(rawData, &coursesData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal courses data", err, nil)
			return nil, err
		}
		unmarshaledData = coursesData
	case "timetable":
		var timetableData models.TimetableData
		if err := s.unmarshalData(rawData, &timetableData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal timetable data", err, nil)
			return nil, err
		}
		unmarshaledData = timetableData
	case "calendar":
		var calendarData models.CalendarData
		if err := s.unmarshalData(rawData, &calendarData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal calendar data", err, nil)
			return nil, err
		}
		unmarshaledData = calendarData
	case "user":
		var userInfo models.UserInfo
		if err := s.unmarshalData(rawData, &userInfo); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal user data", err, nil)
			return nil, err
		}
		unmarshaledData = userInfo
	default:
		logger.Warn("get_user_cache", "Unknown data type, returning raw data", map[string]interface{}{
			"data_type": dataType,
		})
		unmarshaledData = rawData
	}

	logger.Info("get_user_cache", "User cache found and unmarshaled", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})
	return unmarshaledData, nil
}

// unmarshalData unmarshals JSON data from Supabase to the target struct
func (s *SupabaseClient) unmarshalData(rawData interface{}, target interface{}) error {
	// Supabase returns JSON data, so we need to marshal it to JSON bytes first, then unmarshal
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %v", err)
	}

	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// GetUserCacheWithTimestamp retrieves cached data with timestamp for freshness checking
func (s *SupabaseClient) GetUserCacheWithTimestamp(userID, dataType string) (interface{}, *time.Time, *time.Time, error) {
	logger.Info("get_user_cache_timestamp", "Fetching user cache with timestamp", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("user_cache").
		Select("data, updated_at, expires_at", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_cache_timestamp", "Failed to fetch user cache", err, nil)
		return nil, nil, nil, err
	}

	if len(result) == 0 {
		logger.Info("get_user_cache_timestamp", "No cached data found", map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return nil, nil, nil, fmt.Errorf("no cached data found")
	}

	rawData := result[0]["data"]
	updatedAtStr := result[0]["updated_at"].(string)
	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		logger.Warn("get_user_cache_timestamp", "Failed to parse updated_at", map[string]interface{}{"error": err.Error()})
		// Still try to unmarshal data even if timestamp parsing fails
	}

	var expiresAt *time.Time
	if expiresRaw, exists := result[0]["expires_at"]; exists && expiresRaw != nil {
		if expiresStr, ok := expiresRaw.(string); ok && expiresStr != "" {
			if parsedExpires, parseErr := time.Parse(time.RFC3339, expiresStr); parseErr == nil {
				expiresAt = &parsedExpires
			} else {
				logger.Warn("get_user_cache_timestamp", "Failed to parse expires_at", map[string]interface{}{"error": parseErr.Error()})
			}
		}
	}

	// Unmarshal based on data type
	var unmarshaledData interface{}
	switch dataType {
	case "courses":
		var coursesData models.CoursesData
		if err := s.unmarshalData(rawData, &coursesData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal courses data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = coursesData
	case "timetable":
		var timetableData models.TimetableData
		if err := s.unmarshalData(rawData, &timetableData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal timetable data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = timetableData
	case "calendar":
		var calendarData models.CalendarData
		if err := s.unmarshalData(rawData, &calendarData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal calendar data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = calendarData
	case "user":
		var userInfo models.UserInfo
		if err := s.unmarshalData(rawData, &userInfo); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal user data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = userInfo
	default:
		logger.Warn("get_user_cache_timestamp", "Unknown data type, returning raw data", map[string]interface{}{
			"data_type": dataType,
		})
		unmarshaledData = rawData
	}

	logger.Info("get_user_cache_timestamp", "User cache found and unmarshaled", map[string]interface{}{
		"user_id":    userID,
		"data_type":  dataType,
		"updated_at": updatedAt,
		"expires_at": expiresAt,
	})
	return unmarshaledData, &updatedAt, expiresAt, nil
}

// isLoginAuthJobRequest is true for a login row that performs portal authentication (public.jobs:
// job_type=login, data_type=auth). Used to avoid blocking login enqueue while another job is still
// marked running for the same user.
func isLoginAuthJobRequest(req models.JobCreateRequest) bool {
	return req.JobType == "login" && req.DataType == "auth"
}

// userHasRunningJob reports whether this user already has a job in "running" status (any type).
func (s *SupabaseClient) userHasRunningJob(userID string) (bool, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("user_id", userID).
		Eq("status", "running").
		Limit(1, "").
		ExecuteTo(&rows)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// HasRecentSuccessfulLoginJob is true when a done login/auth row exists for this user whose
// created_at is after (now - within). We use created_at (always present on public.jobs) instead
// of completed_at so no extra migration is required; for fast-completing logins this matches
// "recent success" well. Optional migration supabase/migrations/*_jobs_completed_at.sql adds
// completed_at for stricter timing if you apply it and switch this filter.
func (s *SupabaseClient) HasRecentSuccessfulLoginJob(userID string, within time.Duration) (bool, error) {
	cutoff := time.Now().Add(-within).UTC().Format(time.RFC3339)
	var rows []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("user_id", userID).
		Eq("job_type", "login").
		Eq("data_type", "auth").
		Eq("status", "done").
		Gt("created_at", cutoff).
		Limit(1, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("has_recent_successful_login_job", "query public.jobs failed", err, map[string]interface{}{
			"user_id": userID,
			"cutoff":  cutoff,
		})
		return false, err
	}
	return len(rows) > 0, nil
}

// insertFailedLoginRateLimit persists a terminal failed login job when cooldown blocks a new pending login.
func (s *SupabaseClient) insertFailedLoginRateLimit(req models.JobCreateRequest) error {
	reason := "login rate limited: successful auth within last 10 minutes"
	src := req.JobSource
	if src == "" {
		src = models.JobSourceExternal
	}
	jobData := map[string]interface{}{
		"user_id":        req.UserID,
		"job_type":       "login",
		"data_type":      "auth",
		"status":         "failed",
		"priority":       req.Priority,
		"failure_reason": reason,
		"job_source":     src,
	}
	var ins []map[string]interface{}
	_, err := s.client.From("jobs").Insert(jobData, false, "", "", "").ExecuteTo(&ins)
	if err != nil {
		return err
	}
	if len(ins) == 0 {
		return fmt.Errorf("failed to insert rate-limited login job row")
	}
	return nil
}

// isJobConcurrencyViolation returns true when an update failed because another row is already running for the user
// (partial unique index jobs_one_running_per_user) or a similar uniqueness rule fired.
func isJobConcurrencyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique") ||
		strings.Contains(msg, "23505")
}

// EnqueueJob creates a new job with deduplication logic
// For login jobs, returns a WorkerLoginRequest that should be sent to the worker
func (s *SupabaseClient) EnqueueJob(req models.JobCreateRequest) (*models.Job, *models.WorkerLoginRequest, error) {
	logger.Info("enqueue_job", "Attempting to enqueue job", map[string]interface{}{
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
		"priority":  req.Priority,
	})

	// Do not enqueue non-login work while this user already has any job executing. Login+auth
	// requests are excluded: the current fetch job is still "running" until status is updated, so
	// gating login on userHasRunningJob would skip relogin while the failing fetch holds the row.
	if !isLoginAuthJobRequest(req) {
		hasRunning, err := s.userHasRunningJob(req.UserID)
		if err != nil {
			logger.Error("enqueue_job", "Failed to check running jobs for user", err, map[string]interface{}{
				"user_id": req.UserID,
			})
			return nil, nil, err
		}
		if hasRunning {
			logger.Info("enqueue_job", "User already has a running job, skipping enqueue", map[string]interface{}{
				"user_id": req.UserID,
			})
			return nil, nil, fmt.Errorf("job already exists")
		}
	}

	// Regular job handling for fetch jobs
	// Check for existing active job (deduplication)
	var existingResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id, status", "", false).
		Eq("user_id", req.UserID).
		Eq("job_type", req.JobType).
		Eq("data_type", req.DataType).
		In("status", []string{"pending", "running"}).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("enqueue_job", "Failed to check existing jobs", err, nil)
		return nil, nil, err
	}

	if len(existingResult) > 0 {
		logger.Info("enqueue_job", "Job already exists, skipping enqueue", map[string]interface{}{
			"user_id":         req.UserID,
			"job_type":        req.JobType,
			"data_type":       req.DataType,
			"existing_status": existingResult[0]["status"],
		})
		return nil, nil, fmt.Errorf("job already exists")
	}

	// Login rate limit before inserting a pending row (avoids racing the worker on the same row).
	if req.JobType == "login" && req.DataType == "auth" {
		recent, err := s.HasRecentSuccessfulLoginJob(req.UserID, LoginAuthCooldown)
		if err != nil {
			return nil, nil, err
		}
		if recent {
			if err := s.insertFailedLoginRateLimit(req); err != nil {
				logger.Error("enqueue_job", "failed to insert rate-limited login job record", err, map[string]interface{}{
					"user_id": req.UserID,
				})
				return nil, nil, err
			}
			logger.Info("enqueue_job", "Login rejected: recent successful auth for user (failed row inserted)", map[string]interface{}{
				"user_id": req.UserID,
			})
			return nil, nil, ErrLoginRateLimited
		}
	}

	jobSource := req.JobSource
	if jobSource == "" {
		jobSource = models.JobSourceInternal
	}

	// Insert new job
	jobData := map[string]interface{}{
		"user_id":    req.UserID,
		"job_type":   req.JobType,
		"data_type":  req.DataType,
		"status":     "pending",
		"priority":   req.Priority,
		"job_source": jobSource,
	}

	var result []map[string]interface{}
	_, err = s.client.From("jobs").Insert(jobData, false, "", "", "").ExecuteTo(&result)
	if err != nil {
		logger.Error("enqueue_job", "Failed to insert job", err, nil)
		return nil, nil, err
	}

	if len(result) == 0 {
		logger.Error("enqueue_job", "No job returned after insert", nil, nil)
		return nil, nil, fmt.Errorf("failed to create job")
	}

	job := &models.Job{
		ID:         result[0]["id"].(string),
		UserID:     req.UserID,
		JobType:    req.JobType,
		DataType:   req.DataType,
		Status:     "pending",
		Priority:   req.Priority,
		RetryCount: 0,
	}

	if createdAtStr, ok := result[0]["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	logger.Info("enqueue_job", "Job enqueued successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
	})
	return job, nil, nil
}

// CountRunningLoginJobs counts currently running login jobs (for global limit)
func (s *SupabaseClient) CountRunningLoginJobs() (int, error) {
	var result []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("job_type", "login").
		Eq("status", "running").
		ExecuteTo(&result)

	if err != nil {
		return 0, err
	}

	return len(result), nil
}

// CountPendingLoginJobs counts currently pending login jobs (queue soft gate).
func (s *SupabaseClient) CountPendingLoginJobs() (int, error) {
	var result []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("job_type", "login").
		Eq("status", "pending").
		ExecuteTo(&result)

	if err != nil {
		return 0, err
	}

	return len(result), nil
}

// ClaimNextJob atomically claims the next pending job for execution
func (s *SupabaseClient) ClaimNextJob() (*models.Job, error) {

	// First, find the next job to claim
	var jobsResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("*", "", false).
		Eq("status", "pending").
		Eq("job_type", "fetch").
		Neq("data_type", models.AttendanceDataType).
		Order("priority", &postgrest.OrderOpts{Ascending: false}).
		Order("created_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(1, "").
		ExecuteTo(&jobsResult)

	if err != nil {
		logger.Error("claim_next_job", "Failed to find next job", err, nil)
		return nil, err
	}

	if len(jobsResult) == 0 {
		// No pending jobs
		return nil, nil
	}

	jobData := jobsResult[0]
	jobID, err := requireStringField(jobData, "id")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: missing id", err, map[string]interface{}{
			"row": jobData,
		})
		return nil, err
	}

	// Atomically claim the job
	now := time.Now().Format(time.RFC3339)
	updateData := map[string]interface{}{
		"status":     "running",
		"started_at": now,
	}

	var updateResult []map[string]interface{}
	_, err = s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		Eq("status", "pending"). // Atomic check
		ExecuteTo(&updateResult)

	if err != nil {
		if isJobConcurrencyViolation(err) {
			logger.Info("claim_next_job", "Did not claim job: user already has a running job (concurrency)", map[string]interface{}{
				"job_id": jobID,
			})
			return nil, nil
		}
		logger.Error("claim_next_job", "Failed to claim job", err, nil)
		return nil, err
	}

	if len(updateResult) == 0 {
		// Another worker claimed it first
		logger.Info("claim_next_job", "Job was claimed by another worker", map[string]interface{}{
			"job_id": jobID,
		})
		return nil, nil
	}

	// Parse the claimed job
	userID, err := requireStringField(jobData, "user_id")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: missing user_id", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	jobType, err := requireStringField(jobData, "job_type")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: missing job_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	dataType, err := requireStringField(jobData, "data_type")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: missing data_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	priority, err := requireIntField(jobData, "priority")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: invalid priority", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	retryCount, err := requireIntField(jobData, "retry_count")
	if err != nil {
		logger.Error("claim_next_job", "Invalid job row: invalid retry_count", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}

	job := &models.Job{
		ID:         jobID,
		UserID:     userID,
		JobType:    jobType,
		DataType:   dataType,
		Status:     "running",
		Priority:   priority,
		RetryCount: retryCount,
	}

	if createdAtStr, ok := jobData["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	if startedAtStr, ok := jobData["started_at"].(string); ok {
		if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			job.StartedAt = &startedAt
		}
	}

	if failureReason, ok := jobData["failure_reason"].(string); ok {
		job.FailureReason = &failureReason
	}

	logger.Info("claim_next_job", "Job claimed successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})
	return job, nil
}

// ClaimNextAttendanceJob claims only attendance jobs for the dedicated scheduler worker.
func (s *SupabaseClient) ClaimNextAttendanceJob() (*models.Job, error) {

	var jobsResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("*", "", false).
		Eq("status", "pending").
		Eq("job_type", "fetch").
		Eq("data_type", models.AttendanceDataType).
		In("priority", []string{
			fmt.Sprintf("%d", models.JobPriorityLowest),
			"10",
		}).
		Order("created_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(1, "").
		ExecuteTo(&jobsResult)

	if err != nil {
		logger.Error("claim_attendance_job", "Failed to find next attendance job", err, nil)
		return nil, err
	}

	if len(jobsResult) == 0 {
		return nil, nil
	}

	jobData := jobsResult[0]
	jobID, err := requireStringField(jobData, "id")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: missing id", err, map[string]interface{}{
			"row": jobData,
		})
		return nil, err
	}

	now := time.Now().Format(time.RFC3339)
	updateData := map[string]interface{}{
		"status":     "running",
		"started_at": now,
	}

	var updateResult []map[string]interface{}
	_, err = s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		Eq("status", "pending").
		ExecuteTo(&updateResult)

	if err != nil {
		if isJobConcurrencyViolation(err) {
			logger.Info("claim_attendance_job", "Did not claim job: user already has a running job (concurrency)", map[string]interface{}{
				"job_id": jobID,
			})
			return nil, nil
		}
		logger.Error("claim_attendance_job", "Failed to claim attendance job", err, nil)
		return nil, err
	}

	if len(updateResult) == 0 {
		logger.Info("claim_attendance_job", "Attendance job claimed by another worker", map[string]interface{}{
			"job_id": jobID,
		})
		return nil, nil
	}

	userID, err := requireStringField(jobData, "user_id")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: missing user_id", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	jobType, err := requireStringField(jobData, "job_type")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: missing job_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	dataType, err := requireStringField(jobData, "data_type")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: missing data_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	priority, err := requireIntField(jobData, "priority")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: invalid priority", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	retryCount, err := requireIntField(jobData, "retry_count")
	if err != nil {
		logger.Error("claim_attendance_job", "Invalid attendance job row: invalid retry_count", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}

	job := &models.Job{
		ID:         jobID,
		UserID:     userID,
		JobType:    jobType,
		DataType:   dataType,
		Status:     "running",
		Priority:   priority,
		RetryCount: retryCount,
	}

	if createdAtStr, ok := jobData["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	if startedAtStr, ok := jobData["started_at"].(string); ok {
		if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			job.StartedAt = &startedAt
		}
	}

	if failureReason, ok := jobData["failure_reason"].(string); ok {
		job.FailureReason = &failureReason
	}

	logger.Info("claim_attendance_job", "Attendance job claimed successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})

	return job, nil
}

// ClaimNextLoginJob claims the next eligible pending login job, skipping users still in
// LoginAuthCooldown after a successful auth (same window as EnqueueJob rate limit).
func (s *SupabaseClient) ClaimNextLoginJob() (*models.Job, error) {
	const scanLimit = 40
	var jobsResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("*", "", false).
		Eq("status", "pending").
		Eq("job_type", "login").
		Order("priority", &postgrest.OrderOpts{Ascending: false}).
		Order("created_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(scanLimit, "").
		ExecuteTo(&jobsResult)
	if err != nil {
		logger.Error("claim_next_login_job", "Failed to find next login job", err, nil)
		return nil, err
	}
	if len(jobsResult) == 0 {
		return nil, nil
	}

	for _, jobData := range jobsResult {
		userID, err := requireStringField(jobData, "user_id")
		if err != nil {
			continue
		}
		inCooldown, err := s.HasRecentSuccessfulLoginJob(userID, LoginAuthCooldown)
		if err != nil {
			return nil, err
		}
		if inCooldown {
			logger.Info("claim_next_login_job", "Skipping login job: user in post-success auth cooldown", map[string]interface{}{
				"user_id": userID,
			})
			continue
		}

		jobID, err := requireStringField(jobData, "id")
		if err != nil {
			logger.Error("claim_next_login_job", "Invalid login job row: missing id", err, map[string]interface{}{
				"row": jobData,
			})
			continue
		}

		now := time.Now().Format(time.RFC3339)
		updateData := map[string]interface{}{
			"status":     "running",
			"started_at": now,
		}

		var updateResult []map[string]interface{}
		_, err = s.client.From("jobs").
			Update(updateData, "", "").
			Eq("id", jobID).
			Eq("status", "pending").
			ExecuteTo(&updateResult)
		if err != nil {
			if isJobConcurrencyViolation(err) {
				logger.Info("claim_next_login_job", "Did not claim job: user already has a running job (concurrency)", map[string]interface{}{
					"job_id": jobID,
				})
				continue
			}
			logger.Error("claim_next_login_job", "Failed to claim login job", err, nil)
			return nil, err
		}
		if len(updateResult) == 0 {
			logger.Info("claim_next_login_job", "Login job was claimed by another worker", map[string]interface{}{
				"job_id": jobID,
			})
			continue
		}

		return s.buildJobFromClaimedLoginRow(jobData, jobID)
	}

	return nil, nil
}

func (s *SupabaseClient) buildJobFromClaimedLoginRow(jobData map[string]interface{}, jobID string) (*models.Job, error) {
	userID, err := requireStringField(jobData, "user_id")
	if err != nil {
		logger.Error("claim_next_login_job", "Invalid login job row: missing user_id", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	jobType, err := requireStringField(jobData, "job_type")
	if err != nil {
		logger.Error("claim_next_login_job", "Invalid login job row: missing job_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	dataType, err := requireStringField(jobData, "data_type")
	if err != nil {
		logger.Error("claim_next_login_job", "Invalid login job row: missing data_type", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	priority, err := requireIntField(jobData, "priority")
	if err != nil {
		logger.Error("claim_next_login_job", "Invalid login job row: invalid priority", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}
	retryCount, err := requireIntField(jobData, "retry_count")
	if err != nil {
		logger.Error("claim_next_login_job", "Invalid login job row: invalid retry_count", err, map[string]interface{}{
			"job_id": jobID,
			"row":    jobData,
		})
		return nil, err
	}

	job := &models.Job{
		ID:         jobID,
		UserID:     userID,
		JobType:    jobType,
		DataType:   dataType,
		Status:     "running",
		Priority:   priority,
		RetryCount: retryCount,
	}

	if createdAtStr, ok := jobData["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}
	if startedAtStr, ok := jobData["started_at"].(string); ok {
		if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			job.StartedAt = &startedAt
		}
	}
	if failureReason, ok := jobData["failure_reason"].(string); ok {
		job.FailureReason = &failureReason
	}

	logger.Info("claim_next_login_job", "Login job claimed successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})
	return job, nil
}

// UpdateJobStatus updates job status, optional failure_reason, and optional failure_tokens (JSON array text).
// failureTokensJSON: nil = do not change failure_tokens; non-nil empty string = set NULL; non-nil non-empty = set JSON text.
// When status is "done", failure_tokens is always cleared (NULL).
func (s *SupabaseClient) UpdateJobStatus(jobID, status string, failureReason *string, failureTokensJSON *string) error {
	logFields := map[string]interface{}{
		"job_id": jobID,
		"status": status,
	}
	if failureReason != nil {
		logFields["failure_reason"] = *failureReason
	}
	if failureTokensJSON != nil {
		logFields["failure_tokens_set"] = *failureTokensJSON != ""
	}
	logger.Info("update_job_status", "Updating job status", logFields)

	updateData := map[string]interface{}{
		"status": status,
	}

	if failureReason != nil {
		updateData["failure_reason"] = *failureReason
	}
	if status == "done" {
		updateData["failure_tokens"] = nil
	} else if failureTokensJSON != nil {
		if *failureTokensJSON == "" {
			updateData["failure_tokens"] = nil
		} else {
			updateData["failure_tokens"] = *failureTokensJSON
		}
	}

	// Persist wall time for analytics: ms from started_at to now for terminal statuses.
	if status == "done" || status == "failed" {
		var startedRows []map[string]interface{}
		_, selErr := s.client.From("jobs").
			Select("started_at", "", false).
			Eq("id", jobID).
			Limit(1, "").
			ExecuteTo(&startedRows)
		if selErr == nil && len(startedRows) > 0 {
			if startedStr, ok := startedRows[0]["started_at"].(string); ok && startedStr != "" {
				if t0, perr := time.Parse(time.RFC3339, startedStr); perr == nil {
					ms := time.Since(t0).Milliseconds()
					if ms < 0 {
						ms = 0
					}
					updateData["duration"] = ms
				}
			}
		}
	}

	var result []map[string]interface{}
	_, err := s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("update_job_status", "Failed to update job status", err, logFields)
		return err
	}

	logger.Info("update_job_status", "Job status updated", map[string]interface{}{
		"job_id": jobID,
		"status": status,
	})
	return nil
}

// IncrementJobRetry increments retry count for a job
func (s *SupabaseClient) IncrementJobRetry(jobID string) error {
	logger.Info("increment_job_retry", "Incrementing job retry count", map[string]interface{}{
		"job_id": jobID,
	})

	var result []map[string]interface{}
	// Get current retry count
	var currentResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("retry_count", "", false).
		Eq("id", jobID).
		ExecuteTo(&currentResult)

	if err != nil || len(currentResult) == 0 {
		return err
	}

	currentRetry := int(currentResult[0]["retry_count"].(float64))

	_, err = s.client.From("jobs").
		Update(map[string]interface{}{
			"retry_count": currentRetry + 1,
		}, "", "").
		Eq("id", jobID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("increment_job_retry", "Failed to increment retry count", err, nil)
		return err
	}

	logger.Info("increment_job_retry", "Retry count incremented", map[string]interface{}{
		"job_id": jobID,
	})
	return nil
}

// EnqueueSpecificFetchJobs creates fetch jobs for only the requested data types after successful login
func (s *SupabaseClient) EnqueueSpecificFetchJobs(userID string, requestedDataTypes []string) error {
	logger.Info("enqueue_specific_fetch_jobs", "Enqueueing specific fetch jobs after login", map[string]interface{}{
		"user_id":              userID,
		"requested_data_types": requestedDataTypes,
	})

	successCount := 0

	for _, dataType := range requestedDataTypes {
		req := models.JobCreateRequest{
			UserID:    userID,
			JobType:   "fetch",
			DataType:  dataType,
			Priority:  10, // Fetch jobs are priority 10
			JobSource: models.JobSourceInternal,
		}

		_, _, err := s.EnqueueJob(req)
		if err != nil && err.Error() != "job already exists" {
			logger.Error("enqueue_specific_fetch_jobs", "Failed to enqueue fetch job", err, map[string]interface{}{
				"user_id":   userID,
				"data_type": dataType,
			})
			// Continue with other jobs even if one fails
		} else {
			successCount++
		}
	}

	logger.Info("enqueue_specific_fetch_jobs", "Specific fetch jobs enqueued", map[string]interface{}{
		"user_id":    userID,
		"requested":  len(requestedDataTypes),
		"successful": successCount,
		"failed":     len(requestedDataTypes) - successCount,
	})
	return nil
}

// EnqueueDependentFetchJobs creates fetch jobs after successful login (deprecated - use EnqueueSpecificFetchJobs)
func (s *SupabaseClient) EnqueueDependentFetchJobs(userID string) error {
	return s.EnqueueSpecificFetchJobs(userID, []string{"courses", "timetable", "calendar", "user"})
}

func requireStringField(row map[string]interface{}, key string) (string, error) {
	value, ok := row[key]
	if !ok || value == nil {
		return "", fmt.Errorf("field %s is missing or nil", key)
	}
	str, ok := value.(string)
	if !ok || strings.TrimSpace(str) == "" {
		return "", fmt.Errorf("field %s is not a non-empty string", key)
	}
	return str, nil
}

func requireIntField(row map[string]interface{}, key string) (int, error) {
	value, ok := row[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("field %s is missing or nil", key)
	}
	number, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("field %s is not numeric", key)
	}
	return int(number), nil
}
