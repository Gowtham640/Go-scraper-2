package storage

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"

	"github.com/google/uuid"
	"github.com/supabase-community/postgrest-go"
)

const (
	attendanceDateLayout = "2006-01-02"
	userListBatchSize    = 200
)

// ListUsers returns the lightweight list of users that exist in the public.users table.
func (s *SupabaseClient) ListUsers() ([]models.UserRecord, error) {
	logger.Info("list_users", "Listing all users for attendance scheduler", nil)

	offset := 0
	allUsers := make([]models.UserRecord, 0)

	for {
		var rows []map[string]interface{}
		_, err := s.client.From("users").
			Select("id,email", "", false).
			Range(offset, offset+userListBatchSize-1, "").
			ExecuteTo(&rows)
		if err != nil {
			logger.Error("list_users", "Failed to list users", err, map[string]interface{}{
				"offset": offset,
			})
			return nil, err
		}

		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			id, idOk := row["id"].(string)
			email, emailOk := row["email"].(string)
			if !idOk || id == "" || !emailOk || email == "" {
				continue
			}
			allUsers = append(allUsers, models.UserRecord{
				ID:    id,
				Email: email,
			})
		}

		if len(rows) < userListBatchSize {
			break
		}
		offset += len(rows)
	}

	logger.Info("list_users", fmt.Sprintf("Found %d user records", len(allUsers)), nil)
	return allUsers, nil
}

// InsertAttendanceSnapshot appends a new immutable attendance snapshot.
func (s *SupabaseClient) InsertAttendanceSnapshot(userID string, entry models.AttendanceEntry, fetchedAt time.Time) error {
	row := map[string]interface{}{
		"id":                   uuid.New().String(),
		"userid":               userID,
		"slot":                 safeString(entry.Slot),
		"faculty":              safeString(entry.Faculty),
		"category":             safeString(entry.Category),
		"coursecode":           safeString(entry.CourseCode),
		"coursetitle":          safeString(entry.CourseTitle),
		"hoursabsent":          roundFloatToInt(entry.HoursAbsent),
		"hoursconducted":       roundFloatToInt(entry.HoursConducted),
		"attendancepercentage": roundFloatToInt(entry.AttendancePercentage),
		"fetchedat":            fetchedAt.Format(time.RFC3339),
	}

	_, err := s.client.From("attendance_snapshots").
		Insert(row, false, "", "", "").
		ExecuteTo(&map[string]interface{}{})
	if err != nil {
		logger.Error("insert_attendance_snapshot", "Failed to insert attendance snapshot", err, map[string]interface{}{
			"user_id": userID,
			"course":  entry.CourseCode,
		})
		return err
	}

	return nil
}

// GetAttendanceSnapshots fetches the most recent snapshots for a user-course pair.
func (s *SupabaseClient) GetAttendanceSnapshots(userID, courseCode string, limit int) ([]models.AttendanceSnapshot, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("attendance_snapshots").
		Select("*", "", false).
		Eq("userid", userID).
		Eq("coursecode", courseCode).
		Order("fetchedat", &postgrest.OrderOpts{Ascending: false}).
		Limit(limit, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_attendance_snapshots", "Failed to fetch attendance snapshots", err, map[string]interface{}{
			"user_id": userID,
			"course":  courseCode,
			"limit":   limit,
		})
		return nil, err
	}

	snapshots := make([]models.AttendanceSnapshot, 0, len(rows))
	for _, row := range rows {
		if snapshot, parseErr := mapToAttendanceSnapshot(row); parseErr == nil {
			snapshots = append(snapshots, snapshot)
		} else {
			logger.Warn("get_attendance_snapshots", "Failed to parse attendance snapshot", map[string]interface{}{
				"user_id": userID,
				"course":  courseCode,
				"error":   parseErr.Error(),
			})
		}
	}

	return snapshots, nil
}

// InsertAttendanceDelta stores the computed delta between snapshot pairs.
func (s *SupabaseClient) InsertAttendanceDelta(delta models.AttendanceDelta) error {
	row := map[string]interface{}{
		"id":                  uuid.New().String(),
		"userid":              delta.UserID,
		"coursecode":          delta.CourseCode,
		"prevhoursabsent":     delta.PrevHoursAbsent,
		"prevhoursconducted":  delta.PrevHoursConducted,
		"currhoursabsent":     delta.CurrHoursAbsent,
		"currhoursconducted":  delta.CurrHoursConducted,
		"deltahoursabsent":    delta.DeltaHoursAbsent,
		"deltahoursconducted": delta.DeltaHoursConducted,
		"deltahourspresent":   delta.DeltaHoursPresent,
		"deltatype":           delta.DeltaType,
		"computedat":          delta.ComputedAt.Format(time.RFC3339),
	}

	_, err := s.client.From("attendance_deltas").
		Insert(row, false, "", "", "").
		ExecuteTo(&map[string]interface{}{})
	if err != nil {
		logger.Error("insert_attendance_delta", "Failed to insert attendance delta", err, map[string]interface{}{
			"user_id": userIDShortcut(delta.UserID),
			"course":  delta.CourseCode,
		})
		return err
	}

	return nil
}

