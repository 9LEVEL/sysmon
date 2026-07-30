#!/usr/bin/env python3
"""
sysmon_tray - icone de bandeja + overlay, para varios hosts Linux.

O icone mostra a temperatura do host mais quente, colorido pelo pior host da
frota; o overlay lista todos. Quem sobe isto e o sysmon.py, que roda a bandeja
e o dashboard web no mesmo processo.

Requisitos (na maquina que olha, nao nos hosts monitorados):
    pip install pystray pillow

Sem esses dois o import falha, e o sysmon.py segue so com o dashboard web em
vez de nao subir - a bandeja e um extra, nao o caminho principal.

Arquitetura de threads:
    - principal : loop do tkinter (overlay + aplicacao dos comandos do menu)
    - N pollers : um por host, com recuo exponencial (ficam no sysmon_nucleo)
    - tray      : pystray (no Windows o message loop pode ficar fora da main)

Nada que venha das threads de rede toca no tkinter direto: tudo passa pela
fila `pedidos`, porque o Tk nao e thread-safe.
"""

from __future__ import annotations

import ctypes
import json
import logging
import os
import queue
import sys
import threading
import tkinter as tk
import traceback
import urllib.request
import webbrowser
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont
import pystray

__version__ = "2.6.0"

BASE = Path(__file__).resolve().parent
LOG_PATH = Path(os.environ.get("TEMP", BASE)) / "traymon.log"

logging.basicConfig(
    filename=LOG_PATH, level=logging.INFO, encoding="utf-8",
    format="%(asctime)s %(levelname)s %(message)s",
)


def caixa(msg: str, titulo: str = "traymon", icone: int = 0x10) -> None:
    """MessageBox nativa: unica forma de comunicar erro sob pythonw.exe."""
    try:
        ctypes.windll.user32.MessageBoxW(0, msg, titulo, icone)
    except Exception:
        print(msg, file=sys.stderr)


def _fatal(tipo, valor, tb) -> None:
    msg = "".join(traceback.format_exception(tipo, valor, tb))
    logging.error(msg)
    caixa(msg[-800:], "traymon - erro nao tratado")


sys.excepthook = _fatal



from sysmon_nucleo import (
    AVISO, CRITICO, OFFLINE, OK,
    Estado, Frota,
    avaliar, como_dict, fmt_pct, fmt_temp, primeira_temp, resumo_linhas,
)

# ------------------------------------------------------------------- config
# Preenchidos em preparar(); ficam globais porque o Overlay e o menu leem em
# varios pontos e sao valores fixos durante toda a execucao.
CFG = None
BRUTO: dict = {}
POSICAO = [40, 40]
OVERLAY_INICIAL = True
COMPACTO_INICIAL = True
NOTIFICAR = True
PORTA_WEB = 9110

# Cores do icone, por nivel de severidade.
CORES = {
    OK: (80, 200, 120),
    AVISO: (230, 180, 60),
    CRITICO: (225, 80, 80),
    OFFLINE: (140, 140, 140),
}
# Mesmas cores em hex, para o overlay.
CORES_HEX = {n: "#%02x%02x%02x" % c for n, c in CORES.items()}

FUNDO = "#12141a"

pedidos: queue.Queue[tuple[str, object]] = queue.Queue()


# ------------------------------------------------------------------- icone
_fonte_cache: dict[int, object] = {}


def fonte(tam: int):
    if tam not in _fonte_cache:
        for nome in ("segoeuib.ttf", "arialbd.ttf", "DejaVuSans-Bold.ttf"):
            try:
                _fonte_cache[tam] = ImageFont.truetype(nome, tam)
                break
            except OSError:
                continue
        else:
            _fonte_cache[tam] = ImageFont.load_default()
    return _fonte_cache[tam]


