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
	headers := []string{"ID", "S", "TOPIC", "DIR", "AGE"}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		state := "dead"
		if r.Alive {
			state = "live"
		}
		data = append(data, []string{
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
