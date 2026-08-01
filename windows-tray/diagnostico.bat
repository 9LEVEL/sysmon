@echo off
REM Relatorio do estado desta instalacao, para colar numa conversa de suporte.
REM
REM Existe porque diagnosticar "o botao nao aparece" as cegas custou uma tarde:
REM nao havia como ver, de fora, que a janela na tela era de outra instancia,
REM mais antiga, que ja estava rodando. Cada linha do relatorio responde uma
REM pergunta que ja custou tempo.
setlocal
cd /d "%~dp0"

where python >nul 2>&1
if errorlevel 1 (
    echo.
    echo   Python nao encontrado no PATH.
    echo   Instale de https://python.org e marque "Add python.exe to PATH".
    echo.
    pause
    exit /b 1
)

echo.
python "sysmon.pyz" --diagnostico
echo.
echo   Copie o texto acima (clique direito na barra do console ^> Marcar).
echo.
pause
