//! Frame decoding and validation.

use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};

/// A WGS84 coordinate pair as a unit reports it.
#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
pub struct Position {
    /// Degrees north, -90 to 90.
    pub latitude: f64,
    /// Degrees east, -180 to 180.
    pub longitude: f64,
}

/// The sensors a frame carries. A frame may report any subset: a unit that
/// only measured its battery sends only a battery, and the projection then
/// updates only that sensor.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct Sensors {
    /// Measured position, if the frame carries one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub position: Option<Position>,
    /// Battery percentage, validated to 0..=100.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub battery: Option<i64>,
    /// Link quality percentage, validated to 0..=100.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub link: Option<i64>,
}

impl Sensors {
    /// Whether the frame reports anything at all.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.position.is_none() && self.battery.is_none() && self.link.is_none()
    }
}

/// One telemetry frame as received from a unit.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct Frame {
    /// The unit the frame claims to come from.
    pub unit_id: String,
    /// One connected run of that unit. A reconnect starts a new session.
    pub session_id: String,
    /// Position in the session's sequence, starting at 1.
    pub sequence: i64,
    /// When the unit measured the values, which is not when they arrived.
    pub observed_at: DateTime<Utc>,
    /// The sensors this frame reports.
    pub sensors: Sensors,
}

/// Why a frame may not enter the platform. Every variant carries a stable code
/// for the receipt log, so a rejection can be counted and explained later.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Rejection {
    /// The bytes are not a frame: bad JSON, missing field, unknown field.
    Malformed(String),
    /// The frame is larger than the gateway accepts.
    TooLarge {
        /// Size of the received frame.
        bytes: usize,
        /// Largest size the gateway accepts.
        limit: usize,
    },
    /// The frame reports no sensor at all, so there is nothing to apply.
    NoSensorValue,
    /// A reported value cannot be true.
    OutOfRange {
        /// Which sensor was out of range.
        sensor: &'static str,
        /// The offending value, for the receipt log.
        value: String,
    },
    /// Sequence numbers start at 1 and increase.
    InvalidSequence(i64),
    /// The unit claims to have measured the values in the future, so the frame
    /// cannot be ordered against anything.
    MeasuredInFuture {
        /// How far ahead of the gateway's clock the measurement claims to be.
        ahead_seconds: i64,
    },
    /// The frame identifies a unit the platform does not know.
    UnknownUnit(String),
}

impl Rejection {
    /// Stable code for the receipt log and for counters.
    #[must_use]
    pub fn code(&self) -> &'static str {
        match self {
            Self::Malformed(_) => "MALFORMED_FRAME",
            Self::TooLarge { .. } => "FRAME_TOO_LARGE",
            Self::NoSensorValue => "NO_SENSOR_VALUE",
            Self::OutOfRange { .. } => "VALUE_OUT_OF_RANGE",
            Self::InvalidSequence(_) => "INVALID_SEQUENCE",
            Self::MeasuredInFuture { .. } => "MEASURED_IN_FUTURE",
            Self::UnknownUnit(_) => "UNKNOWN_UNIT",
        }
    }

    /// Sentence for the receipt log. Clients key off [`Rejection::code`].
    #[must_use]
    pub fn detail(&self) -> String {
        match self {
            Self::Malformed(reason) => format!("frame does not match the schema: {reason}"),
            Self::TooLarge { bytes, limit } => {
                format!("frame is {bytes} bytes, limit is {limit}")
            }
            Self::NoSensorValue => "frame reports no sensor value".to_owned(),
            Self::OutOfRange { sensor, value } => format!("{sensor} is out of range: {value}"),
            Self::InvalidSequence(sequence) => format!("sequence must start at 1, got {sequence}"),
            Self::MeasuredInFuture { ahead_seconds } => {
                format!("measured {ahead_seconds}s ahead of the gateway clock")
            }
            Self::UnknownUnit(unit) => format!("unknown unit {unit}"),
        }
    }
}

/// Bounds the gateway enforces on every frame.
#[derive(Debug, Clone, Copy)]
pub struct Limits {
    /// Largest frame the gateway accepts, in bytes.
    pub max_bytes: usize,
    /// How far ahead of the gateway's clock a measurement may claim to be
    /// before it is refused. Small clock differences are normal; a frame from
    /// the future is not.
    pub max_clock_skew: Duration,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            max_bytes: 4 * 1024,
            max_clock_skew: Duration::seconds(5),
        }
    }
}

