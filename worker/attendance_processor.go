package worker

import (
	"fmt"
	"math"
	"time"

	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

const (
	tentativeWindowPaddingDays = 4
	tentativeExpirationHours   = 24
)

type attendanceProcessor struct {
	db *storage.SupabaseClient
}

func newAttendanceProcessor(db *storage.SupabaseClient) *attendanceProcessor {
	return &attendanceProcessor{db: db}
}

func (p *attendanceProcessor) Process(jobID, userID string, entries []models.AttendanceEntry, fetchedAt time.Time) error {
	if len(entries) == 0 {
		logger.Info("attendance_processor", "No attendance entries to process", map[string]interface{}{
			"job_id":  jobID,
			"user_id": userID,
		})
		return nil
	}

	logger.Info("attendance_processor", "Pipeline start", map[string]interface{}{
		"job_id":     jobID,
		"user_id":    userID,
		"entry_count": len(entries),
		"fetched_at": fetchedAt.Format(time.RFC3339),
	})

	if err := p.ingestSnapshots(userID, entries, fetchedAt); err != nil {
		logger.Error("attendance_processor", "ingestSnapshots failed; deltas/tentatives skipped", err, map[string]interface{}{
			"job_id":  jobID,
			"user_id": userID,
		})
		return err
	}

	deltas, err := p.computeDeltas(jobID, userID, entries, fetchedAt)
	if err != nil {
		return err
	}

	if err := p.createTentatives(jobID, deltas, fetchedAt); err != nil {
		return err
	}

	if err := p.applyCorrections(jobID, userID, deltas, fetchedAt); err != nil {
		return err
	}

	if err := p.finalizeTentatives(jobID, fetchedAt); err != nil {
		return err
	}

	logger.Info("attendance_processor", "Attendance pipeline completed", map[string]interface{}{
		"job_id":   jobID,
		"user_id":  userID,
		"delta_ct": len(deltas),
		"entry_ct": len(entries),
	})

	return nil
}

func (p *attendanceProcessor) ingestSnapshots(userID string, entries []models.AttendanceEntry, fetchedAt time.Time) error {
	inserted := 0
	for i, entry := range entries {
		logger.Info("attendance_ingestion", "Inserting snapshot row", map[string]interface{}{
			"user_id": userID,
			"course":  entry.CourseCode,
			"slot":    entry.Slot,
			"index":   i,
		})
		if err := p.db.InsertAttendanceSnapshot(userID, entry, fetchedAt); err != nil {
			logger.Warn("attendance_ingestion", "Skipping snapshot due to insert error", map[string]interface{}{
				"user_id": userID,
				"course":  entry.CourseCode,
				"error":   err.Error(),
			})
			continue
		}
		inserted++
	}

	if inserted == 0 {
		logger.Error("attendance_ingestion", "No snapshots inserted for user", nil, map[string]interface{}{
			"user_id":     userID,
			"entry_total": len(entries),
		})
		return fmt.Errorf("no attendance snapshots inserted for user %s", userID)
	}

	logger.Info("attendance_ingestion", "Snapshots persisted", map[string]interface{}{
		"user_id": userID,
		"count":   inserted,
	})
	return nil
}

func (p *attendanceProcessor) computeDeltas(jobID, userID string, entries []models.AttendanceEntry, fetchedAt time.Time) ([]models.AttendanceDelta, error) {
	var deltas []models.AttendanceDelta
	for _, entry := range entries {
		snapshots, err := p.db.GetAttendanceSnapshots(userID, entry.CourseCode, entry.Slot, 2)
		if err != nil {
			logger.Error("attendance_delta", "GetAttendanceSnapshots failed", err, map[string]interface{}{
				"job_id":  jobID,
				"user_id": userID,
				"course":  entry.CourseCode,
				"slot":    entry.Slot,
			})
			return nil, err
		}
		if len(snapshots) < 2 {
			logger.Info("attendance_delta", "Skip delta: need two prior snapshots for this course+slot", map[string]interface{}{
				"job_id":         jobID,
				"user_id":        userID,
				"course":         entry.CourseCode,
				"slot":           entry.Slot,
				"snapshot_count": len(snapshots),
			})
			continue
		}

		curr := snapshots[0]
		prev := snapshots[1]
		deltaHoursConducted := curr.HoursConducted - prev.HoursConducted
		deltaHoursAbsent := curr.HoursAbsent - prev.HoursAbsent
		deltaHoursPresent := deltaHoursConducted - deltaHoursAbsent
		if deltaHoursPresent < 0 {
			deltaHoursPresent = 0
		}

		deltaType := determineDeltaType(deltaHoursConducted, deltaHoursAbsent)
		logger.Info("attendance_delta", "Compared latest two snapshots", map[string]interface{}{
			"job_id":               jobID,
			"user_id":              userID,
			"course":               entry.CourseCode,
			"slot":                 entry.Slot,
			"prev_conducted":       prev.HoursConducted,
			"prev_absent":          prev.HoursAbsent,
			"curr_conducted":       curr.HoursConducted,
			"curr_absent":          curr.HoursAbsent,
			"delta_conducted":      deltaHoursConducted,
			"delta_absent":         deltaHoursAbsent,
			"delta_present_inferred": deltaHoursPresent,
			"delta_type":           deltaType,
		})
		if deltaType == models.DeltaTypeNoChange {
			logger.Info("attendance_delta", "No delta row: classified as NO_CHANGE", map[string]interface{}{
				"job_id": jobID, "user_id": userID, "course": entry.CourseCode, "slot": entry.Slot,
			})
			continue
		}

		delta := models.AttendanceDelta{
			UserID:              userID,
			CourseCode:          entry.CourseCode,
			Slot:                entry.Slot,
			PrevHoursAbsent:     prev.HoursAbsent,
			PrevHoursConducted:  prev.HoursConducted,
			CurrHoursAbsent:     curr.HoursAbsent,
			CurrHoursConducted:  curr.HoursConducted,
			DeltaHoursAbsent:    deltaHoursAbsent,
			DeltaHoursConducted: deltaHoursConducted,
			DeltaHoursPresent:   deltaHoursPresent,
			DeltaType:           deltaType,
			ComputedAt:          fetchedAt,
		}

		if err := p.db.InsertAttendanceDelta(delta); err != nil {
			logger.Error("attendance_delta", "InsertAttendanceDelta failed", err, map[string]interface{}{
				"job_id": jobID, "user_id": userID, "course": entry.CourseCode,
			})
			return nil, err
		}

		deltas = append(deltas, delta)
	}

	logger.Info("attendance_delta", "Computed attendance deltas", map[string]interface{}{
		"job_id":   jobID,
		"user_id":  userID,
		"delta_ct": len(deltas),
	})

	return deltas, nil
}

func determineDeltaType(deltaConducted, deltaAbsent int) string {
	switch {
	case deltaConducted == 0 && deltaAbsent == 0:
		return models.DeltaTypeNoChange
	case deltaConducted == 0 && deltaAbsent != 0:
		return models.DeltaTypeCorrection
	case deltaConducted > 0 && deltaAbsent < 0:
		return models.DeltaTypeMixed
	case deltaConducted > 0:
		return models.DeltaTypeAddition
	default:
		return models.DeltaTypeNoChange
	}
}

func (p *attendanceProcessor) createTentatives(jobID string, deltas []models.AttendanceDelta, fetchedAt time.Time) error {
	if len(deltas) == 0 {
		logger.Info("tentative_creation", "No deltas to derive tentatives from", map[string]interface{}{
			"job_id": jobID,
		})
		return nil
	}

	start, end := candidateWindow(fetchedAt)
	created := 0
	for _, delta := range deltas {
		if delta.DeltaType != models.DeltaTypeAddition && delta.DeltaType != models.DeltaTypeMixed {
			logger.Info("tentative_creation", "Skip tentative: delta type not ADDITION/MIXED", map[string]interface{}{
				"job_id": jobID, "course": delta.CourseCode, "delta_type": delta.DeltaType,
			})
			continue
		}

		inferredAbsent := delta.DeltaHoursAbsent
		if inferredAbsent < 0 {
			inferredAbsent = 0
		}
		inferredPresent := delta.DeltaHoursPresent
		if inferredPresent < 0 {
			inferredPresent = 0
		}
		if inferredAbsent == 0 && inferredPresent == 0 {
			logger.Info("tentative_creation", "Skip tentative: zero inferred absent and present", map[string]interface{}{
				"job_id": jobID, "course": delta.CourseCode,
			})
			continue
		}

		schedule, schedErr := p.db.GetCourseSchedule(delta.CourseCode)
		if schedErr != nil {
			logger.Warn("tentative_creation", "Could not load course_schedule for timetable match log", map[string]interface{}{
				"job_id": jobID, "course": delta.CourseCode, "error": schedErr.Error(),
			})
		}
		todayH, yestH := expectedSlotHoursOnDays(schedule, delta.Slot, fetchedAt)
		matchToday := todayH > 0 && int(math.Round(todayH)) == delta.DeltaHoursConducted
		matchYest := yestH > 0 && int(math.Round(yestH)) == delta.DeltaHoursConducted
		confidence := 0.0
		if matchToday || matchYest {
			confidence = 1.0
		} else if todayH > 0 || yestH > 0 {
			ref := math.Max(todayH, yestH)
			if ref > 0 {
				confidence = math.Max(0, 1.0-math.Abs(float64(delta.DeltaHoursConducted)-ref)/ref)
			}
		}
		logger.Info("tentative_creation", "Timetable vs delta_conducted (log-only confidence)", map[string]interface{}{
			"job_id": jobID, "course": delta.CourseCode, "slot": delta.Slot,
			"delta_conducted": delta.DeltaHoursConducted, "expected_hours_today": todayH, "expected_hours_yesterday": yestH,
			"match_today": matchToday, "match_yesterday": matchYest, "confidence_estimate": confidence,
		})

		tentative := models.TentativeAttendanceEvent{
			UserID:               delta.UserID,
			CourseCode:           delta.CourseCode,
			InferredHoursAbsent:  inferredAbsent,
			InferredHoursPresent: inferredPresent,
			CandidateStartDate:   start,
			CandidateEndDate:     end,
			Status:               models.TentativeStatusPending,
			InferredAt:           fetchedAt,
			ExpiresAt:            fetchedAt.Add(time.Duration(tentativeExpirationHours) * time.Hour),
		}

		if err := p.db.InsertTentativeAttendanceEvent(tentative); err != nil {
			return err
		}
		created++
	}

	if created > 0 {
		logger.Info("tentative_creation", "Created tentative attendance events", map[string]interface{}{
			"job_id": jobID,
			"count":  created,
		})
	}

	return nil
}

func candidateWindow(fetchedAt time.Time) (time.Time, time.Time) {
	day := truncateToDate(fetchedAt)
	start := day.AddDate(0, 0, -tentativeWindowPaddingDays)
	end := day.AddDate(0, 0, tentativeWindowPaddingDays)
	return start, end
}

func truncateToDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (p *attendanceProcessor) applyCorrections(jobID, userID string, deltas []models.AttendanceDelta, now time.Time) error {
	for _, delta := range deltas {
		if delta.DeltaHoursAbsent >= 0 {
			continue
		}
		correctionHours := -delta.DeltaHoursAbsent
		if err := p.revertTentatives(jobID, userID, delta.CourseCode, correctionHours, now); err != nil {
			return err
		}
	}
	return nil
}

func (p *attendanceProcessor) revertTentatives(jobID, userID, courseCode string, correctionHours int, now time.Time) error {
	if correctionHours <= 0 {
		return nil
	}

	pending, err := p.db.GetPendingTentatives(userID, courseCode)
	if err != nil {
		return err
	}

	remaining := correctionHours
	for _, tentative := range pending {
		if remaining <= 0 {
			break
		}
		if tentative.InferredHoursAbsent <= 0 {
			continue
		}

		if tentative.InferredHoursAbsent <= remaining {
			remaining -= tentative.InferredHoursAbsent
			if err := p.db.UpdateTentativeStatus(tentative.ID, models.TentativeStatusReverted); err != nil {
				return err
			}
			logger.Info("correction_reversion", "Reverted entire tentative", map[string]interface{}{
				"tentative_id": tentative.ID,
				"user_id":      userID,
				"course":       courseCode,
			})
			continue
		}

		// Partial correction: revert original and re-create remaining portion
		remainingAbsent := tentative.InferredHoursAbsent - remaining
		if err := p.db.UpdateTentativeStatus(tentative.ID, models.TentativeStatusReverted); err != nil {
			return err
		}

		newTentative := models.TentativeAttendanceEvent{
			UserID:               tentative.UserID,
			CourseCode:           tentative.CourseCode,
			InferredHoursAbsent:  remainingAbsent,
			InferredHoursPresent: tentative.InferredHoursPresent,
			CandidateStartDate:   tentative.CandidateStartDate,
			CandidateEndDate:     tentative.CandidateEndDate,
			Status:               models.TentativeStatusPending,
			InferredAt:           now,
			ExpiresAt:            now.Add(time.Duration(tentativeExpirationHours) * time.Hour),
		}

		if err := p.db.InsertTentativeAttendanceEvent(newTentative); err != nil {
			return err
		}

		logger.Info("correction_reversion", "Partially reverted tentative and re-created remainder", map[string]interface{}{
			"user_id":          userID,
			"course":           courseCode,
			"remaining_absent": remainingAbsent,
		})

		remaining = 0
	}

	if remaining > 0 {
		logger.Warn("correction_reversion", "Unmatched correction hours remain", map[string]interface{}{
			"user_id":   userID,
			"course":    courseCode,
			"remaining": remaining,
		})
	}

	return nil
}

func (p *attendanceProcessor) finalizeTentatives(jobID string, now time.Time) error {
	tentatives, err := p.db.GetExpiredTentatives(now)
	if err != nil {
		return err
	}
	logger.Info("finalize_tentative", "Scanning tentatives past expiresAt for promotion to final", map[string]interface{}{
		"job_id": jobID, "candidate_count": len(tentatives), "now": now.Format(time.RFC3339),
	})

	for _, tentative := range tentatives {
		if tentative.InferredHoursAbsent == 0 && tentative.InferredHoursPresent == 0 {
			continue
		}

		conflict, err := p.db.HasCorrectionsAfter(tentative.UserID, tentative.CourseCode, tentative.InferredAt)
		if err != nil {
			return err
		}
		if conflict {
			logger.Info("finalize_tentative", "Skipping tentative due to later correction", map[string]interface{}{
				"tentative_id": tentative.ID,
				"user_id":      tentative.UserID,
				"course":       tentative.CourseCode,
			})
			continue
		}

		if err := p.confirmTentative(jobID, tentative, now); err != nil {
			return err
		}
	}

	return nil
}

func (p *attendanceProcessor) confirmTentative(jobID string, tentative models.TentativeAttendanceEvent, now time.Time) error {
	snapshots, err := p.db.GetAttendanceSnapshots(tentative.UserID, tentative.CourseCode, "", 1)
	if err != nil {
		return err
	}

	var metadata models.AttendanceSnapshot
	if len(snapshots) > 0 {
		metadata = snapshots[0]
	}

	classDate := p.assignClassDate(tentative, metadata)
	if classDate.IsZero() {
		classDate = truncateToDate(tentative.CandidateStartDate)
		if classDate.IsZero() {
			classDate = truncateToDate(now)
		}
	}

	event := models.FinalAttendanceEvent{
		UserID:       tentative.UserID,
		CourseCode:   tentative.CourseCode,
		CourseTitle:  metadata.CourseTitle,
		Category:     metadata.Category,
		ClassDate:    classDate,
		HoursAbsent:  tentative.InferredHoursAbsent,
		HoursPresent: tentative.InferredHoursPresent,
		FinalizedAt:  now,
	}

	if err := p.db.InsertFinalAttendanceEvent(event); err != nil {
		return err
	}

	if err := p.db.UpdateTentativeStatus(tentative.ID, models.TentativeStatusConfirmed); err != nil {
		return err
	}

	logger.Info("finalize_attendance", "Final attendance event recorded", map[string]interface{}{
		"tentative_id": tentative.ID,
		"job_id":       jobID,
		"user_id":      tentative.UserID,
		"course":       tentative.CourseCode,
		"class_date":   classDate.Format("2006-01-02"),
	})

	return nil
}

func (p *attendanceProcessor) assignClassDate(tentative models.TentativeAttendanceEvent, metadata models.AttendanceSnapshot) time.Time {
	start, end := normalizeRange(tentative.CandidateStartDate, tentative.CandidateEndDate)
	if start.IsZero() || end.IsZero() {
		now := time.Now().UTC()
		start = truncateToDate(now)
		end = start
	}

	schedule, _ := p.db.GetCourseSchedule(tentative.CourseCode)
	calendar, _ := p.db.GetAcademicCalendarEntries(start, end)
	usedDates, _ := p.db.GetFinalClassDates(tentative.UserID, tentative.CourseCode)

	slot := metadata.Slot
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		dateKey := formatDate(date)
		if _, alreadyUsed := usedDates[dateKey]; alreadyUsed {
			continue
		}
		if entry, found := calendar[dateKey]; found && entry.IsHoliday {
			continue
		}

		weekday := goWeekdayToInt(date.Weekday())
		for _, sched := range schedule {
			if slot != "" && sched.Slot != slot {
				continue
			}
			if sched.Weekday != weekday {
				continue
			}
			return date
		}
	}

	return time.Time{}
}

