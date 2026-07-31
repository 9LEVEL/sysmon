#!/usr/bin/env python3
"""
sysmon - ponto de entrada unico dos clientes.

    python3 sysmon.py                 # JANELA NATIVA (Tkinter, estilo OHM)
    python3 sysmon.py --oculto        # sobe minimizado na bandeja (autostart)
    python3 sysmon.py --web           # o dashboard web, mais rico
    python3 sysmon.py web             # so serve a pagina, sem abrir nada
    python3 sysmon.py term            # tabela no terminal
    python3 sysmon.py term --once     # imprime uma vez e sai (para script/cron)
    python3 sysmon.py tray            # so a bandeja, com o overlay antigo
    python3 sysmon.py local           # sensores DESTA maquina, sem rede

Sem subcomando abre uma janela nativa em Tkinter, no estilo do Open Hardware
Monitor: arvore de sensores por host, "sempre no topo" numa caixinha, e o
icone de bandeja junto no mesmo processo. Tkinter vem com o Python, entao esse
caminho nao depende de pip nem de componente do sistema.

Camadas opcionais, todas degradando com aviso em vez de impedir o programa de
subir:

    pystray, Pillow  icone de bandeja; sem eles, so a janela
    pywebview        janela para o dashboard web (--web); sem ele, navegador

Distribuicao: `make bundle` empacota isto num unico sysmon.pyz, que roda com
`python sysmon.pyz` sem instalar nada.
"""

from __future__ import annotations

import argparse
import os
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
    Config, ErroConfig, Frota, achar_config, avisar_permissao, carregar_config,
)

