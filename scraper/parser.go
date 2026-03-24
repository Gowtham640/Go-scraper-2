package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"srm-academia-scraper/models"
)

// marksCreditCellPattern matches a lone credit value in the marks table middle column (e.g. "3", "4.5").
var marksCreditCellPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

// ExtractSanitizedHTML extracts and decodes the content passed to pageSanitizer.sanitize().
func ExtractSanitizedHTML(html string) (string, error) {
	const marker = "pageSanitizer.sanitize('"
	idx := strings.Index(html, marker)
	if idx == -1 {
		return "", fmt.Errorf("pageSanitizer.sanitize() call not found")
	}

	idx += len(marker)
	var builder strings.Builder
	escaped := false
	for i := idx; i < len(html); i++ {
		ch := html[i]
		if escaped {
			builder.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			builder.WriteByte('\\')
			escaped = true
			continue
		}
		if ch == '\'' {
			raw := builder.String()
			decoded, err := strconv.Unquote(`"` + raw + `"`)
			if err != nil {
				return "", fmt.Errorf("failed to decode sanitized html: %w", err)
			}
			return decoded, nil
		}
		builder.WriteByte(ch)
	}

	return "", fmt.Errorf("closing quote for pageSanitizer.sanitize() not found")
}

// ParseAttendance returns attendance entries from provided HTML.
func ParseAttendance(html string) ([]models.AttendanceEntry, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse attendance HTML: %w", err)
	}

	attendanceHeaders := map[string]string{
		"course code":           "courseCode",
		"course title":          "courseTitle",
		"category":              "category",
		"faculty name":          "faculty",
		"faculty":               "faculty",
		"slot":                  "slot",
		"hours conducted":       "hoursConducted",
		"hours absent":          "hoursAbsent",
		"attn %":                "attendancePercentage",
		"attendance %":          "attendancePercentage",
		"attendance percentage": "attendancePercentage",
	}

	const minHeaderMatches = 5

	var entries []models.AttendanceEntry
	found := false

	doc.Find("table").EachWithBreak(func(i int, table *goquery.Selection) bool {
		var headerRowIdx int = -1
		var headerMap map[int]string

		table.Find("tr").EachWithBreak(func(rowIdx int, row *goquery.Selection) bool {
			mapping := map[int]string{}
			matchCount := 0
			row.Find("th, td").Each(func(idx int, cell *goquery.Selection) {
				text := strings.ToLower(strings.TrimSpace(cell.Text()))
				for keyword, field := range attendanceHeaders {
					if text != "" && strings.Contains(text, keyword) {
						if _, exists := mapping[idx]; !exists {
							mapping[idx] = field
							matchCount++
						}
						break
					}
				}
			})
			if matchCount >= minHeaderMatches {
				headerRowIdx = rowIdx
				headerMap = mapping
				return false
			}
			return true
		})

		if headerRowIdx == -1 || len(headerMap) == 0 {
			return true
		}

		table.Find("tr").Each(func(rowIdx int, row *goquery.Selection) {
			if rowIdx <= headerRowIdx {
				return
			}
			cells := row.Find("td")
			if cells.Length() == 0 {
				return
			}

			entry := models.AttendanceEntry{}
			cells.Each(func(cellIdx int, cell *goquery.Selection) {
				field, ok := headerMap[cellIdx]
				if !ok {
					return
				}
				value := strings.TrimSpace(cell.Text())
				switch field {
				case "courseCode":
					entry.CourseCode = value
				case "courseTitle":
					entry.CourseTitle = value
				case "category":
					entry.Category = value
				case "faculty":
					entry.Faculty = value
				case "slot":
					entry.Slot = value
				case "hoursConducted":
					if f, ok := parseFloatValue(value); ok {
						entry.HoursConducted = f
					}
				case "hoursAbsent":
					if f, ok := parseFloatValue(value); ok {
						entry.HoursAbsent = f
					}
				case "attendancePercentage":
					if f, ok := parseFloatValue(value); ok {
						entry.AttendancePercentage = f
					}
				}
			})

			if entry.CourseCode != "" || entry.CourseTitle != "" {
				entries = append(entries, entry)
			}
		})

		if len(entries) > 0 {
			found = true
			return false
		}
		return true
	})

	if !found {
		return nil, fmt.Errorf("attendance table not found")
	}
	return entries, nil
}

// ParseMarks returns normalized mark entries from the provided HTML.
func ParseMarks(html string) ([]models.MarksEntry, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse marks HTML: %w", err)
	}

	marksTable := findMarksTable(doc)
	if marksTable == nil {
		return nil, fmt.Errorf("marks section not found")
	}

	headerRowIdx := -1

	marksTable.Find("tr").EachWithBreak(func(rowIdx int, row *goquery.Selection) bool {
		if headerRowIdx == -1 && row.Find("td").Length() > 0 {
			if strings.Contains(strings.ToLower(row.Text()), "course code") &&
				strings.Contains(strings.ToLower(row.Text()), "test performance") {
				headerRowIdx = rowIdx
				return false
			}
		}
		return true
	})

	if headerRowIdx == -1 {
		return nil, fmt.Errorf("marks header row not found")
	}

	titleLookup := buildCourseTitleLookup(html)
	var entries []models.MarksEntry

	marksTable.Find("tr").Each(func(rowIdx int, row *goquery.Selection) {
		if rowIdx <= headerRowIdx {
			return
		}
		cells := row.Find("td")
		if cells.Length() < 3 {
			return
		}

		courseCode := strings.TrimSpace(cells.Eq(0).Text())
		if courseCode == "" {
			return
		}

		cell1 := strings.TrimSpace(cells.Eq(1).Text())
		credit := ""
		if marksCreditCellPattern.MatchString(cell1) {
			credit = cell1
		}

		entry := models.MarksEntry{
			CourseCode:  courseCode,
			CourseTitle: findCourseTitle(titleLookup, courseCode),
			Credit:      credit,
			Assessments: extractAssessmentsFromCell(cells.Eq(2)),
		}

		if entry.Assessments == nil {
			entry.Assessments = []models.MarksAssessment{}
		}

		entries = append(entries, entry)
	})

	if len(entries) == 0 {
		return nil, fmt.Errorf("marks entries not found")
	}

	return entries, nil
}

