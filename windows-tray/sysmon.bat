@echo off
REM Inicia o sysmon COM console: tudo que der errado aparece na tela.
REM Duplo clique aqui. Para o dia a dia sem janela preta, use o atalho.
setlocal
cd /d "%~dp0"

REM Aplica atualizacao baixada, se houver (mesmo passo do sysmon.vbs).
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

REM A janela nao precisa de nada: Tkinter vem com o Python. Estes dois sao so
REM para o icone de bandeja, e sao opcionais.
python -c "import tkinter" >nul 2>&1
if errorlevel 1 (
    echo.
    echo   Este Python veio sem Tkinter - provavelmente e o da Microsoft Store.
    echo   Instale o oficial de https://python.org
    echo.
    pause
    exit /b 1
)

python -c "import pystray, PIL" >nul 2>&1
if errorlevel 1 (
    echo   Instalando o icone de bandeja (opcional, uma vez so)...
    python -m pip install --disable-pip-version-check --quiet pystray pillow
    echo.
)

python "sysmon.pyz" %*

echo.
echo   O sysmon encerrou. Se foi por erro, a mensagem esta acima.
pause
