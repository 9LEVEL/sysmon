#!/usr/bin/env python3
"""
sysmon_nucleo.py - o que o dashboard do terminal e o tray do Windows tem em comum.

Aqui moram a leitura da configuracao, o polling de N agentes e a regra de
severidade. Os dois clientes so divergem no desenho: um usa ANSI, o outro
usa pystray + tkinter. Mantendo a avaliacao num lugar so, "o que conta como
alerta" nao pode divergir entre as duas telas.

Stdlib pura - nenhuma dependencia. Compativel com Python 3.9+.
"""

from __future__ import annotations

import json
import os
import stat
import threading
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Iterable

__version__ = "2.0.0"

# ------------------------------------------------------------------ severidade
OK, AVISO, CRITICO, OFFLINE = 0, 1, 2, 3

NOMES_NIVEL = {OK: "ok", AVISO: "aviso", CRITICO: "critico", OFFLINE: "offline"}

# Fracao do valor critico que o proprio sensor reporta. Numa CPU com crit de
# 100C, amarelo comeca em 75C. Usar o crit do sensor em vez de um numero fixo
# e o que faz o mesmo config servir para hosts com hardware diferente.
FRAC_AVISO, FRAC_CRITICO = 0.75, 0.90
# Fallback para sensores que nao reportam crit.
TEMP_AVISO, TEMP_CRITICO = 70.0, 85.0

DISCO_AVISO, DISCO_CRITICO = 80.0, 90.0
# Acima de 90% de data o Proxmox comeca a falhar snapshot; acima de 95% o pool
# pode ficar irrecuperavel.
THIN_AVISO, THIN_CRITICO = 80.0, 90.0
MEM_AVISO, MEM_CRITICO = 90.0, 97.0
# PSI: fracao do tempo em que *alguma* tarefa ficou parada esperando o recurso.
PSI_AVISO, PSI_CRITICO = 40.0, 70.0


@dataclass
class Host:
    nome: str
    url: str
    token: str


@dataclass
class Config:
    hosts: list[Host]
    intervalo: float = 5.0
    timeout: float = 4.0
    extra: dict = field(default_factory=dict)


class ErroConfig(Exception):
    """Configuracao ausente ou invalida - a mensagem e para o usuario final."""


def carregar_config(caminho: Path) -> Config:
    """Le o config.json. Aceita o formato antigo de host unico da v1."""
    bruto = _ler_arquivo(caminho)

    # SYSMON_URL vence o arquivo inteiro: aponta o cliente para um host
    # qualquer sem editar nada. E o caminho mais rapido para isolar problema
    # ("e o config ou e o agente?").
    entradas = _host_do_ambiente() or bruto.get("hosts")

    if not entradas:
        # Formato v1: url e token soltos na raiz. Continua funcionando.
        if bruto.get("url"):
            entradas = [{"nome": bruto.get("nome"),
                         "url": bruto["url"], "token": bruto.get("token", "")}]
        else:
            raise ErroConfig(
                "nenhum host configurado.\n\n"
                'Esperado: {"hosts": [{"nome": "...", "url": "...", "token": "..."}]}'
                "\n\nOu defina SYSMON_URL e SYSMON_TOKEN no ambiente."
            )

    token_padrao = bruto.get("token", "")
    hosts: list[Host] = []
    for i, e in enumerate(entradas):
        if not isinstance(e, dict) or not e.get("url"):
            raise ErroConfig(f"host #{i + 1} sem 'url'.")
        nome = e.get("nome") or _apelido(e["url"])
        # Override por host: SYSMON_URL_<NOME> / SYSMON_TOKEN_<NOME>.
        url = os.environ.get(f"SYSMON_URL_{_sufixo(nome)}", "").strip() or e["url"]
        if not url.startswith(("http://", "https://")):
            raise ErroConfig(f"host '{nome}': url deve comecar com http:// ou https://")
        token = (os.environ.get(f"SYSMON_TOKEN_{_sufixo(nome)}", "").strip()
                 or e.get("token") or token_padrao)
        if not token:
            raise ErroConfig(f"host '{nome}' sem token.")
        hosts.append(Host(nome=nome, url=url, token=token))

    nomes = [h.nome for h in hosts]
    if len(set(nomes)) != len(nomes):
        raise ErroConfig(f"nomes de host repetidos: {nomes}")

    return Config(
        hosts=hosts,
        intervalo=float(bruto.get("intervalo", 5)),
        timeout=float(bruto.get("timeout", 4)),
        extra=bruto,
    )


