# Generates local development secrets for the Docker Compose stack:
#   infrastructure/docker/.env        ports, DB password, bootstrap admin
#   infrastructure/docker/.env.keys   token and command signing keys
#   infrastructure/docker/certs/      dev CA + gateway TLS certificate
# Existing files are kept unless -Force is given.
param([switch]$Force)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$docker = Join-Path $root "infrastructure\docker"

function New-Secret([int]$bytes = 24) {
    $b = New-Object byte[] $bytes
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
    return ([Convert]::ToBase64String($b) -replace '[+/=]', '')
}

$envFile = Join-Path $docker ".env"
if ($Force -or -not (Test-Path $envFile)) {
    $adminPw = New-Secret 18
    (Get-Content (Join-Path $docker ".env.example")) `
        -replace '^AM_PG_PASSWORD=.*', "AM_PG_PASSWORD=$(New-Secret)" `
        -replace '^AM_ADMIN_PASSWORD=.*', "AM_ADMIN_PASSWORD=$adminPw" |
        Set-Content -Encoding ascii $envFile
    Write-Host "Created $envFile"
    Write-Host "  Dashboard login: admin@agentmesh.local / $adminPw" -ForegroundColor Green
} else { Write-Host "Keeping existing $envFile" }

Push-Location $root
try {
    $keys = Join-Path $docker ".env.keys"
    if ($Force -or -not (Test-Path $keys)) {
        go run ./backend/cmd/amctl keygen | Set-Content -Encoding ascii $keys
        Write-Host "Created $keys"
    } else { Write-Host "Keeping existing $keys" }

    $certs = Join-Path $docker "certs"
    if ($Force -or -not (Test-Path (Join-Path $certs "gateway.pem"))) {
        go run ./backend/cmd/amctl dev-certs -out $certs
    } else { Write-Host "Keeping existing certificates in $certs" }
} finally { Pop-Location }

Write-Host "`nNext: docker compose -f infrastructure/docker/docker-compose.yml up -d --build"
