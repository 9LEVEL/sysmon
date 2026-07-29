#!/usr/bin/env python3
"""
sysmon_agent.py - agente de telemetria read-only para host Proxmox/Debian.

Expõe UM endpoint JSON. Não executa comandos, não aceita escrita, não tem
dependência externa (stdlib pura -> nada de pip no host do PVE).

    GET /metrics   -> JSON com temperaturas, load, RAM, discos, thin pool
    GET /health    -> {"ok": true}   (sem autenticação, para healthcheck)

Autenticação: header  Authorization: Bearer <token>   ou  ?token=<token>

Uso:
    export SYSMON_TOKEN="$(openssl rand -hex 24)"
    python3 sysmon_agent.py --host 10.0.0.5 --port 9109
"""

from __future__ import annotations

import argparse
import hmac
import json
import os
import socket
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse, parse_qs

__version__ = "1.1.0"

HWMON = Path("/sys/class/hwmon")
THERMAL = Path("/sys/class/thermal")
DMI = Path("/sys/class/dmi/id")
THINPOOL = Path("/run/sysmon/thinpool.json")  # escrito pelo sysmon-thinpool.timer

CACHE_TTL = 1.0  # segundos; evita que N clientes martelem o sysfs


# ------------------------------------------------------------------ helpers
def read(path: Path) -> str | None:
    try:
        return path.read_text(errors="replace").strip()
    except OSError:
        return None


def read_int(path: Path) -> int | None:
    v = read(path)
    try:
        return int(v) if v is not None else None
    except (TypeError, ValueError):
        return None


# ------------------------------------------------------------------ coletas
def temps() -> list[dict]:
    out: list[dict] = []
    for hw in sorted(HWMON.glob("hwmon*")):
        chip = read(hw / "name") or hw.name
        for inp in sorted(hw.glob("temp*_input")):
            raw = read_int(inp)
            if raw is None:
                continue
            pre = inp.name.removesuffix("_input")
            crit = read_int(hw / f"{pre}_crit")
            out.append({
                "chip": chip,
                "label": read(hw / f"{pre}_label") or pre,
                "c": round(raw / 1000, 1),
                "crit": round(crit / 1000, 1) if crit else None,
            })

    if not out and THERMAL.is_dir():
        for z in sorted(THERMAL.glob("thermal_zone*")):
            raw = read_int(z / "temp")
            if raw is not None:
                out.append({"chip": "thermal_zone",
                            "label": read(z / "type") or z.name,
                            "c": round(raw / 1000, 1), "crit": None})
    return out


def sensor_cpu(lista: list[dict]) -> dict | None:
    """Escolhe o sensor mais representativo da CPU (valor + crítico)."""
    prefer = ("package id 0", "tctl", "tdie", "cpu")
    for alvo in prefer:
        for t in lista:
            if t["label"].lower().startswith(alvo):
                return t
    candidatos = [t for t in lista
                  if t["chip"] in ("coretemp", "k10temp", "zenpower")]
    if candidatos:
        return max(candidatos, key=lambda t: t["c"])
    return max(lista, key=lambda t: t["c"]) if lista else None


def fans() -> dict[str, int]:
    out: dict[str, int] = {}
    for hw in sorted(HWMON.glob("hwmon*")):
        chip = read(hw / "name") or hw.name
        for inp in sorted(hw.glob("fan*_input")):
            rpm = read_int(inp)
            if rpm:
                pre = inp.name.removesuffix("_input")
                label = read(hw / f"{pre}_label") or pre
                out[f"{chip}/{label}"] = rpm
    return out


_cpu_prev: tuple[int, int] | None = None


def cpu_percent() -> float | None:
    """Uso de CPU por delta de /proc/stat entre chamadas consecutivas."""
    global _cpu_prev
    linhas = (read(Path("/proc/stat")) or "").splitlines()
    if not linhas or not linhas[0].startswith("cpu "):
        return None
    campos = [int(x) for x in linhas[0].split()[1:]]
    idle = campos[3] + (campos[4] if len(campos) > 4 else 0)
    total = sum(campos)
    anterior, _cpu_prev = _cpu_prev, (idle, total)
    if anterior is None:
        return None
    d_idle, d_total = idle - anterior[0], total - anterior[1]
    if d_total <= 0:
        return None
    return round(100 * (1 - d_idle / d_total), 1)


def memoria() -> dict:
    info: dict[str, int] = {}
    for linha in (read(Path("/proc/meminfo")) or "").splitlines():
        chave, _, resto = linha.partition(":")
        try:
            info[chave] = int(resto.split()[0]) * 1024
        except (IndexError, ValueError):
            continue
    total = info.get("MemTotal", 0)
    disp = info.get("MemAvailable", 0)
    swap_t = info.get("SwapTotal", 0)
    swap_l = info.get("SwapFree", 0)
    return {
        "total": total,
        "usado": total - disp,
        "percent": round(100 * (total - disp) / total, 1) if total else None,
        "swap_total": swap_t,
        "swap_usado": swap_t - swap_l,
        "swap_percent": round(100 * (swap_t - swap_l) / swap_t, 1) if swap_t else None,
    }


def discos(pontos: list[str]) -> list[dict]:
    """Uso por ponto de montagem, deduplicando filesystems iguais por st_dev."""
    out, vistos = [], set()
    for mp in pontos:
        try:
            dev = os.stat(mp).st_dev
            st = os.statvfs(mp)
        except OSError:
            continue
        if dev in vistos:
            continue
        vistos.add(dev)
        total = st.f_blocks * st.f_frsize
        livre = st.f_bavail * st.f_frsize
        if total == 0:
            continue
        out.append({
            "mount": mp,
            "total": total,
            "usado": total - livre,
            "percent": round(100 * (total - livre) / total, 1),
        })
    return out


