package scraper

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"srm-academia-scraper/models"
)

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

	var entries []models.MarksEntry

	doc.Find("table").Each(func(i int, table *goquery.Selection) {
		var headerRowIdx = -1
		codeIdx, titleIdx, totalIdx := -1, -1, -1
		assessmentCols := map[int]string{}

		table.Find("tr").EachWithBreak(func(rowIdx int, row *goquery.Selection) bool {
			matchedCode := false
			matchedTitle := false
			row.Find("th, td").Each(func(idx int, cell *goquery.Selection) {
				text := strings.ToLower(strings.TrimSpace(cell.Text()))
				switch {
				case strings.Contains(text, "course code") && codeIdx == -1:
					codeIdx = idx
					matchedCode = true
				case strings.Contains(text, "course title") && titleIdx == -1:
					titleIdx = idx
					matchedTitle = true
				case strings.Contains(text, "total") && totalIdx == -1:
					totalIdx = idx
				case strings.Contains(text, "marks") || strings.Contains(text, "internal") ||
					strings.Contains(text, "assessment") || strings.Contains(text, "test"):
					assessmentCols[idx] = strings.TrimSpace(cell.Text())
				}
			})
			if matchedCode && matchedTitle {
				headerRowIdx = rowIdx
				return false
			}
			return true
		})

		if headerRowIdx == -1 || codeIdx == -1 || titleIdx == -1 {
			return
		}

		table.Find("tr").Each(func(rowIdx int, row *goquery.Selection) {
			if rowIdx <= headerRowIdx {
				return
			}
			cells := row.Find("td")
			if cells.Length() == 0 {
				return
			}

			entry := models.MarksEntry{}
			entry.Assessments = []models.MarksAssessment{}

			cells.Each(func(idx int, cell *goquery.Selection) {
				text := strings.TrimSpace(cell.Text())
				switch idx {
				case codeIdx:
					entry.CourseCode = text
				case titleIdx:
					entry.CourseTitle = text
				case totalIdx:
					if f, ok := parseFloatValue(text); ok {
						entry.Total = floatPointer(f)
					}
				default:
					if name, ok := assessmentCols[idx]; ok && text != "" {
						score, max := parseScoreMax(text)
						entry.Assessments = append(entry.Assessments, models.MarksAssessment{
							Name:  fallbackAssessmentName(name),
							Score: score,
							Max:   max,
						})
					}
				}
			})

			if entry.CourseCode != "" || entry.CourseTitle != "" {
				entries = append(entries, entry)
			}
		})
	})

	if len(entries) == 0 {
		return nil, fmt.Errorf("marks table not found")
	}
	return entries, nil
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
