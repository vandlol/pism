package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Topic returns a short human label for a session: the pi session's explicit
// name if set, else its first user message, else "pi". It links to the pi
// transcript by session id (filename contains the uuid we passed as
// --session-id), falling back to the newest transcript in the cwd slug.
func Topic(id, cwd string, maxLen int) string {
	f := transcriptForID(id)
	if f == "" {
		f = newestTranscriptInCwd(cwd)
	}
	if f == "" {
		return "pi"
	}
	t := parseTopic(f)
	if t == "" {
		t = "pi"
	}
	return truncate(collapseWS(t), maxLen)
}

// transcriptForID globs pi's sessions dir for a file containing the id.
func transcriptForID(id string) string {
	if id == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(PiSessionsDir(), "*", "*"+id+"*.jsonl"))
	return newestOf(matches)
}

// newestTranscriptInCwd reproduces pi's cwd->slug encoding: strip leading
// separator, replace path separators with '-', wrap in '--...--'.
func newestTranscriptInCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	p := filepath.ToSlash(cwd)
	p = strings.TrimPrefix(p, "/")
	// Windows drive letter "C:/x" -> "C:-x"? pi runs on the same OS; keep it
	// simple and match the '/'->'-' rule pi uses for the recorded cwd.
	slug := "--" + strings.ReplaceAll(p, "/", "-") + "--"
	matches, _ := filepath.Glob(filepath.Join(PiSessionsDir(), slug, "*.jsonl"))
	return newestOf(matches)
}

func newestOf(paths []string) string {
	var best string
	var bestMod int64
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if m := fi.ModTime().UnixNano(); m > bestMod {
			bestMod, best = m, p
		}
	}
	return best
}

// parseTopic scans a JSONL transcript for a session name or first user text.
func parseTopic(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var name, firstUser string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		switch rec.Type {
		case "session_name", "name_change":
			if rec.Name != "" {
				name = rec.Name
			}
		case "message":
			if firstUser == "" && rec.Message.Role == "user" {
				for _, c := range rec.Message.Content {
					if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
						firstUser = strings.TrimSpace(c.Text)
						break
					}
				}
			}
		}
	}
	if name != "" {
		return name
	}
	return firstUser
}

func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, max int) string {
	if max <= 0 {
		max = 40
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
