"""Deterministic patrol telemetry simulator.

The simulator produces the frames a patrol would send, including the broken
ones, from a declared scenario with a fixed seed. It is a test instrument, not
a device: it has no database credentials and no authority over missions.
"""

from .generator import Frame, Run, generate
from .scenario import FAULTS, Fault, Scenario, ScenarioError, Unit, load_scenario, parse_scenario
from .sender import Answer, GatewayClient, Report, send_run

__all__ = [
    "FAULTS",
    "Answer",
    "Fault",
    "Frame",
    "GatewayClient",
    "Report",
    "Run",
    "Scenario",
    "ScenarioError",
    "Unit",
    "generate",
    "load_scenario",
    "parse_scenario",
    "send_run",
]
