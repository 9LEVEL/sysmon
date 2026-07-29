#!/usr/bin/env python3
"""
sysmon-web.py - dashboard da frota no browser.

Sobe um servidor local que consulta os agentes e serve uma pagina com gauges,
temperatura por disco e uso de filesystem. Stdlib pura: nenhuma dependencia,
nenhum CDN, funciona offline.

    python3 sysmon-web.py                    # abre no browser sozinho
    python3 sysmon-web.py --porta 9110
    python3 sysmon-web.py --nao-abrir        # so sobe o servidor

Os TOKENS FICAM NO SERVIDOR. O browser recebe apenas a telemetria ja coletada,
nunca as credenciais dos agentes - por isso o polling acontece aqui e nao no
JavaScript.

Por padrao escuta so em 127.0.0.1: a pagina nao tem autenticacao, entao expor
na rede entregaria a telemetria da frota inteira para qualquer um.
"""

from __future__ import annotations

import argparse
import json
import sys
import threading
import time
import webbrowser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from sysmon_nucleo import (  # noqa: E402
    ErroConfig, Frota,
    achar_config, avisar_permissao, carregar_config, como_dict,
)

__version__ = "2.1.0"

WEB = Path(__file__).resolve().parent / "web"

# Lista fixa em vez de montar caminho com o que o cliente mandou: nenhuma
# requisicao consegue sair deste conjunto, entao nao existe travessia de path.
ESTATICOS = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/index.html": ("index.html", "text/html; charset=utf-8"),
    "/estilo.css": ("estilo.css", "text/css; charset=utf-8"),
    "/app.js": ("app.js", "text/javascript; charset=utf-8"),
}


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
                return self._responder(200, (WEB / arquivo).read_bytes(), tipo)
            except OSError:
                return self._responder(500, b"arquivo da interface ausente",
                                       "text/plain; charset=utf-8")

        self._responder(404, b"nao encontrado", "text/plain; charset=utf-8")

    def log_message(self, fmt: str, *args) -> None:
        pass  # o dashboard e local; log de acesso so polui o terminal


def main() -> int:
    p = argparse.ArgumentParser(description="Dashboard web da frota sysmon.")
    p.add_argument("--config", help="caminho do config.json")
    p.add_argument("--porta", type=int, default=9110)
    p.add_argument("--host", default="127.0.0.1",
                   help="IP de bind (padrao 127.0.0.1; a pagina nao tem senha)")
    p.add_argument("--nao-abrir", action="store_true",
                   help="nao abrir o browser automaticamente")
    p.add_argument("--version", action="version", version=__version__)
    args = p.parse_args()

    caminho = achar_config(args.config)
    try:
        cfg = carregar_config(caminho)
    except ErroConfig as e:
        print(f"erro: {e}", file=sys.stderr)
        return 2

    if aviso := avisar_permissao(caminho):
        print(f"aviso: {aviso}", file=sys.stderr)
    if args.host not in ("127.0.0.1", "::1", "localhost"):
        print(f"AVISO: escutando em {args.host}. A pagina nao tem autenticacao "
              "e mostra a telemetria de todos os hosts.", file=sys.stderr)

    frota = Frota(cfg)
    frota.iniciar()
    Handler.frota = frota

    servidor = ThreadingHTTPServer((args.host, args.porta), Handler)
    servidor.daemon_threads = True

    url = f"http://{args.host}:{args.porta}/"
    print(f"sysmon-web {__version__} em {url}  ({len(cfg.hosts)} host(s))")
    print("Ctrl+C para sair")

    if not args.nao_abrir:
        # Depois do bind, senao o browser chega antes do servidor existir.
        threading.Timer(0.5, lambda: webbrowser.open(url)).start()

    try:
        servidor.serve_forever()
    except KeyboardInterrupt:
        print()
    finally:
        frota.parar()
        servidor.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
