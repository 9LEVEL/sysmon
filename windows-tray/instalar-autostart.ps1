# Registra o traymon para iniciar junto com o Windows.
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
$ErrorActionPreference = "Stop"

$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path
$vbs   = Join-Path $pasta "traymon.vbs"

if (-not (Test-Path (Join-Path $pasta "config.json"))) {
    Write-Host "config.json nao encontrado." -ForegroundColor Red
    Write-Host "Copie config.example.json para config.json e preencha url + token."
    exit 1
}

# Teste rapido antes de registrar: falhar aqui e melhor que falhar no boot.
Write-Host "==> Testando o traymon (5s)..." -ForegroundColor Cyan
$p = Start-Process python -ArgumentList "`"$(Join-Path $pasta 'traymon.py')`"" `
     -WorkingDirectory $pasta -PassThru -WindowStyle Hidden
Start-Sleep 5
if ($p.HasExited) {
    Write-Host "O traymon morreu durante o teste. Rode 'python traymon.py' para ver o erro." -ForegroundColor Red
    Write-Host "Log: $env:TEMP\traymon.log"
    exit 1
}
Stop-Process -Id $p.Id -Force
Write-Host "    OK" -ForegroundColor Green

$acao = New-ScheduledTaskAction -Execute "wscript.exe" `
        -Argument "`"$vbs`"" -WorkingDirectory $pasta
$gatilho = New-ScheduledTaskTrigger -AtLogOn
$gatilho.Delay = "PT30S"
$cfg = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
       -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0 -RestartCount 3 `
       -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask -TaskName "traymon" -Action $acao -Trigger $gatilho `
                       -Settings $cfg -Force `
                       -Description "Monitor do host Proxmox na bandeja" | Out-Null

Write-Host "==> Tarefa 'traymon' registrada (inicia 30s apos o login)." -ForegroundColor Green
Write-Host "    Iniciar agora    : schtasks /run /tn traymon"
Write-Host "    Remover          : schtasks /delete /tn traymon /f"
