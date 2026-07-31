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

__version__ = "4.1.0"

# ------------------------------------------------------------------ severidade
OK, AVISO, CRITICO, OFFLINE = 0, 1, 2, 3

NOMES_NIVEL = {OK: "ok", AVISO: "aviso", CRITICO: "critico", OFFLINE: "offline"}

@dataclass
class Limiares:
    """Onde cada medida vira aviso e onde vira critico.

    Vem do config.json; os padroes sao os que ja valiam antes de isso ser
    configuravel. Cada par e (aviso, critico).

    A fracao de temperatura da CPU e sobre o valor CRITICO que o proprio
    sensor reporta - numa CPU com crit 100C, 0.75 significa aviso em 75C. E o
    que faz o mesmo limiar servir para hardwares com limites diferentes; o par
    fixo so entra quando o sensor nao informa crit.
    """
    temp_frac: tuple[float, float] = (0.75, 0.90)
    temp_fixa: tuple[float, float] = (70.0, 85.0)
    disco: tuple[float, float] = (80.0, 90.0)
    inodes: tuple[float, float] = (90.0, 97.0)
    thinpool: tuple[float, float] = (80.0, 90.0)
    ram: tuple[float, float] = (90.0, 97.0)
    temp_disco: tuple[float, float] = (60.0, 70.0)
    desgaste: tuple[float, float] = (80.0, 90.0)
    psi: tuple[float, float] = (40.0, 70.0)

    # Partições de tamanho fixo cujo percentual nao diz nada util: /boot enche
    # de kernel antigo e a ESP vive quase cheia por natureza. Alertar nelas
    # ensina a ignorar alerta.
    ignorar_mounts: tuple[str, ...] = ("/boot", "/boot/efi")

    # Um unico setor realocado ja e midia se degradando.
    realocados_aviso: int = 1
    # Multiplo do intervalo de coleta a partir do qual o dado e velho demais.
    idade_fator: float = 4.0

    CAMPOS = (
        ("temp_frac",  "temperatura da cpu (fracao do critico do sensor)", "frac"),
        ("temp_fixa",  "temperatura da cpu sem critico no sensor (°C)",    "c"),
        ("temp_disco", "temperatura de disco (°C)",                        "c"),
        ("ram",        "uso de memoria (%)",                               "pct"),
        ("disco",      "uso de filesystem (%)",                            "pct"),
        ("inodes",     "uso de inodes (%)",                                "pct"),
        ("thinpool",   "thin pool LVM (%)",                                "pct"),
        ("desgaste",   "vida consumida do ssd (%)",                        "pct"),
        ("psi",        "pressao PSI (%)",                                  "pct"),
    )

    @classmethod
    def de(cls, bruto: dict) -> "Limiares":
        """Le do config, ignorando entrada malformada em vez de quebrar."""
        vals = {}
        alertas = bruto.get("alertas") or {}
        for nome, _rot, _un in cls.CAMPOS:
            par = alertas.get(nome)
            try:
                a, c = float(par[0]), float(par[1])
            except (TypeError, ValueError, IndexError, KeyError):
                continue
            vals[nome] = (a, c)
        ign = bruto.get("ignorar_mounts")
        if isinstance(ign, list):
            vals["ignorar_mounts"] = tuple(str(x) for x in ign)
        return cls(**vals)

    def como_dict(self) -> dict:
        return {nome: list(getattr(self, nome)) for nome, _r, _u in self.CAMPOS}


PADRAO = Limiares()


def discos_relevantes(discos, lim: "Limiares | None" = None) -> list:
    """Filtra os filesystems que nao vale a pena avaliar por espaco."""
    ign = set((lim or PADRAO).ignorar_mounts)
    return [d for d in (discos or []) if d.get("mount") not in ign]


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
    limiares: Limiares = field(default_factory=Limiares)


class ErroConfig(Exception):
    """Configuracao ausente ou invalida - a mensagem e para o usuario final."""


# Onde os clientes procuram o config quando nao recebem --config.
CAMINHOS_PADRAO = [
    Path("hosts.json"),
    Path("config.json"),
    Path("~/.config/sysmon/hosts.json").expanduser(),
    Path("/etc/sysmon/hosts.json"),
]


