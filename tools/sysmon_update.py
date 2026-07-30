#!/usr/bin/env python3
"""
sysmon_update - atualizacao automatica do proprio sysmon.pyz.

Verifica o release mais novo no GitHub, baixa em segundo plano, confere o
SHA256 e deixa o arquivo pronto ao lado do atual. A troca acontece no proximo
arranque, feita pelo lancador - no Windows nao da para sobrescrever com
seguranca um .pyz que o proprio processo tem aberto.

Fluxo completo:

    sysmon.pyz  roda, verifica, baixa  ->  sysmon-novo.pyz
    voce reinicia (botao ou logon)
    sysmon.vbs  ve o -novo, troca      ->  sysmon.pyz atualizado

Falhar aqui nunca pode atrapalhar o monitoramento: qualquer erro de rede,
JSON ou disco vira "sem atualizacao" e a vida segue.
"""

from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

__version__ = "3.0.0"

REPO = "9LEVEL/sysmon"
API = f"https://api.github.com/repos/{REPO}/releases/latest"
ASSET = "sysmon.pyz"
SOMAS = "SHA256SUMS"

TIMEOUT = 20
MAX_BYTES = 40 * 1024 * 1024  # o .pyz tem dezenas de KB; 40M e teto de sanidade
INTERVALO_PADRAO = 6 * 3600   # a cada 6h, alem da verificacao no arranque


def _versao(txt: str) -> tuple:
    """'v2.3.0' -> (2,3,0). Compara como numero, nao como string: '2.10' > '2.9'."""
    nums = re.findall(r"\d+", txt or "")
    return tuple(int(n) for n in nums[:3]) or (0,)


def _buscar(url: str, limite: int = MAX_BYTES) -> bytes:
    req = urllib.request.Request(url, headers={
        "User-Agent": f"sysmon/{__version__}",
        "Accept": "application/vnd.github+json",
    })
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        return r.read(limite)


def alvo() -> Path | None:
    """O .pyz em execucao, ou None quando rodando do repositorio.

    Atualizar so faz sentido no bundle: no repositorio quem atualiza e o git.
    """
    argv0 = Path(sys.argv[0])
    if argv0.suffix == ".pyz" and argv0.is_file():
        return argv0.resolve()
    # Dentro do zipapp, __file__ aponta para um caminho DENTRO do arquivo,
    # entao o .pyz aparece como um dos diretorios pai.
    for pai in Path(__file__).resolve().parents:
        if pai.suffix == ".pyz" and pai.is_file():
            return pai
    return None


class Atualizador:
    """Estado da atualizacao, consultado pela interface."""

    def __init__(self, versao_atual: str, intervalo: float = INTERVALO_PADRAO) -> None:
        self.versao_atual = versao_atual
        self.intervalo = intervalo
        self.arquivo = alvo()
        self._lock = threading.Lock()
        self._estado = {
            "checando": False,
            "disponivel": None,   # versao nova, quando houver
            "pronta": False,      # ja baixada e conferida
            "erro": None,
            "notas": None,
        }

    # -- leitura ----------------------------------------------------------
    def estado(self) -> dict:
        with self._lock:
            return dict(self._estado, suportado=self.arquivo is not None,
                        versao_atual=self.versao_atual)

    def _marcar(self, **campos) -> None:
        with self._lock:
            self._estado.update(campos)

    # -- verificacao ------------------------------------------------------
    def verificar(self) -> None:
        """Consulta o release mais novo e baixa se houver. Nunca levanta."""
        if not self.arquivo:
            return
        self._marcar(checando=True, erro=None)
        try:
            dados = json.loads(_buscar(API, 512 * 1024).decode("utf-8"))
            tag = dados.get("tag_name") or ""
            if _versao(tag) <= _versao(self.versao_atual):
                self._marcar(checando=False, disponivel=None, pronta=False)
                return

            assets = {a.get("name"): a for a in dados.get("assets") or []}
            pyz, somas = assets.get(ASSET), assets.get(SOMAS)
            if not pyz or not somas:
                self._marcar(checando=False,
                             erro=f"release {tag} sem {ASSET} ou {SOMAS}")
                return

            self._marcar(disponivel=tag, notas=(dados.get("name") or "").strip())
            self._baixar(pyz["browser_download_url"], somas["browser_download_url"])
            self._marcar(checando=False, pronta=True)
            logging.info("atualizacao %s pronta em %s", tag, self._novo())
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            self._marcar(checando=False, erro=f"sem conexao ({e})")
        except (json.JSONDecodeError, KeyError, ValueError) as e:
            self._marcar(checando=False, erro=f"resposta inesperada ({e})")
        except Exception as e:  # noqa: BLE001 - update nunca derruba o monitor
            logging.exception("falha ao atualizar")
            self._marcar(checando=False, erro=str(e))

    def _novo(self) -> Path:
        return self.arquivo.with_name("sysmon-novo.pyz")

    def _baixar(self, url_pyz: str, url_somas: str) -> None:
        corpo = _buscar(url_pyz)

        # A soma do release e a unica garantia de que veio inteiro e do lugar
        # certo. Sem ela, nao troca nada.
        esperado = None
        for linha in _buscar(url_somas, 64 * 1024).decode("utf-8").splitlines():
            partes = linha.split()
            if len(partes) == 2 and partes[1].lstrip("*") == ASSET:
                esperado = partes[0].lower()
                break
        if not esperado:
            raise ValueError(f"{SOMAS} nao lista {ASSET}")

        obtido = hashlib.sha256(corpo).hexdigest()
        if obtido != esperado:
            raise ValueError(f"SHA256 nao confere (esperado {esperado[:12]}..., "
                             f"veio {obtido[:12]}...)")

        # Zipapp valido? Se o download vier corrompido de outra forma, e melhor
        # descobrir agora do que deixar um arquivo quebrado para o proximo boot.
        import zipfile
        import io
        with zipfile.ZipFile(io.BytesIO(corpo)) as z:
            if "__main__.py" not in z.namelist():
                raise ValueError("o arquivo baixado nao parece um sysmon.pyz")

        tmp = self._novo().with_suffix(".parcial")
        tmp.write_bytes(corpo)
        os.replace(tmp, self._novo())   # troca atomica no mesmo diretorio

    # -- laco -------------------------------------------------------------
    def iniciar(self, parar: threading.Event | None = None) -> None:
        """Verifica no arranque e depois a cada intervalo, numa thread."""
        if not self.arquivo or self.intervalo <= 0:
            return

        def laco() -> None:
            # Espera o dashboard subir antes de gastar rede com atualizacao.
            time.sleep(20)
            while True:
                self.verificar()
                if parar and parar.wait(self.intervalo):
                    return
                if not parar:
                    time.sleep(self.intervalo)

        threading.Thread(target=laco, name="update", daemon=True).start()


def aplicar_pendente(caminho_pyz: Path) -> bool:
    """Troca sysmon.pyz pelo sysmon-novo.pyz, se houver.

    Chamado pelo lancador ANTES do Python abrir o .pyz. Existe em Python
    tambem porque o mesmo passo precisa funcionar fora do Windows.
    """
    novo = caminho_pyz.with_name("sysmon-novo.pyz")
    if not novo.is_file():
        return False
    try:
        os.replace(novo, caminho_pyz)
        return True
    except OSError:
        return False
