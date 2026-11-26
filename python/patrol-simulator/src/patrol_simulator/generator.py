"""Frame generation.

The generator produces the frames a run would send, including the broken ones.
It separates the fault that was injected from the frame that results, because
the point of the exercise is to compare what the platform concluded with what
actually happened — and that comparison is worthless if the only record of the
injected fault is the frame itself.
"""

from __future__ import annotations

import datetime as dt
import random
from dataclasses import dataclass, field
from typing import Any

from .scenario import Scenario, Unit

#: Degrees of latitude per tick, roughly 20 m at Seoul's latitude: a patrol
#: moving slowly enough that a checkpoint radius means something.
STEP_LATITUDE = 0.0002
STEP_LONGITUDE = 0.00025


@dataclass
class Frame:
    """One frame to send, with the fault that shaped it."""

    unit_id: str
    session_id: str
    sequence: int
    observed_at: dt.datetime
    sensors: dict[str, Any]
    #: Simulated send time, which is when the gateway would receive it. A
    #: delayed frame is measured early and sent late.
    send_at: dt.datetime
    #: The fault that produced this frame, for the run report. Empty for a
    #: frame that is exactly what the unit measured.
    injected: tuple[str, ...] = field(default_factory=tuple)
    #: Padding that makes the frame oversized, kept separate so the payload
    #: stays readable.
    padding: str = ""

    def payload(self) -> dict[str, Any]:
        """The body as the gateway's contract expects it."""
        body: dict[str, Any] = {
            "unitId": self.unit_id,
            "sessionId": self.session_id,
            "sequence": self.sequence,
            "observedAt": _iso(self.observed_at),
            "sensors": self.sensors,
        }
        if self.padding:
            # An oversized frame has to be oversized on the wire, so the
            # padding rides inside a declared field; the gateway refuses it for
            # its size before it ever looks at the contents.
            body["sensors"] = dict(self.sensors)
            body["sessionId"] = self.session_id + self.padding
        return body


@dataclass
class Run:
    """Everything a scenario produces: the frames, and what was injected."""

    scenario_name: str
    seed: int
    started_at: dt.datetime
    frames: tuple[Frame, ...]

    def injected_faults(self) -> dict[str, int]:
        """How many frames carry each fault, for the run report."""
        counts: dict[str, int] = {}
        for frame in self.frames:
            for fault in frame.injected:
                counts[fault] = counts.get(fault, 0) + 1
        return counts


