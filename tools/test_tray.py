#!/usr/bin/env python3
"""
Testes da logica de exibicao do traymon, sem abrir janela nenhuma.

    python3 -m unittest discover -s windows-tray -t windows-tray -v

pystray, PIL e tkinter sao substituidos por duplos: o objetivo aqui e o que
da para verificar fora do Windows - montagem das linhas, escolha de cor e
conteudo do overlay. O desenho de verdade so da para conferir no Windows.
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import types
import unittest
from pathlib import Path

RAIZ = Path(__file__).resolve().parent
sys.path.insert(0, str(RAIZ))


def _instalar_duplos() -> None:
    """Coloca modulos de mentira no lugar dos que so existem no Windows."""
    for nome in ("pystray", "PIL", "PIL.Image", "PIL.ImageDraw", "PIL.ImageFont",
                 "tkinter"):
        if nome in sys.modules:
            continue
        mod = types.ModuleType(nome)
        sys.modules[nome] = mod

    pil = sys.modules["PIL"]
    for attr in ("Image", "ImageDraw", "ImageFont"):
        setattr(pil, attr, sys.modules[f"PIL.{attr}"])

    tk = sys.modules["tkinter"]
    for attr in ("Tk", "Toplevel", "Text", "Label"):
        if not hasattr(tk, attr):
            setattr(tk, attr, type(attr, (), {}))

    ps = sys.modules["pystray"]
    if not hasattr(ps, "Icon"):
        ps.Icon = type("Icon", (), {})
        ps.MenuItem = type("MenuItem", (), {"__init__": lambda self, *a, **k: None})

        class Menu:
            SEPARATOR = object()

            def __init__(self, *itens):
                self.itens = itens

        ps.Menu = Menu


CONFIG = {
    "hosts": [
        {"nome": "pve", "url": "http://127.0.0.1:9109/metrics", "token": "t1"},
        {"nome": "nas", "url": "http://127.0.0.1:9110/metrics", "token": "t2"},
    ],
    "overlay_ao_iniciar": False,
}

_instalar_duplos()

import sysmon_tray as traymon  # noqa: E402
from sysmon_nucleo import AVISO, CRITICO, OFFLINE, OK, Config, Estado, Frota, Host  # noqa: E402
from test_nucleo import estado_ok  # noqa: E402

# preparar() e quem instala o config nos globais do modulo; sem ele os testes
# de exibicao rodariam contra os padroes, nao contra a configuracao real.
traymon.preparar(Frota(Config(hosts=[Host("pve", "http://x/1", "t1"),
                                     Host("nas", "http://x/2", "t2")],
                              extra=CONFIG)),
                 Config(hosts=[Host("pve", "http://x/1", "t1"),
                               Host("nas", "http://x/2", "t2")], extra=CONFIG))


def frota_com(estados: dict[str, Estado]) -> Frota:
    cfg = Config(hosts=[Host(nome=n, url=f"http://x/{n}", token="t") for n in estados])
    frota = Frota(cfg)
    for m in frota.monitores:
        m._estado = estados[m.host.nome]
    return frota


class TestConfigCarregada(unittest.TestCase):
    def test_leu_os_dois_hosts(self):
        self.assertEqual([h.nome for h in traymon.CFG.hosts], ["pve", "nas"])

    def test_cores_cobrem_todos_os_niveis(self):
        for nivel in (OK, AVISO, CRITICO, OFFLINE):
            self.assertIn(nivel, traymon.CORES)
            self.assertIn(nivel, traymon.CORES_HEX)
            self.assertRegex(traymon.CORES_HEX[nivel], r"^#[0-9a-f]{6}$")


class TestLinhaCompacta(unittest.TestCase):
    def test_host_vivo(self):
        linha = traymon.linha_compacta(Host("pve", "u", "t"), estado_ok(
            cpu_temp=47.0, cpu_percent=12.0))
        self.assertIn("pve", linha)
        self.assertIn("47C", linha)
        self.assertIn("12%", linha)
        self.assertIn("/ 10%", linha)

    def test_host_offline(self):
        linha = traymon.linha_compacta(Host("vps", "u", "t"), Estado(erro="timeout"))
        self.assertIn("vps", linha)
        self.assertIn("offline", linha)

    def test_host_sem_sensor_nao_quebra(self):
        linha = traymon.linha_compacta(Host("vm", "u", "t"), estado_ok(
            cpu_temp=None, cpu_crit=None, discos=[]))
        self.assertIn("--", linha)


class TestTituloTray(unittest.TestCase):
    def test_uma_linha_por_host(self):
        frota = frota_com({"pve": estado_ok(), "nas": estado_ok()})
        self.assertEqual(len(traymon.titulo_tray(frota).splitlines()), 2)

    def test_limita_a_cinco_linhas(self):
        # O tooltip do Windows trunca perto de 127 caracteres; nao adianta
        # mandar 20 hosts.
        frota = frota_com({f"h{i}": estado_ok() for i in range(9)})
        self.assertEqual(len(traymon.titulo_tray(frota).splitlines()), 5)


class TestConteudoOverlay(unittest.TestCase):
    def overlay(self, frota: Frota, compacto: bool) -> traymon.Overlay:
        o = traymon.Overlay.__new__(traymon.Overlay)
        o.frota = frota
        o.compacto = compacto
        return o

    def test_compacto_e_uma_linha_por_host(self):
        frota = frota_com({"pve": estado_ok(), "nas": estado_ok()})
        linhas = self.overlay(frota, True)._conteudo()
        self.assertEqual(len(linhas), 2)
        self.assertTrue(all(tag == f"n{OK}" for _, tag in linhas))

    def test_detalhado_tem_bloco_por_host(self):
        frota = frota_com({"pve": estado_ok(), "nas": estado_ok()})
        linhas = self.overlay(frota, False)._conteudo()
        self.assertGreater(len(linhas), 6)
        # Nao deve sobrar linha em branco no fim do ultimo bloco.
        self.assertNotEqual(linhas[-1][0], "")

    def test_cada_host_recebe_a_propria_cor(self):
        frota = frota_com({
            "bom": estado_ok(),
            "quente": estado_ok(cpu_temp=95.0, cpu_crit=100.0),
            "morto": Estado(erro="sem conexao"),
        })
        linhas = self.overlay(frota, True)._conteudo()
        tags = [tag for _, tag in linhas[:3]]
        self.assertEqual(tags, [f"n{OK}", f"n{CRITICO}", f"n{OFFLINE}"])

    def test_alertas_vao_para_o_fim(self):
        frota = frota_com({"nas": estado_ok(discos=[
            {"mount": "/t", "percent": 95.0, "inodes_percent": 1.0,
             "total": 1, "usado": 1}])})
        linhas = self.overlay(frota, True)._conteudo()
        self.assertEqual(linhas[-1][1], "alerta")
        self.assertIn("disco /t em 95%", linhas[-1][0])

    def test_frota_saudavel_nao_gera_secao_de_alerta(self):
        frota = frota_com({"pve": estado_ok()})
        linhas = self.overlay(frota, True)._conteudo()
        self.assertTrue(all(tag != "alerta" for _, tag in linhas))


if __name__ == "__main__":
    unittest.main(verbosity=2)
