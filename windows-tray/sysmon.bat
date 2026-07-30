@echo off
REM Inicia o sysmon COM console: tudo que der errado aparece na tela.
REM E a forma mais simples de comecar e de diagnosticar - duplo clique aqui.
REM
REM Para o uso do dia a dia, sem janela preta, use o atalho da area de
REM trabalho ou o sysmon.vbs.
cd /d "%~dp0"

REM Aplica atualizacao baixada, se houver (mesmo passo que o sysmon.vbs faz).
if exist "sysmon-novo.pyz" (
    echo Aplicando atualizacao...
    move /y "sysmon-novo.pyz" "sysmon.pyz" >nul
)

where python >nul 2>&1
if errorlevel 1 (
    echo.
    echo   Python nao encontrado no PATH.
    echo   Instale de https://python.org e marque "Add python.exe to PATH".
    echo.
    pause
    exit /b 1
)

python "sysmon.pyz" %*

echo.
echo   O sysmon encerrou. Se foi por erro, a mensagem esta acima.
pause
