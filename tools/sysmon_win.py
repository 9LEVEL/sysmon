#!/usr/bin/env python3
"""
sysmon_win - janela nativa em Tkinter, no estilo Open Hardware Monitor.

Uma arvore de sensores por host, do jeito que CPU-Z e OHM mostram: categoria,
sensor, valor, limite. Sem navegador, sem WebView2, sem pip.

Por que Tkinter e nao um webview: o Tk vem junto com o Python do python.org.
Nao ha pacote para instalar, motor de navegador para faltar nem componente do
sistema para estar ausente - os tres motivos pelos quais a janela do webview
nao subia em maquina real. "Sempre no topo" aqui e uma linha
(attributes -topmost), nao uma ponte entre processos.

    python sysmon.pyz            # esta janela
    python sysmon.pyz --web      # o dashboard rico, no navegador ou webview
"""

from __future__ import annotations

import json
import os
import queue
import tkinter as tk
from pathlib import Path
from tkinter import font as tkfont
from tkinter import messagebox, ttk

from sysmon_nucleo import (
    AVISO, CRITICO, OFFLINE, OK,
    ErroConfig, Frota, avaliar, carregar_config_de, fmt_bps, fmt_bytes,
    fmt_pct, fmt_temp, fmt_uptime, salvar_config, testar_host,
)

__version__ = "3.0.0"

# Cores de status sobre fundo claro (o visual nativo de utilitario no Windows).
# Todas passam 4.5:1 em branco - o valor numerico continua sendo o dado
# principal, a cor so reforca.
COR = {
    OK: "#1a1a1a",
    AVISO: "#8a5a00",
    CRITICO: "#b3261e",
    OFFLINE: "#8a8a8a",
}
COR_FUNDO_ALERTA = "#fdf3f2"


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


# ------------------------------------------------------------------ dialogo
class DialogoHosts(tk.Toplevel):
    """Configuracao dos hosts, sem sair da janela.

    Existe para o programa nunca precisar do navegador: sem isto, configurar
    exigiria abrir a pagina web - justamente o que se quer evitar.
    """

    def __init__(self, pai, cfg_bruto: dict, ao_salvar) -> None:
        super().__init__(pai)
        self.title("Hosts monitorados")
        self.transient(pai)
        self.resizable(True, False)
        self.ao_salvar = ao_salvar
        self.linhas: list[dict] = []

        ttk.Label(self, text="Cada host Linux tem uma URL e um token próprios, "
                             "impressos pelo instalador do agente.",
                  wraplength=560).grid(row=0, column=0, columnspan=2,
                                       sticky="w", padx=12, pady=(12, 8))

        self.quadro = ttk.Frame(self)
        self.quadro.grid(row=1, column=0, columnspan=2, sticky="nsew", padx=12)
        self.columnconfigure(0, weight=1)

        for h in cfg_bruto.get("hosts") or []:
            self.adicionar(h)
        if not self.linhas:
            self.adicionar({})

        acoes = ttk.Frame(self)
        acoes.grid(row=2, column=0, columnspan=2, sticky="ew", padx=12, pady=12)
        ttk.Button(acoes, text="+ Adicionar",
                   command=lambda: self.adicionar({})).pack(side="left")
        ttk.Button(acoes, text="Salvar", command=self.salvar).pack(side="right")
        ttk.Button(acoes, text="Cancelar",
                   command=self.destroy).pack(side="right", padx=(0, 6))

        self.recado = ttk.Label(self, text="", wraplength=560)
        self.recado.grid(row=3, column=0, columnspan=2, sticky="w",
                         padx=12, pady=(0, 12))

    def adicionar(self, h: dict) -> None:
        i = len(self.linhas)
        f = ttk.LabelFrame(self.quadro, text=f"Host {i + 1}")
        f.grid(row=i, column=0, sticky="ew", pady=4)
        f.columnconfigure(1, weight=1)

        campos = {}
        for j, (chave, rotulo, exemplo) in enumerate((
            ("nome", "Apelido", "pve"),
            ("url", "URL", "http://192.168.0.10:9109/metrics"),
            ("token", "Token", "o que o install.sh imprimiu"),
        )):
            ttk.Label(f, text=rotulo).grid(row=j, column=0, sticky="w", padx=8, pady=3)
            e = ttk.Entry(f, width=52)
            valor = h.get(chave, "")
            if chave == "token" and valor:
                e.insert(0, valor)
            elif chave != "token":
                e.insert(0, valor)
            e.grid(row=j, column=1, sticky="ew", padx=(0, 8), pady=3)
            campos[chave] = e
            if chave == "token" and not valor:
                e.insert(0, "")
        ttk.Label(f, text=exemplo, foreground="#777").grid(
            row=1, column=2, sticky="w", padx=(0, 8))

        estado = ttk.Label(f, text="", foreground="#555")
        estado.grid(row=3, column=0, columnspan=3, sticky="w", padx=8, pady=(0, 6))

        botoes = ttk.Frame(f)
        botoes.grid(row=0, column=2, sticky="e", padx=(0, 8))
        ttk.Button(botoes, text="Testar", width=8,
                   command=lambda: self.testar(campos, estado)).pack(side="left")
        ttk.Button(botoes, text="Remover", width=8,
                   command=lambda: self.remover(f, campos)).pack(side="left", padx=4)

        self.linhas.append({"frame": f, "campos": campos, "estado": estado})

    def remover(self, frame, campos) -> None:
        frame.destroy()
        self.linhas = [l for l in self.linhas if l["campos"] is not campos]

    def testar(self, campos, estado) -> None:
        estado.configure(text="testando...", foreground="#555")
        estado.update_idletasks()
        ok, msg = testar_host(campos["url"].get().strip(),
                              campos["token"].get().strip())
        estado.configure(text=("respondeu: " if ok else "falhou: ") + msg,
                         foreground="#1a7f37" if ok else "#b3261e")

    def salvar(self) -> None:
        hosts = []
        for l in self.linhas:
            if not l["frame"].winfo_exists():
                continue
            c = l["campos"]
            url = c["url"].get().strip()
            if not url:
                continue
            hosts.append({"nome": c["nome"].get().strip() or None,
                          "url": url, "token": c["token"].get().strip()})
        hosts = [{k: v for k, v in h.items() if v} for h in hosts]
        if not hosts:
            self.recado.configure(text="Preencha ao menos um host.",
                                  foreground="#b3261e")
            return
        try:
            self.ao_salvar(hosts)
        except ErroConfig as e:
            self.recado.configure(text=str(e).splitlines()[0], foreground="#b3261e")
            return
        self.destroy()


