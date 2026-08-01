#!/usr/bin/env python3
"""
Testes da camada de avaliacao SMART.

A secao 10 da especificacao lista sete casos que devem passar antes de a
camada ser considerada pronta. Eles estao aqui, na classe TestValidacaoDaSpec,
com o numero de cada um - e, onde a especificacao contradiz a si mesma, o
teste registra qual das duas partes foi seguida e por que.
"""

from __future__ import annotations

import unittest

import sysmon_smart as S


def attr(nome, cru, *, id=0, valor=None, limiar=None, pior=None,
         d24=None, d7=None, d30=None, base30=None, amostras=None):
    """Atributo no formato que o agente entrega."""
    return {"id": id, "nome": nome, "cru": cru, "valor": valor,
            "pior": pior, "limiar": limiar, "delta_24h": d24, "delta_7d": d7,
            "delta_30d": d30, "base_30d": base30, "amostras": amostras}


def parado(nome, cru, **kw):
    """Atributo com historico dizendo que nao mexe ha um mes."""
    kw.setdefault("amostras", 180)
    return attr(nome, cru, d24=0, d7=0, d30=0, base30=cru, **kw)


def disco(**kw):
    base = {"dev": "sda", "tipo": "ssd", "serial": "X1", "saude": "ok",
            "atributos": []}
    base.update(kw)
    return base


class TestCatalogoDeAtributos(unittest.TestCase):
    def test_casa_por_nome_e_nao_por_id(self):
        """O mesmo ID 170 e Grown_Bad_Blocks num WD e reserva num Intel.
        Casar por ID seria escolher um significado no cara ou coroa."""
        wd = S.indexar([attr("Grown_Bad_Blocks", 4, id=170)])
        intel = S.indexar([attr("Available_Reservd_Space", 0, id=170,
                                valor=98)])
        self.assertIn("blocos_crescidos", wd)
        self.assertIn("reserva", intel)
        self.assertNotIn("reserva", wd)

    def test_nome_desconhecido_e_ignorado_sem_palpite(self):
        papeis = S.indexar([attr("Vendor_Specific_170", 12345, id=170),
                            attr("Unknown_Attribute", 7, id=169)])
        self.assertEqual(papeis, {})


class TestReserva(unittest.TestCase):
    """Secao 2 - o sinal primario, sempre sobre o valor normalizado."""

    def avaliar(self, valor, limiar=None):
        return S.avaliar_disco(disco(atributos=[
            attr("Available_Reservd_Space", 0, id=232, valor=valor,
                 limiar=limiar)]))

    def test_faixas(self):
        self.assertEqual(self.avaliar(98).dispositivo, S.OK)
        self.assertEqual(self.avaliar(85).dispositivo, S.INFO)
        self.assertEqual(self.avaliar(60).dispositivo, S.WARN)
        self.assertEqual(self.avaliar(40).dispositivo, S.CRITICO)

    def test_limiar_do_fabricante_e_autoridade(self):
        # VALUE <= THRESH: o drive esta declarando falha iminente. Nao ha
        # margem de interpretacao nossa por cima disso.
        v = self.avaliar(10, limiar=10)
        self.assertEqual(v.dispositivo, S.CRITICO)
        self.assertIn("reserva:limiar_fabricante", [a.regra for a in v.achados])

    def test_margem_dispara_antes_do_fabricante(self):
        """Dez pontos antes do limite, para haver janela de substituicao."""
        self.assertEqual(self.avaliar(19, limiar=10).dispositivo, S.CRITICO)
        self.assertEqual(self.avaliar(95, limiar=10).dispositivo, S.OK)

    def test_com_reserva_a_contagem_bruta_nao_dispara(self):
        """Principio 2: havendo reserva, ela e o sinal - 4 blocos com 98% de
        reserva intacta e ruido."""
        v = S.avaliar_disco(disco(atributos=[
            attr("Available_Reservd_Space", 0, id=232, valor=98),
            parado("Grown_Bad_Blocks", 4, id=170)]))
        self.assertEqual(v.dispositivo, S.OK)


