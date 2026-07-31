#!/usr/bin/env python3
"""
sysmon - ponto de entrada unico dos clientes.

    python3 sysmon.py                 # JANELA NATIVA (Tkinter) + bandeja
    python3 sysmon.py --oculto        # sobe minimizado na bandeja (autostart)
    python3 sysmon.py term            # tabela no terminal
    python3 sysmon.py term --once     # imprime uma vez e sai (para script/cron)
    python3 sysmon.py tray            # so a bandeja, com o overlay antigo
    python3 sysmon.py local           # sensores DESTA maquina, sem rede

Sem subcomando abre uma janela nativa em Tkinter: arvore de sensores por host,
"sempre no topo" numa caixinha escura, e o icone de bandeja junto no mesmo
processo. Tkinter vem com o Python, entao esse caminho NAO depende de pip nem
de componente do sistema.

Camada opcional, que degrada com aviso em vez de impedir o programa de subir:

    pystray + Pillow   icone de bandeja; sem eles, so a janela

Distribuicao: `make bundle` empacota isto num unico sysmon.pyz, que roda com
`python sysmon.pyz` sem instalar nada.
"""

from __future__ import annotations

import argparse
import socket
import sys
import threading
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

__version__ = "4.1.0"

# Porta de loopback usada so como trava de instancia unica e canal para
# "traga a janela para a frente". Nunca escuta fora de 127.0.0.1.
PORTA_CONTROLE = 9110


def _comuns(p: argparse.ArgumentParser) -> None:
    p.add_argument("--config", help="caminho do config.json")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        prog="sysmon",
        description="Monitor de varios hosts Linux: janela nativa, terminal e bandeja.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Sem subcomando: abre a janela nativa com a bandeja junto.",
    )
    _comuns(p)
    p.add_argument("--porta", type=int, default=None,
                   help="porta de controle (loopback, instancia unica; padrao "
                        "9110, ou 'porta_controle' do config)")
    p.add_argument("--oculto", action="store_true",
                   help="abrir minimizado na bandeja (usado pelo autostart)")
    p.add_argument("--nao-oculto", action="store_true",
                   help=argparse.SUPPRESS)   # usado pelo atalho, vence --oculto
    p.add_argument("--sem-update", action="store_true",
                   help="nao verificar atualizacao")
    p.add_argument("--version", action="version", version=__version__)

    sub = p.add_subparsers(dest="cmd")

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
    if args.cmd == "tray":
        return _so_bandeja(args)
    return _janela(args)


class _InstanciaUnica:
    """Trava de instancia unica por socket de loopback.

    Serve tambem de IPC minimo para "traga a janela para a frente" quando o
    sysmon e aberto uma segunda vez - no lugar da antiga deteccao pela porta do
    servidor web. Um pequeno banner ("sysmon") na conexao distingue a nossa
    instancia de outro programa que por acaso ocupe a porta.
    """

    def __init__(self, porta: int) -> None:
        self.porta = porta
        self._srv: socket.socket | None = None
        self._mostrar = None

    def adquirir(self) -> bool:
        """Tenta segurar a porta. False = ja existe outra instancia."""
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            s.bind(("127.0.0.1", self.porta))
            s.listen(4)
        except OSError:
            s.close()
            return False
        self._srv = s
        threading.Thread(target=self._servir, name="instancia", daemon=True).start()
        return True

    def _servir(self) -> None:
        while True:
            try:
                conn, _ = self._srv.accept()   # type: ignore[union-attr]
            except OSError:
                return
            with conn:
                try:
                    conn.sendall(b"sysmon\n")
                    pedido = conn.recv(16)
                except OSError:
                    pedido = b""
            if pedido.strip() == b"mostrar" and self._mostrar:
                try:
                    self._mostrar()
                except Exception:  # noqa: BLE001 - IPC nao pode derrubar a janela
                    pass

    def pedir_para_aparecer(self) -> bool:
        """Confirma que quem ocupa a porta e um sysmon, e pede a janela dele."""
        try:
            with socket.create_connection(("127.0.0.1", self.porta), timeout=2) as c:
                if not c.recv(16).startswith(b"sysmon"):
                    return False   # a porta e de outro programa
                c.sendall(b"mostrar")
            return True
        except OSError:
            return False

    def ligar(self, mostrar) -> None:
        self._mostrar = mostrar

    def fechar(self) -> None:
        if self._srv:
            try:
                self._srv.close()
            except OSError:
                pass
            self._srv = None


