package scraper

import (
	"fmt"
	"html"
	"regexp"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DecodeHTMLEntities decodes HTML entities in the content
func DecodeHTMLEntities(htmlContent string) string {
	logger.Info("decode_html_entities", "Starting HTML entity decoding", nil)

	originalLength := len(htmlContent)
	decoded := html.UnescapeString(htmlContent)

	logger.Info("decode_html_entities", "HTML entity decoding completed", map[string]interface{}{
		"original_length": originalLength,
		"decoded_length": len(decoded),
		"bytes_added": len(decoded) - originalLength,
	})

	return decoded
}

// cleanEventText cleans up event text by removing common suffixes and formatting
func cleanEventText(event string) string {
	// Remove " - Holiday" suffix only at the end
	if strings.HasSuffix(event, " - Holiday") {
		event = strings.TrimSuffix(event, " - Holiday")
		event = event + " Holiday"
	}

	// Clean up specific cases - handle apostrophe encoding issues
	event = strings.ReplaceAll(event, "New Year's Day Holiday", "New Year Holiday")
	event = strings.ReplaceAll(event, "Telugu New Year's Day Holiday", "Telugu New Year Holiday")
	event = strings.ReplaceAll(event, "Tamil New Year's Day", "Tamil New Year Day")

	// Handle encoded apostrophes (???s)
	event = strings.ReplaceAll(event, "New Year???s Day Holiday", "New Year Holiday")
	event = strings.ReplaceAll(event, "Telugu New Year???s Day Holiday", "Telugu New Year Holiday")
	event = strings.ReplaceAll(event, "Tamil New Year???s Day", "Tamil New Year Day")

	event = strings.ReplaceAll(event, "Day - Holiday", "Day")

	// Fix spacing issues in program names
	event = strings.ReplaceAll(event, "B.Tech", "B.Tech ")
	event = strings.ReplaceAll(event, "M.Tech", "M.Tech ")
	event = strings.ReplaceAll(event, "(Int)", "(Int) ")
	event = strings.ReplaceAll(event, "(Year)", "(Year)")

	// Clean up other common patterns
	event = strings.ReplaceAll(event, " - ", " ")
	event = strings.ReplaceAll(event, " / ", " / ")

	// Remove extra spaces and trim
	event = strings.TrimSpace(event)

	// Remove multiple spaces
	for strings.Contains(event, "  ") {
		event = strings.ReplaceAll(event, "  ", " ")
	}

	return event
}

// CleanCalendarData cleans the parsed calendar data before storage
func CleanCalendarData(calendarData *models.CalendarData) {
	logger.Info("clean_calendar_data", "Starting calendar data cleaning", nil)

	// Clean event text in all dates (additional cleaning pass before storage)
	for i := range calendarData.Calendar {
		month := &calendarData.Calendar[i]
		for j := range month.Dates {
			date := &month.Dates[j]
			if date.Event != nil {
				cleanEvent := cleanEventText(*date.Event)
				date.Event = &cleanEvent
			}
		}
	}

	logger.Info("clean_calendar_data", "Calendar data cleaning completed", nil)
}

// cleanMalformedHTML fixes common HTML issues that prevent goquery from parsing
func cleanMalformedHTML(htmlContent string) string {
	logger.Info("clean_html", "Starting HTML cleaning process", map[string]interface{}{
		"original_length": len(htmlContent),
	})

	originalLength := len(htmlContent)

	// Remove excessive closing </b> tags (more than 2 in a row)
	bTagsRemoved := regexp.MustCompile(`</b>\s*</b>\s*</b>+`).ReplaceAllString(htmlContent, "</b></b>")
	if len(bTagsRemoved) != len(htmlContent) {
		logger.Info("clean_html", fmt.Sprintf("Removed excessive </b> tags (%d chars)", len(htmlContent)-len(bTagsRemoved)), nil)
		htmlContent = bTagsRemoved
	}

	// Remove excessive closing </div> tags (more than 10 in a row)
	divTagsRemoved := regexp.MustCompile(`</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>+`).ReplaceAllString(htmlContent, "</div></div></div></div></div></div></div></div></div></div>")
	if len(divTagsRemoved) != len(htmlContent) {
		logger.Info("clean_html", fmt.Sprintf("Removed excessive </div> tags (%d chars)", len(htmlContent)-len(divTagsRemoved)), nil)
		htmlContent = divTagsRemoved
	}

	// Fix incomplete script tags
	scriptFixed := regexp.MustCompile(`<script>[^{]*\{([^}]*)\}[^}]*$`).ReplaceAllString(htmlContent, "<script>$1}</script>")
	if len(scriptFixed) != len(htmlContent) {
		logger.Info("clean_html", "Fixed incomplete script tags", nil)
		htmlContent = scriptFixed
	}

	// Remove any remaining unmatched closing tags at the end
	endTagsRemoved := regexp.MustCompile(`</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*</[^>]+>\s*$`).ReplaceAllString(htmlContent, "")
	if len(endTagsRemoved) != len(htmlContent) {
		logger.Info("clean_html", fmt.Sprintf("Removed unmatched closing tags at end (%d chars)", len(htmlContent)-len(endTagsRemoved)), nil)
		htmlContent = endTagsRemoved
	}

	logger.Info("clean_html", "HTML cleaning completed", map[string]interface{}{
		"original_length": originalLength,
		"cleaned_length": len(htmlContent),
		"bytes_removed": originalLength - len(htmlContent),
	})

	return htmlContent
}

// extractCalendarTable extracts the calendar table directly from HTML using regex
func extractCalendarTable(htmlContent string) string {
	logger.Info("extract_calendar_table", "Attempting direct table extraction", nil)

	// Find the table with style="border-color:#b4bed1"
	tableRegex := regexp.MustCompile(`(?s)<table[^>]*style="[^"]*border-color:#b4bed1[^"]*"[^>]*>.*?</table>`)
	matches := tableRegex.FindStringSubmatch(htmlContent)

	if len(matches) > 0 {
		logger.Info("extract_calendar_table", fmt.Sprintf("Successfully extracted table with style border-color:#b4bed1 (length: %d)", len(matches[0])), nil)
		return matches[0]
	}
	logger.Warn("extract_calendar_table", "No table found with style border-color:#b4bed1 selector", nil)

	// Fallback: find any table containing the month headers
	fallbackRegex := regexp.MustCompile(`(?s)<table[^>]*>.*?Jan '26.*?</table>`)
	matches = fallbackRegex.FindStringSubmatch(htmlContent)

	if len(matches) > 0 {
		logger.Info("extract_calendar_table", fmt.Sprintf("Successfully extracted table with Jan '26 fallback (length: %d)", len(matches[0])), nil)
		return matches[0]
	}
	logger.Warn("extract_calendar_table", "No table found with Jan '26 fallback selector", nil)

	logger.Error("extract_calendar_table", "Direct table extraction completely failed", fmt.Errorf("no tables found with any selector"), nil)
	return ""
}

// ParseCalendar parses calendar data from HTML
func ParseCalendar(htmlContent string) (*models.CalendarData, error) {
	logger.Info("parse_calendar", "Starting to parse calendar", nil)

	var table *goquery.Selection

	// Try to extract the table directly first
	logger.Info("parse_calendar", "Attempting direct table extraction from raw HTML", nil)
	tableHTML := extractCalendarTable(htmlContent)
	if tableHTML == "" {
		logger.Warn("parse_calendar", "Direct table extraction failed, falling back to cleaned HTML approach", nil)

		// Fall back to cleaning approach
		logger.Info("parse_calendar", "Cleaning malformed HTML", nil)
		htmlContent = cleanMalformedHTML(htmlContent)

		logger.Info("parse_calendar", "Parsing cleaned HTML document", nil)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		if err != nil {
			logger.Error("parse_calendar", "Failed to parse cleaned HTML document", err, nil)
			return nil, err
		}

		logger.Info("parse_calendar", "Searching for table in cleaned HTML", nil)
		table = doc.Find(`table[style*="border-color:#b4bed1"]`).First()

		if table.Length() == 0 {
			logger.Warn("parse_calendar", "No tables found with border-color selector, trying any table selector", nil)
			table = doc.Find("table").First()
		}
	} else {
		logger.Info("parse_calendar", "Successfully extracted table directly, parsing extracted table HTML", nil)

		// Parse just the table HTML
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(tableHTML))
		if err != nil {
			logger.Error("parse_calendar", "Failed to parse extracted table", err, nil)
			return nil, err
		}
		table = doc.Find("table").First()
		logger.Info("parse_calendar", fmt.Sprintf("Found %d tables in extracted table HTML", table.Length()), nil)
	}

	calendarData := &models.CalendarData{
		Error:    false,
		Status:   200,
		Calendar: []models.CalendarMonth{},
		Index:    0,
	}

	// Check if table was found
	if table.Length() == 0 {
		logger.Error("parse_calendar", "Calendar table not found", fmt.Errorf("table not found"), nil)
		return nil, fmt.Errorf("calendar table not found")
	}

	logger.Info("parse_calendar", "Calendar table found successfully", nil)

	// Step 1: Identify month sections from header
	type MonthSection struct {
		Name     string
		StartCol int
	}

	var monthSections []MonthSection
	headerRow := table.Find("tr").First()
	headerCells := headerRow.Find("th")

	colIndex := 0
	for colIndex < headerCells.Length() {
		cell := headerCells.Eq(colIndex)
		cellText := strings.TrimSpace(cell.Text())

		// Look for month headers (contain '26 or '25)
		if strings.Contains(cellText, "'26") || strings.Contains(cellText, "'25") {
			// The actual data for this month starts 2 columns before the month header
			// Month header is at position N, data starts at position N-2
			startCol := colIndex - 2
			if startCol < 0 {
				startCol = 0
			}

			monthSections = append(monthSections, MonthSection{
				Name:     cellText,
				StartCol: startCol,
			})
		}

		colIndex++
	}

	logger.Info("parse_calendar", fmt.Sprintf("Found %d month sections", len(monthSections)), nil)

	// Step 2: Initialize month structures
	months := make([]models.CalendarMonth, len(monthSections))
	for i, section := range monthSections {
		months[i] = models.CalendarMonth{
			Month: section.Name,
			Dates: []models.CalendarDay{},
		}
	}

	// Step 3: Parse data rows (skip header row)
	table.Find("tr").Each(func(rowIndex int, row *goquery.Selection) {
		if rowIndex == 0 {
			return // Skip header row
		}

		cells := row.Find("td")
		if cells.Length() == 0 {
			return // Skip rows with no data cells
		}

		// Each data row represents the same date number across all months
		// Process each month section
		for monthIndex, section := range monthSections {
			startCol := section.StartCol

			// Ensure we have enough columns for this month section
			if startCol+3 >= cells.Length() {
				continue
			}

			// Extract data: Dt, Day, Event, DayOrder
			dateText := strings.TrimSpace(cells.Eq(startCol).Text())     // Dt column
			dayText := strings.TrimSpace(cells.Eq(startCol+1).Text())   // Day column
			eventText := strings.TrimSpace(cells.Eq(startCol+2).Text()) // Event column
			dayOrderText := strings.TrimSpace(cells.Eq(startCol+3).Text()) // DO column

			// Skip if date is empty or invalid
			if dateText == "" || dateText == "-" {
				continue
			}

			// Parse date number
			dateNum, err := strconv.Atoi(dateText)
			if err != nil {
				continue // Skip invalid dates
			}

			// Clean day order (treat "-" as valid data)
			if dayOrderText == "" {
				dayOrderText = "-"
			}

			// Create calendar day
			day := models.CalendarDay{
				Date:     dateNum,
				Day:      dayText,
				Event:    nil, // Default to null
				DayOrder: dayOrderText,
			}

			// Set event if it exists and is not empty/whitespace
			eventText = strings.TrimSpace(eventText)
			if eventText != "" {
				cleanEvent := cleanEventText(eventText)
				day.Event = &cleanEvent
			}

			// Add to the appropriate month
			months[monthIndex].Dates = append(months[monthIndex].Dates, day)
		}
	})

	// Step 4: Convert to CalendarData format and filter out empty months
	for _, month := range months {
		if len(month.Dates) > 0 {
			calendarData.Calendar = append(calendarData.Calendar, month)
		}
	}

	// Step 5: Set today and tomorrow (if available)
	now := time.Now()
	todayDate := now.Day()
	todayMonth := int(now.Month())
	todayYear := now.Year()

	tomorrow := now.Add(24 * time.Hour)
	tomorrowDate := tomorrow.Day()
	tomorrowMonth := int(tomorrow.Month())
	tomorrowYear := tomorrow.Year()

	for _, month := range calendarData.Calendar {
		monthName := extractMonthFromHeader(month.Month)
		year := extractYearFromHeader(month.Month)
		yearInt, _ := strconv.Atoi(year)
		monthInt := monthNameToNumber(monthName)

		if yearInt == todayYear && monthInt == todayMonth {
			for _, day := range month.Dates {
				if day.Date == todayDate {
					calendarData.Today = &day
					break
				}
			}
		}

		if yearInt == tomorrowYear && monthInt == tomorrowMonth {
			for _, day := range month.Dates {
				if day.Date == tomorrowDate {
					calendarData.Tomorrow = &day
					break
				}
			}
		}
	}

	logger.Info("parse_calendar", "Calendar parsed successfully", map[string]interface{}{
		"months": len(calendarData.Calendar),
		"total_dates": func() int {
			count := 0
			for _, month := range calendarData.Calendar {
				count += len(month.Dates)
			}
			return count
		}(),
	})
	return calendarData, nil
}

