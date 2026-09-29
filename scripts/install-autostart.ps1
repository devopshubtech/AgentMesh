# Keeps AgentMesh up without anyone logged in at the keyboard: registers a
# Windows scheduled task (current user) that runs start-agentmesh.ps1 at logon
# and every 5 minutes. The script is idempotent: it only repairs what is down
# (Docker, containers, an expired public tunnel) and republishes the address.
#
#   powershell -ExecutionPolicy Bypass -File scripts\install-autostart.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\install-autostart.ps1 -Remove
param([switch]$Remove)
$ErrorActionPreference = "Stop"
$name = "AgentMesh watchdog"
if ($Remove) {
    Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
    Write-Host "Removed scheduled task '$name'."
    return
}
$script = Join-Path $PSScriptRoot "start-agentmesh.ps1"
$log = Join-Path (Split-Path -Parent $PSScriptRoot) ".agent-dev\watchdog.log"
New-Item -ItemType Directory -Force (Split-Path $log) | Out-Null
$action = New-ScheduledTaskAction -Execute "powershell.exe" `
    -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -Command `"& '$script' *> '$log'`""
$triggers = @(
    (New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"),
    (New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 5))
)
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable `
    -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 10)
Register-ScheduledTask -TaskName $name -Action $action -Trigger $triggers -Settings $settings `
    -Description "Keeps the AgentMesh server, public tunnel and exit node running." -Force | Out-Null
Write-Host "Registered '$name': runs at logon and every 5 minutes (log: $log)."
