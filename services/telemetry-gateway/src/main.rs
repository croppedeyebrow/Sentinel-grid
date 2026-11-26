//! Telemetry gateway process.

use std::sync::Arc;

use sentinel_telemetry_gateway::config::Config;
use sentinel_telemetry_gateway::http::{router, AppState};
use sentinel_telemetry_gateway::publisher::StreamPublisher;
use sentinel_telemetry_gateway::registry::UnitRegistry;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt().json().init();

    if let Err(error) = run().await {
        tracing::error!(error = %error, "telemetry gateway stopped");
        std::process::exit(1);
    }
}

async fn run() -> Result<(), String> {
    let config = Config::from_env()?;

    let publisher = StreamPublisher::connect(
        &config.redis_url,
        config.stream_key.clone(),
        config.stream_max_len,
    )
    .await?;

    // The registry is loaded once before serving so a gateway that starts
    // while the database is down reports itself unready instead of refusing
    // every frame as an unknown unit.
    let registry = UnitRegistry::new();
    match registry.load(&config.postgres_dsn).await {
        Ok(count) => tracing::info!(units = count, "unit registry loaded"),
        Err(error) => tracing::warn!(error = %error, "unit registry not loaded yet"),
    }
    registry.refresh_in_background(config.postgres_dsn.clone(), config.registry_refresh);

    let address = config.bind_address.clone();
    let stream_key = config.stream_key.clone();
    let state = Arc::new(AppState::new(config, publisher, registry));

    let listener = tokio::net::TcpListener::bind(&address)
        .await
        .map_err(|error| format!("bind {address}: {error}"))?;

    tracing::info!(address = %address, stream = %stream_key, "telemetry gateway listening");

    axum::serve(listener, router(state))
        .with_graceful_shutdown(shutdown())
        .await
        .map_err(|error| format!("serve: {error}"))
}

async fn shutdown() {
    if let Err(error) = tokio::signal::ctrl_c().await {
        tracing::warn!(error = %error, "cannot listen for shutdown signal");
        return;
    }
    tracing::info!("shutdown signal received");
}
