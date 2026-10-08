package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestLoadClientWantsADesktopClient(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, clientFile), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := LoadClient(dir); err == nil || !strings.Contains(err.Error(), "Desktop app") {
		t.Errorf("missing file: %v", err)
	}

	write(`{"web":{"client_id":"x"}}`)
	if _, err := LoadClient(dir); err == nil || !strings.Contains(err.Error(), "Web application") {
		t.Errorf("web client: %v", err)
	}

	write(`{"installed":{"client_id":"id","client_secret":"sec"}}`)
	cfg, err := LoadClient(dir)
	if err != nil || cfg.ClientID != "id" || cfg.ClientSecret != "sec" {
		t.Errorf("desktop client: %+v, %v", cfg, err)
	}
}

func TestEmailFromIDToken(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"jeff@example.com"}`))
	tok := (&oauth2.Token{}).WithExtra(map[string]any{"id_token": "h." + payload + ".sig"})
	got, err := emailFromIDToken(tok)
	if err != nil || got != "jeff@example.com" {
		t.Errorf("got %q, %v", got, err)
	}

	if _, err := emailFromIDToken(&oauth2.Token{}); err == nil {
		t.Error("accepted a token with no ID token")
	}
}

func TestMissingScopes(t *testing.T) {
	// Google spells openid's email scope as the userinfo URL.
	granted := append([]string{"https://www.googleapis.com/auth/userinfo.email"}, Scopes...)
	granted = granted[:len(granted)-1] // drop drive.metadata.readonly
	got := Missing(granted)
	if len(got) != 1 || !strings.HasSuffix(got[0], "drive.metadata.readonly") {
		t.Errorf("missing = %v", got)
	}
}

func TestTokenFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), tokenFile)
	if err := writeToken(path, stored{Account: "a", Token: &oauth2.Token{AccessToken: "x"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}
}
