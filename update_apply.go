package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// assetName is the release file for this platform (see build-all.sh).
func assetName(goos, goarch string) string {
	n := "jirastopwatch-nextgen-" + goos + "-" + goarch
	if goos == "windows" {
		n += ".exe"
	}
	return n
}

// checksumFor finds name in the text of a SHA256SUMS.txt file ("<hex>  <name>" per line).
func checksumFor(sums, name string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// exePath is the file to replace: the running binary with symlinks resolved.
func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// writable reports whether we can create files next to the binary (needed to swap it).
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".update-probe-*")
	if err != nil {
		return false
	}
	f.Close()
	return os.Remove(f.Name()) == nil
}

func get(ctx context.Context, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jirastopwatch-nextgen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit), resp.Body}, nil
}

var errNotWritable = errors.New("err.updNoWrite")

// downloadUpdate fetches this platform's binary for tag next to the running one, verifies its
// SHA-256 against the release's SHA256SUMS.txt and that it starts, and returns its path.
func downloadUpdate(ctx context.Context, tag, exe string) (string, error) {
	dir := filepath.Dir(exe)
	var stage string // private temp folder for an elevated install; removed unless the download succeeds
	succeeded := false
	defer func() {
		if !succeeded && stage != "" {
			os.RemoveAll(stage)
		}
	}()
	if !writable(dir) {
		if !canElevate() {
			return "", errNotWritable
		}
		// Installed system-wide (.deb/.rpm): stage the download in a private temp folder; swapBinary
		// then copies it into place through pkexec, which shows the system password prompt.
		tmpDir, err := os.MkdirTemp("", "jsw-update-")
		if err != nil {
			return "", err
		}
		dir = tmpDir
		stage = tmpDir
	}
	base := downloadBase + "/" + updateRepo + "/releases/download/" + tag + "/"
	name := assetName(runtime.GOOS, runtime.GOARCH)

	sumsBody, err := get(ctx, base+"SHA256SUMS.txt", 1<<20)
	if err != nil {
		return "", err
	}
	sums, _ := io.ReadAll(sumsBody)
	sumsBody.Close()
	want, ok := checksumFor(string(sums), name)
	if !ok {
		return "", fmt.Errorf("no checksum published for %s", name)
	}

	body, err := get(ctx, base+name, 200<<20)
	if err != nil {
		return "", err
	}
	defer body.Close()
	tmp, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return "", errNotWritable
	}
	ok = false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), body); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", errors.New("the download is corrupted (checksum mismatch)")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	// Windows only runs files that end in .exe.
	path := tmp.Name()
	if runtime.GOOS == "windows" {
		path += ".exe"
		if err := os.Rename(tmp.Name(), path); err != nil {
			return "", err
		}
		defer func() {
			if !ok {
				os.Remove(path)
			}
		}()
	}
	// The new binary must report the version we expect: catches a wrong-architecture or broken file.
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, path, "--version").Output()
	if err != nil || !strings.Contains(string(out), strings.TrimPrefix(tag, "v")) {
		return "", errors.New("the downloaded file does not run on this computer")
	}
	ok = true
	succeeded = true
	return path, nil
}

// swapBinary puts newPath in place of exe. Unix allows renaming over a running binary; Windows
// does not, but allows renaming the running file out of the way first.
func swapBinary(newPath, exe string) error {
	if !writable(filepath.Dir(exe)) && canElevate() {
		defer os.RemoveAll(filepath.Dir(newPath))
		// Copy next to the target first, then rename over it: a power cut mid-copy must never leave a truncated binary.
		script := `install -m 0755 "$1" "$2.new" && mv -f "$2.new" "$2" || { rm -f "$2.new"; exit 1; }`
		if out, err := exec.Command("pkexec", "sh", "-c", script, "sh", newPath, exe).CombinedOutput(); err != nil {
			return fmt.Errorf("could not install with administrator rights: %v %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, exe)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe) // put the working version back
		return err
	}
	return nil
}

// canElevate reports whether a protected install can be updated by asking for the
// administrator password (Linux polkit).
var canElevate = func() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("pkexec")
	return err == nil
}

// removeOldBinary deletes the file a previous Windows update left behind.
func removeOldBinary() {
	exe, err := exePath()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
	// A download cut short by a power failure or crash leaves ".update-*" files next to the binary.
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".update-*")); len(left) > 0 {
		for _, f := range left {
			if st, err := os.Stat(f); err == nil && time.Since(st.ModTime()) > 10*time.Minute {
				_ = os.Remove(f)
			}
		}
	}
}