def achar_config(indicado: str | None = None) -> Path:
    """Resolve o caminho do config: --config, depois SYSMON_CONFIG, depois os padroes."""
    if indicado:
        return Path(indicado).expanduser()
    if do_ambiente := os.environ.get("SYSMON_CONFIG"):
        return Path(do_ambiente).expanduser()
    for c in CAMINHOS_PADRAO:
        if c.is_file():
            return c
    return CAMINHOS_PADRAO[0]


def carregar_config(caminho: Path) -> Config:
    """Le o config.json. Aceita o formato antigo de host unico da v1.

    O ARQUIVO MANDA. Nada do ambiente sobrescreve um valor presente no
    config.json - o ambiente so preenche o que o arquivo nao definiu. Isso e
    deliberado: variavel de ambiente e invisivel no dia a dia, e um SYSMON_URL
    esquecido de um teste antigo sequestraria a configuracao inteira sem deixar
    pista nenhuma de por que o cliente esta olhando para o host errado.
    """
    return carregar_config_de(_ler_arquivo(caminho))


def carregar_config_de(bruto: dict) -> Config:
    """Valida um dicionario de configuracao ja lido.

    Separado de carregar_config para a tela de configuracao poder validar o
    que o usuario preencheu ANTES de gravar - salvar algo invalido deixaria o
    programa sem subir no proximo arranque.
    """
    entradas = bruto.get("hosts")
    if not entradas:
        # Formato v1: url e token soltos na raiz. Continua funcionando.
        if bruto.get("url"):
            entradas = [{"nome": bruto.get("nome"),
                         "url": bruto["url"], "token": bruto.get("token", "")}]
        else:
            # Ultimo recurso, so quando o arquivo nao trouxe host nenhum.
            entradas = _host_do_ambiente()
        if not entradas:
            raise ErroConfig(
                "nenhum host configurado.\n\n"
                'Esperado: {"hosts": [{"nome": "...", "url": "...", "token": "..."}]}'
            )

    token_padrao = bruto.get("token", "")
    hosts: list[Host] = []
    for i, e in enumerate(entradas):
        if not isinstance(e, dict) or not e.get("url"):
            raise ErroConfig(f"host #{i + 1} sem 'url'.")
        nome = e.get("nome") or _apelido(e["url"])
        url = e["url"]
        if not url.startswith(("http://", "https://")):
            raise ErroConfig(f"host '{nome}': url deve comecar com http:// ou https://")
        # O ambiente entra so como ultimo recurso, para quem prefere nao deixar
        # token em texto claro no arquivo. Token presente no JSON sempre vence.
        token = (e.get("token") or token_padrao
                 or os.environ.get(f"SYSMON_TOKEN_{_sufixo(nome)}", "").strip())
        if not token:
            raise ErroConfig(
                f"host '{nome}' sem token.\n\n"
                f"Preencha \"token\" no config, ou defina "
                f"SYSMON_TOKEN_{_sufixo(nome)} no ambiente."
            )
        hosts.append(Host(nome=nome, url=url, token=token))

    nomes = [h.nome for h in hosts]
    if len(set(nomes)) != len(nomes):
        raise ErroConfig(f"nomes de host repetidos: {nomes}")

    return Config(
        hosts=hosts,
        intervalo=float(bruto.get("intervalo", 5)),
        timeout=float(bruto.get("timeout", 4)),
        extra=bruto,
        limiares=Limiares.de(bruto),
    )


def salvar_config(caminho: Path, cfg_bruto: dict) -> None:
    """Grava o config.json, preservando as chaves que a tela nao edita.

    Escreve num temporario e troca: uma queda no meio da gravacao nao pode
    deixar o usuario sem configuracao nenhuma.
    """
    caminho = Path(caminho)
    caminho.parent.mkdir(parents=True, exist_ok=True)
    tmp = caminho.with_suffix(caminho.suffix + ".tmp")
    tmp.write_text(json.dumps(cfg_bruto, indent=2, ensure_ascii=False) + "\n",
                   encoding="utf-8")
    os.replace(tmp, caminho)
    if os.name == "posix":
        # Contem os tokens de todos os hosts.
        try:
            caminho.chmod(0o600)
        except OSError:
            pass


