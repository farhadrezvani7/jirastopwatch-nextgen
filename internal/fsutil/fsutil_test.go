package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

type doc struct{ N int }

func TestWriteAtomicKeepsBackupAndReadRecovers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "x.json")
	if err := WriteAtomic(p, []byte(`{"N":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte(`{"N":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var d doc
	if rec, err := ReadJSON(p, &d); err != nil || rec || d.N != 2 {
		t.Fatalf("normal read: %+v rec=%v err=%v", d, rec, err)
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != `{"N":1}` {
		t.Errorf("backup = %q", b)
	}

	// power cut left an empty file: fall back to the backup
	os.WriteFile(p, nil, 0o600)
	d = doc{}
	if rec, err := ReadJSON(p, &d); err != nil || !rec || d.N != 1 {
		t.Errorf("empty file: %+v rec=%v err=%v", d, rec, err)
	}
	// half-written file: the damaged copy is kept, the backup is used
	os.WriteFile(p, []byte(`{"N":`), 0o600)
	d = doc{}
	if rec, err := ReadJSON(p, &d); err != nil || !rec || d.N != 1 {
		t.Errorf("truncated file: %+v rec=%v err=%v", d, rec, err)
	}
	if m, _ := filepath.Glob(p + ".corrupt-*"); len(m) != 1 {
		t.Errorf("damaged file not kept aside: %v", m)
	}
}

func TestReadJSONFirstRunAndUnrecoverable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "none.json")
	var d doc
	if rec, err := ReadJSON(p, &d); err != nil || rec {
		t.Errorf("first run should be fine: rec=%v err=%v", rec, err)
	}
	os.WriteFile(p, []byte("garbage"), 0o600)
	if _, err := ReadJSON(p, &d); err == nil {
		t.Error("damaged file with no backup should report an error")
	}
}

func TestNoBackupOrCorruptCopyForSecretFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	WriteAtomicOpts(p, []byte(`{"N":1,"token":"secret"}`), 0o600, false)
	WriteAtomicOpts(p, []byte(`{"N":2}`), 0o600, false)
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("a backup of a secret-bearing file was kept")
	}
	os.WriteFile(p, []byte(`{"N":`), 0o600)
	var d doc
	ReadJSONOpts(p, &d, false)
	if m, _ := filepath.Glob(p + "*"); len(m) != 0 {
		t.Errorf("damaged secret-bearing file left behind: %v", m)
	}
}

func TestPartialJSONNeverFillsTheValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(p, []byte(`{"N":7,"extra":`), 0o600) // cut off after a complete field
	d := doc{N: 1}
	ReadJSON(p, &d)
	if d.N != 1 {
		t.Errorf("half a file leaked into the value: %+v", d)
	}
}