def desenhar_icone(frota: Frota) -> Image.Image:
    """Icone 64x64: temperatura do host mais quente, cor do pior host da frota.

    Com N hosts um numero so nao conta a historia toda - por isso o ponto de
    alerta no canto, que acende para qualquer host com problema ou offline.
    """
    nivel = frota.pior_nivel()
    temp, _ = primeira_temp(frota.estados())

    img = Image.new("RGBA", (64, 64), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, 63, 63], radius=14, fill=CORES[nivel] + (235,))

    texto = "--" if temp is None else f"{temp:.0f}"
    f = fonte(40 if len(texto) <= 2 else 32)
    cx = d.textbbox((0, 0), texto, font=f)
    d.text(((64 - cx[2] + cx[0]) / 2, (64 - cx[3] + cx[1]) / 2 - 2),
           texto, font=f, fill=(20, 20, 20, 255))

    if nivel >= AVISO:
        d.ellipse([46, 2, 62, 18], fill=(200, 30, 30, 255),
                  outline=(255, 255, 255, 220), width=2)
    return img


def linha_compacta(host, estado: Estado) -> str:
    """Uma linha por host: e o que cabe na tela quando sao muitos."""
    if estado.erro or not estado.dados:
        return f"{host.nome:<10} offline"
    d = estado.dados
    mem = (d.get("mem") or {}).get("percent")
    discos = d.get("discos") or []
    disco = f"{discos[0]['mount']} {discos[0]['percent']:.0f}%" if discos else "--"
    return (f"{host.nome:<10} {fmt_temp(d.get('cpu_temp')):>5}"
            f" {fmt_pct(d.get('cpu_percent')):>5}"
            f"  RAM {fmt_pct(mem):>4}  {disco}")


def titulo_tray(frota: Frota) -> str:
    """Tooltip do Windows - truncado em ~127 caracteres pelo proprio sistema."""
    linhas = [linha_compacta(h, e) for h, e in frota.estados()]
    return "\n".join(linhas[:5])


