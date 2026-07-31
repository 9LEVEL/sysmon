#!/usr/bin/env python3
"""
sysmon_app - janela nativa do dashboard.

Abre o dashboard numa janela do sistema, sem barra de endereco e sem aba de
browser, usando o motor web que o proprio sistema ja tem (WebView2 no Windows,
WebKitGTK no Linux). A interface e a mesma do modo browser - nada de UI
duplicada - o que muda e a moldura em volta e os controles de janela.

Requer pywebview:
    pip install pywebview

Sem ele o sysmon.py cai no browser e diz o motivo, em vez de nao subir.

O botao de alfinete no cabecalho chama alternar_topo() por aqui. "Sempre no
topo" e propriedade da janela, nao da pagina: nao existe forma de uma pagina
web se manter sobre as outras janelas, e e justamente por isso que este modulo
existe.
"""

from __future__ import annotations

import json
import logging
import os
import sys
import webbrowser
from pathlib import Path

import webview  # pywebview

__version__ = "3.5.0"

TITULO = "sysmon"

# Preferencias de janela ficam FORA do config.json do usuario: sao estado de
# interface, nao configuracao, e reescrever o config dele a cada clique
# reformataria um arquivo que ele edita a mao.
def caminho_estado() -> Path:
    if os.name == "nt":
        base = Path(os.environ.get("APPDATA") or Path.home() / "AppData/Roaming")
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")
    return base / "sysmon" / "janela.json"


PADRAO = {"largura": 1180, "altura": 820, "x": None, "y": None, "topo": False}


def ler_estado() -> dict:
    estado = dict(PADRAO)
    try:
        estado.update(json.loads(caminho_estado().read_text(encoding="utf-8")))
    except (OSError, json.JSONDecodeError, TypeError):
        pass  # primeira execucao, ou arquivo corrompido: usa o padrao
    return estado


def gravar_estado(estado: dict) -> None:
    try:
        p = caminho_estado()
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps(estado, indent=2), encoding="utf-8")
    except OSError:
        logging.debug("nao consegui gravar %s", caminho_estado())


class Ponte:
    """O que a pagina pode chamar. Superficie minima de proposito."""

    def __init__(self, url: str) -> None:
        self.url = url
        self.janela: webview.Window | None = None
        self.estado = ler_estado()

    # -- chamados pela pagina ---------------------------------------------
    def alternar_topo(self) -> bool:
        """Liga/desliga 'sempre no topo'. Devolve o novo estado."""
        if not self.janela:
            return False
        novo = not bool(self.janela.on_top)
        self.janela.on_top = novo
        self.estado["topo"] = novo
        gravar_estado(self.estado)
        return novo

    def no_topo(self) -> bool:
        return bool(self.janela.on_top) if self.janela else False

    def minimizar(self) -> None:
        if self.janela:
            self.janela.minimize()

    def abrir_no_browser(self) -> None:
        webbrowser.open(self.url)

    # -- ciclo de vida ----------------------------------------------------
    def guardar_geometria(self) -> None:
        if not self.janela:
            return
        try:
            self.estado.update(largura=int(self.janela.width),
                               altura=int(self.janela.height),
                               x=int(self.janela.x), y=int(self.janela.y))
        except (TypeError, ValueError, AttributeError):
            return  # backend que nao expoe geometria; mantem o que havia
        gravar_estado(self.estado)


# A janela ativa, para a bandeja poder falar com ela de outra thread.
_atual: Ponte | None = None


def mostrar() -> None:
    """Traz a janela de volta (usado pelo item 'Mostrar janela' da bandeja)."""
    if _atual and _atual.janela:
        _atual.janela.restore()
        _atual.janela.show()


def alternar_topo() -> bool:
    return _atual.alternar_topo() if _atual else False


def no_topo() -> bool:
    return _atual.no_topo() if _atual else False


def fechar() -> None:
    """Alias historico de sair(); usado pelo botao de reiniciar apos update."""
    sair()


# Quando ha bandeja, fechar a janela SO esconde: o programa continua vivo no
# icone, como qualquer app de bandeja. Sair de verdade e pelo menu da bandeja.
_ficar_na_bandeja = False
_saindo = False


def sair() -> None:
    """Encerra de verdade. E o que o item 'Sair' da bandeja chama."""
    global _saindo
    _saindo = True
    if _atual and _atual.janela:
        _atual.guardar_geometria()
        _atual.janela.destroy()


def rodar(url: str, ao_fechar=None, oculto: bool = False,
          ficar_na_bandeja: bool = False) -> None:
    """Cria a janela e entra no laco. Bloqueia a thread principal."""
    global _atual, _ficar_na_bandeja
    _ficar_na_bandeja = ficar_na_bandeja
    ponte = Ponte(url)
    _atual = ponte
    e = ponte.estado

    janela = webview.create_window(
        TITULO,
        url=url,
        width=int(e.get("largura") or PADRAO["largura"]),
        height=int(e.get("altura") or PADRAO["altura"]),
        x=e.get("x"), y=e.get("y"),
        min_size=(560, 420),
        on_top=bool(e.get("topo")),
        hidden=oculto,
        # A pagina tem fundo escuro; sem isso a janela pisca branco ao abrir.
        background_color="#0d0d0d",
        text_select=True,
        js_api=ponte,
    )
    ponte.janela = janela
    janela.expose(ponte.alternar_topo, ponte.no_topo,
                  ponte.minimizar, ponte.abrir_no_browser)

    def encerrando():
        """Devolver False cancela o fechamento (pywebview trata em todos os
        backends: winforms, gtk, qt e cocoa)."""
        ponte.guardar_geometria()
        if _ficar_na_bandeja and not _saindo:
            janela.hide()
            return False        # nao fecha: o programa segue na bandeja
        if ao_fechar:
            ao_fechar()
        return None

    janela.events.closing += encerrando

    # private_mode=False guarda o cache do webview entre execucoes, o que deixa
    # a abertura seguinte instantanea.
    webview.start(private_mode=False, storage_path=str(caminho_estado().parent / "web"))


def disponivel() -> bool:
    """Se ha backend de janela utilizavel nesta maquina."""
    try:
        import webview.guilib
        webview.guilib.initialize()
        return True
    except Exception:  # noqa: BLE001 - sem GUI, sem GTK, sem WebView2...
        return False


if __name__ == "__main__":
    sys.exit("Use `python sysmon.py`; este modulo e a janela, nao o programa.")
