$ErrorActionPreference = "Stop"

$schemeKey = "HKCU:\Software\Classes\taskferry"
if (Test-Path $schemeKey) {
  Remove-Item -LiteralPath $schemeKey -Recurse -Force
  Write-Host "Removed taskferry:// protocol handler for current user."
} else {
  Write-Host "taskferry:// protocol handler was not registered for current user."
}
