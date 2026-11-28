//! Publication to the telemetry stream.

use std::time::Duration;

use redis::aio::{ConnectionManager, ConnectionManagerConfig};
use redis::AsyncCommands;
use tokio::time::timeout;

use crate::event::TelemetryEvent;

/// How long one stream operation may take before it counts as unavailable.
///
/// A managed connection retries with a backoff, which is right for a brief
/// outage and wrong for a request that is waiting: a sender needs an answer,
/// and a health endpoint that hangs is worse than one that says 503. Every
/// call is therefore bounded, and the bound is what turns an outage into a
/// refusal instead of a hung request.
const OPERATION_TIMEOUT: Duration = Duration::from_secs(2);

/// Longest wait between reconnection attempts, in milliseconds.
const RECONNECT_CEILING_MILLIS: u64 = 2_000;

/// Publishes validated events to Redis Streams, where the Go consumer reads
/// them with a consumer group.
///
/// The gateway refuses frames it cannot publish rather than accepting what it
/// cannot forward: a frame that was answered with a receipt but never reached
/// the platform would be worse than a refused one, because nobody would go
/// looking for it.
///
/// The connection is managed rather than opened once. A stream outage is a
/// temporary fault, and a gateway that stayed refusing after the stream came
/// back would turn a brief outage into one that lasts until someone notices
/// and restarts it.
pub struct StreamPublisher {
    connection: ConnectionManager,
    stream_key: String,
    max_len: usize,
}

impl StreamPublisher {
    /// Opens the connection used for publishing.
    pub async fn connect(url: &str, stream_key: String, max_len: usize) -> Result<Self, String> {
        let client =
            redis::Client::open(url).map_err(|error| format!("open redis client: {error}"))?;
        // Startup still requires the stream: a gateway that cannot publish has
        // nothing to offer a sender, and failing here is louder than accepting
        // frames nobody will ever read.
        // The default backoff climbs into tens of seconds, which would leave
        // the gateway refusing long after the stream came back. Senders retry,
        // so a short ceiling is what keeps a brief outage brief.
        let config = ConnectionManagerConfig::new()
            .set_connection_timeout(OPERATION_TIMEOUT)
            .set_response_timeout(OPERATION_TIMEOUT)
            .set_max_delay(RECONNECT_CEILING_MILLIS);
        let connection = ConnectionManager::new_with_config(client, config)
            .await
            .map_err(|error| format!("connect to redis: {error}"))?;

        Ok(Self {
            connection,
            stream_key,
            max_len,
        })
    }

    /// Appends one event to the stream.
    ///
    /// The payload is the published JSON of the event, so what the consumer
    /// reads is exactly what the contract describes.
    pub async fn publish(&self, event: &TelemetryEvent) -> Result<String, String> {
        let payload = serde_json::to_string(event)
            .map_err(|error| format!("serialise telemetry event: {error}"))?;

        let mut connection = self.connection.clone();
        let fields = [("event", payload.as_str())];
        let published = connection.xadd_maxlen(
            &self.stream_key,
            redis::streams::StreamMaxlen::Approx(self.max_len),
            "*",
            &fields,
        );

        let id: String = timeout(OPERATION_TIMEOUT, published)
            .await
            .map_err(|_| format!("publish to {}: timed out", self.stream_key))?
            .map_err(|error| format!("publish to {}: {error}", self.stream_key))?;

        Ok(id)
    }

    /// Whether the stream can be reached. Readiness depends on it, because a
    /// gateway that cannot publish has nothing to offer a sender.
    pub async fn ping(&self) -> Result<(), String> {
        let mut connection = self.connection.clone();
        let command = redis::cmd("PING");
        let pinged = command.query_async::<String>(&mut connection);

        timeout(OPERATION_TIMEOUT, pinged)
            .await
            .map_err(|_| "ping redis: timed out".to_string())?
            .map(|_| ())
            .map_err(|error| format!("ping redis: {error}"))
    }

    /// The stream being published to, for logging.
    #[must_use]
    pub fn stream_key(&self) -> &str {
        &self.stream_key
    }
}