def thinpools() -> list[dict]:
    """Lê o snapshot de `lvs` gravado pelo timer. O agente nunca executa nada."""
    bruto = read(THINPOOL)
    if not bruto:
        return []
    try:
        relatorio = json.loads(bruto)["report"][0]["lv"]
    except (json.JSONDecodeError, KeyError, IndexError):
        return []
    saida = []
    for lv in relatorio:
        try:
            saida.append({
                "nome": f"{lv['vg_name']}/{lv['lv_name']}",
                "data_percent": round(float(lv["data_percent"]), 1),
                "meta_percent": round(float(lv["metadata_percent"]), 1),
            })
        except (KeyError, TypeError, ValueError):
            continue
    return saida


def guests() -> dict | None:
    """Conta VMs/CTs lendo /etc/pve/.vmlist. Requer grupo www-data."""
    bruto = read(Path("/etc/pve/.vmlist"))
    if not bruto:
        return None
    try:
        dados = json.loads(bruto).get("ids", {})
    except json.JSONDecodeError:
        return None
    return {
        "qemu": sum(1 for v in dados.values() if v.get("type") == "qemu"),
        "lxc": sum(1 for v in dados.values() if v.get("type") == "lxc"),
    }


def coletar(mounts: list[str]) -> dict:
    lista = temps()
    cpu = sensor_cpu(lista)
    load1, load5, load15 = os.getloadavg()
    uptime = float((read(Path("/proc/uptime")) or "0").split()[0])
    return {
        "v": __version__,
        "ts": time.time(),
        "host": socket.gethostname(),
        "uptime_s": int(uptime),
        "load": [round(load1, 2), round(load5, 2), round(load15, 2)],
        "cpus": os.cpu_count(),
        "cpu_percent": cpu_percent(),
        "cpu_temp": cpu["c"] if cpu else None,
        "cpu_crit": cpu["crit"] if cpu else None,
        "temps": lista,
        "fans": fans(),
        "mem": memoria(),
        "discos": discos(mounts),
        "thinpools": thinpools(),
        "guests": guests(),
        "placa_mae": read(DMI / "board_name"),
    }


# ------------------------------------------------------------------ servidor
class Estado:
    def __init__(self, mounts: list[str]) -> None:
        self.mounts = mounts
        self.lock = threading.Lock()
        self.cache: dict | None = None
        self.expira = 0.0
        cpu_percent()  # primeira leitura para inicializar o delta

    def metrics(self) -> dict:
        with self.lock:
            agora = time.monotonic()
            if self.cache is None or agora >= self.expira:
                self.cache = coletar(self.mounts)
                self.expira = agora + CACHE_TTL
            return self.cache


class Handler(BaseHTTPRequestHandler):
    server_version = f"sysmon-agent/{__version__}"
    protocol_version = "HTTP/1.1"
    estado: Estado
    token: str

    def _json(self, code: int, payload: dict) -> None:
        corpo = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(corpo)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(corpo)

    def _autorizado(self, query: dict) -> bool:
        cabecalho = self.headers.get("Authorization", "")
        if cabecalho.startswith("Bearer "):
            enviado = cabecalho[7:]
        else:
            enviado = (query.get("token") or [""])[0]
        return hmac.compare_digest(enviado, self.token)

    def do_GET(self) -> None:  # noqa: N802
        url = urlparse(self.path)
        query = parse_qs(url.query)

        if url.path == "/health":
            return self._json(200, {"ok": True, "v": __version__})

        if url.path != "/metrics":
            return self._json(404, {"erro": "rota inexistente"})

        if not self._autorizado(query):
            time.sleep(0.5)  # freia tentativa de força bruta
            return self._json(401, {"erro": "token invalido"})

        try:
            self._json(200, self.estado.metrics())
        except Exception as exc:  # nunca derruba o servidor
            self._json(500, {"erro": type(exc).__name__})

    def log_message(self, fmt: str, *args) -> None:
        # journald já carimba data/hora; loga só erros
        if len(args) > 1 and str(args[1]).startswith(("4", "5")):
            sys.stderr.write(f"{self.address_string()} {fmt % args}\n")


def main() -> None:
    p = argparse.ArgumentParser(description="Agente de telemetria read-only")
    p.add_argument("--host", default="127.0.0.1",
                   help="IP de bind. Use o IP da LAN, NUNCA 0.0.0.0 exposto")
    p.add_argument("--port", type=int, default=9109)
    p.add_argument("--mounts", nargs="*", default=["/", "/var/lib/vz"],
                   help="pontos de montagem a reportar")
    p.add_argument("--version", action="version", version=__version__)
    args = p.parse_args()

    token = os.environ.get("SYSMON_TOKEN", "").strip()
    if len(token) < 16:
        sys.exit("ERRO: defina SYSMON_TOKEN com pelo menos 16 caracteres.")

    Handler.estado = Estado(args.mounts)
    Handler.token = token

    servidor = ThreadingHTTPServer((args.host, args.port), Handler)
    servidor.daemon_threads = True
    print(f"sysmon-agent {__version__} em http://{args.host}:{args.port}/metrics",
          file=sys.stderr)
    try:
        servidor.serve_forever()
    except KeyboardInterrupt:
        servidor.shutdown()


if __name__ == "__main__":
    main()