// extractMonthFromHeader extracts month number from header like "Jan '26"
func extractMonthFromHeader(header string) string {
	header = strings.ToLower(header)
	switch {
	case strings.Contains(header, "jan"):
		return "01"
	case strings.Contains(header, "feb"):
		return "02"
	case strings.Contains(header, "mar"):
		return "03"
	case strings.Contains(header, "apr"):
		return "04"
	case strings.Contains(header, "may"):
		return "05"
	case strings.Contains(header, "jun"):
		return "06"
	case strings.Contains(header, "jul"):
		return "07"
	case strings.Contains(header, "aug"):
		return "08"
	case strings.Contains(header, "sep"):
		return "09"
	case strings.Contains(header, "oct"):
		return "10"
	case strings.Contains(header, "nov"):
		return "11"
	case strings.Contains(header, "dec"):
		return "12"
	default:
		return "01"
	}
}

// NormalizeCalendarData converts nested calendar data to flat normalized structure
func NormalizeCalendarData(calendarData *models.CalendarData) *models.NormalizedCalendarData {
	logger.Info("normalize_calendar", "Starting calendar data normalization", nil)

	normalized := &models.NormalizedCalendarData{
		Calendar: []models.NormalizedCalendarEntry{},
	}

	// Process each month and flatten all dates
	for _, month := range calendarData.Calendar {
		monthName := month.Month
		year := extractYearFromHeader(monthName)
		monthNumStr := extractMonthFromHeader(monthName)
		monthNum, _ := strconv.Atoi(monthNumStr) // Convert string to int

		for _, day := range month.Dates {
			entry := models.NormalizedCalendarEntry{
				Date:     formatDate(day.Date, monthNum, year),
				DayName:  day.Day,
				Event:    day.Event,
				DayOrder: day.DayOrder,
				Month:    getMonthShortName(monthNum),
				Year:     parseYear(year),
			}
			normalized.Calendar = append(normalized.Calendar, entry)
		}
	}

	// Normalize today if present
	if calendarData.Today != nil {
		normalized.Today = normalizeCalendarDay(calendarData.Today)
	}

	// Normalize tomorrow if present
	if calendarData.Tomorrow != nil {
		normalized.Tomorrow = normalizeCalendarDay(calendarData.Tomorrow)
	}

	logger.Info("normalize_calendar", "Calendar data normalization completed", map[string]interface{}{
		"total_entries": len(normalized.Calendar),
		"has_today": normalized.Today != nil,
		"has_tomorrow": normalized.Tomorrow != nil,
	})

	return normalized
}

