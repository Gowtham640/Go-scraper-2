package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
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

var calendarMonthAbbrevs = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// GetUserSemesterAndProgramForCalendar loads semester and program for matching public.calendar rows.
func (s *SupabaseClient) GetUserSemesterAndProgramForCalendar(userID string) (semester int, program string, err error) {
	var rows []map[string]interface{}
	_, err = s.client.From("users").
		Select("semester,program", "", false).
		Eq("id", userID).
		Limit(1, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("attendance_calendar", "Failed to load user semester/program", err, map[string]interface{}{"user_id": userID})
		return 0, "", err
	}
	if len(rows) == 0 {
		return 0, "", fmt.Errorf("user not found")
	}
	semester = intFromInterface(rows[0]["semester"])
	program = strings.TrimSpace(safeString(rows[0]["program"]))
	logger.Info("attendance_calendar", "Loaded user calendar keys", map[string]interface{}{
		"user_id": userID, "semester": semester, "program": program,
	})
	return semester, program, nil
}

// GetTimetableDataFromUserCache returns parsed timetable JSON from public.user_cache (data_type=timetable).
func (s *SupabaseClient) GetTimetableDataFromUserCache(userID string) (*models.TimetableData, error) {
	raw, err := s.GetUserCache(userID, "timetable")
	if err != nil {
		logger.Error("attendance_timetable_cache", "GetUserCache timetable failed", err, map[string]interface{}{"user_id": userID})
		return nil, err
	}
	switch v := raw.(type) {
	case models.TimetableData:
		logger.Info("attendance_timetable_cache", "Timetable cache present", map[string]interface{}{
			"user_id": userID, "schedule_days": len(v.Schedule),
		})
		return &v, nil
	case *models.TimetableData:
		if v == nil {
			return nil, fmt.Errorf("timetable cache nil")
		}
		return v, nil
	case string:
		var td models.TimetableData
		if err := json.Unmarshal([]byte(v), &td); err != nil {
			logger.Error("attendance_timetable_cache", "Failed to unmarshal timetable string", err, map[string]interface{}{"user_id": userID})
			return nil, err
		}
		return &td, nil
	default:
		logger.Error("attendance_timetable_cache", "Unexpected timetable cache type", nil, map[string]interface{}{
			"user_id": userID, "type": fmt.Sprintf("%T", raw),
		})
		return nil, fmt.Errorf("unexpected timetable cache type %T", raw)
	}
}

// ResolveTimetableDayFromPublicCalendar uses public.calendar JSON to map onDate to academic day order (timetable day index).
func (s *SupabaseClient) ResolveTimetableDayFromPublicCalendar(userID string, onDate time.Time) (timetableDay int, dayOrderLabel string, err error) {
	semester, program, err := s.GetUserSemesterAndProgramForCalendar(userID)
	if err != nil {
		return 0, "", err
	}
	dataBytes, calCourse, err := s.fetchCalendarPayloadForUser(program, semester)
	if err != nil {
		return 0, "", err
	}
	dayOrderLabel, timetableDay, ok := timetableDayFromCalendarJSON(dataBytes, onDate)
	if !ok {
		logger.Warn("attendance_calendar", "No day_order for date in calendar payload", map[string]interface{}{
			"user_id": userID, "on_date": formatDate(onDate), "calendar_course": calCourse, "semester": semester,
		})
		return 0, "", fmt.Errorf("day order not found for %s", formatDate(onDate))
	}
	logger.Info("attendance_calendar", "Resolved timetable day from public.calendar", map[string]interface{}{
		"user_id": userID, "on_date": formatDate(onDate), "day_order": dayOrderLabel, "timetable_day": timetableDay,
		"calendar_course": calCourse, "semester": semester,
	})
	return timetableDay, dayOrderLabel, nil
}

