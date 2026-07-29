#!/usr/bin/env python3
"""
Testes do nucleo compartilhado pelo dashboard e pelo tray.

    python3 -m unittest discover -s tools -v

Stdlib pura, como o resto dos clientes - da para rodar em qualquer maquina
sem instalar nada.
"""

from __future__ import annotations

import contextlib
import json
import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


@contextlib.contextmanager
def ambiente(**variaveis: str):
    """Define variaveis de ambiente so durante o bloco, restaurando depois."""
    anteriores = {k: os.environ.get(k) for k in variaveis}
    os.environ.update(variaveis)
    try:
        yield
    finally:
        for k, v in anteriores.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v

from sysmon_nucleo import (
    AVISO, CRITICO, OFFLINE, OK,
    Config, ErroConfig, Estado, Frota, Host, Monitor,
    avaliar, carregar_config, fmt_bps, fmt_bytes, fmt_uptime,
    nivel_temp, primeira_temp, resumo_linhas,
)


def cfg_temp(conteudo) -> Path:
    """Escreve um config.json temporario e devolve o caminho."""
    d = Path(tempfile.mkdtemp())
    p = d / "config.json"
    p.write_text(conteudo if isinstance(conteudo, str)
                 else json.dumps(conteudo), encoding="utf-8")
    return p


def snapshot(**campos) -> dict:
    """Um /metrics minimo e saudavel, com os campos sobrescritos pelo teste."""
    base = {
        "v": "2.0.0", "host": "maquina", "uptime_s": 3600,
        "load": [0.1, 0.2, 0.3], "cpus": 4,
        "cpu_percent": 5.0, "cpu_temp": 40.0, "cpu_crit": 100.0,
        "temps": [], "fans": {},
        "mem": {"total": 8 << 30, "usado": 2 << 30, "percent": 25.0,
                "swap_total": 0, "swap_usado": 0, "swap_percent": None},
        "discos": [{"mount": "/", "total": 100, "usado": 10,
                    "percent": 10.0, "inodes_percent": 5.0}],
        "diskio": [], "net": [], "raid": [], "thinpools": [],
        "guests": None, "extras": {}, "pressure": None,
        "idade_s": 1.0, "intervalo_s": 5.0, "coletor_falhas": 0,
    }
    base.update(campos)
    return base


def estado_ok(**campos) -> Estado:
    return Estado(dados=snapshot(**campos), erro=None, atualizado=1.0)


