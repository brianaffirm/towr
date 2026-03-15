package orchestrate

// Slugify converts a task description into a short workspace ID.
func Slugify(s string) string {
	var result []byte
	prevDash := false
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			result = append(result, c)
			prevDash = false
		case c >= 'A' && c <= 'Z':
			result = append(result, c+32) // lowercase
			prevDash = false
		case c == ' ' || c == '-' || c == '_' || c == '/':
			if !prevDash && len(result) > 0 {
				result = append(result, '-')
				prevDash = true
			}
		}
	}
	// Trim trailing dash.
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	// Limit length.
	if len(result) > 30 {
		result = result[:30]
	}
	if len(result) == 0 {
		return "workspace"
	}
	return string(result)
}
