// Package tools offers the inventory and the per-script reads as MCP tools.
//
// Every tool here reads, except script_pull, which writes into a local
// directory and refuses to overwrite anything it did not write itself, and
// the two that edit the local list of remembered ids. Nothing here can change
// a script on Google's side; the token does not carry the permission to.
package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jroedel/google-scripts-mcp/internal/auth"
	"github.com/jroedel/google-scripts-mcp/internal/gas"
	"github.com/jroedel/google-scripts-mcp/internal/inventory"
	"github.com/jroedel/google-scripts-mcp/internal/source"
)

// Deps opens the signed-in session on first use rather than at start. A
// server that exited because nobody had signed in yet would show up in
// Claude Code as a server that failed to start, with the reason in a log
// nobody reads; one that starts and answers each call with "run login" says
// what to do in the place the person is looking.
type Deps struct {
	Dir string
	// NewClient is how a client is made; tests replace it.
	NewClient func(ctx context.Context) (*gas.Client, string, error)

	mu      sync.Mutex
	client  *gas.Client
	account string
}

func (d *Deps) session(ctx context.Context) (*gas.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil {
		return d.client, nil
	}
	c, account, err := d.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	d.client, d.account = c, account
	return c, nil
}

// OpenSession is the real NewClient.
func OpenSession(dir string) func(context.Context) (*gas.Client, string, error) {
	return func(context.Context) (*gas.Client, string, error) {
		// Background, not the call's context: the session outlives the call
		// that opened it, and its token refreshes would otherwise fail once
		// that call returned.
		s, err := auth.Open(context.Background(), dir)
		if err != nil {
			return nil, "", err
		}
		return gas.New(s.Client), s.Account, nil
	}
}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true}
}

// window turns a days argument into a start time, defaulting and capping it.
func window(days int) time.Time {
	if days <= 0 {
		days = 30
	}
	return time.Now().AddDate(0, 0, -min(days, 365))
}

// -------------------------------------------------------------- inventory

type inventoryIn struct {
	Days int `json:"days,omitempty" jsonschema:"how far back to count runs, in days; default 30"`
}

type inventoryOut struct {
	Account string `json:"account"`
	inventory.Report
	Remembered []inventory.Remembered `json:"remembered,omitempty"`
}

// -------------------------------------------------------------- one script

type scriptIn struct {
	ScriptID string `json:"script_id" jsonschema:"the script id: a Drive file id for a standalone script, or the id in the editor URL script.google.com/home/projects/<id>/edit"`
}

