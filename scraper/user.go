package scraper

import (
	"fmt"
	"os"
	"regexp"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// decodeJSEscapes decodes JavaScript hex escape sequences (\xXX) to actual characters
func decodeJSEscapes(s string) string {
	// Replace \xXX with actual characters where XX is hex
	re := regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		hex := match[2:] // Remove \x prefix
		if val, err := strconv.ParseInt(hex, 16, 8); err == nil {
			return string(rune(val))
		}
		return match // Return original if parsing fails
	})
}

// ExtractHTMLFromSanitizer extracts and decodes HTML from pageSanitizer.sanitize() calls
func ExtractHTMLFromSanitizer(htmlContent string, filename string) (string, error) {
	// Check if HTML contains pageSanitizer.sanitize() call
	if strings.Contains(htmlContent, "pageSanitizer.sanitize(") {
		logger.Info("extract_html", "Found pageSanitizer.sanitize() call, extracting content", map[string]interface{}{
			"filename": filename,
		})

		// Extract content from pageSanitizer.sanitize() call
		start := strings.Index(htmlContent, "pageSanitizer.sanitize('")
		if start == -1 {
			logger.Error("extract_html", "Could not find pageSanitizer.sanitize start", nil, nil)
			return "", fmt.Errorf("could not find pageSanitizer.sanitize start")
		}

		start += len("pageSanitizer.sanitize('")
		end := strings.Index(htmlContent[start:], "');")
		if end == -1 {
			logger.Error("extract_html", "Could not find pageSanitizer.sanitize end", nil, nil)
			return "", fmt.Errorf("could not find pageSanitizer.sanitize end")
		}

		// Extract the escaped HTML content
		escapedHTML := htmlContent[start : start+end]

		// Decode JavaScript hex escape sequences
		actualHTML := decodeJSEscapes(escapedHTML)

		// Save the unescaped HTML to file
		err := os.WriteFile(filename, []byte(actualHTML), 0644)
		if err != nil {
			logger.Error("extract_html", "Failed to save unescaped HTML", err, map[string]interface{}{
				"filename": filename,
			})
		} else {
			logger.Info("extract_html", "Saved unescaped HTML", map[string]interface{}{
				"filename": filename,
				"original_length": len(htmlContent),
				"unescaped_length": len(actualHTML),
			})
		}

		return actualHTML, nil
	}

	logger.Info("extract_html", "No pageSanitizer.sanitize() found, using original HTML", map[string]interface{}{
		"filename": filename,
	})
	return htmlContent, nil
}

