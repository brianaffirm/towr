package validate

import "regexp"

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// IsValidEmail reports whether s is a valid email address.
func IsValidEmail(s string) bool {
	return emailRe.MatchString(s)
}