func (s *SupabaseClient) fetchCalendarPayloadForUser(program string, semester int) (data json.RawMessage, courseUsed string, err error) {
	candidates := uniqueNonEmpty([]string{
		normalizeCalendarCourseKey(program),
		strings.TrimSpace(program),
		"BTech",
	})
	var lastErr error
	for _, courseKey := range candidates {
		var rows []map[string]interface{}
		_, qerr := s.client.From("calendar").
			Select("data,course", "", false).
			Eq("course", courseKey).
			Eq("semester", fmt.Sprintf("%d", semester)).
			Limit(1, "").
			ExecuteTo(&rows)
		if qerr != nil {
			lastErr = qerr
			logger.Warn("attendance_calendar", "calendar query failed", map[string]interface{}{"course": courseKey, "semester": semester, "error": qerr.Error()})
			continue
		}
		if len(rows) == 0 {
			continue
		}
		raw := rows[0]["data"]
		b, jerr := json.Marshal(raw)
		if jerr != nil {
			lastErr = jerr
			continue
		}
		logger.Info("attendance_calendar", "Loaded public.calendar row", map[string]interface{}{
			"course": courseKey, "semester": semester, "bytes": len(b),
		})
		return json.RawMessage(b), courseKey, nil
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("no calendar row for program=%q semester=%d", program, semester)
}

func normalizeCalendarCourseKey(program string) string {
	s := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(program), ".", ""), " ", ""))
	if strings.HasPrefix(s, "B") && strings.Contains(s, "TECH") {
		return "BTech"
	}
	return strings.TrimSpace(program)
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func timetableDayFromCalendarJSON(data json.RawMessage, onDate time.Time) (dayOrder string, timetableDay int, ok bool) {
	want := truncateAttendanceDate(onDate.UTC())

	var norm models.NormalizedCalendarData
	if err := json.Unmarshal(data, &norm); err == nil && len(norm.Calendar) > 0 {
		for _, e := range norm.Calendar {
			d, perr := parseNormalizedCalendarDate(e.Date)
			if perr != nil {
				continue
			}
			if truncateAttendanceDate(d).Equal(want) {
				dayOrder = strings.TrimSpace(e.DayOrder)
				timetableDay = parseDayOrderToTimetableIndex(dayOrder)
				return dayOrder, timetableDay, timetableDay > 0 && dayOrder != ""
			}
		}
	}

	var legacy models.CalendarData
	if err := json.Unmarshal(data, &legacy); err != nil || len(legacy.Calendar) == 0 {
		return "", 0, false
	}
	y := want.Year()
	for _, m := range legacy.Calendar {
		mon := monthAbbrevToMonth(m.Month)
		if mon == 0 {
			continue
		}
		for _, d := range m.Dates {
			t := time.Date(y, mon, d.Date, 0, 0, 0, 0, time.UTC)
			if truncateAttendanceDate(t).Equal(want) {
				dayOrder = strings.TrimSpace(d.DayOrder)
				timetableDay = parseDayOrderToTimetableIndex(dayOrder)
				return dayOrder, timetableDay, timetableDay > 0 && dayOrder != ""
			}
		}
	}
	return "", 0, false
}

func parseNormalizedCalendarDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	layouts := []string{"02/01/2006", "2/1/2006", "02/01/06", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsed calendar date %q", s)
}

func monthAbbrevToMonth(abbr string) time.Month {
	abbr = strings.TrimSpace(abbr)
	if len(abbr) >= 3 {
		key := strings.ToLower(abbr[:3])
		if m, ok := calendarMonthAbbrevs[key]; ok {
			return m
		}
	}
	if t, err := time.Parse("January", abbr); err == nil {
		return t.Month()
	}
	return 0
}

func parseDayOrderToTimetableIndex(dayOrder string) int {
	s := strings.TrimSpace(strings.ToLower(dayOrder))
	s = strings.TrimPrefix(s, "day")
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func truncateAttendanceDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ListUsersWithExpiredAttendanceCache returns up to limit users whose attendance cache is expired.
// Query source is public.user_cache with data_type='attendance' and expires_at < now.
func (s *SupabaseClient) ListUsersWithExpiredAttendanceCache(limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}

	nowUTC := time.Now().UTC().Format(time.RFC3339)
	var rows []map[string]interface{}
	_, err := s.client.From("user_cache").
		Select("user_id", "", false).
		Eq("data_type", models.AttendanceDataType).
		Lt("expires_at", nowUTC).
		Order("expires_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(limit, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("expired_attendance_cache_users", "Failed to query expired attendance cache users", err, map[string]interface{}{
			"limit":   limit,
			"now_utc": nowUTC,
		})
		return nil, err
	}

	userIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if userID, ok := row["user_id"].(string); ok && strings.TrimSpace(userID) != "" {
			userIDs = append(userIDs, userID)
		}
	}

	return userIDs, nil
}