const BATTERY: &str = "battery";
const LINK: &str = "link";
const LATITUDE: &str = "latitude";
const LONGITUDE: &str = "longitude";

/// Decodes and validates one frame.
///
/// Size is checked before parsing, so an oversized frame is refused without
/// being deserialised. Everything else is reported as the specific reason it
/// was refused rather than as a generic parse failure, because the receipt log
/// has to say what was wrong with what arrived.
pub fn decode(bytes: &[u8], now: DateTime<Utc>, limits: &Limits) -> Result<Frame, Rejection> {
    if bytes.len() > limits.max_bytes {
        return Err(Rejection::TooLarge {
            bytes: bytes.len(),
            limit: limits.max_bytes,
        });
    }

    let frame: Frame =
        serde_json::from_slice(bytes).map_err(|error| Rejection::Malformed(error.to_string()))?;

    validate(&frame, now, limits)?;
    Ok(frame)
}

/// Validates an already decoded frame. Exposed separately so a caller that
/// received a frame some other way applies the same rules.
pub fn validate(frame: &Frame, now: DateTime<Utc>, limits: &Limits) -> Result<(), Rejection> {
    if frame.unit_id.trim().is_empty() {
        return Err(Rejection::Malformed("unitId is empty".to_owned()));
    }
    if frame.session_id.trim().is_empty() {
        return Err(Rejection::Malformed("sessionId is empty".to_owned()));
    }
    if frame.sequence < 1 {
        return Err(Rejection::InvalidSequence(frame.sequence));
    }
    if frame.sensors.is_empty() {
        return Err(Rejection::NoSensorValue);
    }

    let ahead = frame.observed_at - now;
    if ahead > limits.max_clock_skew {
        return Err(Rejection::MeasuredInFuture {
            ahead_seconds: ahead.num_seconds(),
        });
    }

    if let Some(position) = frame.sensors.position {
        check_range(LATITUDE, position.latitude, -90.0, 90.0)?;
        check_range(LONGITUDE, position.longitude, -180.0, 180.0)?;
    }
    if let Some(battery) = frame.sensors.battery {
        check_percentage(BATTERY, battery)?;
    }
    if let Some(link) = frame.sensors.link {
        check_percentage(LINK, link)?;
    }

    Ok(())
}

fn check_range(sensor: &'static str, value: f64, low: f64, high: f64) -> Result<(), Rejection> {
    if value.is_nan() || value < low || value > high {
        return Err(Rejection::OutOfRange {
            sensor,
            value: value.to_string(),
        });
    }
    Ok(())
}

