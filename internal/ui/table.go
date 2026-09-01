// Package ui renders the session list table.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vandlol/pism/internal/manager"
)

// Render writes an aligned table of sessions.
func Render(w io.Writer, rows []manager.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no sessions. start one with:  pism new")
		return
	}
	headers := []string{"NAME", "ID", "S", "TOPIC", "DIR", "AGE"}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		state := "dead"
		if r.Alive {
			state = "live"
		}
		data = append(data, []string{
			nameOr(r.Meta.Name, r.Meta.ID),
			shortID(r.Meta.ID),
			state,
			r.Topic,
			abbrevHome(r.Meta.Cwd),
			humanAge(r.Age),
		})
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range data {
		for i, c := range row {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	printRow(w, headers, widths)
	for _, row := range data {
		printRow(w, row, widths)
	}
}

// RenderPorcelain writes one tab-separated line per session for machine
// consumption (used by `pism ls --all` to aggregate across hosts). Fields:
//
//	id \t state \t age_seconds \t dir \t topic
//
// The id is the full session id (not shortened) so callers can attach to it.
// Tabs/newlines in the topic are flattened to spaces to keep one record per
// line.
func RenderPorcelain(w io.Writer, rows []manager.Row) {
	for _, r := range rows {
		state := "dead"
		if r.Alive {
			state = "live"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			r.Meta.ID,
			nameOr(r.Meta.Name, r.Meta.ID),
			state,
			int(r.Age.Seconds()),
			abbrevHome(r.Meta.Cwd),
			flatten(r.Topic),
		)
	}
}

// MultiRow is a host-tagged session row for the aggregated `ls --all` view.
type MultiRow struct {
	Host  string
	Name  string
	ID    string // full id
	State string // "live" | "dead"
	Topic string
	Dir   string
	Age   time.Duration
}

// RenderMulti writes an aligned table of sessions across hosts, with a HOST
// column. IDs are shortened for display just like the single-host table.
func RenderMulti(w io.Writer, rows []MultiRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no sessions on any host. start one with:  pism new  (or: pism <host> new)")
		return
	}
	headers := []string{"HOST", "NAME", "ID", "S", "TOPIC", "DIR", "AGE"}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		data = append(data, []string{
			r.Host,
			nameOr(r.Name, r.ID),
			shortID(r.ID),
			r.State,
			r.Topic,
			r.Dir,
			humanAge(r.Age),
		})
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range data {
		for i, c := range row {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	printRow(w, headers, widths)
	for _, row := range data {
		printRow(w, row, widths)
	}
}

// nameOr returns the name, or a shortened id when the session has no name
// (e.g. created by an older pism).
func nameOr(name, id string) string {
	if name != "" {
		return name
	}
	return shortID(id)
}

func flatten(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}

func printRow(w io.Writer, cols []string, widths []int) {
	var b strings.Builder
	for i, c := range cols {
		pad := widths[i] - len([]rune(c))
		if pad < 0 {
			pad = 0
		}
		b.WriteString(c)
		if i < len(cols)-1 {
			b.WriteString(strings.Repeat(" ", pad+2))
		}
	}
	fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
}

// abbrevHome replaces a leading home directory with ~ for compact, non-
// identifying paths.
func abbrevHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
