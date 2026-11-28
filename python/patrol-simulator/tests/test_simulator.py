"""Tests for the simulator.

The simulator is a measuring instrument, so what it has to prove about itself
is that it is repeatable and that it injects the faults it claims to inject.
"""

from __future__ import annotations

import datetime as dt
import json
from pathlib import Path

import pytest

from patrol_simulator import (
    FAULTS,
    ScenarioError,
    generate,
    load_scenario,
    parse_scenario,
)

SCENARIOS = Path(__file__).resolve().parents[1] / "scenarios"
STARTED_AT = dt.datetime(2026, 11, 26, 9, 0, 0, tzinfo=dt.UTC)


def scenario(name: str):
    return load_scenario(SCENARIOS / f"{name}.yaml")


def test_the_published_scenarios_load():
    for path in SCENARIOS.glob("*.yaml"):
        assert load_scenario(path).ticks > 0, path.name


def test_the_same_scenario_and_seed_produce_identical_frames():
    # A fault test that cannot be reproduced cannot be diagnosed.
    first = generate(scenario("unreliable-link"), STARTED_AT)
    second = generate(scenario("unreliable-link"), STARTED_AT)

    assert [frame.payload() for frame in first.frames] == [
        frame.payload() for frame in second.frames
    ]
    assert [frame.injected for frame in first.frames] == [frame.injected for frame in second.frames]


def test_a_different_seed_produces_different_frames():
    document = {
        "name": "seeded",
        "seed": 1,
        "ticks": 5,
        "units": [
            {"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9},
        ],
    }
    first = generate(parse_scenario(document), STARTED_AT)
    second = generate(parse_scenario({**document, "seed": 2}), STARTED_AT)

    assert [frame.payload() for frame in first.frames] != [
        frame.payload() for frame in second.frames
    ]


def test_a_clean_run_injects_nothing():
    run = generate(scenario("nominal"), STARTED_AT)

    assert run.injected_faults() == {}
    assert len(run.frames) == 12
    assert [frame.sequence for frame in run.frames] == list(range(1, 13))


def test_frames_report_every_sensor_by_default():
    run = generate(scenario("nominal"), STARTED_AT)

    sensors = run.frames[0].payload()["sensors"]
    assert set(sensors) == {"position", "battery", "link"}
    assert 0 <= sensors["battery"] <= 100
    assert 0 <= sensors["link"] <= 100


def test_a_dropped_frame_leaves_a_hole_in_the_sequence():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    unit = "30000000-0000-0000-0000-000000000001"
    first_session = min(frame.session_id for frame in run.frames if frame.unit_id == unit)
    sequences = sorted(
        frame.sequence
        for frame in run.frames
        if frame.unit_id == unit and frame.session_id == first_session
    )

    # Tick 8 was dropped, so its sequence number is spent and never sent.
    assert 8 not in sequences
    assert 7 in sequences and 9 in sequences


def test_a_duplicate_sends_the_same_sequence_twice():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    unit = "30000000-0000-0000-0000-000000000001"
    duplicates = [
        frame for frame in run.frames if frame.unit_id == unit and "duplicate" in frame.injected
    ]

    assert len(duplicates) == 1
    # The session matters: a reconnect later in the run restarts numbering,
    # so the same sequence legitimately appears again in the new session.
    twin = [
        frame
        for frame in run.frames
        if frame.session_id == duplicates[0].session_id and frame.sequence == duplicates[0].sequence
    ]
    assert len(twin) == 2
    assert twin[0].payload()["sensors"] == twin[1].payload()["sensors"]


def test_a_delayed_frame_is_measured_early_and_sent_late():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    delayed = next(frame for frame in run.frames if "delay" in frame.injected)

    assert delayed.send_at - delayed.observed_at == dt.timedelta(seconds=600)


def test_a_reordered_pair_keeps_its_measurement_times():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    reordered = [frame for frame in run.frames if "reorder" in frame.injected]
    assert len(reordered) == 2

    earlier, later = sorted(reordered, key=lambda frame: frame.sequence)
    # Only the arrival order changes: the measurements keep their own times,
    # which is the case the projection has to survive.
    assert earlier.observed_at < later.observed_at
    assert earlier.send_at > later.send_at


def test_a_disconnect_starts_a_new_session_and_restarts_numbering():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    unit = "30000000-0000-0000-0000-000000000001"
    sessions = {frame.session_id for frame in run.frames if frame.unit_id == unit}
    assert len(sessions) == 2

    # The session opened by the reconnect is the later of the two.
    second = max(sessions)
    restarted = sorted(frame.sequence for frame in run.frames if frame.session_id == second)
    assert restarted[0] == 1


def test_an_out_of_range_frame_claims_an_impossible_value():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    broken = next(frame for frame in run.frames if "out_of_range" in frame.injected)

    assert broken.payload()["sensors"]["battery"] == 150


def test_a_future_frame_is_stamped_ahead_of_its_tick():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    ahead = next(frame for frame in run.frames if "future" in frame.injected)

    assert ahead.observed_at > ahead.send_at


def test_an_oversized_frame_is_actually_oversized():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    big = next(frame for frame in run.frames if "oversized" in frame.injected)

    assert len(json.dumps(big.payload())) > 4096


def test_frames_are_sent_in_simulated_arrival_order():
    run = generate(scenario("unreliable-link"), STARTED_AT)

    send_times = [frame.send_at for frame in run.frames]
    assert send_times == sorted(send_times)


def test_the_injected_fault_is_recorded_apart_from_the_frame():
    # The run report has to say what was injected, because comparing the
    # platform's conclusion with the frame alone would be circular.
    run = generate(scenario("unreliable-link"), STARTED_AT)

    counts = run.injected_faults()
    assert counts["duplicate"] == 1
    assert counts["out_of_range"] == 1
    assert counts["oversized"] == 1
    assert set(counts) <= FAULTS | {"duplicate"}


def test_a_manifest_naming_an_unknown_fault_is_refused():
    with pytest.raises(ScenarioError, match="unknown fault"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [{"kind": "explode", "atTick": 1, "unit": "unit-1"}],
            }
        )


