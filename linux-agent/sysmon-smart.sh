#!/bin/sh
# Coleta SMART de cada disco e grava em /run/sysmon/smart.json.
#
# Roda como root numa unit isolada, porque smartctl precisa de privilegio e o
# agente exposto a rede nao pode executar nada. O agente so LE o arquivo.
#
# A saida e um objeto {"<dev>": <json cru do smartctl>, ...}. Nada e
# interpretado aqui de proposito: a normalizacao entre NVMe e SATA mora no Go,
# onde da para testar. Este script so precisa produzir JSON valido.
set -eu

SAIDA=/run/sysmon/smart.json
TMP="$SAIDA.tmp"

command -v smartctl >/dev/null 2>&1 || {
    echo "smartctl nao encontrado (apt install smartmontools)" >&2
    exit 0   # ausencia nao e falha: o campo smart fica null e o resto funciona
}

# Discos inteiros, nunca particoes: /sys/block so lista os inteiros.
discos=""
for caminho in /sys/block/*; do
    dev=$(basename "$caminho")
    case "$dev" in
        loop*|ram*|zram*|dm-*|sr*|md*) continue ;;
    esac
    discos="$discos $dev"
done

primeiro=1
{
    printf '{'
    for dev in $discos; do
        # -j JSON, -H saude, -A atributos, -i identidade.
        # O smartctl sai com codigo != 0 quando ha aviso no disco (bit 2 e
        # acima), e isso e justamente o caso que queremos reportar - por isso
        # o "|| true" em vez de deixar o set -e matar a coleta inteira.
        saida=$(smartctl -j -H -A -i "/dev/$dev" 2>/dev/null) || true
        [ -n "$saida" ] || continue
        # Descarta resposta que nao seja objeto JSON.
        case "$saida" in '{'*) ;; *) continue ;; esac

        [ $primeiro -eq 1 ] || printf ','
        primeiro=0
        printf '"%s":%s' "$dev" "$saida"
    done
    printf '}'
} > "$TMP"

# Troca atomica: o agente nunca le um arquivo pela metade.
mv "$TMP" "$SAIDA"
chmod 644 "$SAIDA"