type fileInfo struct {
	Name      string    `json:"name"`
	Local     string    `json:"local_name"`
	Type      string    `json:"type"`
	Bytes     int       `json:"bytes"`
	Updated   time.Time `json:"updated,omitzero"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	Functions []string  `json:"functions,omitempty"`
}

type scriptOut struct {
	Project   gas.Project           `json:"project"`
	Container *inventory.Container  `json:"container,omitempty"`
	Files     []fileInfo            `json:"files"`
	Manifest  string                `json:"manifest,omitempty"`
	Deploys   []gas.Deployment      `json:"deployments"`
	Remember  *inventory.Remembered `json:"remembered,omitempty"`
}

// -------------------------------------------------------------- runs

type runsIn struct {
	ScriptID     string   `json:"script_id,omitempty" jsonschema:"list this script's runs, by anybody; give this or project_name"`
	ProjectName  string   `json:"project_name,omitempty" jsonschema:"list this account's runs of the project with this exact name; for a script whose id is not known"`
	Days         int      `json:"days,omitempty" jsonschema:"how far back, in days; default 30"`
	FunctionName string   `json:"function_name,omitempty" jsonschema:"only runs of this function"`
	Statuses     []string `json:"statuses,omitempty" jsonschema:"only these statuses: COMPLETED, FAILED, TIMED_OUT, CANCELED, RUNNING, PAUSED, DELAYED, UNKNOWN"`
	Types        []string `json:"types,omitempty" jsonschema:"only these process types: TIME_DRIVEN, TRIGGER, EDITOR, WEBAPP, EXECUTION_API, ADD_ON, SIMPLE_TRIGGER, MENU, BATCH_TASK"`
	Max          int      `json:"max,omitempty" jsonschema:"at most this many runs, newest first; default 200, at most 2000"`
}

type runsOut struct {
	Summary inventory.Runs `json:"summary"`
	Runs    []gas.Process  `json:"runs"`
	Note    string         `json:"note"`
}

// -------------------------------------------------------------- metrics

type metricsIn struct {
	ScriptID    string `json:"script_id" jsonschema:"the script id"`
	Granularity string `json:"granularity,omitempty" jsonschema:"DAILY (the last seven days) or WEEKLY; default WEEKLY"`
}

// -------------------------------------------------------------- pull

type pullIn struct {
	ScriptID string `json:"script_id" jsonschema:"the script id"`
	Dir      string `json:"dir" jsonschema:"absolute path of the directory to write into: new, empty, or one an earlier pull of this script wrote"`
}

// -------------------------------------------------------------- remember

type rememberIn struct {
	ScriptID string `json:"script_id" jsonschema:"the id from the editor URL script.google.com/home/projects/<id>/edit"`
	Note     string `json:"note,omitempty" jsonschema:"what the person said about it, e.g. which sheet it is in or whether it is to be kept"`
}

type rememberOut struct {
	Remembered inventory.Remembered   `json:"remembered"`
	All        []inventory.Remembered `json:"all"`
}

type forgetOut struct {
	Forgot bool `json:"forgot"`
}

// Register adds every tool to s.
func Register(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "scripts_inventory",
		Annotations: readOnly("Inventory of Apps Script projects"),
		Description: "List every Apps Script project this account can find, with whether each still runs, what " +
			"starts it, which functions fail, and its deployments (web apps, API endpoints). Start here. " +
			"Scripts come from three places: standalone scripts in Drive, script ids remembered with " +
			"script_remember (usually scripts bound to a Sheet, Doc or Form, which Drive does not list), and " +
			"the account's run history. A project in `unidentified` ran but its id is not known: it is almost " +
			"always a bound script. Ask the person to open its document, then Extensions > Apps Script, and to " +
			"give the id from the editor's URL, then call script_remember with it. `runs.triggered` is the only " +
			"evidence of an installed trigger the API offers; the API cannot list triggers. Check " +
			"`earliest_run` against `since` before reading \"no runs\" as \"never runs\": Google keeps a limited " +
			"run history. Takes a few seconds per dozen scripts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in inventoryIn) (*mcp.CallToolResult, inventoryOut, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, inventoryOut{}, err
		}
		rem, err := inventory.LoadRemembered(d.Dir)
		if err != nil {
			return nil, inventoryOut{}, err
		}
		ids := make([]string, 0, len(rem))
		for _, r := range rem {
			ids = append(ids, r.ScriptID)
		}
		rep, err := inventory.Build(ctx, c, ids, window(in.Days))
		if err != nil {
			return nil, inventoryOut{}, err
		}
		return nil, inventoryOut{Account: d.account, Report: rep, Remembered: rem}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "script_get",
		Annotations: readOnly("One script's details"),
		Description: "One script's metadata, the document it is bound to (if any), its files with the functions " +
			"each defines, its manifest (appsscript.json: time zone, runtime, OAuth scopes, advanced services, " +
			"web app settings), and its deployments. Source code is not returned; use script_pull to get the " +
			"files into a local directory and read them there.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in scriptIn) (*mcp.CallToolResult, scriptOut, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, scriptOut{}, err
		}
		if err := needID(in.ScriptID); err != nil {
			return nil, scriptOut{}, err
		}
		p, err := c.Project(ctx, in.ScriptID)
		if err != nil {
			return nil, scriptOut{}, err
		}
		out := scriptOut{Project: p}
		if p.ParentID != "" {
			out.Container = &inventory.Container{ID: p.ParentID}
			if f, err := c.DriveFile(ctx, p.ParentID); err == nil {
				out.Container.Name, out.Container.MimeType = f.Name, f.MimeType
				out.Container.URL, out.Container.Trashed = f.WebViewLink, f.Trashed
			}
		}
		content, err := c.Content(ctx, in.ScriptID)
		if err != nil {
			return nil, scriptOut{}, err
		}
		for _, f := range content.Files {
			fi := fileInfo{Name: f.Name, Type: f.Type, Bytes: len(f.Source), Updated: f.UpdateTime}
			fi.Local, _ = source.LocalName(f)
			if f.LastModifyUser != nil {
				fi.UpdatedBy = f.LastModifyUser.Email
			}
			if f.FunctionSet != nil {
				for _, fn := range f.FunctionSet.Values {
					fi.Functions = append(fi.Functions, fn.Name)
				}
			}
			if f.Type == gas.FileJSON && f.Name == "appsscript" {
				out.Manifest = f.Source
			}
			out.Files = append(out.Files, fi)
		}
		if out.Deploys, err = c.Deployments(ctx, in.ScriptID); err != nil {
			return nil, scriptOut{}, err
		}
		if rem, err := inventory.LoadRemembered(d.Dir); err == nil {
			if i := slices.IndexFunc(rem, func(r inventory.Remembered) bool { return r.ScriptID == in.ScriptID }); i >= 0 {
				out.Remember = &rem[i]
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "script_runs",
		Annotations: readOnly("A script's run history"),
		Description: "Individual runs of a script, newest first, with a summary by status, type and function. " +
			"Use it to see when something stopped working, which function fails and what starts it. A run says " +
			"that it failed but not why: the error text is in the editor's Executions page, which " +
			"`editor_url` + `/executions` opens. Give script_id, or project_name for a script seen in the " +
			"inventory's `unidentified` list.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runsIn) (*mcp.CallToolResult, runsOut, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, runsOut{}, err
		}
		f := gas.ProcessFilter{
			Since:        window(in.Days),
			FunctionName: in.FunctionName,
			Statuses:     upper(in.Statuses),
			Types:        upper(in.Types),
			Max:          min(cmp.Or(in.Max, 200), 2000),
		}
		var page gas.ProcessPage
		switch {
		case in.ScriptID != "":
			page, err = c.ScriptProcesses(ctx, in.ScriptID, f)
		case in.ProjectName != "":
			f.ProjectName = in.ProjectName
			page, err = c.UserProcesses(ctx, f)
		default:
			return nil, runsOut{}, errors.New("give script_id, or project_name for a script whose id is not known")
		}
		if err != nil {
			return nil, runsOut{}, err
		}
		note := "Runs by anybody, newest first."
		if in.ScriptID == "" {
			note = "Only runs by or on behalf of this account, matched by project name, newest first."
		}
		if page.Truncated {
			note += " Stopped at the cap; there are more."
		}
		return nil, runsOut{
			Summary: inventory.Summarise(page.Processes, page.Truncated),
			Runs:    page.Processes,
			Note:    note,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "script_metrics",
		Annotations: readOnly("A script's usage metrics"),
		Description: "Active users, total executions and failed executions per day (the last seven days) or per " +
			"week, as the editor's Overview page shows them. These counts are Google's own and cover every user, " +
			"which makes them the better measure of whether a script is still used than run history.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in metricsIn) (*mcp.CallToolResult, gas.Metrics, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, gas.Metrics{}, err
		}
		if err := needID(in.ScriptID); err != nil {
			return nil, gas.Metrics{}, err
		}
		g := strings.ToUpper(cmp.Or(in.Granularity, "WEEKLY"))
		if g != "DAILY" && g != "WEEKLY" {
			return nil, gas.Metrics{}, fmt.Errorf("granularity is DAILY or WEEKLY, not %q", in.Granularity)
		}
		m, err := c.Metrics(ctx, in.ScriptID, g)
		return nil, m, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "script_pull",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Pull a script's files into a local directory",
			DestructiveHint: new(false),
			IdempotentHint:  true,
		},
		Description: "Write a script's files (.gs, .html, appsscript.json) into a local directory, to read, " +
			"diff and fix there under git. The directory must be new, empty (a .git directory is fine), or one " +
			"an earlier pull of the same script wrote. Pulling again refreshes it, and refuses if any file was " +
			"changed since the last pull, so local edits are never overwritten. Writes .gas-pull.json (what " +
			"was pulled, for a later push to check against) and .clasp.json (so clasp works there too).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pullIn) (*mcp.CallToolResult, source.Pulled, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, source.Pulled{}, err
		}
		if err := needID(in.ScriptID); err != nil {
			return nil, source.Pulled{}, err
		}
		p, err := c.Project(ctx, in.ScriptID)
		if err != nil {
			return nil, source.Pulled{}, err
		}
		content, err := c.Content(ctx, in.ScriptID)
		if err != nil {
			return nil, source.Pulled{}, err
		}
		out, err := source.Pull(in.Dir, p, content)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "script_remember",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Remember a script id",
			DestructiveHint: new(false),
			IdempotentHint:  true,
		},
		Description: "Add a script id to the local list the inventory reads, with an optional note. Use it for " +
			"scripts bound to a Sheet, Doc, Form or Slides file, which the inventory cannot otherwise find by " +
			"id. The id is checked against Google first. Remembering an id already on the list updates its note.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in rememberIn) (*mcp.CallToolResult, rememberOut, error) {
		c, err := d.session(ctx)
		if err != nil {
			return nil, rememberOut{}, err
		}
		if err := needID(in.ScriptID); err != nil {
			return nil, rememberOut{}, err
		}
		p, err := c.Project(ctx, in.ScriptID)
		if err != nil {
			return nil, rememberOut{}, err
		}
		r := inventory.Remembered{ScriptID: p.ScriptID, Title: p.Title, Note: in.Note, Added: time.Now().UTC().Truncate(time.Second)}
		all, err := inventory.SaveRemembered(d.Dir, r)
		if err != nil {
			return nil, rememberOut{}, err
		}
		i := slices.IndexFunc(all, func(x inventory.Remembered) bool { return x.ScriptID == r.ScriptID })
		return nil, rememberOut{Remembered: all[i], All: all}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "script_forget",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Forget a remembered script id",
			DestructiveHint: new(false),
			IdempotentHint:  true,
		},
		Description: "Take a script id off the local list the inventory reads. Nothing on Google's side changes.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in scriptIn) (*mcp.CallToolResult, forgetOut, error) {
		ok, err := inventory.Forget(d.Dir, in.ScriptID)
		return nil, forgetOut{Forgot: ok}, err
	})
}

func needID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("script_id is required")
	}
	return nil
}

func upper(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(strings.TrimSpace(s))
	}
	return out
}