# ------------------------------------------------------------------ overlay
class Overlay:
    """Janela sempre no topo, sem bordas, arrastavel, opcionalmente click-through.

    Usa um tk.Text em vez de Label porque cada host precisa da propria cor -
    com Label so daria para pintar o bloco inteiro de uma cor so.
    """

    def __init__(self, root: tk.Tk, frota: Frota) -> None:
        self.root = root
        self.frota = frota
        self.win: tk.Toplevel | None = None
        self.texto: tk.Text | None = None
        self.compacto = COMPACTO_INICIAL
        self.click_through = False
        self._drag = (0, 0)

    @property
    def visivel(self) -> bool:
        return self.win is not None

    def abrir(self) -> None:
        if self.win:
            return
        w = tk.Toplevel(self.root)
        w.overrideredirect(True)
        w.attributes("-topmost", True)
        w.attributes("-alpha", float(BRUTO.get("opacidade", 0.86)))
        w.configure(bg=FUNDO)
        w.geometry(f"+{POSICAO[0]}+{POSICAO[1]}")

        self.texto = tk.Text(
            w, font=("Consolas", int(BRUTO.get("fonte_tamanho", 10))),
            bg=FUNDO, fg="#e6e6e6", bd=0, highlightthickness=0,
            padx=12, pady=8, wrap="none", cursor="arrow",
            width=10, height=1,
        )
        for nivel, cor in CORES_HEX.items():
            self.texto.tag_configure(f"n{nivel}", foreground=cor)
        self.texto.tag_configure("alerta", foreground="#ff9a9a")
        self.texto.pack()

        for widget in (w, self.texto):
            widget.bind("<Button-1>", self._inicio_drag)
            widget.bind("<B1-Motion>", self._arrastar)
            widget.bind("<Double-Button-1>", lambda e: pedidos.put(("compacto", None)))
            widget.bind("<Button-3>", lambda e: pedidos.put(("overlay", None)))

        self.win = w
        if self.click_through:
            self.click_through = False
            self.alternar_click_through()
        self.atualizar()

    def fechar(self) -> None:
        if self.win:
            POSICAO[:] = [self.win.winfo_x(), self.win.winfo_y()]
            self.win.destroy()
            self.win = self.texto = None

    def alternar(self) -> None:
        self.fechar() if self.win else self.abrir()

    def alternar_compacto(self) -> None:
        self.compacto = not self.compacto
        self.atualizar()

    def _inicio_drag(self, e) -> None:
        if self.win:
            self._drag = (e.x_root - self.win.winfo_x(), e.y_root - self.win.winfo_y())

    def _arrastar(self, e) -> None:
        if self.win:
            self.win.geometry(f"+{e.x_root - self._drag[0]}+{e.y_root - self._drag[1]}")

    def alternar_click_through(self) -> None:
        """WS_EX_TRANSPARENT faz os cliques atravessarem a janela (so Windows)."""
        if not self.win:
            return
        GWL_EXSTYLE, WS_EX_TRANSPARENT, WS_EX_LAYERED = -20, 0x20, 0x80000
        try:
            user32 = ctypes.windll.user32
        except AttributeError:  # rodando fora do Windows
            return
        hwnd = user32.GetParent(self.win.winfo_id()) or self.win.winfo_id()
        estilo = user32.GetWindowLongW(hwnd, GWL_EXSTYLE)
        self.click_through = not self.click_through
        if self.click_through:
            estilo |= WS_EX_TRANSPARENT | WS_EX_LAYERED
        else:
            estilo &= ~WS_EX_TRANSPARENT
        user32.SetWindowLongW(hwnd, GWL_EXSTYLE, estilo)

    def _conteudo(self) -> list[tuple[str, str]]:
        """(linha, tag) para cada linha da janela."""
        out: list[tuple[str, str]] = []
        for host, estado in self.frota.estados():
            nivel, _ = avaliar(estado)
            tag = f"n{nivel}"
            if self.compacto:
                out.append((linha_compacta(host, estado), tag))
            else:
                out.extend((linha, tag) for linha in resumo_linhas(host, estado))
                out.append(("", tag))
        while out and out[-1][0] == "":
            out.pop()

        if alertas := self.frota.alertas():
            out.append(("", "alerta"))
            out.extend(("! " + a, "alerta") for a in alertas)
        return out

    def atualizar(self) -> None:
        if not self.texto:
            return
        linhas = self._conteudo()
        self.texto.configure(state="normal")
        self.texto.delete("1.0", "end")
        for i, (linha, tag) in enumerate(linhas):
            self.texto.insert("end", linha + ("\n" if i < len(linhas) - 1 else ""), tag)
        # A janela acompanha o conteudo: sem isso a caixa fica com sobra ou corta.
        self.texto.configure(
            state="disabled",
            width=max((len(l) for l, _ in linhas), default=10) + 1,
            height=max(len(linhas), 1),
        )


# ------------------------------------------------------------------ tray
def montar_tray(frota: Frota, overlay: Overlay) -> pystray.Icon:
    def enfileirar(nome: str, arg=None):
        return lambda icon, item: pedidos.put((nome, arg))

    def cabecalho(item) -> str:
        alertas = len(frota.alertas())
        base = f"{len(frota.cfg.hosts)} host(s)"
        return base if not alertas else f"{base} - {alertas} alerta(s)"

    def submenu_host(indice: int):
        def linhas(item):
            host, estado = frota.estados()[indice]
            return "\n".join(resumo_linhas(host, estado))
        return pystray.Menu(
            pystray.MenuItem(linhas, lambda i, it: None, enabled=False),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("Atualizar agora", enfileirar("atualizar_host", indice)),
            pystray.MenuItem("Copiar JSON deste host", enfileirar("copiar_host", indice)),
        )

    itens = [
        pystray.MenuItem(cabecalho, lambda i, it: None, enabled=False),
        pystray.Menu.SEPARATOR,
    ]
    for i, host in enumerate(frota.cfg.hosts):
        itens.append(pystray.MenuItem(host.nome, submenu_host(i)))

    itens += [
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Mostrar overlay", enfileirar("overlay"),
                         checked=lambda item: overlay.visivel, default=True),
        pystray.MenuItem("Overlay compacto", enfileirar("compacto"),
                         checked=lambda item: overlay.compacto),
        pystray.MenuItem("Cliques atravessam", enfileirar("clickthrough"),
                         checked=lambda item: overlay.click_through),
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Abrir dashboard", enfileirar("dashboard")),
        pystray.MenuItem("Atualizar todos", enfileirar("atualizar")),
        pystray.MenuItem("Copiar JSON da frota", enfileirar("copiar")),
        pystray.MenuItem("Sair", enfileirar("sair")),
    ]
    return pystray.Icon("sysmon", desenhar_icone(frota), "sysmon", pystray.Menu(*itens))


