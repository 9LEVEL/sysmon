#!/usr/bin/env python3
"""Gera o icone do sysmon: o PNG mestre, o .ico e o recurso do Windows.

    python3 docs/icone/gerar.py

Produz, a partir de codigo e nao de um arquivo binario opaco:

    docs/icone/sysmon-<n>.png     o desenho, em cada tamanho
    docs/icone/sysmon.ico         o icone multi-tamanho
    cmd/sysmon/icone_windows.syso o recurso que o linkeditor do Go embute

O .syso e o que faz o Explorer, a barra de tarefas e o Alt+Tab mostrarem o
icone: o Go linka automaticamente qualquer *.syso que encontre no diretorio do
pacote main, e o sufixo _windows restringe isso ao Windows. Nao ha passo de
build extra nem ferramenta externa - por isso ele e versionado junto.

O desenho e a curva do osciloscopio do topo da janela, que e a imagem mais
reconhecivel do programa, sem moldura. A restricao que manda e 16px: e onde o
icone passa 99% do tempo, na bandeja e na barra de tarefas.
"""
import math
import struct
from pathlib import Path

from PIL import Image, ImageDraw

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parent.parent
G = 1024                                   # grade de trabalho
TAMANHOS = [256, 128, 64, 48, 32, 24, 16]

CIANO = (57, 197, 207, 255)                # tela.Ativo, a cor do "esta vivo"


def pontos_curva(y0, amp, x0=70, x1=954, passos=420):
    """A curva do osciloscopio.

    Um seno amortecido por uma gaussiana: sobe, desce e volta ao repouso nas
    pontas, que e o que faz o desenho ler como "sinal" e nao como onda infinita.
    """
    pts = []
    for i in range(passos + 1):
        t = i / passos
        x = x0 + (x1 - x0) * t
        y = y0 - amp * math.sin(2 * math.pi * (t * 1.35)) * \
            math.exp(-((t - 0.5) ** 2) * 1.2)
        pts.append((x, y))
    return pts