# ------------------------------------------------------------------ config
class TestConfig(unittest.TestCase):
    def test_formato_multi_host(self):
        p = cfg_temp({"hosts": [
            {"nome": "pve", "url": "http://1.2.3.4:9109/metrics", "token": "t1"},
            {"nome": "nas", "url": "http://1.2.3.5:9109/metrics", "token": "t2"},
        ], "intervalo": 7})
        cfg = carregar_config(p)
        self.assertEqual([h.nome for h in cfg.hosts], ["pve", "nas"])
        self.assertEqual(cfg.hosts[1].token, "t2")
        self.assertEqual(cfg.intervalo, 7)

    def test_compatibilidade_com_config_v1(self):
        # Quem ja usava a versao de host unico nao deve precisar reescrever nada.
        p = cfg_temp({"url": "http://10.0.0.5:9109/metrics", "token": "antigo",
                      "intervalo": 5})
        cfg = carregar_config(p)
        self.assertEqual(len(cfg.hosts), 1)
        self.assertEqual(cfg.hosts[0].token, "antigo")
        self.assertEqual(cfg.hosts[0].nome, "10.0.0.5")

    def test_token_padrao_preenche_hosts_sem_token(self):
        p = cfg_temp({"token": "comum", "hosts": [
            {"nome": "a", "url": "http://1.1.1.1/metrics"},
            {"nome": "b", "url": "http://2.2.2.2/metrics", "token": "proprio"},
        ]})
        cfg = carregar_config(p)
        self.assertEqual(cfg.hosts[0].token, "comum")
        self.assertEqual(cfg.hosts[1].token, "proprio")

    def test_nome_derivado_da_url(self):
        p = cfg_temp({"hosts": [{"url": "http://192.168.0.9:9109/metrics", "token": "t"}]})
        self.assertEqual(carregar_config(p).hosts[0].nome, "192.168.0.9")

    def test_ambiente_vence_o_arquivo(self):
        # A prioridade do ambiente e o atalho para apontar o cliente a outro
        # host sem editar config: util para isolar problema.
        p = cfg_temp({"hosts": [
            {"nome": "pve", "url": "http://do-arquivo/metrics", "token": "arq"}]})
        with ambiente(SYSMON_URL="http://do-ambiente:9109/metrics",
                      SYSMON_TOKEN="env"):
            cfg = carregar_config(p)
        self.assertEqual(len(cfg.hosts), 1)
        self.assertEqual(cfg.hosts[0].url, "http://do-ambiente:9109/metrics")
        self.assertEqual(cfg.hosts[0].token, "env")

    def test_ambiente_dispensa_o_arquivo(self):
        with ambiente(SYSMON_URL="http://so-ambiente:9109/metrics",
                      SYSMON_TOKEN="env", SYSMON_NOME="apelido"):
            cfg = carregar_config(Path("/nao/existe/config.json"))
        self.assertEqual(cfg.hosts[0].nome, "apelido")

    def test_ambiente_sem_url_nao_interfere(self):
        p = cfg_temp({"hosts": [
            {"nome": "pve", "url": "http://do-arquivo/metrics", "token": "arq"}]})
        with ambiente(SYSMON_TOKEN="solto"):  # sem SYSMON_URL: nao aciona nada
            cfg = carregar_config(p)
        self.assertEqual(cfg.hosts[0].url, "http://do-arquivo/metrics")
        self.assertEqual(cfg.hosts[0].token, "arq")

    def test_override_por_host(self):
        p = cfg_temp({"hosts": [
            {"nome": "pve", "url": "http://a/metrics", "token": "ta"},
            {"nome": "nas", "url": "http://b/metrics", "token": "tb"},
        ]})
        with ambiente(SYSMON_TOKEN_NAS="novo-token-do-nas"):
            cfg = carregar_config(p)
        self.assertEqual(cfg.hosts[0].token, "ta")           # intacto
        self.assertEqual(cfg.hosts[1].token, "novo-token-do-nas")
        self.assertEqual(cfg.hosts[1].url, "http://b/metrics")

    def test_override_normaliza_nome_com_hifen(self):
        # Nome de variavel de ambiente nao aceita hifen nem ponto.
        p = cfg_temp({"hosts": [
            {"nome": "pve-01.lan", "url": "http://a/metrics", "token": "ta"}]})
        with ambiente(SYSMON_URL_PVE_01_LAN="http://novo:9109/metrics"):
            cfg = carregar_config(p)
        self.assertEqual(cfg.hosts[0].url, "http://novo:9109/metrics")

    def test_url_invalida_no_ambiente_e_recusada(self):
        p = cfg_temp({"hosts": [
            {"nome": "pve", "url": "http://a/metrics", "token": "t"}]})
        with ambiente(SYSMON_URL_PVE="192.168.0.1:9109"):  # sem esquema
            with self.assertRaises(ErroConfig):
                carregar_config(p)

    def test_erros_uteis(self):
        casos = {
            "arquivo ausente": Path("/nao/existe/config.json"),
            "json quebrado": cfg_temp("{isso nao e json"),
            "sem hosts": cfg_temp({"intervalo": 5}),
            "host sem url": cfg_temp({"hosts": [{"nome": "x", "token": "t"}]}),
            "host sem token": cfg_temp({"hosts": [{"url": "http://1.1.1.1/m"}]}),
            "url sem esquema": cfg_temp({"hosts": [{"url": "1.1.1.1:9109", "token": "t"}]}),
            "nomes repetidos": cfg_temp({"hosts": [
                {"nome": "a", "url": "http://1.1.1.1/m", "token": "t"},
                {"nome": "a", "url": "http://2.2.2.2/m", "token": "t"},
            ]}),
        }
        for nome, caminho in casos.items():
            with self.subTest(nome):
                with self.assertRaises(ErroConfig):
                    carregar_config(caminho)


