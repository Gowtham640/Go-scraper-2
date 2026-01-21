package scraper

import (
	"fmt"
	"regexp"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// STEP 1: Extract the argument of pageSanitizer.sanitize()
func extractPageSanitizerContent(htmlContent string) (string, error) {
	logger.Info("extract_page_sanitizer", "Extracting pageSanitizer.sanitize() content", nil)

	// Use regex with DOTALL mode to capture everything inside pageSanitizer.sanitize('...')
	sanitizerRegex := regexp.MustCompile(`(?s)pageSanitizer\.sanitize\(\s*['"](.*?)['"]\s*\)`)
	matches := sanitizerRegex.FindStringSubmatch(htmlContent)

	if len(matches) < 2 {
		logger.Error("extract_page_sanitizer", "Could not find pageSanitizer.sanitize() call", nil, nil)
		return "", fmt.Errorf("pageSanitizer.sanitize() call not found")
	}

	content := matches[1]
	logger.Info("extract_page_sanitizer", fmt.Sprintf("Extracted content length: %d characters", len(content)), nil)

	// Success criteria checks
	if len(content) <= 3000 {
		logger.Warn("extract_page_sanitizer", "Content length is suspiciously short", map[string]interface{}{
			"length": len(content),
		})
	}

	if !strings.Contains(content, "\\x3Ctable") {
		logger.Warn("extract_page_sanitizer", "Content does not contain expected \\x3Ctable escape sequence", nil)
	}

	if !strings.Contains(content, "\\x3C") || !strings.Contains(content, "\\n") {
		logger.Warn("extract_page_sanitizer", "Content does not contain expected escape sequences", nil)
	}

	logger.Info("extract_page_sanitizer", "Successfully extracted pageSanitizer content", nil)
	return content, nil
}

// STEP 2: Unescape the JS string into real HTML
func unescapeJavaScriptString(jsString string) (string, error) {
	logger.Info("unescape_js_string", "Unescaping JavaScript string", map[string]interface{}{
		"input_length": len(jsString),
	})

	// Use Go's strconv.Unquote to properly unescape JavaScript string literals
	// Add surrounding quotes to make it a valid Go string literal
	quotedString := `"` + jsString + `"`

	unescaped, err := strconv.Unquote(quotedString)
	if err != nil {
		logger.Error("unescape_js_string", "Failed to unquote JavaScript string", err, nil)
		return "", fmt.Errorf("failed to unquote JavaScript string: %v", err)
	}

	logger.Info("unescape_js_string", fmt.Sprintf("Unescaped HTML length: %d characters", len(unescaped)), nil)

	// Success criteria checks
	if len(unescaped) >= len(jsString) {
		logger.Warn("unescape_js_string", "HTML length did not shrink as expected", map[string]interface{}{
			"original": len(jsString),
			"unescaped": len(unescaped),
		})
	}

	if !strings.Contains(unescaped, "<table") {
		logger.Error("unescape_js_string", "Unescaped content does not contain <table>", nil, nil)
		return "", fmt.Errorf("unescaped content does not contain table")
	}

	if !strings.Contains(unescaped, "<tr>") || !strings.Contains(unescaped, "<td>") {
		logger.Warn("unescape_js_string", "Unescaped content missing expected HTML tags", nil)
	}

	logger.Info("unescape_js_string", "Successfully unescaped JavaScript string to HTML", nil)
	return unescaped, nil
}

// STEP 3: Strip non-table noise (light cleanup)
func stripNonTableNoise(html string) string {
	logger.Info("strip_noise", "Stripping non-table noise", map[string]interface{}{
		"input_length": len(html),
	})

	// Remove <style> blocks
	styleRegex := regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
	html = styleRegex.ReplaceAllString(html, "")

	// Remove <script> blocks (though they shouldn't exist after unescaping)
	scriptRegex := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)
	html = scriptRegex.ReplaceAllString(html, "")

	// Trim whitespace
	html = strings.TrimSpace(html)

	logger.Info("strip_noise", fmt.Sprintf("Cleaned HTML length: %d characters", len(html)), nil)

	// Success criteria checks
	if !strings.HasPrefix(html, "<table") && !strings.HasPrefix(html, "<div>") {
		logger.Warn("strip_noise", "HTML does not start with expected tags", nil)
	}

	logger.Info("strip_noise", "Successfully stripped non-table noise", nil)
	return html
}

