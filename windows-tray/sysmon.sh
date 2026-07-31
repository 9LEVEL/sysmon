#!/bin/sh
# Sobe o sysmon aplicando a atualizacao baixada, se houver.
#
#   ./sysmon.sh [argumentos do sysmon]
#
# Ate a v4.1.0 este script nao existia, e o pacote de Linux vinha so com o
# .pyz. O resultado era que a atualizacao automatica baixava, conferia o
# SHA256... e parava ali: ninguem promovia o sysmon-novo.pyz, e o arquivo
# ficava ao lado do antigo para sempre. Atualizar virava baixar do GitHub e
# descompactar na mao, que e exatamente o que o auto-update existe para evitar.
#
# No Unix da para substituir um arquivo aberto - o processo em curso segue
# lendo o inode antigo -, entao a troca aqui e sempre segura.
set -eu

cd "$(dirname "$0")"

if [ -f sysmon-novo.pyz ]; then
    mv -f sysmon-novo.pyz sysmon.pyz
    chmod +x sysmon.pyz 2>/dev/null || true
fi

# python3 do PATH; o .pyz nao depende de nada alem da stdlib.
exec python3 sysmon.pyz "$@"
