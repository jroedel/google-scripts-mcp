// Package gastest is a fake of the Apps Script and Drive endpoints gas calls,
// for tests in this module. It serves canned JSON by path and records the
// queries it was asked, so a test can say what was sent as well as what came
// back.
package gastest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
)

// Fake answers by path. A path with no answer is a 404 in Google's error
// shape.
type Fake struct {
	// Answers maps "drive/files", "script/projects/abc" and the like to the
	// value to send back as JSON. A func(url.Values) any computes the answer
	// from the query, for paging.
	Answers map[string]any

	mu      sync.Mutex
	queries map[string][]url.Values
}

// Start runs the fake and returns a client pointed at it.
func Start(t *testing.T, answers map[string]any) (*Fake, *gas.Client) {
	t.Helper()
	f := &Fake{Answers: answers, queries: map[string][]url.Values{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := &gas.Client{HTTP: srv.Client(), ScriptBase: srv.URL + "/script/", DriveBase: srv.URL + "/drive/"}
	return f, c
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	f.mu.Lock()
	f.queries[key] = append(f.queries[key], r.URL.Query())
	f.mu.Unlock()

	a, ok := f.Answers[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"code": 404, "status": "NOT_FOUND", "message": "Requested entity was not found.",
		}})
		return
	}
	if fn, ok := a.(func(url.Values) any); ok {
		a = fn(r.URL.Query())
	}
	if s, ok := a.(Status); ok {
		w.WriteHeader(s.Code)
		w.Write([]byte(s.Body))
		return
	}
	json.NewEncoder(w).Encode(a)
}

// Status is an answer with a status code and a raw body.
type Status struct {
	Code int
	Body string
}

// Queries returns what was asked of a path, in order.
func (f *Fake) Queries(key string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[key]
}
