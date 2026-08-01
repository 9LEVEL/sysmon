# Remove o sysmon do autostart e encerra o que estiver rodando.
#   powershell -ExecutionPolicy Bypass -File desinstalar-autostart.ps1
$ErrorActionPreference = "Continue"

# Pasta Inicializar (o padrao, sem admin).
$atalhoInicio = Join-Path ([Environment]::GetFolderPath("Startup")) "sysmon.lnk"
if (Test-Path $atalhoInicio) {
    Remove-Item $atalhoInicio -Force
    Write-Host "    atalho de inicializacao removido" -ForegroundColor Green
}

# Tarefas agendadas, se alguem usou -Agendador (ou versoes antigas).
foreach ($t in @("sysmon", "traymon")) {
    $existe = Get-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue
    if ($existe) {
        Unregister-ScheduledTask -TaskName $t -Confirm:$false -ErrorAction SilentlyContinue
        if ($?) { Write-Host "    tarefa '$t' removida" -ForegroundColor Green }
        else    { Write-Host "    tarefa '$t' exige admin para remover" -ForegroundColor Yellow }
    }
}

# Atalho da area de trabalho.
$desktop = Join-Path ([Environment]::GetFolderPath("Desktop")) "sysmon.lnk"
if (Test-Path $desktop) { Remove-Item $desktop -Force }

Get-CimInstance Win32_Process -Filter "Name = 'sysmon.exe'" -ErrorAction SilentlyContinue |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }

Write-Host "sysmon removido do autostart." -ForegroundColor Green