class TestTaxa(unittest.TestCase):
    """Secao 3 - as regras mais importantes."""

    def com(self, **kw):
        return S.avaliar_disco(disco(atributos=[
            attr("Reallocated_Sector_Ct", kw.pop("cru", 10), id=5, **kw)]))

    def test_incremento_pequeno_e_info(self):
        self.assertEqual(self.com(d7=1, d30=1, amostras=30).dispositivo, S.INFO)

    def test_tres_em_sete_dias_e_aviso(self):
        self.assertEqual(self.com(d7=3, d30=3, amostras=30).dispositivo, S.WARN)

    def test_cinco_em_24h_e_critico(self):
        self.assertEqual(self.com(d24=5, d7=5, d30=5, amostras=30).dispositivo,
                         S.CRITICO)

    def test_dez_em_sete_dias_e_critico(self):
        self.assertEqual(self.com(d7=10, d30=10, amostras=30).dispositivo,
                         S.CRITICO)

    def test_dobrar_exige_base_minima(self):
        # 1 -> 2 nao pode virar alarme; 4 -> 8 pode.
        self.assertLess(self.com(cru=2, base30=1, d30=1, amostras=30).dispositivo,
                        S.CRITICO)
        self.assertEqual(self.com(cru=8, base30=4, d30=4, d7=4, amostras=30)
                         .dispositivo, S.CRITICO)

    def test_aceleracao(self):
        """Semana atual 3x acima da media das anteriores."""
        v = self.com(d7=2, d30=3, amostras=30)
        self.assertIn("acelerou", " ".join(a.regra for a in v.achados))


class TestEscalacaoDireta(unittest.TestCase):
    """Secao 4 - erro ja visivel ao host, margem estreita."""

    def um(self, nome, cru, id=0):
        return S.avaliar_disco(disco(atributos=[attr(nome, cru, id=id)]))

    def test_pendentes(self):
        self.assertEqual(self.um("Current_Pending_Sector", 2, 197).dispositivo,
                         S.WARN)
        self.assertEqual(self.um("Current_Pending_Sector", 10, 197).dispositivo,
                         S.CRITICO)

    def test_um_so_ja_e_critico(self):
        for nome, i in (("Offline_Uncorrectable", 198),
                        ("Reported_Uncorrect", 187),
                        ("End-to-End_Error", 184)):
            self.assertEqual(self.um(nome, 1, i).dispositivo, S.CRITICO, nome)

    def test_command_timeout(self):
        self.assertEqual(self.um("Command_Timeout", 5, 188).dispositivo, S.OK)
        self.assertEqual(self.um("Command_Timeout", 10, 188).dispositivo, S.WARN)
        self.assertEqual(self.um("Command_Timeout", 100, 188).dispositivo,
                         S.CRITICO)


class TestInterconexao(unittest.TestCase):
    """Secao 4.1 - CRC nao e falha de disco, e trocar midia por isso e o
    desperdicio que a categoria separada existe para evitar."""

    def test_crc_nao_contamina_o_dispositivo(self):
        v = S.avaliar_disco(disco(atributos=[
            attr("UDMA_CRC_Error_Count", 12, id=199, d7=3, amostras=30)]))
        self.assertEqual(v.dispositivo, S.OK)
        self.assertEqual(v.interconexao, S.WARN)
        self.assertIn("cabo", v.achados[0].mensagem)

    def test_crc_estatico_e_so_registro(self):
        v = S.avaliar_disco(disco(atributos=[
            parado("UDMA_CRC_Error_Count", 40, id=199)]))
        self.assertEqual(v.interconexao, S.INFO)
        self.assertEqual(v.dispositivo, S.OK)


class TestDesgaste(unittest.TestCase):
    """Secao 5."""

    def test_faixas_por_percentual_direto(self):
        for pct, esperado in ((50, S.OK), (75, S.INFO), (90, S.WARN),
                              (96, S.CRITICO)):
            v = S.avaliar_disco(disco(percentual_usado=pct))
            self.assertEqual(v.dispositivo, esperado, pct)

    def test_indicador_conta_vida_restante(self):
        v = S.avaliar_disco(disco(atributos=[
            attr("Media_Wearout_Indicator", 0, id=233, valor=10)]))
        self.assertEqual(v.dispositivo, S.WARN)     # 90% consumido

    def test_desgaste_alto_pede_planejamento_nao_acao_imediata(self):
        """CRITICO de desgaste e "planeje a troca"; CRITICO de setor pendente
        e "aja hoje". Sao urgencias diferentes e o motivo distingue."""
        v = S.avaliar_disco(disco(percentual_usado=99))
        self.assertEqual(v.achados[0].motivo, S.PLANEJAR)

    def test_raw_empacotado_e_descartado(self):
        """0x1b2017001b20 nao e um inteiro decimal. Interpretar raw empacotado
        sem tabela do fabricante e chute."""
        v = S.avaliar_disco(disco(atributos=[
            attr("Media_Wearout_Indicator", 0x1b2017001b20, id=233)]))
        self.assertEqual(v.dispositivo, S.OK)

    def test_deriva_de_ciclos_pe(self):
        v = S.avaliar_disco(disco(nand="tlc", atributos=[
            attr("Ave_Block-Erase_Count", 900, id=173)]))
        self.assertEqual(v.dispositivo, S.WARN)     # 900/1000 = 90%