// STEP 5: Handle malformed rows by iterating <td> in chunks of 11
func handleMalformedRows(row *goquery.Selection) []models.Course {
	var courses []models.Course

	// Collect all <td> elements from this malformed row
	allCells := row.Find("td")
	totalCells := allCells.Length()

	logger.Info("handle_malformed_rows", fmt.Sprintf("Processing malformed row with %d total cells", totalCells), nil)

	// Process in chunks of 11 cells (skip the first cell which is serial number)
	for i := 0; i < totalCells; i += 11 {
		end := i + 11
		if end > totalCells {
			break // Not enough cells for a complete course
		}

		// Extract 11 cells for this course (skip serial number at index 0)
		cells := make([]string, 10) // 10 fields (excluding serial number)
		validCells := 0

		for j := 1; j < 11 && (i+j) < totalCells; j++ { // Start from 1 to skip serial number
			cellText := allCells.Eq(i + j).Text()
			cells[j-1] = strings.TrimSpace(cellText)
			if cells[j-1] != "" {
				validCells++
			}
		}

		if validCells >= 8 { // Require at least 8 meaningful fields
			course := parseCourseFromCellTexts(cells)
			if course != nil {
				courses = append(courses, *course)
			}
		}
	}

	logger.Info("handle_malformed_rows", fmt.Sprintf("Extracted %d courses from malformed row", len(courses)), nil)
	return courses
}

// ParseCourses parses course information from HTML following the 6-step approach
func ParseCourses(htmlContent string, regNumber string) (*models.CoursesData, error) {
	logger.Info("parse_courses", "Starting to parse courses", nil)

	coursesData := &models.CoursesData{
		RegNumber: regNumber,
		Courses:   []models.Course{},
		Status:    200,
	}

	// STEP 1: Extract the argument of pageSanitizer.sanitize()
	sanitizedContent, err := extractPageSanitizerContent(htmlContent)
	if err != nil {
		logger.Error("parse_courses", "Failed to extract pageSanitizer content", err, nil)
		return coursesData, err
	}

	// STEP 2: Unescape the JS string into real HTML
	html, err := unescapeJavaScriptString(sanitizedContent)
	if err != nil {
		logger.Error("parse_courses", "Failed to unescape JavaScript string", err, nil)
		return coursesData, err
	}

	// STEP 3: Strip non-table noise (light cleanup)
	html = stripNonTableNoise(html)

	// STEP 4: Parse with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		logger.Error("parse_courses", "Failed to parse HTML with goquery", err, nil)
		return coursesData, err
	}

	rows := doc.Find("table.course_tbl tr")
	logger.Info("parse_courses", fmt.Sprintf("Found %d table rows using goquery selector", rows.Length()), nil)

	if rows.Length() <= 1 {
		logger.Error("parse_courses", "No data rows found in course table", nil, nil)
		return coursesData, fmt.Errorf("no data rows found in course table")
	}

	// STEP 5: Handle malformed rows by iterating <td> in chunks of 11
	var courses []models.Course
	currentRow := 0

	rows.Each(func(i int, row *goquery.Selection) {
		if i == 0 {
			return // Skip header row
		}

		cells := row.Find("td")
		if cells.Length() >= 11 {
			// Normal row with proper cells
			course := parseCourseRow(cells)
			if course != nil {
				courses = append(courses, *course)
				logger.Info("parse_courses", fmt.Sprintf("Parsed course from row %d: %s (%s)", currentRow, course.Code, course.Title), nil)
			}
		} else {
			// Handle malformed rows - iterate all <td> in chunks of 11
			logger.Info("parse_courses", fmt.Sprintf("Row %d has only %d cells, trying malformed row handling", i, cells.Length()), nil)
			malformedCourses := handleMalformedRows(row)
			for _, course := range malformedCourses {
				courses = append(courses, course)
				logger.Info("parse_courses", fmt.Sprintf("Parsed course from malformed row: %s (%s)", course.Code, course.Title), nil)
			}
		}
		currentRow++
	})

	// STEP 6: Map to model (already handled above)
	coursesData.Courses = courses
	logger.Info("parse_courses", fmt.Sprintf("Successfully extracted %d courses", len(courses)), nil)
	return coursesData, nil
}

