// Package auth signs one Google account in to the Apps Script and Drive APIs
// and keeps it signed in.
//
// # The shape of it
//
// Sign-in is a separate command (`google-scripts-mcp login`) and never happens
// inside the MCP server. The server talks JSON-RPC on stdout, and a person
// cannot see its stderr from inside Claude Code, so a server that needed a
// browser would hang waiting for a click nobody was shown. The login command
// runs in a terminal, prints the URL, and writes a token file; the server only
// reads that file and refreshes what is in it.
//
// The flow is Google's documented one for desktop apps: a loopback redirect to
// 127.0.0.1 on a port the kernel picks, PKCE, and a state value. The OAuth
// client is one we create in our own Google Cloud project (a "Desktop app"
// client, downloaded as JSON). clasp's built-in client would have saved that
// step, but its consent screen asks for far more than this tool reads, and a
// refresh token minted for somebody else's client is one we cannot revoke
// without revoking clasp too.
//
// # Which account
//
// The scripts belong to a personal Gmail account, and the browser that opens
// the consent page is usually signed in to a Workspace account as well. So
// login asks for the openid and email scopes, reads the email Google returns,
// and refuses to save a token for any account other than the one named on the
// command line. Signing in with the wrong account is otherwise silent: every
// call succeeds and returns somebody else's (or nobody's) scripts.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// Scopes are the permissions login asks for. All read-only: this is the
// inventory phase, and a token that cannot change a script cannot break one.
// The write tools will add script.projects and script.deployments, and adding
// them will mean signing in again, which is the moment to notice the change.
var Scopes = []string{
	"openid",
	"email",
	"https://www.googleapis.com/auth/script.projects.readonly",
	"https://www.googleapis.com/auth/script.deployments.readonly",
	"https://www.googleapis.com/auth/script.processes",
	"https://www.googleapis.com/auth/script.metrics",
	// Drive is the only place standalone scripts can be listed from; the Apps
	// Script API has no "list projects". Metadata only -- names, dates,
	// parents -- never file contents.
	"https://www.googleapis.com/auth/drive.metadata.readonly",
}

const (
	clientFile = "client_secret.json"
	tokenFile  = "token.json"
)

