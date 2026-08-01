@echo off
REM Inicia o sysmon COM console: tudo que der errado aparece na tela.
REM Duplo clique aqui. Para o dia a dia sem janela preta, use o atalho.
setlocal
cd /d "%~dp0"

REM Aplica atualizacao baixada, se houver (mesmo passo do sysmon.vbs).
REM Insiste por alguns segundos: quando quem pediu foi o botao da interface,
REM o sysmon antigo ainda pode estar segurando o arquivo neste instante.
if exist "sysmon-novo.pyz" (
    echo Aplicando atualizacao...
    for /l %%t in (1,1,20) do (
        if exist "sysmon-novo.pyz" (
            move /y "sysmon-novo.pyz" "sysmon.pyz" >nul 2>&1
            if exist "sysmon-novo.pyz" ping -n 1 -w 300 127.0.0.1 >nul
        )
    )
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

REM Diz o que vai rodar ANTES de rodar. Sem isto, quem abre a versao nova com
REM a antiga ainda na bandeja ve a janela antiga na tela e conclui que a nova
REM nao tem as novidades - foi o que aconteceu de verdade num teste.
for /f "delims=" %%v in ('python "sysmon.pyz" --version 2^>^&1') do set VERSAO=%%v
echo   sysmon %VERSAO%
echo   pasta: %CD%
echo.

python "sysmon.pyz" %*
set CODIGO=%ERRORLEVEL%

echo.
if "%CODIGO%"=="0" (
    echo   O sysmon encerrou normalmente.
) else (
    echo   O sysmon encerrou com codigo %CODIGO%. A mensagem esta acima.
    echo.
    echo   Para um relatorio completo do que esta instalado, rode:
    echo       diagnostico.bat
)
echo.
pause