def testar_host(url: str, token: str, timeout: float = 4.0) -> tuple[bool, str]:
    """Bate no agente e devolve (ok, mensagem) - usado pela tela de configuracao.

    Sem isso, configurar e adivinhar: errar um digito no IP ou colar meio token
    da exatamente o mesmo resultado visual de host desligado.
    """
    if not url.startswith(("http://", "https://")):
        return False, "a url precisa comecar com http:// ou https://"
    m = Monitor(Host("teste", url, token), intervalo=1, timeout=timeout)
    m.buscar()
    e = m.estado
    if e.erro:
        return False, e.erro
    d = e.dados or {}
    partes = [d.get("host") or "?"]
    if (d.get("so") or {}).get("nome"):
        partes.append(d["so"]["nome"])
    return True, " · ".join(partes)


def _ler_arquivo(caminho: Path) -> dict:
    """Le o config.json. Ausencia so nao e erro quando o ambiente supre o host."""
    try:
        bruto = json.loads(caminho.read_text(encoding="utf-8"))
    except FileNotFoundError:
        if _host_do_ambiente():
            return {}
        raise ErroConfig(
            f"config nao encontrado em:\n{caminho}\n\n"
            "Copie config.example.json para config.json e preencha,\n"
            "ou gere automaticamente com linux-agent/deploy.sh."
        ) from None
    except json.JSONDecodeError as e:
        raise ErroConfig(f"config invalido ({caminho}):\n\n{e}") from None

    if not isinstance(bruto, dict):
        raise ErroConfig("o config precisa ser um objeto JSON.")
    return bruto


def _host_do_ambiente() -> list[dict]:
    """Host vindo de SYSMON_URL/SYSMON_TOKEN.

    Usado APENAS quando o config.json nao define host nenhum - serve para rodar
    o dashboard sem criar arquivo, nao para sobrescrever o que esta no arquivo.
    """
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
        self.ao_mudar = ao_mudar
        self.monitores = [Monitor(h, cfg.intervalo, cfg.timeout, ao_mudar)
                          for h in cfg.hosts]
        self._rodando = False

    def iniciar(self) -> None:
        self._rodando = True
        for m in self.monitores:
            m.iniciar()

    def trocar(self, cfg: Config) -> None:
        """Substitui a configuracao sem reiniciar o programa.

        E o que permite salvar a configuracao pela tela e ver o resultado na
        hora, em vez de pedir para fechar e abrir.
        """
        antigos = self.monitores
        self.cfg = cfg
        self.monitores = [Monitor(h, cfg.intervalo, cfg.timeout, self.ao_mudar)
                          for h in cfg.hosts]
        if self._rodando:
            for m in self.monitores:
                m.iniciar()
        for m in antigos:
            m.parar()

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
        return max((avaliar(e, self.cfg.limiares)[0] for _, e in self.estados()),
                   default=OK)

    def alertas(self) -> list[str]:
        """Todos os alertas da frota, ja prefixados com o nome do host."""
        out: list[str] = []
        for host, estado in self.estados():
            for a in avaliar(estado, self.cfg.limiares)[1]:
                out.append(f"{host.nome}: {a}")
        return out


# ------------------------------------------------------------------ avaliacao
def nivel_temp(c: float | None, crit: float | None,
               lim: Limiares | None = None) -> int:
    lim = lim or PADRAO
    if c is None:
        return OK
    aviso = crit * lim.temp_frac[0] if crit else lim.temp_fixa[0]
    critico = crit * lim.temp_frac[1] if crit else lim.temp_fixa[1]
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