# ------------------------------------------------------------------ avaliacao
class TestNivelTemp(unittest.TestCase):
    def test_usa_o_crit_do_sensor(self):
        # Com crit 100: aviso a partir de 75, critico a partir de 90.
        self.assertEqual(nivel_temp(70, 100), OK)
        self.assertEqual(nivel_temp(75, 100), AVISO)
        self.assertEqual(nivel_temp(89, 100), AVISO)
        self.assertEqual(nivel_temp(90, 100), CRITICO)

    def test_crit_diferente_muda_os_limiares(self):
        # A mesma config serve para hardware com crit menor - o motivo de usar
        # fracao do crit em vez de um numero fixo.
        self.assertEqual(nivel_temp(70, 80), AVISO)   # 70 >= 80*0.75
        self.assertEqual(nivel_temp(73, 80), CRITICO)  # 73 >= 80*0.90

    def test_fallback_sem_crit(self):
        self.assertEqual(nivel_temp(60, None), OK)
        self.assertEqual(nivel_temp(70, None), AVISO)
        self.assertEqual(nivel_temp(85, None), CRITICO)

    def test_sem_sensor_nao_e_alarme(self):
        self.assertEqual(nivel_temp(None, None), OK)


class TestAvaliar(unittest.TestCase):
    def test_host_saudavel(self):
        nivel, alertas = avaliar(estado_ok())
        self.assertEqual(nivel, OK)
        self.assertEqual(alertas, [])

    def test_offline_vence_tudo(self):
        nivel, alertas = avaliar(Estado(erro="sem conexao"))
        self.assertEqual(nivel, OFFLINE)
        self.assertEqual(alertas, ["sem conexao"])

    def test_disco_cheio(self):
        nivel, alertas = avaliar(estado_ok(discos=[
            {"mount": "/", "percent": 93.0, "inodes_percent": 10.0,
             "total": 1, "usado": 1}]))
        self.assertEqual(nivel, CRITICO)
        self.assertIn("disco / em 93%", alertas)

    def test_inodes_esgotados(self):
        # Inode cheio quebra igual a disco cheio, e o df -h nao mostra.
        nivel, alertas = avaliar(estado_ok(discos=[
            {"mount": "/", "percent": 20.0, "inodes_percent": 98.0,
             "total": 1, "usado": 1}]))
        self.assertEqual(nivel, CRITICO)
        self.assertTrue(any("inodes" in a for a in alertas))

    def test_thin_pool(self):
        nivel, alertas = avaliar(estado_ok(thinpools=[
            {"nome": "pve/data", "data_percent": 85.0, "meta_percent": 4.0}]))
        self.assertEqual(nivel, AVISO)
        self.assertIn("thin pool pve/data em 85%", alertas)

    def test_raid_degradado_e_critico(self):
        nivel, alertas = avaliar(estado_ok(raid=[
            {"nome": "md0", "estado": "ativo", "discos": "U_", "degradado": True}]))
        self.assertEqual(nivel, CRITICO)
        self.assertIn("RAID md0 degradado (U_)", alertas)

    def test_raid_saudavel_nao_alerta(self):
        nivel, _ = avaliar(estado_ok(raid=[
            {"nome": "md0", "estado": "ativo", "discos": "UU", "degradado": False}]))
        self.assertEqual(nivel, OK)

    def test_pressao_de_io(self):
        nivel, alertas = avaliar(estado_ok(pressure={
            "io": {"some_avg10": 80.0, "some_avg60": 75.0}}))
        self.assertEqual(nivel, CRITICO)
        self.assertTrue(any("pressao de IO" in a for a in alertas))

    def test_coleta_parada_no_agente(self):
        # O agente responde, mas parou de coletar: dado velho e enganoso.
        nivel, alertas = avaliar(estado_ok(idade_s=300.0, intervalo_s=5.0))
        self.assertEqual(nivel, AVISO)
        self.assertTrue(any("coleta parada" in a for a in alertas))

    def test_idade_normal_nao_alerta(self):
        self.assertEqual(avaliar(estado_ok(idade_s=6.0, intervalo_s=5.0))[0], OK)

    def test_pior_condicao_define_o_nivel(self):
        nivel, alertas = avaliar(estado_ok(
            cpu_temp=80.0, cpu_crit=100.0,                       # aviso
            discos=[{"mount": "/", "percent": 95.0, "inodes_percent": 1.0,
                     "total": 1, "usado": 1}],                    # critico
        ))
        self.assertEqual(nivel, CRITICO)
        self.assertEqual(len(alertas), 2)


