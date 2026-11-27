package app

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Working days for deadlines that can't be missed (the 2026-10-14
// recommended structure decision, 8): a calendar's weekend days and public
// holidays, substitute and temporary holidays among them, kept by year in
// business_calendars.json, which the web's formulas read too
// (scripts/generate-business-calendar.mjs). South Korea's comes first, from
// the government's rules (관공서의 공휴일에 관한 규정) and its temporary
// holidays; a release adds the years and days announced since. A year the
// calendar doesn't hold counts its weekends alone, which the calendar says
// (covers).

//go:embed business_calendars.json
var businessCalendarsJSON []byte

const businessCalendarDefault = "KR"

type businessCalendar struct {
	Name     map[string]string      `json:"name"`
	Source   string                 `json:"source"`
	Weekend  []int                  `json:"weekend"`
	Years    map[string][][2]string `json:"years"`
	holidays map[string]string
}

var loadBusinessCalendars = sync.OnceValues(func() (map[string]*businessCalendar, error) {
	calendars := map[string]*businessCalendar{}
	if err := json.Unmarshal(businessCalendarsJSON, &calendars); err != nil {
		return nil, err
	}
	for _, calendar := range calendars {
		calendar.holidays = map[string]string{}
		for _, days := range calendar.Years {
			for _, day := range days {
				calendar.holidays[day[0]] = day[1]
			}
		}
	}
	return calendars, nil
})

// businessCalendarNamed is the calendar a code names, the default for "".
func businessCalendarNamed(code string) (*businessCalendar, bool) {
	calendars, err := loadBusinessCalendars()
	if err != nil {
		return nil, false
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		code = businessCalendarDefault
	}
	calendar, found := calendars[code]
	return calendar, found
}

// holiday is the name of the public holiday a day is, "" for none.
func (calendar *businessCalendar) holiday(day time.Time) string {
	return calendar.holidays[day.Format(time.DateOnly)]
}

// workday says whether a day is a working day: no weekend day, no holiday.
func (calendar *businessCalendar) workday(day time.Time) bool {
	for _, weekday := range calendar.Weekend {
		if int(day.Weekday()) == weekday {
			return false
		}
	}
	return calendar.holiday(day) == ""
}

// covers says whether the calendar holds a year's holidays.
func (calendar *businessCalendar) covers(year int) bool {
	_, held := calendar.Years[strconv.Itoa(year)]
	return held
}

// nextWorkday is the day itself when it is a working day, else the first
// working day after it.
func (calendar *businessCalendar) nextWorkday(day time.Time) time.Time {
	for steps := 0; steps < 366 && !calendar.workday(day); steps++ {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

// addWorkdays is the day count working days after a day, or before it for
// a negative count; zero leaves the day as it is.
func (calendar *businessCalendar) addWorkdays(day time.Time, count int) time.Time {
	step := 1
	if count < 0 {
		step, count = -1, -count
	}
	for count > 0 {
		day = day.AddDate(0, 0, step)
		if calendar.workday(day) {
			count--
		}
	}
	return day
}
