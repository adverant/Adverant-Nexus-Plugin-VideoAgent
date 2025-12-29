package scene

// extractJSON extracts JSON from markdown code blocks or raw response
func extractJSON(response string) string {
	// Simple extraction - look for { and }
	start := -1
	end := -1

	for i, ch := range response {
		if ch == '{' && start == -1 {
			start = i
		}
		if ch == '}' {
			end = i + 1
		}
	}

	if start >= 0 && end > start {
		return response[start:end]
	}

	return response
}
