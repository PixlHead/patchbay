package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateSchedule(t *testing.T) {
	scheduled := validDefinition()
	scheduled.Schedule = &Schedule{Cron: "*/5 * * * *", Timezone: "UTC"}
	if err := Validate(scheduled); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		schedule Schedule
		want     string
	}{
		{"empty object", Schedule{}, "cron"},
		{"words instead of fields", Schedule{Cron: "every five minutes", Timezone: "UTC"}, "cron"},
		{"six fields", Schedule{Cron: "0 */5 * * * *", Timezone: "UTC"}, "cron"},
		{"timezone inside cron", Schedule{Cron: "CRON_TZ=UTC */5 * * * *", Timezone: "UTC"}, "TZ="},
		{"missing timezone", Schedule{Cron: "*/5 * * * *"}, "timezone"},
		{"implicit local timezone", Schedule{Cron: "*/5 * * * *", Timezone: "Local"}, "timezone"},
		{"unknown timezone", Schedule{Cron: "*/5 * * * *", Timezone: "Mars/Olympus_Mons"}, "timezone"},
		{"sub-minute interval", Schedule{Cron: "@every 10s", Timezone: "UTC"}, "once a minute"},
		{"date that never exists", Schedule{Cron: "0 0 30 2 *", Timezone: "UTC"}, "no occurrence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := validDefinition()
			schedule := test.schedule
			d.Schedule = &schedule
			err := Validate(d)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want an error mentioning %q, got %v", test.want, err)
			}
		})
	}
}

func TestTimetableNextUsesTheScheduleTimezone(t *testing.T) {
	timetable, err := NewTimetable(Schedule{Cron: "0 9 * * *", Timezone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	// 12:00 UTC is 07:00 in New York, so the next 09:00 there falls on the same day.
	got := timetable.Next(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	want := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("want %s, got %s", want, got)
	}
	if got.Location().String() != "America/New_York" {
		t.Fatalf("occurrence should be expressed in the schedule's zone, got %s", got.Location())
	}
}

// In 2026, New York moves clocks forward on March 8 and back on November 1.
// The expected instants follow the cron library's documented wall-clock search.
func TestTimetableNextAcrossDaylightSavingChanges(t *testing.T) {
	t.Run("a skipped wall-clock time waits for the next day", func(t *testing.T) {
		timetable, err := NewTimetable(Schedule{Cron: "30 2 * * *", Timezone: "America/New_York"})
		if err != nil {
			t.Fatal(err)
		}
		// Midnight EST on March 8; 02:30 does not exist that day.
		got := timetable.Next(time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC))
		want := time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC) // 02:30 EDT on March 9
		if !got.Equal(want) {
			t.Fatalf("want %s, got %s", want, got)
		}
	})
	t.Run("a repeated wall-clock time runs twice", func(t *testing.T) {
		timetable, err := NewTimetable(Schedule{Cron: "30 1 * * *", Timezone: "America/New_York"})
		if err != nil {
			t.Fatal(err)
		}
		// Midnight EDT on November 1; 01:30 happens once in EDT and once in EST.
		first := timetable.Next(time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC))
		wantFirst := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) // 01:30 EDT
		if !first.Equal(wantFirst) {
			t.Fatalf("first occurrence: want %s, got %s", wantFirst, first)
		}
		second := timetable.Next(first)
		wantSecond := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) // 01:30 EST
		if !second.Equal(wantSecond) {
			t.Fatalf("second occurrence: want %s, got %s", wantSecond, second)
		}
	})
}

func TestLoadPreservesScheduleAndRejectsEmptyOne(t *testing.T) {
	steps := `[{"id":"check","name":"Check","type":"http.check","config":{"url":"http://localhost/health","expectedStatus":200,"timeoutMs":1000}}]`
	dir := t.TempDir()
	content := `{"schemaVersion":1,"id":"scheduled","name":"Scheduled","description":"","schedule":{"cron":"*/5 * * * *","timezone":"Europe/Amsterdam"},"steps":` + steps + `}`
	if err := os.WriteFile(filepath.Join(dir, "scheduled.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	definitions, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := validDefinition()
	want.ID, want.Name = "scheduled", "Scheduled"
	want.Schedule = &Schedule{Cron: "*/5 * * * *", Timezone: "Europe/Amsterdam"}
	if !reflect.DeepEqual(definitions, []Definition{want}) {
		t.Fatalf("loaded definition differs from the file: %+v", definitions[0])
	}

	// An empty schedule object is a configuration mistake, not a manual workflow.
	dir = t.TempDir()
	content = `{"schemaVersion":1,"id":"empty-schedule","name":"Empty schedule","schedule":{},"steps":` + steps + `}`
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "cron") {
		t.Fatalf("want a cron error for an empty schedule, got %v", err)
	}
}
