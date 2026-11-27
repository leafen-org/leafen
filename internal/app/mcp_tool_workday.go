package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A deadline counted on working days for an agent (the 2026-10-14
// recommended structure decision, 8), the same count the formulas'
// nextWorkday and addWorkdays make (business_calendar.go): a date plus
// calendar days, moved to the next working day when it falls on a weekend
// or a public holiday, or plus working days. It names the days it skipped
// and says when a year it crossed isn't in the calendar, where weekends
// alone were counted.

type leafenMCPWorkdayInput struct {
	Date     string `json:"date" jsonschema:"The day to count from, YYYY-MM-DD (a served or received date)."`
	Days     int    `json:"days,omitempty" jsonschema:"Calendar days to add (negative counts back); the last day moves to the next working day. Give days or workdays, not both."`
	Workdays int    `json:"workdays,omitzero" jsonschema:"Working days to add (negative counts back), skipping weekends and holidays."`
	Calendar string `json:"calendar,omitempty" jsonschema:"The calendar's code; KR, South Korea's public holidays, when empty."`
}

type leafenMCPSkippedDay struct {
	Date    string `json:"date"`
	Holiday string `json:"holiday,omitempty"`
}

type leafenMCPWorkdayOutput struct {
	Date     string                `json:"date"`
	Calendar string                `json:"calendar"`
	Skipped  []leafenMCPSkippedDay `json:"skipped"`
	// Covered is false when a year the count crossed isn't in the calendar.
	Covered bool   `json:"covered"`
	Note    string `json:"note,omitempty"`
}

func leafenMCPWorkday(input leafenMCPWorkdayInput) (leafenMCPWorkdayOutput, error) {
	start, err := time.Parse(time.DateOnly, strings.TrimSpace(input.Date))
	if err != nil {
		return leafenMCPWorkdayOutput{}, errors.New("date must be YYYY-MM-DD")
	}
	if input.Days != 0 && input.Workdays != 0 {
		return leafenMCPWorkdayOutput{}, errors.New("give days or workdays, not both")
	}
	if input.Days > 3660 || input.Days < -3660 || input.Workdays > 3660 || input.Workdays < -3660 {
		return leafenMCPWorkdayOutput{}, errors.New("count at most ten years")
	}
	calendar, found := businessCalendarNamed(input.Calendar)
	if !found {
		return leafenMCPWorkdayOutput{}, errors.New("Leafen has no calendar of that code; KR is South Korea's")
	}
	code := strings.ToUpper(strings.TrimSpace(input.Calendar))
	if code == "" {
		code = businessCalendarDefault
	}
	end := start
	if input.Workdays != 0 {
		end = calendar.addWorkdays(start, input.Workdays)
	} else {
		end = calendar.nextWorkday(start.AddDate(0, 0, input.Days))
	}
	output := leafenMCPWorkdayOutput{Date: end.Format(time.DateOnly), Calendar: code, Skipped: []leafenMCPSkippedDay{}, Covered: true}
	nominal := start.AddDate(0, 0, input.Days)
	from, to := start, end
	for _, day := range []time.Time{nominal, end} {
		if day.Before(from) {
			from = day
		}
		if day.After(to) {
			to = day
		}
	}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		output.Covered = output.Covered && calendar.covers(day.Year())
		if day.Equal(start) || calendar.workday(day) {
			continue
		}
		// What counting stepped over: every closed day for working days, the
		// closed days the last day moved past for calendar days.
		if input.Workdays != 0 || !day.Before(nominal) && day.Before(end) {
			output.Skipped = append(output.Skipped, leafenMCPSkippedDay{Date: day.Format(time.DateOnly), Holiday: calendar.holiday(day)})
		}
	}
	if !output.Covered {
		output.Note = "A year this count crossed isn't in Leafen's calendar yet, so only its weekends were skipped: check its holidays before relying on the date."
	}
	return output, nil
}

func registerMCPWorkdayTool(server *mcp.Server, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_workday", Title: "Count a deadline on working days",
		Description: "Counts a deadline: a date plus calendar days, moved to the next working day when the last day is a weekend or public holiday (as a served date + 14 days), or a date plus working days. South Korea's holidays, substitute and temporary ones included, unless another calendar's code is given. " +
			"The same count as the formulas nextWorkday() and addWorkdays(). Put the result in the item's date and turn on its can't-miss deadline.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, input leafenMCPWorkdayInput) (*mcp.CallToolResult, leafenMCPWorkdayOutput, error) {
		output, err := leafenMCPWorkday(input)
		return nil, output, err
	})
}
