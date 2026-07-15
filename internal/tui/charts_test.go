package tui

import "testing"

func TestBar(t *testing.T) {
	tests := []struct {
		name              string
		value, max        float64
		width             int
		wantLen, wantFull int // total cells, filled cells
	}{
		{"half", 5, 10, 10, 10, 5},
		{"full", 10, 10, 8, 8, 8},
		{"tiny nonzero rounds to at least one", 1, 1000, 10, 10, 1},
		{"overflow clamps to width", 20, 10, 6, 6, 6},
		{"zero value is all empty", 0, 10, 4, 4, 0},
		{"zero max is all empty", 5, 0, 4, 4, 0},
		{"zero width is empty", 5, 10, 0, 0, 0},
		{"negative width is empty", 5, 10, -3, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bar(tt.value, tt.max, tt.width)
			if gotLen := runeLen(got); gotLen != tt.wantLen {
				t.Fatalf("len = %d, want %d (%q)", gotLen, tt.wantLen, got)
			}
			if full := countRune(got, '█'); full != tt.wantFull {
				t.Fatalf("filled = %d, want %d (%q)", full, tt.wantFull, got)
			}
		})
	}
}

func runeLen(s string) int { return len([]rune(s)) }

func countRune(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			n++
		}
	}
	return n
}
