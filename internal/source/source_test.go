package source_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
	"github.com/jroedel/google-scripts-mcp/internal/source"
)

func content(files ...gas.File) gas.Content {
	return gas.Content{ScriptID: "abc", Files: files}
}

var (
	code     = gas.File{Name: "Code", Type: gas.FileServerJS, Source: "function main() {}\n"}
	page     = gas.File{Name: "ui/Sidebar", Type: gas.FileHTML, Source: "<p>hi</p>\n"}
	manifest = gas.File{Name: "appsscript", Type: gas.FileJSON, Source: `{"timeZone":"America/Chicago"}`}
)

func TestPullWritesFilesAndRecord(t *testing.T) {
	dir := t.TempDir()
	got, err := source.Pull(dir, gas.Project{Title: "Budget"}, content(code, page, manifest))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Code.gs", "appsscript.json", "ui/Sidebar.html"}
	if !slices.Equal(got.Written, want) {
		t.Errorf("wrote %v, want %v", got.Written, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ui", "Sidebar.html"))
	if err != nil || string(data) != page.Source {
		t.Errorf("ui/Sidebar.html = %q, %v", data, err)
	}
	for _, f := range []string{source.RecordFile, ".clasp.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
}

func TestPullAgainRefreshesAndRemovesDeleted(t *testing.T) {
	dir := t.TempDir()
	if _, err := source.Pull(dir, gas.Project{}, content(code, page, manifest)); err != nil {
		t.Fatal(err)
	}
	newer := code
	newer.Source = "function main() { return 1 }\n"
	got, err := source.Pull(dir, gas.Project{}, content(newer, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Removed, []string{"ui/Sidebar.html"}) {
		t.Errorf("removed %v", got.Removed)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "Code.gs"))
	if string(data) != newer.Source {
		t.Errorf("Code.gs not refreshed: %q", data)
	}
}

func TestPullRefusesToOverwriteAnEdit(t *testing.T) {
	dir := t.TempDir()
	if _, err := source.Pull(dir, gas.Project{}, content(code, manifest)); err != nil {
		t.Fatal(err)
	}
	edit := "function main() { fixed() }\n"
	if err := os.WriteFile(filepath.Join(dir, "Code.gs"), []byte(edit), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := source.Pull(dir, gas.Project{}, content(code, manifest))
	if err == nil || !strings.Contains(err.Error(), "Code.gs") {
		t.Fatalf("want a refusal naming Code.gs, got %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "Code.gs"))
	if string(data) != edit {
		t.Error("the edit was overwritten")
	}
}

func TestPullRefusesAForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)
	if _, err := source.Pull(dir, gas.Project{}, content(code)); err == nil {
		t.Fatal("pulled into a directory with somebody else's files in it")
	}

	// A fresh git repository is fine.
	dir = t.TempDir()
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	if _, err := source.Pull(dir, gas.Project{}, content(code)); err != nil {
		t.Fatalf("refused a directory holding only .git: %v", err)
	}

	// Another script's directory is not.
	other := content(code)
	other.ScriptID = "xyz"
	if _, err := source.Pull(dir, gas.Project{}, other); err == nil {
		t.Fatal("pulled a second script over the first")
	}
}

func TestPullRefusesNamesThatEscape(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/evil", `..\evil`} {
		dir := t.TempDir()
		_, err := source.Pull(dir, gas.Project{}, content(gas.File{Name: name, Type: gas.FileServerJS}))
		if err == nil {
			t.Errorf("wrote a file named %q", name)
		}
	}
}

func TestPullNeedsAnAbsolutePath(t *testing.T) {
	if _, err := source.Pull("relative/dir", gas.Project{}, content(code)); err == nil {
		t.Fatal("accepted a relative path")
	}
}
