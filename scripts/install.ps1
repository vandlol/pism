# pism installer for Windows (PowerShell).
#
#   irm https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.ps1 | iex
#
# Env overrides:
#   PISM_BASE_URL     download base (default: latest GitHub release)
#   PISM_VERSION      release tag to install (default: latest)
#   PISM_INSTALL_DIR  install directory (default: %LOCALAPPDATA%\pism\bin)
$ErrorActionPreference = 'Stop'

$repo = 'vandlol/pism'
$base = if ($env:PISM_BASE_URL) {
  $env:PISM_BASE_URL
} elseif ($env:PISM_VERSION) {
  "https://github.com/$repo/releases/download/$($env:PISM_VERSION)"
} else {
  "https://github.com/$repo/releases/latest/download"
}
$base = $base.TrimEnd('/')
$installDir = if ($env:PISM_INSTALL_DIR) { $env:PISM_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'pism\bin' }

# --- detect arch ---
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  'x86'   { 'amd64' } # 32-bit shell on 64-bit OS; ship amd64
  default { 'amd64' }
}
$asset = "pism-windows-$arch.exe"
$url = "$base/$asset"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$dest = Join-Path $installDir 'pism.exe'
$tmp  = [System.IO.Path]::GetTempFileName() + '.exe'

Write-Host "downloading $asset from $base ..."
Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing

if ((Get-Item $tmp).Length -lt 1024) {
  Remove-Item $tmp -Force
  throw "downloaded file too small - wrong URL?"
}

Move-Item -Force $tmp $dest
Write-Host "installed: $dest"
& $dest version

# --- ensure install dir is on the user PATH ---
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDir) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
  Write-Host ""
  Write-Host "Added $installDir to your user PATH. Open a NEW terminal for it to take effect."
}

Write-Host ""
Write-Host "try:  pism new                 (start + attach a pi session)"
Write-Host "      pism ls                  (list sessions by topic)"
Write-Host "      pism <host> ls           (manage sessions on a remote host over ssh)"
Write-Host "      pism update              (self-update in place)"