// ListUsers returns the lightweight list of users that exist in the public.users table.
func (s *SupabaseClient) ListUsers() ([]models.UserRecord, error) {
	logger.Info("list_users", "STEP list_users_begin: paging public.users (id, email) for cron / scheduler", map[string]interface{}{
		"page_size": userListBatchSize,
	})

	offset := 0
	pageNum := 0
	allUsers := make([]models.UserRecord, 0)

	for {
		pageNum++
		logger.Info("list_users", "STEP list_users_page: requesting range from Supabase", map[string]interface{}{
			"page":        pageNum,
			"range_start": offset,
			"range_end":   offset + userListBatchSize - 1,
		})

		var rows []map[string]interface{}
		_, err := s.client.From("users").
			Select("id,email", "", false).
			Range(offset, offset+userListBatchSize-1, "").
			ExecuteTo(&rows)
		if err != nil {
			logger.Error("list_users", "STEP list_users_page FAILED: Supabase query error (aborting list)", err, map[string]interface{}{
				"page":        pageNum,
				"range_start": offset,
				"range_end":   offset + userListBatchSize - 1,
				"hint":        "verify URL, key, RLS on public.users, and network",
			})
			return nil, err
		}

		if len(rows) == 0 {
			logger.Info("list_users", "STEP list_users_page: empty page — end of table or no users", map[string]interface{}{
				"page":         pageNum,
				"offset":       offset,
				"users_so_far": len(allUsers),
				"terminal":     true,
			})
			break
		}

		logger.Info("list_users", "STEP list_users_page OK: rows received, validating id+email", map[string]interface{}{
			"page":                     pageNum,
			"row_count":                len(rows),
			"users_so_far_before_page": len(allUsers),
		})

		skippedBad := 0
		for rowIdx, row := range rows {
			id, idOk := row["id"].(string)
			email, emailOk := row["email"].(string)
			if !idOk || id == "" || !emailOk || email == "" {
				skippedBad++
				logger.Warn("list_users", "STEP list_users_row SKIP: row missing valid id or email (not added to cron user list)", map[string]interface{}{
					"page":           pageNum,
					"row_in_page":    rowIdx,
					"id_ok":          idOk,
					"id_empty":       id == "",
					"email_ok":       emailOk,
					"email_empty":    email == "",
					"raw_id_type":    fmt.Sprintf("%T", row["id"]),
					"raw_email_type": fmt.Sprintf("%T", row["email"]),
					"hint":           "public.users should have non-empty string id and email for attendance cron",
				})
				continue
			}
			allUsers = append(allUsers, models.UserRecord{
				ID:    id,
				Email: email,
			})
		}

		if skippedBad > 0 {
			logger.Warn("list_users", "STEP list_users_page: some rows skipped as invalid", map[string]interface{}{
				"page":        pageNum,
				"skipped_bad": skippedBad,
				"kept":        len(rows) - skippedBad,
			})
		}

		if len(rows) < userListBatchSize {
			logger.Info("list_users", "STEP list_users_page: partial page — last page of users", map[string]interface{}{
				"page":        pageNum,
				"row_count":   len(rows),
				"users_total": len(allUsers),
			})
			break
		}
		offset += len(rows)
	}

	logger.Info("list_users", "STEP list_users_complete: finished paging public.users", map[string]interface{}{
		"valid_user_records": len(allUsers),
		"pages_read":         pageNum,
	})
	return allUsers, nil
}

// HasRecentAttendanceFetchJob is true if a fetch/attendance row exists with created_at > since.
// Cron passes since = now - interval so a job created exactly one interval ago can be re-enqueued.
func (s *SupabaseClient) HasRecentAttendanceFetchJob(userID string, since time.Time) (bool, error) {
	var rows []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("user_id", userID).
		Eq("job_type", "fetch").
		Eq("data_type", models.AttendanceDataType).
		Gt("created_at", since.UTC().Format(time.RFC3339)).
		Limit(1, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("has_recent_attendance_job", "HasRecentAttendanceFetchJob FAILED: could not query public.jobs (cron idempotency check)", err, map[string]interface{}{
			"user_id":                  userID,
			"filter_job_type":          "fetch",
			"filter_data_type":         models.AttendanceDataType,
			"filter_created_after_utc": since.UTC().Format(time.RFC3339),
			"hint":                     "check RLS and service role access on public.jobs; verify created_at column exists",
		})
		return false, err
	}
	return len(rows) > 0, nil
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

	// postgrest ExecuteTo must decode into a slice; INSERT returns a JSON array.
	var insResult []map[string]interface{}
	_, err := s.client.From("attendance_snapshots").
		Insert(row, false, "", "", "").
		ExecuteTo(&insResult)
	if err != nil {
		logger.Error("insert_attendance_snapshot", "Failed to insert attendance snapshot", err, map[string]interface{}{
			"user_id": userID,
			"course":  entry.CourseCode,
		})
		return err
	}
	logger.Info("insert_attendance_snapshot", "Snapshot row accepted by API", map[string]interface{}{
		"user_id": userID,
		"course":  entry.CourseCode,
		"slot":    safeString(entry.Slot),
	})

	return nil
}