# ------------------------------------------------------------------ frota
class TestFrota(unittest.TestCase):
    def montar(self, estados: dict[str, Estado]) -> Frota:
        cfg = Config(hosts=[Host(nome=n, url=f"http://x/{n}", token="t")
                            for n in estados])
        frota = Frota(cfg)
        for m in frota.monitores:
            m._estado = estados[m.host.nome]
        return frota

    def test_pior_nivel_e_alertas_prefixados(self):
        frota = self.montar({
            "pve": estado_ok(),
            "nas": estado_ok(discos=[{"mount": "/t", "percent": 95.0,
                                      "inodes_percent": 1.0, "total": 1, "usado": 1}]),
            "vps": Estado(erro="sem conexao"),
        })
        self.assertEqual(frota.pior_nivel(), OFFLINE)
        alertas = frota.alertas()
        self.assertIn("nas: disco /t em 95%", alertas)
        self.assertIn("vps: sem conexao", alertas)
        self.assertNotIn("pve", " ".join(alertas))

    def test_temperatura_mostrada_e_a_do_host_mais_quente(self):
        frota = self.montar({
            "a": estado_ok(cpu_temp=40.0, cpu_crit=100.0),
            "b": estado_ok(cpu_temp=71.0, cpu_crit=90.0),
            "c": Estado(erro="offline"),
        })
        temp, crit = primeira_temp(frota.estados())
        self.assertEqual(temp, 71.0)
        self.assertEqual(crit, 90.0)

    def test_frota_toda_offline(self):
        frota = self.montar({"a": Estado(erro="x"), "b": Estado(erro="y")})
        self.assertEqual(primeira_temp(frota.estados()), (None, None))
        self.assertEqual(frota.pior_nivel(), OFFLINE)


# ------------------------------------------------------------------ rede
class Falso(BaseHTTPRequestHandler):
    """Agente de mentira: responde conforme o caminho pedido."""

    def do_GET(self):  # noqa: N802
        if self.path.startswith("/401"):
            corpo = b'{"erro":"token invalido"}'
            self.send_response(401)
        elif self.path.startswith("/lixo"):
            corpo = b"isso nao e json"
            self.send_response(200)
        elif self.path.startswith("/lista"):
            corpo = b"[1,2,3]"
            self.send_response(200)
        else:
            corpo = json.dumps(snapshot(host="remoto")).encode()
            self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(corpo)))
        self.end_headers()
        self.wfile.write(corpo)

    def log_message(self, *a):
        pass