fn check_percentage(sensor: &'static str, value: i64) -> Result<(), Rejection> {
    if !(0..=100).contains(&value) {
        return Err(Rejection::OutOfRange {
            sensor,
            value: value.to_string(),
        });
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn now() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2026-11-24T12:00:00Z")
            .expect("fixed timestamp")
            .with_timezone(&Utc)
    }

    fn frame_json(sensors: &str, extra: &str) -> String {
        format!(
            r#"{{"unitId":"unit-1","sessionId":"session-1","sequence":1,
                 "observedAt":"2026-11-24T11:59:55Z","sensors":{sensors}{extra}}}"#
        )
    }

    #[test]
    fn accepts_a_frame_reporting_every_sensor() {
        let bytes = frame_json(
            r#"{"position":{"latitude":37.5665,"longitude":126.978},"battery":82,"link":94}"#,
            "",
        );

        let frame = decode(bytes.as_bytes(), now(), &Limits::default()).expect("valid frame");

        assert_eq!(frame.unit_id, "unit-1");
        assert_eq!(frame.sequence, 1);
        assert_eq!(frame.sensors.battery, Some(82));
        assert_eq!(
            frame.sensors.position,
            Some(Position {
                latitude: 37.5665,
                longitude: 126.978
            })
        );
    }

    #[test]
    fn accepts_a_frame_reporting_one_sensor() {
        // A partial frame is the point of per-sensor freshness: a unit that
        // only measured its battery must be able to say so.
        let bytes = frame_json(r#"{"battery":61}"#, "");

        let frame = decode(bytes.as_bytes(), now(), &Limits::default()).expect("valid frame");

        assert_eq!(frame.sensors.battery, Some(61));
        assert_eq!(frame.sensors.position, None);
        assert_eq!(frame.sensors.link, None);
    }

    #[test]
    fn refuses_a_frame_with_no_sensor_value() {
        let bytes = frame_json("{}", "");

        let rejection = decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

        assert_eq!(rejection, Rejection::NoSensorValue);
        assert_eq!(rejection.code(), "NO_SENSOR_VALUE");
    }

    #[test]
    fn refuses_an_oversized_frame_without_parsing_it() {
        let limits = Limits {
            max_bytes: 32,
            ..Limits::default()
        };
        let bytes = frame_json(r#"{"battery":82}"#, "");

        let rejection = decode(bytes.as_bytes(), now(), &limits).expect_err("refused");

        match rejection {
            Rejection::TooLarge { limit, .. } => assert_eq!(limit, 32),
            other => panic!("rejection = {other:?}, want TooLarge"),
        }
    }

    #[test]
    fn refuses_malformed_bytes() {
        let rejection = decode(b"{not json", now(), &Limits::default()).expect_err("refused");

        assert_eq!(rejection.code(), "MALFORMED_FRAME");
    }

    #[test]
    fn refuses_an_undeclared_field() {
        // A field the schema does not declare is a contract violation, not
        // something to ignore quietly.
        let bytes = frame_json(r#"{"battery":82}"#, r#","altitude":120"#);

        let rejection = decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

        assert_eq!(rejection.code(), "MALFORMED_FRAME");
    }

    #[test]
    fn refuses_values_that_cannot_be_true() {
        let cases = [
            (r#"{"battery":150}"#, BATTERY),
            (r#"{"battery":-1}"#, BATTERY),
            (r#"{"link":101}"#, LINK),
            (
                r#"{"position":{"latitude":95.0,"longitude":126.9}}"#,
                LATITUDE,
            ),
            (
                r#"{"position":{"latitude":37.5,"longitude":200.0}}"#,
                LONGITUDE,
            ),
        ];

        for (sensors, expected) in cases {
            let bytes = frame_json(sensors, "");

            let rejection =
                decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

            match rejection {
                Rejection::OutOfRange { sensor, .. } => assert_eq!(sensor, expected, "{sensors}"),
                other => panic!("rejection = {other:?}, want OutOfRange for {sensors}"),
            }
        }
    }

    #[test]
    fn refuses_a_measurement_from_the_future() {
        let bytes = r#"{"unitId":"unit-1","sessionId":"session-1","sequence":1,
            "observedAt":"2026-11-24T12:01:00Z","sensors":{"battery":82}}"#;

        let rejection = decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

        match rejection {
            Rejection::MeasuredInFuture { ahead_seconds } => assert_eq!(ahead_seconds, 60),
            other => panic!("rejection = {other:?}, want MeasuredInFuture"),
        }
    }

    #[test]
    fn tolerates_a_small_clock_difference() {
        // Clocks drift; a frame two seconds ahead is normal and must not be
        // refused.
        let bytes = r#"{"unitId":"unit-1","sessionId":"session-1","sequence":1,
            "observedAt":"2026-11-24T12:00:02Z","sensors":{"battery":82}}"#;

        decode(bytes.as_bytes(), now(), &Limits::default()).expect("valid frame");
    }

    #[test]
    fn refuses_a_sequence_below_one() {
        let bytes = r#"{"unitId":"unit-1","sessionId":"session-1","sequence":0,
            "observedAt":"2026-11-24T11:59:55Z","sensors":{"battery":82}}"#;

        let rejection = decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

        assert_eq!(rejection, Rejection::InvalidSequence(0));
    }

    #[test]
    fn refuses_empty_identifiers() {
        let bytes = r#"{"unitId":"  ","sessionId":"session-1","sequence":1,
            "observedAt":"2026-11-24T11:59:55Z","sensors":{"battery":82}}"#;

        let rejection = decode(bytes.as_bytes(), now(), &Limits::default()).expect_err("refused");

        assert_eq!(rejection.code(), "MALFORMED_FRAME");
    }

    #[test]
    fn every_rejection_explains_itself() {
        let rejections = [
            Rejection::Malformed("bad".to_owned()),
            Rejection::TooLarge {
                bytes: 10,
                limit: 5,
            },
            Rejection::NoSensorValue,
            Rejection::OutOfRange {
                sensor: BATTERY,
                value: "150".to_owned(),
            },
            Rejection::InvalidSequence(0),
            Rejection::MeasuredInFuture { ahead_seconds: 60 },
            Rejection::UnknownUnit("unit-x".to_owned()),
        ];

        for rejection in rejections {
            assert!(!rejection.code().is_empty());
            assert!(!rejection.detail().is_empty(), "{rejection:?}");
        }
    }
}
