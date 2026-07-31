#!/usr/bin/env python3
"""
sysmon_win - janela nativa, sem borda, em Tkinter.

Painel escuro e monoespacado: nenhum icone, a hierarquia e a severidade saem de
tipografia, alinhamento e cor. Cada host e um bloco; dentro dele, secoes em
maiuscula (DESEMPENHO, TEMPERATURA, DISCOS, ARMAZENAMENTO, REDE) e uma linha
por medida, com barra de proporcao desenhada em texto.

Tkinter de proposito: vem junto com o Python do python.org. Sem pip, sem motor
de navegador, sem componente do sistema para faltar. "Sempre no topo" e uma
chamada de atributo, nao uma ponte entre processos.

A janela nao tem moldura do sistema (overrideredirect): arrasta pelo cabecalho,
redimensiona pelo canto inferior direito, e a barra de titulo e desenhada aqui.
O menu do botao direito devolve a moldura, caso o ambiente nao se de bem com
janela sem decoracao.
"""

from __future__ import annotations

import json
import os
import queue
import tkinter as tk
from pathlib import Path
from tkinter import font as tkfont
from tkinter import ttk

from sysmon_nucleo import (
    AVISO, CRITICO, OFFLINE, OK,
    ErroConfig, Frota, Limiares, avaliar, carregar_config_de, discos_relevantes,
    fmt_bps, fmt_bytes, fmt_pct, fmt_temp, fmt_uptime, salvar_config, testar_host,
)

__version__ = "3.5.0"

# Paleta escura. Os tons de status ficam acima de 4.5:1 no fundo, e o valor
# numerico sempre acompanha - cor reforca, nunca carrega sozinha.
P = {
    "fundo":   "#0b0e14",
    "painel":  "#0f131b",
    "linha":   "#1b2130",
    "texto":   "#c9d1d9",
    "fraco":   "#6b7684",
    "titulo":  "#58a6ff",
    "ok":      "#3fb950",
    "aviso":   "#d29922",
    "critico": "#f85149",
    "selecao": "#161b26",
    # Faixa intermediaria: 50-75% nao e alerta, mas tambem nao e ocioso.
    # Cyan porque ambar e vermelho ja tem significado de alerta e nao podem
    # ser gastos com "esta trabalhando".
    "ativo":   "#39c5cf",
    "ocioso":  "#4a5563",
}
COR_NIVEL = {OK: P["texto"], AVISO: P["aviso"],
             CRITICO: P["critico"], OFFLINE: P["fraco"]}

# Cinco degraus de magnitude, nao dois. Com so ok/aviso/critico, sair de 3%
# para 30% de CPU nao mudava nada na tela - e essa e a variacao que interessa
# no dia a dia. Os dois degraus de cima continuam sendo os limiares de alerta,
# entao ambar e vermelho seguem querendo dizer "olhe para isto".
M_OCIOSO, M_NORMAL, M_ATIVO, M_AVISO, M_CRITICO = range(5)
COR_MAG = {
    M_OCIOSO:  P["ocioso"],
    M_NORMAL:  P["texto"],
    M_ATIVO:   P["ativo"],
    M_AVISO:   P["aviso"],
    M_CRITICO: P["critico"],
}
MAG_OCIOSO, MAG_ATIVO = 20.0, 50.0


def magnitude(pct, aviso: float, critico: float) -> int:
    """Degrau de intensidade de uma medida percentual."""
    if pct is None:
        return M_OCIOSO
    if pct >= critico:
        return M_CRITICO
    if pct >= aviso:
        return M_AVISO
    if pct >= MAG_ATIVO:
        return M_ATIVO
    if pct >= MAG_OCIOSO:
        return M_NORMAL
    return M_OCIOSO


# Sparkline: historico curto em texto. Responde "esta subindo?" - o bar sozinho
# so diz "quanto agora", e era o que faltava para perceber variacao.
NIVEIS = "▁▂▃▄▅▆▇█"


def spark(valores, span_minimo: float = 20.0) -> str:
    """Autoescala COM piso de amplitude.

    Escala fixa 0-100 achataria a variacao que mais interessa: sair de 3% para
    30% de CPU mal sairia do chao. Autoescala pura faria o contrario - ruido de
    3.0 para 3.2 viraria um grafico dramatico.

    Com piso, oscilacao menor que `span_minimo` continua parecendo o que e
    (linha reta), e mudanca de verdade preenche o desenho.
    """
    v = [x for x in valores if x is not None]
    if not v:
        return ""
    base = min(v)
    faixa = max(max(v) - base, span_minimo)
    return "".join(
        NIVEIS[max(0, min(len(NIVEIS) - 1,
                          int((x - base) / faixa * (len(NIVEIS) - 1))))]
        for x in v)

# Barra de proporcao em texto: em fonte monoespacada alinha de graca, e nao
# precisa de widget nem de imagem.
CHEIO, VAZIO = "█", "·"

# Tudo que a janela sabe mostrar. A caixa de "exibir" e montada a partir daqui,
# entao a lista serve tambem de inventario: o usuario ve o que existe mesmo
# quando escolhe esconder. Guardamos as chaves OCULTAS, nao as visiveis - assim
# um campo novo em versao futura aparece por padrao em vez de sumir calado.
CATALOGO = [
    ("RESUMO", "na linha do host", [
        ("r:temp", "temperatura da cpu"),
        ("r:cpu",  "uso de cpu"),
        ("r:ram",  "uso de memoria em %"),
        ("r:gb",   "memoria usada em GB"),
        ("r:so",   "sistema operacional"),
    ]),
    ("DESEMPENHO", None, [
        ("p:cpu",  "uso de cpu"),
        ("p:mem",  "memoria"),
        ("p:swap", "swap"),
        ("p:load", "carga (load average)"),
        ("p:up",   "tempo no ar"),
    ]),
    ("TEMPERATURA", None, [
        ("t:cpu",   "cpu"),
        ("t:todos", "demais sensores do hardware"),
    ]),
    ("VENTOINHAS", None, [("v:todas", "rotacao em rpm")]),
    ("DISCOS", None, [
        ("b:todos", "discos fisicos: modelo, temperatura, desgaste, SMART"),
    ]),
    ("ARMAZENAMENTO", None, [
        ("a:fs",   "filesystems montados"),
        ("a:thin", "thin pool LVM (Proxmox)"),
    ]),
    ("REDE", None, [("n:todas", "interfaces ativas")]),
]


