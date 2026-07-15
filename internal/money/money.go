// Package money handles currency math, billing-cycle normalization, and
// human-friendly formatting with currency symbols.
package money

import (
	"fmt"
	"math"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// symbols maps common ISO currency codes to display symbols.
var symbols = map[string]string{
	"INR": "₹",
	"USD": "$",
	"EUR": "€",
	"GBP": "£",
	"JPY": "¥",
	"AUD": "A$",
	"CAD": "C$",
	"SGD": "S$",
}

// Symbol returns the display symbol for a currency code, falling back to the
// code itself plus a space (e.g. "CHF ").
func Symbol(currency string) string {
	if s, ok := symbols[strings.ToUpper(currency)]; ok {
		return s
	}
	return strings.ToUpper(currency) + " "
}

// cycleToMonthly maps a billing cycle to its per-month multiplier.
var cycleToMonthly = map[string]float64{
	model.CycleWeekly:    52.0 / 12.0,
	model.CycleMonthly:   1.0,
	model.CycleQuarterly: 1.0 / 3.0,
	model.CycleYearly:    1.0 / 12.0,
}

// NormalizedMonthly converts an amount billed on the given cycle into an
// equivalent monthly amount. Unknown cycles are treated as monthly.
func NormalizedMonthly(amount float64, cycle string) float64 {
	m, ok := cycleToMonthly[strings.ToLower(cycle)]
	if !ok {
		return amount
	}
	return amount * m
}

// Format renders an amount with its currency symbol, no decimals for whole
// numbers and two decimals otherwise (e.g. "₹649", "$9.99").
func Format(amount float64, currency string) string {
	sym := Symbol(currency)
	sign := ""
	if amount < 0 {
		sign, amount = "-", -amount
	}
	if amount == math.Trunc(amount) {
		return fmt.Sprintf("%s%s%s", sign, sym, group(int64(amount)))
	}
	whole := math.Trunc(amount)
	frac := math.Round((amount - whole) * 100)
	return fmt.Sprintf("%s%s%s.%02d", sign, sym, group(int64(whole)), int(frac))
}

// group inserts thousands separators (Western grouping).
func group(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		return "-" + out
	}
	return out
}

// Convert applies an FX rate to move an amount from its source currency into a
// target currency. A rate of 1.0 is a no-op (same currency).
func Convert(amount, rate float64) float64 { return amount * rate }

// Round2 rounds to two decimal places.
func Round2(v float64) float64 { return math.Round(v*100) / 100 }