class TestMonitor(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.srv = ThreadingHTTPServer(("127.0.0.1", 0), Falso)
        cls.srv.daemon_threads = True
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()
        cls.base = f"http://127.0.0.1:{cls.srv.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()
        cls.srv.server_close()

    def monitor(self, caminho: str) -> Monitor:
        return Monitor(Host("t", self.base + caminho, "tok"), intervalo=1, timeout=2)

    def test_leitura_boa(self):
        m = self.monitor("/metrics")
        m.buscar()
        self.assertIsNone(m.estado.erro)
        self.assertEqual(m.estado.dados["host"], "remoto")
        self.assertEqual(m.estado.falhas, 0)

    def test_token_invalido_tem_mensagem_propria(self):
        m = self.monitor("/401")
        m.buscar()
        self.assertEqual(m.estado.erro, "token invalido")
        self.assertIsNone(m.estado.dados)

    def test_resposta_nao_json(self):
        m = self.monitor("/lixo")
        m.buscar()
        self.assertIsNotNone(m.estado.erro)

    def test_json_que_nao_e_objeto(self):
        # Uma lista passaria no json.loads e quebraria os clientes adiante.
        m = self.monitor("/lista")
        m.buscar()
        self.assertIn("resposta invalida", m.estado.erro)

    def test_host_inalcancavel(self):
        m = Monitor(Host("t", "http://127.0.0.1:1/metrics", "tok"), 1, 1)
        m.buscar()
        self.assertIn("sem conexao", m.estado.erro)

    def test_recuo_exponencial_com_teto(self):
        m = self.monitor("/metrics")
        self.assertEqual(m._espera(), 1)  # sem falhas: intervalo normal
        for falhas, esperado in [(1, 2), (2, 4), (3, 8), (6, 60), (99, 60)]:
            m._estado = Estado(erro="x", falhas=falhas)
            self.assertEqual(m._espera(), esperado, f"falhas={falhas}")

    def test_falhas_zeram_quando_o_host_volta(self):
        m = self.monitor("/metrics")
        m._estado = Estado(erro="x", falhas=5)
        m.buscar()
        self.assertEqual(m.estado.falhas, 0)
        self.assertEqual(m._espera(), 1)

    def test_callback_so_dispara_na_mudanca_de_nivel(self):
        mudancas = []
        m = Monitor(Host("t", self.base + "/metrics", "tok"), 1, 2,
                    ao_mudar=lambda nome, e: mudancas.append(nome))
        m.buscar()   # OFFLINE (estado inicial) -> OK: notifica
        m.buscar()   # OK -> OK: nao notifica de novo
        self.assertEqual(mudancas, ["t"])


# ------------------------------------------------------------------ formato
class TestFormato(unittest.TestCase):
    def test_bytes(self):
        self.assertEqual(fmt_bytes(None), "--")
        self.assertEqual(fmt_bytes(512), "512B")
        self.assertEqual(fmt_bytes(2048), "2K")
        self.assertEqual(fmt_bytes(5 * 1024 ** 3), "5G")

    def test_bps(self):
        self.assertEqual(fmt_bps(None), "--")
        self.assertEqual(fmt_bps(2048), "2K/s")

    def test_uptime(self):
        self.assertEqual(fmt_uptime(None), "--")
        self.assertEqual(fmt_uptime(0), "--")
        self.assertEqual(fmt_uptime(3600), "1h00m")
        self.assertEqual(fmt_uptime(90000), "1d1h")

    def test_resumo_de_host_offline(self):
        linhas = resumo_linhas(Host("pve", "http://x/m", "t"), Estado(erro="timeout"))
        self.assertEqual(len(linhas), 1)
        self.assertIn("offline", linhas[0])

    def test_resumo_de_host_vivo(self):
        linhas = resumo_linhas(Host("pve", "http://x/m", "t"), estado_ok())
        self.assertTrue(linhas[0].startswith("pve"))
        self.assertTrue(any("CPU" in l for l in linhas))
        self.assertTrue(any("RAM" in l for l in linhas))


if __name__ == "__main__":
    unittest.main(verbosity=2)
