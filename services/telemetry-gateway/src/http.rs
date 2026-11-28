//! HTTP surface of the gateway.
//!
//! Endpoints are published in `contracts/README.md`. The order of checks is
//! the point of this module: identity, then size, then schema, then values,
//! then the unit registry, and only then the sequence. A frame that fails any
//! of them is answered with the reason and recorded as received.

use std::collections::HashMap;
use std::sync::Arc;

use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use chrono::{DateTime, Utc};
use serde::Serialize;
use tokio::sync::Mutex;

use crate::config::Config;
use crate::event::{Outcome, TelemetryEvent};
use crate::frame::{validate, Frame, Rejection};
use crate::publisher::StreamPublisher;
use crate::registry::UnitRegistry;
use crate::sequence::{Classification, SessionTracker};

/// Where a unit should resume after a reconnect.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ResumePoint {
    /// Unit the answer is about.
    pub unit_id: String,
    /// Highest sequence the gateway accepted, null if it never accepted one.
    pub last_accepted_sequence: Option<i64>,
    /// Session that sequence belongs to.
    pub last_session_id: Option<String>,
    /// When the gateway last received anything from the unit.
    pub last_received_at: Option<DateTime<Utc>>,
}

/// Answer for a frame that entered the platform.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct Receipt {
    outcome: Outcome,
    unit_id: String,
    session_id: String,
    sequence: i64,
    received_at: DateTime<Utc>,
    last_accepted_sequence: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    missing_frames: Option<u64>,
}

/// Answer for a frame that may not enter the platform.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ErrorBody {
    code: String,
    error: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    details: Option<ErrorDetails>,
}

#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
struct ErrorDetails {
    #[serde(skip_serializing_if = "Option::is_none")]
    sensor: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    value: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    bytes: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    limit: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    ahead_seconds: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    sequence: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    unit_id: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct Health {
    status: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    reason: Option<String>,
}

/// Everything the handlers share.
pub struct AppState {
    config: Config,
    publisher: StreamPublisher,
    registry: UnitRegistry,
    tracker: Mutex<SessionTracker>,
    resume: Mutex<HashMap<String, ResumePoint>>,
}

impl AppState {
    /// Builds the shared state.
    #[must_use]
    pub fn new(config: Config, publisher: StreamPublisher, registry: UnitRegistry) -> Self {
        Self {
            config,
            publisher,
            registry,
            tracker: Mutex::new(SessionTracker::new()),
            resume: Mutex::new(HashMap::new()),
        }
    }
}

/// Where frames are posted.
pub const FRAMES_PATH: &str = "/api/v1/telemetry/frames";

/// Where a reconnecting unit asks where to resume.
///
/// The parameter is written in this router's own syntax, which is not the one
/// the contract uses, so the two are checked against each other by test rather
/// than by eye: a path that silently stops matching answers 404 to every
/// caller, and the frame endpoint next to it keeps working.
pub const RESUME_PATH: &str = "/api/v1/telemetry/units/:unit_id/resume";

/// Builds the router.
pub fn router(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health/live", get(live))
        .route("/health/ready", get(ready))
        .route(FRAMES_PATH, post(accept_frame))
        .route(RESUME_PATH, get(resume_point))
        .with_state(state)
}

/// Liveness deliberately ignores Redis and Postgres: a dependency outage must
/// not make the process look dead.
async fn live() -> Json<Health> {
    Json(Health {
        status: "live",
        reason: None,
    })
}

async fn ready(State(state): State<Arc<AppState>>) -> Response {
    if !state.registry.is_ready() {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(Health {
                status: "unavailable",
                reason: Some("unit registry has not loaded".to_owned()),
            }),
        )
            .into_response();
    }
    if let Err(error) = state.publisher.ping().await {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(Health {
                status: "unavailable",
                reason: Some(error),
            }),
        )
            .into_response();
    }

    Json(Health {
        status: "ready",
        reason: None,
    })
    .into_response()
}

