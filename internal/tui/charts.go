package tui

import "strings"

// bar renders a horizontal bar `width` cells wide representing value/max, using
// a filled block for the proportion and a light block for the remainder. It is
// pure so it can be table-tested: non-positive width/max or value yields "".
func bar(value, max float64, width int) string {
	if width <= 0 || max <= 0 || value <= 0 {
		return strings.Repeat("░", clampNonNeg(width))
	}
	filled := int((value/max)*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 1 {
		filled = 1
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