def loop_ui(root: tk.Tk, frota: Frota, overlay: Overlay, icone: pystray.Icon) -> None:
    """Roda na thread principal: aplica os comandos do menu e redesenha."""
    while not pedidos.empty():
        cmd, arg = pedidos.get()
        if cmd == "overlay":
            overlay.alternar()
        elif cmd == "compacto":
            overlay.alternar_compacto()
        elif cmd == "clickthrough":
            overlay.alternar_click_through()
        elif cmd == "dashboard":
            threading.Thread(target=abrir_dashboard, daemon=True).start()
        elif cmd == "atualizar":
            frota.atualizar_agora()
        elif cmd == "atualizar_host":
            frota.monitores[arg].atualizar_agora()
        elif cmd == "copiar":
            copiar(root, como_dict(frota))
        elif cmd == "copiar_host":
            _, estado = frota.estados()[arg]
            copiar(root, estado.dados or {"erro": estado.erro})
        elif cmd == "notificar":
            notificar(icone, arg)
        elif cmd == "sair":
            overlay.fechar()
            icone.stop()
            root.quit()
            return

    overlay.atualizar()
    try:
        icone.icon = desenhar_icone(frota)
        icone.title = titulo_tray(frota)
    except Exception:
        logging.exception("falha ao redesenhar o icone")
    root.after(1000, loop_ui, root, frota, overlay, icone)


def copiar(root: tk.Tk, dados: dict) -> None:
    root.clipboard_clear()
    root.clipboard_append(json.dumps(dados, indent=2, ensure_ascii=False))


def abrir_dashboard() -> None:
    """Abre o dashboard no browser.

    O servidor sobe junto com a bandeja no mesmo processo (sysmon.py), entao
    aqui e so apontar o browser. Se alguem rodou `sysmon.py tray` sozinho, o
    endereco nao responde e a mensagem explica o que fazer.
    """
    url = f"http://127.0.0.1:{PORTA_WEB}/"
    try:
        with urllib.request.urlopen(url + "api/frota", timeout=1):
            pass
    except Exception:  # noqa: BLE001
        caixa("O dashboard web nao esta no ar.\n\n"
              "Voce iniciou so a bandeja. Rode `python sysmon.py` (sem "
              "subcomando) para subir os dois juntos.", icone=0x40)
        return
    webbrowser.open(url)


def notificar(icone: pystray.Icon, texto: str) -> None:
    """Balao do Windows. Nem todo backend do pystray suporta; falhar e ok."""
    try:
        icone.notify(texto[:250], "sysmon")
    except Exception:
        logging.info("notificacao nao suportada: %s", texto)