// Dir is where the client and the token live. GOOGLE_SCRIPTS_MCP_DIR moves it,
// which is how a second account would get its own directory and how the tests
// keep out of the real one.
func Dir() (string, error) {
	if d := os.Getenv("GOOGLE_SCRIPTS_MCP_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}
	return filepath.Join(base, "google-scripts-mcp"), nil
}

// LoadClient reads the OAuth client downloaded from the Cloud console.
func LoadClient(dir string) (*oauth2.Config, error) {
	path := filepath.Join(dir, clientFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no OAuth client at %s. Create a Desktop app client in the Google Cloud "+
			"console (APIs & Services > Credentials), download its JSON, and save it there", path)
	}
	if err != nil {
		return nil, err
	}

	// A Desktop client downloads as {"installed": {...}}. A Web client
	// downloads as {"web": {...}} and would fail later, at the redirect, with
	// a message from Google that does not mention the client type -- so it is
	// refused here, where the reason can be said.
	var file struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web json.RawMessage `json:"web"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s is not an OAuth client file: %w", path, err)
	}
	if file.Installed == nil {
		if file.Web != nil {
			return nil, fmt.Errorf("%s is a Web application client. Create a Desktop app client instead", path)
		}
		return nil, fmt.Errorf("%s has no \"installed\" client in it", path)
	}

	return &oauth2.Config{
		ClientID:     file.Installed.ClientID,
		ClientSecret: file.Installed.ClientSecret,
		Endpoint:     endpoints.Google,
		Scopes:       Scopes,
	}, nil
}

// stored is the token file. The account and the granted scopes travel with
// the token so that the server can say who it is acting as, and so that a
// tool needing a scope the person unticked on the consent screen can say so
// instead of failing with a bare 403.
type stored struct {
	Account string        `json:"account"`
	Scopes  []string      `json:"scopes"`
	Token   *oauth2.Token `json:"token"`
}

// Session is a signed-in account, ready to make requests.
type Session struct {
	Account string
	Scopes  []string
	Client  *http.Client
}

// Open loads the saved token and returns an HTTP client that refreshes it,
// writing each refreshed token back so that the next process starts from it.
func Open(ctx context.Context, dir string) (*Session, error) {
	cfg, err := LoadClient(dir)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, tokenFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("not signed in. Run: google-scripts-mcp login -account <gmail address>")
	}
	if err != nil {
		return nil, err
	}
	var s stored
	if err := json.Unmarshal(data, &s); err != nil || s.Token == nil {
		return nil, fmt.Errorf("%s is unreadable. Sign in again: google-scripts-mcp login -account <gmail address>", path)
	}

	src := &savingSource{
		path:  path,
		saved: s,
		src:   oauth2.ReuseTokenSource(s.Token, cfg.TokenSource(ctx, s.Token)),
	}
	client := oauth2.NewClient(ctx, src)
	client.Timeout = 60 * time.Second

	return &Session{Account: s.Account, Scopes: s.Scopes, Client: client}, nil
}

// savingSource writes the token back whenever the access token changes.
// Google keeps the refresh token across refreshes, so this is not strictly
// needed for the next process to work -- it saves that process one refresh,
// and it means the file on disk is never the stalest copy.
type savingSource struct {
	path string
	src  oauth2.TokenSource

	mu    sync.Mutex
	saved stored
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err != nil {
		// invalid_grant is how Google says the refresh token is gone: revoked,
		// or expired because the consent screen is still in Testing, where
		// refresh tokens last seven days.
		if strings.Contains(err.Error(), "invalid_grant") {
			return nil, errors.New("the saved sign-in is no longer accepted by Google (revoked, or expired " +
				"because the OAuth consent screen is in Testing). Run: google-scripts-mcp login -account <gmail address>")
		}
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if tok.AccessToken != s.saved.Token.AccessToken {
		// Keep the refresh token if the refresh response left it out.
		if tok.RefreshToken == "" {
			tok.RefreshToken = s.saved.Token.RefreshToken
		}
		s.saved.Token = tok
		// A failed write costs one extra refresh next time and nothing else,
		// so it is not allowed to fail the request that triggered it.
		_ = writeToken(s.path, s.saved)
	}
	return tok, nil
}

// writeToken replaces the token file atomically and readable only by its
// owner. Atomically because a crash halfway through would otherwise leave a
// truncated file and a person signing in again for no visible reason.
func writeToken(path string, s stored) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Login runs the browser sign-in and saves the token, refusing it unless it is
// for wantAccount.
func Login(ctx context.Context, dir, wantAccount string, out io.Writer) error {
	cfg, err := LoadClient(dir)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("opening a local port for the sign-in redirect: %w", err)
	}
	defer ln.Close()
	cfg.RedirectURL = "http://" + ln.Addr().String() + "/"

	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	url := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		// consent, so that Google issues a refresh token even when this
		// client was approved before; login_hint, so the account chooser
		// starts on the right account.
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("login_hint", wantAccount),
	)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			var res result
			switch {
			case q.Get("state") != state:
				// A request without our state is not the redirect -- a
				// favicon fetch, or another page probing the port. Ignore it
				// rather than ending the sign-in.
				http.Error(w, "not the sign-in redirect", http.StatusBadRequest)
				return
			case q.Get("error") != "":
				res.err = fmt.Errorf("the sign-in was refused by Google: %s", q.Get("error"))
			default:
				res.code = q.Get("code")
			}
			if res.err != nil {
				fmt.Fprintln(w, "Sign-in failed. The terminal says why. You can close this tab.")
			} else {
				fmt.Fprintln(w, "Signed in. You can close this tab.")
			}
			select {
			case done <- res:
			default:
			}
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	fmt.Fprintf(out, "Open this link in a browser and sign in as %s:\n\n%s\n\n", wantAccount, url)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		return errors.New("gave up waiting for the browser after 10 minutes")
	}
	if res.err != nil {
		return res.err
	}

	tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("exchanging the sign-in code: %w", err)
	}

	account, err := emailFromIDToken(tok)
	if err != nil {
		return err
	}
	if !strings.EqualFold(account, wantAccount) {
		return fmt.Errorf("signed in as %s, not %s. Nothing was saved. Run login again and pick %s in the account chooser",
			account, wantAccount, wantAccount)
	}

	granted := strings.Fields(fmt.Sprint(tok.Extra("scope")))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeToken(filepath.Join(dir, tokenFile), stored{Account: account, Scopes: granted, Token: tok}); err != nil {
		return fmt.Errorf("saving the token: %w", err)
	}

	fmt.Fprintf(out, "Signed in as %s.\n", account)
	// Google's consent screen lets a person untick individual permissions.
	// Say which were left off now, rather than as a 403 from some tool later.
	if missing := Missing(granted); len(missing) > 0 {
		fmt.Fprintf(out, "These permissions were not granted, and the tools that need them will fail:\n  %s\n",
			strings.Join(missing, "\n  "))
	}
	return nil
}

// Missing lists the scopes in Scopes that granted does not include. openid
// and email come back from Google spelled as their full userinfo URLs, so
// those two are matched by suffix.
func Missing(granted []string) []string {
	var missing []string
	for _, want := range Scopes {
		ok := slices.ContainsFunc(granted, func(g string) bool {
			return g == want || (want == "email" && strings.HasSuffix(g, "/userinfo.email"))
		})
		if !ok {
			missing = append(missing, want)
		}
	}
	return missing
}

// emailFromIDToken reads the email claim from the ID token that came back
// with the access token. The signature is not checked: the token arrived over
// TLS directly from Google's token endpoint in answer to our own code
// exchange, which is the case OpenID Connect says needs no further
// validation. It is not a token anybody handed us.
func emailFromIDToken(tok *oauth2.Token) (string, error) {
	raw, _ := tok.Extra("id_token").(string)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("no ID token came back from Google, so the signed-in account is unknown. Nothing was saved")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("reading the ID token: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Email == "" {
		return "", errors.New("the ID token carries no email address. Nothing was saved")
	}
	return claims.Email, nil
}
