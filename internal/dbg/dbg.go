// Package dbg is a tiny leveled logger for pism's -v/-vv/-vvv diagnostics.
//
// Levels: 0 quiet (default), 1 info, 2 debug, 3 trace. Messages are written to
// stderr with a timestamp. In the detached holder, stderr is the session log
// file, so holder diagnostics land there automatically.
package dbg

import (
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

var level int32

func init() {
	if v := os.Getenv("PISM_VERBOSITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			SetLevel(n)
		}
	}
}

// SetLevel sets the active verbosity (0-3).
func SetLevel(n int) {
	if n < 0 {
		n = 0
	}
	atomic.StoreInt32(&level, int32(n))
}

// Level returns the active verbosity.
func Level() int { return int(atomic.LoadInt32(&level)) }

// Enabled reports whether messages at n would be emitted.
func Enabled(n int) bool { return Level() >= n }

// Logf emits a message if the active level is >= n.
func Logf(n int, format string, a ...any) {
	if Level() < n {
		return
	}
	tag := map[int]string{1: "info", 2: "debug", 3: "trace"}[n]
	if tag == "" {
		tag = "log"
	}
	fmt.Fprintf(os.Stderr, "%s pism[%s] %s\n",
		time.Now().Format("15:04:05.000"), tag, fmt.Sprintf(format, a...))
}
