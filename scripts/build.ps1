$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Push-Location $root
try {
    go test ./...
    go build -trimpath -o (Join-Path $dist "taskferry-relay.exe") ./cmd/relay
    go build -trimpath -o (Join-Path $dist "taskferry-client.exe") ./cmd/client
    go build -trimpath -o (Join-Path $dist "taskferry-writer-agent.exe") ./cmd/writer-agent
    go build -trimpath -o (Join-Path $dist "taskferry-requester-agent.exe") ./cmd/requester-agent
} finally {
    Pop-Location
}