def avaliar(estado: Estado, lim: Limiares | None = None) -> tuple[int, list[str]]:
    """Devolve (nivel, alertas) de um host.

    E a unica definicao de "isso merece sua atencao" no projeto. Tanto a cor do
    icone quanto as linhas do dashboard saem daqui.
    """
    lim = lim or PADRAO
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
    n = nivel_temp(temp, crit, lim)
    marcar(n, f"CPU em {temp:.0f}C" if n >= AVISO and temp is not None else None)

    mem = d.get("mem") or {}
    n = _faixa(mem.get("percent"), *lim.ram)
    marcar(n, f"RAM em {mem['percent']:.0f}%" if n >= AVISO else None)

    # Particoes de tamanho fixo ficam de fora: ver Limiares.ignorar_mounts.
    for disco in discos_relevantes(d.get("discos"), lim):
        n = _faixa(disco.get("percent"), *lim.disco)
        marcar(n, f"disco {disco['mount']} em {disco['percent']:.0f}%" if n >= AVISO else None)
        # Inode esgotado quebra igual a disco cheio, e o df -h nao mostra.
        n = _faixa(disco.get("inodes_percent"), *lim.inodes)
        marcar(n, f"inodes de {disco['mount']} em {disco['inodes_percent']:.0f}%"
               if n >= AVISO else None)

    for tp in d.get("thinpools") or []:
        n = _faixa(tp.get("data_percent"), *lim.thinpool)
        marcar(n, f"thin pool {tp['nome']} em {tp['data_percent']:.0f}%" if n >= AVISO else None)
        n = _faixa(tp.get("meta_percent"), *lim.thinpool)
        marcar(n, f"metadata de {tp['nome']} em {tp['meta_percent']:.0f}%" if n >= AVISO else None)

    for r in d.get("raid") or []:
        if r.get("degradado"):
            marcar(CRITICO, f"RAID {r['nome']} degradado ({r.get('discos', '?')})")

    for b in d.get("blocos") or []:
        dev = b.get("dev", "?")
        # NVMe faz throttling termico por volta de 70C; acima disso o disco
        # fica lento e a vida util encurta.
        n = _faixa(b.get("temp_c"), *lim.temp_disco)
        marcar(n, f"disco {dev} em {b['temp_c']:.0f}C" if n >= AVISO else None)

        smart = b.get("smart") or {}
        if smart.get("saude") == "falha":
            marcar(CRITICO, f"SMART reprovou o disco {dev}")
        n = _faixa(smart.get("desgaste_percent"), *lim.desgaste)
        marcar(n, f"disco {dev} com {smart['desgaste_percent']:.0f}% de vida consumida"
               if n >= AVISO else None)
        # Um setor realocado ja significa midia se degradando: nao espera piorar.
        if (smart.get("realocados") or 0) >= lim.realocados_aviso:
            marcar(AVISO, f"disco {dev} com {smart['realocados']} setores realocados")

    # PSI 'some' alto significa que ha tarefa parada esperando o recurso - e o
    # sinal que aparece antes de o host ficar visivelmente lento.
    psi = d.get("pressure") or {}
    for recurso, rotulo in (("io", "IO"), ("cpu", "CPU"), ("memory", "memoria")):
        v = (psi.get(recurso) or {}).get("some_avg60")
        n = _faixa(v, *lim.psi)
        marcar(n, f"pressao de {rotulo} em {v:.0f}%" if n >= AVISO else None)

    # Dado velho: o agente responde, mas a coleta dele parou.
    idade, intervalo = d.get("idade_s"), d.get("intervalo_s") or 5
    if idade is not None and idade > max(lim.idade_fator * intervalo, 30):
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

    for disco in discos_relevantes(d.get("discos"))[:3]:
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
    """Snapshot da frota inteira, para o --json do terminal e para o tray.

    A severidade vai calculada junto de proposito: quem consome nao reimplementa
    avaliar(). Manter "o que conta como alerta" num lugar so vale mais do que
    economizar alguns bytes no snapshot.

    Nao inclui token: o snapshot leva telemetria, nunca credencial.
    """
    hosts = []
    for host, estado in frota.estados():
        nivel, alertas = avaliar(estado, frota.cfg.limiares)
        hosts.append({
            "nome": host.nome,
            "url": host.url,
            "nivel": nivel,
            "nivel_nome": NOMES_NIVEL[nivel],
            "alertas": alertas,
            "erro": estado.erro,
            "atualizado": estado.atualizado,
            "dados": estado.dados,
        })
    return {
        "ts": time.time(),
        "intervalo": frota.cfg.intervalo,
        "limiares": frota.cfg.limiares.como_dict(),
        "ignorar_mounts": list(frota.cfg.limiares.ignorar_mounts),
        "pior_nivel": max((h["nivel"] for h in hosts), default=OK),
        "hosts": hosts,
    }
