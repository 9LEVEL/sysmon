#!/usr/bin/env bash
# Confere que todo lugar que declara versao diz o mesmo numero.
#
#   ./checar-versao.sh          -> exige que todos concordem entre si
#   ./checar-versao.sh 5.0.0    -> exige, alem disso, que o numero seja esse
#
# O segundo uso e o do corte de release: a tag e a fonte da verdade e tudo
# que foi compilado tem que dizer o mesmo. Um agente que responde /version
# diferente do que o release diz e um bug caro de diagnosticar - o host esta
# rodando o que voce acha que esta rodando?
#
# A fonte e o arquivo VERSAO. O cliente recebe o numero por -ldflags no
# build, entao nao tem literal para divergir; o agente tem dois, e sao esses
# que precisam ser conferidos.
set -euo pipefail

AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ESPERADA="${1:-}"

vermelho() { printf '\033[31m%s\033[0m\n' "$*"; }
verde()    { printf '\033[32m%s\033[0m\n' "$*"; }

arquivos=()
versoes=()
anotar() { arquivos+=("$1"); versoes+=("$2"); }

anotar "VERSAO" "$(tr -d ' \n' < "$AQUI/VERSAO")"

v=$(sed -n 's/^var versao = "\(.*\)"$/\1/p' "$AQUI/linux-agent/main.go" | head -n1)
if [[ -n "$v" ]]; then anotar "linux-agent/main.go" "$v"; fi

v=$(sed -n 's/^VERSAO[[:space:]]*?=[[:space:]]*\(.*\)$/\1/p' "$AQUI/linux-agent/Makefile" | head -n1)
if [[ -n "$v" ]]; then anotar "linux-agent/Makefile" "$v"; fi

if [[ ${#arquivos[@]} -eq 0 ]]; then
    vermelho "ERRO: nenhuma declaracao de versao encontrada - a varredura quebrou?"
    exit 1
fi

largura=0
for a in "${arquivos[@]}"; do
    if (( ${#a} > largura )); then largura=${#a}; fi
done

referencia="${ESPERADA:-${versoes[0]}}"
divergentes=0
for i in "${!arquivos[@]}"; do
    if [[ "${versoes[$i]}" == "$referencia" ]]; then
        printf '  %-*s  %s\n' "$largura" "${arquivos[$i]}" "${versoes[$i]}"
    else
        printf '  %-*s  %s  <== diverge\n' "$largura" "${arquivos[$i]}" "${versoes[$i]}"
        divergentes=$((divergentes + 1))
    fi
done
echo

if [[ $divergentes -gt 0 ]]; then
    if [[ -n "$ESPERADA" ]]; then
        vermelho "ERRO: $divergentes de ${#arquivos[@]} nao dizem $ESPERADA."
    else
        vermelho "ERRO: as ${#arquivos[@]} declaracoes nao concordam entre si."
    fi
    echo "Acerte todas para o mesmo numero antes de cortar o release." >&2
    exit 1
fi

verde "versao $referencia, igual nos ${#arquivos[@]} lugares que a declaram"
