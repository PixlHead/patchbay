package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule is a workflow's optional cron trigger. A workflow with a schedule
// can still be started by hand.
type Schedule struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

// Timetable computes a validated schedule's occurrences in the schedule's time zone.
type Timetable struct {
	schedule cron.Schedule
	location *time.Location
}

// A five-field cron expression cannot repeat more often than once a minute.
// The same floor applies to "@every" descriptors, which the parser also accepts.
const minimumInterval = time.Minute

// NewTimetable parses a schedule. Each error names the schedule field at fault.
func NewTimetable(s Schedule) (Timetable, error) {
	if strings.TrimSpace(s.Cron) == "" {
		return Timetable{}, fmt.Errorf("schedule needs a cron expression")
	}
	if strings.Contains(s.Cron, "TZ=") {
		return Timetable{}, fmt.Errorf("schedule cron must not contain TZ=; set the timezone field instead")
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return Timetable{}, fmt.Errorf("schedule cron %q is invalid: %w", s.Cron, err)
	}
	if s.Timezone == "" || s.Timezone == "Local" {
		return Timetable{}, fmt.Errorf("schedule needs an explicit timezone name such as UTC or Europe/Amsterdam")
	}
	location, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return Timetable{}, fmt.Errorf("schedule timezone %q is unknown", s.Timezone)
	}
	timetable := Timetable{schedule: parsed, location: location}
	first := timetable.Next(time.Now())
	if first.IsZero() {
		return Timetable{}, fmt.Errorf("schedule cron %q has no occurrence in the next five years", s.Cron)
	}
	if second := timetable.Next(first); second.Sub(first) < minimumInterval {
		return Timetable{}, fmt.Errorf("schedule cron %q repeats more often than once a minute", s.Cron)
	}
	return timetable, nil
}

// Next returns the first occurrence after the given instant, expressed in the
// schedule's time zone. The zero time means no occurrence within five years.
func (t Timetable) Next(after time.Time) time.Time {
	return t.schedule.Next(after.In(t.location))
}