def barra(pct, largura: int = 10) -> str:
    if pct is None:
        return VAZIO * largura
    n = max(0, min(largura, round(pct / 100 * largura)))
    return CHEIO * n + VAZIO * (largura - n)


def caminho_estado() -> Path:
    if os.name == "nt":
        base = Path(os.environ.get("APPDATA") or Path.home() / "AppData/Roaming")
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")
    return base / "sysmon" / "janela-tk.json"


def ler_estado() -> dict:
    try:
        return json.loads(caminho_estado().read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}


def gravar_estado(d: dict) -> None:
    try:
        p = caminho_estado()
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps(d, indent=2), encoding="utf-8")
    except OSError:
        pass


def fonte_mono(tam: int, bold: bool = False):
    """Consolas no Windows; o resto e fallback razoavel por sistema."""
    for nome in ("Consolas", "DejaVu Sans Mono", "Menlo", "Courier New"):
        try:
            f = tkfont.Font(family=nome, size=tam,
                            weight="bold" if bold else "normal")
            if f.actual("family").lower().startswith(nome.split()[0].lower()):
                return f
        except tk.TclError:
            continue
    return tkfont.Font(font="TkFixedFont", size=tam,
                       weight="bold" if bold else "normal")


# ------------------------------------------------------------------ dialogo
class DialogoHosts(tk.Toplevel):
    """Configuracao dos hosts sem sair do programa - nem para isso ha navegador."""

    def __init__(self, pai, cfg_bruto: dict, ao_salvar, mono) -> None:
        super().__init__(pai)
        self.title("sysmon · hosts")
        self.configure(bg=P["fundo"])
        self.transient(pai)
        self.ao_salvar = ao_salvar
        self.mono = mono
        self.linhas: list[dict] = []

        tk.Label(self, text="cada host tem url e token proprios, impressos pelo "
                            "instalador do agente",
                 bg=P["fundo"], fg=P["fraco"], font=mono, wraplength=620,
                 justify="left").pack(anchor="w", padx=14, pady=(14, 10))

        self.quadro = tk.Frame(self, bg=P["fundo"])
        self.quadro.pack(fill="both", expand=True, padx=14)

        for h in cfg_bruto.get("hosts") or []:
            self.adicionar(h)
        if not self.linhas:
            self.adicionar({})

        acoes = tk.Frame(self, bg=P["fundo"])
        acoes.pack(fill="x", padx=14, pady=12)
        self._botao(acoes, "+ ADICIONAR", lambda: self.adicionar({})).pack(side="left")
        self._botao(acoes, "SALVAR", self.salvar, P["ok"]).pack(side="right")
        self._botao(acoes, "CANCELAR", self.destroy).pack(side="right", padx=6)

        self.recado = tk.Label(self, text="", bg=P["fundo"], fg=P["critico"],
                               font=mono, anchor="w")
        self.recado.pack(fill="x", padx=14, pady=(0, 12))

    def _botao(self, pai, texto, comando, cor=None):
        b = tk.Label(pai, text=f" {texto} ", bg=P["painel"], fg=cor or P["texto"],
                     font=self.mono, cursor="hand2", padx=6, pady=3)
        b.bind("<Button-1>", lambda e: comando())
        b.bind("<Enter>", lambda e: b.configure(bg=P["linha"]))
        b.bind("<Leave>", lambda e: b.configure(bg=P["painel"]))
        return b

    def adicionar(self, h: dict) -> None:
        f = tk.Frame(self.quadro, bg=P["painel"], highlightthickness=1,
                     highlightbackground=P["linha"])
        f.pack(fill="x", pady=4)
        f.columnconfigure(1, weight=1)

        campos = {}
        for j, (chave, rotulo) in enumerate((("nome", "APELIDO"), ("url", "URL"),
                                             ("token", "TOKEN"))):
            tk.Label(f, text=rotulo, bg=P["painel"], fg=P["fraco"],
                     font=self.mono, width=8, anchor="w").grid(
                row=j, column=0, sticky="w", padx=(10, 6), pady=3)
            e = tk.Entry(f, bg=P["fundo"], fg=P["texto"], font=self.mono,
                         insertbackground=P["titulo"], relief="flat",
                         highlightthickness=1, highlightbackground=P["linha"],
                         highlightcolor=P["titulo"])
            if h.get(chave):
                e.insert(0, h[chave])
            e.grid(row=j, column=1, sticky="ew", padx=(0, 8), pady=3)
            campos[chave] = e

        estado = tk.Label(f, text="", bg=P["painel"], fg=P["fraco"],
                          font=self.mono, anchor="w")
        estado.grid(row=3, column=0, columnspan=3, sticky="w", padx=10, pady=(2, 8))

        botoes = tk.Frame(f, bg=P["painel"])
        botoes.grid(row=0, column=2, rowspan=2, sticky="ne", padx=8, pady=6)
        self._botao(botoes, "TESTAR",
                    lambda: self.testar(campos, estado)).pack(pady=(0, 4))
        self._botao(botoes, "REMOVER",
                    lambda: self.remover(f, campos), P["critico"]).pack()

        self.linhas.append({"frame": f, "campos": campos})

    def remover(self, frame, campos) -> None:
        frame.destroy()
        self.linhas = [l for l in self.linhas if l["campos"] is not campos]

    def testar(self, campos, estado) -> None:
        estado.configure(text="testando...", fg=P["fraco"])
        estado.update_idletasks()
        ok, msg = testar_host(campos["url"].get().strip(),
                              campos["token"].get().strip())
        estado.configure(text=("OK  " if ok else "FALHOU  ") + msg,
                         fg=P["ok"] if ok else P["critico"])

    def salvar(self) -> None:
        hosts = []
        for l in self.linhas:
            if not l["frame"].winfo_exists():
                continue
            c = l["campos"]
            url = c["url"].get().strip()
            if not url:
                continue
            h = {"url": url}
            if c["nome"].get().strip():
                h["nome"] = c["nome"].get().strip()
            if c["token"].get().strip():
                h["token"] = c["token"].get().strip()
            hosts.append(h)
        if not hosts:
            self.recado.configure(text="preencha ao menos um host")
            return
        try:
            self.ao_salvar(hosts)
        except ErroConfig as e:
            self.recado.configure(text=str(e).splitlines()[0])
            return
        self.destroy()


