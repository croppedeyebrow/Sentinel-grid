"""Sending frames to the gateway.

The simulator holds no database credentials and knows nothing about missions:
it posts frames and records what the gateway answered. Comparing those answers
with the faults that were injected is how the run is judged.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import httpx

from .generator import Frame, Run


@dataclass
class Answer:
    """What the gateway said about one frame."""

    status: int
    #: Outcome for an accepted frame, or the refusal code for a refused one.
    code: str
    body: dict[str, Any]
    frame: Frame

    @property
    def refused(self) -> bool:
        """Whether the frame was kept out of the platform."""
        return self.status >= 400


@dataclass
class Report:
    """The result of a run: what was injected, and what came back."""

    scenario_name: str
    seed: int
    answers: list[Answer] = field(default_factory=list)

    def outcomes(self) -> dict[str, int]:
        """How many frames came back with each outcome or refusal code."""
        counts: dict[str, int] = {}
        for answer in self.answers:
            counts[answer.code] = counts.get(answer.code, 0) + 1
        return counts

    def injected(self) -> dict[str, int]:
        """How many frames carried each injected fault."""
        counts: dict[str, int] = {}
        for answer in self.answers:
            for fault in answer.frame.injected:
                counts[fault] = counts.get(fault, 0) + 1
        return counts

    def refused(self) -> list[Answer]:
        """Frames the gateway kept out."""
        return [answer for answer in self.answers if answer.refused]


class GatewayClient:
    """Posts frames to the telemetry gateway."""

    def __init__(self, base_url: str, token: str, timeout: float = 5.0) -> None:
        self._client = httpx.Client(
            base_url=base_url.rstrip("/"),
            headers={"Authorization": f"Bearer {token}"},
            timeout=timeout,
        )

    def __enter__(self) -> GatewayClient:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def close(self) -> None:
        """Releases the connection pool."""
        self._client.close()

    def send(self, frame: Frame) -> Answer:
        """Posts one frame and reads the gateway's answer."""
        response = self._client.post("/api/v1/telemetry/frames", json=frame.payload())

        try:
            body = response.json()
        except ValueError:
            body = {}

        # An accepted frame answers with its outcome, a refused one with the
        # reason: both are keyed off a single field for the report.
        code = str(body.get("outcome") or body.get("code") or response.status_code)

        return Answer(status=response.status_code, code=code, body=body, frame=frame)

    def resume_point(self, unit_id: str) -> dict[str, Any]:
        """Asks where a unit should resume after a reconnect."""
        response = self._client.get(f"/api/v1/telemetry/units/{unit_id}/resume")
        response.raise_for_status()
        return dict(response.json())


def send_run(run: Run, client: GatewayClient) -> Report:
    """Sends every frame of a run in simulated send order."""
    report = Report(scenario_name=run.scenario_name, seed=run.seed)

    for frame in run.frames:
        report.answers.append(client.send(frame))

    return report
