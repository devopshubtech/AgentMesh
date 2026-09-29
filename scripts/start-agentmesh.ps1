# Starts the whole local AgentMesh stack for remote (mobile-data) use:
#   server + dashboard + public Cloudflare tunnel + Docker exit node.
#
# The free tunnel gets a new address on every restart. So that phone connect
# links (QR codes) keep working without a domain, this script publishes the
# current address to a private GitHub gist ("rendezvous"); the Android app
# looks it up there whenever the old address stops answering.
#
#   powershell -ExecutionPolicy Bypass -File scripts\start-agentmesh.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$compose = @("-f", (Join-Path $root "infrastructure\docker\docker-compose.yml"), "--profile", "public", "--profile", "exit")
$envFile = Join-Path $root "infrastructure\docker\.env"

function Get-EnvValue($name) {
    $line = Get-Content $envFile | Where-Object { $_ -like "$name=*" } | Select-Object -First 1
    if ($line) { return $line.Substring($name.Length + 1) } else { return "" }
}
function Set-EnvValue($name, $value) {
    $lines = @(Get-Content $envFile)
    if ($lines | Where-Object { $_ -like "$name=*" }) {
        $lines = $lines -replace "^$([regex]::Escape($name))=.*", "$name=$value"
    } else { $lines += "$name=$value" }
    $lines | Set-Content -Encoding ascii $envFile
}

# 1. Docker engine
docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Host "Starting Docker Desktop..."
    Start-Process "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe"
    for ($i = 0; $i -lt 120; $i++) { Start-Sleep 2; docker info *> $null; if ($LASTEXITCODE -eq 0) { break } }
    if ($LASTEXITCODE -ne 0) { throw "Docker did not start" }
}

# 2. Services
Write-Host "Starting AgentMesh services..."
docker compose @compose up -d --wait postgres nats control-api agent-gateway worker dashboard | Out-Null
docker compose @compose up -d tunnel exit-agent | Out-Null

# 3. Current public URL. Free quick tunnels can be dropped by Cloudflare
#    (e.g. after the PC slept); then cloudflared keeps retrying a dead address,
#    so check it from the outside and recreate the tunnel when it is gone.
function Get-TunnelUrl {
    for ($i = 0; $i -lt 60; $i++) {
        $u = docker compose @compose logs --no-log-prefix tunnel 2>&1 |
            Select-String -Pattern "https://[a-z0-9-]+\.trycloudflare\.com" |
            ForEach-Object { $_.Matches[0].Value } | Select-Object -Last 1
        if ($u) { return $u }
        Start-Sleep 1
    }
    return $null
}
function Test-PublicUrl($u) {
    for ($i = 0; $i -lt 10; $i++) {
        try { if ((Invoke-WebRequest -UseBasicParsing "$u/agentmesh-ca.pem" -TimeoutSec 8).StatusCode -eq 200) { return $true } } catch { }
        Start-Sleep 3
    }
    return $false
}
$url = Get-TunnelUrl
if (-not $url -or -not (Test-PublicUrl $url)) {
    Write-Host "Public tunnel is not reachable; creating a new one..."
    docker compose @compose up -d --force-recreate tunnel | Out-Null
    Start-Sleep 5
    $url = Get-TunnelUrl
    if (-not $url -or -not (Test-PublicUrl $url)) { throw "Tunnel did not come up; check: docker compose logs tunnel" }
}

# 4. Rendezvous gist (private) with the current address
$gistId = Get-EnvValue "AM_RENDEZVOUS_GIST_ID"
$token = ""
try {
    # PowerShell 5 pipes to native programs with CRLF/BOM, which git rejects; feed a file via cmd instead.
    $credIn = Join-Path $env:TEMP "agentmesh-cred-query.txt"
    [IO.File]::WriteAllText($credIn, "protocol=https`nhost=github.com`n`n")
    $out = cmd /c "set GIT_TERMINAL_PROMPT=0&& git credential fill < `"$credIn`" 2>nul"
    Remove-Item $credIn -ErrorAction SilentlyContinue
    $token = (($out | Where-Object { $_ -like "password=*" }) -replace "^password=", "") | Select-Object -First 1
} catch { $token = "" }
if ($token) {
    $h = @{ Authorization = "Bearer $token"; Accept = "application/vnd.github+json" }
    $content = @{ url = $url; updated = (Get-Date).ToUniversalTime().ToString("o") } | ConvertTo-Json -Compress
    $body = @{ description = "AgentMesh current server address (updated by start-agentmesh.ps1)"; public = $false
               files = @{ "agentmesh-endpoint.json" = @{ content = $content } } } | ConvertTo-Json -Depth 5
    try {
        if ($gistId) {
            $g = Invoke-RestMethod -Method Patch -Headers $h -ContentType "application/json" -Body $body "https://api.github.com/gists/$gistId"
        } else {
            $g = Invoke-RestMethod -Method Post -Headers $h -ContentType "application/json" -Body $body "https://api.github.com/gists"
            Set-EnvValue "AM_RENDEZVOUS_GIST_ID" $g.id
        }
        # The API URL always returns the latest content (the raw URL is cached ~5 min).
        Set-EnvValue "AM_RENDEZVOUS_URL" "https://api.github.com/gists/$($g.id)"
    } catch { Write-Warning "Could not update the rendezvous gist: $($_.Exception.Message)" }
} else { Write-Warning "No GitHub credential found; connect links will need re-sharing after restarts." }

# 5. Point the server at the current address (control-api is only recreated if its settings changed)
if ((Get-EnvValue "AM_PUBLIC_GATEWAY_URL") -ne $url) { Set-EnvValue "AM_PUBLIC_GATEWAY_URL" $url }
docker compose @compose up -d --wait control-api | Out-Null

# 6. This computer's own agent (shows it online in the dashboard). Runs from
#    the dev state dir when it was enrolled that way and no service is installed.
$agentExe = Join-Path $root "bin\agentmesh-agent.exe"
$agentState = Join-Path $root ".agent-dev\windows"
if (-not (Get-Service AgentMesh -ErrorAction SilentlyContinue) -and (Test-Path $agentExe) -and
    (Test-Path (Join-Path $agentState "agent.json")) -and -not (Get-Process agentmesh-agent -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath $agentExe -ArgumentList "run --state-dir `"$agentState`"" -WindowStyle Hidden `
        -RedirectStandardError (Join-Path $root ".agent-dev\windows-agent.log") `
        -RedirectStandardOutput (Join-Path $root ".agent-dev\windows-agent.out")
    Write-Host "Started this computer's agent ($env:COMPUTERNAME)."
}

Write-Host ""
Write-Host "AgentMesh is running." -ForegroundColor Green
Write-Host "  Public address : $url" -ForegroundColor Cyan
Write-Host "  Dashboard      : http://localhost:13000  ->  'Connect a phone' shows the QR code"
Write-Host "  Rendezvous     : $(Get-EnvValue 'AM_RENDEZVOUS_URL')"