// normalizeCalendarDay converts a CalendarDay to NormalizedCalendarEntry
func normalizeCalendarDay(day *models.CalendarDay) *models.NormalizedCalendarEntry {
	if day == nil {
		return nil
	}

	// For today/tomorrow, we need to infer month/year from current date
	now := time.Now()
	monthName := getMonthShortName(int(now.Month()))
	year := now.Year()

	return &models.NormalizedCalendarEntry{
		Date:     formatDate(day.Date, int(now.Month()), strconv.Itoa(year)),
		DayName:  day.Day,
		Event:    day.Event,
		DayOrder: day.DayOrder,
		Month:    monthName,
		Year:     year,
	}
}

// formatDate creates DD/MM/YYYY string from day, month, year
func formatDate(day int, monthNum int, yearStr string) string {
	year := parseYear(yearStr)
	return fmt.Sprintf("%02d/%02d/%d", day, monthNum, year)
}

// parseYear converts year string to int
func parseYear(yearStr string) int {
	if year, err := strconv.Atoi(yearStr); err == nil {
		return year
	}
	// Fallback to current year
	return time.Now().Year()
}

// getMonthShortName returns 3-letter month name from month number
func getMonthShortName(monthNum int) string {
	months := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	if monthNum >= 1 && monthNum <= 12 {
		return months[monthNum]
	}
	return "Jan"
}

// extractYearFromHeader extracts year from header like "Jan '26"
func extractYearFromHeader(header string) string {
	if strings.Contains(header, "'26") {
		return "2026"
	} else if strings.Contains(header, "'25") {
		return "2025"
	}
	return "2026"
}

// padZero pads single digit with zero
func padZero(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// monthNameToNumber converts month name to integer
func monthNameToNumber(monthName string) int {
	switch strings.ToLower(monthName) {
	case "01", "jan":
		return 1
	case "02", "feb":
		return 2
	case "03", "mar":
		return 3
	case "04", "apr":
		return 4
	case "05", "may":
		return 5
	case "06", "jun":
		return 6
	case "07", "jul":
		return 7
	case "08", "aug":
		return 8
	case "09", "sep":
		return 9
	case "10", "oct":
		return 10
	case "11", "nov":
		return 11
	case "12", "dec":
		return 12
	default:
		return 1
	}
}

