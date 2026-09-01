//go:build windows

package holder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolvePiCommand adapts the pi launch command for Windows. pi is commonly
// installed as a script shim (pi.cmd / pi.bat / pi.ps1) rather than a native
// .exe. ConPTY spawns processes via CreateProcess, which can only launch real
// executables — a batch/PowerShell shim fails instantly (holder exits 1). So
// when the resolved pi is a shim, wrap it in its interpreter.
//
// The interpreter path must be ABSOLUTE: in some environments (Cygwin/MSYS zsh,
// and even detached processes) the Windows PATH isn't resolvable by Go's
// lookup, so a bare "cmd.exe" gets treated as relative to the cwd
// (C:\Users\you\cmd.exe -> not found). We resolve cmd.exe / powershell.exe via
// %ComSpec% / %SystemRoot%.
func resolvePiCommand(name string, args []string) (string, []string) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return name, args // let Start fail with a clear "not found" error
	}
	switch {
	case hasSuffixFold(resolved, ".cmd"), hasSuffixFold(resolved, ".bat"):
		return cmdExe(), append([]string{"/c", resolved}, args...)
	case hasSuffixFold(resolved, ".ps1"):
		return powershellExe(),
			append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolved}, args...)
	default:
		return resolved, args
	}
}

func hasSuffixFold(s, suffix string) bool {
	return strings.HasSuffix(strings.ToLower(s), suffix)
}

// cmdExe returns an absolute path to cmd.exe.
func cmdExe() string {
	if c := os.Getenv("ComSpec"); c != "" {
		return c
	}
	return filepath.Join(systemRoot(), "System32", "cmd.exe")
}

// powershellExe returns an absolute path to Windows PowerShell.
func powershellExe() string {
	return filepath.Join(systemRoot(), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func systemRoot() string {
	if r := os.Getenv("SystemRoot"); r != "" {
		return r
	}
	if r := os.Getenv("windir"); r != "" {
		return r
	}
	return `C:\Windows`
}
