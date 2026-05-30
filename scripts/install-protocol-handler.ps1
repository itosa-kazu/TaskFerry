param(
  [string]$TaskFerryPath = "",
  [string]$BaseUrl = "http://127.0.0.1:4318",
  [string]$ApiToken = ""
)

$ErrorActionPreference = "Stop"

if ($TaskFerryPath -eq "") {
  $cmd = Get-Command taskferry.exe -ErrorAction SilentlyContinue
  if ($cmd) {
    $TaskFerryPath = $cmd.Source
  } elseif (Test-Path ".\taskferry.exe") {
    $TaskFerryPath = (Resolve-Path ".\taskferry.exe").Path
  } else {
    throw "Pass -TaskFerryPath or put taskferry.exe on PATH."
  }
}

$resolved = (Resolve-Path -LiteralPath $TaskFerryPath).Path
$schemeKey = "HKCU:\Software\Classes\taskferry"
$commandKey = "$schemeKey\shell\open\command"

New-Item -Path $schemeKey -Force | Out-Null
Set-Item -Path $schemeKey -Value "URL:TaskFerry Invite"
Set-ItemProperty -Path $schemeKey -Name "URL Protocol" -Value ""
New-Item -Path "$schemeKey\shell" -Force | Out-Null
New-Item -Path "$schemeKey\shell\open" -Force | Out-Null
New-Item -Path $commandKey -Force | Out-Null

$command = "`"$resolved`" --base-url `"$BaseUrl`""
if ($ApiToken -ne "") {
  $command += " --api-token `"$ApiToken`""
}
$command += " invite-open `"%1`""

Set-Item -Path $commandKey -Value $command

Write-Host "Registered taskferry:// protocol handler for current user."
Write-Host $command
