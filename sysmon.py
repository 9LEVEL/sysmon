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
import os
import re
import socket
import sys
import threading
import time
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

__version__ = "4.3.0"

# Porta de loopback usada so como trava de instancia unica e canal para
# "traga a janela para a frente". Nunca escuta fora de 127.0.0.1.
PORTA_CONTROLE = 9110

# Prazo da caixa de aviso de conflito de versao. Ver _avisar_conflito.
SEGUNDOS_AVISO = 45.0

# Banner sem versao = sysmon 4.2.0 ou anterior. Sabemos de antemao que essas
# nao entendem o pedido de encerrar, entao nem tentamos negociar com elas.
ANTIGA = "anterior"


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
    p.add_argument("--diagnostico", action="store_true",
                   help="imprime o estado da instalacao e sai")

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

    if getattr(args, "diagnostico", False):
        return _diagnostico(args)
    if args.cmd == "local":
        import sysmon_local
        return sysmon_local.main(args) or 0
    if args.cmd == "term":
        import sysmon_dash
        return sysmon_dash.main(args)
    if args.cmd == "tray":
        return _so_bandeja(args)
    return _janela(args)


def _diagnostico(args) -> int:
    """Estado da instalacao, num texto so para colar numa conversa.

    Nasceu de um teste no Windows em que o botao de atualizar "nao aparecia": a
    causa era outra instancia, mais antiga, ja rodando - e nao havia como ver
    isso de fora. Cada linha aqui responde uma pergunta que ja custou tempo.
    """
    linhas = [f"sysmon {__version__}"]
    ap = linhas.append
    ap(f"  python          : {sys.version.split()[0]}  ({sys.executable})")
    ap(f"  argv[0]         : {sys.argv[0]}")
    ap(f"  pasta atual     : {Path.cwd()}")

    try:
        import sysmon_update
        alvo = sysmon_update.alvo()
        ap(f"  bundle (.pyz)   : {alvo or 'nao - rodando do repositorio'}")
        if alvo:
            lanc = sysmon_update.lancador(alvo)
            ap(f"  lancador        : {lanc.name if lanc else 'NENHUM ao lado do .pyz'}")
            ap(f"  modo de troca   : {sysmon_update.como_aplicar(os.name == 'nt', lanc is not None)}")
            ap(f"  pendente        : {'sim' if alvo.with_name('sysmon-novo.pyz').is_file() else 'nao'}")
        ap(f"  botao ⭳         : {'aparece' if alvo else 'NAO (so existe rodando do .pyz)'}"
           + ("" if not getattr(args, "sem_update", False) else "  [--sem-update ligado]"))
    except Exception as e:  # noqa: BLE001
        ap(f"  auto-update     : INDISPONIVEL ({e})")

    try:
        caminho = achar_config(args.config)
        ap(f"  config          : {caminho}")
        try:
            cfg = carregar_config(caminho)
            ap(f"  hosts           : {len(cfg.hosts)}")
        except ErroConfig as e:
            ap(f"  hosts           : config invalido ({e.args[0].splitlines()[0]})")
    except Exception as e:  # noqa: BLE001
        ap(f"  config          : nao encontrado ({e})")

    porta = getattr(args, "porta", None) or PORTA_CONTROLE
    outra = _InstanciaUnica(porta).quem_esta_ai()
    if outra is None:
        ap(f"  porta {porta}     : livre (nenhum sysmon rodando)")
    else:
        alerta = "" if outra == __version__ else "  <== E OUTRA VERSAO"
        ap(f"  porta {porta}     : sysmon {outra} JA RODANDO{alerta}")

    try:
        import tkinter
        ap(f"  tkinter         : {tkinter.TkVersion}")
    except Exception as e:  # noqa: BLE001
        ap(f"  tkinter         : AUSENTE ({e})")
    try:
        import pystray  # noqa: F401
        import PIL      # noqa: F401
        ap("  bandeja         : pystray + pillow ok")
    except Exception:  # noqa: BLE001
        ap("  bandeja         : sem pystray/pillow (opcional; so a janela)")

    print("\n".join(linhas))
    return 0