def _iso(moment: dt.datetime) -> str:
    return moment.astimezone(dt.UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def generate(scenario: Scenario, started_at: dt.datetime) -> Run:
    """Produces the frames of one run.

    The same scenario and start time always produce the same frames: the
    randomness is seeded from the manifest, so a failure can be reproduced
    exactly.
    """
    frames: list[Frame] = []

    for unit in scenario.units:
        frames.extend(_frames_for(scenario, unit, started_at))

    # Sending order follows the simulated clock, which is what makes a delayed
    # frame arrive out of order without anyone reordering it by hand.
    frames.sort(key=lambda frame: (frame.send_at, frame.unit_id, frame.sequence))

    return Run(
        scenario_name=scenario.name,
        seed=scenario.seed,
        started_at=started_at,
        frames=tuple(frames),
    )


def _session_id(scenario: Scenario, unit: Unit, started_at: dt.datetime, index: int) -> str:
    """Names a session.

    A run is one connected run of a unit, so the start instant is part of the
    name: replaying the same manifest later is a new session, not a replay of
    the old sequence numbers. Two runs from the same instant still produce the
    same names, which is what keeps a scenario reproducible.
    """
    stamp = started_at.astimezone(dt.UTC).strftime("%Y%m%dT%H%M%S")
    return f"{scenario.name}-{unit.unit_id}-{stamp}-{index}"


def _frames_for(scenario: Scenario, unit: Unit, started_at: dt.datetime) -> list[Frame]:
    rng = random.Random(f"{scenario.seed}:{unit.unit_id}")
    frames: list[Frame] = []

    sessions = 1
    session_id = _session_id(scenario, unit, started_at, sessions)
    sequence = 0
    latitude, longitude = unit.latitude, unit.longitude
    battery = float(unit.battery)
    link = unit.link

    for tick in range(1, scenario.ticks + 1):
        faults = {fault.kind: fault for fault in scenario.faults_for(unit.unit_id, tick)}
        moment = started_at + dt.timedelta(seconds=scenario.tick_seconds * (tick - 1))

        # A disconnect ends the session: numbering restarts, and the platform
        # has to treat that as a reconnect rather than as duplicates.
        if "disconnect" in faults:
            sessions += 1
            session_id = _session_id(scenario, unit, started_at, sessions)
            sequence = 0

        sequence += 1
        latitude += STEP_LATITUDE + rng.uniform(-0.00004, 0.00004)
        longitude += STEP_LONGITUDE + rng.uniform(-0.00004, 0.00004)
        battery = max(0.0, battery - rng.uniform(0.4, 0.9))
        link = max(0, min(100, link + rng.randint(-3, 3)))

        if "drop" in faults:
            # The frame is never sent, but its sequence number is spent. The
            # gateway sees the hole when the next frame arrives, which is the
            # gap it has to report.
            continue

        sensors: dict[str, Any] = {
            "position": {"latitude": round(latitude, 6), "longitude": round(longitude, 6)},
            "battery": int(round(battery)),
            "link": link,
        }
        injected: list[str] = []
        observed_at = moment
        send_at = moment
        padding = ""

        if "out_of_range" in faults:
            sensors["battery"] = 150
            injected.append("out_of_range")
        if "future" in faults:
            observed_at = moment + dt.timedelta(minutes=5)
            injected.append("future")
        if "delay" in faults:
            held = faults["delay"].delay_seconds or 600
            send_at = moment + dt.timedelta(seconds=held)
            injected.append("delay")
        if "oversized" in faults:
            padding = "x" * 8192
            injected.append("oversized")

        frame = Frame(
            unit_id=unit.unit_id,
            session_id=session_id,
            sequence=sequence,
            observed_at=observed_at,
            sensors=sensors,
            send_at=send_at,
            injected=tuple(injected),
            padding=padding,
        )
        frames.append(frame)

        if "duplicate" in faults:
            frames.append(
                Frame(
                    unit_id=frame.unit_id,
                    session_id=frame.session_id,
                    sequence=frame.sequence,
                    observed_at=frame.observed_at,
                    sensors=dict(frame.sensors),
                    # The copy arrives a moment later, as a retransmission
                    # would.
                    send_at=frame.send_at + dt.timedelta(seconds=1),
                    injected=frame.injected + ("duplicate",),
                )
            )

    if any(fault.kind == "reorder" for fault in scenario.faults if fault.unit == unit.unit_id):
        frames = _swap_for_reorder(scenario, unit, frames)

    return frames


def _swap_for_reorder(scenario: Scenario, unit: Unit, frames: list[Frame]) -> list[Frame]:
    """Swaps the send times of a frame and its successor.

    Reordering is a property of the network, not of the unit: the measurements
    keep their own times and only their arrival order changes, which is exactly
    the case the projection has to survive.
    """
    for fault in scenario.faults:
        if fault.unit != unit.unit_id or fault.kind != "reorder":
            continue

        indexes = [
            index
            for index, frame in enumerate(frames)
            if frame.sequence in (fault.at_tick, fault.at_tick + 1)
        ]
        if len(indexes) < 2:
            continue

        first, second = indexes[0], indexes[1]
        frames[first].send_at, frames[second].send_at = (
            frames[second].send_at,
            frames[first].send_at,
        )
        frames[first].injected += ("reorder",)
        frames[second].injected += ("reorder",)

    return frames
