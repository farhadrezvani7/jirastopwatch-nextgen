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
	if !writable(dir) {
		return "", errNotWritable
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
	return path, nil
}

// swapBinary puts newPath in place of exe. Unix allows renaming over a running binary; Windows
// does not, but allows renaming the running file out of the way first.
func swapBinary(newPath, exe string) error {
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

// removeOldBinary deletes the file a previous Windows update left behind.
func removeOldBinary() {
	if exe, err := exePath(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
