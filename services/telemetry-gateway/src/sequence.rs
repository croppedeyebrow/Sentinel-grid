//! Session and sequence classification.
//!
//! A session is one connected run of one unit; a reconnect starts a new one.
//! Within a session sequence numbers increase by one, and the gateway's job is
//! to say where an arriving frame sits: expected, already seen, behind, or
//! past a hole in the stream. Nothing is discarded on the strength of this
//! verdict — the classification travels with the frame so the projection and
//! the receipt log can both act on it.

use std::collections::{HashMap, VecDeque};

use crate::frame::Frame;

/// Where a frame sits in its session's sequence.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Classification {
    /// The frame the gateway was expecting, or the first of a session.
    Accepted,
    /// This sequence was already seen in this session.
    Duplicate,
    /// The frame is behind the session's progress and was not seen before.
    OutOfOrder,
    /// The frame is ahead of the expected sequence: frames are missing.
    Gap {
        /// How many sequence numbers were skipped.
        missing: u64,
    },
}

impl Classification {
    /// Stable code for the receipt log.
    #[must_use]
    pub fn code(&self) -> &'static str {
        match self {
            Self::Accepted => "ACCEPTED",
            Self::Duplicate => "DUPLICATE",
            Self::OutOfOrder => "OUT_OF_ORDER",
            Self::Gap { .. } => "GAP",
        }
    }

    /// Whether the projection may apply the frame's values.
    ///
    /// A duplicate carries nothing new. Everything else may be applied, though
    /// an out-of-order frame is applied per sensor and only where its
    /// measurement is newer than the stored one — that rule belongs to the
    /// projection, not to the gateway.
    #[must_use]
    pub fn is_applicable(&self) -> bool {
        !matches!(self, Self::Duplicate)
    }
}

/// How many recent sequence numbers a session remembers for duplicate
/// detection. A duplicate older than this window is reported as out of order,
/// which is the safe direction: the projection still refuses to move a sensor
/// backwards in time.
const DEFAULT_WINDOW: usize = 1024;

#[derive(Debug)]
struct SessionState {
    highest: i64,
    recent: VecDeque<i64>,
}

impl SessionState {
    fn new(sequence: i64) -> Self {
        let mut recent = VecDeque::with_capacity(DEFAULT_WINDOW);
        recent.push_back(sequence);
        Self {
            highest: sequence,
            recent,
        }
    }

    fn remember(&mut self, sequence: i64, window: usize) {
        self.recent.push_back(sequence);
        while self.recent.len() > window {
            self.recent.pop_front();
        }
        if sequence > self.highest {
            self.highest = sequence;
        }
    }

    fn has_seen(&self, sequence: i64) -> bool {
        self.recent.contains(&sequence)
    }
}

/// Tracks sessions so arriving frames can be classified, and remembers the
/// last sequence accepted per unit so a reconnecting unit can be told where to
/// resume.
#[derive(Debug)]
pub struct SessionTracker {
    sessions: HashMap<(String, String), SessionState>,
    last_accepted: HashMap<String, i64>,
    window: usize,
}

impl Default for SessionTracker {
    fn default() -> Self {
        Self::new()
    }
}

impl SessionTracker {
    /// A tracker with the default duplicate-detection window.
    #[must_use]
    pub fn new() -> Self {
        Self::with_window(DEFAULT_WINDOW)
    }

    /// A tracker that remembers `window` recent sequence numbers per session.
    #[must_use]
    pub fn with_window(window: usize) -> Self {
        Self {
            sessions: HashMap::new(),
            last_accepted: HashMap::new(),
            window: window.max(1),
        }
    }

    /// Classifies a frame and records it.
    pub fn classify(&mut self, frame: &Frame) -> Classification {
        let key = (frame.unit_id.clone(), frame.session_id.clone());

        let Some(state) = self.sessions.get_mut(&key) else {
            // First frame of a session. A reconnect may resume at any
            // sequence, so this is not treated as a gap.
            self.sessions.insert(key, SessionState::new(frame.sequence));
            self.note_accepted(&frame.unit_id, frame.sequence);
            return Classification::Accepted;
        };

        if state.has_seen(frame.sequence) {
            return Classification::Duplicate;
        }

        let expected = state.highest + 1;
        let classification = if frame.sequence == expected {
            Classification::Accepted
        } else if frame.sequence < expected {
            Classification::OutOfOrder
        } else {
            Classification::Gap {
                missing: u64::try_from(frame.sequence - expected).unwrap_or(0),
            }
        };

        state.remember(frame.sequence, self.window);
        if !matches!(classification, Classification::OutOfOrder) {
            self.note_accepted(&frame.unit_id, frame.sequence);
        }

        classification
    }

    fn note_accepted(&mut self, unit_id: &str, sequence: i64) {
        let entry = self
            .last_accepted
            .entry(unit_id.to_owned())
            .or_insert(sequence);
        if sequence > *entry {
            *entry = sequence;
        }
    }

    /// The highest sequence accepted for a unit, which is what the reconnect
    /// handshake answers with so a unit holding buffered frames knows where to
    /// resume.
    #[must_use]
    pub fn last_accepted_sequence(&self, unit_id: &str) -> Option<i64> {
        self.last_accepted.get(unit_id).copied()
    }

    /// Number of sessions currently tracked, for the gateway's own metrics.
    #[must_use]
    pub fn session_count(&self) -> usize {
        self.sessions.len()
    }
}

