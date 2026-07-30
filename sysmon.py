#!/usr/bin/env python3
"""
sysmon - ponto de entrada unico dos clientes.

    python3 sysmon.py                 # JANELA NATIVA do dashboard (padrao)
    python3 sysmon.py --oculto        # sobe minimizado na bandeja (autostart)
    python3 sysmon.py --browser       # forcar o browser em vez da janela
    python3 sysmon.py web             # so serve a pagina, sem abrir nada
    python3 sysmon.py term            # tabela no terminal
    python3 sysmon.py term --once     # imprime uma vez e sai (para script/cron)
    python3 sysmon.py tray            # so a bandeja, com o overlay antigo
    python3 sysmon.py local           # sensores DESTA maquina, sem rede

Sem subcomando abre uma JANELA do sistema - sem barra de endereco e sem aba de
browser - com o dashboard dentro, e o icone de bandeja junto no mesmo processo.
Um autostart, um processo.

Camadas opcionais, todas degradando com aviso em vez de impedir o programa de
subir:

    pywebview        janela nativa e "sempre no topo"; sem ele, cai no browser
    pystray, Pillow  icone de bandeja; sem eles, so a janela

O dashboard em si nao precisa de nada alem da stdlib.

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

__version__ = "2.3.0"

PORTA_PADRAO = 9110


def _comuns(p: argparse.ArgumentParser) -> None:
    p.add_argument("--config", help="caminho do config.json")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        prog="sysmon",
        description="Monitor de varios hosts Linux: web, terminal e bandeja.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Sem subcomando: abre a janela nativa com a bandeja junto.",
    )
    _comuns(p)
    p.add_argument("--porta", type=int, default=PORTA_PADRAO)
    p.add_argument("--host", default="127.0.0.1",
                   help="IP de bind do dashboard (padrao 127.0.0.1; nao tem senha)")
    p.add_argument("--nao-abrir", action="store_true",
                   help="no modo browser, nao abrir a aba automaticamente")
    p.add_argument("--browser", action="store_true",
                   help="forcar o browser em vez da janela nativa")
    p.add_argument("--oculto", action="store_true",
                   help="abrir minimizado na bandeja (usado pelo autostart)")
    p.add_argument("--version", action="version", version=__version__)

    sub = p.add_subparsers(dest="cmd")

    web = sub.add_parser("web", help="so o dashboard web")
    _comuns(web)
    web.add_argument("--porta", type=int, default=PORTA_PADRAO)
    web.add_argument("--host", default="127.0.0.1")
    web.add_argument("--nao-abrir", action="store_true")
    web.add_argument("--browser", action="store_true", default=True,
                     help=argparse.SUPPRESS)

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
        # `web` e explicitamente o modo browser: serve a pagina e nao abre janela.
        return _subir(args, janela=False)
    if args.cmd == "tray":
        return _subir(args, web=False, janela=False)
    return _subir(args)


def _subir(args, web: bool = True, janela: bool = True) -> int:
    """Sobe o servidor local e a interface, tudo no mesmo processo.

    A interface preferida e a JANELA NATIVA (pywebview): sem barra de endereco,
    sem aba de browser, e com "sempre no topo" - que so a janela do sistema
    consegue fazer. Sem pywebview instalado, cai no browser e diz por que.
    """
    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)

    endereco = (getattr(args, "host", "127.0.0.1") or "127.0.0.1",
                getattr(args, "porta", PORTA_PADRAO))
    url = f"http://{endereco[0]}:{endereco[1]}/"

    # Decide a interface ANTES de abrir a porta, porque o modo vai no payload
    # que a pagina consome para saber se mostra os controles de janela.
    app = None
    if janela and not getattr(args, "browser", False):
        try:
            import sysmon_app
            if sysmon_app.disponivel():
                app = sysmon_app
            else:
                print("Sem motor de janela nesta maquina (WebView2 no Windows, "
                      "WebKitGTK no Linux); abrindo no browser.", file=sys.stderr)
        except ImportError:
            print("Janela nativa indisponivel: pip install pywebview\n"
                  "Abrindo no browser por enquanto.", file=sys.stderr)

    frota = Frota(cfg)
    modo = "app" if app else "browser"

    if not web:
        modo = "browser"
    servidor = None
    if web:
        if endereco[0] not in ("127.0.0.1", "::1", "localhost"):
            print(f"AVISO: dashboard escutando em {endereco[0]}. A pagina nao tem "
                  "autenticacao e mostra a telemetria de todos os hosts.",
                  file=sys.stderr)
        try:
            import sysmon_web
            servidor = sysmon_web.servidor(frota, *endereco, modo=modo)
        except OSError as e:
            print(f"erro: nao consegui escutar em {endereco[0]}:{endereco[1]}: {e}",
                  file=sys.stderr)
            print("Ja ha um sysmon rodando? Use --porta para escolher outra.",
                  file=sys.stderr)
            return 1

    frota.iniciar()
    if servidor:
        threading.Thread(target=servidor.serve_forever, name="web", daemon=True).start()

    def encerrar() -> None:
        frota.parar()
        if servidor:
            servidor.shutdown()

    print(f"sysmon {__version__}   {len(cfg.hosts)} host(s)   {url}")

    try:
        if app:
            com_bandeja = _bandeja_junto(frota, cfg, app)
            oculto = bool(getattr(args, "oculto", False))
            if oculto and not com_bandeja:
                # Sem bandeja nao existe caminho de volta para uma janela
                # oculta: o usuario ficaria com um processo invisivel.
                print("--oculto ignorado: sem bandeja nao haveria como reabrir "
                      "a janela.", file=sys.stderr)
                oculto = False
            print("janela nativa; Ctrl+C aqui tambem encerra")
            app.rodar(url, ao_fechar=encerrar, oculto=oculto)
            return 0
        if not web:
            # `sysmon.py tray`: bandeja com overlay, sem servidor.
            return _so_bandeja(frota, cfg)
        if not getattr(args, "nao_abrir", False):
            threading.Timer(0.6, lambda: webbrowser.open(url)).start()
        print("Ctrl+C para sair")
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        print()
    finally:
        encerrar()
    return 0


def _bandeja_junto(frota, cfg, app) -> bool:
    """Icone de bandeja ao lado da janela nativa, se pystray existir.

    Modo reduzido: sem o overlay do tkinter, que seria redundante com a janela
    e disputaria a thread principal com ela. Falha silenciosa de proposito - a
    bandeja e um extra, e nao deve impedir a janela de abrir.
    """
    try:
        import sysmon_tray
    except ImportError:
        return False
    try:
        icone = sysmon_tray.icone_simples(frota, {
            "mostrar": lambda: app.mostrar(),
            "alternar_topo": lambda: app.alternar_topo(),
            "no_topo": lambda: app.no_topo(),
            "atualizar": lambda: frota.atualizar_agora(),
            "sair": lambda: app.fechar(),
        })
        sysmon_tray.acompanhar(icone, frota)
        print("bandeja ativa")
        return True
    except Exception as e:  # noqa: BLE001
        print(f"bandeja indisponivel ({e}); seguindo so com a janela.",
              file=sys.stderr)
        return False


def _so_bandeja(frota, cfg) -> int:
    try:
        import sysmon_tray
    except ImportError as e:
        print(f"erro: a bandeja precisa de pystray e Pillow ({e}).\n"
              "  pip install pystray pillow", file=sys.stderr)
        return 1
    sysmon_tray.preparar(frota, cfg)
    print("bandeja ativa (sem dashboard: use `sysmon.py` para os dois juntos)")
    sysmon_tray.rodar(frota)
    return 0


if __name__ == "__main__":
    sys.exit(main())
