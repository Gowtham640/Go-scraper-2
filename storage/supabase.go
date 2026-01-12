package storage

import (
	"fmt"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"

	"github.com/google/uuid"
	supabase "github.com/supabase-community/supabase-go"
	"github.com/supabase-community/gotrue-go/types"
)

type SupabaseClient struct {
	client *supabase.Client
}

func NewSupabaseClient(url, key string) (*SupabaseClient, error) {
	logger.Info("supabase_init", "Initializing Supabase client", nil)
	
	client, err := supabase.NewClient(url, key, nil)
	if err != nil {
		logger.Error("supabase_init", "Failed to create Supabase client", err, nil)
		return nil, err
	}

	logger.Info("supabase_init", "Supabase client initialized successfully", nil)
	return &SupabaseClient{client: client}, nil
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
	logger.InfoWithUser(email, "upsert_user", "Upserting user data", nil)
	
	data := map[string]interface{}{
		"id":             userID,
		"email":          email,
		"role":           "public",
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

// UpsertToken stores or updates session tokens in public.tokens table
func (s *SupabaseClient) UpsertToken(userID, email, tokens string, expiryDays int) error {
	logger.InfoWithUser(email, "upsert_token", "Upserting token", map[string]interface{}{
		"expiry_days": expiryDays,
	})
	
	expiryTimestamp := time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour)
	
	data := map[string]interface{}{
		"user_id":          userID,
		"tokens":           tokens,
		"expiry_timestamp": expiryTimestamp.Format(time.RFC3339),
		"email":            email,
	}

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("tokens").Upsert(data, "", "", "").ExecuteTo(&result)
	if err != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to upsert token", err, nil)
		return err
	}

	logger.InfoWithUser(email, "upsert_token", "Token upserted successfully", nil)
	return nil
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

// UpsertCalendar stores calendar data in public.calendar table
func (s *SupabaseClient) UpsertCalendar(course string, semester int, data interface{}) error {
	logger.Info("upsert_calendar", "Upserting calendar", map[string]interface{}{
		"course":   course,
		"semester": semester,
	})

	// Check if record exists
	var existingResult []map[string]interface{}
	_, err := s.client.From("calendar").
		Select("id", "", false).
		Eq("course", course).
		Eq("semester", fmt.Sprintf("%d", semester)).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("upsert_calendar", "Failed to check existing calendar", err, nil)
		return err
	}

	var result []map[string]interface{}
	if len(existingResult) > 0 {
		// Update existing record
		updateData := map[string]interface{}{
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err = s.client.From("calendar").
			Update(updateData, "", "").
			Eq("course", course).
			Eq("semester", fmt.Sprintf("%d", semester)).
			ExecuteTo(&result)
	} else {
		// Insert new record
		insertData := map[string]interface{}{
			"id":         uuid.New().String(),
			"course":     course,
			"semester":   semester,
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err = s.client.From("calendar").Insert(insertData, false, "", "", "").ExecuteTo(&result)
	}

	if err != nil {
		logger.Error("upsert_calendar", "Failed to upsert calendar", err, nil)
		return err
	}

	logger.Info("upsert_calendar", "Calendar upserted successfully", nil)
	return nil
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

	batch := result[0]["batch"].(string)
	logger.Info("get_user_batch", "User batch found", map[string]interface{}{
		"user_id": userID,
		"batch":   batch,
	})
	return batch, nil
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

	data := result[0]["data"]
	logger.Info("get_user_cache", "User cache found", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})
	return data, nil
}