def _ler_arquivo(caminho: Path) -> dict:
    """Le o config.json. Ausencia so e erro quando o ambiente nao supre o host."""
    try:
        bruto = json.loads(caminho.read_text(encoding="utf-8"))
    except FileNotFoundError:
        if _host_do_ambiente():
            return {}
        raise ErroConfig(
            f"config nao encontrado em:\n{caminho}\n\n"
            "Copie config.example.json para config.json e preencha,\n"
            "ou gere automaticamente com linux-agent/deploy.sh.\n"
            "Para um teste rapido, defina SYSMON_URL e SYSMON_TOKEN."
        ) from None
    except json.JSONDecodeError as e:
        raise ErroConfig(f"config invalido ({caminho}):\n\n{e}") from None

    if not isinstance(bruto, dict):
        raise ErroConfig("o config precisa ser um objeto JSON.")
    return bruto


def _host_do_ambiente() -> list[dict]:
    """SYSMON_URL/SYSMON_TOKEN definem um host unico com prioridade sobre o arquivo."""
    url = os.environ.get("SYSMON_URL", "").strip()
    if not url:
        return []
    return [{
        "nome": os.environ.get("SYSMON_NOME", "").strip() or None,
        "url": url,
        "token": os.environ.get("SYSMON_TOKEN", "").strip(),
    }]


def _sufixo(nome: str) -> str:
    """Nome de host -> sufixo de variavel de ambiente: 'pve-01' vira 'PVE_01'.

    Nome de variavel nao aceita hifen nem ponto em nenhum dos dois sistemas.
    """
    return "".join(c if c.isalnum() else "_" for c in nome).upper()


def _apelido(url: str) -> str:
    """Deriva um nome legivel da url quando o config nao traz um."""
    resto = url.split("://", 1)[-1]
    return resto.split("/", 1)[0].split(":", 1)[0]


def avisar_permissao(caminho: Path) -> str | None:
    """O config guarda tokens em texto claro; avisa se estiver legivel por todos.

    So faz sentido em POSIX - no Windows o modo do arquivo nao reflete a ACL.
    """
    if os.name != "posix":
        return None
    try:
        modo = caminho.stat().st_mode
    except OSError:
        return None
    if modo & (stat.S_IRGRP | stat.S_IROTH):
        return (f"{caminho} esta legivel por outros usuarios e contem tokens. "
                f"Corrija com: chmod 600 {caminho}")
    return None


# ------------------------------------------------------------------ coleta
@dataclass
class Estado:
    """Ultima leitura de um host. Sempre consistente: dados OU erro, nunca os dois."""
    dados: dict | None = None
    erro: str | None = None
    atualizado: float = 0.0
    falhas: int = 0


