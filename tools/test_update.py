#!/usr/bin/env python3
"""
Testes da atualizacao automatica, contra um GitHub de mentira.

O caminho inteiro e exercitado sem rede externa: release falso, asset falso,
SHA256SUMS falso. O que importa aqui e que atualizacao ruim NAO seja aplicada -
trocar o proprio binario por algo nao verificado seria o pior bug possivel
neste projeto.
"""

from __future__ import annotations

import hashlib
import io
import json
import tempfile
import threading
import unittest
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import sysmon_update as U


def pyz_valido(marca: bytes = b"x") -> bytes:
    """Um zipapp minimo, do jeito que o empacotar.sh produz."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        z.writestr("__main__.py", b"import sysmon, sys\n# " + marca)
        z.writestr("sysmon.py", b"def main(): return 0\n")
    return buf.getvalue()


class FalsoGitHub(BaseHTTPRequestHandler):
    """Serve /latest, /pyz e /somas conforme a configuracao da classe."""

    tag = "v9.0.0"
    corpo_pyz = b""
    soma_declarada: str | None = None   # None = usa a soma real do corpo
    listar_asset = True

    def do_GET(self):  # noqa: N802
        if self.path == "/latest":
            assets = [{"name": "SHA256SUMS",
                       "browser_download_url": self._u("/somas")}]
            if self.listar_asset:
                assets.insert(0, {"name": "sysmon.pyz",
                                  "browser_download_url": self._u("/pyz")})
            corpo = json.dumps({"tag_name": self.tag, "name": f"sysmon {self.tag}",
                                "assets": assets}).encode()
        elif self.path == "/pyz":
            corpo = self.corpo_pyz
        elif self.path == "/somas":
            soma = self.soma_declarada or hashlib.sha256(self.corpo_pyz).hexdigest()
            corpo = (f"{soma}  sysmon.pyz\n"
                     f"{'0' * 64}  sysmon-clientes-9.0.0.zip\n").encode()
        else:
            self.send_response(404)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(corpo)))
        self.end_headers()
        self.wfile.write(corpo)

    def _u(self, caminho: str) -> str:
        return f"http://127.0.0.1:{self.server.server_address[1]}{caminho}"

    def log_message(self, *a):
        pass


class TestVersao(unittest.TestCase):
    def test_compara_como_numero(self):
        # Comparar como string faria "2.9" > "2.10", e a atualizacao pararia
        # de ser oferecida na decima versao menor.
        self.assertGreater(U._versao("v2.10.0"), U._versao("2.9.0"))
        self.assertGreater(U._versao("2.3.1"), U._versao("2.3.0"))
        self.assertEqual(U._versao("v2.3.0"), U._versao("2.3.0"))

    def test_lixo_nao_quebra(self):
        self.assertEqual(U._versao(""), (0,))
        self.assertEqual(U._versao(None), (0,))
        self.assertEqual(U._versao("sem numero"), (0,))


class TestAtualizador(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.srv = ThreadingHTTPServer(("127.0.0.1", 0), FalsoGitHub)
        cls.srv.daemon_threads = True
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()
        cls.base = f"http://127.0.0.1:{cls.srv.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()
        cls.srv.server_close()

    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.pyz = self.dir / "sysmon.pyz"
        self.pyz.write_bytes(pyz_valido(b"antigo"))
        self.novo = self.dir / "sysmon-novo.pyz"

        self._api = U.API
        U.API = self.base + "/latest"
        FalsoGitHub.tag = "v9.0.0"
        FalsoGitHub.corpo_pyz = pyz_valido(b"novo")
        FalsoGitHub.soma_declarada = None
        FalsoGitHub.listar_asset = True

    def tearDown(self):
        U.API = self._api

    def montar(self, versao="1.0.0") -> U.Atualizador:
        a = U.Atualizador(versao, intervalo=0)
        a.arquivo = self.pyz
        return a

    def test_baixa_e_deixa_pronto(self):
        a = self.montar()
        a.verificar()
        e = a.estado()
        self.assertEqual(e["disponivel"], "v9.0.0")
        self.assertTrue(e["pronta"], e["erro"])
        self.assertIsNone(e["erro"])
        self.assertTrue(self.novo.is_file())
        self.assertEqual(self.novo.read_bytes(), FalsoGitHub.corpo_pyz)
        # O arquivo em uso nao pode ser tocado antes da troca pelo lancador.
        self.assertIn(b"antigo", self.pyz.read_bytes())

    def test_ja_atualizado_nao_baixa(self):
        a = self.montar("9.0.0")
        a.verificar()
        self.assertIsNone(a.estado()["disponivel"])
        self.assertFalse(self.novo.exists())

    def test_sha_diferente_recusa(self):
        # O caso que mais importa: conteudo trocado no meio do caminho.
        FalsoGitHub.soma_declarada = "a" * 64
        a = self.montar()
        a.verificar()
        e = a.estado()
        self.assertFalse(e["pronta"])
        self.assertIn("SHA256 nao confere", e["erro"])
        self.assertFalse(self.novo.exists())

    def test_sem_entrada_no_somas_recusa(self):
        FalsoGitHub.listar_asset = False
        a = self.montar()
        a.verificar()
        self.assertIn("sem sysmon.pyz", a.estado()["erro"])
        self.assertFalse(self.novo.exists())

    def test_download_que_nao_e_zipapp_recusa(self):
        FalsoGitHub.corpo_pyz = b"isso nao e um zip"
        a = self.montar()
        a.verificar()
        self.assertFalse(a.estado()["pronta"])
        self.assertFalse(self.novo.exists())

    def test_zip_sem_main_recusa(self):
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as z:
            z.writestr("qualquer.py", b"nada")
        FalsoGitHub.corpo_pyz = buf.getvalue()
        a = self.montar()
        a.verificar()
        self.assertIn("nao parece um sysmon.pyz", a.estado()["erro"])
        self.assertFalse(self.novo.exists())

    def test_github_fora_do_ar_nao_derruba(self):
        U.API = "http://127.0.0.1:1/latest"
        a = self.montar()
        a.verificar()   # nao pode levantar
        self.assertIn("sem conexao", a.estado()["erro"])

    def test_nao_deixa_parcial_para_tras(self):
        FalsoGitHub.soma_declarada = "b" * 64
        self.montar().verificar()
        sobras = list(self.dir.glob("*.parcial"))
        self.assertEqual(sobras, [], f"sobrou arquivo parcial: {sobras}")

    def test_fora_do_bundle_nao_faz_nada(self):
        # Rodando do repositorio, quem atualiza e o git.
        a = U.Atualizador("1.0.0", intervalo=0)
        a.arquivo = None
        a.verificar()
        self.assertFalse(a.estado()["suportado"])
        self.assertIsNone(a.estado()["disponivel"])


class TestAplicarPendente(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.pyz = self.dir / "sysmon.pyz"
        self.pyz.write_bytes(b"antigo")

    def test_troca_quando_ha_pendente(self):
        (self.dir / "sysmon-novo.pyz").write_bytes(b"novo")
        self.assertTrue(U.aplicar_pendente(self.pyz))
        self.assertEqual(self.pyz.read_bytes(), b"novo")
        self.assertFalse((self.dir / "sysmon-novo.pyz").exists())

    def test_sem_pendente_nao_mexe(self):
        self.assertFalse(U.aplicar_pendente(self.pyz))
        self.assertEqual(self.pyz.read_bytes(), b"antigo")


if __name__ == "__main__":
    unittest.main(verbosity=2)
