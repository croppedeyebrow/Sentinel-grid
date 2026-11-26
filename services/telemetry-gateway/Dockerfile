# Build the gateway with the toolchain pinned by the image tag, then ship only
# the binary: the runtime image carries no compiler and no source.
FROM rust:1.92-slim AS build
WORKDIR /src

# Dependencies are fetched against the manifests alone, so a source change does
# not refetch the registry.
COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo "fn main() {}" > src/main.rs && echo "" > src/lib.rs \
    && cargo build --release --locked \
    && rm -rf src

COPY src ./src
RUN touch src/main.rs src/lib.rs && cargo build --release --locked

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# The gateway accepts untrusted input, so it runs as a non-root user with no
# write access to anything it does not need.
RUN useradd --system --no-create-home --shell /usr/sbin/nologin gateway
COPY --from=build /src/target/release/sentinel-telemetry-gateway /usr/local/bin/telemetry-gateway

USER gateway
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/telemetry-gateway"]
