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
# A versao mora num arquivo so. Antes ela era literal em oito lugares e um
# script conferia se concordavam; com o cliente em Go ela entra por -ldflags,
# entao guardar o numero e mais simples que conferir copias.
VERSAO="${1:-$(tr -d " \n" < "$AQUI/VERSAO")}"
DIST="$AQUI/dist"

verde() { printf '\033[32m%s\033[0m\n' "$*"; }
azul()  { printf '\033[36m%s\033[0m\n' "$*"; }

[[ -n "$VERSAO" ]] || { echo "nao consegui descobrir a versao"; exit 1; }

command -v go >/dev/null || { echo "Go nao encontrado."; exit 1; }

rm -rf "$DIST"
mkdir -p "$DIST"
azul "==> sysmon $VERSAO"

# ------------------------------------------------------------------ agente
# So amd64. O arm64 saiu na v5.1.0 por falta de demanda - nenhum pedido, e
# cada alvo a mais e um binario a conferir e publicar em todo release. Volta
# na hora em que alguem pedir: e uma linha nesta lista.
for ARCO in amd64; do
    NOME="sysmon-agent-$VERSAO-linux-$ARCO"
    PASTA="$DIST/$NOME"
    mkdir -p "$PASTA"

    ( cd "$AQUI" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCO" \
      go build -trimpath -ldflags "-s -w -X main.versao=$VERSAO" \
        -o "$PASTA/sysmon-agent" ./cmd/sysmon-agent )

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

    # Uma copia com nome SEM versao.
    #
    # E o que faz o passo 1 do README ser copiavel: a URL
    # .../releases/latest/download/<nome> exige um nome fixo, e com a versao
    # embutida quem instala precisa saber qual e antes de baixar - o que
    # obriga a abrir o navegador no meio de um passo de terminal.
    cp "$DIST/$NOME.tar.gz" "$DIST/sysmon-agent-linux-$ARCO.tar.gz"
    verde "    $NOME.tar.gz"
done

# ------------------------------------------------------------------ cliente
# Um binario por plataforma, sem runtime nenhum. Ate a v4 aqui se montava um
# .pyz que exigia Python instalado, mais um lancador por sistema so para
# conseguir trocar o arquivo na atualizacao. Sumiu tudo.
#
# Os nomes SOLTOS (sysmon-<so>-<arco>) sao o que o auto-update procura no
# release: mudar um deles quebra a atualizacao de quem ja tem a ferramenta.
azul "==> cliente"
# So amd64. A janela no Linux exige CGO, e cross-compilar isso para arm64
# pediria uma toolchain cruzada inteira - custo alto para um desktop Linux em
# ARM, que e raro. O agente tambem saiu do arm64 na v5.1.0; ver a nota la em
# cima.
for ALVO in "windows/amd64" "linux/amd64"; do
    SO="${ALVO%%/*}"; ARCO="${ALVO##*/}"
    NOME="sysmon-$SO-$ARCO"
    LD="-s -w -X main.versao=$VERSAO"
    if [ "$SO" = "windows" ]; then
        NOME="$NOME.exe"
        LD="$LD -H windowsgui"
    fi

    # CGO so e preciso para a janela no Linux (X11/Wayland), e so da para
    # usa-lo na arquitetura desta maquina. O Windows cross-compila daqui sem
    # nada disso, que e o que mantem o CI simples.
    CGO=0
    if [ "$SO" = "linux" ] && [ "$ARCO" = "$(go env GOHOSTARCH)" ]; then
        CGO=1
    fi

    ( cd "$AQUI" && CGO_ENABLED=$CGO GOOS="$SO" GOARCH="$ARCO" \
        go build -trimpath -ldflags "$LD" -o "$DIST/$NOME" ./cmd/sysmon )
    verde "    $NOME  ($(du -h "$DIST/$NOME" | cut -f1))"
done

# Compilado como console, a janela abriria uma janela preta junto - e isso
# nao aparece em teste nenhum, so no duplo clique de quem baixou.
python3 - "$DIST/sysmon-windows-amd64.exe" <<'PYEOF'
import struct, sys
d = open(sys.argv[1], "rb").read()
if d[:2] != b"MZ":
    sys.exit("ERRO: o binario do Windows nao e um executavel")
pe = struct.unpack_from("<I", d, 0x3C)[0]
sub, = struct.unpack_from("<H", d, pe + 24 + 68)
if sub != 2:
    sys.exit(f"ERRO: subsistema {sub}; esperado 2 (GUI)")
PYEOF

# ------------------------------------------------------ pacotes por sistema
# Um pacote por sistema, com o nome dizendo para quem e.

# --- Windows: o executavel, o exemplo de config e os scripts de autostart
NOME="sysmon-windows-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA"
install -m 755 "$DIST/sysmon-windows-amd64.exe" "$PASTA/sysmon.exe"
for f in config.example.json instalar-autostart.ps1 desinstalar-autostart.ps1 \
         limpar.ps1; do
    install -m 644 "$AQUI/windows-tray/$f" "$PASTA/"
done
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon para Windows $VERSAO

Um executavel. Sem Python, sem instalar nada.

1) Extraia numa pasta sua (nao dentro de Downloads) e de duplo clique em
   sysmon.exe.

   A janela abre na TELA DE CONFIGURACAO: preencha apelido, url e token de
   cada host Linux, clique em Testar e salve. A url e o token sao os que o
   install.sh imprimiu em cada host monitorado.

   Fechar a janela nao encerra: o programa fica no icone da bandeja, que
   muda de cor pelo pior host. Sair e pelo menu do icone.

