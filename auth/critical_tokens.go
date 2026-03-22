package auth

import "strings"

// cookieRequirement defines how to validate a required cookie by name or prefix.
type cookieRequirement struct {
	name    string
	matcher func(string) bool
}

var criticalCookieRequirements = []cookieRequirement{
	{
		name: "iamcsr",
		matcher: func(name string) bool {
			return name == "iamcsr"
		},
	},
	{
		name: "wms-tkp-token",
		matcher: func(name string) bool {
			return strings.HasPrefix(name, "wms-tkp-token")
		},
	},
}

// GetMissingCriticalCookies returns a list of required cookie names that are not
// present in the provided cookie header string.
func GetMissingCriticalCookies(cookieHeader string) []string {
	if strings.TrimSpace(cookieHeader) == "" {
		missing := make([]string, len(criticalCookieRequirements))
		for i, req := range criticalCookieRequirements {
			missing[i] = req.name
		}
		return missing
	}

	available := parseCookieNames(cookieHeader)
	var missing []string

	for _, req := range criticalCookieRequirements {
		found := false
		for _, name := range available {
			if req.matcher(name) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, req.name)
		}
	}

	return missing
}

// parseCookieNames extracts the cookie names from a cookie header string.
func parseCookieNames(header string) []string {
	var names []string
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx > -1 {
			names = append(names, strings.TrimSpace(part[:idx]))
		} else {
			names = append(names, part)
		}
	}
	return names
}