# ------------------------------------------------------------------ janela
class Janela:
    """A janela principal: arvore de sensores, uma raiz por host."""

    def __init__(self, frota: Frota, caminho_config: Path,
                 intervalo: float = 3.0, abrir_web=None) -> None:
        self.frota = frota
        self.caminho_config = caminho_config
        self.intervalo = max(1.0, intervalo)
        self.abrir_web = abrir_web
        self.estado = ler_estado()
        self.itens: dict[str, str] = {}   # chave logica -> id do Treeview
        # Tkinter nao e thread-safe. A bandeja roda em outra thread, entao os
        # comandos dela entram por aqui e sao aplicados no laco principal.
        self.fila: queue.Queue[str] = queue.Queue()
        self.na_bandeja = False

        self.root = tk.Tk()
        self.root.title(f"sysmon {__version__}")
        self.root.geometry(self.estado.get("geometria", "920x620"))
        self.root.minsize(560, 320)
        self._icone()

        self.no_topo = tk.BooleanVar(value=bool(self.estado.get("topo")))
        self.root.attributes("-topmost", self.no_topo.get())

        self._barra()
        self._arvore()
        self._rodape()

        self.root.protocol("WM_DELETE_WINDOW", self.fechar)
        self.root.bind("<F5>", lambda e: self.atualizar_agora())
        self.root.bind("<Control-r>", lambda e: self.atualizar_agora())

    # -- construcao -------------------------------------------------------
    def _icone(self) -> None:
        # Quadrado colorido como icone da janela: sem arquivo externo, e ja
        # diz o estado da frota na barra de tarefas.
        self._img_icone = tk.PhotoImage(width=32, height=32)
        self.root.iconphoto(True, self._img_icone)
        self._pintar_icone(OK)

    def _pintar_icone(self, nivel: int) -> None:
        cor = {OK: "#1a7f37", AVISO: "#c98500", CRITICO: "#b3261e",
               OFFLINE: "#8a8a8a"}[nivel]
        self._img_icone.put(cor, to=(0, 0, 32, 32))

    def _barra(self) -> None:
        barra = ttk.Frame(self.root, padding=(8, 6))
        barra.pack(fill="x")

        ttk.Checkbutton(barra, text="Sempre no topo", variable=self.no_topo,
                        command=self._alternar_topo).pack(side="left")
        ttk.Button(barra, text="Hosts...", width=10,
                   command=self.editar_hosts).pack(side="right")
        ttk.Button(barra, text="Atualizar", width=10,
                   command=self.atualizar_agora).pack(side="right", padx=6)
        if self.abrir_web:
            ttk.Button(barra, text="Dashboard", width=11,
                       command=self.abrir_web).pack(side="right")

        self.resumo = ttk.Label(barra, text="")
        self.resumo.pack(side="left", padx=14)

    def _arvore(self) -> None:
        quadro = ttk.Frame(self.root)
        quadro.pack(fill="both", expand=True, padx=8)

        self.arvore = ttk.Treeview(quadro, columns=("valor", "limite"),
                                   selectmode="none")
        self.arvore.heading("#0", text="Sensor", anchor="w")
        self.arvore.heading("valor", text="Valor", anchor="e")
        self.arvore.heading("limite", text="Limite / detalhe", anchor="w")
        self.arvore.column("#0", width=300, minwidth=160, stretch=True)
        self.arvore.column("valor", width=110, minwidth=70, anchor="e", stretch=False)
        self.arvore.column("limite", width=250, minwidth=100, stretch=True)

        for nivel, cor in COR.items():
            self.arvore.tag_configure(f"n{nivel}", foreground=cor)
        negrito = tkfont.nametofont("TkDefaultFont").copy()
        negrito.configure(weight="bold")
        self.arvore.tag_configure("host", font=negrito)
        self.arvore.tag_configure("grupo", foreground="#444")

        barra = ttk.Scrollbar(quadro, orient="vertical", command=self.arvore.yview)
        self.arvore.configure(yscrollcommand=barra.set)
        self.arvore.pack(side="left", fill="both", expand=True)
        barra.pack(side="right", fill="y")

    def _rodape(self) -> None:
        # A barra de status vai por ultimo no fundo; os alertas entram acima
        # dela. Empacotar nesta ordem e o que garante isso.
        self.status = ttk.Label(self.root, text="", anchor="w", padding=(10, 4),
                                relief="sunken")
        self.status.pack(side="bottom", fill="x")

        self.alertas = tk.Text(self.root, height=3, wrap="word", relief="flat",
                               background=COR_FUNDO_ALERTA, foreground=COR[CRITICO],
                               padx=10, pady=6, borderwidth=0,
                               font=tkfont.nametofont("TkDefaultFont"))
        self.alertas.configure(state="disabled")

    # -- acoes ------------------------------------------------------------
    def _alternar_topo(self) -> None:
        self.root.attributes("-topmost", self.no_topo.get())

    def atualizar_agora(self) -> None:
        self.frota.atualizar_agora()
        self.root.after(400, self.desenhar)

    def editar_hosts(self) -> None:
        bruto = dict(self.frota.cfg.extra)
        bruto["hosts"] = [{"nome": h.nome, "url": h.url, "token": h.token}
                          for h in self.frota.cfg.hosts]

        def salvar(hosts: list[dict]) -> None:
            # Token em branco mantem o que ja estava: a caixa nunca deve
            # exigir redigitar o segredo para mudar um apelido.
            atuais = {h.nome: h.token for h in self.frota.cfg.hosts}
            for h in hosts:
                if not h.get("token") and h.get("nome") in atuais:
                    h["token"] = atuais[h["nome"]]
            novo = dict(bruto, hosts=hosts)
            cfg = carregar_config_de(novo)      # valida ANTES de gravar
            salvar_config(self.caminho_config, novo)
            self.frota.trocar(cfg)
            self.root.after(300, self.desenhar)

        DialogoHosts(self.root, bruto, salvar)

    def fechar(self) -> None:
        """Fechar com bandeja ativa apenas esconde - o programa segue vivo.

        E o comportamento de app de bandeja: sair de verdade e pelo menu do
        icone. Sem bandeja, fechar encerra, senao sobraria um processo sem
        nenhuma forma de trazer a janela de volta.
        """
        self._guardar()
        if self.na_bandeja:
            self.root.withdraw()
        else:
            self.root.destroy()

    def _guardar(self) -> None:
        try:
            self.estado.update(geometria=self.root.geometry(),
                               topo=bool(self.no_topo.get()))
            gravar_estado(self.estado)
        except tk.TclError:
            pass

    # -- comandos vindos da bandeja (outra thread) -------------------------
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
                self.root.focus_force()
            elif cmd == "topo":
                self.no_topo.set(not self.no_topo.get())
                self._alternar_topo()
            elif cmd == "atualizar":
                self.atualizar_agora()
            elif cmd == "sair":
                self._guardar()
                self.na_bandeja = False
                self.root.destroy()
                return

    # -- desenho ----------------------------------------------------------
    def _no(self, pai: str, chave: str, texto: str, valor: str = "",
            limite: str = "", tags: tuple = ()) -> str:
        """Cria ou atualiza um no, preservando a expansao entre atualizacoes.

        Recriar a arvore a cada ciclo fecharia tudo que o usuario abriu, a cada
        tres segundos - inutilizavel.
        """
        iid = self.itens.get(chave)
        if iid and self.arvore.exists(iid):
            self.arvore.item(iid, text=texto, values=(valor, limite), tags=tags)
            return iid
        iid = self.arvore.insert(pai, "end", text=texto, values=(valor, limite),
                                 tags=tags, open=True)
        self.itens[chave] = iid
        return iid

    def desenhar(self) -> None:
        vistos: set[str] = set()
        pior = OK

        for host, estado in self.frota.estados():
            nivel, _ = avaliar(estado)
            pior = max(pior, nivel)
            d = estado.dados or {}
            raiz_chave = f"h:{host.nome}"
            so = (d.get("so") or {}).get("nome") or ""
            resumo = so if estado.dados else (estado.erro or "sem dados")
            raiz = self._no("", raiz_chave, host.nome,
                            fmt_temp(d.get("cpu_temp")) if estado.dados else "offline",
                            resumo, ("host", f"n{nivel}"))
            vistos.add(raiz_chave)
            if not estado.dados:
                continue

            def grupo(nome: str) -> str:
                c = f"{raiz_chave}/{nome}"
                vistos.add(c)
                return self._no(raiz, c, nome, tags=("grupo",))

            def item(g: str, chave: str, texto: str, valor: str,
                     limite: str = "", nivel_item: int = OK) -> None:
                c = f"{raiz_chave}/{chave}"
                vistos.add(c)
                self._no(g, c, texto, valor, limite, (f"n{nivel_item}",))

            # --- Uso
            g = grupo("Uso")
            mem = d.get("mem") or {}
            item(g, "cpu", "CPU", fmt_pct(d.get("cpu_percent")),
                 f"{d.get('cpus', '?')} núcleos · {d.get('cpu_modelo') or ''}".strip(" ·"),
                 _faixa(d.get("cpu_percent"), 80, 95))
            item(g, "ram", "Memória", fmt_pct(mem.get("percent")),
                 f"{fmt_bytes(mem.get('usado'))} de {fmt_bytes(mem.get('total'))}",
                 _faixa(mem.get("percent"), 90, 97))
            if mem.get("swap_percent"):
                item(g, "swap", "Swap", fmt_pct(mem.get("swap_percent")),
                     fmt_bytes(mem.get("swap_usado")))
            load = d.get("load") or []
            if len(load) == 3:
                item(g, "load", "Load", f"{load[0]:.2f}",
                     f"{load[1]:.2f} (5m) · {load[2]:.2f} (15m)")
            item(g, "up", "Ligado há", fmt_uptime(d.get("uptime_s")))

            # --- Temperaturas
            temps = d.get("temps") or []
            if temps or d.get("cpu_temp") is not None:
                g = grupo("Temperaturas")
                crit = d.get("cpu_crit")
                item(g, "t:cpu", "CPU", fmt_temp(d.get("cpu_temp")),
                     f"crítico {fmt_temp(crit)}" if crit else "",
                     _nivel_temp(d.get("cpu_temp"), crit))
                for i, s in enumerate(temps[:12]):
                    item(g, f"t:{i}", f"{s.get('chip')} · {s.get('label')}",
                         fmt_temp(s.get("c")),
                         f"crítico {fmt_temp(s['crit'])}" if s.get("crit") else "",
                         _nivel_temp(s.get("c"), s.get("crit")))

            fans = d.get("fans") or {}
            if fans:
                g = grupo("Ventoinhas")
                for nome, rpm in list(fans.items())[:8]:
                    item(g, f"f:{nome}", nome, f"{rpm} RPM")

            # --- Discos fisicos
            blocos = d.get("blocos") or []
            if blocos:
                g = grupo("Discos")
                for b in blocos:
                    smart = b.get("smart") or {}
                    extra = [b.get("modelo") or "", fmt_bytes(b.get("tamanho"))]
                    if smart.get("desgaste_percent") is not None:
                        extra.append(f"{smart['desgaste_percent']:.0f}% de vida usada")
                    if smart.get("saude") == "falha":
                        extra.append("SMART REPROVOU")
                    item(g, f"b:{b['dev']}", b["dev"],
                         fmt_temp(b.get("temp_c")), " · ".join(x for x in extra if x),
                         CRITICO if smart.get("saude") == "falha"
                         else _faixa(b.get("temp_c"), 60, 70))

            # --- Filesystems
            discos = d.get("discos") or []
            if discos:
                g = grupo("Armazenamento")
                for x in discos:
                    item(g, f"d:{x['mount']}", x["mount"], fmt_pct(x.get("percent")),
                         f"{fmt_bytes(x.get('usado'))} de {fmt_bytes(x.get('total'))}",
                         _faixa(x.get("percent"), 80, 90))
            for tp in d.get("thinpools") or []:
                item(grupo("Armazenamento"), f"tp:{tp['nome']}", tp["nome"],
                     fmt_pct(tp.get("data_percent")),
                     f"metadata {fmt_pct(tp.get('meta_percent'))}",
                     _faixa(tp.get("data_percent"), 80, 90))

            # --- Rede
            redes = [n for n in (d.get("net") or []) if n.get("up")]
            if redes:
                g = grupo("Rede")
                for n in redes:
                    item(g, f"n:{n['iface']}", n["iface"],
                         fmt_bps(n.get("rx_bps")),
                         f"envio {fmt_bps(n.get('tx_bps'))}"
                         + (f" · {n['mbps']} Mbit" if n.get("mbps") else ""))

        # Remove o que sumiu da configuracao.
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
        partes = [f"{n} host{'s' if n != 1 else ''}"]
        if offline:
            partes.append(f"{offline} offline")
        alertas = self.frota.alertas()
        if alertas:
            partes.append(f"{len(alertas)} alerta{'s' if len(alertas) != 1 else ''}")
        self.resumo.configure(text=" · ".join(partes), foreground=COR[pior])

        self.alertas.configure(state="normal")
        self.alertas.delete("1.0", "end")
        if alertas:
            texto = [f"!  {a}" for a in alertas[:5]]
            if len(alertas) > 5:
                texto.append(f"   ... e mais {len(alertas) - 5}")
            self.alertas.insert("1.0", "\n".join(texto))
            self.alertas.configure(height=min(len(alertas), 5) + (1 if len(alertas) > 5 else 0))
            if not self.alertas.winfo_ismapped():
                self.alertas.pack(side="bottom", fill="x")
        elif self.alertas.winfo_ismapped():
            self.alertas.pack_forget()
        self.alertas.configure(state="disabled")

        if n == 0:
            self.status.configure(
                text="Nenhum host configurado — clique em Hosts... para começar.")
        else:
            self.status.configure(
                text=f"Atualiza a cada {self.intervalo:.0f}s · F5 força · "
                     "a coleta acontece neste processo")

    # -- ciclo ------------------------------------------------------------
    def _tique(self) -> None:
        try:
            self._drenar()
            if not self.root.winfo_exists():
                return
            self.desenhar()
        except tk.TclError:
            return          # janela fechando
        except Exception:   # noqa: BLE001 - desenho nunca derruba a janela
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
    if v >= critico:
        return CRITICO
    if v >= aviso:
        return AVISO
    return OK


def _nivel_temp(c, crit) -> int:
    if c is None:
        return OFFLINE
    return _faixa(c, (crit or 100) * 0.75, (crit or 100) * 0.90)


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
