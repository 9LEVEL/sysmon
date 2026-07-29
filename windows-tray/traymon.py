#!/usr/bin/env python3
"""
traymon.py - tray icon + overlay no Windows mostrando a saude do host Proxmox.

Consome o JSON de sysmon_agent.py rodando no PVE.

Requisitos (no Windows, nao no PVE):
    pip install -r requirements.txt

Configuracao: config.json na mesma pasta deste arquivo.
As variaveis de ambiente SYSMON_URL / SYSMON_TOKEN, se existirem, tem prioridade.

Arquitetura de threads:
    - thread principal : loop do tkinter (overlay + aplicacao de comandos)
    - thread poller    : busca o JSON a cada N segundos
    - thread tray      : pystray (no Windows o message loop pode ser off-main)
"""

from __future__ import annotations

import ctypes
import json
import logging
import os
import queue
import sys
import threading
import time
import tkinter as tk
import traceback
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont
import pystray

__version__ = "1.1.0"

BASE = Path(sys.argv[0]).resolve().parent
CFG_PATH = BASE / "config.json"
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


# ------------------------------------------------------------------- config
def carregar_config() -> dict:
    try:
        cfg = json.loads(CFG_PATH.read_text(encoding="utf-8"))
    except FileNotFoundError:
        caixa(f"config.json nao encontrado em:\n{CFG_PATH}\n\n"
              "Copie config.example.json para config.json e preencha.")
        sys.exit(1)
    except json.JSONDecodeError as e:
        caixa(f"config.json invalido:\n\n{e}")
        sys.exit(1)

    cfg["url"] = os.environ.get("SYSMON_URL") or cfg.get("url", "")
    cfg["token"] = os.environ.get("SYSMON_TOKEN") or cfg.get("token", "")
    if not cfg["url"] or not cfg["token"]:
        caixa("Preencha 'url' e 'token' no config.json.")
        sys.exit(1)
    return cfg


CFG = carregar_config()
URL: str = CFG["url"]
TOKEN: str = CFG["token"]
INTERVALO = float(CFG.get("intervalo", 5))
TIMEOUT = float(CFG.get("timeout", 4))
OVERLAY_INICIAL = bool(CFG.get("overlay_ao_iniciar", True))
POSICAO = CFG.get("posicao", [40, 40])

# Fracao do valor critico do sensor a partir da qual pinta amarelo / vermelho.
FRAC_AVISO = float(CFG.get("frac_aviso", 0.75))
FRAC_CRITICO = float(CFG.get("frac_critico", 0.90))
# Usados quando o sensor nao reporta 'crit'.
FALLBACK_AVISO = float(CFG.get("aviso_c", 70))
FALLBACK_CRITICO = float(CFG.get("critico_c", 85))
# Thin pool LVM: acima disso o Proxmox comeca a falhar snapshots.
THIN_AVISO, THIN_CRITICO = 80.0, 90.0

VERDE = (80, 200, 120)
AMARELO = (230, 180, 60)
VERMELHO = (225, 80, 80)
CINZA = (140, 140, 140)


# ------------------------------------------------------------------- estado
@dataclass
class Estado:
    dados: dict | None = None
    erro: str | None = None
    atualizado: float = 0.0
    lock: threading.Lock = field(default_factory=threading.Lock)

    def set(self, dados: dict | None, erro: str | None) -> None:
        with self.lock:
            self.dados, self.erro, self.atualizado = dados, erro, time.time()

    def get(self) -> tuple[dict | None, str | None]:
        with self.lock:
            return self.dados, self.erro


estado = Estado()
pedidos: queue.Queue[str] = queue.Queue()  # tray -> thread do tkinter


# ------------------------------------------------------------------- coleta
def buscar() -> None:
    req = urllib.request.Request(URL, headers={"Authorization": f"Bearer {TOKEN}"})
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
            estado.set(json.loads(r.read().decode()), None)
    except urllib.error.HTTPError as e:
        estado.set(None, "token invalido" if e.code == 401 else f"HTTP {e.code}")
    except urllib.error.URLError as e:
        estado.set(None, f"sem conexao ({e.reason})")
    except (json.JSONDecodeError, TimeoutError, OSError) as e:
        estado.set(None, type(e).__name__)


def poller() -> None:
    while True:
        try:
            buscar()
        except Exception:
            logging.exception("falha inesperada no poller")
        time.sleep(INTERVALO)


# ------------------------------------------------------------------ formato
def cor_temp(c: float | None, crit: float | None = None) -> tuple[int, int, int]:
    """Limiares derivados do critico real do sensor, com fallback fixo."""
    if c is None:
        return CINZA
    if crit:
        aviso, critico = crit * FRAC_AVISO, crit * FRAC_CRITICO
    else:
        aviso, critico = FALLBACK_AVISO, FALLBACK_CRITICO
    if c >= critico:
        return VERMELHO
    if c >= aviso:
        return AMARELO
    return VERDE