// getCourseCategoryCode returns a short code for course category
func getCourseCategoryCode(category string) string {
	category = strings.ToLower(category)
	switch {
	case strings.Contains(category, "professional core"):
		return "PCC"
	case strings.Contains(category, "professional elective"):
		return "PEC"
	case strings.Contains(category, "basic science"):
		return "BSC"
	case strings.Contains(category, "humanities"):
		return "HSS"
	case strings.Contains(category, "engineering science"):
		return "ESC"
	default:
		return "OTH"
	}
}


// parseCourseFromCellTexts parses course from cell text array
func parseCourseFromCellTexts(cells []string) *models.Course {
	if len(cells) < 10 {
		return nil
	}

	// Index mapping for the 10 fields (serial number already skipped)
	// 0=Code, 1=Title, 2=Credit, 3=Category, 4=CourseCategory, 5=Type, 6=Faculty, 7=Slot, 8=Room, 9=AcademicYear
	code := strings.TrimSpace(cells[0])
	title := strings.TrimSpace(cells[1])
	credit := strings.TrimSpace(cells[2])
	category := strings.TrimSpace(cells[3])
	courseCategory := strings.TrimSpace(cells[4])
	courseType := strings.TrimSpace(cells[5])
	faculty := strings.TrimSpace(cells[6])
	slot := strings.TrimSpace(cells[7])
	room := strings.TrimSpace(cells[8])
	academicYear := strings.TrimSpace(cells[9])

	logger.Info("parse_course_manual", fmt.Sprintf("Raw data - Code: '%s', Title: '%s', Slot: '%s'", code, title, slot), nil)

	// Handle empty values
	if credit == "" {
		credit = "N/A"
	}
	if courseType == "" {
		courseType = "N/A"
	}
	if faculty == "" {
		faculty = "N/A"
	}
	if room == "" {
		room = "N/A"
	} else {
		// Capitalize first letter
		room = strings.ToUpper(room[:1]) + room[1:]
	}

	slot = strings.TrimSuffix(slot, "-")

	// Clean title (remove any unicode dash artifacts)
	title = strings.Split(title, " \\u2013")[0]

	course := &models.Course{
		Code:           code,
		Title:          title,
		Credit:         credit,
		Category:       category,
		CourseCategory: courseCategory,
		Type:           courseType,
		SlotType:       getSlotType(slot),
		Faculty:        faculty,
		Slot:           slot,
		Room:           room,
		AcademicYear:   academicYear,
	}

	return course
}

