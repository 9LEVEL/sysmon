@echo off
REM Inicia o sysmon COM console: tudo que der errado aparece na tela.
REM Duplo clique aqui. Para o dia a dia sem janela preta, use o atalho da
REM area de trabalho.
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

REM O app de verdade e janela + bandeja. Sem estes pacotes o sysmon ainda
REM funciona, mas cai no browser - que nao e o que se quer aqui.
python -c "import webview, pystray, PIL" >nul 2>&1
if errorlevel 1 (
    echo.
    echo   Faltam os componentes da janela e da bandeja.
    echo   Sem eles o sysmon abre no navegador em vez de virar um app.
    echo.
    set /p RESP="   Instalar agora? [S/n] "
    if /i "%RESP%"=="n" goto :rodar
    echo.
    python -m pip install --disable-pip-version-check pywebview pystray pillow
    echo.
    python -c "import webview, pystray, PIL" >nul 2>&1
    if errorlevel 1 (
        echo   A instalacao nao completou. O sysmon vai abrir no navegador.
        echo   Tente manualmente:  python -m pip install pywebview pystray pillow
        echo.
        pause
    )
)

:rodar
python "sysmon.pyz" %*

echo.
echo   O sysmon encerrou. Se foi por erro, a mensagem esta acima.
pause