__version__ = "3.4.0"

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
    p.add_argument("--web", action="store_true",
                   help="usar o dashboard web (janela do webview ou navegador)")
    p.add_argument("--browser", action="store_true",
                   help="com --web, forcar o navegador em vez do webview")
    p.add_argument("--oculto", action="store_true",
                   help="abrir minimizado na bandeja (usado pelo autostart)")
    p.add_argument("--nao-oculto", action="store_true",
                   help=argparse.SUPPRESS)   # usado pelo atalho, vence --oculto
    p.add_argument("--sem-update", action="store_true",
                   help="nao verificar atualizacao")
    p.add_argument("--sem-instalar", action="store_true",
                   help="nao instalar componentes faltando automaticamente")
    p.add_argument("--version", action="version", version=__version__)

    sub = p.add_subparsers(dest="cmd")

    web = sub.add_parser("web", help="so serve a pagina, nao abre janela")
    _comuns(web)
    web.add_argument("--porta", type=int, default=PORTA_PADRAO)
    web.add_argument("--host", default="127.0.0.1")
    web.add_argument("--nao-abrir", action="store_true")
    web.add_argument("--web", action="store_true", default=True,
                     help=argparse.SUPPRESS)
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
        # Sem configuracao valida NAO morremos: subimos com a frota vazia e a
        # interface abre na tela de configuracao. Morrer aqui era o pior caso
        # possivel - sob pythonw a mensagem ia para lugar nenhum e o usuario
        # via apenas uma janela que nao abria.
        if web:
            print(f"sem configuracao ainda ({e.args[0].splitlines()[0]})",
                  file=sys.stderr)
            print("abrindo a tela de configuracao...", file=sys.stderr)
            cfg = Config(hosts=[])
        else:
            print(f"erro: {e}", file=sys.stderr)
            return 2
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)

    endereco = (getattr(args, "host", "127.0.0.1") or "127.0.0.1",
                getattr(args, "porta", PORTA_PADRAO))
    url = f"http://{endereco[0]}:{endereco[1]}/"

    # Decide a interface ANTES de abrir a porta, porque o modo vai no payload
    # que a pagina consome para saber se mostra os controles de janela.
    # A janela padrao e a nativa em Tkinter: vem com o Python, nao depende de
    # pip nem de motor de navegador do sistema. O dashboard web continua
    # disponivel em --web, e e mais rico, mas depende de coisas que faltam em
    # maquina real com frequencia demais para ser o padrao.
    nativa = None
    app = None
    if janela:
        if getattr(args, "web", False):
            app = _abrir_janela(args)
        else:
            try:
                import sysmon_win
                nativa = sysmon_win
            except ImportError as e:
                print(f"Tkinter indisponivel ({e}); usando o dashboard web.",
                      file=sys.stderr)
                print("  No Debian/Ubuntu: apt install python3-tk", file=sys.stderr)
                app = _abrir_janela(args)

    frota = Frota(cfg)
    modo = "app" if app else "browser"

    # Atualizacao silenciosa: verifica no arranque e a cada N horas, baixa em
    # segundo plano e deixa pronto. A troca acontece no proximo arranque,
    # feita pelo lancador - ver sysmon_update.
    atualizador = None
    if not getattr(args, "sem_update", False):
        try:
            import sysmon_update
            horas = float(cfg.extra.get("horas_entre_updates", 6))
            atualizador = sysmon_update.Atualizador(
                __version__, intervalo=horas * 3600 if horas > 0 else 0)
            atualizador.iniciar()
        except Exception:  # noqa: BLE001 - update nunca impede o monitor de subir
            atualizador = None

    servidor = None
    if web:
        if endereco[0] not in ("127.0.0.1", "::1", "localhost"):
            print(f"AVISO: dashboard escutando em {endereco[0]}. A pagina nao tem "
                  "autenticacao e mostra a telemetria de todos os hosts.",
                  file=sys.stderr)
        try:
            import sysmon_web
            servidor = sysmon_web.servidor(
                frota, *endereco, modo=modo, atualizador=atualizador,
                reiniciar=(lambda: _reiniciar(app)) if app else None,
                mostrar=(lambda: app.mostrar()) if app else None,
                caminho_config=caminho)
        except OSError:
            # Porta ocupada quase sempre significa "ja esta rodando" - o caso
            # do duplo clique no atalho, ou de autostart duplicado. Em vez de
            # morrer em silencio sob pythonw, pede para a instancia existente
            # aparecer e sai limpo.
            if _pedir_para_aparecer(url):
                print("sysmon ja esta rodando; trouxe a janela para a frente.")
                return 0
            print(f"erro: a porta {endereco[1]} esta ocupada por outro programa.",
                  file=sys.stderr)
            print("Use --porta para escolher outra.", file=sys.stderr)
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
        if nativa:
            return _rodar_nativa(nativa, frota, caminho, cfg, url, args)
        if app:
            com_bandeja = _bandeja_junto(frota, cfg, app)
            oculto = bool(getattr(args, "oculto", False)) and \
                not getattr(args, "nao_oculto", False)
            if oculto and not com_bandeja:
                # Sem bandeja nao existe caminho de volta para uma janela
                # oculta: o usuario ficaria com um processo invisivel.
                print("--oculto ignorado: sem bandeja nao haveria como reabrir "
                      "a janela.", file=sys.stderr)
                oculto = False
            if com_bandeja:
                print("app na bandeja: fechar a janela nao encerra; use Sair no icone")
            else:
                print("janela nativa; fechar a janela encerra")
            app.rodar(url, ao_fechar=encerrar, oculto=oculto,
                      ficar_na_bandeja=com_bandeja)
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


# Marcador de "ja tentei instalar": evita laco de instalacao a cada arranque
# quando o pip nao resolve (sem rede, sem permissao, wheel indisponivel).
def _marcador_instalacao() -> Path:
    if os.name == "nt":
        base = Path(os.environ.get("APPDATA") or Path.home() / "AppData/Roaming")
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")
    return base / "sysmon" / f"componentes-{__version__}.tentado"


def _instalar_componentes() -> bool:
    """Instala pywebview/pystray/pillow com o pip do proprio interpretador.

    O usuario pediu um app, nao um site: cair no navegador porque falta um
    pacote e detalhe de instalacao virando defeito de produto. E como o
    programa pode ser iniciado pelo atalho, pelo agendador ou pelo .bat, o
    unico lugar que cobre todos os casos e aqui dentro.
    """
    import subprocess
    marcador = _marcador_instalacao()
    if marcador.exists():
        return False
    try:
        marcador.parent.mkdir(parents=True, exist_ok=True)
        marcador.touch()
    except OSError:
        pass

    print("Instalando os componentes do app (janela e bandeja)...", file=sys.stderr)
    try:
        r = subprocess.run(
            [sys.executable, "-m", "pip", "install", "--disable-pip-version-check",
             "pywebview", "pystray", "pillow"],
            capture_output=True, text=True, timeout=300)
    except (OSError, subprocess.SubprocessError) as e:
        print(f"  nao consegui rodar o pip: {e}", file=sys.stderr)
        return False
    if r.returncode != 0:
        print("  o pip falhou. Instale manualmente:", file=sys.stderr)
        print("  python -m pip install pywebview pystray pillow", file=sys.stderr)
        for linha in (r.stderr or "").strip().splitlines()[-3:]:
            print(f"    {linha}", file=sys.stderr)
        return False
    print("  instalados.", file=sys.stderr)
    return True