// GetPendingTentatives returns pending tentative events for a course in LIFO order.
func (s *SupabaseClient) GetPendingTentatives(userID, courseCode string) ([]models.TentativeAttendanceEvent, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("tentative_attendance_events").
		Select("*", "", false).
		Eq("userid", userID).
		Eq("coursecode", courseCode).
		Eq("status", models.TentativeStatusPending).
		Order("inferredAt", &postgrest.OrderOpts{Ascending: false}).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_pending_tentatives", "Failed to fetch pendings", err, map[string]interface{}{
			"user_id": userID,
			"course":  courseCode,
		})
		return nil, err
	}

	result := make([]models.TentativeAttendanceEvent, 0, len(rows))
	for _, row := range rows {
		if tentative, parseErr := mapToTentativeEvent(row); parseErr == nil {
			result = append(result, tentative)
		} else {
			logger.Warn("get_pending_tentatives", "Failed to parse tentative event", map[string]interface{}{
				"user_id": userID,
				"course":  courseCode,
				"error":   parseErr.Error(),
			})
		}
	}

	return result, nil
}

// InsertTentativeAttendanceEvent stores a predicted attendance event that may later be confirmed.
func (s *SupabaseClient) InsertTentativeAttendanceEvent(event models.TentativeAttendanceEvent) error {
	row := map[string]interface{}{
		"id":                   uuid.New().String(),
		"userid":               event.UserID,
		"coursecode":           event.CourseCode,
		"inferredhoursabsent":  event.InferredHoursAbsent,
		"inferredhourspresent": event.InferredHoursPresent,
		"candidatestartdate":   formatDate(event.CandidateStartDate),
		"candidateenddate":     formatDate(event.CandidateEndDate),
		"status":               event.Status,
		"inferredat":           event.InferredAt.Format(time.RFC3339),
		"expiresat":            event.ExpiresAt.Format(time.RFC3339),
	}

	_, err := s.client.From("tentative_attendance_events").
		Insert(row, false, "", "", "").
		ExecuteTo(&map[string]interface{}{})
	if err != nil {
		logger.Error("insert_tentative_event", "Failed to insert tentative attendance event", err, map[string]interface{}{
			"user_id": event.UserID,
			"course":  event.CourseCode,
		})
		return err
	}

	return nil
}

// UpdateTentativeStatus updates the status flag for a tentative record.
func (s *SupabaseClient) UpdateTentativeStatus(id, status string) error {
	if id == "" {
		return fmt.Errorf("tentative id required")
	}

	_, err := s.client.From("tentative_attendance_events").
		Update(map[string]interface{}{
			"status": status,
		}, "", "").
		Eq("id", id).
		ExecuteTo(&map[string]interface{}{})
	if err != nil {
		logger.Error("update_tentative_status", "Failed to update tentative status", err, map[string]interface{}{
			"tentative_id": id,
			"status":       status,
		})
		return err
	}

	return nil
}

// GetExpiredTentatives fetches pending tentatives that reached their expiration window.
func (s *SupabaseClient) GetExpiredTentatives(now time.Time) ([]models.TentativeAttendanceEvent, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("tentative_attendance_events").
		Select("*", "", false).
		Eq("status", models.TentativeStatusPending).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_expired_tentatives", "Failed to fetch tentatives", err, nil)
		return nil, err
	}

	result := make([]models.TentativeAttendanceEvent, 0, len(rows))
	for _, row := range rows {
		if tentative, parseErr := mapToTentativeEvent(row); parseErr == nil {
			if tentative.ExpiresAt.IsZero() || now.After(tentative.ExpiresAt) || now.Equal(tentative.ExpiresAt) {
				result = append(result, tentative)
			}
		}
	}

	return result, nil
}

// HasCorrectionsAfter checks if a correction delta exists after the provided timestamp.
func (s *SupabaseClient) HasCorrectionsAfter(userID, courseCode string, after time.Time) (bool, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("attendance_deltas").
		Select("*", "", false).
		Eq("userid", userID).
		Eq("coursecode", courseCode).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("has_corrections_after", "Failed to fetch deltas for correction check", err, map[string]interface{}{
			"user_id": userID,
			"course":  courseCode,
		})
		return false, err
	}

	for _, row := range rows {
		delta, parseErr := mapToAttendanceDelta(row)
		if parseErr != nil {
			continue
		}
		if delta.ComputedAt.After(after) && delta.DeltaHoursAbsent < 0 {
			return true, nil
		}
	}

	return false, nil
}

