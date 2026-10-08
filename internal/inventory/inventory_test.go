package inventory_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
	"github.com/jroedel/google-scripts-mcp/internal/gas/gastest"
	"github.com/jroedel/google-scripts-mcp/internal/inventory"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func run(name, fn, typ, status string, ago time.Duration) gas.Process {
	return gas.Process{ProjectName: name, FunctionName: fn, ProcessType: typ, ProcessStatus: status, StartTime: t0.Add(-ago)}
}

// A standalone script in Drive, a remembered bound script, and a third that
// only shows up in the account's run history.
func fakeGoogle() map[string]any {
	return map[string]any{
		"drive/files": map[string]any{"files": []map[string]any{
			{"id": "std", "name": "Newsletter sender", "ownedByMe": true, "modifiedTime": "2025-01-02T00:00:00Z"},
		}},
		"script/projects/std":             gas.Project{ScriptID: "std", Title: "Newsletter sender"},
		"script/projects/std/deployments": map[string]any{"deployments": []map[string]any{{"deploymentId": "HEAD", "deploymentConfig": map[string]any{}}}},
		"script/projects/bnd":             gas.Project{ScriptID: "bnd", Title: "Intentions import", ParentID: "sheet1"},
		"script/projects/bnd/deployments": map[string]any{"deployments": []map[string]any{{
			"deploymentId":     "AKfy",
			"deploymentConfig": map[string]any{"versionNumber": 3, "description": "form handler"},
			"entryPoints": []map[string]any{{
				"entryPointType": "WEB_APP",
				"webApp":         map[string]any{"url": "https://script.google.com/macros/s/AKfy/exec", "entryPointConfig": map[string]any{"access": "ANYONE_ANONYMOUS", "executeAs": "USER_DEPLOYING"}},
			}},
		}}},
		"drive/files/sheet1":                   map[string]any{"id": "sheet1", "name": "Mass intentions 2026", "mimeType": "application/vnd.google-apps.spreadsheet", "ownedByMe": true},
		"script/processes:listScriptProcesses": scriptRuns,
	}
}

func TestBuild(t *testing.T) {
	answers := fakeGoogle()
	answers["script/processes"] = map[string]any{"processes": []gas.Process{
		run("Intentions import", "onSubmit", "TRIGGER", "COMPLETED", time.Hour),
		run("Old sidebar", "showSidebar", "MENU", "COMPLETED", 20*24*time.Hour),
	}}
	_, c := gastest.Start(t, answers)

	rep, err := inventory.Build(t.Context(), c, []string{"bnd", "std"}, t0.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Scripts) != 2 {
		t.Fatalf("got %d scripts, want 2 (std listed by Drive and remembered is one script)", len(rep.Scripts))
	}
	bound := rep.Scripts[0] // ran most recently
	if bound.ScriptID != "bnd" || bound.Kind != "bound" || bound.Container == nil || bound.Container.Name != "Mass intentions 2026" {
		t.Errorf("bound script: %+v", bound)
	}
	if len(bound.Deployments) != 1 || bound.Deployments[0].Access != "ANYONE_ANONYMOUS" {
		t.Errorf("deployments: %+v", bound.Deployments)
	}
	if !bound.Runs.Triggered || bound.Runs.Functions[0].Name != "onSubmit" || bound.Runs.Functions[0].Failures != 1 {
		t.Errorf("runs: %+v", bound.Runs)
	}

	std := rep.Scripts[1]
	if std.Kind != "standalone" || std.Runs.Total != 0 || len(std.Deployments) != 0 {
		t.Errorf("standalone script: %+v", std)
	}

	// "Intentions import" is known by its title; only "Old sidebar" is a stranger.
	if len(rep.Unidentified) != 1 || rep.Unidentified[0].ProjectName != "Old sidebar" {
		t.Errorf("unidentified: %+v", rep.Unidentified)
	}
	if want := t0.Add(-20 * 24 * time.Hour); !rep.EarliestRun.Equal(want) {
		t.Errorf("earliest run %v, want %v", rep.EarliestRun, want)
	}
}

// scriptRuns answers listScriptProcesses: three runs for bnd, one of them
// failed, and none for std.
func scriptRuns(q url.Values) any {
	if q.Get("scriptId") != "bnd" {
		return map[string]any{}
	}
	return map[string]any{"processes": []gas.Process{
		run("Intentions import", "onSubmit", "TRIGGER", "COMPLETED", time.Hour),
		run("Intentions import", "onSubmit", "TRIGGER", "FAILED", 2*time.Hour),
		run("Intentions import", "doGet", "WEBAPP", "COMPLETED", 3*time.Hour),
	}}
}

func TestSummariseOrdersFailingFunctionsFirst(t *testing.T) {
	r := inventory.Summarise([]gas.Process{
		run("p", "busy", "TIME_DRIVEN", "COMPLETED", time.Hour),
		run("p", "busy", "TIME_DRIVEN", "COMPLETED", 2*time.Hour),
		run("p", "busy", "TIME_DRIVEN", "COMPLETED", 3*time.Hour),
		run("p", "flaky", "EDITOR", "TIMED_OUT", 4*time.Hour),
	}, false)
	if r.Functions[0].Name != "flaky" || r.Functions[0].Failures != 1 {
		t.Errorf("functions: %+v", r.Functions)
	}
	if !r.Triggered || r.ByType["TIME_DRIVEN"] != 3 || !r.Last.Equal(t0.Add(-time.Hour)) {
		t.Errorf("summary: %+v", r)
	}
}
