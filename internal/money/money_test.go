package money

import (
	"testing"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestNormalizedMonthly(t *testing.T) {
	assert.InDelta(t, 649, NormalizedMonthly(649, model.CycleMonthly), 1e-9)
	assert.InDelta(t, 10, NormalizedMonthly(120, model.CycleYearly), 1e-9)
	assert.InDelta(t, 100, NormalizedMonthly(300, model.CycleQuarterly), 1e-9)
	assert.InDelta(t, 43.333, NormalizedMonthly(10, model.CycleWeekly), 1e-3)
	// unknown cycle -> treated as monthly
	assert.InDelta(t, 5, NormalizedMonthly(5, "biweekly"), 1e-9)
}

func TestSymbol(t *testing.T) {
	assert.Equal(t, "₹", Symbol("INR"))
	assert.Equal(t, "$", Symbol("usd"))
	assert.Equal(t, "€", Symbol("EUR"))
	assert.Equal(t, "CHF ", Symbol("CHF"))
}

func TestFormat(t *testing.T) {
	assert.Equal(t, "₹649", Format(649, "INR"))
	assert.Equal(t, "₹4,823", Format(4823, "INR"))
	assert.Equal(t, "₹77,784", Format(77784, "INR"))
	assert.Equal(t, "$9.99", Format(9.99, "USD"))
	assert.Equal(t, "$10", Format(10, "USD"))
	assert.Equal(t, "-₹500", Format(-500, "INR"))
}

func TestConvert(t *testing.T) {
	assert.InDelta(t, 867, Convert(10, 86.7), 1e-9)
	assert.InDelta(t, 10, Convert(10, 1.0), 1e-9)
}
