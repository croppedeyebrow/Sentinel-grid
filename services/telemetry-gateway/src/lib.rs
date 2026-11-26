//! Communication boundary for SentinelGrid telemetry.
//!
//! The gateway turns untrusted frames into validated events. It decides
//! whether a frame may enter the platform at all — identity, schema, size,
//! value ranges, and where the frame sits in its session's sequence — and it
//! decides nothing about missions. A frame claiming `battery=150` is refused
//! here; whether a low battery should stop a patrol is the Go mission policy's
//! business, as `종합_기획_문서/04_Rust_텔레메트리_통신.md` §3 requires.
//!
//! Nothing that arrives is thrown away. A frame that cannot be applied is
//! still published as a receipt with the reason, so the platform keeps the
//! fact of receipt.

pub mod config;
pub mod event;
pub mod frame;
pub mod http;
pub mod publisher;
pub mod registry;
pub mod sequence;

pub use frame::{decode, validate, Frame, Limits, Position, Rejection, Sensors};
pub use sequence::{Classification, SessionTracker};
