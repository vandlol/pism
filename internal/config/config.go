// Package config manages pism's persistent configuration file.
//
// Location (via os.UserConfigDir, the OS-correct base):
//   - Linux:   $XDG_CONFIG_HOME/pism/config   (default ~/.config/pism/config)
//   - macOS:   ~/Library/Application Support/pism/config
//   - Windows: %AppData%\pism\config
//
// Override the whole path with $PISM_CONFIG. The file is a simple, hand-
// editable "key = value" format with '#' comments, and is created with a
// commented template on first run.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Keys is the set of recognised configuration keys, with descriptions.
var Keys = []struct{ Name, Desc string }{
	{"pi", "command used to launch pi (default: pi)"},
	{"detach-key", `detach key: ^\ , ctrl-o, a char, a code, or "none" (default: ^\)`},
	{"topic-len", "max topic width in ls (default: 40)"},
	{"remote-bin", "pism path on remote hosts (default: pism)"},
	{"ssh-config", "ssh config file to pass as -F (default: auto/none)"},
	{"update-url", "custom base URL for `pism update` (overrides channel)"},
	{"update-channel", "update channel: stable|latest, or unstable|dev|nightly (pre-releases)"},
	{"ready-timeout", "how long `new` waits for pi to come up (e.g. 30s, 5m, 0=forever)"},
}

func known(k string) bool {
	for _, e := range Keys {
		if e.Name == k {
			return true
		}
	}
	return false
}

// Config is an in-memory view of the config file.
type Config struct {
	path   string
	values map[string]string
}

// Path returns the resolved config file path.
func Path() string {
	if p := os.Getenv("PISM_CONFIG"); p != "" {
		return p
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "pism", "config")
}

// Load reads the config file, creating a commented template if it is missing.
func Load() (*Config, error) {
	c := &Config{path: Path(), values: map[string]string{}}
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		if err := writeTemplate(c.path); err != nil {
			return c, err // still usable with defaults
		}
	}
	f, err := os.Open(c.path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if known(k) && v != "" {
			c.values[k] = v
		}
	}
	return c, sc.Err()
}

// Get returns a value and whether it was set.
func (c *Config) Get(key string) (string, bool) {
	v, ok := c.values[key]
	return v, ok
}

// GetString returns the value or a fallback.
func (c *Config) GetString(key, def string) string {
	if v, ok := c.values[key]; ok {
		return v
	}
	return def
}

// GetInt returns an int value or a fallback.
func (c *Config) GetInt(key string, def int) int {
	if v, ok := c.values[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// All returns a sorted copy of set key/values.
func (c *Config) All() [][2]string {
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, c.values[k]})
	}
	return out
}

// Set validates and stores a key, then persists the file.
func (c *Config) Set(key, val string) error {
	if !known(key) {
		return fmt.Errorf("unknown config key %q (known: %s)", key, strings.Join(keyNames(), ", "))
	}
	if key == "topic-len" {
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Errorf("topic-len must be an integer: %v", err)
		}
	}
	if key == "update-channel" {
		switch strings.ToLower(val) {
		case "stable", "latest", "unstable", "dev", "nightly":
		default:
			return fmt.Errorf("update-channel must be one of: stable, latest, unstable, dev, nightly")
		}
	}
	c.values[key] = val
	return c.save()
}

// Unset removes a key and persists the file.
func (c *Config) Unset(key string) error {
	if !known(key) {
		return fmt.Errorf("unknown config key %q", key)
	}
	delete(c.values, key)
	return c.save()
}

func (c *Config) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(header)
	for _, e := range Keys {
		if v, ok := c.values[e.Name]; ok {
			fmt.Fprintf(&b, "%s = %s\n", e.Name, v)
		} else {
			fmt.Fprintf(&b, "# %s = \n", e.Name)
		}
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func writeTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(header)
	for _, e := range Keys {
		fmt.Fprintf(&b, "# %s = \n", e.Name)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func keyNames() []string {
	out := make([]string, len(Keys))
	for i, e := range Keys {
		out[i] = e.Name
	}
	return out
}

const header = `# pism configuration
# Managed with:  pism config <key> <value>   (get: pism config <key>; list: pism config --list)
# Precedence: command-line flag > this file > built-in default.
#
`