func buildCourseTitleLookup(html string) map[string]string {
	lookup := map[string]string{}
	attendanceEntries, err := ParseAttendance(html)
	if err != nil {
		return lookup
	}

	for _, entry := range attendanceEntries {
		key := canonicalCourseCode(entry.CourseCode)
		if key == "" {
			continue
		}
		lookup[key] = entry.CourseTitle
	}
	return lookup
}

func findCourseTitle(lookup map[string]string, courseCode string) string {
	if len(lookup) == 0 {
		return ""
	}

	key := canonicalCourseCode(courseCode)
	if title, ok := lookup[key]; ok {
		return title
	}

	upper := strings.ToUpper(strings.TrimSpace(courseCode))
	if title, ok := lookup[upper]; ok {
		return title
	}

	return ""
}

func canonicalCourseCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	upper := strings.ToUpper(code)
	if strings.HasSuffix(upper, "REGULAR") {
		return strings.TrimSpace(upper[:len(upper)-len("REGULAR")])
	}
	return upper
}

func extractAssessmentsFromCell(cell *goquery.Selection) []models.MarksAssessment {
	var assessments []models.MarksAssessment
	inner := cell.Find("table").First()
	if inner.Length() == 0 {
		inner = cell
	}

	inner.Find("td").Each(func(_ int, assessmentCell *goquery.Selection) {
		nameRaw := strings.TrimSpace(assessmentCell.Find("strong").First().Text())
		if nameRaw == "" {
			return
		}
		if assessment := parseAssessmentCell(assessmentCell); assessment != nil {
			assessments = append(assessments, *assessment)
		}
	})
	return assessments
}

func parseAssessmentCell(cell *goquery.Selection) *models.MarksAssessment {
	nameRaw := strings.TrimSpace(cell.Find("strong").First().Text())
	if nameRaw == "" {
		return nil
	}

	name, max := parseAssessmentNameAndMax(nameRaw)

	rawText := strings.TrimSpace(cell.Text())
	scoreText := strings.TrimSpace(strings.TrimPrefix(rawText, nameRaw))
	var score *float64
	if scoreText != "" {
		parts := strings.Fields(scoreText)
		if len(parts) > 0 {
			scoreStr := parts[0]
			if !strings.EqualFold(scoreStr, "abs") {
				if parsed, ok := parseFloatValue(scoreStr); ok {
					score = floatPointer(parsed)
				}
			}
		}
	}

	return &models.MarksAssessment{
		Name:  fallbackAssessmentName(name),
		Score: score,
		Max:   max,
	}
}

func parseAssessmentNameAndMax(raw string) (string, *float64) {
	parts := strings.SplitN(raw, "/", 2)
	name := strings.TrimSpace(parts[0])
	var max *float64
	if len(parts) > 1 {
		if parsed, ok := parseFloatValue(parts[1]); ok {
			max = floatPointer(parsed)
		}
	}
	return name, max
}

func findMarksTable(doc *goquery.Document) *goquery.Selection {
	var table *goquery.Selection
	doc.Find("p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		text := strings.ToLower(strings.TrimSpace(p.Text()))
		if !strings.Contains(text, "internal marks detail") {
			return true
		}

		current := p
		for current.Length() > 0 {
			if current.Is("table") {
				table = current
				return false
			}
			current = current.Next()
		}
		return true
	})

	if table == nil {
		table = findMarksTableFallback(doc)
	}
	return table
}

func findMarksTableFallback(doc *goquery.Document) *goquery.Selection {
	var fallback *goquery.Selection
	doc.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		headerText := strings.ToLower(strings.TrimSpace(table.Find("tr").First().Text()))
		if strings.Contains(headerText, "course code") && strings.Contains(headerText, "test performance") {
			fallback = table
			return false
		}
		return true
	})
	return fallback
}
func parseFloatValue(input string) (float64, bool) {
	value := strings.TrimSpace(strings.ReplaceAll(input, ",", ""))
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" || value == "-" {
		return 0, false
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func floatPointer(value float64) *float64 {
	return &value
}

func parseScoreMax(input string) (*float64, *float64) {
	normalized := strings.TrimSpace(strings.ReplaceAll(input, "\u00A0", ""))
	if normalized == "" {
		return nil, nil
	}
	if strings.Contains(normalized, "/") {
		parts := strings.SplitN(normalized, "/", 2)
		if score, ok := parseFloatValue(parts[0]); ok {
			if max, ok2 := parseFloatValue(parts[1]); ok2 {
				return floatPointer(score), floatPointer(max)
			}
			return floatPointer(score), nil
		}
		return nil, nil
	}
	if score, ok := parseFloatValue(normalized); ok {
		return floatPointer(score), nil
	}
	return nil, nil
}

func fallbackAssessmentName(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return "Assessment"
	}
	return header
}
