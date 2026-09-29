package knowledge

// Split preserves every rune and separator so concatenation reconstructs the source.
func Split(text string, limit int) []string {
	if limit < 1 {
		limit = 2000
	}
	runes := []rune(text)
	result := make([]string, 0, (len(runes)+limit-1)/limit)
	for len(runes) > 0 {
		n := min(len(runes), limit)
		result = append(result, string(runes[:n]))
		runes = runes[n:]
	}
	return result
}
