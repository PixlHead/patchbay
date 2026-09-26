package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

type fakeNextRuns map[string]time.Time

func (f fakeNextRuns) NextRun(workflowID string) (time.Time, bool) {
	next, ok := f[workflowID]
	return next, ok
}

func TestWorkflowListIncludesScheduleAndNextRun(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	runner, err := engine.New(1, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	scheduled := testDefinitions()[0]
	scheduled.ID = "scheduled"
	scheduled.Schedule = &workflow.Schedule{Cron: "*/5 * * * *", Timezone: "UTC"}
	definitions := append(testDefinitions(), scheduled)
	next := time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)

	type listed struct {
		ID        string             `json:"id"`
		Schedule  *workflow.Schedule `json:"schedule"`
		NextRunAt *time.Time         `json:"nextRunAt"`
	}
	list := func(schedules NextRunSource) []listed {
		t.Helper()
		handler := New(definitions, runner, schedules, db, t.TempDir())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET /api/workflows returned %d: %s", response.Code, response.Body.String())
		}
		var workflows []listed
		if err := json.Unmarshal(response.Body.Bytes(), &workflows); err != nil {
			t.Fatal(err)
		}
		if len(workflows) != 2 || workflows[0].ID != "test-workflow" || workflows[1].ID != "scheduled" {
			t.Fatalf("workflow list changed shape: %+v", workflows)
		}
		return workflows
	}

	workflows := list(fakeNextRuns{"scheduled": next})
	if workflows[0].Schedule != nil || workflows[0].NextRunAt != nil {
		t.Fatalf("a manual workflow must not report a schedule or next run: %+v", workflows[0])
	}
	if workflows[1].Schedule == nil || workflows[1].Schedule.Cron != "*/5 * * * *" || workflows[1].Schedule.Timezone != "UTC" {
		t.Fatalf("schedule was not listed: %+v", workflows[1])
	}
	if workflows[1].NextRunAt == nil || !workflows[1].NextRunAt.Equal(next) {
		t.Fatalf("want next run %s, got %v", next, workflows[1].NextRunAt)
	}

	// Without a source, the same workflows list without next-run times.
	for _, w := range list(nil) {
		if w.NextRunAt != nil {
			t.Fatalf("nil source reported a next run: %+v", w)
		}
	}
}
