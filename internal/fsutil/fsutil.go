// Package fsutil writes and reads small state files so that a power cut, crash or full disk
// cannot leave the user with an empty or half-written file.
package fsutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// WriteAtomic replaces path with data: the bytes are written to a temp file and flushed to disk
// before they take the old file's place, and the previous version is kept as path+".bak".
// (A plain write-then-rename can leave an empty file after a power cut, because the rename may
// reach the disk before the data does.)
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	return WriteAtomicOpts(path, data, perm, true)
}

// WriteAtomicOpts is WriteAtomic with the backup optional. Files that may hold a secret (the
// config with a fallback token) use backup=false so no stale copy of the secret is left behind.
func WriteAtomicOpts(path string, data []byte, perm os.FileMode, backup bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Keep the last good copy (only if the current file is worth keeping: non-empty).
	if st, err := os.Stat(path); backup && err == nil && st.Size() > 0 {
		_ = os.Rename(path, path+".bak")
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil { // make the rename itself durable (not supported on Windows: ignored)
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// ReadJSON loads path into v. If the file is missing, empty or damaged it falls back to the
// ".bak" copy; a damaged file is kept aside as path+".corrupt-<time>" instead of being overwritten,
// and recovered reports that the backup was used. A first run (no file at all) is not an error.
func ReadJSON(path string, v any) (recovered bool, err error) {
	return ReadJSONOpts(path, v, true)
}

// ReadJSONOpts is ReadJSON; keepCorrupt=false deletes a damaged file instead of keeping it aside
// (for files that may contain a secret).
func ReadJSONOpts(path string, v any, keepCorrupt bool) (recovered bool, err error) {
	// json.Valid first: a file that is not complete JSON must not leave v half filled.
	b, rerr := os.ReadFile(path)
	if rerr == nil && len(b) > 0 && json.Valid(b) && json.Unmarshal(b, v) == nil {
		return false, nil
	}
	if rerr == nil && len(b) > 0 { // damaged
		if keepCorrupt {
			_ = os.Rename(path, path+".corrupt-"+time.Now().Format("20060102-150405"))
		} else {
			_ = os.Remove(path)
		}
	}
	if bb, berr := os.ReadFile(path + ".bak"); berr == nil && len(bb) > 0 && json.Valid(bb) && json.Unmarshal(bb, v) == nil {
		return true, nil
	}
	if rerr != nil && os.IsNotExist(rerr) {
		return false, nil
	}
	return false, os.ErrInvalid
}
