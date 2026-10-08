package inventory

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Remembered is a script id the person supplied, with what they said about
// it. It lives beside the token rather than in this repository: the ids are
// not secret, but the titles and notes are the account's business, and the
// repository may be public.
type Remembered struct {
	ScriptID string    `json:"script_id"`
	Title    string    `json:"title"`
	Note     string    `json:"note,omitempty"`
	Added    time.Time `json:"added"`
}

const rememberedFile = "remembered-scripts.json"

// LoadRemembered reads the list; a missing file is an empty list.
func LoadRemembered(dir string) ([]Remembered, error) {
	data, err := os.ReadFile(filepath.Join(dir, rememberedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []Remembered
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, err
	}
	return rs, nil
}

// SaveRemembered replaces the list, adding r or updating the entry with its
// id.
func SaveRemembered(dir string, r Remembered) ([]Remembered, error) {
	rs, err := LoadRemembered(dir)
	if err != nil {
		return nil, err
	}
	if i := slices.IndexFunc(rs, func(x Remembered) bool { return x.ScriptID == r.ScriptID }); i >= 0 {
		r.Added = rs[i].Added
		rs[i] = r
	} else {
		rs = append(rs, r)
	}
	return rs, writeRemembered(dir, rs)
}

// Forget removes an id; it reports whether it was there.
func Forget(dir, scriptID string) (bool, error) {
	rs, err := LoadRemembered(dir)
	if err != nil {
		return false, err
	}
	n := len(rs)
	rs = slices.DeleteFunc(rs, func(x Remembered) bool { return x.ScriptID == scriptID })
	if len(rs) == n {
		return false, nil
	}
	return true, writeRemembered(dir, rs)
}

func writeRemembered(dir string, rs []Remembered) error {
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rememberedFile), append(data, '\n'), 0o600)
}
