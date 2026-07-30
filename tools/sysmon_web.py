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
    ErroConfig, Frota, achar_config, avisar_permissao, carregar_config,
    carregar_config_de, como_dict, salvar_config, testar_host,
)

__version__ = "2.6.0"

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
    modo = "browser"      # "app" quando servindo a janela nativa
    atualizador = None    # sysmon_update.Atualizador, quando ha bundle
    reiniciar = None      # callable que fecha o app para o lancador trocar
    mostrar = None        # callable que traz a janela para a frente
    caminho_config = None # Path do config.json, para a tela de configuracao

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

    # ---------------------------------------------------------------- POST
    def _corpo_json(self) -> dict | None:
        """Le e valida um corpo JSON de requisicao local.

        Exigir Content-Type application/json e recusar Origin de fora e o que
        impede uma pagina qualquer da internet de escrever no seu config: um
        POST cross-origin com esse content-type dispara preflight, e nos nao
        respondemos OPTIONS com CORS permissivo.
        """
        origem = self.headers.get("Origin")
        if origem and origem not in (f"http://{self.headers.get('Host')}",
                                     f"https://{self.headers.get('Host')}"):
            return None
        if "application/json" not in (self.headers.get("Content-Type") or ""):
            return None
        try:
            n = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            return None
        if n <= 0 or n > 1 << 20:
            return None
        try:
            dados = json.loads(self.rfile.read(n).decode("utf-8"))
        except (json.JSONDecodeError, UnicodeDecodeError):
            return None
        return dados if isinstance(dados, dict) else None

    def do_POST(self) -> None:  # noqa: N802
        caminho = self.path.split("?", 1)[0]
        if caminho not in ("/api/config", "/api/testar"):
            return self._responder(404, b"nao encontrado", "text/plain; charset=utf-8")

        dados = self._corpo_json()
        if dados is None:
            return self.escreverJSON(400, {"erro": "requisicao invalida"})

        if caminho == "/api/testar":
            ok, msg = testar_host(str(dados.get("url") or ""),
                                  str(dados.get("token") or ""))
            return self.escreverJSON(200, {"ok": ok, "mensagem": msg})

        return self._salvar_config(dados)

    def escreverJSON(self, codigo: int, v) -> None:  # noqa: N802
        self._responder(codigo, json.dumps(v, ensure_ascii=False).encode(),
                        "application/json; charset=utf-8")

    def _salvar_config(self, dados: dict) -> None:
        if not self.caminho_config:
            return self.escreverJSON(409, {"erro": "sem arquivo de configuracao"})

        # Preserva o que a tela nao edita (opacidade, porta_web, etc).
        try:
            bruto = json.loads(Path(self.caminho_config).read_text(encoding="utf-8"))
            if not isinstance(bruto, dict):
                bruto = {}
        except (OSError, json.JSONDecodeError):
            bruto = {}

        bruto.pop("url", None)    # formato v1 daria conflito com hosts[]
        bruto.pop("token", None)

        # Campo de token em branco significa "mantem o que ja estava", nao
        # "apaga": a tela nunca exibe o token de volta, entao exigir redigitar
        # a cada edicao seria pedir para o usuario perder o acesso.
        atuais = {h.nome: h.token for h in self.frota.cfg.hosts}
        novos = []
        for h in dados.get("hosts") or []:
            if not isinstance(h, dict):
                continue
            h = {k: v for k, v in h.items() if k in ("nome", "url", "token")}
            if not h.get("token") and h.get("nome") in atuais:
                h["token"] = atuais[h["nome"]]
            novos.append(h)
        bruto["hosts"] = novos
        for chave in ("intervalo", "timeout"):
            if chave in dados:
                bruto[chave] = dados[chave]

        try:
            cfg = carregar_config_de(bruto)
        except ErroConfig as e:
            return self.escreverJSON(400, {"erro": str(e)})

        try:
            salvar_config(Path(self.caminho_config), bruto)
        except OSError as e:
            return self.escreverJSON(500, {"erro": f"nao consegui gravar: {e}"})

        self.frota.trocar(cfg)
        return self.escreverJSON(200, {"ok": True, "hosts": len(cfg.hosts)})

    def do_GET(self) -> None:  # noqa: N802
        caminho = self.path.split("?", 1)[0]

        if caminho == "/api/config":
            # Devolve a configuracao SEM os tokens: a tela mostra se ha token
            # guardado, mas nao precisa exibi-lo de volta.
            hosts = [{"nome": h.nome, "url": h.url, "tem_token": bool(h.token)}
                     for h in self.frota.cfg.hosts]
            return self.escreverJSON(200, {
                "hosts": hosts,
                "intervalo": self.frota.cfg.intervalo,
                "timeout": self.frota.cfg.timeout,
                "arquivo": str(self.caminho_config or ""),
                "editavel": bool(self.caminho_config),
            })

        if caminho == "/api/frota":
            # O modo vai no payload para a pagina saber se ha janela nativa em
            # volta - e o que decide mostrar os controles de janela. A pagina
            # nao tem como descobrir isso sozinha de forma confiavel.
            dados = como_dict(self.frota) | {"modo": self.modo}
            if self.atualizador:
                dados["update"] = self.atualizador.estado()
            corpo = json.dumps(dados, ensure_ascii=False, default=str).encode()
            return self._responder(200, corpo, "application/json; charset=utf-8")

        if caminho == "/api/atualizar":
            self.frota.atualizar_agora()
            return self._responder(200, b'{"ok":true}', "application/json")

        if caminho == "/api/mostrar":
            # Chamado por uma SEGUNDA instancia que encontrou a porta ocupada:
            # em vez de morrer disputando, ela pede para esta aparecer e sai.
            if self.mostrar:
                threading.Thread(target=self.mostrar, daemon=True).start()
            return self._responder(200, b'{"ok":true}', "application/json")

        if caminho == "/api/reiniciar":
            # A troca do .pyz e feita pelo lancador no proximo arranque: no
            # Windows nao da para sobrescrever com seguranca um arquivo que
            # este processo tem aberto.
            if not self.reiniciar:
                return self._responder(409, b'{"erro":"sem reinicio automatico"}',
                                       "application/json")
            threading.Timer(0.3, self.reiniciar).start()
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


def servidor(frota: Frota, host: str = "127.0.0.1", porta: int = 9110,
             modo: str = "browser", atualizador=None,
             reiniciar=None, mostrar=None,
             caminho_config=None) -> ThreadingHTTPServer:
    """Cria o servidor ja ligado na porta. Quem chama decide quando servir.

    Separado do laco para o sysmon.py poder subir o dashboard e a janela no
    mesmo processo, e para o erro de porta ocupada aparecer antes de qualquer
    thread comecar.
    """
    classe = type("HandlerLigado", (Handler,), {
        "frota": frota, "modo": modo,
        "atualizador": atualizador,
        "reiniciar": staticmethod(reiniciar) if reiniciar else None,
        "mostrar": staticmethod(mostrar) if mostrar else None,
        "caminho_config": caminho_config,
    })
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