async fn resume_point(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Path(unit_id): Path<String>,
) -> Response {
    if let Err(response) = authenticate(&state, &headers) {
        return *response;
    }

    // A device the platform does not know learns it here rather than at its
    // first frame. The frame endpoint already refuses it, and answering the
    // handshake as though the unit were merely new would send it on to be
    // refused one request later.
    if !state.registry.contains(&unit_id) {
        let rejection = Rejection::UnknownUnit(unit_id);
        let (status, details) = describe(&rejection);
        return (
            status,
            Json(ErrorBody {
                code: rejection.code().to_owned(),
                error: rejection.detail(),
                details,
            }),
        )
            .into_response();
    }

    let known = state.resume.lock().await.get(&unit_id).cloned();
    Json(known.unwrap_or(ResumePoint {
        unit_id,
        last_accepted_sequence: None,
        last_session_id: None,
        last_received_at: None,
    }))
    .into_response()
}

/// The frame pipeline.
async fn accept_frame(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    if let Err(response) = authenticate(&state, &headers) {
        return *response;
    }

    let received_at = Utc::now();
    let frame_bytes = body.len();

    // The registry is the gateway's knowledge of which devices exist. Without
    // it the gateway cannot tell a real unit from an invented one, so it
    // refuses rather than guessing.
    if !state.registry.is_ready() {
        return refuse_without_receipt(
            StatusCode::SERVICE_UNAVAILABLE,
            "STREAM_UNAVAILABLE",
            "unit registry has not loaded",
        );
    }

    // Size first: an oversized frame is refused without being parsed.
    if frame_bytes > state.config.limits.max_bytes {
        let rejection = Rejection::TooLarge {
            bytes: frame_bytes,
            limit: state.config.limits.max_bytes,
        };
        return reject(&state, &rejection, None, received_at, frame_bytes).await;
    }

    let frame: Frame = match serde_json::from_slice(&body) {
        Ok(frame) => frame,
        Err(error) => {
            let rejection = Rejection::Malformed(error.to_string());
            return reject(&state, &rejection, None, received_at, frame_bytes).await;
        }
    };

    // From here the frame has an identity, so a refusal can be attributed.
    let identity = (
        frame.unit_id.clone(),
        frame.session_id.clone(),
        frame.sequence,
    );

    if let Err(rejection) = validate(&frame, received_at, &state.config.limits) {
        return reject(&state, &rejection, Some(identity), received_at, frame_bytes).await;
    }
    if !state.registry.contains(&frame.unit_id) {
        let rejection = Rejection::UnknownUnit(frame.unit_id.clone());
        return reject(&state, &rejection, Some(identity), received_at, frame_bytes).await;
    }

    let classification = {
        let mut tracker = state.tracker.lock().await;
        tracker.classify(&frame)
    };

    let event = TelemetryEvent::applied(
        new_event_id(),
        &frame,
        classification,
        received_at,
        frame_bytes,
    );

    if let Err(error) = state.publisher.publish(&event).await {
        // Nothing is recorded as received, because nothing was forwarded. A
        // sender told its frame was accepted while it vanished would be worse
        // than one told to retry.
        tracing::error!(error = %error, "cannot publish telemetry event");
        return refuse_without_receipt(
            StatusCode::SERVICE_UNAVAILABLE,
            "STREAM_UNAVAILABLE",
            "telemetry stream unavailable",
        );
    }

    let last_accepted = note_progress(&state, &frame, classification, received_at).await;

    tracing::info!(
        unit = %frame.unit_id,
        session = %frame.session_id,
        sequence = frame.sequence,
        outcome = classification.code(),
        "frame accepted"
    );

    (
        StatusCode::ACCEPTED,
        Json(Receipt {
            outcome: classification.into(),
            unit_id: frame.unit_id,
            session_id: frame.session_id,
            sequence: frame.sequence,
            received_at,
            last_accepted_sequence: last_accepted,
            missing_frames: match classification {
                Classification::Gap { missing } => Some(missing),
                _ => None,
            },
        }),
    )
        .into_response()
}

