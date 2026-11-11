# ADR 0001: Initial Operational Picture architecture

## Status

Accepted

## Decision

The first vertical slice uses a React/TypeScript console, a Go snapshot API, and PostgreSQL/PostGIS. The API validates its public response shape through tests; the console validates every response with Zod before converting it into UI data.

Rust, Python, Redis, observability, and deployment services are deferred until their domain slices need them.