class Monitor:
    """Consulta um agente em laco, numa thread propria.

    Faz recuo exponencial em caso de falha: um host desligado nao deve gerar
    uma tentativa a cada 5s indefinidamente, ainda mais quando ha varios.
    """

    RECUO_MAX = 60.0

    def __init__(self, host: Host, intervalo: float, timeout: float,
                 ao_mudar: Callable[[str, Estado], None] | None = None) -> None:
        self.host = host
        self.intervalo = intervalo
        self.timeout = timeout
        self.ao_mudar = ao_mudar
        self._lock = threading.Lock()
        self._estado = Estado()
        self._parar = threading.Event()
        self._acordar = threading.Event()
        self._thread: threading.Thread | None = None

    # -- acesso -----------------------------------------------------------
    @property
    def estado(self) -> Estado:
        with self._lock:
            return self._estado

    def _definir(self, novo: Estado) -> None:
        with self._lock:
            anterior, self._estado = self._estado, novo
        if self.ao_mudar and nivel_do(anterior) != nivel_do(novo):
            try:
                self.ao_mudar(self.host.nome, novo)
            except Exception:  # noqa: BLE001 - callback do cliente nao derruba o poller
                pass

    # -- ciclo ------------------------------------------------------------
    def buscar(self) -> None:
        req = urllib.request.Request(
            self.host.url,
            headers={"Authorization": f"Bearer {self.host.token}",
                     "User-Agent": f"sysmon-cliente/{__version__}"},
        )
        anterior = self.estado
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:
                dados = json.loads(r.read().decode("utf-8"))
            if not isinstance(dados, dict):
                raise ValueError("resposta nao e um objeto JSON")
            self._definir(Estado(dados=dados, erro=None, atualizado=time.time()))
            return
        except urllib.error.HTTPError as e:
            erro = "token invalido" if e.code == 401 else f"HTTP {e.code}"
            # HTTPError tambem e um objeto de resposta: sem fechar, cada erro
            # deixa um socket pendurado - e um host offline erra em laco.
            e.close()
        except urllib.error.URLError as e:
            erro = f"sem conexao ({e.reason})"
        except (json.JSONDecodeError, ValueError) as e:
            erro = f"resposta invalida ({e})"
        except (TimeoutError, OSError) as e:
            erro = type(e).__name__

        self._definir(Estado(dados=None, erro=erro, atualizado=time.time(),
                             falhas=anterior.falhas + 1))

    def _espera(self) -> float:
        f = self.estado.falhas
        if f == 0:
            return self.intervalo
        return min(self.intervalo * (2 ** min(f, 6)), self.RECUO_MAX)

    def _rodar(self) -> None:
        while not self._parar.is_set():
            self.buscar()
            self._acordar.clear()
            # Acorda cedo se alguem pedir "atualizar agora".
            self._acordar.wait(self._espera())

    def iniciar(self) -> None:
        self._thread = threading.Thread(
            target=self._rodar, name=f"monitor-{self.host.nome}", daemon=True)
        self._thread.start()

    def atualizar_agora(self) -> None:
        self._acordar.set()

    def parar(self) -> None:
        self._parar.set()
        self._acordar.set()


class Frota:
    """Todos os hosts monitorados juntos."""

    def __init__(self, cfg: Config,
                 ao_mudar: Callable[[str, Estado], None] | None = None) -> None:
        self.cfg = cfg
        self.monitores = [Monitor(h, cfg.intervalo, cfg.timeout, ao_mudar)
                          for h in cfg.hosts]

    def iniciar(self) -> None:
        for m in self.monitores:
            m.iniciar()

    def parar(self) -> None:
        for m in self.monitores:
            m.parar()

    def atualizar_agora(self) -> None:
        for m in self.monitores:
            m.atualizar_agora()

    def estados(self) -> list[tuple[Host, Estado]]:
        return [(m.host, m.estado) for m in self.monitores]

    def esperar_primeira_leitura(self, limite: float = 3.0) -> None:
        """Da um tempo para a primeira rodada antes de desenhar a tela vazia."""
        fim = time.monotonic() + limite
        while time.monotonic() < fim:
            if all(m.estado.atualizado for m in self.monitores):
                return
            time.sleep(0.1)

    # -- visao agregada ---------------------------------------------------
    def pior_nivel(self) -> int:
        return max((nivel_do(e) for _, e in self.estados()), default=OK)

    def alertas(self) -> list[str]:
        """Todos os alertas da frota, ja prefixados com o nome do host."""
        out: list[str] = []
        for host, estado in self.estados():
            for a in avaliar(estado)[1]:
                out.append(f"{host.nome}: {a}")
        return out


