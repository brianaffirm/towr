package validate

import "testing"

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user@example.com", true},
		{"user.name+tag@sub.domain.org", true},
		{"notanemail", false},
		{"missing@tld", false},
		{"@nodomain.com", false},
		{"", false},
	}
	for _, tc := range tests {
		got := IsValidEmail(tc.input)
		if got != tc.want {
			t.Errorf("IsValidEmail(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
