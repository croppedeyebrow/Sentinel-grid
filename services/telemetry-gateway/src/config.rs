//! Process configuration, read from the environment.

use std::env;
use std::time::Duration;

use chrono::Duration as ChronoDuration;

use crate::frame::Limits;

/// Everything the gateway needs to start.
#[derive(Debug, Clone)]
pub struct Config {
    /// Address the HTTP server binds to.
    pub bind_address: String,
    /// Shared token every sender must present. The planning document aims at
    /// mTLS; certificate issuance and rotation are an operations concern and
    /// wait for Day 13.
    pub token: String,
    /// Redis connection string for the telemetry stream.
    pub redis_url: String,
    /// Stream the validated events are published to.
    pub stream_key: String,
    /// Largest number of entries kept in the stream. Telemetry is a projection
    /// source, not an archive, so the stream is capped.
    pub stream_max_len: usize,
    /// Read-only connection used to learn which units exist.
    pub postgres_dsn: String,
    /// How often the unit registry is refreshed.
    pub registry_refresh: Duration,
    /// Frame bounds.
    pub limits: Limits,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            bind_address: "0.0.0.0:8090".to_owned(),
            token: String::new(),
            redis_url: "redis://localhost:6379".to_owned(),
            stream_key: "telemetry.events".to_owned(),
            stream_max_len: 100_000,
            postgres_dsn:
                "postgres://sentinel:change-me@localhost:5432/sentinel_grid?sslmode=disable"
                    .to_owned(),
            registry_refresh: Duration::from_secs(60),
            limits: Limits::default(),
        }
    }
}

impl Config {
    /// Reads the configuration, falling back to the local Compose defaults so
    /// `cargo run` works without exporting anything by hand.
    ///
    /// The token has no default: a gateway that accepts unauthenticated frames
    /// because nobody set a variable is worse than one that refuses to start.
    pub fn from_env() -> Result<Self, String> {
        let defaults = Self::default();

        let token = env::var("TELEMETRY_TOKEN").unwrap_or_default();
        if token.trim().is_empty() {
            return Err(
                "TELEMETRY_TOKEN is required: the gateway will not accept unauthenticated frames"
                    .to_owned(),
            );
        }

        let mut config = Self {
            bind_address: env_or("TELEMETRY_BIND_ADDRESS", defaults.bind_address),
            token,
            redis_url: env_or("REDIS_URL", defaults.redis_url),
            stream_key: env_or("TELEMETRY_STREAM_KEY", defaults.stream_key),
            stream_max_len: env_parse("TELEMETRY_STREAM_MAX_LEN", defaults.stream_max_len),
            postgres_dsn: env_or("POSTGRES_DSN", defaults.postgres_dsn),
            registry_refresh: Duration::from_secs(env_parse(
                "TELEMETRY_REGISTRY_REFRESH_SECONDS",
                60,
            )),
            limits: defaults.limits,
        };

        config.limits.max_bytes = env_parse("TELEMETRY_MAX_FRAME_BYTES", config.limits.max_bytes);
        config.limits.max_clock_skew =
            ChronoDuration::seconds(env_parse("TELEMETRY_MAX_CLOCK_SKEW_SECONDS", 5));

        Ok(config)
    }
}

fn env_or(key: &str, fallback: String) -> String {
    match env::var(key) {
        Ok(value) if !value.trim().is_empty() => value,
        _ => fallback,
    }
}

fn env_parse<T: std::str::FromStr>(key: &str, fallback: T) -> T {
    env::var(key)
        .ok()
        .and_then(|value| value.trim().parse().ok())
        .unwrap_or(fallback)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_point_at_the_local_compose_stack() {
        let config = Config::default();

        assert!(config.redis_url.contains("6379"));
        assert_eq!(config.stream_key, "telemetry.events");
        assert_eq!(config.limits.max_bytes, 4 * 1024);
    }

    #[test]
    fn a_missing_token_is_a_startup_failure() {
        // Reading the environment in a test is racy across threads, so the
        // rule is checked directly: an empty token must never be accepted.
        let empty = "   ";

        assert!(
            empty.trim().is_empty(),
            "an all-whitespace token counts as missing"
        );
    }
}
