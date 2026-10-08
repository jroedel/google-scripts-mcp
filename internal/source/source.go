// Package source writes a script's files into a local directory and keeps
// enough of a record to know, later, whether they have been edited since.
//
// # Why the record
//
// The bugs get fixed in local files, under git, with an editor and a diff --
// not by passing whole sources through tool arguments. That makes a pull
// destructive in the one way that matters: pulling again over a file somebody
// edited and has not pushed loses the edit. So every pull writes
// .gas-pull.json beside the files, with the hash of each file as written, and
// the next pull into that directory refuses to replace a file whose hash no
// longer matches. The same record is what a push will check the project's
// remote update time against, so that an edit made in the web editor since
// the pull is not silently overwritten either.
//
// .clasp.json is written too, holding only the script id, so that clasp can
// be pointed at the same directory if it is ever the easier tool.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jroedel/google-scripts-mcp/internal/gas"
)

// RecordFile is the pull record's name.
const RecordFile = ".gas-pull.json"

// Record is what a pull leaves behind.
type Record struct {
	ScriptID string    `json:"scriptId"`
	Title    string    `json:"title"`
	PulledAt time.Time `json:"pulledAt"`
	// RemoteUpdated is the project's update time when it was pulled.
	RemoteUpdated time.Time `json:"remoteUpdated,omitzero"`
	// Files maps each written path, relative and slash-separated, to the
	// SHA-256 of what was written.
	Files map[string]string `json:"files"`
}

// LocalName is the file name a project file is written under. Apps Script
// names carry no extension and may contain "/", which the editor shows as
// folders; clasp maps those to directories, and so does this.
func LocalName(f gas.File) (string, error) {
	var ext string
	switch f.Type {
	case gas.FileServerJS:
		ext = ".gs"
	case gas.FileHTML:
		ext = ".html"
	case gas.FileJSON:
		ext = ".json"
	default:
		return "", fmt.Errorf("file %q has type %q, which this tool does not know how to write", f.Name, f.Type)
	}
	name := f.Name + ext
	// A name from the API is not trusted to stay inside the directory.
	// os.Root below enforces that as well; checking here gives the person a
	// sentence instead of a "path escapes from parent" error.
	if !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("file name %q would land outside the directory; refusing to write it", f.Name)
	}
	return path.Clean(name), nil
}

// Pulled is what Pull wrote and removed.
type Pulled struct {
	Dir     string   `json:"dir"`
	Written []string `json:"written"`
	Removed []string `json:"removed,omitempty"`
}

// Pull writes content into dir, which must be absolute. dir may be new, empty,
// or a directory an earlier pull of the same script wrote; anything else is
// refused, and so is replacing a file edited since that pull.
func Pull(dir string, p gas.Project, content gas.Content) (Pulled, error) {
	if !filepath.IsAbs(dir) {
		return Pulled{}, fmt.Errorf("%q is not an absolute path", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Pulled{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Pulled{}, err
	}
	defer root.Close()

	prev, err := readRecord(root)
	if err != nil {
		return Pulled{}, err
	}
	if prev == nil {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Pulled{}, err
		}
		// A .git directory is allowed: initialising the repository before the
		// first pull is the order a person would naturally do it in.
		for _, e := range entries {
			if e.Name() != ".git" {
				return Pulled{}, fmt.Errorf("%s is not empty and was not written by a pull. Choose a new directory", dir)
			}
		}
	} else if prev.ScriptID != content.ScriptID {
		return Pulled{}, fmt.Errorf("%s holds a different script (%s, %q). Choose another directory", dir, prev.ScriptID, prev.Title)
	} else if edited, err := Edited(root, prev); err != nil {
		return Pulled{}, err
	} else if len(edited) > 0 {
		return Pulled{}, fmt.Errorf("these files were changed since the last pull, and pulling would overwrite them: %s. "+
			"Commit or move them first", strings.Join(edited, ", "))
	}

	rec := Record{
		ScriptID:      content.ScriptID,
		Title:         p.Title,
		PulledAt:      time.Now().UTC().Truncate(time.Second),
		RemoteUpdated: p.UpdateTime,
		Files:         map[string]string{},
	}
	var out Pulled
	out.Dir = dir
	for _, f := range content.Files {
		name, err := LocalName(f)
		if err != nil {
			return Pulled{}, err
		}
		if d := path.Dir(name); d != "." {
			if err := root.MkdirAll(d, 0o755); err != nil {
				return Pulled{}, err
			}
		}
		if err := root.WriteFile(name, []byte(f.Source), 0o644); err != nil {
			return Pulled{}, err
		}
		rec.Files[name] = hash([]byte(f.Source))
		out.Written = append(out.Written, name)
	}

	// A file deleted in the editor since the last pull goes here too, or the
	// directory would keep it and a later push would put it back. Edited()
	// above already established that every one of these is unchanged.
	if prev != nil {
		for _, name := range slices.Sorted(maps.Keys(prev.Files)) {
			if _, ok := rec.Files[name]; ok {
				continue
			}
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return Pulled{}, err
			}
			out.Removed = append(out.Removed, name)
		}
	}

	if err := writeJSON(root, RecordFile, rec); err != nil {
		return Pulled{}, err
	}
	if err := writeJSON(root, ".clasp.json", map[string]string{"scriptId": content.ScriptID}); err != nil {
		return Pulled{}, err
	}
	slices.Sort(out.Written)
	return out, nil
}

// Edited lists the files in rec whose content on disk no longer matches what
// the pull wrote. A file that is missing counts as edited: deleting it was a
// change somebody made.
func Edited(root *os.Root, rec *Record) ([]string, error) {
	var edited []string
	for _, name := range slices.Sorted(maps.Keys(rec.Files)) {
		data, err := root.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			edited = append(edited, name+" (deleted)")
			continue
		}
		if err != nil {
			return nil, err
		}
		if hash(data) != rec.Files[name] {
			edited = append(edited, name)
		}
	}
	return edited, nil
}

func readRecord(root *os.Root) (*Record, error) {
	data, err := root.ReadFile(RecordFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%s is unreadable: %w", RecordFile, err)
	}
	return &rec, nil
}

func writeJSON(root *os.Root, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return root.WriteFile(name, append(data, '\n'), 0o644)
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
