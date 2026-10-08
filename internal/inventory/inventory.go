// Package inventory puts together, from the pieces the APIs offer, the one
// answer the person is after: which scripts exist, which of them still run,
// what starts them, and what fails.
//
// # Three sources, because no one of them is complete
//
// Drive lists standalone scripts with their ids. A local list of ids the
// person has supplied covers bound scripts, which Drive does not list. And the
// account's run history covers whatever ran but is in neither -- by name
// only, since a run does not carry its script's id. A bound script that has
// never been remembered and never runs is invisible to all three; it is also,
// by that description, doing nothing.
package inventory

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
)

// Script is one project, as much of it as could be learnt.
type Script struct {
	ScriptID  string     `json:"script_id"`
	Title     string     `json:"title"`
	Kind      string     `json:"kind"` // standalone or bound
	Container *Container `json:"container,omitempty"`
	OwnedByMe *bool      `json:"owned_by_me,omitempty"`
	Owner     string     `json:"owner,omitempty"`
	Created   time.Time  `json:"created,omitzero"`
	Updated   time.Time  `json:"updated,omitzero"`
	// UpdatedBy is the last person to edit the code, which on a shared script
	// is often the answer to "whose is this?".
	UpdatedBy   string       `json:"updated_by,omitempty"`
	EditorURL   string       `json:"editor_url"`
	Deployments []Deployment `json:"deployments,omitempty"`
	Runs        Runs         `json:"runs"`
	// Errors are the per-script calls that failed. One script being
	// unreadable does not fail the inventory.
	Errors []string `json:"errors,omitempty"`
}

// Container is the document a bound script lives in.
type Container struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
	Trashed  bool   `json:"trashed,omitempty"`
}

// Deployment is a deployment reduced to what decides whether retiring the
// script breaks something: is it a web app or an API endpoint, and who can
// reach it.
type Deployment struct {
	ID          string    `json:"id"`
	Version     int64     `json:"version,omitempty"` // zero is HEAD
	Description string    `json:"description,omitempty"`
	Updated     time.Time `json:"updated,omitzero"`
	EntryPoints []string  `json:"entry_points,omitempty"`
	WebAppURL   string    `json:"web_app_url,omitempty"`
	Access      string    `json:"access,omitempty"`
	ExecuteAs   string    `json:"execute_as,omitempty"`
}

// Runs summarises a script's run history in the window.
type Runs struct {
	Total int `json:"total"`
	// Truncated means the listing stopped at its cap, so the counts are a
	// lower bound and Last is still right (runs come newest first).
	Truncated bool           `json:"truncated,omitempty"`
	Last      time.Time      `json:"last,omitzero"`
	ByStatus  map[string]int `json:"by_status,omitempty"`
	ByType    map[string]int `json:"by_type,omitempty"`
	// Triggered is the evidence of an installed trigger, which the API
	// cannot list: a run started by a time-driven or event trigger.
	Triggered bool       `json:"triggered"`
	Functions []Function `json:"functions,omitempty"`
}

// Function is one entry point's history: which function fails is usually
// the first thing a bug hunt needs.
type Function struct {
	Name        string    `json:"name"`
	Runs        int       `json:"runs"`
	Failures    int       `json:"failures"`
	LastRun     time.Time `json:"last_run"`
	LastFailure time.Time `json:"last_failure,omitzero"`
	Types       []string  `json:"types"`
}

// Unidentified is a project seen running whose id is not known.
type Unidentified struct {
	ProjectName string `json:"project_name"`
	Runs        Runs   `json:"runs"`
}

// Report is the whole inventory.
type Report struct {
	Since   time.Time `json:"since"`
	Scripts []Script  `json:"scripts"`
	// Unidentified are almost always bound scripts. Open the document, then
	// Extensions > Apps Script, and give the id from the editor's URL to
	// script_remember.
	Unidentified []Unidentified `json:"unidentified,omitempty"`
	// EarliestRun is the oldest run any listing returned. Google does not
	// document how long run history is kept; if this is much later than
	// Since, that is the retention, and "no runs" means "none recently".
	EarliestRun time.Time `json:"earliest_run,omitzero"`
	Errors      []string  `json:"errors,omitempty"`
}