/// Records a refused frame and answers with the reason.
async fn reject(
    state: &Arc<AppState>,
    rejection: &Rejection,
    identity: Option<(String, String, i64)>,
    received_at: DateTime<Utc>,
    frame_bytes: usize,
) -> Response {
    let borrowed = identity
        .as_ref()
        .map(|(unit, session, sequence)| (unit.as_str(), session.as_str(), *sequence));
    let event = TelemetryEvent::refused(
        new_event_id(),
        rejection,
        borrowed,
        received_at,
        frame_bytes,
    );

    // A refusal is still a receipt: the platform keeps the fact that something
    // arrived even when the value is not applied.
    if let Err(error) = state.publisher.publish(&event).await {
        tracing::error!(error = %error, "cannot publish the receipt for a refused frame");
    }
    if let Some((unit_id, _, _)) = identity.as_ref() {
        let mut resume = state.resume.lock().await;
        let entry = resume.entry(unit_id.clone()).or_insert(ResumePoint {
            unit_id: unit_id.clone(),
            last_accepted_sequence: None,
            last_session_id: None,
            last_received_at: None,
        });
        entry.last_received_at = Some(received_at);
    }

    tracing::info!(code = rejection.code(), detail = %rejection.detail(), "frame refused");

    let (status, details) = describe(rejection);
    (
        status,
        Json(ErrorBody {
            code: rejection.code().to_owned(),
            error: rejection.detail(),
            details,
        }),
    )
        .into_response()
}

/// Updates what a reconnecting unit would be told, and returns the resume
/// point for the receipt.
async fn note_progress(
    state: &Arc<AppState>,
    frame: &Frame,
    classification: Classification,
    received_at: DateTime<Utc>,
) -> Option<i64> {
    let last_accepted = {
        let tracker = state.tracker.lock().await;
        tracker.last_accepted_sequence(&frame.unit_id)
    };

    let mut resume = state.resume.lock().await;
    let entry = resume.entry(frame.unit_id.clone()).or_insert(ResumePoint {
        unit_id: frame.unit_id.clone(),
        last_accepted_sequence: None,
        last_session_id: None,
        last_received_at: None,
    });

    entry.last_received_at = Some(received_at);
    if classification != Classification::Duplicate {
        entry.last_accepted_sequence = last_accepted;
        entry.last_session_id = Some(frame.session_id.clone());
    }

    last_accepted
}

/// Checks the bearer token. The refusal is returned boxed because an axum
/// response is large and this result is on the hot path of every frame.
fn authenticate(state: &Arc<AppState>, headers: &HeaderMap) -> Result<(), Box<Response>> {
    let presented = headers
        .get("authorization")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.strip_prefix("Bearer "))
        .unwrap_or_default()
        .trim();

    if presented.is_empty() || presented != state.config.token {
        // An unauthenticated sender is not a unit, so nothing is recorded as
        // telemetry: the receipt log would otherwise be a place for anyone to
        // write.
        return Err(Box::new(refuse_without_receipt(
            StatusCode::UNAUTHORIZED,
            "UNAUTHENTICATED",
            "a valid bearer token is required",
        )));
    }
    Ok(())
}

fn refuse_without_receipt(status: StatusCode, code: &str, message: &str) -> Response {
    (
        status,
        Json(ErrorBody {
            code: code.to_owned(),
            error: message.to_owned(),
            details: None,
        }),
    )
        .into_response()
}

/// Maps a rejection to its HTTP status and the facts behind it, as published
/// in `contracts/README.md`.
fn describe(rejection: &Rejection) -> (StatusCode, Option<ErrorDetails>) {
    match rejection {
        Rejection::Malformed(_) => (StatusCode::BAD_REQUEST, None),
        Rejection::TooLarge { bytes, limit } => (
            StatusCode::PAYLOAD_TOO_LARGE,
            Some(ErrorDetails {
                bytes: Some(*bytes),
                limit: Some(*limit),
                ..ErrorDetails::default()
            }),
        ),
        Rejection::NoSensorValue => (StatusCode::UNPROCESSABLE_ENTITY, None),
        Rejection::OutOfRange { sensor, value } => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Some(ErrorDetails {
                sensor: Some((*sensor).to_owned()),
                value: Some(value.clone()),
                ..ErrorDetails::default()
            }),
        ),
        Rejection::InvalidSequence(sequence) => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Some(ErrorDetails {
                sequence: Some(*sequence),
                ..ErrorDetails::default()
            }),
        ),
        Rejection::MeasuredInFuture { ahead_seconds } => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Some(ErrorDetails {
                ahead_seconds: Some(*ahead_seconds),
                ..ErrorDetails::default()
            }),
        ),
        Rejection::UnknownUnit(unit) => (
            StatusCode::NOT_FOUND,
            Some(ErrorDetails {
                unit_id: Some(unit.clone()),
                ..ErrorDetails::default()
            }),
        ),
    }
}

fn new_event_id() -> String {
    uuid::Uuid::new_v4().to_string()
}
