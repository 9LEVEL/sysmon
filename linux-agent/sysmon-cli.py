#!/usr/bin/env python3
"""
sysmon.py - leitor de temperaturas e info de hardware no Linux.

Uso:
    python3.12 sysmon.py            # snapshot único
    python3.12 sysmon.py --watch    # atualiza a cada 2s
    python3.12 sysmon.py --json     # saída em JSON (para scripts/dashboards)

Dependências: nenhuma (stdlib). Se 'psutil' estiver instalado, é usado como
fonte extra de fans/bateria.
"""

from __future__ import annotations

import argparse
import json
import time
from dataclasses import dataclass, asdict
from pathlib import Path

HWMON = Path("/sys/class/hwmon")
DMI = Path("/sys/class/dmi/id")
THERMAL = Path("/sys/class/thermal")


# ---------------------------------------------------------------- utilidades
def read(path: Path) -> str | None:
    """Lê um arquivo do sysfs devolvendo None em qualquer erro (permissão etc)."""
    try:
        return path.read_text(errors="replace").strip()
    except (OSError, PermissionError):
        return None


def read_int(path: Path) -> int | None:
    v = read(path)
    try:
        return int(v) if v is not None else None
    except ValueError:
        return None


# ---------------------------------------------------------------- estruturas
@dataclass
class Sensor:
    chip: str          # ex: coretemp, nvme, amdgpu
    label: str         # ex: Package id 0, Core 0
    current: float     # °C
    high: float | None = None
    critical: float | None = None

    def status(self) -> str:
        limit = self.critical or self.high
        if limit is None:
            return "ok"
        if self.current >= limit:
            return "CRÍTICO"
        if self.current >= limit * 0.85:
            return "alerta"
        return "ok"


# ---------------------------------------------------------------- coletores
def collect_temps() -> list[Sensor]:
    """Percorre /sys/class/hwmon lendo todos os tempN_input."""
    sensors: list[Sensor] = []
    if not HWMON.is_dir():
        return sensors

    for hw in sorted(HWMON.iterdir()):
        chip = read(hw / "name") or hw.name
        for inp in sorted(hw.glob("temp*_input")):
            raw = read_int(inp)
            if raw is None:
                continue
            prefix = inp.name.removesuffix("_input")  # ex: temp1
            label = read(hw / f"{prefix}_label") or prefix
            high = read_int(hw / f"{prefix}_max")
            crit = read_int(hw / f"{prefix}_crit")
            sensors.append(
                Sensor(
                    chip=chip,
                    label=label,
                    current=raw / 1000,
                    high=high / 1000 if high else None,
                    critical=crit / 1000 if crit else None,
                )
            )

    # Fallback: thermal zones (comum em ARM / notebooks sem hwmon exposto)
    if not sensors and THERMAL.is_dir():
        for zone in sorted(THERMAL.glob("thermal_zone*")):
            raw = read_int(zone / "temp")
            if raw is not None:
                sensors.append(
                    Sensor(chip="thermal_zone",
                           label=read(zone / "type") or zone.name,
                           current=raw / 1000)
                )
    return sensors


def collect_fans() -> dict[str, int]:
    fans: dict[str, int] = {}
    for hw in sorted(HWMON.glob("hwmon*")):
        chip = read(hw / "name") or hw.name
        for inp in sorted(hw.glob("fan*_input")):
            rpm = read_int(inp)
            if rpm:
                prefix = inp.name.removesuffix("_input")
                label = read(hw / f"{prefix}_label") or prefix
                fans[f"{chip}/{label}"] = rpm
    return fans