class TestTemperatura(unittest.TestCase):
    """Secao 6 - envelopes diferentes para SSD e HDD."""

    def test_ssd(self):
        for t, esperado in ((45, S.OK), (55, S.INFO), (65, S.WARN),
                            (72, S.CRITICO)):
            self.assertEqual(S.avaliar_disco(disco(temp_c=t)).dispositivo,
                             esperado, t)

    def test_hdd(self):
        for t, esperado in ((35, S.OK), (42, S.INFO), (50, S.WARN),
                            (56, S.CRITICO), (10, S.CRITICO)):
            v = S.avaliar_disco(disco(tipo="hdd", temp_c=t))
            self.assertEqual(v.dispositivo, esperado, t)

    def test_maxima_historica_conta_um_nivel_abaixo(self):
        """Pico de 65 C ha seis meses e registro, nao emergencia de agora."""
        v = S.avaliar_disco(disco(temp_c=40, temp_max_c=65))
        self.assertEqual(v.dispositivo, S.INFO)

    def test_throttle_dispara_sozinho(self):
        v = S.avaliar_disco(disco(temp_c=40, throttle=True))
        self.assertEqual(v.dispositivo, S.WARN)


class TestSaudeDoHost(unittest.TestCase):
    """Secao 7 - categoria separada; e causa, nao consequencia."""

    def razao(self, sujos, ciclos):
        return S.avaliar_disco(disco(desligamentos_sujos=sujos,
                                     ciclos_energia=ciclos))

    def test_faixas(self):
        self.assertEqual(self.razao(1, 100).host, S.OK)
        self.assertEqual(self.razao(10, 100).host, S.INFO)
        self.assertEqual(self.razao(20, 100).host, S.WARN)
        self.assertEqual(self.razao(40, 100).host, S.CRITICO)

    def test_nao_contamina_o_dispositivo(self):
        v = self.razao(40, 100)
        self.assertEqual(v.dispositivo, S.OK)

    def test_recomenda_nobreak_nao_troca_de_disco(self):
        v = self.razao(40, 100)
        self.assertIn("nobreak", v.achados[0].mensagem)
        self.assertIn("nao e a causa", v.achados[0].mensagem)


class TestAntiRuido(unittest.TestCase):
    """Secao 8."""

    def test_subir_exige_duas_leituras(self):
        e = S.Estabilizador()
        quente = [S.Achado(S.DISPOSITIVO, S.WARN, "temp", "61 C")]
        self.assertEqual(e.estabilizar("X1", quente), [])       # 1a: segura
        self.assertEqual(e.estabilizar("X1", quente)[0].severidade, S.WARN)

    def test_pico_isolado_nao_promove(self):
        e = S.Estabilizador()
        e.estabilizar("X1", [S.Achado(S.DISPOSITIVO, S.WARN, "temp", "61 C")])
        e.estabilizar("X1", [])                                  # voltou
        self.assertEqual(e.estabilizar("X1",
                         [S.Achado(S.DISPOSITIVO, S.WARN, "temp", "61 C")]), [])

    def test_debounce_nao_repete_na_janela(self):
        e = S.Estabilizador()
        a = S.Achado(S.DISPOSITIVO, S.CRITICO, "imediato:pendentes", "10")
        self.assertTrue(e.deve_notificar("X1", a, 0.0))
        self.assertFalse(e.deve_notificar("X1", a, 3600.0))      # 1h depois
        self.assertTrue(e.deve_notificar("X1", a, 7 * 3600.0))   # 7h depois

    def test_info_nao_notifica(self):
        e = S.Estabilizador()
        a = S.Achado(S.DISPOSITIVO, S.INFO, "x", "y")
        self.assertFalse(e.deve_notificar("X1", a, 0.0))

    def test_contador_que_diminui_e_anomalia(self):
        """So cresce. Diminuiu = disco trocado na baia, firmware bugado ou
        parsing errado - nunca melhora."""
        self.assertTrue(S.contador_regrediu(10, 4))
        self.assertFalse(S.contador_regrediu(4, 10))