# ------------------------------------------------------------------ avaliacao
def nivel_temp(c: float | None, crit: float | None) -> int:
    if c is None:
        return OK
    aviso = crit * FRAC_AVISO if crit else TEMP_AVISO
    critico = crit * FRAC_CRITICO if crit else TEMP_CRITICO
    if c >= critico:
        return CRITICO
    if c >= aviso:
        return AVISO
    return OK


def _faixa(v: float | None, aviso: float, critico: float) -> int:
    if v is None:
        return OK
    if v >= critico:
        return CRITICO
    if v >= aviso:
        return AVISO
    return OK


def avaliar(estado: Estado) -> tuple[int, list[str]]:
    """Devolve (nivel, alertas) de um host.

    E a unica definicao de "isso merece sua atencao" no projeto. Tanto a cor do
    icone quanto as linhas do dashboard saem daqui.
    """
    if estado.erro or not estado.dados:
        return OFFLINE, [estado.erro or "sem dados"]

    d = estado.dados
    nivel, alertas = OK, []

    def marcar(n: int, msg: str | None = None) -> None:
        nonlocal nivel
        nivel = max(nivel, n)
        if msg and n >= AVISO:
            alertas.append(msg)

    temp, crit = d.get("cpu_temp"), d.get("cpu_crit")
    n = nivel_temp(temp, crit)
    marcar(n, f"CPU em {temp:.0f}C" if n >= AVISO and temp is not None else None)

    mem = d.get("mem") or {}
    n = _faixa(mem.get("percent"), MEM_AVISO, MEM_CRITICO)
    marcar(n, f"RAM em {mem['percent']:.0f}%" if n >= AVISO else None)

    for disco in d.get("discos") or []:
        n = _faixa(disco.get("percent"), DISCO_AVISO, DISCO_CRITICO)
        marcar(n, f"disco {disco['mount']} em {disco['percent']:.0f}%" if n >= AVISO else None)
        # Inode esgotado quebra igual a disco cheio, e o df -h nao mostra.
        n = _faixa(disco.get("inodes_percent"), 90.0, 97.0)
        marcar(n, f"inodes de {disco['mount']} em {disco['inodes_percent']:.0f}%"
               if n >= AVISO else None)

    for tp in d.get("thinpools") or []:
        n = _faixa(tp.get("data_percent"), THIN_AVISO, THIN_CRITICO)
        marcar(n, f"thin pool {tp['nome']} em {tp['data_percent']:.0f}%" if n >= AVISO else None)
        n = _faixa(tp.get("meta_percent"), THIN_AVISO, THIN_CRITICO)
        marcar(n, f"metadata de {tp['nome']} em {tp['meta_percent']:.0f}%" if n >= AVISO else None)

    for r in d.get("raid") or []:
        if r.get("degradado"):
            marcar(CRITICO, f"RAID {r['nome']} degradado ({r.get('discos', '?')})")

    # PSI 'some' alto significa que ha tarefa parada esperando o recurso - e o
    # sinal que aparece antes de o host ficar visivelmente lento.
    psi = d.get("pressure") or {}
    for recurso, rotulo in (("io", "IO"), ("cpu", "CPU"), ("memory", "memoria")):
        v = (psi.get(recurso) or {}).get("some_avg60")
        n = _faixa(v, PSI_AVISO, PSI_CRITICO)
        marcar(n, f"pressao de {rotulo} em {v:.0f}%" if n >= AVISO else None)

    # Dado velho: o agente responde, mas a coleta dele parou.
    idade, intervalo = d.get("idade_s"), d.get("intervalo_s") or 5
    if idade is not None and idade > max(4 * intervalo, 30):
        marcar(AVISO, f"coleta parada ha {idade:.0f}s")

    return nivel, alertas


def nivel_do(estado: Estado) -> int:
    return avaliar(estado)[0]


# ------------------------------------------------------------------ formato
def fmt_bytes(n: float | None) -> str:
    if n is None:
        return "--"
    for unidade in ("B", "K", "M", "G", "T"):
        if abs(n) < 1024:
            return f"{n:.0f}{unidade}"
        n /= 1024
    return f"{n:.1f}P"


