package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
	"github.com/jroedel/google-scripts-mcp/internal/gas/gastest"
	"github.com/jroedel/google-scripts-mcp/internal/tools"
)

// connect runs the server in memory and returns a client session on it, the
// way Claude Code would see it.
func connect(t *testing.T, d *tools.Deps) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tools.Register(s, d)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return res, text.String()
}

func withFake(t *testing.T, answers map[string]any) *tools.Deps {
	_, c := gastest.Start(t, answers)
	return &tools.Deps{
		Dir: t.TempDir(),
		NewClient: func(context.Context) (*gas.Client, string, error) {
			return c, "someone@example.com", nil
		},
	}
}

func TestEveryToolIsListed(t *testing.T) {
	cs := connect(t, withFake(t, nil))
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"scripts_inventory", "script_get", "script_runs", "script_metrics", "script_pull", "script_remember", "script_forget"} {
		if !got[want] {
			t.Errorf("%s is not listed", want)
		}
	}
}

func TestNotSignedInSaysWhatToDo(t *testing.T) {
	d := &tools.Deps{Dir: t.TempDir(), NewClient: func(context.Context) (*gas.Client, string, error) {
		return nil, "", errors.New("not signed in. Run: google-scripts-mcp login -account <gmail address>")
	}}
	res, text := call(t, connect(t, d), "scripts_inventory", nil)
	if !res.IsError || !strings.Contains(text, "login") {
		t.Errorf("want a tool error mentioning login, got %v %q", res.IsError, text)
	}
}

func TestRememberThenInventory(t *testing.T) {
	d := withFake(t, map[string]any{
		"drive/files":                          map[string]any{},
		"script/projects/bnd":                  gas.Project{ScriptID: "bnd", Title: "Intentions import"},
		"script/projects/bnd/deployments":      map[string]any{},
		"script/processes:listScriptProcesses": map[string]any{},
		"script/processes":                     map[string]any{},
	})
	cs := connect(t, d)

	if res, text := call(t, cs, "script_remember", map[string]any{"script_id": "bnd", "note": "in the intentions sheet"}); res.IsError {
		t.Fatal(text)
	}
	res, text := call(t, cs, "scripts_inventory", map[string]any{"days": 7})
	if res.IsError {
		t.Fatal(text)
	}
	var out struct {
		Account string `json:"account"`
		Scripts []struct {
			ScriptID string `json:"script_id"`
			Title    string `json:"title"`
		} `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Account != "someone@example.com" || len(out.Scripts) != 1 || out.Scripts[0].Title != "Intentions import" {
		t.Errorf("inventory: %s", text)
	}

	if res, text := call(t, cs, "script_remember", map[string]any{"script_id": "nope"}); !res.IsError {
		t.Errorf("remembered an id Google does not know: %s", text)
	}
}

func TestPullThroughTheTool(t *testing.T) {
	d := withFake(t, map[string]any{
		"script/projects/abc": gas.Project{ScriptID: "abc", Title: "Budget"},
		"script/projects/abc/content": gas.Content{ScriptID: "abc", Files: []gas.File{
			{Name: "Code", Type: gas.FileServerJS, Source: "function a() {}"},
			{Name: "appsscript", Type: gas.FileJSON, Source: "{}"},
		}},
	})
	dir := filepath.Join(t.TempDir(), "budget")
	res, text := call(t, connect(t, d), "script_pull", map[string]any{"script_id": "abc", "dir": dir})
	if res.IsError {
		t.Fatal(text)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Code.gs")); err != nil || string(data) != "function a() {}" {
		t.Errorf("Code.gs = %q, %v", data, err)
	}
}

func TestRunsNeedsAScript(t *testing.T) {
	res, text := call(t, connect(t, withFake(t, nil)), "script_runs", map[string]any{})
	if !res.IsError || !strings.Contains(text, "script_id") {
		t.Errorf("got %v %q", res.IsError, text)
	}
}