def _num(versao: str) -> tuple:
    """'4.10.0' -> (4,10,0). Numero, nao texto: '4.10' vem depois de '4.9'."""
    return tuple(int(n) for n in re.findall(r"\d+", versao or "")[:3]) or (0,)


def _mais_novo(a: str, b: str) -> bool:
    return _num(a) > _num(b)


def _ceder_lugar(inst: "_InstanciaUnica", janela) -> None:
    """Uma versao mais nova pediu a vez: solta a porta JA e encerra.

    A ordem importa. Encerrar primeiro e soltar a porta depois foi o que
    quebrou na primeira tentativa: o inst.fechar() so acontece no finally,
    depois do frota.parar(), e parar a frota pode levar segundos quando ha
    host inalcancavel terminando um timeout. A instancia nova desistia de
    esperar, avisava do conflito e saia - e esta aqui, ja a caminho do fim,
    saia tambem. O usuario ficava sem nenhuma das duas.

    Soltando a porta antes, a nova assume em menos de um tique enquanto esta
    termina de desligar no seu proprio ritmo.
    """
    inst.fechar()
    janela.pedir("sair")


def _avisar_conflito(rodando: str) -> None:
    """Conta o que houve, inclusive quando nao ha console para contar.

    Sob pythonw - que e como o atalho do Windows abre o programa - nada do que
    vai para stdout aparece em lugar nenhum. Uma versao nova aberta com a
    antiga rodando ficava, para o usuario, "nao aconteceu nada"; e a janela
    antiga na tela ainda dava a entender que a versao nova nao tinha as
    novidades. Por isso a caixa de dialogo: e o unico canal que sempre existe.
    """
    quem = ("uma versao anterior a 4.2.1" if rodando == ANTIGA
            else f"a versao {rodando}")
    msg = (f"Ja existe um sysmon rodando nesta maquina - {quem} - e voce "
           f"acabou de abrir a {__version__}.\n\n"
           "A janela na tela e a da instancia ANTIGA, entao as novidades desta "
           "versao parecem nao existir.\n\n"
           "Encerre a antiga pelo menu do icone na bandeja (Sair) e abra esta "
           "de novo.")
    print(msg, file=sys.stderr)
    try:
        import tkinter as tk
        from tkinter import messagebox
        raiz = tk.Tk()
        raiz.withdraw()
        # Caixa modal com prazo: isto pode disparar no logon, com a maquina
        # sozinha. Um dialogo esperando clique para sempre seguraria o
        # processo indefinidamente, e ninguem estaria la para ver.
        raiz.after(int(SEGUNDOS_AVISO * 1000), raiz.destroy)
        messagebox.showwarning("sysmon", msg)
        raiz.destroy()
    except Exception:  # noqa: BLE001 - sem Tk, o texto no console ja foi
        pass


