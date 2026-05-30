param(
    [Parameter(Mandatory = $true)]
    [string]$Owner,

    [string]$RelayHost = "relay.example.com",
    [int]$LocalPort = 4318
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function New-Token([int]$Bytes = 32) {
    $data = [byte[]]::new($Bytes)
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($data)
    } finally {
        $rng.Dispose()
    }
    return [Convert]::ToBase64String($data).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

function Normalize-Owner([string]$Value) {
    $normalized = $Value.ToLowerInvariant() -replace "[^a-z0-9_-]", "-"
    $normalized = $normalized.Trim("-")
    if ($normalized.Length -eq 0) {
        throw "Owner must contain at least one ASCII letter or digit."
    }
    return $normalized
}

$ownerSlug = Normalize-Owner $Owner
$clientID = "client_$ownerSlug"
$deviceID = "device_$ownerSlug"
$relayToken = New-Token
$localToken = New-Token

$relayHTTP = "https://$RelayHost"
$relayWS = "wss://$RelayHost/v1/ws"

Write-Output "Relay token mapping for operator .env:"
Write-Output "$clientID=$relayToken"
Write-Output ""
Write-Output "Private onboarding block for this user:"
Write-Output @"
Relay HTTP: $relayHTTP
Relay WS: $relayWS
Client ID: $clientID
Device ID: $deviceID
Relay Token: $relayToken
Suggested Local API Token: $localToken
Local Port: $LocalPort

PowerShell startup:
`$env:TASKFERRY_CLIENT_ADDR="127.0.0.1:$LocalPort"
`$env:TASKFERRY_CLIENT_ID="$clientID"
`$env:TASKFERRY_DEVICE_ID="$deviceID"
`$env:TASKFERRY_CLIENT_DB=".taskferry\$clientID.db"
`$env:TASKFERRY_RELAY_HTTP="$relayHTTP"
`$env:TASKFERRY_RELAY_WS="$relayWS"
`$env:TASKFERRY_RELAY_TOKEN="$relayToken"
`$env:TASKFERRY_LOCAL_API_TOKEN="$localToken"
go run ./cmd/client

Dashboard:
http://127.0.0.1:$LocalPort/?token=$localToken
"@
