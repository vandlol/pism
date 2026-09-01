// Package update replaces the running pism binary in place with a build
// downloaded from an update server. By default it pulls the latest GitHub
// release asset; override with the `update-url` config key or $PISM_UPDATE_URL.
package update

import (
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

// DefaultBaseURL is where release binaries are served: the latest GitHub
// release. GitHub redirects /releases/latest/download/<asset> to the asset of
// the most recent release, so this always fetches the newest published build.
const DefaultBaseURL = "https://github.com/vandlol/pism/releases/latest/download"

// AssetName is the canonical binary name for the current platform, e.g.
// "pism-darwin-arm64" or "pism-windows-amd64.exe".
func AssetName() string {
	n := fmt.Sprintf("pism-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// Run downloads the matching binary from baseURL and swaps it in for the
// currently-running executable. currentVersion is printed for context.
func Run(baseURL, currentVersion string) error {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	asset := AssetName()
	url := baseURL + "/" + asset

	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	dir := filepath.Dir(self)

	fmt.Fprintf(os.Stderr, "pism update: fetching %s\n", url)
	body, err := download(url)
	if err != nil {
		return err
	}

	// Write to a temp file in the SAME directory (so the final rename is
	// atomic on the same filesystem) with a runnable extension.
	pattern := "pism-update-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe"
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temp (is %s writable?): %w", dir, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Validate the download actually runs and report its version.
	newVer := probeVersion(tmpPath)
	if newVer == "" {
		os.Remove(tmpPath)
		return fmt.Errorf("downloaded binary failed to run; aborting")
	}

	if err := replaceExecutable(self, tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	fmt.Fprintf(os.Stderr, "pism updated: %s -> %s  (%s)\n", currentVersion, newVer, self)
	return nil
}

func download(url string) ([]byte, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: server returned %s for the current platform (%s)", resp.Status, AssetName())
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(b) < 1024 {
		return nil, fmt.Errorf("download: suspiciously small (%d bytes)", len(b))
	}
	return b, nil
}

// probeVersion runs `<bin> version` and returns the trimmed output.
func probeVersion(bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
