# Registra o sysmon para iniciar junto com o Windows.
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
#
# Um processo so: sobe o dashboard web e, se pystray/Pillow estiverem
# instalados, o icone de bandeja junto.
$ErrorActionPreference = "Stop"

$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path
$vbs   = Join-Path $pasta "sysmon.vbs"
$pyz   = Join-Path $pasta "sysmon.pyz"

if (-not (Test-Path $pyz)) {
    Write-Host "sysmon.pyz nao encontrado nesta pasta." -ForegroundColor Red
    Write-Host "Baixe o pacote de clientes do release e deixe os dois arquivos juntos."
    exit 1
}
if (-not (Test-Path (Join-Path $pasta "config.json"))) {
    Write-Host "config.json nao encontrado." -ForegroundColor Red
    Write-Host "Copie config.example.json para config.json e preencha url + token,"
    Write-Host "ou use o hosts.json que o deploy.sh gerou."
    exit 1
}

# Teste rapido antes de registrar: falhar aqui e melhor que falhar no boot.
# Em modo --browser --nao-abrir para nao piscar janela na tela do usuario; o
# que se quer verificar aqui e config e porta, que e onde as coisas falham.
Write-Host "==> Testando (6s)..." -ForegroundColor Cyan
$p = Start-Process python -ArgumentList "`"$pyz`"", "--browser", "--nao-abrir" `
     -WorkingDirectory $pasta -PassThru -WindowStyle Hidden
Start-Sleep 6
if ($p.HasExited) {
    Write-Host "O sysmon morreu durante o teste." -ForegroundColor Red
    Write-Host "Rode 'python sysmon.pyz' num terminal para ver o erro."
    Write-Host "Log da bandeja: $env:TEMP\traymon.log"
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

Register-ScheduledTask -TaskName "sysmon" -Action $acao -Trigger $gatilho `
                       -Settings $cfg -Force `
                       -Description "Monitor da frota Linux (dashboard + bandeja)" | Out-Null

# A tarefa antiga da v2.1 e anteriores chamava outro script; remove para nao
# ficarem duas instancias disputando a mesma porta.
Unregister-ScheduledTask -TaskName "traymon" -Confirm:$false -ErrorAction SilentlyContinue

# Atalho na area de trabalho: abrir sem terminal e sem procurar a pasta.
try {
    $desktop  = [Environment]::GetFolderPath("Desktop")
    $atalho   = Join-Path $desktop "sysmon.lnk"
    $ws       = New-Object -ComObject WScript.Shell
    $lnk      = $ws.CreateShortcut($atalho)
    $lnk.TargetPath       = "wscript.exe"
    # Sem --oculto: clicar no atalho e pedir a janela agora.
    $lnk.Arguments        = "`"$vbs`" --nao-oculto"
    $lnk.WorkingDirectory = $pasta
    $lnk.Description      = "Monitor da frota Linux"
    $lnk.IconLocation     = "$((Get-Command python).Source),0"
    $lnk.Save()
    Write-Host "==> Atalho criado na area de trabalho." -ForegroundColor Green
} catch {
    Write-Host "    (nao consegui criar o atalho: $_)" -ForegroundColor Yellow
}

Write-Host "==> Tarefa 'sysmon' registrada (inicia 30s apos o login)." -ForegroundColor Green
Write-Host "    Dashboard        : http://127.0.0.1:9110/"
Write-Host "    Iniciar agora    : schtasks /run /tn sysmon"
Write-Host "    Remover          : schtasks /delete /tn sysmon /f"
