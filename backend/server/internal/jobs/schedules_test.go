package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptr(n int) *int { return &n }

func TestNewTimingRejects(t *testing.T) {
	cases := []struct {
		name     string
		interval *int
		cron, tz string
		field    string
	}{
		{"neither interval nor cron", nil, "", "UTC", "/interval_seconds"},
		{"both interval and cron", ptr(60), "* * * * *", "UTC", "/cron"},
		{"interval below minimum", ptr(MinIntervalSeconds - 1), "", "UTC", "/interval_seconds"},
		{"interval above maximum", ptr(MaxIntervalSeconds + 1), "", "UTC", "/interval_seconds"},
		{"minute out of range", nil, "61 * * * *", "UTC", "/cron"},
		{"seconds field", nil, "0 0 18 * * *", "UTC", "/cron"},
		{"zone inside the expression", nil, "TZ=UTC 0 18 * * *", "UTC", "/cron"},
		{"@every descriptor", nil, "@every 1s", "UTC", "/cron"},
		{"unknown zone", nil, "0 18 * * *", "Mars/Olympus", "/timezone"},
		{"server-local zone", ptr(60), "", "Local", "/timezone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, fe := newTiming(c.interval, c.cron, c.tz)
			require.Len(t, fe, 1)
			require.Equal(t, c.field, fe[0].Field)
		})
	}
}

func TestNewTimingAccepts(t *testing.T) {
	for _, c := range []struct {
		interval *int
		cron, tz string
	}{
		{ptr(17), "", "UTC"},
		{nil, "*/1 * * * *", "UTC"},
		{nil, "0 18 * * *", "Asia/Kolkata"},
		{nil, "@daily", "Africa/Nairobi"},
	} {
		_, fe := newTiming(c.interval, c.cron, c.tz)
		require.Empty(t, fe, "%v %q %q", c.interval, c.cron, c.tz)
	}
}

func TestIntervalKeepsPhaseAndSkipsMissedRuns(t *testing.T) {
	tm, fe := newTiming(ptr(20), "", "UTC")
	require.Empty(t, fe)
	due := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	require.Equal(t, due.Add(20*time.Second), tm.next(due, due), "fired on time")
	require.Equal(t, due.Add(20*time.Second), tm.next(due, due.Add(3*time.Second)), "fired late: phase kept")
	require.Equal(t, due.Add(60*time.Second), tm.next(due, due.Add(45*time.Second)), "two missed runs skipped")
	require.Equal(t, due.Add(60*time.Second), tm.next(due, due.Add(40*time.Second)), "never schedules at now")
}

func TestCronRunsInItsTimeZone(t *testing.T) {
	tm, fe := newTiming(nil, "0 18 * * *", "Asia/Kolkata") // 18:00 IST = 12:30 UTC
	require.Empty(t, fe)
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	first := tm.next(now, now)
	require.Equal(t, time.Date(2026, 10, 13, 12, 30, 0, 0, time.UTC), first.UTC())
	require.Equal(t, time.Date(2026, 10, 14, 12, 30, 0, 0, time.UTC), tm.next(first, first).UTC())
}

func TestCronThatNeverMatches(t *testing.T) {
	tm, fe := newTiming(nil, "0 0 30 2 *", "UTC") // 30 February
	require.Empty(t, fe)
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	require.True(t, tm.next(now, now).IsZero())
}