def carimbar(img, pts, cor, larg, alfa=255):
    """Traco grosso, carimbando circulos ao longo do caminho.

    ImageDraw.line com traco grosso emenda os segmentos por retangulos, e numa
    curva sobram entalhes na borda de fora. O joint="curve" melhora mas nao
    resolve - fica um serrilhado que aparece justo no tamanho grande, onde o
    icone e julgado de perto.
    """
    camada = Image.new("RGBA", img.size, (0, 0, 0, 0))
    d = ImageDraw.Draw(camada)
    r = larg / 2
    for x, y in pts:
        d.ellipse([x - r, y - r, x + r, y + r], fill=cor[:3] + (255,))
    if alfa < 255:
        camada.putalpha(camada.getchannel("A").point(lambda v: v * alfa // 255))
    img.alpha_composite(camada)


def desenhar():
    """O icone, em 1024, com fundo transparente."""
    img = Image.new("RGBA", (G, G), (0, 0, 0, 0))
    pts = pontos_curva(512, 260)
    # Halo primeiro, numa camada propria: no mesmo desenho o alfa somaria
    # sobre si mesmo a cada carimbo e sairia um borrao irregular.
    carimbar(img, pts, CIANO, 124, 70)
    carimbar(img, pts, CIANO, 62)
    # O ponto da leitura atual, na ponta - o mesmo que a janela desenha.
    d = ImageDraw.Draw(img)
    x, y = pts[-1]
    d.ellipse([x - 60, y - 60, x + 60, y + 60], fill=(57, 197, 207, 80))
    d.ellipse([x - 34, y - 34, x + 34, y + 34], fill=CIANO)
    return img


# ------------------------------------------------------------------- .syso
#
# Um .syso e um objeto COFF com uma secao .rsrc. O formato esta em "PE Format"
# da Microsoft; o que segue e o minimo que o linkeditor do Go aceita.

RT_ICON, RT_GROUP_ICON = 3, 14
IMAGE_SCN_CNT_INITIALIZED_DATA = 0x00000040
IMAGE_SCN_MEM_READ = 0x40000000
IMAGE_REL_AMD64_ADDR32NB = 0x0003


def _dir(entradas, dados_ofs):
    """Serializa um no da arvore de recursos.

    A arvore tem tres niveis fixos - tipo, nome, idioma -, e cada no e um
    cabecalho de 16 bytes seguido das entradas ordenadas por id.
    """
    cab = struct.pack("<IIHHHH", 0, 0, 0, 0, 0, len(entradas))
    corpo = b""
    for ident, offset, folha in sorted(entradas):
        corpo += struct.pack("<II", ident, offset | (0 if folha else 0x80000000))
    return cab + corpo


def syso(icones):
    """Monta o objeto COFF com os icones dentro.

    icones: lista de (id, bytes do PNG/DIB, largura, altura, bpp).
    """
    # --- as folhas: uma entrada de dados por icone, mais o grupo
    grupo = struct.pack("<HHH", 0, 1, len(icones))
    for ident, dados, larg, alt, bpp in icones:
        grupo += struct.pack("<BBBBHHIH",
                             larg % 256, alt % 256, 0, 0, 1, bpp,
                             len(dados), ident)

    folhas = [(RT_ICON, ident, dados) for ident, dados, *_ in icones]
    folhas.append((RT_GROUP_ICON, 1, grupo))

    # --- layout: diretorios, depois as entradas de dados, depois os dados
    tipos = sorted({t for t, _, _ in folhas})
    tam_dir = 16 + 8 * len(tipos)
    for t in tipos:
        nomes = [f for f in folhas if f[0] == t]
        tam_dir += 16 + 8 * len(nomes)          # nivel do nome
        tam_dir += len(nomes) * (16 + 8)        # um nivel de idioma por nome

    ofs_entradas = tam_dir
    ofs_dados = ofs_entradas + 16 * len(folhas)

    # cada dado alinhado a 8 bytes
    pos, mapa_dados = ofs_dados, {}
    blob = b""
    for t, ident, dados in folhas:
        mapa_dados[(t, ident)] = (pos, len(dados))
        blob += dados
        resto = (-len(dados)) % 8
        blob += b"\0" * resto
        pos += len(dados) + resto

    # --- escreve a arvore
    rsrc = bytearray(ofs_dados)
    p = 0

    def escreve_dir(off, entradas):
        struct.pack_into("<IIHHHH", rsrc, off, 0, 0, 0, 0, 0, len(entradas))
        for i, (ident, alvo, folha) in enumerate(sorted(entradas)):
            struct.pack_into("<II", rsrc, off + 16 + 8 * i,
                             ident, alvo if folha else alvo | 0x80000000)

    # nivel 1 (tipos)
    p = 16 + 8 * len(tipos)
    nivel1 = []
    posicoes_nome = {}
    for t in tipos:
        posicoes_nome[t] = p
        nomes = [f for f in folhas if f[0] == t]
        p += 16 + 8 * len(nomes)
        nivel1.append((t, posicoes_nome[t], False))
    escreve_dir(0, nivel1)

    posicoes_lang = {}
    for t in tipos:
        nomes = sorted({n for tt, n, _ in folhas if tt == t})
        entradas = []
        for n in nomes:
            posicoes_lang[(t, n)] = p
            p += 16 + 8
            entradas.append((n, posicoes_lang[(t, n)], False))
        escreve_dir(posicoes_nome[t], entradas)

    i_entrada = 0
    entradas_dados = {}
    for t in tipos:
        for n in sorted({n for tt, n, _ in folhas if tt == t}):
            off = ofs_entradas + 16 * i_entrada
            entradas_dados[(t, n)] = off
            escreve_dir(posicoes_lang[(t, n)], [(0, off, True)])
            i_entrada += 1

    rsrc = bytes(rsrc) + blob

    # --- as entradas de dados, e as relocacoes que apontam para elas
    relocs = b""
    rsrc = bytearray(rsrc)
    for (t, n), off in entradas_dados.items():
        pos_dado, tam = mapa_dados[(t, n)]
        struct.pack_into("<IIII", rsrc, off, pos_dado, tam, 0, 0)
        # O OffsetToData e um RVA: o linkeditor precisa somar o endereco da
        # secao, e e isso que a relocacao pede.
        # O simbolo da secao e o indice 0 da tabela.
        relocs += struct.pack("<IIH", off, 0, IMAGE_REL_AMD64_ADDR32NB)
    rsrc = bytes(rsrc)

    # --- COFF
    ofs_secao = 20 + 40
    ofs_relocs = ofs_secao + len(rsrc)
    ofs_simbolos = ofs_relocs + len(relocs)

    cab = struct.pack("<HHIIIHH", 0x8664, 1, 0, ofs_simbolos, 2, 0, 0)
    secao = struct.pack("<8sIIIIIIHHI", b".rsrc\0\0\0", 0, 0,
                        len(rsrc), ofs_secao, ofs_relocs, 0,
                        len(relocs) // 10, 0,
                        IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ)
    # Dois registros de 18 bytes: o simbolo da secao e o auxiliar dele.
    # O auxiliar tem campos fixos - Length, NumberOfRelocations,
    # NumberOfLinenumbers, CheckSum, Number, Selection, 3 bytes nao usados -
    # e passar de 18 faz o Go recusar com "fail to read string table",
    # porque a tabela de strings passa a ser lida do lugar errado.
    simbolos = struct.pack("<8sIhHBB", b".rsrc\0\0\0", 0, 1, 0, 3, 1)
    simbolos += struct.pack("<IHHIHB3s", len(rsrc), len(relocs) // 10, 0,
                            0, 0, 0, b"\0\0\0")
    assert len(simbolos) == 36, len(simbolos)
    tabela_strings = struct.pack("<I", 4)

    return cab + secao + rsrc + relocs + simbolos + tabela_strings


def png_bytes(img, tam):
    from io import BytesIO
    b = BytesIO()
    img.resize((tam, tam), Image.LANCZOS).save(b, format="PNG")
    return b.getvalue()


def main():
    img = desenhar()
    for t in TAMANHOS:
        img.resize((t, t), Image.LANCZOS).save(AQUI / f"sysmon-{t}.png")
    img.save(AQUI / "sysmon-1024.png")

    ico = AQUI / "sysmon.ico"
    img.save(ico, format="ICO",
             sizes=[(t, t) for t in sorted(TAMANHOS)])
    print(f"  {ico.relative_to(RAIZ)}")

    icones = []
    for i, t in enumerate(sorted(TAMANHOS), start=1):
        icones.append((i, png_bytes(img, t), t, t, 32))
    destino = RAIZ / "cmd" / "sysmon" / "icone_windows.syso"
    destino.write_bytes(syso(icones))
    print(f"  {destino.relative_to(RAIZ)}  ({destino.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
