#!/usr/bin/env python3
"""
sysmon_web - dashboard da frota no browser.

Serve uma pagina local com gauges, temperatura e SMART por disco. Stdlib pura:
nenhuma dependencia, nenhum CDN, funciona offline.

Normalmente voce nao chama este modulo direto - use `python3 sysmon.py`, que
sobe o dashboard e a bandeja juntos.

Os TOKENS FICAM NO SERVIDOR. O browser recebe apenas a telemetria ja coletada,
nunca as credenciais dos agentes - por isso o polling acontece aqui e nao no
JavaScript.
"""

from __future__ import annotations

import json
import sys
import threading
import webbrowser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.resources import files
from pathlib import Path

from sysmon_nucleo import (
    ErroConfig, Frota, achar_config, avisar_permissao, carregar_config, como_dict,
)

__version__ = "2.2.0"

# Lista fixa em vez de montar caminho com o que o cliente mandou: nenhuma
# requisicao consegue sair deste conjunto, entao nao existe travessia de path.
ESTATICOS = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/index.html": ("index.html", "text/html; charset=utf-8"),
    "/estilo.css": ("estilo.css", "text/css; charset=utf-8"),
    "/app.js": ("app.js", "text/javascript; charset=utf-8"),
}

_cache: dict[str, bytes] = {}


def asset(nome: str) -> bytes:
    """Le um arquivo da interface.

    Usa importlib.resources em vez de abrir caminho no disco porque isso
    funciona igual rodando do repositorio e de dentro do sysmon.pyz, onde os
    arquivos estao comprimidos e nao existem no sistema de arquivos.
    """
    if nome not in _cache:
        _cache[nome] = (files("web") / nome).read_bytes()
    return _cache[nome]


class Handler(BaseHTTPRequestHandler):
    server_version = f"sysmon-web/{__version__}"
    protocol_version = "HTTP/1.1"
    timeout = 15
    frota: Frota

    def _responder(self, codigo: int, corpo: bytes, tipo: str) -> None:
        self.send_response(codigo)
        self.send_header("Content-Type", tipo)
        self.send_header("Content-Length", str(len(corpo)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        # A pagina e inteiramente local; nada externo deve ser carregavel.
        #
        # style-src permite inline porque o dashboard escreve largura de barra e
        # cor de status em atributos style, que sao valores calculados aqui e
        # nunca texto vindo dos agentes. O que importa continua fechado:
        # script-src fica em 'self', e todo dado remoto entra por textContent,
        # entao nome de host ou modelo de disco nunca vira HTML.
        self.send_header(
            "Content-Security-Policy",
            "default-src 'self'; style-src 'self' 'unsafe-inline'; "
            "img-src 'self' data:; base-uri 'none'; form-action 'none'; "
            "object-src 'none'")
        self.end_headers()
        self.wfile.write(corpo)

    def do_GET(self) -> None:  # noqa: N802
        caminho = self.path.split("?", 1)[0]

        if caminho == "/api/frota":
            corpo = json.dumps(como_dict(self.frota), ensure_ascii=False,
                               default=str).encode()
            return self._responder(200, corpo, "application/json; charset=utf-8")

        if caminho == "/api/atualizar":
            self.frota.atualizar_agora()
            return self._responder(200, b'{"ok":true}', "application/json")

        if caminho in ESTATICOS:
            arquivo, tipo = ESTATICOS[caminho]
            try:
                return self._responder(200, asset(arquivo), tipo)
            except (OSError, ModuleNotFoundError):
                return self._responder(500, b"arquivo da interface ausente",
                                       "text/plain; charset=utf-8")

        self._responder(404, b"nao encontrado", "text/plain; charset=utf-8")

    def log_message(self, fmt: str, *args) -> None:
        pass  # o dashboard e local; log de acesso so polui o terminal


def servidor(frota: Frota, host: str = "127.0.0.1",
             porta: int = 9110) -> ThreadingHTTPServer:
    """Cria o servidor ja ligado na porta. Quem chama decide quando servir.

    Separado do laco para o sysmon.py poder subir o dashboard e a bandeja no
    mesmo processo, e para o erro de porta ocupada aparecer antes de qualquer
    thread comecar.
    """
    classe = type("HandlerLigado", (Handler,), {"frota": frota})
    srv = ThreadingHTTPServer((host, porta), classe)
    srv.daemon_threads = True
    return srv


def main(argv: list[str] | None = None) -> int:
    """Uso avulso: `python3 sysmon_web.py`. O caminho normal e o sysmon.py."""
    import argparse
    p = argparse.ArgumentParser(description="Dashboard web da frota sysmon.")
    p.add_argument("--config")
    p.add_argument("--porta", type=int, default=9110)
    p.add_argument("--host", default="127.0.0.1")
    p.add_argument("--nao-abrir", action="store_true")
    args = p.parse_args(argv)

    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2
    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)

    frota = Frota(cfg)
    frota.iniciar()
    srv = servidor(frota, args.host, args.porta)

    url = f"http://{args.host}:{args.porta}/"
    print(f"sysmon-web {__version__} em {url}  ({len(cfg.hosts)} host(s))")
    if not args.nao_abrir:
        threading.Timer(0.5, lambda: webbrowser.open(url)).start()
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        print()
    finally:
        frota.parar()
        srv.shutdown()
    return 0


if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    sys.exit(main())
