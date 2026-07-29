#!/usr/bin/env bash
# Instala o sysmon-agent em varios hosts de uma vez, por SSH.
#
#   ./deploy.sh hosts.conf [saida.json]
#
# Formato do hosts.conf (uma linha por host, # comenta):
#
#   # apelido   destino_ssh          ip_de_bind      porta
#   pve         root@192.168.0.10    192.168.0.10    9109
#   nas         root@192.168.0.20    192.168.0.20
#   backup      root@100.90.1.5      100.90.1.5             # IP do Tailscale
#
# No fim ele le o token de cada host e escreve um hosts.json pronto para o
# sysmon.pyz (dashboard e bandeja) - o passo que na v1 voce fazia na mao,
# copiando token por token.
set -euo pipefail

AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF="${1:-}"
SAIDA="${2:-hosts.json}"
REMOTO=/tmp/sysmon-deploy

vermelho() { printf '\033[31m%s\033[0m\n' "$*"; }
amarelo()  { printf '\033[33m%s\033[0m\n' "$*"; }
verde()    { printf '\033[32m%s\033[0m\n' "$*"; }
azul()     { printf '\033[36m%s\033[0m\n' "$*"; }

if [[ -z "$CONF" || ! -f "$CONF" ]]; then
    vermelho "Uso: ./deploy.sh hosts.conf [saida.json]"
    echo
    echo "Exemplo de hosts.conf:"
    echo "  pve   root@192.168.0.10   192.168.0.10   9109"
    echo "  nas   root@192.168.0.20   192.168.0.20"
    exit 1
fi

command -v go >/dev/null || { vermelho "Go nao encontrado - precisa dele para compilar."; exit 1; }

echo "==> Compilando os binarios"
make -C "$AQUI" dist >/dev/null
verde "    ok"

# ssh sem prompt interativo: se a chave nao estiver configurada, falha rapido
# em vez de travar o loop inteiro esperando senha.
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)

declare -a OK_APELIDO=() OK_URL=() OK_TOKEN=()
declare -a FALHOU=()

while read -r apelido destino bind porta _resto; do
    [[ -z "${apelido:-}" || "$apelido" == \#* ]] && continue
    porta="${porta:-9109}"

    echo
    azul "==> $apelido ($destino)"

    if ! ssh "${SSH_OPTS[@]}" "$destino" true 2>/dev/null; then
        vermelho "    sem acesso SSH (chave configurada?)"
        FALHOU+=("$apelido: ssh")
        continue
    fi

    arco="$(ssh "${SSH_OPTS[@]}" "$destino" 'uname -m' 2>/dev/null || echo desconhecida)"
    case "$arco" in
        x86_64)  bin="sysmon-agent-linux-amd64" ;;
        aarch64) bin="sysmon-agent-linux-arm64" ;;
        *)       vermelho "    arquitetura nao suportada: $arco"
                 FALHOU+=("$apelido: arquitetura $arco"); continue ;;
    esac

    ssh "${SSH_OPTS[@]}" "$destino" "rm -rf $REMOTO && mkdir -p $REMOTO/bin"
    scp -q "${SSH_OPTS[@]}" \
        "$AQUI/install.sh" "$AQUI/sysmon-agent.service" \
        "$AQUI/sysmon-thinpool.service" "$AQUI/sysmon-thinpool.timer" \
        "$destino:$REMOTO/"
    scp -q "${SSH_OPTS[@]}" "$AQUI/bin/$bin" "$destino:$REMOTO/bin/sysmon-agent"

    if ssh "${SSH_OPTS[@]}" "$destino" \
        "chmod +x $REMOTO/install.sh $REMOTO/bin/sysmon-agent && \
         sudo $REMOTO/install.sh $bind $porta -y" 2>&1 | sed 's/^/    /'; then
        token="$(ssh "${SSH_OPTS[@]}" "$destino" \
                 "sudo sed -n 's/^SYSMON_TOKEN=//p' /etc/sysmon/token.env")"
        ssh "${SSH_OPTS[@]}" "$destino" "rm -rf $REMOTO"
        OK_APELIDO+=("$apelido")
        OK_URL+=("http://$bind:$porta/metrics")
        OK_TOKEN+=("$token")
        verde "    instalado"
    else
        vermelho "    falhou na instalacao"
        FALHOU+=("$apelido: install")
    fi
done < "$CONF"

# ---------------------------------------------------------------- resultado
echo
if [[ ${#OK_APELIDO[@]} -eq 0 ]]; then
    vermelho "Nenhum host instalado."
    exit 1
fi

{
    echo '{'
    echo '  "intervalo": 5,'
    echo '  "timeout": 4,'
    echo '  "hosts": ['
    for i in "${!OK_APELIDO[@]}"; do
        virgula=","
        [[ $i -eq $((${#OK_APELIDO[@]} - 1)) ]] && virgula=""
        printf '    {"nome": "%s", "url": "%s", "token": "%s"}%s\n' \
            "${OK_APELIDO[$i]}" "${OK_URL[$i]}" "${OK_TOKEN[$i]}" "$virgula"
    done
    echo '  ]'
    echo '}'
} > "$SAIDA"
chmod 600 "$SAIDA"

verde "${#OK_APELIDO[@]} host(s) instalado(s). Config escrita em $SAIDA (modo 600)."
echo "  Terminal Linux : python3 sysmon.py term --config $SAIDA"
echo "  Windows        : copie para windows-tray/config.json"

if [[ ${#FALHOU[@]} -gt 0 ]]; then
    echo
    amarelo "Falharam:"
    printf '  %s\n' "${FALHOU[@]}"
    exit 1
fi
