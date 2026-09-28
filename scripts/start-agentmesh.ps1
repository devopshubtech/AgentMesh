# Starts the whole local AgentMesh stack for remote (mobile-data) use:
#   server + dashboard + public Cloudflare tunnel + Docker exit node.
# The quick-tunnel URL changes whenever the tunnel restarts, so this script
# reads the current one, points the server at it and prints it for the app.
#
#   powershell -ExecutionPolicy Bypass -File scripts\start-agentmesh.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$compose = @("-f", (Join-Path $root "infrastructure\docker\docker-compose.yml"), "--profile", "public", "--profile", "exit")
$envFile = Join-Path $root "infrastructure\docker\.env"

# 1. Docker engine
docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Host "Starting Docker Desktop..."
    Start-Process "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe"
    for ($i = 0; $i -lt 120; $i++) { Start-Sleep 2; docker info *> $null; if ($LASTEXITCODE -eq 0) { break } }
    if ($LASTEXITCODE -ne 0) { throw "Docker did not start" }
}

# 2. Services (exit-agent is already enrolled; its identity lives in a volume)
Write-Host "Starting AgentMesh services..."
docker compose @compose up -d --wait postgres nats control-api agent-gateway worker dashboard | Out-Null
docker compose @compose up -d tunnel exit-agent | Out-Null

# 3. Current public URL
$url = $null
for ($i = 0; $i -lt 60 -and -not $url; $i++) {
    Start-Sleep 1
    $url = docker compose @compose logs --no-log-prefix tunnel 2>&1 |
        Select-String -Pattern "https://[a-z0-9-]+\.trycloudflare\.com" |
        ForEach-Object { $_.Matches[0].Value } | Select-Object -Last 1
}
if (-not $url) { throw "Tunnel URL not found; check: docker compose logs tunnel" }

# 4. Point the server (relay URL, install snippets) at it
$current = (Get-Content $envFile | Where-Object { $_ -like "AM_PUBLIC_GATEWAY_URL=*" }) -replace "^AM_PUBLIC_GATEWAY_URL=", ""
if ($current -ne $url) {
    (Get-Content $envFile) -replace "^AM_PUBLIC_GATEWAY_URL=.*", "AM_PUBLIC_GATEWAY_URL=$url" | Set-Content -Encoding ascii $envFile
    docker compose @compose up -d --wait control-api | Out-Null
}
for ($i = 0; $i -lt 30; $i++) {
    try { if ((Invoke-WebRequest -UseBasicParsing "$url/agentmesh-ca.pem" -TimeoutSec 5).StatusCode -eq 200) { break } } catch { Start-Sleep 2 }
}

Write-Host ""
Write-Host "AgentMesh is running." -ForegroundColor Green
Write-Host "  Android app server URL : $url" -ForegroundColor Cyan
Write-Host "  Dashboard (this PC)    : http://localhost:13000"
Write-Host "  Exit node device       : home-exit-node"