class TestValidacaoDaSpec(unittest.TestCase):
    """Os sete casos da secao 10, na ordem em que ela os lista."""

    def test_1_wd_blue_do_exemplo(self):
        """WD Blue 240G: 4 blocos crescidos, reserva 98, 0 erros, 11% de
        desgaste, 39 de 90 desligamentos sujos (razao 0,43).

        DIVERGENCIA: a secao 10 espera `host` em WARN. A tabela da secao 7 e o
        esqueleto de configuracao da secao 9 dizem os dois que acima de 0,30 e
        CRITICO, e 0,43 esta acima. Seguimos as duas fontes concordantes.
        """
        v = S.avaliar_disco(disco(
            modelo="WDC WDS240G2G0A", serial="WD-1", percentual_usado=11,
            desligamentos_sujos=39, ciclos_energia=90,
            atributos=[
                attr("Available_Reservd_Space", 0, id=232, valor=98,
                     limiar=10),
                parado("Grown_Bad_Blocks", 4, id=170),
                attr("Current_Pending_Sector", 0, id=197),
                attr("Reported_Uncorrect", 0, id=187),
            ]))
        self.assertEqual(v.dispositivo, S.OK, [a.mensagem for a in v.achados])
        self.assertEqual(v.host, S.CRITICO)
        self.assertEqual(v.interconexao, S.OK)

    def test_2_duzentos_setores_estaticos_ha_um_ano(self):
        """No maximo INFO. Sair WARN significa que a regra de taxa esta sendo
        ignorada - o principio 3 vence a tabela de contagem bruta."""
        v = S.avaliar_disco(disco(tipo="hdd", atributos=[
            parado("Reallocated_Sector_Ct", 200, id=5)]))
        self.assertLessEqual(v.dispositivo, S.INFO,
                             [a.mensagem for a in v.achados])

    def test_3_crescimento_recente_vence_reserva_saudavel(self):
        """0 -> 6 em poucos dias, com reserva em 97.

        DIVERGENCIA: a secao 10 espera CRITICO. Pela tabela da secao 3, seis
        novos em sete dias e WARN (>= 3) e nao CRITICO (>= 10); so vira
        CRITICO se cinco deles cairem em 24 h. Os dois casos abaixo cobrem a
        diferenca, que e onde o surto acontece.
        """
        espalhado = S.avaliar_disco(disco(atributos=[
            attr("Available_Reservd_Space", 0, id=232, valor=97),
            attr("Grown_Bad_Blocks", 6, id=170, d24=1, d7=6, d30=6,
                 base30=0, amostras=30)]))
        self.assertEqual(espalhado.dispositivo, S.WARN)

        surto = S.avaliar_disco(disco(atributos=[
            attr("Available_Reservd_Space", 0, id=232, valor=97),
            attr("Grown_Bad_Blocks", 6, id=170, d24=5, d7=6, d30=6,
                 base30=0, amostras=30)]))
        self.assertEqual(surto.dispositivo, S.CRITICO)

    def test_4_pendente_com_o_resto_limpo(self):
        v = S.avaliar_disco(disco(atributos=[
            attr("Current_Pending_Sector", 2, id=197),
            attr("Available_Reservd_Space", 0, id=232, valor=99)]))
        self.assertEqual(v.dispositivo, S.WARN)

    def test_5_sem_historico_nao_e_ok(self):
        """Dispositivo novo no inventario: as regras de taxa ficam em
        "sem dados", que e diferente de afirmar que esta tudo bem."""
        v = S.avaliar_disco(disco(atributos=[
            attr("Reallocated_Sector_Ct", 3, id=5)]))   # sem amostras
        self.assertIn("realocados", v.sem_dados)

    def test_6_vendor_specific_fora_da_tabela_e_ignorado(self):
        v = S.avaliar_disco(disco(atributos=[
            attr("Unknown_Attribute_177", 999999, id=177)]))
        self.assertEqual(v.dispositivo, S.OK)
        self.assertEqual(v.achados, [])

    def test_7_falha_de_coleta_nao_e_ok(self):
        """Disco atras de RAID sem -d: a coleta falha em silencio. Isso
        precisa ser um estado proprio, senao ele fica saudavel para sempre."""
        v = S.avaliar_disco(disco(coleta_ok=False,
                                  erro_coleta="precisa de -d megaraid,N"))
        self.assertFalse(v.coleta_ok)
        self.assertNotEqual(v.dispositivo, S.OK)
        self.assertIn("desconhecida", v.resumo())


class TestComposicao(unittest.TestCase):
    def test_severidade_e_o_maximo_nunca_a_media(self):
        v = S.avaliar_disco(disco(temp_c=45, atributos=[
            attr("Available_Reservd_Space", 0, id=232, valor=99),
            attr("Reported_Uncorrect", 1, id=187)]))
        self.assertEqual(v.dispositivo, S.CRITICO)

    def test_nunca_afirma_que_o_disco_esta_saudavel(self):
        """Entre 23% e 36% dos discos que falharam nao tinham indicador SMART.
        Prometer saude e o unico erro deste modulo que custaria dados."""
        texto = S.avaliar_disco(disco()).resumo().lower()
        self.assertNotIn("saudavel", texto)
        self.assertNotIn("saudável", texto)
        self.assertIn("sem indicadores", texto)


if __name__ == "__main__":
    unittest.main(verbosity=2)