// GetCourseSchedule returns schedule entries for the requested course.
func (s *SupabaseClient) GetCourseSchedule(courseCode string) ([]models.CourseScheduleEntry, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("course_schedule").
		Select("*", "", false).
		Eq("coursecode", courseCode).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_course_schedule", "Failed to fetch schedule", err, map[string]interface{}{
			"course": courseCode,
		})
		return nil, err
	}

	schedule := make([]models.CourseScheduleEntry, 0, len(rows))
	for _, row := range rows {
		weekday := intFromInterface(row["weekday"])
		schedule = append(schedule, models.CourseScheduleEntry{
			CourseCode: courseCode,
			Slot:       safeString(row["slot"]),
			Weekday:    weekday,
			StartTime:  safeString(row["startTime"]),
			EndTime:    safeString(row["endTime"]),
		})
	}

	return schedule, nil
}

// GetAcademicCalendarEntries returns calendar entries inside the window.
func (s *SupabaseClient) GetAcademicCalendarEntries(start, end time.Time) (map[string]models.AcademicCalendarEntry, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("academic_calendar").
		Select("*", "", false).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_academic_calendar", "Failed to fetch academic calendar", err, nil)
		return nil, err
	}

	calendar := make(map[string]models.AcademicCalendarEntry)
	for _, row := range rows {
		date, parseErr := dateFromInterface(row["date"])
		if parseErr != nil {
			continue
		}
		if start.IsZero() && end.IsZero() || (start.IsZero() || end.IsZero()) || (date.Equal(start) || date.Equal(end) || (date.After(start) && date.Before(end))) {
			calendar[formatDate(date)] = models.AcademicCalendarEntry{
				Date:        date,
				IsHoliday:   boolFromInterface(row["isHoliday"]),
				Description: stringPtrFromInterface(row["description"]),
			}
		}
	}

	return calendar, nil
}

// GetFinalClassDates returns dates already used for a user's course.
func (s *SupabaseClient) GetFinalClassDates(userID, courseCode string) (map[string]struct{}, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("final_attendance_events").
		Select("classDate", "", false).
		Eq("userid", userID).
		Eq("coursecode", courseCode).
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_final_class_dates", "Failed to fetch final events", err, map[string]interface{}{
			"user_id": userID,
			"course":  courseCode,
		})
		return nil, err
	}

	dates := make(map[string]struct{})
	for _, row := range rows {
		if classDate, parseErr := dateFromInterface(row["classDate"]); parseErr == nil {
			dates[formatDate(classDate)] = struct{}{}
		}
	}

	return dates, nil
}

// InsertFinalAttendanceEvent inserts a confirmed attendance record.
func (s *SupabaseClient) InsertFinalAttendanceEvent(event models.FinalAttendanceEvent) error {
	row := map[string]interface{}{
		"id":           uuid.New().String(),
		"userid":       event.UserID,
		"coursecode":   event.CourseCode,
		"coursetitle":  event.CourseTitle,
		"category":     event.Category,
		"classdate":    formatDate(event.ClassDate),
		"hoursabsent":  event.HoursAbsent,
		"hourspresent": event.HoursPresent,
		"finalizedat":  event.FinalizedAt.Format(time.RFC3339),
	}

	_, err := s.client.From("final_attendance_events").
		Insert(row, false, "", "", "").
		ExecuteTo(&map[string]interface{}{})
	if err != nil {
		logger.Error("insert_final_attendance_event", "Failed to insert final record", err, map[string]interface{}{
			"user_id": event.UserID,
			"course":  event.CourseCode,
		})
		return err
	}

	return nil
}

func safeString(value interface{}) string {
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}

