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
    install -m 755 "$AQUI/linux-agent/sysmon-smart.sh"         "$PASTA/"
    install -m 644 "$AQUI/linux-agent/sysmon-smart.service"    "$PASTA/"
    install -m 644 "$AQUI/linux-agent/sysmon-smart.timer"      "$PASTA/"
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

# ------------------------------------------------------- cliente: arquivo unico
# zipapp da stdlib: um .pyz e um zip com __main__.py que o Python executa
# direto. Vira UM arquivo para copiar e atualizar, em vez de uma arvore de
# scripts soltos - que era a principal reclamacao de manutencao.
PALCO="$DIST/.palco"
mkdir -p "$PALCO/web"
for m in sysmon_nucleo sysmon_web sysmon_dash sysmon_local sysmon_tray; do
    install -m 644 "$AQUI/tools/$m.py" "$PALCO/"
done
install -m 644 "$AQUI/sysmon.py" "$PALCO/"
for w in "$AQUI"/tools/web/*.html "$AQUI"/tools/web/*.css "$AQUI"/tools/web/*.js \
         "$AQUI"/tools/web/__init__.py; do
    install -m 644 "$w" "$PALCO/web/"
done
cat > "$PALCO/__main__.py" <<'EOF'
import sysmon, sys
sys.exit(sysmon.main())
EOF

python3 -m zipapp "$PALCO" -o "$DIST/sysmon.pyz" -p "/usr/bin/env python3" -c
rm -rf "$PALCO"
chmod 755 "$DIST/sysmon.pyz"
verde "    sysmon.pyz  ($(du -h "$DIST/sysmon.pyz" | cut -f1))"

# ---------------------------------------------------------------- clientes
NOME="sysmon-clientes-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA"

# O pacote de clientes agora e so o .pyz + o que ajuda a instalar.
install -m 755 "$DIST/sysmon.pyz" "$PASTA/"
for f in config.example.json requirements.txt sysmon.vbs \
         instalar-autostart.ps1 desinstalar-autostart.ps1; do
    install -m 644 "$AQUI/windows-tray/$f" "$PASTA/"
done
rmdir "$PASTA/tools" "$PASTA/windows-tray" 2>/dev/null || true
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon - clientes $VERSAO

Vai para a maquina de onde voce OLHA os hosts. Sao dois arquivos: o sysmon.pyz
e o seu config.json. Precisa de Python 3.9 ou mais novo, e nada alem disso.

1) Config: copie config.example.json para config.json e liste seus hosts com
   url e token (o install.sh de cada host imprimiu os dois). Se voce usou o
   deploy.sh, ele ja gerou esse arquivo pronto.

2) Rode:

    python sysmon.pyz

Isso sobe o dashboard web e abre o browser. No Windows, se pystray e Pillow
estiverem instalados, o icone de bandeja sobe junto no mesmo processo.

Outros modos:

    python sysmon.pyz web           # so o dashboard
    python sysmon.pyz term          # tabela no terminal
    python sysmon.pyz term --once   # imprime uma vez e sai (script/cron)
    python sysmon.pyz tray          # so a bandeja
    python sysmon.pyz local         # sensores DESTA maquina, sem rede

Bandeja no Windows (opcional):

    python -m pip install -r requirements.txt
    powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1

Os tokens ficam no servidor local: o browser recebe so telemetria. Por padrao
escuta apenas em 127.0.0.1, porque a pagina nao tem senha.

O config.json manda. Nada do ambiente sobrescreve valor presente nele; o
ambiente so preenche o que faltar:

    SYSMON_CONFIG          -> qual arquivo carregar
    SYSMON_TOKEN_<NOME>    -> so se aquele host nao tiver token no arquivo
    SYSMON_URL/_TOKEN      -> so se o arquivo nao definir host nenhum

O config.json guarda os tokens de TODOS os hosts em texto claro. Proteja:

    Windows : icacls config.json /inheritance:r /grant:r "%USERNAME%:R"
    Linux   : chmod 600 config.json

Atualizar: substitua o sysmon.pyz. So isso - o config.json fica.

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
