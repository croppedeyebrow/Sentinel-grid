"""Command line entry point."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import sys
import time

from .generator import generate
from .scenario import ScenarioError, load_scenario
from .sender import GatewayClient, send_run


def run_window(scenario) -> dt.datetime:
    """Start time that makes the run end at the present moment.

    A scenario describes a stretch of patrol, and the gateway refuses
    measurements stamped ahead of its clock — rightly so. Generating the run
    forward from now would therefore have most of it refused as measured in the
    future, so the run is placed in the recent past and its last tick lands
    about now.
    """
    return dt.datetime.now(dt.UTC) - scenario.duration()


def build_parser() -> argparse.ArgumentParser:
    """Builds the argument parser."""
    parser = argparse.ArgumentParser(
        prog="patrol-simulator",
        description="Runs a declared telemetry scenario against the gateway.",
    )
    parser.add_argument("scenario", help="path to a scenario manifest")
    parser.add_argument(
        "--gateway",
        default="http://localhost:8090",
        help="gateway base URL (default: %(default)s)",
    )
    parser.add_argument(
        "--token",
        default="",
        help="bearer token the gateway requires; also read from TELEMETRY_TOKEN",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="print the frames the scenario would send without sending them",
    )
    parser.add_argument(
        "--loop",
        action="store_true",
        help="repeat the scenario until interrupted, for a live demo",
    )
    parser.add_argument(
        "--pause",
        type=float,
        default=0.0,
        help="seconds to wait between frames when sending (default: %(default)s)",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    """Runs the simulator."""
    import os

    arguments = build_parser().parse_args(argv)

    try:
        scenario = load_scenario(arguments.scenario)
    except (OSError, ScenarioError) as error:
        print(f"scenario cannot be used: {error}", file=sys.stderr)
        return 2

    if arguments.dry_run:
        run = generate(scenario, run_window(scenario))
        for frame in run.frames:
            print(json.dumps({"injected": list(frame.injected), **frame.payload()}))
        print(
            f"{len(run.frames)} frames, injected: {run.injected_faults()}",
            file=sys.stderr,
        )
        return 0

    token = arguments.token or os.environ.get("TELEMETRY_TOKEN", "")
    if not token:
        print("a gateway token is required: pass --token or set TELEMETRY_TOKEN", file=sys.stderr)
        return 2

    with GatewayClient(arguments.gateway, token) as client:
        while True:
            run = generate(scenario, run_window(scenario))
            report = send_run(run, client)

            print(
                json.dumps(
                    {
                        "scenario": report.scenario_name,
                        "seed": report.seed,
                        "frames": len(report.answers),
                        "injected": report.injected(),
                        "outcomes": report.outcomes(),
                        "refused": [
                            {"code": answer.code, "sequence": answer.frame.sequence}
                            for answer in report.refused()
                        ],
                    },
                    ensure_ascii=False,
                )
            )

            if not arguments.loop:
                return 0
            time.sleep(max(arguments.pause, 1.0))


if __name__ == "__main__":
    raise SystemExit(main())