2) Para subir junto com o Windows:

    powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1

Tabela no terminal, para olhar rapido ou para script:

    sysmon.exe term
    sysmon.exe term --once      (imprime uma vez; codigo 0 ok, 1 alerta,
                                 2 host fora do ar)
    sysmon.exe term --json

Atualizar: o botao no cabecalho procura, baixa conferindo o SHA256 e
reinicia ja na versao nova. Ele tambem verifica sozinho a cada 6h.

O config.json guarda os tokens de TODOS os hosts em texto claro. Proteja:

    icacls config.json /inheritance:r /grant:r "%USERNAME%:R"

Documentacao: https://github.com/9LEVEL/sysmon
EOF

if command -v zip >/dev/null; then
    ( cd "$DIST" && zip -qr "$NOME.zip" "$NOME" )
else
    ( cd "$DIST" && python3 -c "
import shutil, sys
shutil.make_archive(sys.argv[1], 'zip', '.', sys.argv[1])
" "$NOME" )
fi
rm -rf "$PASTA"
verde "    $NOME.zip"

# --- Linux: o binario e o exemplo de config
NOME="sysmon-linux-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA"
install -m 755 "$DIST/sysmon-linux-amd64" "$PASTA/sysmon"
install -m 644 "$AQUI/windows-tray/config.example.json" "$PASTA/"
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon para Linux $VERSAO

Um binario. Sem Python, sem pip, sem instalar nada.

    cp config.example.json config.json    # preencha url e token de cada host
    chmod 600 config.json
    ./sysmon                              # janela nativa
    ./sysmon term                         # tabela no terminal
    ./sysmon term --once                  # uma vez so (cron, script)

Nao ha icone de bandeja no Linux: o mecanismo varia entre ambientes de
desktop e uma implementacao pela metade seria pior que nenhuma. Aqui fechar
a janela encerra o programa.

Para monitorar ESTA maquina tambem, instale o agente nela e acrescente
http://127.0.0.1:9109/metrics ao config.

Documentacao: https://github.com/9LEVEL/sysmon
EOF

( cd "$DIST" && tar czf "$NOME.tar.gz" "$NOME" )
rm -rf "$PASTA"
verde "    $NOME.tar.gz"

# ---------------------------------------------------------------- somas
# Os binarios PRECISAM estar aqui: o auto-update confere o download contra
# esta lista antes de trocar o executavel. Sem a entrada, ele recusa - e o
# usuario fica preso na versao que tem, sem saber por que.
# nullglob para que a ausencia do zip (sem o utilitario instalado) nao passe
# o padrao literal para o sha256sum e derrube o script pelo set -e.
( shopt -s nullglob; cd "$DIST" && sha256sum ./*.tar.gz ./*.zip \
    ./sysmon-windows-amd64.exe ./sysmon-linux-amd64 \
    | sed 's|\./||' > SHA256SUMS )
for n in sysmon-windows-amd64.exe sysmon-linux-amd64; do
    grep -q " $n\$" "$DIST/SHA256SUMS" || {
        echo "ERRO: SHA256SUMS sem $n - o auto-update ficaria quebrado." >&2
        exit 1
    }
done
verde "    SHA256SUMS"

echo
azul "==> dist/"
ls -lh "$DIST"