def icone_simples(frota: Frota, acoes: dict):
    """Icone de bandeja sem overlay, para acompanhar a janela nativa.

    Quando existe janela nativa, ela e a interface - o overlay do tkinter seria
    redundante, e as duas coisas nao podem disputar a thread principal. Aqui
    sobra o que a bandeja faz de unico: presenca permanente, cor de status,
    notificacao e um caminho de volta para a janela.
    """
    def cabecalho(item) -> str:
        n = len(frota.cfg.hosts)
        alertas = len(frota.alertas())
        base = f"{n} host{'s' if n != 1 else ''}"
        return base if not alertas else f"{base} - {alertas} alerta(s)"

    menu = pystray.Menu(
        pystray.MenuItem(cabecalho, lambda i, it: None, enabled=False),
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Mostrar janela", lambda i, it: acoes["mostrar"](),
                         default=True),
        pystray.MenuItem("Sempre no topo", lambda i, it: acoes["alternar_topo"](),
                         checked=lambda item: acoes["no_topo"]()),
        pystray.MenuItem("Atualizar agora", lambda i, it: acoes["atualizar"]()),
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Sair", lambda i, it: acoes["sair"]()),
    )
    return pystray.Icon("sysmon", desenhar_icone(frota), "sysmon", menu)


def acompanhar(icone, frota: Frota, notificar_mudanca: bool = True) -> None:
    """Mantem o icone e o tooltip em dia, numa thread propria."""
    import time as _time

    def laco() -> None:
        while True:
            try:
                icone.icon = desenhar_icone(frota)
                icone.title = titulo_tray(frota)
            except Exception:  # noqa: BLE001 - redesenho nunca derruba nada
                logging.exception("falha ao redesenhar o icone")
            _time.sleep(2)

    threading.Thread(target=laco, name="icone", daemon=True).start()
    threading.Thread(target=icone.run, name="tray", daemon=True).start()


def preparar(frota: Frota, cfg):
    """Configura a bandeja a partir do config, sem ainda entrar em nenhum laco.

    Devolve um contexto opaco que o rodar() consome. Separar as duas etapas
    deixa o sysmon.py descobrir cedo se a bandeja e viavel, antes de subir o
    servidor web.
    """
    global CFG, BRUTO, POSICAO, OVERLAY_INICIAL, COMPACTO_INICIAL, NOTIFICAR, PORTA_WEB
    CFG = cfg
    BRUTO = cfg.extra
    POSICAO = list(BRUTO.get("posicao", [40, 40]))
    OVERLAY_INICIAL = bool(BRUTO.get("overlay_ao_iniciar", False))
    COMPACTO_INICIAL = bool(BRUTO.get("overlay_compacto", len(cfg.hosts) > 2))
    NOTIFICAR = bool(BRUTO.get("notificar", True))
    PORTA_WEB = int(BRUTO.get("porta_web", 9110))

    def ao_mudar(nome: str, estado: Estado) -> None:
        """Chamado da thread do poller quando um host muda de nivel."""
        nivel, alertas = avaliar(estado)
        if nivel == OK:
            texto = f"{nome}: normalizado"
        elif nivel == OFFLINE:
            texto = f"{nome}: offline - {estado.erro}"
        else:
            texto = f"{nome}: " + "; ".join(alertas[:3])
        logging.info("mudanca de nivel: %s", texto)
        if NOTIFICAR and nivel != OK:
            pedidos.put(("notificar", texto))

    for m in frota.monitores:
        m.ao_mudar = ao_mudar

    logging.info("bandeja preparada, %d host(s)", len(cfg.hosts))
    return frota


def rodar(frota: Frota) -> None:
    """Laco da bandeja. Precisa da thread principal (tkinter e pystray)."""
    root = tk.Tk()
    root.withdraw()  # a janela raiz nunca aparece
    overlay = Overlay(root, frota)
    if OVERLAY_INICIAL:
        overlay.abrir()

    icone = montar_tray(frota, overlay)
    threading.Thread(target=icone.run, name="tray", daemon=True).start()

    root.after(500, loop_ui, root, frota, overlay, icone)
    try:
        root.mainloop()
    finally:
        logging.info("bandeja encerrada")
