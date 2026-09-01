//go:build windows

package holder

import (
	"os/exec"
	"strings"
)

// resolvePiCommand adapts the pi launch command for Windows. pi is commonly
// installed as a script shim (pi.cmd / pi.bat / pi.ps1) rather than a native
// .exe. ConPTY spawns processes via CreateProcess, which can only launch real
// executables — a batch/PowerShell shim fails instantly (holder exits 1). So
// when the resolved pi is a shim, wrap it in its interpreter.
func resolvePiCommand(name string, args []string) (string, []string) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		// Let Start fail with a clear "not found" error.
		return name, args
	}
	switch {
	case hasSuffixFold(resolved, ".cmd"), hasSuffixFold(resolved, ".bat"):
		// cmd.exe /c <shim> <args...>
		return "cmd.exe", append([]string{"/c", resolved}, args...)
	case hasSuffixFold(resolved, ".ps1"):
		return "powershell.exe", append(
			[]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolved}, args...)
	default:
		return resolved, args
	}
}

func hasSuffixFold(s, suffix string) bool {
	return strings.HasSuffix(strings.ToLower(s), suffix)
}