# ------------------------------------------------------------------ exibir
class DialogoExibir(tk.Toplevel):
    """Escolhe o que aparece. A lista mostra TUDO que existe, marcado ou nao."""

    def __init__(self, pai, oculto: set[str], ao_salvar, mono, mono_b) -> None:
        super().__init__(pai)
        self.title("sysmon · exibir")
        self.configure(bg=P["fundo"])
        self.transient(pai)
        self.ao_salvar = ao_salvar
        self.vars: dict[str, tk.BooleanVar] = {}

        tk.Label(self, text="desmarque o que nao quer ver; a lista mostra tudo "
                            "que a ferramenta coleta",
                 bg=P["fundo"], fg=P["fraco"], font=mono, justify="left",
                 wraplength=520).pack(anchor="w", padx=14, pady=(14, 10))

        corpo = tk.Frame(self, bg=P["fundo"])
        corpo.pack(fill="both", expand=True, padx=14)

        for secao, nota, itens in CATALOGO:
            chave = f"sec:{secao}"
            v = tk.BooleanVar(value=chave not in oculto)
            self.vars[chave] = v
            linha = tk.Frame(corpo, bg=P["fundo"])
            linha.pack(fill="x", pady=(8, 0))
            self._caixa(linha, secao, v, mono_b, P["titulo"]).pack(side="left")
            if nota:
                tk.Label(linha, text=nota, bg=P["fundo"], fg=P["fraco"],
                         font=mono).pack(side="left", padx=6)

            for k, rotulo in itens:
                vi = tk.BooleanVar(value=k not in oculto)
                self.vars[k] = vi
                self._caixa(corpo, rotulo, vi, mono, P["texto"]).pack(
                    anchor="w", padx=(26, 0))

        acoes = tk.Frame(self, bg=P["fundo"])
        acoes.pack(fill="x", padx=14, pady=12)
        self._botao(acoes, "TUDO", lambda: self._todos(True), mono).pack(side="left")
        self._botao(acoes, "NADA", lambda: self._todos(False), mono).pack(
            side="left", padx=6)
        self._botao(acoes, "APLICAR", self.salvar, mono, P["ok"]).pack(side="right")
        self._botao(acoes, "CANCELAR", self.destroy, mono).pack(side="right", padx=6)

    def _caixa(self, pai, texto, var, fonte, cor):
        return tk.Checkbutton(
            pai, text=texto, variable=var, bg=P["fundo"], fg=cor, font=fonte,
            activebackground=P["fundo"], activeforeground=cor,
            selectcolor=P["painel"], highlightthickness=0, borderwidth=0,
            anchor="w", padx=2)

    def _botao(self, pai, texto, comando, fonte, cor=None):
        b = tk.Label(pai, text=f" {texto} ", bg=P["painel"], fg=cor or P["texto"],
                     font=fonte, cursor="hand2", padx=6, pady=3)
        b.bind("<Button-1>", lambda e: comando())
        b.bind("<Enter>", lambda e: b.configure(bg=P["linha"]))
        b.bind("<Leave>", lambda e: b.configure(bg=P["painel"]))
        return b

    def _todos(self, valor: bool) -> None:
        for v in self.vars.values():
            v.set(valor)

    def salvar(self) -> None:
        self.ao_salvar({k for k, v in self.vars.items() if not v.get()})
        self.destroy()


# ------------------------------------------------------------------ alertas
class DialogoAlertas(tk.Toplevel):
    """Onde cada medida vira aviso e onde vira critico.

    Dois campos por linha, na mesma ordem em que a cor aparece na tela. O que
    nao for numero valido volta ao padrao em vez de gravar lixo - limiar
    quebrado calaria alerta sem avisar.
    """

    def __init__(self, pai, lim: Limiares, ao_salvar, mono, mono_b) -> None:
        super().__init__(pai)
        self.title("sysmon · alertas")
        self.configure(bg=P["fundo"])
        self.transient(pai)
        self.ao_salvar = ao_salvar
        self.mono = mono
        self.campos: dict[str, tuple[tk.Entry, tk.Entry]] = {}

        tk.Label(self, text="a partir de que valor cada medida vira aviso "
                            "(ambar) e critico (vermelho)",
                 bg=P["fundo"], fg=P["fraco"], font=mono, justify="left",
                 wraplength=560).pack(anchor="w", padx=14, pady=(14, 4))

        grade = tk.Frame(self, bg=P["fundo"])
        grade.pack(fill="x", padx=14, pady=6)
        grade.columnconfigure(0, weight=1)
        for col, titulo in ((1, "aviso"), (2, "critico")):
            tk.Label(grade, text=titulo, bg=P["fundo"], fg=P["fraco"],
                     font=mono_b).grid(row=0, column=col, padx=4, pady=(0, 4))

        for i, (nome, rotulo, unidade) in enumerate(Limiares.CAMPOS, start=1):
            tk.Label(grade, text=rotulo, bg=P["fundo"], fg=P["texto"],
                     font=mono, anchor="w").grid(row=i, column=0, sticky="w", pady=2)
            par = getattr(lim, nome)
            entradas = []
            for j, valor in enumerate(par):
                e = tk.Entry(grade, width=7, justify="right", bg=P["painel"],
                             fg=P["aviso"] if j == 0 else P["critico"],
                             font=mono, relief="flat", insertbackground=P["titulo"],
                             highlightthickness=1, highlightbackground=P["linha"],
                             highlightcolor=P["titulo"])
                e.insert(0, f"{valor:g}")
                e.grid(row=i, column=j + 1, padx=4, pady=2)
                entradas.append(e)
            self.campos[nome] = tuple(entradas)

        tk.Label(self, text="filesystems ignorados  (um por linha; percentual "
                            "de /boot e da ESP nao diz nada util)",
                 bg=P["fundo"], fg=P["fraco"], font=mono, justify="left",
                 wraplength=560).pack(anchor="w", padx=14, pady=(12, 4))
        self.mounts = tk.Text(self, height=3, bg=P["painel"], fg=P["texto"],
                              font=mono, relief="flat", insertbackground=P["titulo"],
                              highlightthickness=1, highlightbackground=P["linha"],
                              padx=8, pady=5)
        self.mounts.insert("1.0", "\n".join(lim.ignorar_mounts))
        self.mounts.pack(fill="x", padx=14)

        acoes = tk.Frame(self, bg=P["fundo"])
        acoes.pack(fill="x", padx=14, pady=12)
        self._botao(acoes, "PADRAO", self.restaurar).pack(side="left")
        self._botao(acoes, "APLICAR", self.salvar, P["ok"]).pack(side="right")
        self._botao(acoes, "CANCELAR", self.destroy).pack(side="right", padx=6)

        self.recado = tk.Label(self, text="", bg=P["fundo"], fg=P["critico"],
                               font=mono, anchor="w")
        self.recado.pack(fill="x", padx=14, pady=(0, 12))

    def _botao(self, pai, texto, comando, cor=None):
        b = tk.Label(pai, text=f" {texto} ", bg=P["painel"], fg=cor or P["texto"],
                     font=self.mono, cursor="hand2", padx=6, pady=3)
        b.bind("<Button-1>", lambda e: comando())
        b.bind("<Enter>", lambda e: b.configure(bg=P["linha"]))
        b.bind("<Leave>", lambda e: b.configure(bg=P["painel"]))
        return b

    def restaurar(self) -> None:
        padrao = Limiares()
        for nome, (a, c) in self.campos.items():
            for e, v in zip((a, c), getattr(padrao, nome)):
                e.delete(0, "end")
                e.insert(0, f"{v:g}")
        self.mounts.delete("1.0", "end")
        self.mounts.insert("1.0", "\n".join(padrao.ignorar_mounts))

    def salvar(self) -> None:
        alertas, ruins = {}, []
        for nome, (ea, ec) in self.campos.items():
            try:
                a, c = float(ea.get().replace(",", ".")), float(ec.get().replace(",", "."))
            except ValueError:
                ruins.append(nome)
                continue
            if a >= c:
                ruins.append(f"{nome} (aviso deve ser menor que critico)")
                continue
            alertas[nome] = [a, c]
        if ruins:
            self.recado.configure(text="valor invalido em: " + ", ".join(ruins))
            return
        mounts = [l.strip() for l in self.mounts.get("1.0", "end").splitlines()
                  if l.strip()]
        self.ao_salvar(alertas, mounts)
        self.destroy()