// ParseUserInfo parses user information from HTML
func ParseUserInfo(htmlContent string) (*models.UserInfo, error) {
	logger.Info("parse_user", "Starting to parse user info", nil)

	// Extract HTML from pageSanitizer.sanitize() if present
	actualHTML, err := ExtractHTMLFromSanitizer(htmlContent, "users.html")
	if err != nil {
		logger.Error("parse_user", "Failed to extract HTML from sanitizer", err, nil)
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(actualHTML))
	if err != nil {
		logger.Error("parse_user", "Failed to parse HTML document", err, nil)
		return nil, err
	}

	userInfo := &models.UserInfo{}

	// Find the table with style="width:900px;"
	table := doc.Find(`table[style="width:900px;"]`).First()
	if table.Length() == 0 {
		logger.Error("parse_user", "User info table not found", nil, nil)
		return nil, fmt.Errorf("user info table not found")
	}

	// Parse each row
	table.Find("tr").Each(func(i int, row *goquery.Selection) {
		cells := row.Find("td")
		
		// Process pairs of label-value
		for j := 0; j < cells.Length(); j += 2 {
			if j+1 < cells.Length() {
				label := strings.TrimSpace(cells.Eq(j).Text())
				value := strings.TrimSpace(cells.Eq(j+1).Find("strong").Text())
				
				// Map fields
				switch {
				case strings.Contains(label, "Registration Number"):
					userInfo.RegNumber = value
				case strings.Contains(label, "Name"):
					userInfo.Name = value
				case strings.Contains(label, "Mobile"):
					userInfo.Mobile = value
				case strings.Contains(label, "Program"):
					userInfo.Program = value
				case strings.Contains(label, "Department"):
					// Extract department and section from red font tag
					departmentHTML, _ := cells.Eq(j+1).Html()
					if strings.Contains(departmentHTML, `color = 'Red'`) || strings.Contains(departmentHTML, `color="Red"`) {
						// Extract section from red font
						doc3, _ := goquery.NewDocumentFromReader(strings.NewReader(departmentHTML))
						redText := strings.TrimSpace(doc3.Find("font[color='Red'], font[color=\"Red\"]").Text())
						if strings.Contains(redText, "(") && strings.Contains(redText, ")") {
							// Extract G2 from (G2 Section)
							start := strings.Index(redText, "(")
							end := strings.Index(redText, ")")
							if start != -1 && end != -1 && end > start {
								sectionContent := redText[start+1 : end]
								// Remove "Section" and trim
								sectionContent = strings.ReplaceAll(sectionContent, "Section", "")
								userInfo.Section = strings.TrimSpace(sectionContent)
							}
						}
					}
					// Extract department name (everything before the red font)
					department := value
					if strings.Contains(department, "-") {
						parts := strings.Split(department, "-")
						userInfo.Department = strings.TrimSpace(parts[0])
					} else {
						userInfo.Department = department
					}
				case strings.Contains(label, "Semester"):
					semester, _ := strconv.Atoi(value)
					userInfo.Semester = semester
				case strings.Contains(label, "Batch"):
					// Extract batch number - try multiple methods
					batchHTML, _ := cells.Eq(j+1).Html()
					logger.Info("parse_user", "Batch extraction attempt", map[string]interface{}{
						"batch_html": batchHTML,
						"has_red_color": strings.Contains(batchHTML, `color = 'Red'`) || strings.Contains(batchHTML, `color="Red"`),
					})

					var extractedBatch string

					// Method 1: Extract from red font (original method)
					if strings.Contains(batchHTML, `color = 'Red'`) || strings.Contains(batchHTML, `color="Red"`) {
						doc2, _ := goquery.NewDocumentFromReader(strings.NewReader(batchHTML))
						extractedBatch = strings.TrimSpace(doc2.Find("font[color='Red'], font[color=\"Red\"]").Text())
						logger.Info("parse_user", "Batch extracted from red font", map[string]interface{}{
							"batch": extractedBatch,
						})
					}

					// Method 2: If red font failed or not present, extract from any font tag
					if extractedBatch == "" {
						doc2, _ := goquery.NewDocumentFromReader(strings.NewReader(batchHTML))
						extractedBatch = strings.TrimSpace(doc2.Find("font").First().Text())
						logger.Info("parse_user", "Batch extracted from any font", map[string]interface{}{
							"batch": extractedBatch,
						})
					}

					// Method 3: If still empty, extract from any text content
					if extractedBatch == "" {
						doc2, _ := goquery.NewDocumentFromReader(strings.NewReader(batchHTML))
						extractedBatch = strings.TrimSpace(doc2.Text())
						logger.Info("parse_user", "Batch extracted from text content", map[string]interface{}{
							"batch": extractedBatch,
						})
					}

					// Clean up the batch (remove extra spaces, newlines)
					extractedBatch = strings.TrimSpace(strings.ReplaceAll(extractedBatch, "\n", ""))
					extractedBatch = strings.TrimSpace(strings.ReplaceAll(extractedBatch, "\r", ""))
					extractedBatch = strings.TrimSpace(strings.ReplaceAll(extractedBatch, "\t", ""))

					userInfo.Batch = extractedBatch
				}
			}
		}
	})

	// Calculate year from semester (1-2: Year 1, 3-4: Year 2, etc.)
	if userInfo.Semester > 0 {
		userInfo.Year = (userInfo.Semester + 1) / 2
	}

	logger.Info("parse_user", "User info parsed successfully", map[string]interface{}{
		"reg_number": userInfo.RegNumber,
		"name":       userInfo.Name,
		"batch":      userInfo.Batch,
		"semester":   userInfo.Semester,
	})
	return userInfo, nil
}
