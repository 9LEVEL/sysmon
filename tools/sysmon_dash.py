#!/usr/bin/env python3
"""
sysmon-dash.py - dashboard de varios hosts no terminal.

Consulta todos os agentes do config e desenha uma tabela, uma linha por host.
Roda de qualquer maquina Linux (ou por SSH), sem instalar nada: stdlib pura.

    python3 sysmon-dash.py                      # tabela viva, atualiza sozinha
    python3 sysmon-dash.py --once               # imprime uma vez e sai
    python3 sysmon-dash.py --json               # a frota inteira em JSON
    python3 sysmon-dash.py --host pve           # detalhe completo de um host
    python3 sysmon-dash.py --config outro.json

O config e o mesmo do tray do Windows. Gere com linux-agent/deploy.sh.

O arquivo manda: nada do ambiente sobrescreve valor presente no config.json.
Sem arquivo nenhum, da para checar um host avulso pelo ambiente:

    SYSMON_URL=http://10.0.0.5:9109/metrics SYSMON_TOKEN=... sysmon-dash.py --once

Ver a tabela completa de variaveis no README.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from sysmon_nucleo import (  # noqa: E402
    AVISO, CRITICO, OFFLINE, OK,
    ErroConfig, Frota,
    achar_config, avaliar, avisar_permissao, carregar_config, como_dict,
    discos_relevantes,
    fmt_bps, fmt_bytes, fmt_pct, fmt_temp, fmt_uptime, resumo_linhas,
)

__version__ = "4.1.0"


RESET = "\033[0m"
NEGRITO = "\033[1m"
FRACO = "\033[2m"
COR_NIVEL = {OK: "\033[32m", AVISO: "\033[33m", CRITICO: "\033[31m", OFFLINE: "\033[90m"}


class Tinta:
    """Some com as cores quando a saida nao e um terminal (pipe, arquivo, log)."""

    def __init__(self, ativo: bool) -> None:
        self.ativo = ativo

    def __call__(self, texto: str, *codigos: str) -> str:
        if not self.ativo or not codigos:
            return texto
        return "".join(codigos) + texto + RESET


# ------------------------------------------------------------------ colunas
def _maior_disco(d: dict, lim=None) -> str:
    discos = discos_relevantes(d.get("discos"), lim)
    if not discos:
        return "--"
    # O agente ja devolve ordenado por uso; o primeiro e o que importa.
    pior = discos[0]
    mount = pior["mount"]
    if len(mount) > 12:
        mount = "..." + mount[-9:]
    return f"{mount} {pior['percent']:.0f}%"


def _rede(d: dict) -> str:
    ativas = [n for n in (d.get("net") or []) if n.get("up")]
    if not ativas:
        return "--"
    # Soma as interfaces ativas: o interesse aqui e o trafego do host.
    rx = sum(n["rx_bps"] for n in ativas if n.get("rx_bps") is not None)
    tx = sum(n["tx_bps"] for n in ativas if n.get("tx_bps") is not None)
    return f"v{fmt_bps(rx)} ^{fmt_bps(tx)}"


def _psi_io(d: dict) -> str:
    v = ((d.get("pressure") or {}).get("io") or {}).get("some_avg60")
    return fmt_pct(v)


def _load(d: dict) -> str:
    load = d.get("load") or []
    if len(load) < 3:
        return "--"
    return f"{load[0]:.2f} {load[1]:.2f} {load[2]:.2f}"


# (titulo, largura, extrator, prioridade) - prioridade menor cai primeiro
# quando o terminal e estreito.
COLUNAS = [
    ("HOST", 10, None, 0),
    ("CPU", 6, lambda d: fmt_pct(d.get("cpu_percent")), 0),
    ("TEMP", 6, lambda d: fmt_temp(d.get("cpu_temp")), 1),
    ("RAM", 6, lambda d: fmt_pct((d.get("mem") or {}).get("percent")), 0),
    ("SWAP", 6, lambda d: fmt_pct((d.get("mem") or {}).get("swap_percent")), 4),
    ("DISCO", 17, _maior_disco, 1),
    ("LOAD", 17, _load, 2),
    ("REDE", 18, _rede, 3),
    ("PSI-IO", 7, _psi_io, 5),
    ("UP", 8, lambda d: fmt_uptime(d.get("uptime_s")), 3),
]


def colunas_que_cabem(largura: int) -> list[tuple]:
    cols = list(COLUNAS)
    while len(cols) > 2 and sum(c[1] + 1 for c in cols) > largura:
        # Descarta a coluna menos importante ainda presente.
        pior = max(cols[1:], key=lambda c: c[3])
        cols.remove(pior)
    return cols


# ------------------------------------------------------------------ desenho
def desenhar(frota: Frota, tinta: Tinta, largura: int) -> list[str]:
    cols = colunas_que_cabem(largura)
    saida: list[str] = []

    estados = frota.estados()
    alertas = frota.alertas()
    offline = sum(1 for _, e in estados if e.erro or not e.dados)

    cabecalho = f"sysmon  {len(estados)} host(s)"
    if offline:
        cabecalho += tinta(f"  {offline} offline", COR_NIVEL[OFFLINE])
    if alertas:
        cabecalho += tinta(f"  {len(alertas)} alerta(s)", COR_NIVEL[AVISO])
    relogio = time.strftime("%H:%M:%S")
    espaco = max(1, largura - len(_sem_cor(cabecalho)) - len(relogio))
    saida.append(tinta(cabecalho, NEGRITO) + " " * espaco + tinta(relogio, FRACO))
    saida.append("")

    titulo = " ".join(f"{c[0]:<{c[1]}}" for c in cols)
    saida.append(tinta(titulo[:largura], NEGRITO))

    for host, estado in estados:
        nivel, _ = avaliar(estado, frota.cfg.limiares)
        cor = COR_NIVEL[nivel]
        celulas = [f"{_cortar(host.nome, cols[0][1]):<{cols[0][1]}}"]

        if estado.erro or not estado.dados:
            resto = f"offline: {estado.erro or 'sem dados'}"
            linha = celulas[0] + " " + resto
            saida.append(tinta(_cortar(linha, largura), cor))
            continue

        for titulo_col, larg, extrator, _ in cols[1:]:
            try:
                valor = extrator(estado.dados)
            except Exception:  # noqa: BLE001 - campo faltando nunca quebra a tela
                valor = "?"
            celulas.append(f"{_cortar(valor, larg):<{larg}}")
        saida.append(tinta(_cortar(" ".join(celulas), largura), cor))

    if alertas:
        saida.append("")
        for a in alertas:
            saida.append(tinta(_cortar("! " + a, largura), COR_NIVEL[CRITICO]))
    return saida


def _cortar(texto: str, largura: int) -> str:
    return texto if len(texto) <= largura else texto[: max(0, largura - 1)] + "~"


def _sem_cor(texto: str) -> str:
    out, dentro = [], False
    for ch in texto:
        if ch == "\033":
            dentro = True
        elif dentro:
            if ch == "m":
                dentro = False
        else:
            out.append(ch)
    return "".join(out)


def detalhe(frota: Frota, nome: str, tinta: Tinta) -> int:
    for host, estado in frota.estados():
        if host.nome != nome:
            continue
        nivel, alertas = avaliar(estado, frota.cfg.limiares)
        for linha in resumo_linhas(host, estado):
            print(tinta(linha, COR_NIVEL[nivel]))
        if estado.dados:
            d = estado.dados
            so = d.get("so") or {}
            print(f"  {'SO':<5} {so.get('nome') or '?'}  (kernel {so.get('kernel') or '?'})")
            if d.get("cpu_modelo"):
                print(f"  {'Chip':<5} {d['cpu_modelo']}")
            for b in d.get("blocos") or []:
                smart = b.get("smart") or {}
                detalhes = [
                    fmt_bytes(b.get("tamanho")),
                    fmt_temp(b.get("temp_c")),
                    f"L {fmt_bps(b.get('leitura_bps'))}",
                    f"E {fmt_bps(b.get('escrita_bps'))}",
                    f"uso {fmt_pct(b.get('util_percent'))}",
                ]
                if smart.get("desgaste_percent") is not None:
                    detalhes.append(f"vida consumida {smart['desgaste_percent']:.0f}%")
                if smart.get("horas_ligado"):
                    detalhes.append(f"{smart['horas_ligado']}h")
                if smart.get("realocados"):
                    detalhes.append(f"{smart['realocados']} realocados")
                if smart.get("saude") == "falha":
                    detalhes.append("SMART REPROVOU")
                print(f"  {b['dev']:<10} [{(b.get('tipo') or '?'):>4}] "
                      f"{(b.get('modelo') or '?')[:26]:<26} " + "  ".join(detalhes))
            for s in (d.get("temps") or []):
                crit = f"  (crit {s['crit']:.0f}C)" if s.get("crit") else ""
                print(f"  {s['chip']:<12} {s['label']:<20} {s['c']:>6.1f}C{crit}")
            for nome_fan, rpm in (d.get("fans") or {}).items():
                print(f"  {nome_fan:<33} {rpm:>6} RPM")
        for a in alertas:
            print(tinta("! " + a, COR_NIVEL[CRITICO]))
        return 0 if nivel < CRITICO else 1

    print(f"host '{nome}' nao esta no config.", file=sys.stderr)
    return 2


# ------------------------------------------------------------------ main


def argumentos(argv: list[str] | None = None):
    """Usado so quando este modulo roda sozinho; o sysmon.py ja entrega args."""
    p = argparse.ArgumentParser(
        description="Dashboard de varios agentes sysmon no terminal.")
    p.add_argument("--config", help="caminho do hosts.json")
    p.add_argument("--once", action="store_true", help="imprime uma vez e sai")
    p.add_argument("--json", action="store_true", dest="como_json",
                   help="despeja a frota inteira em JSON e sai")
    p.add_argument("--host", help="mostra o detalhe completo de um host e sai")
    p.add_argument("--intervalo", type=float,
                   help="segundos entre atualizacoes (sobrepoe o config)")
    p.add_argument("--sem-cor", action="store_true")
    p.add_argument("--version", action="version", version=__version__)
    return p.parse_args(argv)


def main(args=None) -> int:
    if args is None:
        args = argumentos()

    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2

    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)
    if getattr(args, "intervalo", None):
        cfg.intervalo = args.intervalo

    tinta = Tinta(not getattr(args, "sem_cor", False) and sys.stdout.isatty()
                  and os.environ.get("TERM") not in (None, "dumb"))

    frota = Frota(cfg)
    frota.iniciar()
    try:
        # Uma saida unica so faz sentido depois da primeira rodada completa.
        frota.esperar_primeira_leitura(limite=cfg.timeout + 1)

        if getattr(args, "como_json", False):
            print(json.dumps(como_dict(frota), indent=2, ensure_ascii=False))
            return 0
        if getattr(args, "host", None):
            return detalhe(frota, args.host, tinta)
        if getattr(args, "once", False):
            largura = shutil.get_terminal_size((100, 24)).columns
            print("\n".join(desenhar(frota, tinta, largura)))
            return 1 if frota.pior_nivel() >= CRITICO else 0

        interativo = sys.stdout.isatty()
        while True:
            largura, _ = shutil.get_terminal_size((100, 24))
            quadro = "\n".join(desenhar(frota, tinta, largura))
            if interativo:
                # Limpa e volta ao topo; sem scroll infinito.
                sys.stdout.write("\033[2J\033[H")
            sys.stdout.write(quadro + "\n")
            if interativo:
                sys.stdout.write(f"\n{tinta('Ctrl+C para sair', FRACO)}\n")
            sys.stdout.flush()
            time.sleep(max(1.0, cfg.intervalo))
    except KeyboardInterrupt:
        if sys.stdout.isatty():
            sys.stdout.write("\n")
        return 0
    finally:
        frota.parar()


if __name__ == "__main__":
    sys.exit(main())