def cor_percent(p: float | None, aviso: float, critico: float) -> tuple[int, int, int]:
    if p is None:
        return CINZA
    return VERMELHO if p >= critico else AMARELO if p >= aviso else VERDE


def fmt_uptime(s: int) -> str:
    d, resto = divmod(s, 86400)
    h, m = divmod(resto // 60, 60)
    return f"{d}d {h}h {m}m" if d else f"{h}h {m}m"


def fmt_bytes(n: float) -> str:
    for unidade in ("B", "K", "M", "G", "T"):
        if n < 1024:
            return f"{n:.0f}{unidade}"
        n /= 1024
    return f"{n:.1f}P"


def resumo() -> list[str]:
    dados, erro = estado.get()
    if erro or not dados:
        return [f"Offline - {erro or 'sem dados'}"]

    l = [str(dados.get("host", "?"))]
    t, crit = dados.get("cpu_temp"), dados.get("cpu_crit")
    l.append(f"CPU  {t:.0f}C" + (f" / {crit:.0f}C" if crit else "")
             if t is not None else "CPU  --")
    if dados.get("cpu_percent") is not None:
        l.append(f"Uso  {dados['cpu_percent']:.0f}%  ({dados.get('cpus', '?')} cores)")
    if load := dados.get("load"):
        l.append(f"Load {load[0]:.2f} {load[1]:.2f} {load[2]:.2f}")

    mem = dados.get("mem") or {}
    if mem.get("percent") is not None:
        l.append(f"RAM  {mem['percent']:.0f}%  {fmt_bytes(mem['usado'])}"
                 f"/{fmt_bytes(mem['total'])}")
    if mem.get("swap_percent"):
        l.append(f"Swap {mem['swap_percent']:.0f}%  {fmt_bytes(mem['swap_usado'])}")

    for d in dados.get("discos", [])[:3]:
        l.append(f"{d['mount']:<12} {d['percent']:.0f}%")
    for tp in dados.get("thinpools", []):
        l.append(f"{tp['nome']:<12} {tp['data_percent']:.0f}%"
                 f" (meta {tp['meta_percent']:.0f}%)")

    if fans := dados.get("fans"):
        l.append("Fans " + " ".join(f"{v}" for v in list(fans.values())[:3]) + " rpm")
    if g := dados.get("guests"):
        l.append(f"VMs {g['qemu']}   CTs {g['lxc']}")
    l.append(f"Up   {fmt_uptime(dados.get('uptime_s', 0))}")
    return l


def alertas() -> list[str]:
    """Condicoes que merecem atencao agora."""
    dados, erro = estado.get()
    if erro or not dados:
        return []
    out = []
    t, crit = dados.get("cpu_temp"), dados.get("cpu_crit")
    if t is not None and cor_temp(t, crit) == VERMELHO:
        out.append(f"CPU em {t:.0f}C")
    for d in dados.get("discos", []):
        if d["percent"] >= 90:
            out.append(f"Disco {d['mount']} em {d['percent']:.0f}%")
    for tp in dados.get("thinpools", []):
        if tp["data_percent"] >= THIN_AVISO:
            out.append(f"Thin pool {tp['nome']} em {tp['data_percent']:.0f}%")
    return out


# ------------------------------------------------------------------ icone
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


def desenhar_icone() -> Image.Image:
    """Icone 64x64 com a temperatura em numero, colorido pelo limiar."""
    dados, erro = estado.get()
    temp = (dados or {}).get("cpu_temp") if not erro else None
    crit = (dados or {}).get("cpu_crit") if not erro else None

    img = Image.new("RGBA", (64, 64), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    cor = cor_temp(temp, crit)
    d.rounded_rectangle([0, 0, 63, 63], radius=14, fill=cor + (235,))

    texto = "--" if temp is None else f"{temp:.0f}"
    f = fonte(40 if len(texto) <= 2 else 32)
    cx = d.textbbox((0, 0), texto, font=f)
    d.text(((64 - cx[2] + cx[0]) / 2, (64 - cx[3] + cx[1]) / 2 - 2),
           texto, font=f, fill=(20, 20, 20, 255))

    if alertas():  # ponto de alerta no canto
        d.ellipse([46, 2, 62, 18], fill=(200, 30, 30, 255),
                  outline=(255, 255, 255, 220), width=2)
    return img


# ------------------------------------------------------------------ overlay
class Overlay:
    """Janela sempre no topo, sem bordas, arrastavel, opcionalmente click-through."""

    def __init__(self, root: tk.Tk) -> None:
        self.root = root
        self.win: tk.Toplevel | None = None
        self.label: tk.Label | None = None
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
        w.attributes("-alpha", float(CFG.get("opacidade", 0.86)))
        w.configure(bg="#12141a")
        w.geometry(f"+{POSICAO[0]}+{POSICAO[1]}")

        self.label = tk.Label(
            w, justify="left", anchor="w",
            font=("Consolas", int(CFG.get("fonte_tamanho", 11))),
            bg="#12141a", fg="#e6e6e6", padx=12, pady=8,
        )
        self.label.pack()

        for widget in (w, self.label):
            widget.bind("<Button-1>", self._inicio_drag)
            widget.bind("<B1-Motion>", self._arrastar)
            widget.bind("<Double-Button-1>", lambda e: pedidos.put("overlay"))

        self.win = w
        if self.click_through:
            self.click_through = False
            self.alternar_click_through()
        self.atualizar()

    def fechar(self) -> None:
        if self.win:
            POSICAO[:] = [self.win.winfo_x(), self.win.winfo_y()]
            self.win.destroy()
            self.win = self.label = None

    def alternar(self) -> None:
        self.fechar() if self.win else self.abrir()

    def _inicio_drag(self, e) -> None:
        self._drag = (e.x_root - self.win.winfo_x(), e.y_root - self.win.winfo_y())

    def _arrastar(self, e) -> None:
        self.win.geometry(f"+{e.x_root - self._drag[0]}+{e.y_root - self._drag[1]}")

    def alternar_click_through(self) -> None:
        """WS_EX_TRANSPARENT faz os cliques atravessarem a janela (Windows)."""
        if not self.win:
            return
        GWL_EXSTYLE, WS_EX_TRANSPARENT, WS_EX_LAYERED = -20, 0x20, 0x80000
        user32 = ctypes.windll.user32
        hwnd = user32.GetParent(self.win.winfo_id()) or self.win.winfo_id()
        estilo = user32.GetWindowLongW(hwnd, GWL_EXSTYLE)
        self.click_through = not self.click_through
        if self.click_through:
            estilo |= WS_EX_TRANSPARENT | WS_EX_LAYERED
        else:
            estilo &= ~WS_EX_TRANSPARENT
        user32.SetWindowLongW(hwnd, GWL_EXSTYLE, estilo)

    def atualizar(self) -> None:
        if not self.label:
            return
        dados, erro = estado.get()
        temp = (dados or {}).get("cpu_temp") if not erro else None
        crit = (dados or {}).get("cpu_crit") if not erro else None
        cor = "#e15050" if erro else "#%02x%02x%02x" % cor_temp(temp, crit)
        linhas = resumo()
        if av := alertas():
            linhas += ["", "! " + "; ".join(av)]
        self.label.configure(text="\n".join(linhas), fg=cor)


# ------------------------------------------------------------------ tray
def montar_tray(overlay: Overlay) -> pystray.Icon:
    def enfileirar(nome: str):
        return lambda icon, item: pedidos.put(nome)

    def atualizar_agora(icon, item):
        threading.Thread(target=buscar, daemon=True).start()

    def copiar_json(icon, item):
        dados, _ = estado.get()
        if dados:
            pedidos.put("copiar")

    menu = pystray.Menu(
        pystray.MenuItem(lambda item: resumo()[0], lambda i, it: None, enabled=False),
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Mostrar overlay", enfileirar("overlay"),
                         checked=lambda item: overlay.visivel, default=True),
        pystray.MenuItem("Cliques atravessam", enfileirar("clickthrough"),
                         checked=lambda item: overlay.click_through),
        pystray.MenuItem("Atualizar agora", atualizar_agora),
        pystray.MenuItem("Copiar JSON", copiar_json),
        pystray.Menu.SEPARATOR,
        pystray.MenuItem("Sair", enfileirar("sair")),
    )
    return pystray.Icon("sysmon", desenhar_icone(), "sysmon", menu)


def loop_ui(root: tk.Tk, overlay: Overlay, icone: pystray.Icon) -> None:
    """Roda na thread principal: aplica comandos do tray e redesenha."""
    while not pedidos.empty():
        cmd = pedidos.get()
        if cmd == "overlay":
            overlay.alternar()
        elif cmd == "clickthrough":
            overlay.alternar_click_through()
        elif cmd == "copiar":
            dados, _ = estado.get()
            root.clipboard_clear()
            root.clipboard_append(json.dumps(dados, indent=2, ensure_ascii=False))
        elif cmd == "sair":
            overlay.fechar()
            icone.stop()
            root.quit()
            return

    overlay.atualizar()
    try:
        icone.icon = desenhar_icone()
        icone.title = "\n".join(resumo()[:6])  # tooltip do Windows trunca em ~127 ch
    except Exception:
        logging.exception("falha ao redesenhar o icone")
    root.after(1000, loop_ui, root, overlay, icone)


def main() -> None:
    logging.info("iniciando traymon %s -> %s", __version__, URL)

    threading.Thread(target=poller, daemon=True).start()
    time.sleep(0.8)  # da tempo do primeiro fetch antes de desenhar o icone

    root = tk.Tk()
    root.withdraw()  # a janela raiz nunca aparece
    overlay = Overlay(root)
    if OVERLAY_INICIAL:
        overlay.abrir()

    icone = montar_tray(overlay)
    threading.Thread(target=icone.run, daemon=True).start()

    root.after(500, loop_ui, root, overlay, icone)
    root.mainloop()
    logging.info("encerrado")


if __name__ == "__main__":
    main()