def _janela(args) -> int:
    """Sobe a janela nativa (Tkinter) com a bandeja junto, tudo num processo."""
    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        # Sem configuracao valida NAO morremos: subimos com a frota vazia e a
        # janela abre direto na tela de configuracao de hosts. Morrer aqui era o
        # pior caso - sob pythonw a mensagem ia para lugar nenhum e o usuario
        # via apenas uma janela que nao abria.
        print(f"sem configuracao ainda ({e.args[0].splitlines()[0]})", file=sys.stderr)
        print("abrindo a tela de configuracao...", file=sys.stderr)
        cfg = Config(hosts=[])
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)

    # Instancia unica ANTES de tudo: duplo clique no atalho ou autostart
    # duplicado trazem a janela existente para a frente em vez de subir de novo.
    # A porta vem do --porta, ou do config (aceita o nome antigo porta_web), ou
    # do padrao - quem trocou a porta para fugir de conflito continua valendo.
    porta = getattr(args, "porta", None) or int(
        cfg.extra.get("porta_controle") or cfg.extra.get("porta_web") or PORTA_CONTROLE)
    inst = _InstanciaUnica(porta)
    if not inst.adquirir():
        if inst.pedir_para_aparecer():
            print("sysmon ja esta rodando; trouxe a janela para a frente.")
            return 0
        print(f"erro: a porta de controle {inst.porta} esta ocupada por outro "
              "programa.", file=sys.stderr)
        print("Use --porta para escolher outra.", file=sys.stderr)
        return 1

    try:
        import sysmon_win
    except ImportError as e:
        print(f"erro: Tkinter indisponivel ({e}).", file=sys.stderr)
        print("  No Debian/Ubuntu: apt install python3-tk", file=sys.stderr)
        inst.fechar()
        return 2

    frota = Frota(cfg)

    # Atualizacao silenciosa: verifica no arranque e a cada N horas, baixa em
    # segundo plano e confere o SHA256. A troca em si acontece no proximo
    # arranque, feita pelo lancador (sysmon.vbs/.bat) - um processo nao
    # sobrescreve com seguranca o proprio .pyz que tem aberto.
    if not getattr(args, "sem_update", False):
        try:
            import sysmon_update
            horas = float(cfg.extra.get("horas_entre_updates", 6))
            sysmon_update.Atualizador(
                __version__, intervalo=horas * 3600 if horas > 0 else 0).iniciar()
        except Exception:  # noqa: BLE001 - update nunca impede o monitor de subir
            pass

    def com_bandeja(janela) -> bool:
        """Icone de bandeja ao lado da janela, se pystray existir. Falha em
        silencio de proposito: a bandeja e um extra, nao deve impedir a janela."""
        try:
            import sysmon_tray
        except ImportError:
            return False
        try:
            icone = sysmon_tray.icone_simples(frota, {
                "mostrar": lambda: janela.pedir("mostrar"),
                "alternar_topo": lambda: janela.pedir("topo"),
                # Leitura de BooleanVar de outra thread e inofensiva, e o menu
                # precisa do valor na hora de desenhar.
                "no_topo": lambda: bool(janela.no_topo.get()),
                "atualizar": lambda: janela.pedir("atualizar"),
                "sair": lambda: janela.pedir("sair"),
            })
            sysmon_tray.acompanhar(icone, frota)
            print("bandeja ativa; fechar a janela nao encerra; use Sair no icone")
            return True
        except Exception as e:  # noqa: BLE001
            print(f"bandeja indisponivel ({e}); seguindo so com a janela.",
                  file=sys.stderr)
            return False

    frota.iniciar()
    oculto = bool(getattr(args, "oculto", False)) and \
        not getattr(args, "nao_oculto", False)
    print(f"sysmon {__version__}   {len(cfg.hosts)} host(s)")
    try:
        sysmon_win.rodar(frota, caminho, intervalo=max(2.0, cfg.intervalo),
                         com_bandeja=com_bandeja, oculto=oculto,
                         ao_criar=lambda j: inst.ligar(lambda: j.pedir("mostrar")))
    finally:
        frota.parar()
        inst.fechar()
    return 0


def _so_bandeja(args) -> int:
    """`sysmon.py tray`: bandeja com o overlay antigo, sem janela nem servidor."""
    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)
    try:
        import sysmon_tray
    except ImportError as e:
        print(f"erro: a bandeja precisa de pystray e Pillow ({e}).\n"
              "  pip install pystray pillow", file=sys.stderr)
        return 1
    frota = Frota(cfg)
    sysmon_tray.preparar(frota, cfg)
    frota.iniciar()
    print(f"sysmon {__version__}   bandeja ativa ({len(cfg.hosts)} host(s))")
    try:
        sysmon_tray.rodar(frota)
    finally:
        frota.parar()
    return 0


if __name__ == "__main__":
    sys.exit(main())