def _abrir_janela(args):
    """Devolve o modulo da janela nativa, ou None para cair no navegador.

    Tenta instalar o que falta uma vez antes de desistir - reiniciando o
    processo em seguida, porque um pacote instalado depois do arranque nao
    entra no interpretador ja em execucao.
    """
    import importlib

    def tentar():
        try:
            mod = importlib.import_module("sysmon_app")
            if mod.disponivel():
                return mod
            print("pywebview instalado, mas sem motor de janela nesta maquina.",
                  file=sys.stderr)
            if os.name == "nt":
                print("  No Windows isso costuma ser o WebView2 ausente - ele vem "
                      "com o Edge; instale de https://go.microsoft.com/fwlink/p/?LinkId=2124703",
                      file=sys.stderr)
            return None
        except ImportError:
            return "faltando"

    r = tentar()
    if r != "faltando":
        return r

    if getattr(args, "sem_instalar", False) or not _instalar_componentes():
        print("Abrindo no navegador. Para a janela do app:", file=sys.stderr)
        print("  python -m pip install pywebview pystray pillow", file=sys.stderr)
        return None

    # Reinicia para que os pacotes recem-instalados sejam importaveis.
    print("Reiniciando com a janela do app...", file=sys.stderr)
    try:
        os.execv(sys.executable, [sys.executable] + sys.argv)
    except OSError:
        pass   # nao deu: segue no navegador, ja avisado acima
    return None


def _pedir_para_aparecer(url: str) -> bool:
    """Confirma que quem ocupa a porta e um sysmon, e pede a janela dele."""
    import urllib.request
    try:
        with urllib.request.urlopen(url + "api/mostrar", timeout=2) as r:
            return r.status == 200
    except Exception:  # noqa: BLE001 - nao e um sysmon, ou nao respondeu
        return False


def _reiniciar(app) -> None:
    """Fecha a janela e pede ao lancador para subir de novo.

    Quem troca o sysmon.pyz e o lancador, antes do Python abrir o arquivo.
    Aqui so encerramos e disparamos o lancador de novo; se ele nao existir,
    fecha mesmo assim e o usuario reabre - a atualizacao entra igual.
    """
    import subprocess
    lancador = Path(sys.argv[0]).resolve().parent / "sysmon.vbs"
    try:
        if os.name == "nt" and lancador.is_file():
            subprocess.Popen(["wscript.exe", str(lancador)],
                             cwd=str(lancador.parent),
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except Exception:  # noqa: BLE001 - fechar mesmo assim e melhor que travar
        pass
    app.fechar()


def _rodar_nativa(nativa, frota, caminho, cfg, url, args) -> int:
    """Janela Tkinter na thread principal, bandeja e servidor em threads."""
    def com_bandeja(janela) -> bool:
        try:
            import sysmon_tray
        except ImportError:
            return False
        try:
            icone = sysmon_tray.icone_simples(frota, {
                "mostrar": lambda: janela.pedir("mostrar"),
                "alternar_topo": lambda: janela.pedir("topo"),
                # Consulta direto: leitura de BooleanVar de outra thread e
                # inofensiva, e o menu precisa do valor na hora de desenhar.
                "no_topo": lambda: bool(janela.no_topo.get()),
                "atualizar": lambda: janela.pedir("atualizar"),
                "sair": lambda: janela.pedir("sair"),
            })
            sysmon_tray.acompanhar(icone, frota)
            print("bandeja ativa; fechar a janela nao encerra")
            return True
        except Exception as e:  # noqa: BLE001
            print(f"bandeja indisponivel ({e}); seguindo so com a janela.",
                  file=sys.stderr)
            return False

    import webbrowser as wb
    nativa.rodar(frota, caminho, intervalo=max(2.0, cfg.intervalo),
                 abrir_web=lambda: wb.open(url), com_bandeja=com_bandeja,
                 oculto=bool(getattr(args, "oculto", False)))
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
            "sair": lambda: app.sair(),
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