// GetAttendanceSnapshots fetches the most recent snapshots for a user-course pair, optionally scoped to one timetable slot.
// When slot is non-empty, only rows for that slot are considered (correct pairing for deltas).
func (s *SupabaseClient) GetAttendanceSnapshots(userID, courseCode, slot string, limit int) ([]models.AttendanceSnapshot, error) {
	var rows []map[string]interface{}
	q := s.client.From("attendance_snapshots").
		Select("*", "", false).
		Eq("userid", userID).
		Eq("coursecode", courseCode)
	if strings.TrimSpace(slot) != "" {
		q = q.Eq("slot", strings.TrimSpace(slot))
	}
	_, err := q.Order("fetchedat", &postgrest.OrderOpts{Ascending: false}).
		Limit(limit, "").
		ExecuteTo(&rows)
	if err != nil {
		logger.Error("get_attendance_snapshots", "Failed to fetch attendance snapshots", err, map[string]interface{}{
			"user_id": userID,
			"course":  courseCode,
			"slot":    slot,
			"limit":   limit,
		})
		return nil, err
	}

	logger.Info("get_attendance_snapshots", "Fetched snapshot rows", map[string]interface{}{
		"user_id":  userID,
		"course":   courseCode,
		"slot":     slot,
		"raw_rows": len(rows),
		"limit":    limit,
	})

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

	var insResult []map[string]interface{}
	_, err := s.client.From("attendance_deltas").
		Insert(row, false, "", "", "").
		ExecuteTo(&insResult)
	if err != nil {
		logger.Error("insert_attendance_delta", "Failed to insert attendance delta", err, map[string]interface{}{
			"user_id": userIDShortcut(delta.UserID),
			"course":  delta.CourseCode,
		})
		return err
	}
	logger.Info("insert_attendance_delta", "Delta row accepted by API", map[string]interface{}{
		"user_id":    delta.UserID,
		"course":     delta.CourseCode,
		"delta_type": delta.DeltaType,
	})

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
		"confidence_score":     event.ConfidenceScore,
		"is_ambiguous":         event.IsAmbiguous,
	}
	if strings.TrimSpace(event.SourceSnapshotRange) != "" {
		row["source_snapshot_range"] = strings.TrimSpace(event.SourceSnapshotRange)
	}

	var insResult []map[string]interface{}
	_, err := s.client.From("tentative_attendance_events").
		Insert(row, false, "", "", "").
		ExecuteTo(&insResult)
	if err != nil {
		logger.Error("insert_tentative_event", "Failed to insert tentative attendance event", err, map[string]interface{}{
			"user_id": event.UserID,
			"course":  event.CourseCode,
		})
		return err
	}
	logger.Info("insert_tentative_event", "Tentative row accepted by API", map[string]interface{}{
		"user_id":            event.UserID,
		"course":             event.CourseCode,
		"confidence_score":   event.ConfidenceScore,
		"is_ambiguous":       event.IsAmbiguous,
		"has_snapshot_range": event.SourceSnapshotRange != "",
	})

	return nil
}

// UpdateTentativeStatus updates the status flag for a tentative record.
func (s *SupabaseClient) UpdateTentativeStatus(id, status string) error {
	if id == "" {
		return fmt.Errorf("tentative id required")
	}

	var updResult []map[string]interface{}
	_, err := s.client.From("tentative_attendance_events").
		Update(map[string]interface{}{
			"status": status,
		}, "", "").
		Eq("id", id).
		ExecuteTo(&updResult)
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

	var insResult []map[string]interface{}
	_, err := s.client.From("final_attendance_events").
		Insert(row, false, "", "", "").
		ExecuteTo(&insResult)
	if err != nil {
		logger.Error("insert_final_attendance_event", "Failed to insert final record", err, map[string]interface{}{
			"user_id": event.UserID,
			"course":  event.CourseCode,
		})
		return err
	}
	logger.Info("insert_final_attendance_event", "Final attendance row accepted by API", map[string]interface{}{
		"user_id": event.UserID,
		"course":  event.CourseCode,
	})

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

func floatFromInterface(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return f
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

	ev := models.TentativeAttendanceEvent{
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
		ConfidenceScore:      floatFromInterface(getRowValue(row, "confidence_score", "confidenceScore")),
		IsAmbiguous:          boolFromInterface(getRowValue(row, "is_ambiguous", "isAmbiguous")),
		SourceSnapshotRange:  safeString(getRowValue(row, "source_snapshot_range", "sourceSnapshotRange")),
	}
	return ev, nil
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
