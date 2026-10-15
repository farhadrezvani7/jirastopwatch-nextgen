# Builds the app and adds a Start Menu shortcut (Windows, needs Go installed).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
go build -trimpath -ldflags "-s -w -H=windowsgui" -o jirastopwatch-nextgen.exe .
$lnk = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Jira StopWatch NextGen.lnk"
$s = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
$s.TargetPath = Join-Path $PSScriptRoot "jirastopwatch-nextgen.exe"
$s.WorkingDirectory = $PSScriptRoot
$s.Description = "Track time on Jira issues"
$s.Save()
Write-Host 'Installed. Search for "Jira StopWatch NextGen" in the Start Menu.'
