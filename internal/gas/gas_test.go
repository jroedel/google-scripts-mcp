package gas_test

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
	"github.com/jroedel/google-scripts-mcp/internal/gas/gastest"
)

func TestErrorsCarryAHint(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"per-account switch",
			`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"User has not enabled the Apps Script API. Enable it by visiting https://script.google.com/home/usersettings then retry."}}`,
			"script.google.com/home/usersettings",
		},
		{
			"per-project switch",
			`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Apps Script API has not been used in project 123 before or it is disabled.","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED"}]}}`,
			"APIs & Services > Library",
		},
		{
			"unticked scope",
			`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Request had insufficient authentication scopes.","details":[{"reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`,
			"leave every box ticked",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := gastest.Start(t, map[string]any{"script/projects/x": gastest.Status{Code: 403, Body: tc.body}})
			_, err := c.Project(t.Context(), "x")
			apiErr, ok := errors.AsType[*gas.APIError](err)
			if !ok {
				t.Fatalf("got %v, want an *APIError", err)
			}
			if !strings.Contains(apiErr.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", apiErr.Error(), tc.want)
			}
		})
	}
}

func TestNonJSONErrorIsKeptShort(t *testing.T) {
	_, c := gastest.Start(t, map[string]any{"script/projects/x": gastest.Status{Code: 502, Body: strings.Repeat("<html>", 200)}})
	_, err := c.Project(t.Context(), "x")
	if err == nil || len(err.Error()) > 400 {
		t.Fatalf("want a short error, got %d bytes", len(err.Error()))
	}
}

// processPages serves n runs, pageSize at a time, newest first.
func processPages(n int) func(url.Values) any {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return func(q url.Values) any {
		offset := 0
		if tok := q.Get("pageToken"); tok != "" {
			offset = len(tok)
		}
		size := 200
		var ps []gas.Process
		for i := offset; i < min(offset+size, n); i++ {
			ps = append(ps, gas.Process{FunctionName: "f", ProcessStatus: "COMPLETED", StartTime: start.Add(-time.Duration(i) * time.Minute)})
		}
		page := map[string]any{"processes": ps}
		if offset+size < n {
			page["nextPageToken"] = strings.Repeat("x", offset+size)
		}
		return page
	}
}

func TestProcessesStopAtTheCap(t *testing.T) {
	f, c := gastest.Start(t, map[string]any{"script/processes:listScriptProcesses": processPages(450)})

	page, err := c.ScriptProcesses(t.Context(), "abc", gas.ProcessFilter{
		Max:      300,
		Since:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Statuses: []string{"FAILED", "TIMED_OUT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Processes) != 300 || !page.Truncated {
		t.Errorf("got %d runs, truncated=%v; want 300, true", len(page.Processes), page.Truncated)
	}

	q := f.Queries("script/processes:listScriptProcesses")[0]
	if q.Get("scriptId") != "abc" || q.Get("scriptProcessFilter.startTime") != "2026-09-01T00:00:00Z" {
		t.Errorf("query %v", q)
	}
	if got := q["scriptProcessFilter.statuses"]; !slices.Equal(got, []string{"FAILED", "TIMED_OUT"}) {
		t.Errorf("statuses sent as %v", got)
	}
}

func TestProcessesUnderTheCapAreComplete(t *testing.T) {
	_, c := gastest.Start(t, map[string]any{"script/processes": processPages(250)})
	page, err := c.UserProcesses(t.Context(), gas.ProcessFilter{Max: 500, ProjectName: "Budget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Processes) != 250 || page.Truncated {
		t.Errorf("got %d runs, truncated=%v; want 250, false", len(page.Processes), page.Truncated)
	}
}

func TestMetricsValuesAreStrings(t *testing.T) {
	// The API sends counts as JSON strings; a plain uint64 field would fail
	// to decode the real answer.
	_, c := gastest.Start(t, map[string]any{"script/projects/abc/metrics": gastest.Status{Code: 200, Body: `{
		"totalExecutions":[{"value":"42","startTime":"2026-10-01T00:00:00Z","endTime":"2026-10-08T00:00:00Z"}],
		"failedExecutions":[{"startTime":"2026-10-01T00:00:00Z","endTime":"2026-10-08T00:00:00Z"}]}`}})
	m, err := c.Metrics(t.Context(), "abc", "WEEKLY")
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalExecutions[0].Value != 42 || m.FailedExecutions[0].Value != 0 {
		t.Errorf("got %+v", m)
	}
}