# ------------------------------------------------------------------ janela
class Janela:
    LARGURA_BARRA = 10

    def __init__(self, frota: Frota, caminho_config: Path,
                 intervalo: float = 3.0, abrir_web=None) -> None:
        self.frota = frota
        self.caminho_config = caminho_config
        self.intervalo = max(1.0, intervalo)
        self.abrir_web = abrir_web
        self.estado = ler_estado()
        self.itens: dict[str, str] = {}
        self.fila: queue.Queue[str] = queue.Queue()
        self.na_bandeja = False
        self.oculto: set[str] = set(self.estado.get("oculto") or [])
        # Historico curto por (host, medida) para os sparklines. Guardado no
        # cliente, nao no agente: e memoria de janela aberta, nao telemetria.
        self.hist: dict[str, list[float]] = {}
        self._ultimo_ts: dict[str, float] = {}

        self.root = tk.Tk()
        self.root.title("sysmon")
        self.root.configure(bg=P["fundo"])
        self.root.geometry(self.estado.get("geometria", "820x640"))
        self.root.minsize(470, 260)

        self.mono = fonte_mono(10)
        self.mono_b = fonte_mono(10, bold=True)
        self.mono_t = fonte_mono(12, bold=True)

        self.sem_borda = tk.BooleanVar(value=self.estado.get("sem_borda", True))
        self.no_topo = tk.BooleanVar(value=bool(self.estado.get("topo")))

        self._icone()
        self._cabecalho()
        self._corpo()
        self._rodape()
        self._aplicar_moldura()
        self.root.attributes("-topmost", self.no_topo.get())

        self.root.protocol("WM_DELETE_WINDOW", self.fechar)
        self.root.bind("<F5>", lambda e: self.atualizar_agora())
        self.root.bind("<Control-r>", lambda e: self.atualizar_agora())

    # -- moldura ----------------------------------------------------------
    def _aplicar_moldura(self) -> None:
        """Sem moldura do sistema por padrao; a barra de titulo e nossa.

        Guarda e restaura a geometria em volta: alternar overrideredirect faz
        alguns gerenciadores de janela reposicionarem a janela.
        """
        # Antes da janela ser realizada, geometry() devolve "1x1+0+0"; reaplicar
        # isso encolhia a janela para o tamanho minimo. So preserva depois de
        # mapeada, que e quando alternar a moldura de fato reposiciona.
        mapeada = self.root.winfo_width() > 1
        geo = self.root.geometry() if mapeada else None
        self.root.overrideredirect(bool(self.sem_borda.get()))
        if geo:
            self.root.geometry(geo)
        if self.sem_borda.get():
            self.grip.place(relx=1.0, rely=1.0, anchor="se")
        else:
            self.grip.place_forget()

    # -- construcao -------------------------------------------------------
    def _icone(self) -> None:
        self._img = tk.PhotoImage(width=32, height=32)
        self.root.iconphoto(True, self._img)
        self._pintar_icone(OK)

    JANELA_HIST = 12

    def _anotar(self, host: str, ts: float, medidas: dict) -> None:
        """Guarda uma amostra por coleta, nao por redesenho.

        A janela redesenha a cada 3s e a frota coleta no ritmo dela; sem esta
        checagem o sparkline repetiria o mesmo valor e mentiria sobre o tempo.
        """
        if self._ultimo_ts.get(host) == ts:
            return
        self._ultimo_ts[host] = ts
        for nome, valor in medidas.items():
            fila = self.hist.setdefault(f"{host}:{nome}", [])
            fila.append(valor)
            del fila[:-self.JANELA_HIST]

    def serie(self, host: str, nome: str) -> list:
        return self.hist.get(f"{host}:{nome}", [])

    def ver(self, *chaves: str) -> bool:
        """Um campo aparece a menos que tenha sido desmarcado."""
        return not any(c in self.oculto for c in chaves)

    def editar_alertas(self) -> None:
        def aplicar(alertas: dict, mounts: list) -> None:
            bruto = dict(self.frota.cfg.extra)
            bruto["hosts"] = [{"nome": h.nome, "url": h.url, "token": h.token}
                              for h in self.frota.cfg.hosts]
            bruto["alertas"] = alertas
            bruto["ignorar_mounts"] = mounts
            cfg = carregar_config_de(bruto)     # valida antes de gravar
            salvar_config(self.caminho_config, bruto)
            self.frota.trocar(cfg)
            self.root.after(200, self.desenhar)

        DialogoAlertas(self.root, self.frota.cfg.limiares, aplicar,
                       self.mono, self.mono_b)

    def escolher_campos(self) -> None:
        def aplicar(oculto: set[str]) -> None:
            self.oculto = oculto
            self.estado["oculto"] = sorted(oculto)
            gravar_estado(self.estado)
            # Redesenha do zero: esconder um campo tem que remover o no, e a
            # poda so acontece comparando com o que foi visto nesta passada.
            self.desenhar()

        DialogoExibir(self.root, self.oculto, aplicar, self.mono, self.mono_b)

    def _pintar_icone(self, nivel: int) -> None:
        self._img.put(COR_NIVEL[nivel], to=(0, 0, 32, 32))

    def _cabecalho(self) -> None:
        c = tk.Frame(self.root, bg=P["fundo"])
        c.pack(fill="x", padx=10, pady=(9, 5))
        self.cabecalho = c

        marca = tk.Label(c, text="sysmon", bg=P["fundo"], fg=P["titulo"],
                         font=self.mono_t)
        marca.pack(side="left")
        self.resumo = tk.Label(c, text="", bg=P["fundo"], fg=P["fraco"],
                               font=self.mono)
        self.resumo.pack(side="left", padx=10)

        self._acao(c, "×", self.fechar, "fechar").pack(side="right", padx=(2, 0))
        self._acao(c, "–", self.minimizar, "minimizar").pack(side="right", padx=2)
        self._acao(c, "⌂", self.editar_hosts, "hosts").pack(side="right", padx=2)
        self._acao(c, "☰", self.escolher_campos, "escolher o que exibir").pack(
            side="right", padx=2)
        self._acao(c, "!", self.editar_alertas, "limiares de alerta").pack(
            side="right", padx=2)
        self._acao(c, "↻", self.atualizar_agora, "atualizar  F5").pack(
            side="right", padx=2)
        self.btn_topo = self._acao(c, "▲", self.alternar_topo,
                                   "sempre no topo")
        self.btn_topo.pack(side="right", padx=2)
        if self.abrir_web:
            self._acao(c, "◱", self.abrir_web, "dashboard web").pack(
                side="right", padx=2)

        # Arrastar pelo cabecalho, como qualquer janela sem moldura.
        for w in (c, marca, self.resumo):
            w.bind("<Button-1>", self._pegar)
            w.bind("<B1-Motion>", self._arrastar)
            w.bind("<Button-3>", self._menu)
        self._pintar_topo()

    def _acao(self, pai, texto, comando, dica=""):
        b = tk.Label(pai, text=texto, bg=P["fundo"], fg=P["fraco"],
                     font=self.mono_b, cursor="hand2", padx=5)
        b.bind("<Button-1>", lambda e: comando())

        def entrar(_e):
            b.configure(fg=P["texto"])
            if dica:
                self._dica(dica)

        def sair(_e):
            b.configure(fg=P["titulo"] if b is getattr(self, "btn_topo", None)
                        and self.no_topo.get() else P["fraco"])
            if dica:
                self._dica("")

        b.bind("<Enter>", entrar)
        b.bind("<Leave>", sair)
        return b

    def _corpo(self) -> None:
        quadro = tk.Frame(self.root, bg=P["fundo"])
        quadro.pack(fill="both", expand=True, padx=10)

        estilo = ttk.Style()
        # clam e o unico tema que honra cor de fundo no Treeview em todos os
        # sistemas; o padrao do Windows ignora e volta ao cinza de 1998.
        try:
            estilo.theme_use("clam")
        except tk.TclError:
            pass
        estilo.configure("sysmon.Treeview",
                         background=P["painel"], fieldbackground=P["painel"],
                         foreground=P["texto"], borderwidth=0, relief="flat",
                         bordercolor=P["fundo"], lightcolor=P["fundo"],
                         darkcolor=P["fundo"],
                         font=self.mono, rowheight=21)
        estilo.map("sysmon.Treeview",
                   background=[("selected", P["selecao"])],
                   foreground=[("selected", P["texto"])])
        estilo.configure("sysmon.Vertical.TScrollbar",
                         background=P["linha"], troughcolor=P["fundo"],
                         bordercolor=P["fundo"], arrowcolor=P["fraco"],
                         relief="flat")

        # Ordem das colunas: nome (fixo) · detalhe (elastico) · valor (fixo,
        # colado na borda direita). Antes o nome esticava e era ele quem era
        # espremido ao estreitar a janela, cortando ate o titulo da secao,
        # enquanto os numeros ficavam parados longe da borda. Agora quem cede
        # e o detalhe - o texto mais dispensavel - e os numeros acompanham a
        # borda.
        self.arvore = ttk.Treeview(quadro, style="sysmon.Treeview",
                                   columns=("d", "v"), show="tree",
                                   selectmode="none", takefocus=False)
        self.arvore.column("#0", width=190, minwidth=190, stretch=False)
        self.arvore.column("d", width=180, minwidth=0, stretch=True)
        self.arvore.column("v", width=230, minwidth=230, anchor="e", stretch=False)

        for n, cor in COR_NIVEL.items():
            self.arvore.tag_configure(f"n{n}", foreground=cor)
        for m, cor in COR_MAG.items():
            self.arvore.tag_configure(f"m{m}", foreground=cor)
        # Uma tag por severidade para a linha do host: no Treeview a tag pinta
        # a linha toda, entao um host critico fica vermelho de ponta a ponta,
        # nao so o nome. A faixa de fundo separa um bloco do seguinte.
        for n, cor in ((OK, P["titulo"]), (AVISO, P["aviso"]),
                       (CRITICO, P["critico"]), (OFFLINE, P["fraco"])):
            self.arvore.tag_configure(f"host{n}", foreground=cor,
                                      background=P["selecao"], font=self.mono_b)
        self.arvore.tag_configure("secao", foreground=P["fraco"], font=self.mono_b)

        rolagem = ttk.Scrollbar(quadro, orient="vertical",
                                style="sysmon.Vertical.TScrollbar",
                                command=self.arvore.yview)
        self.arvore.configure(yscrollcommand=rolagem.set)
        self.arvore.pack(side="left", fill="both", expand=True)
        rolagem.pack(side="right", fill="y")

    def _rodape(self) -> None:
        self.status = tk.Label(self.root, text="", bg=P["fundo"], fg=P["fraco"],
                               font=self.mono, anchor="w", padx=10, pady=5)
        self.status.pack(side="bottom", fill="x")

        self.painel_alertas = tk.Frame(self.root, bg=P["fundo"])
        self.rotulo_alertas = tk.Label(
            self.painel_alertas, text="", bg=P["fundo"], fg=P["critico"],
            font=self.mono, anchor="w", justify="left", padx=10)
        self.rotulo_alertas.pack(fill="x")

        # Canto de redimensionar: sem moldura do sistema, ele nao vem de graca.
        self.grip = tk.Label(self.root, text="◢", bg=P["fundo"],
                             fg=P["linha"], font=self.mono,
                             cursor="bottom_right_corner")
        self.grip.bind("<Button-1>", self._pegar_tamanho)
        self.grip.bind("<B1-Motion>", self._redimensionar)

    # -- interacao --------------------------------------------------------
    def _dica(self, texto: str) -> None:
        if texto:
            self.status.configure(text=texto)
        else:
            self.status.configure(text=getattr(self, "_status_base", ""))

    def _pegar(self, e) -> None:
        self._off = (e.x_root - self.root.winfo_x(), e.y_root - self.root.winfo_y())

    def _arrastar(self, e) -> None:
        self.root.geometry(f"+{e.x_root - self._off[0]}+{e.y_root - self._off[1]}")

    def _pegar_tamanho(self, e) -> None:
        self._base = (e.x_root, e.y_root, self.root.winfo_width(),
                      self.root.winfo_height())

    def _redimensionar(self, e) -> None:
        x0, y0, w, h = self._base
        self.root.geometry(
            f"{max(470, w + e.x_root - x0)}x{max(260, h + e.y_root - y0)}")

    def _menu(self, e) -> None:
        m = tk.Menu(self.root, tearoff=0, bg=P["painel"], fg=P["texto"],
                    activebackground=P["linha"], activeforeground=P["texto"],
                    font=self.mono, borderwidth=0)
        m.add_checkbutton(label="sempre no topo", variable=self.no_topo,
                          command=self.aplicar_topo)
        m.add_checkbutton(label="sem moldura", variable=self.sem_borda,
                          command=self._aplicar_moldura)
        m.add_separator()
        m.add_command(label="hosts...", command=self.editar_hosts)
        m.add_command(label="exibir...", command=self.escolher_campos)
        m.add_command(label="alertas...", command=self.editar_alertas)
        if self.abrir_web:
            m.add_command(label="dashboard web", command=self.abrir_web)
        m.add_separator()
        m.add_command(label="sair", command=self.sair)
        try:
            m.tk_popup(e.x_root, e.y_root)
        finally:
            m.grab_release()

    def alternar_topo(self) -> None:
        self.no_topo.set(not self.no_topo.get())
        self.aplicar_topo()

    def aplicar_topo(self) -> None:
        self.root.attributes("-topmost", self.no_topo.get())
        self._pintar_topo()

    def _pintar_topo(self) -> None:
        self.btn_topo.configure(fg=P["titulo"] if self.no_topo.get() else P["fraco"])

    def minimizar(self) -> None:
        if self.na_bandeja:
            self.root.withdraw()
            return
        # iconify nao funciona com overrideredirect: devolve a moldura, minimiza,
        # e tira de novo quando a janela reaparece.
        self.root.overrideredirect(False)
        self.root.iconify()
        self.root.bind("<Map>", self._remoldar, add="+")

    def _remoldar(self, _e=None) -> None:
        if self.sem_borda.get():
            self.root.after(60, self._aplicar_moldura)

    def atualizar_agora(self) -> None:
        self.frota.atualizar_agora()
        self.root.after(400, self.desenhar)

    def editar_hosts(self) -> None:
        bruto = dict(self.frota.cfg.extra)
        bruto["hosts"] = [{"nome": h.nome, "url": h.url, "token": h.token}
                          for h in self.frota.cfg.hosts]

        def salvar(hosts: list[dict]) -> None:
            # Token em branco mantem o que ja havia: mudar um apelido nao deve
            # exigir redigitar o segredo.
            atuais = {h.nome: h.token for h in self.frota.cfg.hosts}
            for h in hosts:
                if not h.get("token") and h.get("nome") in atuais:
                    h["token"] = atuais[h["nome"]]
            novo = dict(bruto, hosts=hosts)
            cfg = carregar_config_de(novo)      # valida ANTES de gravar
            salvar_config(self.caminho_config, novo)
            self.frota.trocar(cfg)
            self.root.after(300, self.desenhar)

        DialogoHosts(self.root, bruto, salvar, self.mono)

    def fechar(self) -> None:
        """Com bandeja ativa, fechar so esconde - sair e pelo icone."""
        self._guardar()
        if self.na_bandeja:
            self.root.withdraw()
        else:
            self.root.destroy()

    def sair(self) -> None:
        self._guardar()
        self.na_bandeja = False
        self.root.destroy()

    def _guardar(self) -> None:
        try:
            self.estado.update(geometria=self.root.geometry(),
                               topo=bool(self.no_topo.get()),
                               sem_borda=bool(self.sem_borda.get()),
                               oculto=sorted(self.oculto))
            gravar_estado(self.estado)
        except tk.TclError:
            pass

    # -- comandos da bandeja (outra thread) -------------------------------
    def pedir(self, comando: str) -> None:
        self.fila.put(comando)

    def _drenar(self) -> None:
        while True:
            try:
                cmd = self.fila.get_nowait()
            except queue.Empty:
                return
            if cmd == "mostrar":
                self.root.deiconify()
                self.root.lift()
                self._remoldar()
            elif cmd == "topo":
                self.alternar_topo()
            elif cmd == "atualizar":
                self.atualizar_agora()
            elif cmd == "sair":
                self.sair()
                return

    # -- desenho ----------------------------------------------------------
    def _no(self, pai, chave, texto, valor="", detalhe="", tags=()):
        """Atualiza no lugar. Recriar a arvore fecharia o que o usuario abriu."""
        iid = self.itens.get(chave)
        if iid and self.arvore.exists(iid):
            self.arvore.item(iid, text=texto, values=(detalhe, valor), tags=tags)
            return iid
        iid = self.arvore.insert(pai, "end", text=texto, values=(detalhe, valor),
                                 tags=tags, open=True)
        self.itens[chave] = iid
        return iid

    def _medida(self, pai, hk, chave, rotulo, pct, detalhe, limiar, serie,
                vistos) -> None:
        """Linha de medida percentual: sparkline, barra, valor e cor graduada.

        A cor sai da magnitude, nao so do limiar: cinco degraus em vez de tres,
        para que sair de 3% para 30% tenha efeito visivel.
        """
        c = f"{hk}/{chave}"
        vistos.add(c)
        valor = f"{spark(serie, 8):<9}{barra(pct, self.LARGURA_BARRA)} " \
                f"{fmt_pct(pct):>4}" if serie else \
                f"{barra(pct, self.LARGURA_BARRA)} {fmt_pct(pct):>4}"
        self._no(pai, c, "    " + rotulo, valor, detalhe,
                 (f"m{magnitude(pct, *limiar)}",))

    def desenhar(self) -> None:
        vistos: set[str] = set()
        pior = OK
        b = self.LARGURA_BARRA

        lim = self.frota.cfg.limiares
        for host, estado in self.frota.estados():
            nivel, _ = avaliar(estado, lim)
            pior = max(pior, nivel)
            d = estado.dados or {}
            hk = f"h:{host.nome}"
            vistos.add(hk)

            mem = d.get("mem") or {}
            so = (d.get("so") or {}).get("nome") or ""
            self._anotar(host.nome, d.get("ts"), {
                "cpu": d.get("cpu_percent"),
                "ram": mem.get("percent"),
                "temp": d.get("cpu_temp"),
            })

            # As tres medidas que se olha primeiro, juntas na linha do host.
            mostrar_resumo = self.ver("sec:RESUMO")
            resumo = "" if not mostrar_resumo else " · ".join(x for x in (
                fmt_temp(d.get("cpu_temp"))
                if self.ver("r:temp") and d.get("cpu_temp") is not None else "",
                f"cpu {fmt_pct(d.get('cpu_percent'))}"
                if self.ver("r:cpu") and d.get("cpu_percent") is not None else "",
                f"ram {fmt_pct(mem.get('percent'))}"
                if self.ver("r:ram") and mem.get("percent") is not None else "",
            ) if x)
            detalhe = "" if not mostrar_resumo else " · ".join(x for x in (
                f"{fmt_bytes(mem.get('usado'))} de {fmt_bytes(mem.get('total'))}"
                if self.ver("r:gb") and mem.get("total") else "",
                so if self.ver("r:so") else "",
            ) if x)

            raiz = self._no(
                "", hk, host.nome.upper(),
                resumo if estado.dados else "OFFLINE",
                detalhe if estado.dados else (estado.erro or "sem dados"),
                (f"host{nivel}",))

            if not estado.dados:
                continue

            def secao(nome: str) -> str:
                c = f"{hk}/{nome}"
                vistos.add(c)
                return self._no(raiz, c, "  " + nome, tags=("secao",))

            def linha(pai, chave, rotulo, valor, detalhe="", nv=OK):
                c = f"{hk}/{chave}"
                vistos.add(c)
                self._no(pai, c, "    " + rotulo, valor, detalhe, (f"n{nv}",))

            # ---- desempenho
            if self.ver("sec:DESEMPENHO"):
                g = secao("DESEMPENHO")
                cpu = d.get("cpu_percent")
                chip = (d.get("cpu_modelo") or "").replace("(R)", "").replace("(TM)", "")
                if self.ver("p:cpu"):
                    self._medida(g, hk, "cpu", "cpu", cpu,
                                 " · ".join(x for x in (f"{d.get('cpus', '?')} nucleos",
                                                        chip.strip()[:30]) if x),
                                 (80, 95), self.serie(host.nome, "cpu"), vistos)
                mp = mem.get("percent")
                if self.ver("p:mem"):
                    self._medida(g, hk, "mem", "memoria", mp,
                                 f"{fmt_bytes(mem.get('usado'))} / "
                                 f"{fmt_bytes(mem.get('total'))}",
                                 lim.ram, self.serie(host.nome, "ram"), vistos)
                if self.ver("p:swap") and mem.get("swap_percent"):
                    self._medida(g, hk, "swap", "swap", mem["swap_percent"],
                                 fmt_bytes(mem.get("swap_usado")), (50, 80),
                                 [], vistos)
                load = d.get("load") or []
                if self.ver("p:load") and len(load) == 3:
                    linha(g, "load", "carga", f"{load[0]:.2f}",
                          f"{load[1]:.2f} 5m · {load[2]:.2f} 15m")
                if self.ver("p:up"):
                    linha(g, "up", "no ar", fmt_uptime(d.get("uptime_s")))

            # ---- temperatura
            temps = d.get("temps") or []
            if self.ver("sec:TEMPERATURA") and (temps or d.get("cpu_temp") is not None):
                g = secao("TEMPERATURA")
                crit = d.get("cpu_crit")
                if self.ver("t:cpu"):
                    t = d.get("cpu_temp")
                    linha(g, "t:cpu", "cpu",
                          f"{spark(self.serie(host.nome, 'temp'), 8):<12} "
                          f"{fmt_temp(t)}",
                          f"critico {fmt_temp(crit)}" if crit else "",
                          _nivel_temp(t, crit, lim))
                if self.ver("t:todos"):
                    for i, sen in enumerate(temps[:10]):
                        linha(g, f"t:{i}", (sen.get("label") or "").lower()[:18],
                              fmt_temp(sen.get("c")), (sen.get("chip") or "")[:18],
                              _nivel_temp(sen.get("c"), sen.get("crit"), lim))

            fans = d.get("fans") or {}
            if self.ver("sec:VENTOINHAS", "v:todas") and fans:
                g = secao("VENTOINHAS")
                for nome, rpm in list(fans.items())[:6]:
                    linha(g, f"f:{nome}", nome.split("/")[-1].lower()[:18],
                          f"{rpm} rpm")

            # ---- discos fisicos
            for x in (d.get("blocos") or []) if self.ver("sec:DISCOS",
                                                         "b:todos") else []:
                g = secao("DISCOS")
                sm = x.get("smart") or {}
                # Ordem por importancia: o que faz trocar o disco primeiro, o
                # modelo por ultimo - e ele que cede quando a coluna aperta.
                det = []
                if sm.get("saude") == "falha":
                    det.append("SMART REPROVOU")
                if sm.get("desgaste_percent") is not None:
                    det.append(f"{sm['desgaste_percent']:.0f}% usado")
                det += [fmt_bytes(x.get("tamanho")), (x.get("modelo") or "")[:22]]
                linha(g, f"b:{x['dev']}", x["dev"], fmt_temp(x.get("temp_c")),
                      " · ".join(v for v in det if v),
                      CRITICO if sm.get("saude") == "falha"
                      else _faixa(x.get("temp_c"), 60, 70))

            # ---- armazenamento
            # Mesmo filtro do alerta: se nao vale avaliar, nao vale ocupar linha.
            discos = (discos_relevantes(d.get("discos"), self.frota.cfg.limiares)
                      if self.ver("a:fs") else [])
            tps = (d.get("thinpools") or []) if self.ver("a:thin") else []
            if self.ver("sec:ARMAZENAMENTO") and (discos or tps):
                g = secao("ARMAZENAMENTO")
                for x in discos:
                    self._medida(g, hk, f"d:{x['mount']}", x["mount"][:22],
                                 x.get("percent"),
                                 f"{fmt_bytes(x.get('usado'))} / "
                                 f"{fmt_bytes(x.get('total'))}",
                                 lim.disco, [], vistos)
                for t in tps:
                    self._medida(g, hk, f"tp:{t['nome']}", t["nome"][:22],
                                 t.get("data_percent"),
                                 f"metadata {fmt_pct(t.get('meta_percent'))}",
                                 lim.thinpool, [], vistos)

            # ---- rede
            redes = ([n for n in (d.get("net") or []) if n.get("up")]
                     if self.ver("sec:REDE", "n:todas") else [])
            if redes:
                g = secao("REDE")
                for n in redes:
                    linha(g, f"n:{n['iface']}", n["iface"][:18],
                          f"↓{fmt_bps(n.get('rx_bps'))}",
                          f"↑{fmt_bps(n.get('tx_bps'))}"
                          + (f" · {n['mbps']} Mbit" if n.get("mbps") else ""))

        for chave, iid in list(self.itens.items()):
            if chave not in vistos:
                if self.arvore.exists(iid):
                    self.arvore.delete(iid)
                del self.itens[chave]

        self._pintar_icone(pior)
        self._resumo(pior)

    def _resumo(self, pior: int) -> None:
        n = len(self.frota.cfg.hosts)
        offline = sum(1 for _, e in self.frota.estados() if e.erro or not e.dados)
        alertas = self.frota.alertas()

        partes = [f"{n} host{'s' if n != 1 else ''}"]
        if offline:
            partes.append(f"{offline} offline")
        if alertas:
            partes.append(f"{len(alertas)} alerta{'s' if len(alertas) != 1 else ''}")
        self.resumo.configure(text=" · ".join(partes), fg=COR_NIVEL[pior])

        if alertas:
            txt = "\n".join("! " + a for a in alertas[:4])
            if len(alertas) > 4:
                txt += f"\n  + {len(alertas) - 4} outros"
            self.rotulo_alertas.configure(text=txt)
            if not self.painel_alertas.winfo_ismapped():
                self.painel_alertas.pack(side="bottom", fill="x", pady=(4, 0))
        elif self.painel_alertas.winfo_ismapped():
            self.painel_alertas.pack_forget()

        self._status_base = ("sem hosts · use ⌂ para configurar" if n == 0
                             else f"atualiza {self.intervalo:.0f}s · F5 forca"
                                  " · arraste pelo topo")
        self.status.configure(text=self._status_base)

    # -- ciclo ------------------------------------------------------------
    def _tique(self) -> None:
        try:
            self._drenar()
            if not self.root.winfo_exists():
                return
            self.desenhar()
        except tk.TclError:
            return
        except Exception:  # noqa: BLE001 - desenhar nunca derruba a janela
            import logging
            logging.exception("falha ao desenhar")
        self.root.after(int(self.intervalo * 1000), self._tique)

    def rodar(self) -> None:
        self.frota.esperar_primeira_leitura(limite=2.5)
        self.desenhar()
        if not self.frota.cfg.hosts:
            self.root.after(300, self.editar_hosts)
        self.root.after(int(self.intervalo * 1000), self._tique)
        self.root.mainloop()


def _faixa(v, aviso, critico) -> int:
    if v is None:
        return OFFLINE
    return CRITICO if v >= critico else AVISO if v >= aviso else OK


def _nivel_temp(c, crit, lim=None) -> int:
    """Fracao do critico do sensor, com os limiares configurados."""
    if c is None:
        return OFFLINE
    fa, fc = (lim.temp_frac if lim else (0.75, 0.90))
    return _faixa(c, (crit or 100) * fa, (crit or 100) * fc)


def rodar(frota: Frota, caminho_config, intervalo: float = 3.0,
          abrir_web=None, com_bandeja=None, oculto: bool = False) -> Janela:
    """Abre a janela. com_bandeja recebe a Janela e devolve True se subiu o icone."""
    j = Janela(frota, Path(caminho_config), intervalo, abrir_web)
    if com_bandeja:
        j.na_bandeja = bool(com_bandeja(j))
    if oculto and j.na_bandeja:
        j.root.withdraw()
    j.rodar()
    return j


def disponivel() -> bool:
    try:
        import tkinter  # noqa: F401
        return True
    except ImportError:
        return False