func normalizeRange(start, end time.Time) (time.Time, time.Time) {
	if start.IsZero() && end.IsZero() {
		now := time.Now().UTC()
		return truncateToDate(now), truncateToDate(now)
	}
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	if start.After(end) {
		start, end = end, start
	}
	return truncateToDate(start), truncateToDate(end)
}

func formatDate(value time.Time) string {
	return value.Format("2006-01-02")
}

func goWeekdayToInt(day time.Weekday) int {
	if day == time.Sunday {
		return 7
	}
	return int(day)
}

// expectedSlotHoursOnDays sums scheduled duration (hours) for the slot on ref day and previous day.
func expectedSlotHoursOnDays(schedule []models.CourseScheduleEntry, slot string, ref time.Time) (today float64, yesterday float64) {
	day := truncateToDate(ref)
	prev := day.AddDate(0, 0, -1)
	wToday := goWeekdayToInt(day.Weekday())
	wPrev := goWeekdayToInt(prev.Weekday())
	for _, e := range schedule {
		if slot != "" && e.Slot != slot {
			continue
		}
		h := durationHoursFromScheduleTimes(e.StartTime, e.EndTime)
		if e.Weekday == wToday {
			today += h
		}
		if e.Weekday == wPrev {
			yesterday += h
		}
	}
	return today, yesterday
}

func durationHoursFromScheduleTimes(start, end string) float64 {
	if start == "" || end == "" {
		return 0
	}
	t0, err0 := time.Parse("15:04:05", start)
	t1, err1 := time.Parse("15:04:05", end)
	if err0 != nil || err1 != nil {
		t0, err0 = time.Parse("15:04", start)
		t1, err1 = time.Parse("15:04", end)
	}
	if err0 != nil || err1 != nil {
		return 0
	}
	sec := t1.Sub(t0).Seconds()
	if sec <= 0 {
		return 0
	}
	return sec / 3600.0
}
