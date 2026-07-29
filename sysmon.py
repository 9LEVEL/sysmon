#!/usr/bin/env python3
"""
sysmon - ponto de entrada unico dos clientes.

    python3 sysmon.py                 # dashboard web + bandeja, tudo junto
    python3 sysmon.py web             # so o dashboard web
    python3 sysmon.py term            # tabela no terminal
    python3 sysmon.py term --once     # imprime uma vez e sai (para script/cron)
    python3 sysmon.py tray            # so o icone de bandeja (Windows)
    python3 sysmon.py local           # sensores DESTA maquina, sem rede

Sem subcomando ele sobe o servidor web e, se pystray estiver instalado, o icone
de bandeja no mesmo processo - um autostart so, um processo so.

O dashboard web nao precisa de nada alem da stdlib. A bandeja precisa de
pystray e Pillow; sem eles o resto continua funcionando e o motivo aparece no
terminal, em vez de o programa simplesmente nao subir.

Distribuicao: `make bundle` empacota isto num unico sysmon.pyz, que roda com
`python sysmon.pyz` sem instalar nada.
"""

from __future__ import annotations

import argparse
import sys
import threading
import time
import webbrowser
from pathlib import Path

# Funciona tanto do repositorio (tools/ ao lado) quanto de dentro do .pyz,
# onde os modulos ficam na raiz do arquivo.
_AQUI = Path(__file__).resolve().parent
for _cand in (_AQUI / "tools", _AQUI):
    if (_cand / "sysmon_nucleo.py").is_file() or (_cand / "sysmon_nucleo.pyc").is_file():
        sys.path.insert(0, str(_cand))
        break
else:
    sys.path.insert(0, str(_AQUI))

from sysmon_nucleo import (  # noqa: E402
    ErroConfig, Frota, achar_config, avisar_permissao, carregar_config,
)

__version__ = "2.2.0"

PORTA_PADRAO = 9110


def _comuns(p: argparse.ArgumentParser) -> None:
    p.add_argument("--config", help="caminho do config.json")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        prog="sysmon",
        description="Monitor de varios hosts Linux: web, terminal e bandeja.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Sem subcomando: sobe o dashboard web e a bandeja juntos.",
    )
    _comuns(p)
    p.add_argument("--porta", type=int, default=PORTA_PADRAO)
    p.add_argument("--host", default="127.0.0.1",
                   help="IP de bind do dashboard (padrao 127.0.0.1; nao tem senha)")
    p.add_argument("--nao-abrir", action="store_true", help="nao abrir o browser")
    p.add_argument("--sem-bandeja", action="store_true", help="nao iniciar o icone")
    p.add_argument("--version", action="version", version=__version__)

    sub = p.add_subparsers(dest="cmd")

    web = sub.add_parser("web", help="so o dashboard web")
    _comuns(web)
    web.add_argument("--porta", type=int, default=PORTA_PADRAO)
    web.add_argument("--host", default="127.0.0.1")
    web.add_argument("--nao-abrir", action="store_true")

    term = sub.add_parser("term", help="tabela no terminal")
    _comuns(term)
    term.add_argument("--once", action="store_true")
    term.add_argument("--json", action="store_true", dest="como_json")
    term.add_argument("--host", help="detalhe completo de um host")
    term.add_argument("--intervalo", type=float)
    term.add_argument("--sem-cor", action="store_true")

    tray = sub.add_parser("tray", help="so o icone de bandeja (Windows)")
    _comuns(tray)

    loc = sub.add_parser("local", help="sensores desta maquina, sem rede")
    loc.add_argument("--watch", action="store_true")
    loc.add_argument("--intervalo", type=float, default=2.0)
    loc.add_argument("--json", action="store_true")

    args = p.parse_args(argv)

    if args.cmd == "local":
        import sysmon_local
        return sysmon_local.main(args) or 0
    if args.cmd == "term":
        import sysmon_dash
        return sysmon_dash.main(args)
    if args.cmd == "web":
        return _subir(args, bandeja=False)
    if args.cmd == "tray":
        return _subir(args, web=False)
    return _subir(args)


def _subir(args, web: bool = True, bandeja: bool = True) -> int:
    """Sobe servidor web e/ou bandeja no mesmo processo."""
    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)

    frota = Frota(cfg)
    servidor = None

    if web:
        import sysmon_web
        endereco = (getattr(args, "host", "127.0.0.1") or "127.0.0.1",
                    getattr(args, "porta", PORTA_PADRAO))
        if endereco[0] not in ("127.0.0.1", "::1", "localhost"):
            print(f"AVISO: dashboard escutando em {endereco[0]}. A pagina nao tem "
                  "autenticacao e mostra a telemetria de todos os hosts.",
                  file=sys.stderr)
        try:
            servidor = sysmon_web.servidor(frota, *endereco)
        except OSError as e:
            print(f"erro: nao consegui escutar em {endereco[0]}:{endereco[1]}: {e}",
                  file=sys.stderr)
            print("Ja ha um sysmon rodando? Use --porta para escolher outra.",
                  file=sys.stderr)
            return 1

    # A bandeja e opcional de proposito: no Linux quase ninguem instala pystray,
    # e o dashboard web sozinho ja resolve. Sem ela, avisa e segue.
    tray = None
    if bandeja:
        try:
            import sysmon_tray
            sysmon_tray.preparar(frota, cfg)
            tray = sysmon_tray
        except ImportError as e:
            if not web:
                print(f"erro: a bandeja precisa de pystray e Pillow ({e}).\n"
                      "  pip install pystray pillow", file=sys.stderr)
                return 1
            print(f"bandeja indisponivel ({e}); seguindo so com o dashboard.",
                  file=sys.stderr)

    frota.iniciar()

    url = f"http://{getattr(args, 'host', '127.0.0.1')}:{getattr(args, 'porta', PORTA_PADRAO)}/"
    if servidor:
        threading.Thread(target=servidor.serve_forever, name="web", daemon=True).start()
        print(f"sysmon {__version__}  ->  {url}   ({len(cfg.hosts)} host(s))")
        if not getattr(args, "nao_abrir", False):
            threading.Timer(0.6, lambda: webbrowser.open(url)).start()
    if tray:
        print("bandeja ativa")
    print("Ctrl+C para sair")

    try:
        if tray:
            # pystray precisa do loop dele na thread principal no Windows, e o
            # tkinter do overlay tambem - por isso a bandeja manda aqui.
            tray.rodar(frota)
        else:
            while True:
                time.sleep(3600)
    except KeyboardInterrupt:
        print()
    finally:
        frota.parar()
        if servidor:
            servidor.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
