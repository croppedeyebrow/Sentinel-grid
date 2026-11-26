//! The gateway is checked against the published contract, not against its own
//! idea of a frame. Every example in `contracts/examples` has to decode here,
//! and what the gateway would send back has to match the published shape — the
//! same files the Go service and the console are verified against.

use std::fs;
use std::path::PathBuf;

use chrono::{DateTime, TimeZone, Utc};
use sentinel_telemetry_gateway::{decode, Limits, Position, Rejection};

fn contracts_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../contracts")
}

fn example(name: &str) -> Vec<u8> {
    let path = contracts_dir()
        .join("examples")
        .join(format!("{name}.json"));
    fs::read(&path).unwrap_or_else(|error| panic!("read {}: {error}", path.display()))
}

/// Later than every timestamp in the published examples, so a fixed example is
/// never refused for being "in the future" as the calendar moves on.
fn after_the_examples() -> DateTime<Utc> {
    Utc.with_ymd_and_hms(2026, 11, 26, 10, 0, 0)
        .single()
        .expect("valid timestamp")
}

#[test]
fn decodes_the_published_frame_example() {
    let bytes = example("telemetry-frame");

    let frame = decode(&bytes, after_the_examples(), &Limits::default()).expect("valid frame");

    assert_eq!(frame.unit_id, "30000000-0000-0000-0000-000000000001");
    assert_eq!(frame.sequence, 42);
    assert_eq!(
        frame.sensors.position,
        Some(Position {
            latitude: 37.5665,
            longitude: 126.978
        })
    );
    assert_eq!(frame.sensors.battery, Some(82));
    assert_eq!(frame.sensors.link, Some(94));
}

#[test]
fn decodes_the_published_partial_frame_example() {
    // Per-sensor freshness only works if a frame may carry one sensor, so the
    // contract publishes that case and the gateway has to accept it.
    let bytes = example("telemetry-frame-partial");

    let frame = decode(&bytes, after_the_examples(), &Limits::default()).expect("valid frame");

    assert_eq!(frame.sensors.battery, Some(81));
    assert_eq!(frame.sensors.position, None);
    assert_eq!(frame.sensors.link, None);
}

#[test]
fn a_decoded_frame_serialises_back_to_the_published_shape() {
    let bytes = example("telemetry-frame");
    let frame = decode(&bytes, after_the_examples(), &Limits::default()).expect("valid frame");

    let round_tripped: serde_json::Value =
        serde_json::from_str(&serde_json::to_string(&frame).expect("serialise"))
            .expect("parse serialised frame");
    let published: serde_json::Value =
        serde_json::from_slice(&bytes).expect("parse published example");

    assert_eq!(
        round_tripped, published,
        "the gateway's frame shape drifted from the published contract"
    );
}

#[test]
fn a_partial_frame_does_not_invent_absent_sensors() {
    // Serialising must not turn an unreported sensor into an explicit null:
    // "not measured" and "measured as nothing" are different claims.
    let bytes = example("telemetry-frame-partial");
    let frame = decode(&bytes, after_the_examples(), &Limits::default()).expect("valid frame");

    let serialised = serde_json::to_string(&frame).expect("serialise");

    assert!(
        !serialised.contains("null"),
        "absent sensors must be omitted, got {serialised}"
    );
}

#[test]
fn the_rejection_codes_match_the_published_vocabulary() {
    let schema = fs::read_to_string(
        contracts_dir()
            .join("schemas")
            .join("telemetry-event.schema.json"),
    )
    .expect("read telemetry event schema");

    // Every code the gateway can produce has to exist in the published
    // vocabulary, or the receipt log would carry a code no reader knows.
    let codes = [
        Rejection::Malformed(String::new()),
        Rejection::TooLarge { bytes: 1, limit: 1 },
        Rejection::NoSensorValue,
        Rejection::OutOfRange {
            sensor: "battery",
            value: "150".to_owned(),
        },
        Rejection::InvalidSequence(0),
        Rejection::MeasuredInFuture { ahead_seconds: 60 },
        Rejection::UnknownUnit(String::new()),
    ];

    for rejection in codes {
        let code = rejection.code();
        assert!(
            schema.contains(&format!("\"{code}\"")),
            "code {code} is not in the published rejection vocabulary"
        );
    }
}

#[test]
fn refuses_a_frame_the_contract_forbids() {
    // The schema requires at least one sensor and a sequence of at least 1.
    // The gateway has to enforce the same, or it would accept frames the
    // contract says cannot exist.
    let empty_sensors = br#"{"unitId":"u","sessionId":"s","sequence":1,
        "observedAt":"2026-11-26T09:00:00Z","sensors":{}}"#;

    let rejection =
        decode(empty_sensors, after_the_examples(), &Limits::default()).expect_err("refused");

    assert_eq!(rejection.code(), "NO_SENSOR_VALUE");
}