// parseCourseRow parses a single course row from table cells
func parseCourseRow(cells *goquery.Selection) *models.Course {
	if cells.Length() < 11 {
		logger.Info("parse_course", fmt.Sprintf("Insufficient cells: %d", cells.Length()), nil)
		return nil
	}

	getText := func(index int) string {
		return strings.TrimSpace(cells.Eq(index).Text())
	}

	// Map according to working implementation:
	// Column 0: Serial Number (ignored)
	// Column 1: AcademicYear
	// Column 2: Code
	// Column 3: Title
	// Column 4: Credit
	// Column 5: Category
	// Column 6: CourseCategory
	// Column 7: Type
	// Column 8: Faculty
	// Column 9: Slot
	// Column 10: Room

	academicYear := getText(10) // Column 1: Academic Year
	code := getText(1)         // Column 2: Code
	title := getText(2)        // Column 3: Title
	credit := getText(3)       // Column 4: Credit
	category := getText(4)     // Column 5: Category
	courseCategory := getText(5) // Column 6: CourseCategory
	courseType := getText(6)   // Column 7: Type
	faculty := getText(7)      // Column 8: Faculty
	slot := getText(8)         // Column 9: Slot
	room := getText(9)        // Column 10: Room

	logger.Info("parse_course", fmt.Sprintf("Raw data - Code: '%s', Title: '%s', Credit: '%s'", code, title, credit), nil)

	// Handle empty values
	if credit == "" {
		credit = "N/A"
	}
	if courseType == "" {
		courseType = "N/A"
	}
	if faculty == "" {
		faculty = "N/A"
	}
	if room == "" {
		room = "N/A"
	} else {
		// Capitalize first letter
		room = strings.ToUpper(room[:1]) + room[1:]
	}
	slot = strings.TrimSuffix(slot, "-")

	// Clean title (remove any unicode dash artifacts)
	title = strings.Split(title, " \\u2013")[0]

	course := &models.Course{
		Code:           code,
		Title:          title,
		Credit:         credit,
		Category:       category,
		CourseCategory: courseCategory,
		Type:           courseType,
		SlotType:       getSlotType(slot),
		Faculty:        faculty,
		Slot:           slot,
		Room:           room,
		AcademicYear:   academicYear,
	}

	logger.Info("parse_course", fmt.Sprintf("Created course: %s - %s", course.Code, course.Title), nil)
	return course
}

// CleanCoursesData cleans the parsed courses data before storage
func CleanCoursesData(coursesData *models.CoursesData) {
	logger.Info("clean_courses_data", "Starting courses data cleaning", nil)

	// Clean text fields in all courses
	for i := range coursesData.Courses {
		course := &coursesData.Courses[i]

		// Clean text fields that may contain unnecessary backslashes
		course.Title = cleanCourseText(course.Title)
		course.Category = cleanCourseText(course.Category)
		course.CourseCategory = cleanCourseText(course.CourseCategory)
		course.Type = cleanCourseText(course.Type)
		course.Faculty = cleanCourseText(course.Faculty)
		course.Slot = cleanCourseText(course.Slot)
		course.Room = cleanCourseText(course.Room)
	}

	logger.Info("clean_courses_data", "Courses data cleaning completed", nil)
}

// cleanCourseText cleans up course text by removing unnecessary backslashes and escape sequences
func cleanCourseText(text string) string {
	if text == "" {
		return text
	}

	// Remove unnecessary backslashes before common characters
	text = strings.ReplaceAll(text, "\\u0026", "&")  // & symbol
	text = strings.ReplaceAll(text, "\\u0027", "'")  // ' symbol
	text = strings.ReplaceAll(text, "\\u0022", "\"") // " symbol
	text = strings.ReplaceAll(text, "\\u005C", "\\") // \ symbol

	// Clean up any remaining unnecessary escape sequences that might appear
	text = strings.ReplaceAll(text, "\\n", " ")
	text = strings.ReplaceAll(text, "\\t", " ")
	text = strings.ReplaceAll(text, "\\r", " ")

	// Clean up multiple spaces
	text = strings.Join(strings.Fields(text), " ")

	return text
}

// getSlotType returns the slot type based on course type
func getSlotType(courseType string) string {
	courseType = strings.ToLower(courseType)
	switch {
	case strings.Contains(courseType, "lab"):
		return "Lab"
	case strings.Contains(courseType, "project"):
		return "Project"
	case strings.Contains(courseType, "theory"):
		return "Theory"
	default:
		return "Theory"
	}
}