class _InstanciaUnica:
    """Trava de instancia unica por socket de loopback.

    Serve tambem de IPC minimo para "traga a janela para a frente" quando o
    sysmon e aberto uma segunda vez - no lugar da antiga deteccao pela porta do
    servidor web. Um pequeno banner na conexao distingue a nossa instancia de
    outro programa que por acaso ocupe a porta.

    O banner carrega a VERSAO ("sysmon 4.2.1") porque so "esta ocupado" nao
    basta: abrir a versao nova com a antiga rodando trazia a janela ANTIGA para
    a frente e saia calado, entao a novidade parecia nao existir. Versoes ate a
    4.2.0 mandam so "sysmon", e sao reconhecidas como versao desconhecida.
    """

    BANNER = b"sysmon"

    def __init__(self, porta: int) -> None:
        self.porta = porta
        self._srv: socket.socket | None = None
        self._mostrar = None
        self._sair = None

    def adquirir(self) -> bool:
        """Tenta segurar a porta. False = ja existe outra instancia."""
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        if os.name != "nt":
            # Rede de seguranca para o caso de sobrar um TIME_WAIT nesta porta
            # (instancia morta a forca no meio de uma conexao): sem isto o
            # bind falha por ate um minuto e o programa acusa "porta ocupada
            # por outro programa", que e falso.
            #
            # So no Unix. No Windows o SO_REUSEADDR permite roubar uma porta
            # que outro processo esta ESCUTANDO, e ai a trava de instancia
            # unica - a razao de este socket existir - deixaria de valer.
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
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
            srv = self._srv
            if srv is None:
                return          # fechar() enquanto cediamos o lugar
            try:
                conn, _ = srv.accept()
            except OSError:
                return
            pedido = b""
            with conn:
                try:
                    conn.sendall(f"sysmon {__version__}\n".encode())
                    pedido = conn.recv(16)
                    # Deixa o CLIENTE fechar primeiro. Quem fecha antes fica
                    # com o TIME_WAIT, e um TIME_WAIT nesta porta impede o
                    # proximo bind por perto de um minuto - foi o que fez a
                    # instancia nova desistir de assumir o lugar da antiga,
                    # mesmo com a antiga ja tendo saido da escuta. Fechando
                    # depois, o TIME_WAIT nasce na porta efemera do cliente,
                    # onde nao atrapalha ninguem.
                    conn.settimeout(2.0)
                    while conn.recv(16):
                        pass
                except OSError:
                    pass
            acao = {b"mostrar": self._mostrar, b"sair": self._sair}.get(
                pedido.strip())
            if acao:
                try:
                    acao()
                except Exception:  # noqa: BLE001 - IPC nao pode derrubar a janela
                    pass

    def _conversar(self, comando: bytes | None) -> str | None:
        """Fala com quem esta na porta. Devolve a versao dela, ou None.

        None quer dizer "nao e um sysmon" - a porta e de outro programa, e ai
        trocar de porta e o unico caminho.
        """
        try:
            with socket.create_connection(("127.0.0.1", self.porta), timeout=2) as c:
                banner = c.recv(32)
                if not banner.startswith(self.BANNER):
                    return None
                if comando:
                    c.sendall(comando)
        except OSError:
            return None
        partes = banner.decode("utf-8", "replace").split()
        return partes[1] if len(partes) > 1 else ANTIGA

    def quem_esta_ai(self) -> str | None:
        return self._conversar(None)

    def pedir_para_aparecer(self) -> bool:
        return self._conversar(b"mostrar") is not None

    def pedir_para_sair(self) -> bool:
        """Pede a instancia em curso que encerre, para assumirmos o lugar.

        Versoes ate a 4.2.0 nao entendem este pedido e simplesmente ignoram;
        quem decide se funcionou e o esperar_livre(), nao esta resposta.
        """
        return self._conversar(b"sair") is not None

    def esperar_livre(self, limite: float = 8.0) -> bool:
        """Tenta tomar a porta ate o limite. False = o outro nao saiu."""
        fim = time.monotonic() + limite
        while time.monotonic() < fim:
            if self.adquirir():
                return True
            time.sleep(0.25)
        return False

    def ligar(self, mostrar, sair=None) -> None:
        self._mostrar = mostrar
        self._sair = sair

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
        rodando = inst.quem_esta_ai()
        if rodando is None:
            print(f"erro: a porta de controle {inst.porta} esta ocupada por outro "
                  "programa.", file=sys.stderr)
            print("Use --porta para escolher outra.", file=sys.stderr)
            return 1
        if rodando == __version__:
            inst.pedir_para_aparecer()
            print(f"sysmon {rodando} ja esta rodando; trouxe a janela para a frente.")
            return 0
        # Versoes diferentes. Antes este caso caia no ramo de cima e a janela
        # ANTIGA vinha para a frente sem avisar - quem tinha acabado de abrir a
        # versao nova concluia, com razao, que ela nao tinha as novidades.
        #
        # Com a versao conhecida e menor que a nossa, pedimos o lugar. Com o
        # banner antigo nem tentamos: aquelas versoes ignoram o pedido, e
        # insistir seria so oito segundos de espera antes do mesmo aviso.
        pode_assumir = rodando != ANTIGA and _mais_novo(__version__, rodando)
        if pode_assumir and inst.pedir_para_sair() and inst.esperar_livre():
            print(f"a instancia {rodando} encerrou; assumindo com a {__version__}.")
        else:
            _avisar_conflito(rodando)
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
                         atualizador=atualizador,
                         ao_criar=lambda j: inst.ligar(
                             lambda: j.pedir("mostrar"),
                             sair=lambda: _ceder_lugar(inst, j)))
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
