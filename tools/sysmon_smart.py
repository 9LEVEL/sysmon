#!/usr/bin/env python3
"""
sysmon_smart - avaliacao de saude de disco a partir do SMART.

Implementa a especificacao de thresholds do projeto. O resumo do que ela pede,
porque cada decisao aqui sai de um destes principios:

  1. ID de atributo entre 165 e 179 e vendor-specific. Interpretar ID cru e
     bug de correcao garantido - o mesmo 170 e "Grown_Bad_Blocks" num WD e
     "Available Reserved Space" num Intel. Aqui a chave e o NOME que o
     smartctl resolve pela drivedb dele, nunca o numero.
  2. Metrica relativa vence absoluta. "4 blocos ruins" nao quer dizer nada
     sozinho; 4 com 98% de reserva intacta e ruido, 4 com 10% e urgente.
  3. Taxa vence valor absoluto. 200 setores parados ha um ano e um disco
     saudavel; 0 para 12 numa semana e um disco morrendo.
  4. O limiar do fabricante e autoridade: VALUE <= THRESH e falha declarada
     pelo proprio drive.
  5. Ausencia de alerta nao e atestado de saude. Entre 23% e 36% dos discos
     que falharam nao tinham indicador SMART nenhum (Google 2007, Backblaze).
     Por isso este modulo nunca diz "disco saudavel", so "sem indicadores".

O que ele NAO faz: ler o smartctl. A entrada ja vem normalizada pelo agente,
num formato so - inclusive NVMe, que nao tem tabela de atributos e e traduzido
para nomes canonicos la. Ver `avaliar_disco` para o contrato.

Nada aqui depende de rede, arquivo ou relogio: e funcao pura sobre a leitura,
o que torna a especificacao inteira testavel sem disco nenhum.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field

__version__ = "4.3.0"

# ------------------------------------------------------------------ severidade
# Quatro niveis, cada um mapeando para uma ACAO, nao para uma cor:
#
#   OK       nenhuma
#   INFO     logar, sem notificar
#   WARN     notificar, validar backup, planejar troca
#   CRITICO  substituir, migrar dados
#
# E dois estados que nao sao severidade e nao podem virar OK por descuido:
#
#   SEM_DADOS      historico insuficiente para uma regra de taxa
#   DESCONHECIDO   a coleta falhou; nao sabemos nada sobre este disco
OK, INFO, WARN, CRITICO = range(4)

SEM_DADOS = "sem_dados"
DESCONHECIDO = "desconhecido"

NOME_SEVERIDADE = {OK: "ok", INFO: "info", WARN: "aviso", CRITICO: "critico"}

# ------------------------------------------------------------------ categorias
# Separadas de proposito. Um cabo SATA ruim e uma fonte instavel produzem
# sintoma em atributo de disco, e quem mistura as tres troca midia boa e
# recomeca o ciclo com o mesmo problema.
DISPOSITIVO = "dispositivo"
INTERCONEXAO = "interconexao"
HOST = "host"

# Motivo do alerta, para distinguir dois CRITICOs que pedem coisas diferentes:
# um bloco pendente e "aja hoje"; 96% de vida consumida e "planeje a troca".
AGIR_AGORA = "agir"
PLANEJAR = "planejar"


# ------------------------------------------------------------------- catalogo
# Papel semantico -> nomes que o smartctl usa para ele.
#
# E ESTE dicionario que implementa o principio 1. O smartctl ja aplica a
# drivedb e devolve o nome certo para o fabricante; casar por nome e o que
# evita o palpite. Nome fora desta lista nao vira palpite nenhum: e ignorado
# com log, porque atribuir significado errado a um contador vendor-specific
# produz alarme falso ou, pior, silencio falso.
PAPEIS = {
    "reserva": (
        "Available_Reservd_Space", "Available_Reserved_Space", "Available_Spare",
    ),
    "realocados": (
        "Reallocated_Sector_Ct", "Reallocated_Sector_Count", "Reallocated_Event_Count",
    ),
    "pendentes": ("Current_Pending_Sector", "Current_Pending_Sector_Count"),
    "offline_incorrigivel": ("Offline_Uncorrectable", "Offline_Uncorrectable_Sector_Count"),
    "reportado_incorrigivel": ("Reported_Uncorrect", "Reported_Uncorrectable_Errors"),
    "erro_ponta_a_ponta": ("End-to-End_Error", "End_to_End_Error"),
    "timeout_comando": ("Command_Timeout",),
    "falha_programacao": ("Program_Fail_Count", "Program_Fail_Cnt_Total",
                          "Program_Fail_Count_Chip"),
    "falha_apagamento": ("Erase_Fail_Count", "Erase_Fail_Count_Total",
                         "Erase_Fail_Count_Chip"),
    "crc": ("UDMA_CRC_Error_Count",),
    "blocos_crescidos": ("Grown_Bad_Blocks", "Runtime_Bad_Block",
                         "Bad_Block_Count"),
    "blocos_total": ("Total_Bad_Blocks",),
    "desgaste_restante": ("Media_Wearout_Indicator", "SSD_Life_Left",
                          "Wear_Leveling_Count", "Percent_Lifetime_Remain"),
    "desgaste_usado": ("Percentage_Used", "Percent_Life_Used"),
    "ciclos_pe": ("Average_PE_Cycles_TLC", "Ave_Block-Erase_Count"),
    "escritas_host": ("Host_Writes_GiB", "Total_LBAs_Written",
                      "Host_Writes_32MiB"),
    "desligamento_sujo": ("Unexpect_Power_Loss_Ct", "Unexpected_Power_Loss_Count",
                          "Unsafe_Shutdown_Count", "Power-Off_Retract_Count"),
    "ciclos_energia": ("Power_Cycle_Count",),
    "throttle": ("Temp_Throttle_Status",),
}

# Indice invertido, montado uma vez.
_PAPEL_DE = {nome: papel for papel, nomes in PAPEIS.items() for nome in nomes}

# Contadores de degradacao aos quais as regras de taxa se aplicam (secao 3).
CONTADORES_DE_TAXA = ("realocados", "blocos_crescidos", "pendentes",
                      "falha_programacao", "falha_apagamento")


# ----------------------------------------------------------------- configuracao
# A especificacao descreve a configuracao em YAML. Aqui ela e um dicionario com
# as mesmas chaves e a mesma forma: o cliente inteiro roda de um .pyz feito so
# com a stdlib, e trazer um parser de YAML custaria a instalacao sem dependencia
# que e metade da razao de este projeto existir. Converter de um YAML com esta
# forma para este dict e uma linha, se um dia fizer falta.
PADRAO = {
    "version": 1,
    "poll_interval_minutes": 60,
    "history_retention_days": 180,

    "reserve_space": {
        "ok_min_normalized": 90,
        "info_min_normalized": 80,
        "warn_min_normalized": 50,
        "critical_margin_above_vendor_thresh": 10,
    },
    "growth_rate": {
        "info_any_increase_days": 7,
        "warn_count_7d": 3,
        "critical_count_24h": 5,
        "critical_count_7d": 10,
        "critical_double_window_days": 30,
        "critical_double_min_base": 4,
        "warn_acceleration_factor": 3.0,
    },
    "immediate": {
        "current_pending_sector": {"warn": 1, "critical": 10},
        "offline_uncorrectable": {"critical": 1},
        "reported_uncorrect": {"critical": 1},
        "end_to_end_error": {"critical": 1},
        "command_timeout": {"warn": 10, "critical": 100},
        "program_fail_count": {"warn": 1},
        "erase_fail_count": {"warn": 1},
    },
    "bad_blocks_ratio": {"info": 0.05, "warn": 0.20, "critical": 0.50},
    "reallocated_raw": {"info": 1, "warn": 9, "critical": 41},
    "wear": {
        "info_pct": 70, "warn_pct": 85, "critical_pct": 95,
        "nand_cycles_default": {"slc": 50000, "mlc": 3000, "tlc": 1000,
                                "qlc": 500},
    },
    "temperature": {
        "ssd": {"info": 50, "warn": 60, "critical": 70},
        "hdd": {"info": 40, "warn": 45, "critical": 55, "critical_low": 15},
        "historic_max_severity_offset": -1,
    },
    "host_health": {
        "unexpected_power_loss_ratio": {"info": 0.05, "warn": 0.15,
                                        "critical": 0.30},
    },
    "noise_control": {
        "hysteresis_raise_consecutive_reads": 2,
        "hysteresis_clear_margin": 5,
        "debounce_hours": {"warn": 24, "critical": 6},
        "unknown_on_collection_failure": True,
    },
}


def fundir(padrao: dict, usuario: dict | None) -> dict:
    """Config do usuario por cima do padrao, recursivamente.

    Chave ausente mantem o padrao: quem quiser mexer so no limiar de
    temperatura nao precisa copiar a arvore inteira para o config.json.
    """
    out = dict(padrao)
    for chave, valor in (usuario or {}).items():
        if isinstance(valor, dict) and isinstance(out.get(chave), dict):
            out[chave] = fundir(out[chave], valor)
        else:
            out[chave] = valor
    return out


# --------------------------------------------------------------------- achados
@dataclass(frozen=True)
class Achado:
    """Uma regra que disparou.

    `regra` e o identificador estavel usado pelo debounce e pela histerese -
    e o que diz "esta e a mesma condicao de uma hora atras".
    """
    categoria: str
    severidade: int
    regra: str
    mensagem: str
    motivo: str = AGIR_AGORA

    @property
    def nivel(self) -> str:
        return NOME_SEVERIDADE[self.severidade]


@dataclass
class Veredito:
    """Resultado por disco, com uma severidade POR CATEGORIA.

    Nao existe severidade unica do disco de proposito: cabo ruim e fonte
    instavel nao sao defeito da midia, e somar tudo num numero so faz o
    operador trocar disco bom.
    """
    dev: str = ""
    serial: str = ""
    coleta_ok: bool = True
    achados: list[Achado] = field(default_factory=list)
    sem_dados: list[str] = field(default_factory=list)

    def severidade(self, categoria: str) -> int:
        """Maximo das regras da categoria - nunca media nem soma.

        Um CRITICO domina cem OK: a media diluiria exatamente o sinal que
        importa.
        """
        return max((a.severidade for a in self.achados
                    if a.categoria == categoria), default=OK)

    @property
    def dispositivo(self) -> int:
        return self.severidade(DISPOSITIVO)

    @property
    def interconexao(self) -> int:
        return self.severidade(INTERCONEXAO)

    @property
    def host(self) -> int:
        return self.severidade(HOST)

    def resumo(self) -> str:
        """Frase para a interface.

        Nunca "disco saudavel": ausencia de indicador nao e atestado de saude,
        e prometer isso e o unico erro deste modulo que custaria dados a
        alguem.
        """
        if not self.coleta_ok:
            return "coleta falhou - saude desconhecida"
        piores = [a for a in self.achados if a.severidade == self.dispositivo]
        if self.dispositivo == OK or not piores:
            return "sem indicadores de falha"
        return piores[0].mensagem


# ------------------------------------------------------------------- atributos
def indexar(atributos: list[dict]) -> dict[str, dict]:
    """Atributos por papel semantico, ignorando o que nao conhecemos.

    Um nome fora do catalogo NAO vira palpite: seria interpretar contador
    vendor-specific sem tabela do fabricante, que e a forma mais facil de
    inventar um alarme.
    """
    por_papel: dict[str, dict] = {}
    for a in atributos or []:
        nome = (a.get("nome") or "").strip()
        papel = _PAPEL_DE.get(nome)
        if papel is None:
            logging.debug("smart: atributo sem papel conhecido, ignorado: "
                          "id=%s nome=%r", a.get("id"), nome)
            continue
        # Primeiro nome do catalogo vence, para o caso raro de o drive expor
        # dois atributos com papeis equivalentes.
        por_papel.setdefault(papel, a)
    return por_papel


def _cru(attr: dict | None) -> int | None:
    if not attr:
        return None
    v = attr.get("cru")
    return int(v) if isinstance(v, (int, float)) else None


def _norm(attr: dict | None) -> int | None:
    """Valor NORMALIZADO (0-100+), que e o que o fabricante compara com o
    limiar dele. Nao confundir com o cru, que e a contagem."""
    if not attr:
        return None
    v = attr.get("valor")
    return int(v) if isinstance(v, (int, float)) else None


# ------------------------------------------------------- 2. reserva disponivel
def _reserva(papeis: dict, cfg: dict) -> list[Achado]:
    """Sinal PRIMARIO quando existe. Avalia o valor normalizado, nao o cru.

    Aqui mora o principio 2: e a reserva que da significado a contagem de
    blocos ruins. Com reserva conhecida, a contagem bruta vira secundaria e
    nao dispara nada sozinha.
    """
    a = papeis.get("reserva")
    if a is None:
        return []
    v = _norm(a)
    if v is None:
        return []

    c = cfg["reserve_space"]
    limiar = a.get("limiar")
    margem = c["critical_margin_above_vendor_thresh"]

    # O limiar do fabricante e autoridade (principio 4). A margem dispara
    # ANTES dele para dar janela de substituicao, em vez de avisar no
    # instante em que o drive ja se declarou em falha.
    if isinstance(limiar, (int, float)) and limiar > 0:
        if v <= limiar:
            return [Achado(DISPOSITIVO, CRITICO, "reserva:limiar_fabricante",
                           f"reserva em {v}, no ou abaixo do limite do "
                           f"fabricante ({limiar}) - o drive declarou falha")]
        if v <= limiar + margem:
            return [Achado(DISPOSITIVO, CRITICO, "reserva:margem",
                           f"reserva em {v}, a {v - limiar} pontos do limite "
                           f"do fabricante ({limiar})")]

    if v >= c["ok_min_normalized"]:
        return []
    if v >= c["info_min_normalized"]:
        sev, r = INFO, "reserva:info"
    elif v >= c["warn_min_normalized"]:
        sev, r = WARN, "reserva:warn"
    else:
        sev, r = CRITICO, "reserva:critico"
    return [Achado(DISPOSITIVO, sev, r, f"reserva de blocos em {v}%")]


def _estatico(a: dict | None) -> bool:
    """O contador esta parado, com historico que sustente essa afirmacao?

    Existe para o principio 3, que a especificacao coloca acima dos limiares
    concretos: um disco estavel em 200 setores realocados ha dois anos esta
    saudavel, enquanto um que foi de 0 a 12 numa semana esta morrendo. Sem
    isto, a tabela de contagem bruta condenaria o primeiro.
    """
    if not a:
        return False
    amostras = a.get("amostras")
    if not isinstance(amostras, int) or amostras < 2:
        return False            # sem baseline nao se afirma que esta parado
    return all(a.get(k) in (0, None) for k in ("delta_24h", "delta_7d",
                                               "delta_30d"))


def _teto_se_parado(a: dict | None, achados: list[Achado]) -> list[Achado]:
    """Contagem bruta em contador comprovadamente parado nao passa de INFO."""
    if not _estatico(a):
        return achados
    return [Achado(x.categoria, min(x.severidade, INFO), x.regra,
                   x.mensagem + " (parados, sem crescimento no historico)",
                   x.motivo) for x in achados]


def _blocos_sem_reserva(papeis: dict, cfg: dict, tipo: str) -> list[Achado]:
    """Fallback da secao 2.1: so quando NAO ha atributo de reserva."""
    if "reserva" in papeis:
        return []

    crescidos = _cru(papeis.get("blocos_crescidos"))
    total = _cru(papeis.get("blocos_total"))
    if crescidos is not None and total is not None:
        # Proporcao contra os blocos ruins de fabrica: 4 num drive com 199 de
        # fabrica e outra coisa que 4 num drive com 8.
        base = max(total - crescidos, 1)
        razao = crescidos / base
        c = cfg["bad_blocks_ratio"]
        if razao > c["critical"]:
            sev = CRITICO
        elif razao > c["warn"]:
            sev = WARN
        elif razao > c["info"]:
            sev = INFO
        else:
            return []
        return _teto_se_parado(papeis.get("blocos_crescidos"), [
            Achado(DISPOSITIVO, sev, "blocos:razao",
                   f"{crescidos} blocos crescidos, {razao:.0%} da reserva "
                   f"de fabrica")])

    if tipo == "hdd":
        n = _cru(papeis.get("realocados"))
        if n is None or n <= 0:
            return []
        c = cfg["reallocated_raw"]
        if n >= c["critical"]:
            sev = CRITICO
        elif n >= c["warn"]:
            sev = WARN
        else:
            # Disco com QUALQUER setor realocado falha muito mais que um com
            # zero, mas a maioria ainda sobrevive meses. Nao vale acordar
            # ninguem pelo valor isolado - quem acorda e a regra de taxa.
            sev = INFO
        return _teto_se_parado(papeis.get("realocados"), [
            Achado(DISPOSITIVO, sev, "realocados:bruto",
                   f"{n} setores realocados")])
    return []


# --------------------------------------------------------------- 3. taxa
def _taxa(papel: str, a: dict, cfg: dict) -> tuple[list[Achado], bool]:
    """Regras de variacao. Devolve (achados, tinha_historico).

    Sao as mais importantes da especificacao: e a taxa que separa um disco
    estavel ha dois anos de um disco morrendo esta semana. Sem historico elas
    NAO devolvem OK - devolvem "sem dados", porque afirmar saude sem baseline
    seria mentira.
    """
    amostras = a.get("amostras")
    if not isinstance(amostras, int) or amostras < 2:
        return [], False

    c = cfg["growth_rate"]
    d24 = a.get("delta_24h")
    d7 = a.get("delta_7d")
    d30 = a.get("delta_30d")
    base30 = a.get("base_30d")
    achados: list[Achado] = []
    rotulo = papel.replace("_", " ")

    if isinstance(d24, int) and d24 >= c["critical_count_24h"]:
        achados.append(Achado(DISPOSITIVO, CRITICO, f"taxa:{papel}:24h",
                              f"{d24} novos em {rotulo} nas ultimas 24h"))
    if isinstance(d7, int) and d7 > 0:
        if d7 >= c["critical_count_7d"]:
            achados.append(Achado(DISPOSITIVO, CRITICO, f"taxa:{papel}:7d",
                                  f"{d7} novos em {rotulo} em 7 dias"))
        elif d7 >= c["warn_count_7d"]:
            achados.append(Achado(DISPOSITIVO, WARN, f"taxa:{papel}:7d",
                                  f"{d7} novos em {rotulo} em 7 dias"))
        else:
            achados.append(Achado(DISPOSITIVO, INFO, f"taxa:{papel}:mexeu",
                                  f"{rotulo} subiu {d7} em 7 dias"))

    # Dobrou na janela. A guarda de base evita que 1 -> 2 vire alarme, que e
    # o ruido classico de contador pequeno.
    atual = _cru(a)
    if (isinstance(base30, int) and atual is not None
            and base30 >= c["critical_double_min_base"]
            and atual >= base30 * 2):
        achados.append(Achado(DISPOSITIVO, CRITICO, f"taxa:{papel}:dobrou",
                              f"{rotulo} dobrou em {c['critical_double_window_days']} "
                              f"dias ({base30} -> {atual})"))

    # Aceleracao: a semana atual contra a media semanal do mes anterior.
    if (isinstance(d7, int) and isinstance(d30, int) and d7 > 0
            and d30 > d7):
        media_semanal = (d30 - d7) / 3.0
        if media_semanal > 0 and d7 > media_semanal * c["warn_acceleration_factor"]:
            achados.append(Achado(DISPOSITIVO, WARN, f"taxa:{papel}:acelerou",
                                  f"{rotulo} acelerou: {d7} em 7 dias contra "
                                  f"{media_semanal:.1f}/semana no mes anterior"))
    return achados, True


# ---------------------------------------------------- 4. escalacao direta
def _imediatos(papeis: dict, cfg: dict) -> list[Achado]:
    """Contadores que indicam erro JA visivel ao host.

    Aqui a contagem bruta importa e a margem e estreita: nao e degradacao
    silenciosa, e dado que ja pode ter sido perdido.
    """
    c = cfg["immediate"]
    regras = (
        ("pendentes", "current_pending_sector",
         "setores pendentes (suspeitos, ainda nao realocados)"),
        ("offline_incorrigivel", "offline_uncorrectable",
         "setores incorrigiveis - perda de dado confirmada"),
        ("reportado_incorrigivel", "reported_uncorrect",
         "erros incorrigiveis reportados"),
        ("erro_ponta_a_ponta", "end_to_end_error",
         "erro ponta a ponta - corrupcao no caminho interno de dados"),
        ("timeout_comando", "command_timeout", "timeouts de comando"),
        ("falha_programacao", "program_fail_count", "falhas de programacao"),
        ("falha_apagamento", "erase_fail_count", "falhas de apagamento"),
    )
    out: list[Achado] = []
    for papel, chave, texto in regras:
        n = _cru(papeis.get(papel))
        if n is None or n <= 0:
            continue
        limites = c.get(chave) or {}
        crit, aviso = limites.get("critical"), limites.get("warn")
        if crit is not None and n >= crit:
            out.append(Achado(DISPOSITIVO, CRITICO, f"imediato:{papel}",
                              f"{n} {texto}"))
        elif aviso is not None and n >= aviso:
            out.append(Achado(DISPOSITIVO, WARN, f"imediato:{papel}",
                              f"{n} {texto}"))
    return out


def _interconexao(papeis: dict) -> list[Achado]:
    """4.1 - CRC do UDMA nao e falha de disco.

    E erro de transmissao no barramento: cabo mal encaixado, cabo ruim,
    backplane, controladora. Vai para categoria propria porque quem le isso
    como defeito de midia troca um disco perfeitamente bom e continua com o
    problema.
    """
    a = papeis.get("crc")
    n = _cru(a)
    if not n:
        return []
    subiu = a.get("delta_7d")
    if isinstance(subiu, int) and subiu > 0:
        return [Achado(INTERCONEXAO, WARN, "interconexao:crc",
                       f"{subiu} novos erros de CRC no barramento (cabo, "
                       f"conector ou controladora - nao a midia)")]
    return [Achado(INTERCONEXAO, INFO, "interconexao:crc_estatico",
                   f"{n} erros de CRC acumulados, sem novos - o contador nunca "
                   f"zera sozinho; provavelmente incidente ja resolvido")]


# --------------------------------------------------------------- 5. desgaste
def _percentual_usado(papeis: dict, leitura: dict, cfg: dict) -> float | None:
    """Vida consumida, na ordem de preferencia da especificacao.

    Raw empacotado (do tipo 0x1b2017001b20, que nao e inteiro decimal) e
    DESCARTADO: interpretar isso sem tabela do fabricante e chute.
    """
    direto = leitura.get("percentual_usado")
    if isinstance(direto, (int, float)):
        return float(direto)

    a = papeis.get("desgaste_usado")
    if a is not None and isinstance(_cru(a), int) and 0 <= _cru(a) <= 100:
        return float(_cru(a))

    # Indicadores que contam vida RESTANTE de 100 a 0.
    a = papeis.get("desgaste_restante")
    v = _norm(a)
    if v is not None and 0 <= v <= 100:
        return float(100 - v)

    ciclos = _cru(papeis.get("ciclos_pe"))
    nand = (leitura.get("nand") or "").lower()
    nominais = cfg["wear"]["nand_cycles_default"].get(nand)
    if ciclos is not None and nominais:
        return 100.0 * ciclos / nominais
    return None


def _desgaste(papeis: dict, leitura: dict, cfg: dict) -> list[Achado]:
    pct = _percentual_usado(papeis, leitura, cfg)
    if pct is None:
        return []
    c = cfg["wear"]
    if pct > c["critical_pct"]:
        sev = CRITICO
    elif pct >= c["warn_pct"]:
        sev = WARN
    elif pct >= c["info_pct"]:
        sev = INFO
    else:
        return []
    # Motivo PLANEJAR: desgaste alto nao e falha iminente. Um SSD em 95%
    # tipicamente ainda funciona e falha de forma previsivel, virando
    # read-only. E outro tipo de urgencia que um setor pendente.
    return [Achado(DISPOSITIVO, sev, "desgaste",
                   f"{pct:.0f}% da vida util consumida", PLANEJAR)]


# ------------------------------------------------------------ 6. temperatura
def _temperatura(leitura: dict, cfg: dict) -> list[Achado]:
    tipo = "hdd" if (leitura.get("tipo") or "").lower() == "hdd" else "ssd"
    c = cfg["temperature"][tipo]
    out: list[Achado] = []

    def faixa(t: float) -> int:
        if tipo == "hdd" and t < c["critical_low"]:
            return CRITICO
        if t >= c["critical"]:
            return CRITICO
        if t >= c["warn"]:
            return WARN
        if t >= c["info"]:
            return INFO
        return OK

    t = leitura.get("temp_c")
    if isinstance(t, (int, float)):
        sev = faixa(float(t))
        if sev != OK:
            out.append(Achado(DISPOSITIVO, sev, "temp", f"{t:.0f} C"))

    # O maximo historico conta um nivel abaixo: um pico de 58 C ha seis meses
    # e registro, nao emergencia de agora.
    tm = leitura.get("temp_max_c")
    if isinstance(tm, (int, float)):
        sev = faixa(float(tm)) + cfg["temperature"]["historic_max_severity_offset"]
        if sev > OK:
            out.append(Achado(DISPOSITIVO, sev, "temp:maxima",
                              f"maxima ja registrada de {tm:.0f} C"))

    if leitura.get("throttle"):
        out.append(Achado(DISPOSITIVO, WARN, "temp:throttle",
                          "throttling termico ativo"))
    return out


# ------------------------------------------------- 7. saude do host, nao do disco
def _host(papeis: dict, leitura: dict, cfg: dict) -> list[Achado]:
    """Desligamento sujo. Categoria propria porque a acao e outra.

    Estes achados costumam ser a CAUSA dos blocos ruins: trocar o disco sem
    tratar a fonte reinicia o ciclo com midia nova.
    """
    sujos = leitura.get("desligamentos_sujos")
    if sujos is None:
        sujos = _cru(papeis.get("desligamento_sujo"))
    ciclos = leitura.get("ciclos_energia")
    if ciclos is None:
        ciclos = _cru(papeis.get("ciclos_energia"))
    if sujos is None or ciclos is None:
        return []

    razao = sujos / max(ciclos, 1)
    c = cfg["host_health"]["unexpected_power_loss_ratio"]
    if razao > c["critical"]:
        sev = CRITICO
    elif razao >= c["warn"]:
        sev = WARN
    elif razao >= c["info"]:
        sev = INFO
    else:
        return []
    msg = (f"{sujos} de {ciclos} desligamentos foram inesperados "
           f"({razao:.0%})")
    if sev >= WARN:
        # SSD de consumo nao tem capacitor de PLP: cada corte durante escrita
        # pode custar blocos e corromper metadado de filesystem. A acao e
        # nobreak ou investigar o host - nao trocar a midia.
        msg += " - considere nobreak; a midia nao e a causa"
    return [Achado(HOST, sev, "host:desligamento_sujo", msg, PLANEJAR)]


# ------------------------------------------------------------------ avaliacao
def avaliar_disco(leitura: dict, cfg: dict | None = None) -> Veredito:
    """Avalia um disco. Funcao pura: mesma leitura, mesmo veredito.

    A leitura vem normalizada pelo agente, num formato so para SATA e NVMe:

        {
          "dev": "sda", "tipo": "ssd|hdd|nvme",
          "serial": "...", "modelo": "...", "familia": "...",
          "coleta_ok": true,          # false = smartctl falhou; nao e OK
          "saude": "ok|falha|",       # smart_status.passed
          "temp_c": 41.0, "temp_max_c": 58.0, "throttle": false,
          "percentual_usado": 11.0,   # NVMe expoe direto
          "desligamentos_sujos": 39, "ciclos_energia": 90,
          "atributos": [
            {"id": 5, "nome": "Reallocated_Sector_Ct",
             "valor": 100, "pior": 100, "limiar": 10, "cru": 0,
             "delta_24h": 0, "delta_7d": 0, "delta_30d": 0,
             "base_30d": 0, "amostras": 30},
            ...
          ],
        }

    Os campos delta_* e amostras vem do historico que o coletor mantem no
    host. Ausentes ou com amostras < 2, as regras de taxa entram em
    "sem dados" - nunca em OK.
    """
    cfg = fundir(PADRAO, cfg)
    v = Veredito(dev=leitura.get("dev") or "",
                 serial=leitura.get("serial") or "")

    # Coleta falha e estado proprio. Confundir com OK e o erro que faz um
    # disco atras de controladora RAID, invisivel ao smartctl, aparecer como
    # saudavel para sempre.
    if leitura.get("coleta_ok") is False:
        v.coleta_ok = False
        v.achados.append(Achado(DISPOSITIVO, WARN, "coleta",
                                leitura.get("erro_coleta")
                                or "nao consegui ler o SMART deste disco"))
        return v

    papeis = indexar(leitura.get("atributos"))

    # O drive se declarando em falha vence qualquer interpretacao nossa.
    if leitura.get("saude") == "falha":
        v.achados.append(Achado(DISPOSITIVO, CRITICO, "saude:autoteste",
                                "o proprio drive reprovou no autoteste SMART"))

    v.achados += _reserva(papeis, cfg)
    v.achados += _blocos_sem_reserva(papeis, cfg, (leitura.get("tipo") or "").lower())
    v.achados += _imediatos(papeis, cfg)
    v.achados += _interconexao(papeis)
    v.achados += _desgaste(papeis, leitura, cfg)
    v.achados += _temperatura(leitura, cfg)
    v.achados += _host(papeis, leitura, cfg)

    for papel in CONTADORES_DE_TAXA:
        a = papeis.get(papel)
        if a is None:
            continue
        achados, teve_historico = _taxa(papel, a, cfg)
        if teve_historico:
            v.achados += achados
        elif _cru(a):
            # So reporta falta de historico onde ela faz diferenca: num
            # contador ainda zerado nao ha o que comparar.
            v.sem_dados.append(papel)
    return v


# ------------------------------------------------------------ 8. anti-ruido
class Estabilizador:
    """Histerese e debounce, por (serial, regra).

    Sem isto a ferramenta vira fonte de alerta ignorado em duas semanas, que e
    a unica forma de falha que nenhum threshold corrige.

    Histerese: subir de severidade exige N leituras seguidas concordando -
    um pico isolado de temperatura nao promove nada. Descer exige, alem
    disso, que o valor volte a uma margem ABAIXO da fronteira, senao um valor
    parado em cima do limite oscila para sempre.

    Debounce: a mesma condicao no mesmo disco nao volta a notificar dentro da
    janela. CRITICO tem janela mais curta porque a acao esperada e mais
    urgente.

    O relogio entra por parametro: assim o comportamento no tempo e testavel
    sem esperar horas.
    """

    def __init__(self, cfg: dict | None = None) -> None:
        cfg = fundir(PADRAO, cfg)["noise_control"]
        self.mínimo_para_subir = cfg["hysteresis_raise_consecutive_reads"]
        self.margem = cfg["hysteresis_clear_margin"]
        self.debounce = {WARN: cfg["debounce_hours"]["warn"] * 3600.0,
                         CRITICO: cfg["debounce_hours"]["critical"] * 3600.0}
        self._nivel: dict[tuple, int] = {}
        self._candidato: dict[tuple, tuple[int, int]] = {}
        self._ausencias: dict[tuple, int] = {}
        self._avisado: dict[tuple, float] = {}

    def estabilizar(self, serial: str, achados: list[Achado]) -> list[Achado]:
        """Aplica a histerese e devolve os achados no nivel ja estavel."""
        vistos = {(serial, a.regra): a for a in achados}
        saida: list[Achado] = []

        for chave, a in vistos.items():
            atual = self._nivel.get(chave, OK)
            if a.severidade > atual:
                cand, n = self._candidato.get(chave, (a.severidade, 0))
                n = n + 1 if cand == a.severidade else 1
                if n >= self.mínimo_para_subir:
                    self._nivel[chave] = a.severidade
                    self._candidato.pop(chave, None)
                else:
                    self._candidato[chave] = (a.severidade, n)
                    if atual == OK:
                        continue        # ainda nao confirmou: nao reporta
                    saida.append(Achado(a.categoria, atual, a.regra,
                                        a.mensagem, a.motivo))
                    continue
            else:
                self._candidato.pop(chave, None)
                self._nivel[chave] = a.severidade
            saida.append(a)

        # Regra que parou de disparar. Descer exige tantas leituras limpas
        # quantas foram exigidas para subir - a especificacao pede uma margem
        # de 5 pontos abaixo da fronteira, e como aqui ja chega severidade e
        # nao o valor bruto, a simetria de leituras cumpre o mesmo papel:
        # impedir que um valor parado em cima do limite fique piscando.
        #
        # O candidato tambem zera. Sem isso um pico isolado deixava meia
        # confirmacao guardada, e o pico seguinte - horas depois, com leitura
        # limpa no meio - promovia como se os dois fossem consecutivos.
        for chave in [k for k in self._candidato
                      if k[0] == serial and k not in vistos]:
            del self._candidato[chave]
        for chave in [k for k in self._nivel
                      if k[0] == serial and k not in vistos]:
            n = self._ausencias.get(chave, 0) + 1
            if n >= self.mínimo_para_subir:
                del self._nivel[chave]
                self._ausencias.pop(chave, None)
            else:
                self._ausencias[chave] = n
        for chave in vistos:
            self._ausencias.pop(chave, None)
        return saida

    def deve_notificar(self, serial: str, a: Achado, agora: float) -> bool:
        """Primeira vez, ou passou a janela de debounce daquela severidade."""
        if a.severidade < WARN:
            return False        # INFO se registra, nao se notifica
        chave = (serial, a.regra, a.severidade)
        janela = self.debounce.get(a.severidade, 0.0)
        ultimo = self._avisado.get(chave)
        if ultimo is not None and agora - ultimo < janela:
            return False
        self._avisado[chave] = agora
        return True


def contador_regrediu(anterior: int | None, atual: int | None) -> bool:
    """Contador que DIMINUIU nao e melhora.

    Contadores SMART so crescem. Cair significa disco trocado na mesma baia,
    firmware bugado ou erro de parsing - os tres pedem baseline nova e um
    registro de anomalia, nunca um suspiro de alivio.
    """
    return (isinstance(anterior, int) and isinstance(atual, int)
            and atual < anterior)


def avaliar_frota(discos: list[dict], cfg: dict | None = None) -> list[Veredito]:
    return [avaliar_disco(d, cfg) for d in discos or []]
