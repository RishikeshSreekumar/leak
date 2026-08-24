package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDateYAMLRoundTrip(t *testing.T) {
	in := Subscription{ID: "netflix", RenewalDate: NewDate(2026, 7, 21)}
	b, err := yaml.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(b), "renewal_date: \"2026-07-21\"")

	var out Subscription
	require.NoError(t, yaml.Unmarshal(b, &out))
	assert.Equal(t, in.RenewalDate.String(), out.RenewalDate.String())
}

func TestDateZeroMarshalsEmpty(t *testing.T) {
	var d Date
	assert.Equal(t, "", d.String())
	b, err := d.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, `""`, string(b))
}

func TestParseDateInvalid(t *testing.T) {
	_, err := ParseDate("21-07-2026")
	assert.Error(t, err)
}

func TestDaysSince(t *testing.T) {
	d := NewDate(2026, 1, 1)
	now := time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, 10, d.DaysSince(now))
	assert.Equal(t, 0, Date{}.DaysSince(now))
}

func TestTouchBumpsRev(t *testing.T) {
	s := Subscription{}
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	s.Touch(now)
	assert.Equal(t, 1, s.Rev)
	assert.Equal(t, now, s.UpdatedAt)
	s.Touch(now)
	assert.Equal(t, 2, s.Rev)
}

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()
	assert.Equal(t, "INR", p.DefaultCurrency)
	assert.Equal(t, 180, p.StaleAfterDays)
	assert.Contains(t, p.Categories, "AI")
}

func TestDataMigrateStampsVersion(t *testing.T) {
	// A pre-versioning file (Version 0) upgrades to the current schema.
	d := &Data{Subscriptions: []Subscription{{ID: "a"}}}
	changed := d.Migrate()
	assert.True(t, changed, "0 -> current should report a change")
	assert.Equal(t, SchemaVersion, d.Version)

	// Already-current is a no-op.
	changed = d.Migrate()
	assert.False(t, changed)
	assert.Equal(t, SchemaVersion, d.Version)
}
