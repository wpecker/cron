package cron

import (
	"math/big"
	"strconv"
	"time"
)

// SpecSchedule specifies a duty cycle (to the second granularity), based on a
// traditional crontab specification. It is computed initially and stored as bit sets.
type SpecSchedule struct {
	Second, Minute, Hour, Dom, Month, Dow, Year *big.Int

	// Override location for this schedule.
	Location *time.Location
}

// bounds provides a range of acceptable values (plus a map of name to value).
type bounds struct {
	min, max uint
	names    map[string]uint
}

// The bounds for each field.
var (
	seconds = bounds{0, 59, nil}
	minutes = bounds{0, 59, nil}
	hours   = bounds{0, 23, nil}
	dom     = bounds{1, 31, map[string]uint{
		"l":  55,
		"1l": 54,
		"2l": 53,
		"3l": 52,
		"4l": 51,
		"5l": 50,
		"6l": 49,
		"7l": 48,
	}}
	months = bounds{1, 12, map[string]uint{
		"jan": 1,
		"feb": 2,
		"mar": 3,
		"apr": 4,
		"may": 5,
		"jun": 6,
		"jul": 7,
		"aug": 8,
		"sep": 9,
		"oct": 10,
		"nov": 11,
		"dec": 12,
	}}
	dow = bounds{0, 6, map[string]uint{
		"sun":  0,
		"mon":  1,
		"tue":  2,
		"wed":  3,
		"thu":  4,
		"fri":  5,
		"sat":  6,
		"sunl": 49,
		"monl": 50,
		"tuel": 51,
		"wedl": 52,
		"thul": 53,
		"fril": 54,
		"satl": 55,
		"0l":   49,
		"1l":   50,
		"2l":   51,
		"3l":   52,
		"4l":   53,
		"5l":   54,
		"6l":   55,
	}}
	years = bounds{0, maxYear - minYear, nil} // 1970~2099
)

func init() {
	years.names = make(map[string]uint)
	for i := minYear; i <= maxYear; i++ {
		years.names[strconv.Itoa(i)] = uint(i - minYear)
	}
}

const (
	maxBits = 160

	minYear = 1970
	maxYear = 2099
)

// Next returns the next time this schedule is activated, greater than the given
// time.  If no time can be found to satisfy the schedule, return the zero time.
func (s *SpecSchedule) Next(t time.Time) time.Time {
	// General approach
	//
	// For Month, Day, Hour, Minute, Second:
	// Check if the time value matches.  If yes, continue to the next field.
	// If the field doesn't match the schedule, then increment the field until it matches.
	// While incrementing the field, a wrap-around brings it back to the beginning
	// of the field list (since it is necessary to re-verify previous field
	// values)

	// Convert the given time into the schedule's timezone, if one is specified.
	// Save the original timezone so we can convert back after we find a time.
	// Note that schedules without a time zone specified (time.Local) are treated
	// as local to the time provided.
	origLocation := t.Location()
	loc := s.Location
	if loc == time.Local {
		loc = t.Location()
	}
	if s.Location != time.Local {
		t = t.In(s.Location)
	}

	// Start at the earliest possible time (the upcoming second).
	t = t.Add(1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)

	// This flag indicates whether a field has been incremented.
	added := false

	// If no time is found within five years, return zero.
	yearLimit := t.Year() + 5

WRAP:
	if t.Year() > yearLimit || t.Year() > maxYear {
		return time.Time{}
	}

	for t.Year() < minYear || s.Year.Bit(t.Year()-minYear) == 0 {
		if !added {
			added = true
			t = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(1, 0, 0)
		if t.Year() > yearLimit || t.Year() > maxYear {
			return time.Time{}
		}
	}

	// Find the first applicable month.
	// If it's this month, then do nothing.
	for s.Month.Bit(int(t.Month())) == 0 {
		// If we have to add a month, reset the other parts to 0.
		if !added {
			added = true
			// Otherwise, set the date at the beginning (since the current time is irrelevant).
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 1, 0)

		// Wrapped around.
		if t.Month() == time.January {
			goto WRAP
		}
	}

	// Now get a day in that month.
	//
	// NOTE: This causes issues for daylight savings regimes where midnight does
	// not exist.  For example: Sao Paulo has DST that transforms midnight on
	// 11/3 into 1am. Handle that by noticing when the Hour ends up != 0.
	for !dayMatches(s, t) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 0, 1)
		// Notice if the hour is no longer midnight due to DST.
		// Add an hour if it's 23, subtract an hour if it's 1.
		if t.Hour() != 0 {
			if t.Hour() > 12 {
				t = t.Add(time.Duration(24-t.Hour()) * time.Hour)
			} else {
				t = t.Add(time.Duration(-t.Hour()) * time.Hour)
			}
		}

		if t.Day() == 1 {
			goto WRAP
		}
	}

	for s.Hour.Bit(t.Hour()) == 0 {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		}
		t = t.Add(1 * time.Hour)

		if t.Hour() == 0 {
			goto WRAP
		}
	}

	for s.Minute.Bit(t.Minute()) == 0 {
		if !added {
			added = true
			t = t.Truncate(time.Minute)
		}
		t = t.Add(1 * time.Minute)

		if t.Minute() == 0 {
			goto WRAP
		}
	}

	for s.Second.Bit(t.Second()) == 0 {
		if !added {
			added = true
			t = t.Truncate(time.Second)
		}
		t = t.Add(1 * time.Second)

		if t.Second() == 0 {
			goto WRAP
		}
	}

	return t.In(origLocation)
}

