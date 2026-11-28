//! The paths the gateway answers on.
//!
//! These are checked because a path that stops matching is invisible from the
//! inside: the handler is still compiled, still tested, and still wrong, while
//! every caller gets an empty 404. That is what happened to the reconnect
//! handshake — the router's parameter syntax differed from the web framework's
//! and only an end-to-end call revealed it.

use axum::body::Body;
use axum::http::{Request, StatusCode};
use axum::routing::{get, post};
use axum::Router;
use sentinel_telemetry_gateway::http::{FRAMES_PATH, RESUME_PATH};
use tower::ServiceExt;

/// A router carrying the production paths with handlers that only answer, so
/// the test measures routing and nothing else.
fn routes() -> Router {
    Router::new()
        .route(FRAMES_PATH, post(|| async { "posted" }))
        .route(RESUME_PATH, get(|| async { "resumed" }))
}

async fn status_of(method: &str, uri: &str) -> StatusCode {
    let request = Request::builder()
        .method(method)
        .uri(uri)
        .body(Body::empty())
        .expect("build request");

    routes()
        .oneshot(request)
        .await
        .expect("route the request")
        .status()
}

/// The unit id as the published contract example spells it.
const CONTRACT_UNIT_ID: &str = "30000000-0000-0000-0000-000000000001";

#[tokio::test]
async fn the_frame_endpoint_matches_the_published_path() {
    assert_eq!(status_of("POST", FRAMES_PATH).await, StatusCode::OK);
}

#[tokio::test]
async fn the_resume_endpoint_matches_a_real_unit_url() {
    // The concrete URL is the point: a parameter written in another
    // framework's syntax becomes a literal segment, which matches nothing a
    // unit would ever ask for.
    let uri = format!("/api/v1/telemetry/units/{CONTRACT_UNIT_ID}/resume");

    assert_eq!(status_of("GET", &uri).await, StatusCode::OK);
}

#[tokio::test]
async fn the_resume_path_declares_exactly_one_parameter() {
    let segments: Vec<&str> = RESUME_PATH.split('/').collect();

    let parameters: Vec<&&str> = segments
        .iter()
        .filter(|segment| segment.starts_with(':'))
        .collect();
    assert_eq!(parameters.len(), 1, "resume path = {RESUME_PATH}");
    assert!(
        !RESUME_PATH.contains('{'),
        "a braced parameter is not this router's syntax: {RESUME_PATH}"
    );
}