// Build assembles the inventory. remembered are script ids the person has
// supplied, typically bound scripts; ids that Drive also lists are not looked
// up twice.
func Build(ctx context.Context, c *gas.Client, remembered []string, since time.Time) (Report, error) {
	rep := Report{Since: since}

	standalone, err := c.ListStandalone(ctx)
	if err != nil {
		return Report{}, err
	}

	scripts := map[string]*Script{}
	for _, f := range standalone {
		owned := f.OwnedByMe
		s := &Script{
			ScriptID:  f.ID,
			Title:     f.Name,
			Kind:      "standalone",
			OwnedByMe: &owned,
			Created:   f.CreatedTime,
			Updated:   f.ModifiedTime,
		}
		if len(f.Owners) > 0 {
			s.Owner = f.Owners[0].EmailAddress
		}
		scripts[f.ID] = s
	}
	for _, id := range remembered {
		if _, ok := scripts[id]; !ok {
			scripts[id] = &Script{ScriptID: id}
		}
	}

	// Each script costs three or four requests. Six at a time is well inside
	// the per-user quota and keeps an inventory of fifty scripts to seconds.
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, 6)
		mu  sync.Mutex
	)
	for _, s := range scripts {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			earliest := fill(ctx, c, s, since)
			mu.Lock()
			rep.EarliestRun = earlier(rep.EarliestRun, earliest)
			mu.Unlock()
		})
	}

	// Meanwhile, the account's own runs, to find what nobody has named.
	var (
		mine    gas.ProcessPage
		mineErr error
	)
	wg.Go(func() {
		mine, mineErr = c.UserProcesses(ctx, gas.ProcessFilter{Since: since, Max: 2000})
	})
	wg.Wait()

	titles := map[string]bool{}
	for _, s := range scripts {
		titles[s.Title] = true
	}
	if mineErr != nil {
		rep.Errors = append(rep.Errors, "listing this account's runs: "+mineErr.Error())
	} else {
		byName := map[string][]gas.Process{}
		for _, p := range mine.Processes {
			rep.EarliestRun = earlier(rep.EarliestRun, p.StartTime)
			if !titles[p.ProjectName] {
				byName[p.ProjectName] = append(byName[p.ProjectName], p)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(byName)) {
			rep.Unidentified = append(rep.Unidentified, Unidentified{
				ProjectName: name,
				Runs:        Summarise(byName[name], mine.Truncated),
			})
		}
	}

	for _, s := range scripts {
		rep.Scripts = append(rep.Scripts, *s)
	}
	// Most recently run first, then never-run by title: the order a person
	// deciding what to retire reads in.
	slices.SortFunc(rep.Scripts, func(a, b Script) int {
		if c := b.Runs.Last.Compare(a.Runs.Last); c != 0 {
			return c
		}
		return cmp.Compare(a.Title, b.Title)
	})
	return rep, nil
}

// fill looks up one script's metadata, deployments and runs, and returns the
// earliest run it saw.
func fill(ctx context.Context, c *gas.Client, s *Script, since time.Time) time.Time {
	s.EditorURL = "https://script.google.com/home/projects/" + s.ScriptID + "/edit"

	p, err := c.Project(ctx, s.ScriptID)
	if err != nil {
		s.Errors = append(s.Errors, "project: "+err.Error())
	} else {
		s.Title = cmp.Or(p.Title, s.Title)
		s.Created = cmp.Or(p.CreateTime, s.Created)
		s.Updated = cmp.Or(p.UpdateTime, s.Updated)
		if p.LastModifyUser != nil {
			s.UpdatedBy = p.LastModifyUser.Email
		}
		if p.ParentID != "" {
			s.Kind = "bound"
			s.Container = &Container{ID: p.ParentID}
			if f, err := c.DriveFile(ctx, p.ParentID); err != nil {
				s.Errors = append(s.Errors, "container: "+err.Error())
			} else {
				s.Container.Name = f.Name
				s.Container.MimeType = f.MimeType
				s.Container.URL = f.WebViewLink
				s.Container.Trashed = f.Trashed
				owned := f.OwnedByMe
				s.OwnedByMe = &owned
				if len(f.Owners) > 0 {
					s.Owner = f.Owners[0].EmailAddress
				}
			}
		} else if s.Kind == "" {
			s.Kind = "standalone"
		}
	}

	if ds, err := c.Deployments(ctx, s.ScriptID); err != nil {
		s.Errors = append(s.Errors, "deployments: "+err.Error())
	} else {
		s.Deployments = summariseDeployments(ds)
	}

	var earliest time.Time
	if page, err := c.ScriptProcesses(ctx, s.ScriptID, gas.ProcessFilter{Since: since}); err != nil {
		s.Errors = append(s.Errors, "runs: "+err.Error())
	} else {
		s.Runs = Summarise(page.Processes, page.Truncated)
		for _, p := range page.Processes {
			earliest = earlier(earliest, p.StartTime)
		}
	}
	return earliest
}