#[cfg(test)]
mod tests {
    use chrono::{DateTime, TimeZone, Utc};

    use super::*;
    use crate::frame::Sensors;

    fn frame(session: &str, sequence: i64) -> Frame {
        Frame {
            unit_id: "unit-1".to_owned(),
            session_id: session.to_owned(),
            sequence,
            observed_at: observed(sequence),
            sensors: Sensors {
                battery: Some(80),
                ..Sensors::default()
            },
        }
    }

    fn observed(sequence: i64) -> DateTime<Utc> {
        Utc.timestamp_opt(1_795_000_000 + sequence, 0)
            .single()
            .expect("valid timestamp")
    }

    #[test]
    fn the_first_frame_of_a_session_is_accepted() {
        let mut tracker = SessionTracker::new();

        // A reconnect may resume at any sequence, so a session that opens at
        // 7 is not a gap — there is nothing yet to be missing from.
        assert_eq!(
            tracker.classify(&frame("session-1", 7)),
            Classification::Accepted
        );
        assert_eq!(tracker.last_accepted_sequence("unit-1"), Some(7));
    }

    #[test]
    fn classifies_a_stream_in_order() {
        let mut tracker = SessionTracker::new();

        for sequence in 1..=5 {
            assert_eq!(
                tracker.classify(&frame("session-1", sequence)),
                Classification::Accepted,
                "sequence {sequence}"
            );
        }
        assert_eq!(tracker.last_accepted_sequence("unit-1"), Some(5));
    }

    #[test]
    fn classifies_every_deviation() {
        let mut tracker = SessionTracker::new();
        for sequence in 1..=3 {
            tracker.classify(&frame("session-1", sequence));
        }

        let cases = [
            (3, Classification::Duplicate, "a sequence already seen"),
            (2, Classification::Duplicate, "an earlier sequence seen"),
            (4, Classification::Accepted, "the expected next frame"),
            (7, Classification::Gap { missing: 2 }, "two frames missing"),
            (6, Classification::OutOfOrder, "behind after the gap"),
            (5, Classification::OutOfOrder, "still behind"),
            (8, Classification::Accepted, "back in step"),
        ];

        for (sequence, want, why) in cases {
            assert_eq!(
                tracker.classify(&frame("session-1", sequence)),
                want,
                "sequence {sequence}: {why}"
            );
        }
    }

    #[test]
    fn a_gap_reports_how_much_is_missing() {
        let mut tracker = SessionTracker::new();
        tracker.classify(&frame("session-1", 1));

        match tracker.classify(&frame("session-1", 10)) {
            Classification::Gap { missing } => assert_eq!(missing, 8),
            other => panic!("classification = {other:?}, want Gap"),
        }
    }

    #[test]
    fn out_of_order_does_not_move_the_resume_point() {
        let mut tracker = SessionTracker::new();
        tracker.classify(&frame("session-1", 1));
        tracker.classify(&frame("session-1", 5));

        tracker.classify(&frame("session-1", 3));

        // The handshake must resume from the furthest point reached, not from
        // a late frame.
        assert_eq!(tracker.last_accepted_sequence("unit-1"), Some(5));
    }

    #[test]
    fn a_reconnect_starts_a_new_session() {
        let mut tracker = SessionTracker::new();
        for sequence in 1..=3 {
            tracker.classify(&frame("session-1", sequence));
        }

        // The unit comes back with a new session and restarts at 1. That is
        // not a duplicate: the old session's numbering is irrelevant.
        assert_eq!(
            tracker.classify(&frame("session-2", 1)),
            Classification::Accepted
        );
        assert_eq!(
            tracker.classify(&frame("session-2", 2)),
            Classification::Accepted
        );
        assert_eq!(tracker.session_count(), 2);
    }

    #[test]
    fn sessions_of_different_units_do_not_interfere() {
        let mut tracker = SessionTracker::new();
        tracker.classify(&frame("session-1", 1));

        let mut other = frame("session-1", 1);
        other.unit_id = "unit-2".to_owned();

        assert_eq!(tracker.classify(&other), Classification::Accepted);
        assert_eq!(tracker.last_accepted_sequence("unit-2"), Some(1));
    }

    #[test]
    fn a_duplicate_older_than_the_window_is_reported_as_out_of_order() {
        let mut tracker = SessionTracker::with_window(2);
        for sequence in 1..=5 {
            tracker.classify(&frame("session-1", sequence));
        }

        // Sequence 1 has fallen out of the remembered window. Calling it out
        // of order is the safe direction: the projection still refuses to move
        // a sensor backwards in time.
        assert_eq!(
            tracker.classify(&frame("session-1", 1)),
            Classification::OutOfOrder
        );
    }

    #[test]
    fn only_a_duplicate_carries_nothing_to_apply() {
        assert!(Classification::Accepted.is_applicable());
        assert!(Classification::Gap { missing: 3 }.is_applicable());
        assert!(Classification::OutOfOrder.is_applicable());
        assert!(!Classification::Duplicate.is_applicable());
    }

    #[test]
    fn every_classification_has_a_code() {
        for classification in [
            Classification::Accepted,
            Classification::Duplicate,
            Classification::OutOfOrder,
            Classification::Gap { missing: 1 },
        ] {
            assert!(!classification.code().is_empty());
        }
    }
}