def test_a_fault_for_a_unit_not_in_the_run_is_refused():
    with pytest.raises(ScenarioError, match="does not simulate"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [{"kind": "drop", "atTick": 1, "unit": "unit-9"}],
            }
        )


def test_a_fault_past_the_end_of_the_run_is_refused():
    with pytest.raises(ScenarioError, match="past the"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [{"kind": "drop", "atTick": 9, "unit": "unit-1"}],
            }
        )


def test_a_manifest_without_units_is_refused():
    with pytest.raises(ScenarioError, match="at least one unit"):
        parse_scenario({"name": "bad", "seed": 1, "ticks": 3, "units": []})


def test_a_later_run_is_a_new_session_not_a_replay():
    # Re-running a manifest must not resend the same session and sequence
    # numbers: the platform would rightly call every frame a duplicate.
    first = generate(scenario("nominal"), STARTED_AT)
    later = generate(scenario("nominal"), STARTED_AT + dt.timedelta(minutes=5))

    assert {frame.session_id for frame in first.frames}.isdisjoint(
        frame.session_id for frame in later.frames
    )
    # The frames themselves are still deterministic for a given start.
    assert [frame.sequence for frame in first.frames] == [frame.sequence for frame in later.frames]


def test_a_dropped_sensor_stops_reporting_while_the_others_continue():
    # TEL-01 from streamed data: one reading has to be able to age while the
    # others stay current, which a frame that always carries all three sensors
    # could never show.
    run = generate(scenario("partial-sensors"), STARTED_AT)

    before = [frame for frame in run.frames if frame.sequence < 10]
    after = [frame for frame in run.frames if frame.sequence >= 10]

    assert all("position" in frame.payload()["sensors"] for frame in before)
    assert all("position" not in frame.payload()["sensors"] for frame in after)
    # The sensors that did not fail keep arriving to the last tick.
    assert all({"battery", "link"} <= set(frame.payload()["sensors"]) for frame in after)


def test_a_dropout_lasts_to_the_end_of_the_run():
    run = generate(scenario("partial-sensors"), STARTED_AT)

    last = max(run.frames, key=lambda frame: frame.sequence)
    assert "position" not in last.payload()["sensors"]
    assert run.injected_faults()["sensor_dropout"] >= 1


def test_the_dropped_reading_ages_past_the_others():
    # The gap between the last position and the last battery is what makes the
    # stale-beside-fresh case visible at all, so it is asserted here rather
    # than left to the live run.
    run = generate(scenario("partial-sensors"), STARTED_AT)

    with_position = [frame for frame in run.frames if "position" in frame.payload()["sensors"]]
    last_position = max(frame.observed_at for frame in with_position)
    last_battery = max(
        frame.observed_at for frame in run.frames if "battery" in frame.payload()["sensors"]
    )

    assert last_battery - last_position >= dt.timedelta(minutes=10)


def test_a_dropout_that_names_no_sensor_is_refused():
    with pytest.raises(ScenarioError, match="must name the sensors"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [{"kind": "sensor_dropout", "atTick": 1, "unit": "unit-1"}],
            }
        )


def test_a_dropout_that_names_an_unknown_sensor_is_refused():
    with pytest.raises(ScenarioError, match="unknown sensors"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [
                    {
                        "kind": "sensor_dropout",
                        "atTick": 1,
                        "unit": "unit-1",
                        "sensors": ["altitude"],
                    }
                ],
            }
        )


def test_a_dropout_cannot_silence_a_unit_entirely():
    # Dropping everything is a silent unit, not an ageing reading, and the
    # gateway already refuses an empty frame as NO_SENSOR_VALUE.
    with pytest.raises(ScenarioError, match="silent unit"):
        parse_scenario(
            {
                "name": "bad",
                "seed": 1,
                "ticks": 3,
                "units": [{"unitId": "unit-1", "latitude": 37.5, "longitude": 126.9}],
                "faults": [
                    {
                        "kind": "sensor_dropout",
                        "atTick": 1,
                        "unit": "unit-1",
                        "sensors": ["position", "battery", "link"],
                    }
                ],
            }
        )
