# Limpa TODAS as tentativas anteriores de instalacao do sysmon nesta maquina.
#
#   powershell -ExecutionPolicy Bypass -File limpar.ps1
#
# Encerra processos, remove tarefas agendadas, atalhos de inicializacao e de
# area de trabalho, e restos de atualizacao. NAO apaga o seu config.json - ele
# tem os tokens dos hosts, e voce vai querer reaproveitar.
$ErrorActionPreference = "Continue"

function Ok($m)    { Write-Host "  [removido] $m" -ForegroundColor Green }
function Nada($m)  { Write-Host "  [nao havia] $m" -ForegroundColor DarkGray }
function Aviso($m) { Write-Host "  [atencao]  $m" -ForegroundColor Yellow }

Write-Host ""
Write-Host "=== Limpando instalacoes anteriores do sysmon ===" -ForegroundColor Cyan
Write-Host ""

# ------------------------------------------------------------- 1. processos
$procs = Get-CimInstance Win32_Process -Filter "Name = 'sysmon.exe'" -ErrorAction SilentlyContinue |
         Where-Object { $true -or
                        $_.CommandLine -like "*traymon.py*" -or
                        $_.CommandLine -like "*sysmon.py*" }
if ($procs) {
    foreach ($p in $procs) {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
        Ok "processo em execucao (PID $($p.ProcessId))"
    }
} else { Nada "processo em execucao" }

# ------------------------------------------------------ 2. tarefas agendadas
foreach ($t in @("sysmon", "traymon")) {
    if (Get-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $t -Confirm:$false -ErrorAction SilentlyContinue
        if (Get-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue) {
            Aviso "tarefa '$t' exige admin para remover:  schtasks /delete /tn $t /f"
        } else { Ok "tarefa agendada '$t'" }
    } else { Nada "tarefa agendada '$t'" }
}

# ------------------------------------------------------------- 3. atalhos
$alvos = @(
    (Join-Path ([Environment]::GetFolderPath("Startup")) "sysmon.lnk"),
    (Join-Path ([Environment]::GetFolderPath("Startup")) "traymon.lnk"),
    (Join-Path ([Environment]::GetFolderPath("Startup")) "traymon.vbs"),
    (Join-Path ([Environment]::GetFolderPath("Desktop")) "sysmon.lnk")
)
foreach ($a in $alvos) {
    if (Test-Path $a) { Remove-Item $a -Force; Ok $a } else { Nada (Split-Path -Leaf $a) }
}

# --------------------------------------------- 4. variaveis de ambiente
foreach ($v in @("SYSMON_URL", "SYSMON_TOKEN", "SYSMON_NOME", "SYSMON_CONFIG")) {
    if ([Environment]::GetEnvironmentVariable($v, "User")) {
        [Environment]::SetEnvironmentVariable($v, $null, "User")
        Ok "variavel de ambiente $v (ela sobrepunha o config.json)"
    } else { Nada "variavel $v" }
}

# ------------------------------------------------- 5. restos de atualizacao
$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path
# Sobra da troca de binario: ver internal/atualizar.
foreach ($f in @("sysmon.exe.old")) {
    $p = Join-Path $pasta $f
    if (Test-Path $p) { Remove-Item $p -Force; Ok $f } else { Nada $f }
}

# ------------------------------------------------------------- 6. relatorio
Write-Host ""
$cfg = Join-Path $pasta "config.json"
if (Test-Path $cfg) {
    $n = (Get-Content $cfg -Raw | ConvertFrom-Json).hosts.Count
    Write-Host "Seu config.json foi MANTIDO ($n host(s)):" -ForegroundColor Green
    Write-Host "  $cfg"
} else {
    Write-Host "Nao ha config.json nesta pasta - o sysmon vai abrir a tela de" -ForegroundColor Yellow
    Write-Host "configuracao no primeiro arranque para voce preencher." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Limpo. Para comecar do zero:" -ForegroundColor Cyan
Write-Host "  1. duplo clique em sysmon.bat        (abre com console, mostra erros)"
Write-Host "  2. instalar-autostart.ps1            (so quando ja estiver funcionando)"
Write-Host ""
