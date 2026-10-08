package gas

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// ListStandalone returns every standalone script this account can see in
// Drive, its own and those shared with it. OwnedByMe tells them apart.
func (c *Client) ListStandalone(ctx context.Context) ([]DriveFile, error) {
	q := url.Values{
		"q":        {"mimeType='application/vnd.google-apps.script' and trashed=false"},
		"fields":   {"nextPageToken,files(id,name,createdTime,modifiedTime,ownedByMe,owners(emailAddress),webViewLink)"},
		"pageSize": {"1000"},
		"orderBy":  {"modifiedTime desc"},
	}
	var all []DriveFile
	for {
		var page struct {
			Files         []DriveFile `json:"files"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.DriveBase, "files", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		if page.NextPageToken == "" {
			return all, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
}

// DriveFile returns one file's metadata: here, the document a bound script
// lives in.
func (c *Client) DriveFile(ctx context.Context, id string) (DriveFile, error) {
	q := url.Values{"fields": {"id,name,mimeType,modifiedTime,ownedByMe,owners(emailAddress),webViewLink,trashed"}}
	var f DriveFile
	err := c.get(ctx, c.DriveBase, "files/"+url.PathEscape(id), q, &f)
	return f, err
}

// Project returns a script's metadata.
func (c *Client) Project(ctx context.Context, scriptID string) (Project, error) {
	var p Project
	err := c.get(ctx, c.ScriptBase, "projects/"+url.PathEscape(scriptID), nil, &p)
	return p, err
}

// Content returns a script's files, source included, at HEAD.
func (c *Client) Content(ctx context.Context, scriptID string) (Content, error) {
	var ct Content
	err := c.get(ctx, c.ScriptBase, "projects/"+url.PathEscape(scriptID)+"/content", nil, &ct)
	return ct, err
}

// Deployments lists a script's deployments, the HEAD deployment included.
func (c *Client) Deployments(ctx context.Context, scriptID string) ([]Deployment, error) {
	q := url.Values{"pageSize": {"50"}}
	var all []Deployment
	for {
		var page struct {
			Deployments   []Deployment `json:"deployments"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.ScriptBase, "projects/"+url.PathEscape(scriptID)+"/deployments", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Deployments...)
		if page.NextPageToken == "" {
			return all, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
}

// Metrics returns active users and executions per period. Granularity is
// "DAILY" (the last seven days) or "WEEKLY".
func (c *Client) Metrics(ctx context.Context, scriptID, granularity string) (Metrics, error) {
	q := url.Values{"metricsGranularity": {granularity}}
	var m Metrics
	err := c.get(ctx, c.ScriptBase, "projects/"+url.PathEscape(scriptID)+"/metrics", q, &m)
	return m, err
}

// ProcessFilter narrows a listing of runs. Zero fields do not filter.
type ProcessFilter struct {
	Since        time.Time
	FunctionName string
	// ProjectName applies to UserProcesses only; it is how runs of a script
	// known only by name are listed.
	ProjectName string
	Statuses    []string // e.g. FAILED, TIMED_OUT
	Types       []string // e.g. TIME_DRIVEN, TRIGGER
	// Max caps how many are fetched, because a busy time-driven trigger is
	// thousands of runs a month and nobody reads past the first few hundred.
	// Zero means 500.
	Max int
}

// ProcessPage is runs and whether the cap stopped the listing short.
type ProcessPage struct {
	Processes []Process
	Truncated bool
}

// UserProcesses lists runs made by or on behalf of this account, across every
// script. It is the only way to see a bound script that nobody has told us
// the id of.
func (c *Client) UserProcesses(ctx context.Context, f ProcessFilter) (ProcessPage, error) {
	return c.processes(ctx, "processes", "userProcessFilter.", f, nil)
}

// ScriptProcesses lists runs of one script, by anybody.
func (c *Client) ScriptProcesses(ctx context.Context, scriptID string, f ProcessFilter) (ProcessPage, error) {
	return c.processes(ctx, "processes:listScriptProcesses", "scriptProcessFilter.", f, url.Values{"scriptId": {scriptID}})
}

func (c *Client) processes(ctx context.Context, path, prefix string, f ProcessFilter, q url.Values) (ProcessPage, error) {
	if q == nil {
		q = url.Values{}
	}
	limit := f.Max
	if limit <= 0 {
		limit = 500
	}
	q.Set("pageSize", strconv.Itoa(min(limit, 200)))
	if !f.Since.IsZero() {
		q.Set(prefix+"startTime", f.Since.UTC().Format(time.RFC3339))
	}
	if f.FunctionName != "" {
		q.Set(prefix+"functionName", f.FunctionName)
	}
	if f.ProjectName != "" && prefix == "userProcessFilter." {
		q.Set(prefix+"projectName", f.ProjectName)
	}
	for _, s := range f.Statuses {
		q.Add(prefix+"statuses", s)
	}
	for _, t := range f.Types {
		q.Add(prefix+"types", t)
	}

	var out ProcessPage
	for {
		var page struct {
			Processes     []Process `json:"processes"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.ScriptBase, path, q, &page); err != nil {
			return ProcessPage{}, err
		}
		out.Processes = append(out.Processes, page.Processes...)
		if len(out.Processes) >= limit {
			out.Truncated = len(out.Processes) > limit || page.NextPageToken != ""
			out.Processes = out.Processes[:limit]
			return out, nil
		}
		if page.NextPageToken == "" {
			return out, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
}
