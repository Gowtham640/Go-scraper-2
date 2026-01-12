package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

type LogLevel string

const (
	INFO  LogLevel = "INFO"
	ERROR LogLevel = "ERROR"
	WARN  LogLevel = "WARN"
	DEBUG LogLevel = "DEBUG"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "", 0)
}

// Log writes a human-readable log entry
func Log(level LogLevel, user, action, message string, data map[string]interface{}) {
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05")

	// Format user if present
	userStr := ""
	if user != "" {
		userStr = user + " "
	}

	// Format data if present
	dataStr := ""
	if data != nil && len(data) > 0 {
		var parts []string
		for key, value := range data {
			parts = append(parts, fmt.Sprintf("%s: %v", key, value))
		}
		dataStr = " (" + fmt.Sprintf("%s", parts) + ")"
	}

	// Format: [TIMESTAMP] LEVEL user action: message data
	logLine := fmt.Sprintf("[%s] %s %s%s: %s%s",
		timestamp, level, userStr, action, message, dataStr)

	logger.Println(logLine)
}

// Info logs an informational message
func Info(action, message string, data map[string]interface{}) {
	Log(INFO, "", action, message, data)
}

// InfoWithUser logs an informational message with user context
func InfoWithUser(user, action, message string, data map[string]interface{}) {
	Log(INFO, user, action, message, data)
}

// Error logs an error message
func Error(action, message string, err error, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	if err != nil {
		data["error"] = err.Error()
	}
	Log(ERROR, "", action, message, data)
}

// ErrorWithUser logs an error message with user context
func ErrorWithUser(user, action, message string, err error, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	if err != nil {
		data["error"] = err.Error()
	}
	Log(ERROR, user, action, message, data)
}

// Warn logs a warning message
func Warn(action, message string, data map[string]interface{}) {
	Log(WARN, "", action, message, data)
}

// WarnWithUser logs a warning message with user context
func WarnWithUser(user, action, message string, data map[string]interface{}) {
	Log(WARN, user, action, message, data)
}

// Debug logs a debug message
func Debug(action, message string, data map[string]interface{}) {
	Log(DEBUG, "", action, message, data)
}

// Fatal logs a fatal error and exits
func Fatal(action, message string, err error) {
	data := make(map[string]interface{})
	if err != nil {
		data["error"] = err.Error()
	}
	Log(ERROR, "", action, message, data)
	os.Exit(1)
}

// FormatError formats an error for logging
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
