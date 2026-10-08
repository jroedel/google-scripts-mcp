// Package gas is a client for the parts of the Apps Script and Drive REST
// APIs that taking stock of scripts needs.
//
// # Why not google.golang.org/api
//
// Google's generated clients are exact, and they were used as the reference
// for every field name in types.go. They are not imported because
// google.golang.org/api brings gRPC, OpenTelemetry and the cloud auth stack
// with it -- some thirty modules -- for what is here eight GET requests with
// JSON answers. net/http and encoding/json answer that completely, and the
// vulnerability scan has a fraction of the surface to cover.
//
// # What the API cannot tell us
//
// Three gaps shape everything above this package, and are worth knowing
// before reading a result as complete:
//
//   - There is no "list my projects". Drive lists standalone scripts. A
//     script bound to a Sheet, Doc, Form or Slides file is not a Drive file
//     and does not appear there.
//   - A process (one run) carries the project's name but not its script id.
//     So a bound script that runs shows up by name only, and its id has to be
//     supplied from its URL in the editor.
//   - Triggers are not in the REST API at all. A run's process type
//     (TIME_DRIVEN, TRIGGER) is the evidence that a trigger exists.
package gas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The two APIs' roots. Client carries its own copy, so that tests can point
// it at a fake.
const (
	ScriptBase = "https://script.googleapis.com/v1/"
	DriveBase  = "https://www.googleapis.com/drive/v3/"
)

// Client makes requests as one signed-in account.
type Client struct {
	HTTP       *http.Client
	ScriptBase string
	DriveBase  string
}

// New returns a client against Google's real endpoints.
func New(h *http.Client) *Client {
	return &Client{HTTP: h, ScriptBase: ScriptBase, DriveBase: DriveBase}
}

// get fetches base+path?query into out.
func (c *Client) get(ctx context.Context, base, path string, query url.Values, out any) error {
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unexpected answer from %s: %w", path, err)
	}
	return nil
}

// APIError is Google's error envelope, with a hint added when we know what
// the person has to do about it.
type APIError struct {
	Status  int
	Code    string // e.g. PERMISSION_DENIED
	Reason  string // e.g. SERVICE_DISABLED, ACCESS_TOKEN_SCOPE_INSUFFICIENT
	Message string
	Hint    string
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("Google answered %d %s: %s", e.Status, e.Code, e.Message)
	if e.Hint != "" {
		s += " -- " + e.Hint
	}
	return s
}

func parseError(status int, body []byte) error {
	var env struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	e := &APIError{Status: status}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		e.Code = env.Error.Status
		e.Message = env.Error.Message
		for _, d := range env.Error.Details {
			if d.Reason != "" {
				e.Reason = d.Reason
				break
			}
		}
	} else {
		e.Message = strings.TrimSpace(string(body))
		if len(e.Message) > 300 {
			e.Message = e.Message[:300] + "…"
		}
	}
	e.Hint = hint(e)
	return e
}

// hint says what to do for the failures a first run is likely to meet. The
// two "enable" cases read alike and are not the same switch: one is per
// account, at script.google.com, and the other is per Cloud project, in the
// console.
func hint(e *APIError) string {
	msg := strings.ToLower(e.Message)
	switch {
	case strings.Contains(msg, "user has not enabled the apps script api"):
		return "turn on \"Google Apps Script API\" at https://script.google.com/home/usersettings for this account"
	case e.Reason == "SERVICE_DISABLED" || strings.Contains(msg, "has not been used in project"):
		return "enable this API for the OAuth client's Cloud project, in the Google Cloud console under APIs & Services > Library"
	case e.Reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT":
		return "the sign-in does not carry the permission this needs; run login again and leave every box ticked"
	case e.Status == http.StatusNotFound:
		return "no script with that id is visible to this account"
	}
	return ""
}
