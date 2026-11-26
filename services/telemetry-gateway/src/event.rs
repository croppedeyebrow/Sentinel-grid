//! The event the gateway publishes, which is also the receipt log entry.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::frame::{Frame, Rejection, Sensors};
use crate::sequence::Classification;

/// What happened to a frame. Only `Rejected` means it never entered the
/// platform; everything else was published and may be applied.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum Outcome {
    /// The frame the gateway expected, or the first of a session.
    Accepted,
    /// A sequence already seen in this session.
    Duplicate,
    /// A frame behind the session's progress.
    OutOfOrder,
    /// A frame past missing ones.
    Gap,
    /// A frame that may not enter the platform.
    Rejected,
}

impl From<Classification> for Outcome {
    fn from(classification: Classification) -> Self {
        match classification {
            Classification::Accepted => Self::Accepted,
            Classification::Duplicate => Self::Duplicate,
            Classification::OutOfOrder => Self::OutOfOrder,
            Classification::Gap { .. } => Self::Gap,
        }
    }
}

/// Why a frame was refused, as it appears in the receipt log.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RejectionRecord {
    /// Stable code for counting and explaining.
    pub code: String,
    /// Sentence for a human reading the log.
    pub detail: String,
}

/// One entry of the telemetry stream, matching
/// `contracts/schemas/telemetry-event.schema.json`.
///
/// Everything that arrived appears here, including what was refused: the
/// platform keeps the fact of receipt even when the value is not applied. A
/// refused frame may not have parsed, so its unit, session and sequence can be
/// absent, while anything that was applied always carries them.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct TelemetryEvent {
    /// Identifies this receipt.
    pub event_id: String,
    /// The gateway's verdict.
    pub outcome: Outcome,
    /// Unit the frame claimed to come from.
    pub unit_id: Option<String>,
    /// Session the frame belonged to.
    pub session_id: Option<String>,
    /// Position in that session's sequence.
    pub sequence: Option<i64>,
    /// When the unit measured the values.
    pub observed_at: Option<DateTime<Utc>>,
    /// When the gateway received the frame.
    pub received_at: DateTime<Utc>,
    /// The values the frame reported, absent for a refusal.
    pub sensors: Option<Sensors>,
    /// Skipped sequence numbers, set on a gap.
    pub missing_frames: Option<u64>,
    /// Why the frame was refused, set on a rejection.
    pub rejection: Option<RejectionRecord>,
    /// Size of what arrived, so an oversized frame is still accounted for.
    pub frame_bytes: usize,
}

impl TelemetryEvent {
    /// Builds the event for a frame that passed validation.
    #[must_use]
    pub fn applied(
        event_id: String,
        frame: &Frame,
        classification: Classification,
        received_at: DateTime<Utc>,
        frame_bytes: usize,
    ) -> Self {
        Self {
            event_id,
            outcome: classification.into(),
            unit_id: Some(frame.unit_id.clone()),
            session_id: Some(frame.session_id.clone()),
            sequence: Some(frame.sequence),
            observed_at: Some(frame.observed_at),
            received_at,
            sensors: Some(frame.sensors.clone()),
            missing_frames: match classification {
                Classification::Gap { missing } => Some(missing),
                _ => None,
            },
            rejection: None,
            frame_bytes,
        }
    }

    /// Builds the event for a frame that was refused. What little could be read
    /// from it is still reported, because a refusal nobody can attribute is of
    /// no use to an operator.
    #[must_use]
    pub fn refused(
        event_id: String,
        rejection: &Rejection,
        identity: Option<(&str, &str, i64)>,
        received_at: DateTime<Utc>,
        frame_bytes: usize,
    ) -> Self {
        let (unit_id, session_id, sequence) = match identity {
            Some((unit, session, sequence)) => (
                Some(unit.to_owned()),
                Some(session.to_owned()),
                Some(sequence),
            ),
            None => (None, None, None),
        };

        Self {
            event_id,
            outcome: Outcome::Rejected,
            unit_id,
            session_id,
            sequence,
            observed_at: None,
            received_at,
            sensors: None,
            missing_frames: None,
            rejection: Some(RejectionRecord {
                code: rejection.code().to_owned(),
                detail: rejection.detail(),
            }),
            frame_bytes,
        }
    }
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;

    use super::*;
    use crate::frame::Position;

    fn received() -> DateTime<Utc> {
        Utc.with_ymd_and_hms(2026, 11, 26, 9, 0, 0)
            .single()
            .expect("valid timestamp")
    }

    fn frame() -> Frame {
        Frame {
            unit_id: "unit-1".to_owned(),
            session_id: "session-1".to_owned(),
            sequence: 7,
            observed_at: received(),
            sensors: Sensors {
                position: Some(Position {
                    latitude: 37.5665,
                    longitude: 126.978,
                }),
                battery: Some(82),
                link: None,
            },
        }
    }

    #[test]
    fn an_applied_event_identifies_what_it_applies_to() {
        let event = TelemetryEvent::applied(
            "event-1".to_owned(),
            &frame(),
            Classification::Accepted,
            received(),
            180,
        );

        assert_eq!(event.outcome, Outcome::Accepted);
        assert_eq!(event.unit_id.as_deref(), Some("unit-1"));
        assert_eq!(event.sequence, Some(7));
        assert!(event.observed_at.is_some());
        assert!(event.sensors.is_some());
        assert!(event.rejection.is_none());
        assert_eq!(event.missing_frames, None);
    }

    #[test]
    fn a_gap_event_carries_how_much_is_missing() {
        let event = TelemetryEvent::applied(
            "event-1".to_owned(),
            &frame(),
            Classification::Gap { missing: 4 },
            received(),
            180,
        );

        assert_eq!(event.outcome, Outcome::Gap);
        assert_eq!(event.missing_frames, Some(4));
    }

    #[test]
    fn a_refusal_carries_its_reason_and_whatever_identity_was_readable() {
        let event = TelemetryEvent::refused(
            "event-2".to_owned(),
            &Rejection::OutOfRange {
                sensor: "battery",
                value: "150".to_owned(),
            },
            Some(("unit-1", "session-1", 8)),
            received(),
            166,
        );

        assert_eq!(event.outcome, Outcome::Rejected);
        assert_eq!(
            event
                .rejection
                .as_ref()
                .map(|rejection| rejection.code.as_str()),
            Some("VALUE_OUT_OF_RANGE")
        );
        assert_eq!(event.unit_id.as_deref(), Some("unit-1"));
        assert!(event.sensors.is_none(), "a refused value is not applied");
        assert!(event.observed_at.is_none());
    }

    #[test]
    fn an_unparsable_frame_is_still_recorded() {
        // The platform keeps the fact of receipt even when it cannot say what
        // arrived.
        let event = TelemetryEvent::refused(
            "event-3".to_owned(),
            &Rejection::Malformed("trailing characters".to_owned()),
            None,
            received(),
            42,
        );

        assert!(event.unit_id.is_none());
        assert!(event.session_id.is_none());
        assert!(event.sequence.is_none());
        assert_eq!(event.frame_bytes, 42);
        assert!(event.rejection.is_some());
    }

    #[test]
    fn outcomes_serialise_as_the_contract_spells_them() {
        let json = serde_json::to_string(&Outcome::OutOfOrder).expect("serialise");

        assert_eq!(json, "\"OUT_OF_ORDER\"");
    }
}
