#!/usr/bin/env bash
# Gera os pacotes de distribuicao em dist/.
#
#   ./empacotar.sh [versao]
#
# Sao dois tipos de pacote, porque as duas metades tem publicos diferentes:
#
#   sysmon-agent-<versao>-linux-<arco>.tar.gz  -> vai para CADA host monitorado
#   sysmon-clientes-<versao>.tar.gz/.zip       -> vai para a maquina que OLHA
#
# O pacote do agente e autocontido: binario estatico, units e instalador.
# Nao precisa de Go, Python nem rede no host de destino.
set -euo pipefail

AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSAO="${1:-$(sed -n 's/^var versao = "\(.*\)"$/\1/p' "$AQUI/linux-agent/main.go")}"
DIST="$AQUI/dist"

verde() { printf '\033[32m%s\033[0m\n' "$*"; }
azul()  { printf '\033[36m%s\033[0m\n' "$*"; }

[[ -n "$VERSAO" ]] || { echo "nao consegui descobrir a versao"; exit 1; }

command -v go >/dev/null || { echo "Go nao encontrado."; exit 1; }

rm -rf "$DIST"
mkdir -p "$DIST"
azul "==> sysmon $VERSAO"

# ------------------------------------------------------------------ agente
for ARCO in amd64 arm64; do
    NOME="sysmon-agent-$VERSAO-linux-$ARCO"
    PASTA="$DIST/$NOME"
    mkdir -p "$PASTA"

    ( cd "$AQUI/linux-agent" && \
      CGO_ENABLED=0 GOOS=linux GOARCH="$ARCO" \
      go build -trimpath -ldflags "-s -w -X main.versao=$VERSAO" \
        -o "$PASTA/sysmon-agent" . )

    install -m 755 "$AQUI/linux-agent/install.sh"   "$PASTA/"
    install -m 755 "$AQUI/linux-agent/uninstall.sh" "$PASTA/"
    install -m 644 "$AQUI/linux-agent/sysmon-agent.service"    "$PASTA/"
    install -m 644 "$AQUI/linux-agent/sysmon-thinpool.service" "$PASTA/"
    install -m 644 "$AQUI/linux-agent/sysmon-thinpool.timer"   "$PASTA/"
    install -m 644 "$AQUI/LICENSE" "$PASTA/"

    cat > "$PASTA/LEIAME.txt" <<EOF
sysmon-agent $VERSAO (linux/$ARCO)

Este pacote vai para CADA host que voce quer monitorar. E autocontido: nao
precisa de Go, Python nem acesso a internet no host de destino.

Instalacao:

    sudo ./install.sh <IP_DE_BIND> [porta]

O IP de bind deve ser o da LAN ou o do tunel (WireGuard/Tailscale) - nunca
0.0.0.0, porque o transporte e HTTP puro e o token viaja em texto claro. O
instalador recusa 0.0.0.0 de proposito.

Ele copia o binario para /opt/sysmon, gera o token em /etc/sysmon/token.env,
sobe o servico, liga o timer do thin pool se houver LVM thin, e testa. No fim
imprime a URL e o token para colar no cliente.

Depois, feche a porta para o resto da rede:

    iptables -A INPUT -p tcp --dport 9109 -s <IP_DO_CLIENTE> -j ACCEPT
    iptables -A INPUT -p tcp --dport 9109 -j DROP

Conferir:

    systemctl status sysmon-agent
    journalctl -u sysmon-agent -n 30
    ss -lntp | grep 9109

ATENCAO: o agente escuta SO no IP que voce passou ao install.sh, nao em
localhost. Teste sempre com o mesmo IP:

    curl -s http://<IP_DE_BIND>:9109/health      # funciona
    curl -s http://localhost:9109/health         # conexao recusada, e esperado

"Conexao recusada" quase sempre significa uma destas tres coisas: o servico
nao subiu, voce testou em um IP diferente do de bind, ou a porta e outra.
O `ss -lntp | grep 9109` mostra em qual IP ele esta de fato escutando.

Remover:

    sudo ./uninstall.sh

Documentacao completa: https://github.com/9LEVEL/sysmon
EOF

    ( cd "$DIST" && tar czf "$NOME.tar.gz" "$NOME" && rm -rf "$NOME" )
    verde "    $NOME.tar.gz"
done

# ---------------------------------------------------------------- clientes
NOME="sysmon-clientes-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA/tools" "$PASTA/windows-tray"

install -m 644 "$AQUI/tools/sysmon_nucleo.py" "$PASTA/tools/"
install -m 755 "$AQUI/tools/sysmon-dash.py"   "$PASTA/tools/"
install -m 755 "$AQUI/tools/sysmon-cli.py"    "$PASTA/tools/"
for f in traymon.py traymon.vbs config.example.json requirements.txt \
         instalar-autostart.ps1 desinstalar-autostart.ps1; do
    install -m 644 "$AQUI/windows-tray/$f" "$PASTA/windows-tray/"
done
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon - clientes $VERSAO

Este pacote vai para a maquina de onde voce OLHA os hosts. Python 3.9 ou mais
novo; o dashboard do terminal nao precisa de mais nada.

Preencha primeiro o config: copie windows-tray/config.example.json para
config.json e liste seus hosts com url e token (o install.sh de cada host
imprimiu os dois). Se voce usou o deploy.sh, ele ja gerou esse arquivo pronto.

Dashboard no terminal (Linux, ou WSL):

    python3 tools/sysmon-dash.py --config config.json
    python3 tools/sysmon-dash.py --config config.json --once    # uma vez, para script
    python3 tools/sysmon-dash.py --config config.json --host pve

Bandeja do Windows:

    cd windows-tray
    python -m pip install -r requirements.txt
    copy ..\\config.json config.json
    python traymon.py          # teste COM console primeiro, para ver erros

Funcionando, registre o autostart:

    powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1

O config.json guarda os tokens de TODOS os hosts em texto claro. Proteja:

    Windows : icacls config.json /inheritance:r /grant:r "%USERNAME%:R"
    Linux   : chmod 600 config.json

O traymon.py importa tools/sysmon_nucleo.py - por isso mantenha as duas pastas
lado a lado, ou copie sysmon_nucleo.py para dentro de windows-tray.

Documentacao completa: https://github.com/9LEVEL/sysmon
EOF

( cd "$DIST" && tar czf "$NOME.tar.gz" "$NOME" )
verde "    $NOME.tar.gz"

# Zip tambem: o Windows abre sem instalar nada, e tar.gz nao e obvio la.
# Cai no Python quando o utilitario zip nao existe na maquina de build.
if command -v zip >/dev/null; then
    ( cd "$DIST" && zip -qr "$NOME.zip" "$NOME" )
elif command -v python3 >/dev/null; then
    ( cd "$DIST" && python3 -c "
import shutil, sys
shutil.make_archive(sys.argv[1], 'zip', '.', sys.argv[1])
" "$NOME" )
fi
[[ -f "$DIST/$NOME.zip" ]] && verde "    $NOME.zip"
rm -rf "$PASTA"

# ---------------------------------------------------------------- somas
# nullglob para que a ausencia do zip (sem o utilitario instalado) nao passe
# o padrao literal para o sha256sum e derrube o script pelo set -e.
( shopt -s nullglob; cd "$DIST" && sha256sum ./*.tar.gz ./*.zip | sed 's|\./||' > SHA256SUMS )
verde "    SHA256SUMS"

echo
azul "==> dist/"
ls -lh "$DIST"
