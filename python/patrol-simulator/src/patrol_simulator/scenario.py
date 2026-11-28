"""Scenario manifests: what to simulate, and which faults to inject.

A fault test that cannot be reproduced cannot be diagnosed, so a run is
declared rather than improvised: the manifest names the units, the pace, the
seed and the faults, and the same manifest always produces the same frames.
"""

from __future__ import annotations

import datetime as dt
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

#: Faults the simulator knows how to inject. Each one matches a row of the
#: failure table in docs/scenarios/telemetry-reliability.md.
FAULTS = frozenset(
    {
        "delay",  # the frame is received long after it was measured
        "duplicate",  # the frame is sent twice
        "reorder",  # two consecutive frames swap places
        "drop",  # the frame is never sent, leaving a sequence gap
        "disconnect",  # the session ends and a new one starts
        "out_of_range",  # a value that cannot be true
        "oversized",  # a frame larger than the gateway accepts
        "future",  # a measurement stamped ahead of the gateway clock
        "sensor_dropout",  # the unit stops reporting a sensor from this tick on
    }
)

#: Sensors a frame may report, and therefore a dropout may suppress.
SENSORS = ("position", "battery", "link")


class ScenarioError(ValueError):
    """The manifest cannot be used as written."""


@dataclass(frozen=True)
class Fault:
    """One fault, injected at a given tick of a given unit."""

    kind: str
    #: Tick the fault applies to, counting from 1.
    at_tick: int
    unit: str
    #: How long the frame is held before it is sent, for `delay`.
    delay_seconds: int = 0
    #: Sensors the unit stops reporting, for `sensor_dropout`. The dropout
    #: lasts to the end of the run: a sensor that failed does not come back on
    #: the next tick, and letting it would make the fault untestable — the
    #: point is to watch one reading age while the others stay current.
    sensors: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if self.kind not in FAULTS:
            raise ScenarioError(f"unknown fault {self.kind!r}; known faults are {sorted(FAULTS)}")
        if self.at_tick < 1:
            raise ScenarioError(f"fault tick must start at 1, got {self.at_tick}")
        if self.kind == "sensor_dropout":
            if not self.sensors:
                raise ScenarioError("sensor_dropout must name the sensors that stop reporting")
            unknown = sorted(set(self.sensors) - set(SENSORS))
            if unknown:
                raise ScenarioError(f"unknown sensors {unknown}; known sensors are {list(SENSORS)}")
            if set(self.sensors) == set(SENSORS):
                # A frame with no sensor at all is a different fault, and the
                # gateway refuses it as NO_SENSOR_VALUE. Dropping everything
                # here would silently stop the unit instead of ageing one
                # reading, which is what this fault is for.
                raise ScenarioError(
                    "sensor_dropout cannot drop every sensor; that is a silent unit"
                )


@dataclass(frozen=True)
class Unit:
    """A unit to simulate, and where it starts."""

    unit_id: str
    callsign: str
    latitude: float
    longitude: float
    battery: int = 90
    link: int = 95


@dataclass(frozen=True)
class Scenario:
    """A declared run."""

    name: str
    seed: int
    ticks: int
    #: Simulated seconds between frames.
    tick_seconds: int
    units: tuple[Unit, ...]
    faults: tuple[Fault, ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        if self.ticks < 1:
            raise ScenarioError(f"a scenario needs at least one tick, got {self.ticks}")
        if self.tick_seconds < 1:
            raise ScenarioError(f"tick_seconds must be positive, got {self.tick_seconds}")
        if not self.units:
            raise ScenarioError("a scenario needs at least one unit")

        callsigns = {unit.unit_id for unit in self.units}
        for fault in self.faults:
            if fault.unit not in callsigns:
                raise ScenarioError(
                    f"fault {fault.kind!r} names unit {fault.unit!r}, "
                    "which the scenario does not simulate"
                )
            if fault.at_tick > self.ticks:
                raise ScenarioError(
                    f"fault {fault.kind!r} is set for tick {fault.at_tick}, "
                    f"past the {self.ticks} ticks of the run"
                )

    def faults_for(self, unit_id: str, tick: int) -> tuple[Fault, ...]:
        """Faults that apply to one unit at one tick."""
        return tuple(
            fault for fault in self.faults if fault.unit == unit_id and fault.at_tick == tick
        )

    def dropped_sensors(self, unit_id: str, tick: int) -> frozenset[str]:
        """Sensors this unit has stopped reporting by this tick."""
        dropped: set[str] = set()
        for fault in self.faults:
            if fault.kind != "sensor_dropout" or fault.unit != unit_id:
                continue
            if fault.at_tick <= tick:
                dropped.update(fault.sensors)
        return frozenset(dropped)

    def duration(self) -> dt.timedelta:
        """How much simulated time the run covers."""
        return dt.timedelta(seconds=self.ticks * self.tick_seconds)


def load_scenario(path: str | Path) -> Scenario:
    """Reads a manifest from disk."""
    text = Path(path).read_text(encoding="utf-8")
    return parse_scenario(yaml.safe_load(text))


def parse_scenario(document: Any) -> Scenario:
    """Builds a scenario from a parsed manifest."""
    if not isinstance(document, dict):
        raise ScenarioError("a manifest must be a mapping")

    missing = {"name", "seed", "ticks", "units"} - document.keys()
    if missing:
        raise ScenarioError(f"manifest is missing {sorted(missing)}")

    units = tuple(
        Unit(
            unit_id=str(entry["unitId"]),
            callsign=str(entry.get("callsign", entry["unitId"])),
            latitude=float(entry["latitude"]),
            longitude=float(entry["longitude"]),
            battery=int(entry.get("battery", 90)),
            link=int(entry.get("link", 95)),
        )
        for entry in document["units"]
    )

    faults = tuple(
        Fault(
            kind=str(entry["kind"]),
            at_tick=int(entry["atTick"]),
            unit=str(entry["unit"]),
            delay_seconds=int(entry.get("delaySeconds", 0)),
            sensors=tuple(str(sensor) for sensor in entry.get("sensors", ())),
        )
        for entry in document.get("faults", [])
    )

    return Scenario(
        name=str(document["name"]),
        seed=int(document["seed"]),
        ticks=int(document["ticks"]),
        tick_seconds=int(document.get("tickSeconds", 5)),
        units=units,
        faults=faults,
    )