func boolFromInterface(value interface{}) bool {
	if value == nil {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	if str, ok := value.(string); ok {
		lower := str
		if lower == "true" || lower == "t" || lower == "1" {
			return true
		}
	}
	return false
}

func dateFromInterface(value interface{}) (time.Time, error) {
	switch val := value.(type) {
	case time.Time:
		return val, nil
	case string:
		if val == "" {
			return time.Time{}, fmt.Errorf("empty date value")
		}
		return time.Parse(attendanceDateLayout, val)
	default:
		return time.Time{}, fmt.Errorf("unsupported date value: %v", value)
	}
}

func roundFloatToInt(value float64) int {
	return int(math.Round(value))
}

func intFromInterface(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(attendanceDateLayout)
}

func stringPtrFromInterface(value interface{}) *string {
	if str, ok := value.(string); ok && str != "" {
		return &str
	}
	return nil
}

func mapToAttendanceSnapshot(row map[string]interface{}) (models.AttendanceSnapshot, error) {
	fetchedAt, err := parseTimeField(getRowValue(row, "fetchedat", "fetchedAt"))
	if err != nil {
		return models.AttendanceSnapshot{}, err
	}

	createdAt, _ := parseTimeField(getRowValue(row, "createdat", "createdAt"))

	return models.AttendanceSnapshot{
		ID:                   safeString(row["id"]),
		UserID:               safeString(getRowValue(row, "userid", "userId")),
		Slot:                 safeString(row["slot"]),
		Faculty:              safeString(row["faculty"]),
		Category:             safeString(row["category"]),
		CourseCode:           safeString(getRowValue(row, "coursecode", "courseCode")),
		CourseTitle:          safeString(getRowValue(row, "coursetitle", "courseTitle")),
		HoursAbsent:          intFromInterface(getRowValue(row, "hoursabsent", "hoursAbsent")),
		HoursConducted:       intFromInterface(getRowValue(row, "hoursconducted", "hoursConducted")),
		AttendancePercentage: intFromInterface(getRowValue(row, "attendancepercentage", "attendancePercentage")),
		FetchedAt:            fetchedAt,
		CreatedAt:            createdAt,
	}, nil
}

func mapToTentativeEvent(row map[string]interface{}) (models.TentativeAttendanceEvent, error) {
	inferredAt, err := parseTimeField(getRowValue(row, "inferredat", "inferredAt"))
	if err != nil {
		return models.TentativeAttendanceEvent{}, err
	}
	expiresAt, err := parseTimeField(getRowValue(row, "expiresat", "expiresAt"))
	if err != nil {
		return models.TentativeAttendanceEvent{}, err
	}
	startDate, err := dateFromInterface(getRowValue(row, "candidatestartdate", "candidateStartDate"))
	if err != nil {
		startDate = time.Time{}
	}
	endDate, err := dateFromInterface(getRowValue(row, "candidateenddate", "candidateEndDate"))
	if err != nil {
		endDate = time.Time{}
	}

	return models.TentativeAttendanceEvent{
		ID:                   safeString(row["id"]),
		UserID:               safeString(getRowValue(row, "userid", "userId")),
		CourseCode:           safeString(getRowValue(row, "coursecode", "courseCode")),
		InferredHoursAbsent:  intFromInterface(getRowValue(row, "inferredhoursabsent", "inferredHoursAbsent")),
		InferredHoursPresent: intFromInterface(getRowValue(row, "inferredhourspresent", "inferredHoursPresent")),
		CandidateStartDate:   startDate,
		CandidateEndDate:     endDate,
		Status:               safeString(row["status"]),
		InferredAt:           inferredAt,
		ExpiresAt:            expiresAt,
	}, nil
}

func mapToAttendanceDelta(row map[string]interface{}) (models.AttendanceDelta, error) {
	computedAt, err := parseTimeField(getRowValue(row, "computedat", "computedAt"))
	if err != nil {
		return models.AttendanceDelta{}, err
	}
	return models.AttendanceDelta{
		ID:                  safeString(row["id"]),
		UserID:              safeString(getRowValue(row, "userid", "userId")),
		CourseCode:          safeString(getRowValue(row, "coursecode", "courseCode")),
		PrevHoursAbsent:     intFromInterface(getRowValue(row, "prevhoursabsent", "prevHoursAbsent")),
		PrevHoursConducted:  intFromInterface(getRowValue(row, "prevhoursconducted", "prevHoursConducted")),
		CurrHoursAbsent:     intFromInterface(getRowValue(row, "currhoursabsent", "currHoursAbsent")),
		CurrHoursConducted:  intFromInterface(getRowValue(row, "currhoursconducted", "currHoursConducted")),
		DeltaHoursAbsent:    intFromInterface(getRowValue(row, "deltahoursabsent", "deltaHoursAbsent")),
		DeltaHoursConducted: intFromInterface(getRowValue(row, "deltahoursconducted", "deltaHoursConducted")),
		DeltaHoursPresent:   intFromInterface(getRowValue(row, "deltahourspresent", "deltaHoursPresent")),
		DeltaType:           safeString(getRowValue(row, "deltatype", "deltaType")),
		ComputedAt:          computedAt,
	}, nil
}

func parseTimeField(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		if v == "" {
			return time.Time{}, fmt.Errorf("empty timestamp")
		}
		return time.Parse(time.RFC3339, v)
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp: %v", value)
	}
}

func userIDShortcut(userID string) string {
	if userID == "" {
		return "unknown"
	}
	return userID
}

func getRowValue(row map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if val, ok := row[key]; ok {
			return val
		}
	}
	return nil
}
