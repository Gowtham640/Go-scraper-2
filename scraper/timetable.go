package scraper

import (
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"strings"
)

// Hardcoded timetable slot mappings for Batch 1 and Batch 2
var batch1Slots = [][]string{
	{"A", "A", "F", "F", "G", "P6", "P7", "P8", "P9", "P10"},        // Day 1
	{"P11", "P12", "P13", "P14", "P15", "B", "B", "G", "G", "A"},    // Day 2
	{"C", "C", "A", "D", "B", "P26", "P27", "P28", "P29", "P30"},    // Day 3
	{"P31", "P32", "P33", "P34", "P35", "D", "D", "B", "E", "C"},    // Day 4
	{"E", "E", "C", "F", "D", "P46", "P47", "P48", "P49", "P50"},    // Day 5
}

var batch2Slots = [][]string{
	{"P1", "P2", "P3", "P4", "P5", "A", "A", "F", "F", "G"},         // Day 1
	{"B", "B", "G", "G", "A", "P16", "P17", "P18", "P19", "P20"},    // Day 2
	{"P21", "P22", "P23", "P24", "P25", "C", "C", "A", "D", "B"},    // Day 3
	{"D", "D", "B", "E", "C", "P36", "P37", "P38", "P39", "P40"},    // Day 4
	{"P41", "P42", "P43", "P44", "P45", "E", "E", "C", "F", "D"},    // Day 5
}

// GenerateTimetable generates timetable from courses using hardcoded slot mappings
func GenerateTimetable(courses []models.Course, batch string) (*models.TimetableData, error) {
	logger.Info("generate_timetable", "Generating timetable", map[string]interface{}{
		"batch":        batch,
		"course_count": len(courses),
	})

	// Select slot mapping based on batch
	var slotMapping [][]string
	if batch == "1" {
		slotMapping = batch1Slots
	} else {
		slotMapping = batch2Slots
	}

	// Create course map for quick lookup
	courseMap := make(map[string]models.Course)
	for _, course := range courses {
		// Handle multi-slot courses (e.g., "P1-P2-" -> ["P1", "P2"])
		slots := parseSlots(course.Slot)
		for _, slot := range slots {
			courseMap[slot] = course
		}
	}

	// Generate timetable
	timetable := &models.TimetableData{
		Batch:    batch,
		Schedule: []models.TimetableDay{},
	}

	for dayNum, daySlots := range slotMapping {
		day := models.TimetableDay{
			Day:   dayNum + 1,
			Table: make([]*models.TimetableSlot, 10), // Initialize with 10 slots, all nil (null)
		}

		for slotIndex, slot := range daySlots {
			if course, found := courseMap[slot]; found {
				timetableSlot := &models.TimetableSlot{
					Code:       course.Code,
					Name:       course.Title,
					SlotType:   getSlotTypeFromSlot(course.Slot),
					Slot:       course.Slot,
					RoomNo:     course.Room,
					CourseType: course.Type,
					Online:     false,
					IsOptional: false,
				}
				day.Table[slotIndex] = timetableSlot
			}
			// If no course found for this slot, leave it as nil (null)
		}

		timetable.Schedule = append(timetable.Schedule, day)
	}

	logger.Info("generate_timetable", "Timetable generated successfully", map[string]interface{}{
		"days": len(timetable.Schedule),
	})
	return timetable, nil
}

// getSlotTypeFromSlot determines if a slot is Theory or Lab based on slot value
// If slot is in A,B,C,D,E,F,G then it is Theory, else it is Lab
func getSlotTypeFromSlot(slot string) string {
	// Clean the slot (remove suffixes like "-" from P1-P2-)
	slot = strings.TrimSuffix(slot, "-")

	// Check if it's a single letter A-G (Theory slots)
	if len(slot) == 1 {
		slotLetter := strings.ToUpper(slot)
		if slotLetter >= "A" && slotLetter <= "G" {
			return "Theory"
		}
	}

	// Everything else is Lab (P1, P2, etc.)
	return "Lab"
}

// parseSlots parses slot string into individual slots
// Examples: "A" -> ["A"], "P1-P2-" -> ["P1", "P2"], "A1+A2" -> ["A1", "A2"]
func parseSlots(slotStr string) []string {
	slotStr = strings.TrimSpace(slotStr)
	if slotStr == "" {
		return []string{}
	}

	// Handle different separators
	var slots []string

	// Split by '-'
	if strings.Contains(slotStr, "-") {
		parts := strings.Split(slotStr, "-")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				slots = append(slots, part)
			}
		}
		return slots
	}

	// Split by '+'
	if strings.Contains(slotStr, "+") {
		parts := strings.Split(slotStr, "+")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				slots = append(slots, part)
			}
		}
		return slots
	}

	// Single slot
	return []string{slotStr}
}
