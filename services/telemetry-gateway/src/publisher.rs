//! Publication to the telemetry stream.

use redis::aio::MultiplexedConnection;
use redis::AsyncCommands;
use tokio::sync::Mutex;

use crate::event::TelemetryEvent;

/// Publishes validated events to Redis Streams, where the Go consumer reads
/// them with a consumer group.
///
/// The gateway refuses frames it cannot publish rather than accepting what it
/// cannot forward: a frame that was answered with a receipt but never reached
/// the platform would be worse than a refused one, because nobody would go
/// looking for it.
pub struct StreamPublisher {
    connection: Mutex<MultiplexedConnection>,
    stream_key: String,
    max_len: usize,
}

impl StreamPublisher {
    /// Opens the connection used for publishing.
    pub async fn connect(url: &str, stream_key: String, max_len: usize) -> Result<Self, String> {
        let client =
            redis::Client::open(url).map_err(|error| format!("open redis client: {error}"))?;
        let connection = client
            .get_multiplexed_async_connection()
            .await
            .map_err(|error| format!("connect to redis: {error}"))?;

        Ok(Self {
            connection: Mutex::new(connection),
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

        let mut connection = self.connection.lock().await;
        let id: String = connection
            .xadd_maxlen(
                &self.stream_key,
                redis::streams::StreamMaxlen::Approx(self.max_len),
                "*",
                &[("event", payload.as_str())],
            )
            .await
            .map_err(|error| format!("publish to {}: {error}", self.stream_key))?;

        Ok(id)
    }

    /// Whether the stream can be reached. Readiness depends on it, because a
    /// gateway that cannot publish has nothing to offer a sender.
    pub async fn ping(&self) -> Result<(), String> {
        let mut connection = self.connection.lock().await;
        redis::cmd("PING")
            .query_async::<String>(&mut *connection)
            .await
            .map(|_| ())
            .map_err(|error| format!("ping redis: {error}"))
    }

    /// The stream being published to, for logging.
    #[must_use]
    pub fn stream_key(&self) -> &str {
        &self.stream_key
    }
}