def collect_hardware() -> dict[str, str]:
    """Placa-mãe, BIOS, fabricante e CPU."""
    campos = {
        "Fabricante (sistema)": "sys_vendor",
        "Modelo": "product_name",
        "Placa-mãe": "board_name",
        "Fabricante da placa": "board_vendor",
        "BIOS": "bios_version",
        "Data da BIOS": "bios_date",
        "Chassi": "chassis_type",
    }
    info = {}
    for nome, arquivo in campos.items():
        valor = read(DMI / arquivo)
        if valor and valor.lower() not in {"to be filled by o.e.m.", "default string", ""}:
            info[nome] = valor

    # CPU a partir do /proc/cpuinfo
    cpuinfo = read(Path("/proc/cpuinfo")) or ""
    for linha in cpuinfo.splitlines():
        if linha.startswith("model name"):
            info["CPU"] = linha.split(":", 1)[1].strip()
            break
    info["Núcleos lógicos"] = str(cpuinfo.count("processor\t"))
    return info


def collect_psutil_extras() -> dict:
    """Bateria e temperaturas extras, só se psutil existir."""
    try:
        import psutil
    except ImportError:
        return {}

    extras: dict = {}
    bat = psutil.sensors_battery()
    if bat:
        extras["bateria"] = {
            "percentual": bat.percent,
            "na_tomada": bat.power_plugged,
        }
    extras["cpu_percent"] = psutil.cpu_percent(interval=0.1)
    extras["ram_percent"] = psutil.virtual_memory().percent
    return extras


# ---------------------------------------------------------------- saída
def snapshot() -> dict:
    return {
        "hardware": collect_hardware(),
        "temperaturas": [asdict(s) | {"status": s.status()} for s in collect_temps()],
        "fans_rpm": collect_fans(),
        "extras": collect_psutil_extras(),
    }


def imprimir(dados: dict) -> None:
    print("\033[1m=== HARDWARE ===\033[0m")
    for k, v in dados["hardware"].items():
        print(f"  {k:<22} {v}")

    print("\n\033[1m=== TEMPERATURAS ===\033[0m")
    if not dados["temperaturas"]:
        print("  Nenhum sensor encontrado. Rode 'sudo sensors-detect' (pacote lm-sensors).")
    for s in dados["temperaturas"]:
        cor = {"ok": "\033[32m", "alerta": "\033[33m", "CRÍTICO": "\033[31m"}[s["status"]]
        limite = f"  (crit: {s['critical']:.0f}°C)" if s["critical"] else ""
        print(f"  {s['chip']:<12} {s['label']:<20} {cor}{s['current']:>6.1f}°C\033[0m{limite}")

    if dados["fans_rpm"]:
        print("\n\033[1m=== VENTOINHAS ===\033[0m")
        for nome, rpm in dados["fans_rpm"].items():
            print(f"  {nome:<33} {rpm:>6} RPM")

    if dados["extras"]:
        print("\n\033[1m=== USO ===\033[0m")
        e = dados["extras"]
        if "cpu_percent" in e:
            print(f"  CPU: {e['cpu_percent']}%   RAM: {e['ram_percent']}%")
        if "bateria" in e:
            tomada = "na tomada" if e["bateria"]["na_tomada"] else "na bateria"
            print(f"  Bateria: {e['bateria']['percentual']:.0f}% ({tomada})")


def main() -> None:
    p = argparse.ArgumentParser(description="Monitor de sensores para Linux")
    p.add_argument("--watch", action="store_true", help="atualiza continuamente")
    p.add_argument("--intervalo", type=float, default=2.0, help="segundos entre updates")
    p.add_argument("--json", action="store_true", help="saída em JSON")
    args = p.parse_args()

    if args.json:
        print(json.dumps(snapshot(), indent=2, ensure_ascii=False))
        return

    if not args.watch:
        imprimir(snapshot())
        return

    try:
        while True:
            print("\033[2J\033[H", end="")  # limpa a tela
            imprimir(snapshot())
            print(f"\n(atualizando a cada {args.intervalo}s — Ctrl+C para sair)")
            time.sleep(args.intervalo)
    except KeyboardInterrupt:
        print("\nSaindo.")


if __name__ == "__main__":
    main()
