package scraper

import "strings"

// LoginPageExactTotalBytes is the raw response body length for the portal login
// shell (user-provided fingerprint).
const LoginPageExactTotalBytes = 8261

// loginPageKeyPhrases are substrings that appear on the real Academia login HTML.
// Verified against scraper 3/attendance.html in this repository.
var loginPageKeyPhrases = []string{
	"Forgot Password?",
	"Thank you for signing up!",
	"Customer Portal Default Signin Form",
	"Academic Web Services Login",
}

// DetectLoginLikePage reports whether body is the portal login shell.
// True when len(body) == LoginPageExactTotalBytes (8261), or when any
// loginPageKeyPhrases substring is found (exact match, case-sensitive).
// Indicators name the rule that fired (for logging).
func DetectLoginLikePage(body string) (bool, []string) {
	if len(body) == LoginPageExactTotalBytes {
		return true, []string{"size:exact_8261"}
	}
	for _, phrase := range loginPageKeyPhrases {
		if strings.Contains(body, phrase) {
			return true, []string{"phrase:" + phrase}
		}
	}
	return false, nil
}
