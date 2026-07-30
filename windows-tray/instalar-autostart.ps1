# Registra o sysmon para iniciar junto com o Windows.
#
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1 -Agendador
#
# Por padrao usa a pasta Inicializar, que NAO exige administrador. O Agendador
# de Tarefas so entra com -Agendador: registrar tarefa na raiz da biblioteca
# pede elevacao, e um monitor de bandeja nao deveria precisar disso.
#
# Um processo so: sobe o dashboard e, se pystray/Pillow estiverem instalados,
# o icone de bandeja junto.
param(
    [switch]$Agendador,   # usa o Agendador de Tarefas (precisa de admin)
    [int]$AtrasoSeg = 20  # espera antes de subir, para a rede estabilizar
)

# Sem "Stop" global: um passo opcional que falhe nao pode abortar os demais.
$ErrorActionPreference = "Continue"

$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path
$vbs   = Join-Path $pasta "sysmon.vbs"
$pyz   = Join-Path $pasta "sysmon.pyz"

function Ok($m)    { Write-Host "    $m" -ForegroundColor Green }
function Aviso($m) { Write-Host "    $m" -ForegroundColor Yellow }
function Erro($m)  { Write-Host $m -ForegroundColor Red }

# ---------------------------------------------------------------- checagens
foreach ($f in @($pyz, $vbs)) {
    if (-not (Test-Path $f)) {
        Erro "$(Split-Path -Leaf $f) nao encontrado nesta pasta."
        Erro "Baixe sysmon.pyz do release e deixe junto do sysmon.vbs."
        exit 1
    }
}
if (-not (Test-Path (Join-Path $pasta "config.json"))) {
    Erro "config.json nao encontrado."
    Erro "Copie config.example.json para config.json e preencha url + token,"
    Erro "ou use o hosts.json que o deploy.sh gerou."
    exit 1
}

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
    Erro "python nao esta no PATH. Instale o Python do python.org (marque"
    Erro "'Add python.exe to PATH' no instalador)."
    exit 1
}

# Teste rapido antes de registrar: falhar aqui e melhor que falhar no boot.
# Em modo --browser --nao-abrir para nao piscar janela na tela; o que se quer
# verificar e config e porta, que e onde as coisas costumam falhar.
Write-Host "==> Testando (6s)..." -ForegroundColor Cyan
$p = Start-Process $python.Source -ArgumentList "`"$pyz`"", "--browser", "--nao-abrir" `
     -WorkingDirectory $pasta -PassThru -WindowStyle Hidden
Start-Sleep 6
if ($p.HasExited) {
    Erro "O sysmon morreu durante o teste."
    Erro "Rode 'python sysmon.pyz' num terminal para ver o erro."
    Erro "Log da bandeja: $env:TEMP\traymon.log"
    exit 1
}
Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
Ok "OK"

# ------------------------------------------------------------------ atalhos
function Criar-Atalho($destino, $argumentos, $descricao) {
    $ws  = New-Object -ComObject WScript.Shell
    $lnk = $ws.CreateShortcut($destino)
    $lnk.TargetPath       = "wscript.exe"
    $lnk.Arguments        = $argumentos
    $lnk.WorkingDirectory = $pasta
    $lnk.Description      = $descricao
    $lnk.IconLocation     = "$($python.Source),0"
    $lnk.Save()
}

Write-Host "==> Atalhos" -ForegroundColor Cyan
try {
    # Sem --oculto: clicar no atalho e pedir a janela agora.
    Criar-Atalho (Join-Path ([Environment]::GetFolderPath("Desktop")) "sysmon.lnk") `
                 "`"$vbs`" --nao-oculto" "Monitor da frota Linux"
    Ok "atalho criado na area de trabalho"
} catch {
    Aviso "nao consegui criar o atalho da area de trabalho: $($_.Exception.Message)"
}

# ---------------------------------------------------------------- autostart
$inicializar = Join-Path ([Environment]::GetFolderPath("Startup")) "sysmon.lnk"

if ($Agendador) {
    Write-Host "==> Agendador de Tarefas" -ForegroundColor Cyan
    $admin = ([Security.Principal.WindowsPrincipal] `
              [Security.Principal.WindowsIdentity]::GetCurrent()
             ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $admin) {
        Erro "    -Agendador exige PowerShell como administrador."
        Erro "    Sem admin, rode sem esse parametro: o autostart vai pela"
        Erro "    pasta Inicializar, que funciona igual bem."
        exit 1
    }
    try {
        $acao = New-ScheduledTaskAction -Execute "wscript.exe" `
                -Argument "`"$vbs`"" -WorkingDirectory $pasta
        $gatilho = New-ScheduledTaskTrigger -AtLogOn
        $gatilho.Delay = "PT$($AtrasoSeg)S"
        $cfg = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
               -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0 -RestartCount 3 `
               -RestartInterval (New-TimeSpan -Minutes 1)
        Register-ScheduledTask -TaskName "sysmon" -Action $acao -Trigger $gatilho `
                               -Settings $cfg -Force -ErrorAction Stop `
                               -Description "Monitor da frota Linux" | Out-Null
        Ok "tarefa 'sysmon' registrada (inicia $AtrasoSeg s apos o login)"
        # Evita subir duas vezes se a pasta Inicializar tambem tiver o atalho.
        Remove-Item $inicializar -ErrorAction SilentlyContinue
    } catch {
        Erro "    falhou: $($_.Exception.Message)"
        Erro "    Rode sem -Agendador para usar a pasta Inicializar."
        exit 1
    }
} else {
    Write-Host "==> Autostart (pasta Inicializar, sem admin)" -ForegroundColor Cyan
    try {
        # Com --oculto: no logon a janela sobe minimizada na bandeja, em vez de
        # pular na frente do que voce estava fazendo.
        Criar-Atalho $inicializar "`"$vbs`"" "Monitor da frota Linux (inicio automatico)"
        Ok "registrado para iniciar no seu login"
    } catch {
        Erro "    nao consegui criar o atalho em $inicializar"
        Erro "    $($_.Exception.Message)"
        exit 1
    }
}

# Tarefas de versoes anteriores, para nao ficarem duas instancias na mesma porta.
foreach ($t in @("traymon")) {
    Unregister-ScheduledTask -TaskName $t -Confirm:$false -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "Pronto." -ForegroundColor Green
Write-Host "  Abrir agora   : duplo clique no atalho da area de trabalho"
Write-Host "  Dashboard     : http://127.0.0.1:9110/"
Write-Host "  Remover       : powershell -ExecutionPolicy Bypass -File desinstalar-autostart.ps1"
