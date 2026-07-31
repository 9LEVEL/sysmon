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
mkdir -p "$PALCO"
for m in sysmon_nucleo sysmon_dash sysmon_local sysmon_tray sysmon_update sysmon_win; do
    install -m 644 "$AQUI/tools/$m.py" "$PALCO/"
done
install -m 644 "$AQUI/sysmon.py" "$PALCO/"
cat > "$PALCO/__main__.py" <<'EOF'
import sysmon, sys
sys.exit(sysmon.main())
EOF

python3 -m zipapp "$PALCO" -o "$DIST/sysmon.pyz" -p "/usr/bin/env python3" -c
rm -rf "$PALCO"
chmod 755 "$DIST/sysmon.pyz"
verde "    sysmon.pyz  ($(du -h "$DIST/sysmon.pyz" | cut -f1))"

# ---------------------------------------------------------------- clientes
# Um pacote por sistema, com o nome dizendo para quem e. Antes era um so
# chamado "clientes", e ninguem adivinha que o .zip e o do Windows.

# --- Windows: .pyz + lancadores + scripts de instalacao
NOME="sysmon-windows-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA"
install -m 755 "$DIST/sysmon.pyz" "$PASTA/"
for f in config.example.json requirements.txt sysmon.vbs sysmon.bat \
         instalar-autostart.ps1 desinstalar-autostart.ps1 limpar.ps1; do
    install -m 644 "$AQUI/windows-tray/$f" "$PASTA/"
done
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon para Windows $VERSAO

E ESTE o pacote do Windows. Extraia numa pasta sua (nao dentro de Downloads) e:

1) Duplo clique em sysmon.bat

   A janela usa Tkinter, que ja vem com o Python - nao instala nada. Na
   primeira vez ele instala so o icone de bandeja (pystray + pillow, dois
   pacotes do pip, opcionais); sem eles a janela funciona igual.

   Abre com console, entao qualquer erro aparece na tela. A interface abre na
   TELA DE CONFIGURACAO: preencha apelido, url e token de cada host Linux,
   clique em Testar e salve. Nao precisa editar arquivo nenhum.

   A url e o token sao os que o install.sh imprimiu em cada host monitorado.

2) Funcionando, registre o inicio automatico:

    powershell -ExecutionPolicy Bypass -File instalar-autostart.ps1

   A partir dai use o atalho da area de trabalho, sem console. Fechar a janela
   nao encerra: o programa fica na bandeja. Sair e pelo menu do icone.

Deu errado alguma tentativa anterior? Limpe tudo e recomece:

    powershell -ExecutionPolicy Bypass -File limpar.ps1

O config.json guarda os tokens de TODOS os hosts em texto claro. Proteja:

    icacls config.json /inheritance:r /grant:r "%USERNAME%:R"

Atualizar: o proprio programa avisa e troca sozinho. Manualmente, basta
substituir o sysmon.pyz - o config.json fica.

Documentacao: https://github.com/9LEVEL/sysmon
EOF

( cd "$DIST" && tar czf "$NOME.tar.gz" "$NOME" >/dev/null 2>&1 || true )
rm -f "$DIST/$NOME.tar.gz"
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

# --- Linux/macOS: .pyz + exemplo de config, sem os scripts do Windows
NOME="sysmon-linux-$VERSAO"
PASTA="$DIST/$NOME"
mkdir -p "$PASTA"
install -m 755 "$DIST/sysmon.pyz" "$PASTA/"
install -m 644 "$AQUI/windows-tray/config.example.json" "$PASTA/"
install -m 644 "$AQUI/LICENSE" "$PASTA/"

cat > "$PASTA/LEIAME.txt" <<EOF
sysmon para Linux/macOS $VERSAO

Cliente para acompanhar seus hosts. Precisa de Python 3.9 ou mais novo.

    cp config.example.json config.json    # preencha url e token de cada host
    chmod 600 config.json
    python3 sysmon.pyz term               # tabela no terminal
    python3 sysmon.pyz                    # janela nativa + bandeja (padrao)

Sem config, a janela abre direto na tela de configuracao. O modo terminal
exige o arquivo pronto.

Para o icone de bandeja (opcional):  pip install pystray pillow

Outros modos:

    python3 sysmon.pyz term --once        # imprime uma vez e sai (cron)
    python3 sysmon.pyz term --host pve    # detalhe de um host
    python3 sysmon.pyz local              # sensores DESTA maquina, sem rede

Documentacao: https://github.com/9LEVEL/sysmon
EOF

( cd "$DIST" && tar czf "$NOME.tar.gz" "$NOME" )
rm -rf "$PASTA"
verde "    $NOME.tar.gz"

# ---------------------------------------------------------------- somas
# O .pyz PRECISA estar aqui: o auto-update confere o download contra esta
# lista antes de trocar o binario. Sem a entrada, ele recusa a atualizacao.
# nullglob para que a ausencia do zip (sem o utilitario instalado) nao passe
# o padrao literal para o sha256sum e derrube o script pelo set -e.
( shopt -s nullglob; cd "$DIST" && sha256sum ./*.tar.gz ./*.zip ./*.pyz \
    | sed 's|\./||' > SHA256SUMS )
grep -q ' sysmon.pyz$' "$DIST/SHA256SUMS" || {
    echo "ERRO: SHA256SUMS sem sysmon.pyz - o auto-update ficaria quebrado." >&2
    exit 1
}
verde "    SHA256SUMS"

echo
azul "==> dist/"
ls -lh "$DIST"
