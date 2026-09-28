<#
AgentMesh agent installer for Windows (run in an elevated PowerShell).

  irm https://raw.githubusercontent.com/devopshubtech/AgentMesh/main/scripts/install/install.ps1 -OutFile install.ps1
  .\install.ps1 -Server https://<gateway>:18443 -Token am_enr_... [-CaFile .\ca.pem] [-EnableExitNode]

If agentmesh-agent.exe sits next to this script (extracted release zip) it is
used; otherwise the matching zip is downloaded from the latest GitHub release
and verified against SHA256SUMS before installing.
#>
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$CaFile,
    [switch]$EnableExitNode,
    [string]$ReleaseUrl = "https://github.com/devopshubtech/AgentMesh/releases/latest/download"
)
$ErrorActionPreference = "Stop"

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this script in an elevated (Administrator) PowerShell."
}

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$exe = Join-Path $PSScriptRoot "agentmesh-agent.exe"
if (-not ($PSScriptRoot -and (Test-Path $exe))) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $tmp = Join-Path $env:TEMP ("agentmesh-" + [guid]::NewGuid())
    New-Item -ItemType Directory $tmp | Out-Null
    Write-Host ">> finding latest release for windows/$arch"
    $sums = (Invoke-WebRequest -UseBasicParsing "$ReleaseUrl/SHA256SUMS").Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $line = ($sums -split "`n") | Where-Object { $_ -match "agentmesh-agent_\S+_windows_$arch\.zip" } | Select-Object -First 1
    if (-not $line) { throw "No release zip for windows/$arch" }
    $want, $asset = ($line.Trim() -split "\s+", 2)
    Write-Host ">> downloading $asset"
    $zip = Join-Path $tmp $asset
    Invoke-WebRequest -UseBasicParsing "$ReleaseUrl/$asset" -OutFile $zip
    $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "Checksum mismatch for $asset" }
    Write-Host ">> checksum verified"
    Expand-Archive $zip -DestinationPath $tmp -Force
    $exe = Join-Path $tmp "agentmesh-agent.exe"
    Unblock-File $exe
}

& $exe version
$argsList = @("install", "--server", $Server, "--token", $Token)
if ($CaFile) { $argsList += @("--ca-file", (Resolve-Path $CaFile).Path) }
if ($EnableExitNode) { $argsList += "--enable-exit-node" }
& $exe @argsList
if ($LASTEXITCODE -ne 0) { throw "agentmesh-agent install failed ($LASTEXITCODE)" }
