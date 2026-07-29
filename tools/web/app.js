/* sysmon - dashboard da frota.
 *
 * Sem framework e sem CDN: a pagina e servida localmente e precisa funcionar
 * offline, entao vale mais nao ter dependencia nenhuma do que economizar
 * algumas linhas de render.
 *
 * A severidade NAO e calculada aqui. O servidor manda `nivel` e `alertas`
 * prontos, vindos do mesmo avaliar() que o tray do Windows usa - "o que conta
 * como alerta" tem uma definicao so no projeto inteiro.
 */
'use strict';

const NIVEL = { 0: 'ok', 1: 'aviso', 2: 'critico', 3: 'offline' };
const COR = {
  0: 'var(--ok)', 1: 'var(--aviso)', 2: 'var(--critico)', 3: 'var(--offline)',
};

// ------------------------------------------------------------------ formato
const num = (v, casas = 0) =>
  v === null || v === undefined || Number.isNaN(v) ? null : Number(v).toFixed(casas);

function bytes(n) {
  if (n === null || n === undefined) return '--';
  const u = ['B', 'K', 'M', 'G', 'T', 'P'];
  let i = 0;
  while (Math.abs(n) >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)}${u[i]}`;
}

const bps = (n) => (n === null || n === undefined ? '--' : `${bytes(n)}/s`);

function uptime(s) {
  if (!s) return '--';
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : `${h}h ${String(m).padStart(2, '0')}m`;
}

function horas(h) {
  if (h === null || h === undefined) return '--';
  return h >= 8760 ? `${(h / 8760).toFixed(1)} anos` : `${h.toLocaleString('pt-BR')} h`;
}

/* Mesmos limiares de sysmon_nucleo.avaliar(), usados so para COLORIR medidas
   que nao tem nivel proprio no payload (uso de disco, desgaste). O que decide
   alerta continua sendo o servidor. */
function faixa(v, aviso, critico) {
  if (v === null || v === undefined) return 3;
  if (v >= critico) return 2;
  if (v >= aviso) return 1;
  return 0;
}

// ------------------------------------------------------------------ DOM
function el(tag, props = {}, filhos = []) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (v === null || v === undefined) continue;
    if (k === 'class') n.className = v;
    else if (k === 'text') n.textContent = v;
    else if (k === 'style') n.setAttribute('style', v);
    else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v);
  }
  for (const f of [].concat(filhos)) if (f) n.append(f);
  return n;
}

function svgEl(tag, props = {}) {
  const n = document.createElementNS('http://www.w3.org/2000/svg', tag);
  for (const [k, v] of Object.entries(props)) {
    if (v !== null && v !== undefined) n.setAttribute(k, v);
  }
  return n;
}

// ------------------------------------------------------------------ gauge
const R = 28;                          // raio do anel
const CIRC = 2 * Math.PI * R;          // 175.93
const ARCO = 0.75;                     // sweep de 270 graus, abertura embaixo

/* Anel de 270 graus. `frac` de 0 a 1; `nivel` colore; `rotulo` e `valor`
   estao sempre escritos - a cor nunca carrega a informacao sozinha. */
function gauge({ frac, nivel, valor, unidade, rotulo, detalhe, titulo }) {
  const svg = svgEl('svg', {
    width: 76, height: 76, viewBox: '0 0 72 72', role: 'img',
    'aria-label': `${rotulo}: ${valor ?? 'sem leitura'}${unidade || ''}`,
  });

  const comum = {
    cx: 36, cy: 36, r: R, fill: 'none', 'stroke-width': 6,
    transform: 'rotate(135 36 36)', 'stroke-linecap': 'round',
  };
  // Trilha recessiva: presente para dar a escala, sem competir com o dado.
  svg.append(svgEl('circle', {
    ...comum, stroke: 'var(--grade)',
    'stroke-dasharray': `${(CIRC * ARCO).toFixed(2)} ${CIRC.toFixed(2)}`,
  }));

  if (frac !== null && frac !== undefined) {
    const usado = CIRC * ARCO * Math.max(0, Math.min(1, frac));
    svg.append(svgEl('circle', {
      ...comum, stroke: COR[nivel],
      'stroke-dasharray': `${usado.toFixed(2)} ${CIRC.toFixed(2)}`,
    }));
  }

  const t = svgEl('text', { x: 36, y: 38, 'text-anchor': 'middle', class: 'valor' });
  t.classList.add('num');
  t.textContent = valor ?? '--';
  if (unidade && valor !== null && valor !== undefined) {
    const u = svgEl('tspan', { class: 'unidade', dx: 1 });
    u.textContent = unidade;
    t.append(u);
  }
  svg.append(t);

  return el('div', { class: 'gauge', title: titulo || '' }, [
    svg,
    el('div', { class: 'rotulo', text: rotulo }),
    detalhe ? el('div', { class: 'detalhe num', text: detalhe }) : null,
  ]);
}

// ------------------------------------------------------------------ barra
function barra(rotulo, percent, nivel, direita, titulo) {
  return el('div', { class: 'barra', title: titulo || '' }, [
    el('div', { class: 'rot', text: rotulo }),
    el('div', { class: 'trilha' }, [
      el('div', {
        class: 'preenche',
        style: `width:${Math.max(0, Math.min(100, percent || 0))}%;--cor:${COR[nivel]}`,
      }),
    ]),
    el('div', { class: 'pct num', text: direita }),
  ]);
}

// ------------------------------------------------------------------ discos
function linhaDisco(b) {
  const smart = b.smart || {};
  const temp = num(b.temp_c);

  // Temperatura de disco: acima de 60C ja preocupa; acima de 70C e critico
  // para NVMe (a partir dai costuma haver throttling termico).
  const nivelTemp = temp === null ? 3 : faixa(b.temp_c, 60, 70);

  const topo = el('div', { class: 'topo' }, [
    el('span', { class: 'nome num', text: b.dev }),
    smart.saude === 'falha'
      ? el('span', { class: 'reprovado', text: '⚠ SMART REPROVOU' }) : null,
    el('span', { class: 'direita' }, [
      temp !== null
        ? el('span', { class: 'temp num', style: `color:${COR[nivelTemp]}`, text: `${temp}°C` })
        : el('span', { class: 'num', style: 'color:var(--tinta-3)', text: 'sem sensor' }),
      el('span', { class: 'num', style: 'color:var(--tinta-3)', text: ` · ${bytes(b.tamanho)}` }),
    ]),
  ]);

  const partes = [];
  if (smart.horas_ligado) partes.push(horas(smart.horas_ligado));
  if (smart.realocados) partes.push(`${smart.realocados} setores realocados`);
  if (smart.erros_midia) partes.push(`${smart.erros_midia} erros de mídia`);
  if (smart.spare_restante !== null && smart.spare_restante !== undefined
      && smart.spare_restante < 100) {
    partes.push(`spare ${num(smart.spare_restante)}%`);
  }

  const linha = el('div', { class: 'disco' }, [
    el('div', { class: 'selo', text: (b.tipo || '?').toUpperCase() }),
    topo,
    el('div', {
      class: 'modelo',
      text: [b.modelo || 'modelo desconhecido', ...partes].join(' · '),
      title: [b.fabricante, b.modelo].filter(Boolean).join(' '),
    }),
    el('div', { class: 'fluxo', style: 'justify-content:flex-start' }, [
      el('span', { title: 'leitura' }, [
        el('i', { style: '--c:var(--serie-1)' }),
        el('span', { class: 'num', text: bps(b.leitura_bps) }),
      ]),
      el('span', { title: 'escrita' }, [
        el('i', { style: '--c:var(--serie-2)' }),
        el('span', { class: 'num', text: bps(b.escrita_bps) }),
      ]),
      b.util_percent !== null && b.util_percent !== undefined
        ? el('span', { title: 'ocupação do disco' }, [
          el('span', { class: 'num', text: `${num(b.util_percent)}% ocupado` }),
        ]) : null,
    ]),
  ]);

  // Desgaste vira barra propria: e o numero que decide trocar o disco.
  if (smart.desgaste_percent !== null && smart.desgaste_percent !== undefined) {
    const d = smart.desgaste_percent;
    linha.append(el('div', {}, [
      barra('vida consumida', d, faixa(d, 80, 90), `${num(d)}%`,
        'percentage_used do SMART: quanto da resistência de escrita já foi gasta'),
    ]));
  }
  return linha;
}

// ------------------------------------------------------------------ host
function cartaoHost(h) {
  const d = h.dados;

  if (!d) {
    return el('article', { class: 'host offline', style: `--cor:${COR[3]}` }, [
      el('header', {}, [
        el('h2', { text: h.nome }),
        el('div', { class: 'sub', text: h.url }),
      ]),
      el('div', { class: 'corpo' }, [
        el('span', { class: 'ponto', style: `--cor:${COR[3]}` }),
        el('span', {}, [
          'Offline — ',
          el('span', { class: 'motivo', text: h.erro || 'sem dados' }),
        ]),
      ]),
    ]);
  }

  const so = d.so || {};
  const mem = d.mem || {};
  const crit = d.cpu_crit;

  // A fracao do gauge de temperatura usa o critico do proprio sensor, entao a
  // mesma tela serve para hardware com limites diferentes.
  const fracTemp = d.cpu_temp === null || d.cpu_temp === undefined
    ? null : d.cpu_temp / (crit || 100);
  const nivelTemp = d.cpu_temp === null || d.cpu_temp === undefined
    ? 3 : faixa(d.cpu_temp, (crit || 100) * 0.75, (crit || 100) * 0.9);

  const gauges = el('div', { class: 'gauges' }, [
    gauge({
      frac: fracTemp, nivel: nivelTemp, valor: num(d.cpu_temp), unidade: '°',
      rotulo: 'temp', detalhe: crit ? `crit ${num(crit)}°` : null,
      titulo: crit ? `Limiares derivados do crit do sensor: aviso ${num(crit * 0.75)}°, critico ${num(crit * 0.9)}°` : 'Sensor sem valor critico reportado',
    }),
    gauge({
      frac: d.cpu_percent === null ? null : d.cpu_percent / 100,
      nivel: faixa(d.cpu_percent, 80, 95),
      valor: num(d.cpu_percent), unidade: '%', rotulo: 'cpu',
      detalhe: d.cpus ? `${d.cpus} cpus` : null, titulo: d.cpu_modelo || '',
    }),
    gauge({
      frac: mem.percent === null ? null : (mem.percent || 0) / 100,
      nivel: faixa(mem.percent, 90, 97),
      valor: num(mem.percent), unidade: '%', rotulo: 'ram',
      detalhe: `${bytes(mem.usado)}/${bytes(mem.total)}`,
      titulo: `Cache ${bytes(mem.cache)}`,
    }),
    el('div', { class: 'stats num' }, [
      el('div', {}, ['load', el('b', { text: (d.load || []).map((n) => n.toFixed(2)).join(' ') })]),
      el('div', {}, ['up', el('b', { text: uptime(d.uptime_s) })]),
      mem.swap_percent
        ? el('div', {}, ['swap', el('b', { text: `${num(mem.swap_percent)}%` })]) : null,
      d.guests
        ? el('div', {}, ['vms', el('b', { text: `${d.guests.qemu} · ${d.guests.lxc} cts` })]) : null,
    ]),
  ]);

  const corpo = el('div', { class: 'corpo' }, [gauges]);

  const blocos = d.blocos || [];
  if (blocos.length) {
    corpo.append(el('section', { class: 'secao' }, [
      el('h3', { text: `Discos físicos (${blocos.length})` }),
      ...blocos.map(linhaDisco),
    ]));
  }

  const discos = d.discos || [];
  if (discos.length) {
    corpo.append(el('section', { class: 'secao' }, [
      el('h3', { text: 'Filesystems' }),
      ...discos.map((x) => barra(
        x.mount, x.percent, faixa(x.percent, 80, 90),
        `${num(x.percent)}%`,
        `${bytes(x.usado)} de ${bytes(x.total)}` +
        (x.inodes_percent !== null && x.inodes_percent !== undefined
          ? ` · inodes ${num(x.inodes_percent)}%` : ''))),
    ]));
  }

  for (const tp of d.thinpools || []) {
    corpo.append(el('section', { class: 'secao' }, [
      el('h3', { text: 'Thin pool LVM' }),
      barra(`${tp.nome} data`, tp.data_percent, faixa(tp.data_percent, 80, 90), `${num(tp.data_percent)}%`),
      barra(`${tp.nome} meta`, tp.meta_percent, faixa(tp.meta_percent, 80, 90), `${num(tp.meta_percent)}%`),
    ]));
  }

  const redes = (d.net || []).filter((n) => n.up);
  if (redes.length) {
    corpo.append(el('section', { class: 'secao' }, [
      el('h3', { text: 'Rede' }),
      ...redes.map((n) => el('div', { class: 'barra' }, [
        el('div', { class: 'rot num', text: n.iface }),
        el('div', { class: 'fluxo', style: 'justify-content:flex-start' }, [
          el('span', { title: 'entrada' }, [
            el('i', { style: '--c:var(--serie-1)' }),
            el('span', { class: 'num', text: bps(n.rx_bps) }),
          ]),
          el('span', { title: 'saída' }, [
            el('i', { style: '--c:var(--serie-2)' }),
            el('span', { class: 'num', text: bps(n.tx_bps) }),
          ]),
        ]),
        el('div', { class: 'pct num', style: 'color:var(--tinta-3);font-weight:400',
          text: n.mbps ? `${n.mbps}M` : '' }),
      ])),
    ]));
  }

  const raid = (d.raid || []).filter((r) => r.degradado !== null);
  if (raid.length) {
    corpo.append(el('section', { class: 'secao' }, [
      el('h3', { text: 'RAID' }),
      ...raid.map((r) => el('div', { class: 'barra' }, [
        el('div', { class: 'rot num', text: r.nome }),
        el('div', { class: 'num', style: `color:${COR[r.degradado ? 2 : 0]}`,
          text: `${r.discos} ${r.degradado ? 'degradado' : 'ok'}` }),
        el('div', {}),
      ])),
    ]));
  }

  return el('article', { class: 'host', style: `--cor:${COR[h.nivel]}` }, [
    el('header', {}, [
      el('h2', {}, [
        el('span', { class: 'ponto', style: `--cor:${COR[h.nivel]};display:inline-block;margin-right:7px` }),
        h.nome,
      ]),
      el('div', { class: 'sub' }, [
        el('div', { text: [so.nome, d.cpu_modelo].filter(Boolean).join(' · ') || d.host }),
        el('div', { class: 'num', text: [so.kernel, d.placa_mae].filter(Boolean).join(' · ') }),
      ]),
    ]),
    corpo,
  ]);
}

// ------------------------------------------------------------------ render
function render(frota) {
  const main = document.getElementById('frota');
  main.replaceChildren(...(frota.hosts.length
    ? frota.hosts.map(cartaoHost)
    : [el('p', { class: 'vazio', text: 'Nenhum host configurado.' })]));

  const offline = frota.hosts.filter((h) => h.nivel === 3).length;
  const alertas = frota.hosts.flatMap((h) => h.alertas.map((a) => [h.nome, a]));

  const contagem = document.getElementById('contagem');
  contagem.replaceChildren(
    el('span', { class: 'ponto', style: `--cor:${COR[frota.pior_nivel]}` }),
    el('b', { text: String(frota.hosts.length) }),
    document.createTextNode(frota.hosts.length === 1 ? ' host' : ' hosts'),
    ...(offline ? [document.createTextNode(` · ${offline} offline`)] : []),
    ...(alertas.length ? [document.createTextNode(` · ${alertas.length} alerta(s)`)] : []),
  );

  const painel = document.getElementById('alertas');
  const lista = document.getElementById('lista-alertas');
  painel.hidden = alertas.length === 0;
  lista.replaceChildren(...alertas.map(([host, texto]) =>
    el('li', {}, [el('b', { text: `${host}: ` }), texto])));

  document.getElementById('relogio').textContent =
    new Date(frota.ts * 1000).toLocaleTimeString('pt-BR');
  document.title = alertas.length || offline
    ? `(${alertas.length + offline}) sysmon` : 'sysmon';
  document.getElementById('rodape').textContent =
    `Atualiza a cada ${frota.intervalo}s · a coleta acontece no servidor local, ` +
    'os tokens não chegam ao browser.';
}

async function buscar() {
  try {
    const r = await fetch('/api/frota', { cache: 'no-store' });
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    render(await r.json());
  } catch (e) {
    document.getElementById('relogio').textContent = `sem o servidor (${e.message})`;
  }
}

document.getElementById('atualizar').addEventListener('click', async () => {
  await fetch('/api/atualizar', { cache: 'no-store' }).catch(() => {});
  setTimeout(buscar, 400);
});

buscar();
setInterval(buscar, 3000);