// Latest returns the latest activation time, include the given time.
// This rounds so that the latest activation time will be on the second.
// If no time can be found to satisfy the schedule, return the zero time.
func (s *SpecSchedule) Latest(t time.Time) time.Time {
	// General approach
	//
	// For Month, Day, Hour, Minute, Second:
	// Check if the time value matches.  If yes, continue to the next field.
	// If the field doesn't match the schedule, then decrement the field until it matches.
	// While decrementing the field, a wrap-around brings it back to the beginning
	// of the field list (since it is necessary to re-verify previous field
	// values)

	// Convert the given time into the schedule's timezone, if one is specified.
	// Save the original timezone so we can convert back after we find a time.
	// Note that schedules without a time zone specified (time.Local) are treated
	// as local to the time provided.
	origLocation := t.Location()
	loc := s.Location
	if loc == time.Local {
		loc = t.Location()
	}
	if s.Location != time.Local {
		t = t.In(s.Location)
	}

	// Rounds the given time down to the second.
	t = t.Truncate(time.Second)

	// If no time is found within five years, return zero.
	yearLimit := t.Year() - 5

WRAP:
	if t.Year() < yearLimit || t.Year() < minYear {
		return time.Time{}
	}

	for t.Year() > maxYear || s.Year.Bit(t.Year()-minYear) == 0 {
		t = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc).Add(-time.Second)
		if t.Year() < yearLimit || t.Year() < minYear {
			return time.Time{}
		}
	}

	// Find the first applicable month.
	// If it's this month, then do nothing.
	for s.Month.Bit(int(t.Month())) == 0 {
		t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).Add(-time.Second)

		// Wrapped around.
		if t.Month() == time.December {
			goto WRAP
		}
	}

	// Now get a day in that month.
	//
	// NOTE: This causes issues for daylight savings regimes where midnight does
	// not exist.  For example: Sao Paulo has DST that transforms midnight on
	// 11/3 into 1am. Handle that by noticing when the Hour ends up != 0.
	for !dayMatches(s, t) {
		var needWrap bool
		if t.Day() == 1 {
			needWrap = true
		}
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Add(-time.Second)
		if needWrap {
			goto WRAP
		}
	}

	for s.Hour.Bit(t.Hour()) == 0 {
		var needWrap bool
		if t.Hour() == 0 {
			needWrap = true
		}
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).Add(-time.Second)
		if needWrap {
			goto WRAP
		}
	}

	for s.Minute.Bit(t.Minute()) == 0 {
		var needWrap bool
		if t.Minute() == 0 {
			needWrap = true
		}
		t = t.Truncate(time.Minute).Add(-time.Second)
		if needWrap {
			goto WRAP
		}
	}

	for s.Second.Bit(t.Second()) == 0 {
		var needWrap bool
		if t.Second() == 0 {
			needWrap = true
		}
		t = t.Truncate(time.Second).Add(-time.Second)
		if needWrap {
			goto WRAP
		}
	}

	return t.In(origLocation)
}

// dayMatches returns true if the schedule's day-of-week and day-of-month
// restrictions are satisfied by the given time.
func dayMatches(s *SpecSchedule, t time.Time) bool {
	var (
		domMatch bool = s.Dom.Bit(t.Day()) > 0
		dowMatch bool = s.Dow.Bit(int(t.Weekday())) > 0
	)
	
	eom, eowd := eomBits(s, t)
	if eom > 0 {
		domMatch = domMatch || (1<<uint(t.Day())&eom > 0)
	}
	if eowd > 0 {
		dowMatch = dowMatch || (1<<uint(t.Day())&eowd > 0)
	}
	if s.Dom.Bit(maxBits) > 0 || s.Dow.Bit(maxBits) > 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

// basically EOM(to EOM - 7) flag is stored in bits 55 - 48 of SpecSchedule's Dom
// you just need to know what date of t's eom, and shift bits 55 - 48 (0x00FF_0000_0000_0000) to that position
func eomBits(s *SpecSchedule, t time.Time) (uint64, uint64) {
	bDow := byte(s.Dow.Bit(0x00FE000000000000) >> (6 * 8))
	if s.Dom.Bit(0x00FF000000000000) == 0 && bDow == 0 {
		return 0, 0
	}
	eom := byte(30)
	year := t.Year()
	leapYear := byte(0)
	if (year%4 == 0 && year%100 != 0) || year%400 == 0 {
		leapYear = 1
	}

	switch t.Month() {
	case time.April, time.June, time.September, time.November:
	case time.February:
		eom = 28 + leapYear
	default:
		eom = 31
	}

	dowBits := uint64(0)
	if bDow > 0 {
		lastDayOfWeek := time.Date(t.Year(), t.Month(), int(eom), 0, 0, 0, 0, t.Location()).Weekday()
		switch lastDayOfWeek {
		case 0:
			bDow = (0xFC&bDow)>>1 | (bDow << 6)
		case 1:
			bDow = (0xF8&bDow)>>2 | (bDow << 5)
		case 2:
			bDow = (0xF0&bDow)>>3 | (bDow << 4)
		case 3:
			bDow = (0xE0&bDow)>>4 | (bDow << 3)
		case 4:
			bDow = (0xC0&bDow)>>5 | (bDow << 2)
		case 5:
			bDow = (0x80&bDow)>>6 | (bDow << 1)
		default: // actaually case 6 // which is doing nothing
		}
		dowBits = uint64(bDow) << (6 * 8)
	}
	return s.Dom.Bit(0x00FF000000000000) >> (uint64(55) - uint64(eom)), dowBits >> (uint64(55) - uint64(eom))
}