// summariseDeployments drops the HEAD deployment when it has no entry
// points, which is every script's: it exists for the editor's test runs and
// says nothing about whether anything outside depends on the script.
func summariseDeployments(ds []gas.Deployment) []Deployment {
	var out []Deployment
	for _, d := range ds {
		if d.DeploymentConfig.VersionNumber == 0 && len(d.EntryPoints) == 0 {
			continue
		}
		sd := Deployment{
			ID:          d.DeploymentID,
			Version:     d.DeploymentConfig.VersionNumber,
			Description: d.DeploymentConfig.Description,
			Updated:     d.UpdateTime,
		}
		for _, e := range d.EntryPoints {
			sd.EntryPoints = append(sd.EntryPoints, e.EntryPointType)
			if e.WebApp != nil {
				sd.WebAppURL = e.WebApp.URL
				sd.Access = e.WebApp.EntryPointConfig.Access
				sd.ExecuteAs = e.WebApp.EntryPointConfig.ExecuteAs
			}
			if e.ExecutionAPI != nil && sd.Access == "" {
				sd.Access = e.ExecutionAPI.EntryPointConfig.Access
			}
		}
		out = append(out, sd)
	}
	return out
}

// Summarise counts runs by status, type and function.
func Summarise(ps []gas.Process, truncated bool) Runs {
	r := Runs{Total: len(ps), Truncated: truncated}
	if len(ps) == 0 {
		return r
	}
	r.ByStatus = map[string]int{}
	r.ByType = map[string]int{}
	fns := map[string]*Function{}
	for _, p := range ps {
		r.ByStatus[p.ProcessStatus]++
		r.ByType[p.ProcessType]++
		if p.ProcessType == "TIME_DRIVEN" || p.ProcessType == "TRIGGER" {
			r.Triggered = true
		}
		if p.StartTime.After(r.Last) {
			r.Last = p.StartTime
		}

		f := fns[p.FunctionName]
		if f == nil {
			f = &Function{Name: p.FunctionName}
			fns[p.FunctionName] = f
		}
		f.Runs++
		if p.StartTime.After(f.LastRun) {
			f.LastRun = p.StartTime
		}
		if Failed(p.ProcessStatus) {
			f.Failures++
			if p.StartTime.After(f.LastFailure) {
				f.LastFailure = p.StartTime
			}
		}
		if !slices.Contains(f.Types, p.ProcessType) {
			f.Types = append(f.Types, p.ProcessType)
		}
	}
	for _, f := range fns {
		slices.Sort(f.Types)
		r.Functions = append(r.Functions, *f)
	}
	// Failing functions first, then the busiest.
	slices.SortFunc(r.Functions, func(a, b Function) int {
		return cmp.Or(cmp.Compare(b.Failures, a.Failures), cmp.Compare(b.Runs, a.Runs), cmp.Compare(a.Name, b.Name))
	})
	return r
}

// Failed is whether a process status counts as a failure. TIMED_OUT is one: a
// script that hits the six-minute limit did not finish its job.
func Failed(status string) bool {
	return status == "FAILED" || status == "TIMED_OUT"
}

func earlier(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero():
		return a
	case b.Before(a):
		return b
	}
	return a
}
