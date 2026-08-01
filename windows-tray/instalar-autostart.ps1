# Registra o sysmon para iniciar junto com o Windows.
#
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1 -Agendador
#   powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1 -Inicializar
#
# Por padrao tenta o Agendador de Tarefas (como sempre foi) e, se nao houver
# permissao, cai na pasta Inicializar - que nao exige administrador. Seja qual
# for o caminho, o outro e removido: os dois ativos fariam duas instancias
# subirem no logon e uma morrer disputando a porta.
#
# Um processo so: sobe o dashboard e, se pystray/Pillow estiverem instalados,
# o icone de bandeja junto.
param(
    [switch]$Agendador,    # exige o Agendador; falha se nao houver admin
    [switch]$Inicializar,  # forca a pasta Inicializar, sem tentar o Agendador
    [int]$AtrasoSeg = 20   # espera antes de subir, para a rede estabilizar
)

# Sem "Stop" global: um passo opcional que falhe nao pode abortar os demais.
$ErrorActionPreference = "Continue"

$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path
$vbs   = Join-Path $pasta "sysmon.vbs"
$exe   = Join-Path $pasta "sysmon.exe"
$pyz   = Join-Path $pasta "sysmon.pyz"

# Lancador preferido: o sysmon.exe. Ele avisa em caixa de dialogo quando nao
# acha o Python, enquanto o wscript+vbs simplesmente nao abre e nao diz nada -
# a falha de partida mais chata de diagnosticar no Windows. O .vbs continua
# aqui como reserva, para pacote antigo que ainda nao tenha o executavel.
if (Test-Path $exe) {
    $execAlvo  = $exe
    $argsLogon = ""                        # sem argumento = modo logon
    $argsAgora = "/agora"                  # sobe ja, e visivel
} else {
    $execAlvo  = "wscript.exe"
    $argsLogon = "`"$vbs`""
    $argsAgora = "`"$vbs`" --nao-oculto"
}

function Ok($m)    { Write-Host "    $m" -ForegroundColor Green }
function Aviso($m) { Write-Host "    $m" -ForegroundColor Yellow }
function Erro($m)  { Write-Host $m -ForegroundColor Red }

# ---------------------------------------------------------------- checagens
if (-not (Test-Path $pyz)) {
    Erro "sysmon.pyz nao encontrado nesta pasta."
    Erro "Extraia o pacote inteiro do release, nao so este script."
    exit 1
}
if (-not (Test-Path $exe) -and -not (Test-Path $vbs)) {
    Erro "Nenhum lancador nesta pasta (sysmon.exe ou sysmon.vbs)."
    Erro "Extraia o pacote inteiro do release."
    exit 1
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

# A janela nao precisa de pacote nenhum: Tkinter vem com o Python. So o icone
# de bandeja precisa, e ele e opcional.
Write-Host "==> Componentes" -ForegroundColor Cyan
& $python.Source -c "import tkinter" 2>$null
if ($LASTEXITCODE -ne 0) {
    Erro "    este Python veio sem Tkinter - provavelmente o da Microsoft Store."
    Erro "    Instale o oficial de https://python.org"
    exit 1
}
Ok "Tkinter presente (a janela nao depende de mais nada)"

& $python.Source -c "import pystray, PIL" 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "    instalando o icone de bandeja (opcional)..." -ForegroundColor Cyan
    & $python.Source -m pip install --disable-pip-version-check --quiet pystray pillow
    & $python.Source -c "import pystray, PIL" 2>$null
    if ($LASTEXITCODE -ne 0) {
        Aviso "sem icone de bandeja; a janela funciona igual"
    } else { Ok "icone de bandeja instalado" }
} else { Ok "icone de bandeja disponivel" }

# Teste rapido antes de registrar: falhar aqui e melhor que falhar no boot.
# Em modo --oculto sobe minimizado na bandeja, sem piscar janela na tela; o que
# se quer verificar e que a config carrega e o processo nao morre no arranque.
Write-Host "==> Testando (6s)..." -ForegroundColor Cyan
$p = Start-Process $python.Source -ArgumentList "`"$pyz`"", "--oculto" `
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
function Criar-Atalho($destino, $alvo, $argumentos, $descricao) {
    $ws  = New-Object -ComObject WScript.Shell
    $lnk = $ws.CreateShortcut($destino)
    $lnk.TargetPath       = $alvo
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
                 $execAlvo $argsAgora "Monitor da frota Linux"
    Ok "atalho criado na area de trabalho"
} catch {
    Aviso "nao consegui criar o atalho da area de trabalho: $($_.Exception.Message)"
}

# ---------------------------------------------------------------- autostart
# Nome diferente do parametro [switch]$Inicializar de proposito: PowerShell
# nao diferencia maiusculas em nome de variavel, entao "$atalhoInicio = ..."
# tentaria atribuir uma string ao switch e estouraria.
$atalhoInicio = Join-Path ([Environment]::GetFolderPath("Startup")) "sysmon.lnk"

