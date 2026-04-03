package storage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"srm-academia-scraper/models"
)

// timetableModificationDoc matches the sparse JSON stored in public.timetable_modification.modified_json.
type timetableModificationDoc struct {
	Batch     string                 `json:"batch"`
	RegNumber string                 `json:"regNumber"`
	Schedule  []timetableModDayPatch `json:"schedule"`
}

type timetableModDayPatch struct {
	Day   int               `json:"day"`
	Table []json.RawMessage `json:"table"`
}

type timetablePatchCell struct {
	Type string               `json:"__type"`
	Data *models.TimetableSlot `json:"data"`
}

// ApplyTimetableModifications merges a sparse patch onto the generated timetable.
// JSON null or omitted table cells mean "leave portal value"; REMOVE clears the slot; ADD replaces the slot.
func ApplyTimetableModifications(base *models.TimetableData, raw []byte) error {
	if base == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var doc timetableModificationDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("unmarshal timetable modification: %w", err)
	}
	if len(doc.Schedule) == 0 {
		return nil
	}

	for _, dayPatch := range doc.Schedule {
		if dayPatch.Day < 1 || dayPatch.Day > 5 {
			continue
		}
		baseDayIdx := -1
		for i := range base.Schedule {
			if base.Schedule[i].Day == dayPatch.Day {
				baseDayIdx = i
				break
			}
		}
		if baseDayIdx < 0 {
			continue
		}
		dayRef := &base.Schedule[baseDayIdx]

		for j, cell := range dayPatch.Table {
			if cell == nil || isJSONNullRaw(cell) {
				continue
			}
			var p timetablePatchCell
			if err := json.Unmarshal(cell, &p); err != nil {
				continue
			}
			if j < 0 || j >= len(dayRef.Table) {
				continue
			}
			switch p.Type {
			case "REMOVE":
				dayRef.Table[j] = nil
			case "ADD":
				if p.Data == nil {
					continue
				}
				slot := *p.Data
				dayRef.Table[j] = &slot
			default:
				continue
			}
		}
	}
	return nil
}

func isJSONNullRaw(b json.RawMessage) bool {
	s := bytes.TrimSpace(b)
	return len(s) == 0 || string(s) == "null"
}
