// Package update replaces the running pism binary in place with a build
// downloaded from GitHub releases.
//
// Channels:
//   - stable  (aka "latest"): the newest non-prerelease, via the stable
//     redirect /releases/latest/download/<asset>.
//   - unstable (aka "dev"/"nightly"): the newest release INCLUDING
//     pre-releases, resolved through the GitHub API.
//
// Set the channel with the `update-channel` config key or $PISM_UPDATE_CHANNEL;
// a custom `update-url` / $PISM_UPDATE_URL overrides the source entirely.
package update

import (
	"encoding/json"
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

// Repo is the GitHub owner/repo releases are pulled from. Override with
// $PISM_UPDATE_REPO.
const Repo = "vandlol/pism"

// stableBaseURL is the redirect that always resolves to the newest non-
// prerelease asset.
func stableBaseURL(repo string) string {
	return "https://github.com/" + repo + "/releases/latest/download"
}

// Options controls an update run.
type Options struct {
	CurrentVersion string
	Channel        string // "stable" (default) or "unstable"
	BaseURL        string // explicit source override (verbatim base); wins over Channel
	Repo           string // owner/repo for API lookups (default Repo)
}

// NormalizeChannel maps user-facing synonyms to "stable" or "unstable".
func NormalizeChannel(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "unstable", "dev", "nightly", "pre", "prerelease", "pre-release", "edge":
		return "unstable"
	default:
		return "stable"
	}
}

// AssetName is the canonical binary name for the current platform, e.g.
// "pism-darwin-arm64" or "pism-windows-amd64.exe".
func AssetName() string {
	n := fmt.Sprintf("pism-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// Run resolves the right asset for the requested channel, downloads it, and
// swaps it in for the currently-running executable.
func Run(o Options) error {
	repo := o.Repo
	if repo == "" {
		repo = Repo
	}
	if r := os.Getenv("PISM_UPDATE_REPO"); r != "" {
		repo = r
	}
	asset := AssetName()

	url, channelLabel, err := resolveAssetURL(o, repo, asset)
	if err != nil {
		return err
	}
	currentVersion := o.CurrentVersion

	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	dir := filepath.Dir(self)

	fmt.Fprintf(os.Stderr, "pism update [%s]: fetching %s\n", channelLabel, url)
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

// resolveAssetURL decides where to download the platform asset from, honouring
// an explicit BaseURL, then the channel. Returns the URL and a short label.
func resolveAssetURL(o Options, repo, asset string) (url, label string, err error) {
	if o.BaseURL != "" {
		return strings.TrimRight(o.BaseURL, "/") + "/" + asset, "custom", nil
	}
	if NormalizeChannel(o.Channel) == "unstable" {
		u, rel, e := latestReleaseAsset(repo, asset, true)
		if e != nil {
			return "", "unstable", e
		}
		return u, "unstable " + rel, nil
	}
	return stableBaseURL(repo) + "/" + asset, "stable", nil
}

// latestReleaseAsset queries the GitHub API for the newest release (including
// pre-releases when includePre is true) and returns the download URL of the
// asset matching name, plus the release tag.
func latestReleaseAsset(repo, name string, includePre bool) (string, string, error) {
	api := "https://api.github.com/repos/" + repo + "/releases?per_page=30"
	req, _ := http.NewRequest(http.MethodGet, api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	// Public releases are readable anonymously; we deliberately do NOT send
	// $GITHUB_TOKEN (a stale/invalid one would cause 401).
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("query releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("query releases: GitHub returned %s", resp.Status)
	}
	var rels []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return "", "", fmt.Errorf("parse releases: %w", err)
	}
	// GitHub returns releases newest-first. Pick the first non-draft release
	// (any prerelease allowed when includePre) that has our asset.
	for _, r := range rels {
		if r.Draft {
			continue
		}
		if r.Prerelease && !includePre {
			continue
		}
		for _, a := range r.Assets {
			if a.Name == name {
				return a.URL, r.Tag, nil
			}
		}
	}
	return "", "", fmt.Errorf("no release with asset %s found for %s", name, repo)
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