# Duas formas de iniciar no logon, e SO UMA pode ficar ativa: se as duas
# dispararem, a segunda instancia briga pela porta 9110 e morre em silencio.
#
#   Agendador de Tarefas  - como sempre foi; reinicia o processo se cair,
#                           mas registrar tarefa na raiz exige administrador
#   pasta Inicializar     - nao exige nada, sem reinicio automatico
#
# O padrao tenta o Agendador primeiro e cai na pasta Inicializar se nao houver
# permissao. Os parametros forcam um dos dois.
Write-Host "==> Autostart" -ForegroundColor Cyan

function Remover-Tarefa($nome) {
    if (Get-ScheduledTask -TaskName $nome -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $nome -Confirm:$false -ErrorAction SilentlyContinue
        return $?
    }
    return $true
}

function Registrar-Tarefa {
    # -Argument vazio nao e aceito; com o .exe simplesmente nao passamos.
    if ($argsLogon) {
        $acao = New-ScheduledTaskAction -Execute $execAlvo `
                -Argument $argsLogon -WorkingDirectory $pasta
    } else {
        $acao = New-ScheduledTaskAction -Execute $execAlvo -WorkingDirectory $pasta
    }
    $gatilho = New-ScheduledTaskTrigger -AtLogOn
    $gatilho.Delay = "PT$($AtrasoSeg)S"
    $cfg = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
           -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0 -RestartCount 3 `
           -RestartInterval (New-TimeSpan -Minutes 1)
    Register-ScheduledTask -TaskName "sysmon" -Action $acao -Trigger $gatilho `
                           -Settings $cfg -Force -ErrorAction Stop `
                           -Description "Monitor da frota Linux" | Out-Null
}

$metodo = $null

if (-not $Inicializar) {
    try {
        Registrar-Tarefa
        $metodo = "agendador"
        Ok "tarefa 'sysmon' registrada (inicia $AtrasoSeg s apos o login, com reinicio automatico)"
    } catch {
        if ($Agendador) {
            Erro "    nao consegui registrar a tarefa: $($_.Exception.Message)"
            Erro "    Registrar tarefa na raiz da biblioteca exige PowerShell como"
            Erro "    administrador. Rode elevado, ou use -Inicializar."
            exit 1
        }
        Aviso "sem permissao para o Agendador de Tarefas (precisa de admin)"
        Aviso "usando a pasta Inicializar, que nao exige nada"
    }
}

if (-not $metodo) {
    try {
        # Com --oculto (padrao do vbs): no logon sobe minimizado na bandeja.
        Criar-Atalho $atalhoInicio $execAlvo $argsLogon `
                     "Monitor da frota Linux (inicio automatico)"
        $metodo = "inicializar"
        Ok "registrado na pasta Inicializar"
    } catch {
        Erro "    nao consegui criar o atalho em $atalhoInicio"
        Erro "    $($_.Exception.Message)"
        exit 1
    }
}

# --------------------------------------------------- garantir UM so caminho
# O que nao foi escolhido tem que sair, inclusive resquicio de instalacao
# anterior que usava o outro metodo.
if ($metodo -eq "agendador") {
    if (Test-Path $atalhoInicio) {
        Remove-Item $atalhoInicio -Force -ErrorAction SilentlyContinue
        Ok "atalho antigo da pasta Inicializar removido"
    }
} else {
    if (Get-ScheduledTask -TaskName "sysmon" -ErrorAction SilentlyContinue) {
        if (Remover-Tarefa "sysmon") {
            Ok "tarefa 'sysmon' antiga removida"
        } else {
            Aviso "ha uma tarefa 'sysmon' no Agendador que eu nao consigo remover"
            Aviso "sem admin. Ela e a pasta Inicializar vao subir DUAS instancias."
            Aviso "Remova elevado:  schtasks /delete /tn sysmon /f"
        }
    }
}

# Tarefa das versoes ate a 2.1, que chamava outro script.
Remover-Tarefa "traymon" | Out-Null

# --------------------------------------------------------------- conferencia
$temTarefa = [bool](Get-ScheduledTask -TaskName "sysmon" -ErrorAction SilentlyContinue)
$temAtalho = Test-Path $atalhoInicio
if ($temTarefa -and $temAtalho) {
    Aviso ""
    Aviso "ATENCAO: os dois metodos estao ativos. Duas instancias vao tentar"
    Aviso "subir no logon e uma vai morrer disputando a porta. Remova um:"
    Aviso "  schtasks /delete /tn sysmon /f        (remove a tarefa)"
    Aviso "  del `"$atalhoInicio`"                  (remove o atalho)"
} elseif (-not $temTarefa -and -not $temAtalho) {
    Aviso "nenhum autostart ficou ativo - reveja as mensagens acima"
}

Write-Host ""
Write-Host "Pronto." -ForegroundColor Green
Write-Host "  Abrir agora   : duplo clique no atalho da area de trabalho"
Write-Host "  Janela        : sobe minimizada na bandeja; clique no icone para abrir"
Write-Host "  Remover       : powershell -ExecutionPolicy Bypass -File desinstalar-autostart.ps1"