def fmt_bps(n: float | None) -> str:
    if n is None:
        return "--"
    return fmt_bytes(n) + "/s"


def fmt_uptime(s: float | None) -> str:
    if not s:
        return "--"
    s = int(s)
    d, resto = divmod(s, 86400)
    h, m = divmod(resto // 60, 60)
    if d:
        return f"{d}d{h}h"
    return f"{h}h{m:02d}m"


def fmt_pct(v: float | None, casas: int = 0) -> str:
    return "--" if v is None else f"{v:.{casas}f}%"


def fmt_temp(v: float | None) -> str:
    return "--" if v is None else f"{v:.0f}C"


def resumo_linhas(host: Host, estado: Estado) -> list[str]:
    """Bloco de texto de um host, usado no overlay e no menu do tray."""
    if estado.erro or not estado.dados:
        return [f"{host.nome}: offline - {estado.erro or 'sem dados'}"]

    d = estado.dados
    linhas = [f"{host.nome}  ({d.get('host', '?')})"]

    temp, crit = d.get("cpu_temp"), d.get("cpu_crit")
    linha = f"  CPU  {fmt_pct(d.get('cpu_percent'))}"
    if temp is not None:
        linha += f"  {fmt_temp(temp)}" + (f"/{crit:.0f}C" if crit else "")
    linhas.append(linha)

    if load := d.get("load"):
        linhas.append(f"  Load {load[0]:.2f} {load[1]:.2f} {load[2]:.2f}"
                      f"  ({d.get('cpus', '?')} cpus)")

    mem = d.get("mem") or {}
    if mem.get("percent") is not None:
        linhas.append(f"  RAM  {fmt_pct(mem['percent'])}  "
                      f"{fmt_bytes(mem.get('usado'))}/{fmt_bytes(mem.get('total'))}")
    if mem.get("swap_percent"):
        linhas.append(f"  Swap {fmt_pct(mem['swap_percent'])}")

    for disco in (d.get("discos") or [])[:3]:
        linhas.append(f"  {disco['mount']:<14} {fmt_pct(disco['percent'])}")
    for tp in d.get("thinpools") or []:
        linhas.append(f"  {tp['nome']:<14} {fmt_pct(tp['data_percent'])}"
                      f" (meta {fmt_pct(tp['meta_percent'])})")

    # So interfaces com link: as demais so ocupam espaco com zeros.
    ativas = [n for n in (d.get("net") or []) if n.get("up")]
    for n in ativas[:2]:
        linhas.append(f"  {n['iface']:<14} v{fmt_bps(n.get('rx_bps'))}"
                      f" ^{fmt_bps(n.get('tx_bps'))}")

    if g := d.get("guests"):
        linhas.append(f"  VMs {g.get('qemu', 0)}   CTs {g.get('lxc', 0)}")
    linhas.append(f"  Up   {fmt_uptime(d.get('uptime_s'))}")
    return linhas


def primeira_temp(estados: Iterable[tuple[Host, Estado]]) -> tuple[float | None, float | None]:
    """Temperatura do host mais quente da frota, com o crit dele.

    O icone da bandeja mostra um numero so; o util e o pior caso.
    """
    melhor: tuple[float, float | None] | None = None
    for _, e in estados:
        if e.erro or not e.dados:
            continue
        t, c = e.dados.get("cpu_temp"), e.dados.get("cpu_crit")
        if t is None:
            continue
        if melhor is None or t > melhor[0]:
            melhor = (t, c)
    return melhor if melhor else (None, None)


def como_dict(frota: Frota) -> dict[str, Any]:
    """Snapshot da frota inteira, para --json e para o 'copiar JSON' do tray."""
    return {
        "ts": time.time(),
        "hosts": {
            host.nome: (
                {"erro": estado.erro} if estado.erro or not estado.dados
                else estado.dados
            )
            for host, estado in frota.estados()
        },
    }
