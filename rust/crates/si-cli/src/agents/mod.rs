//! Agent account operations: profile commands, runner, tmux lifecycle.
//!
//! Extracted from the CLI root dispatch so Codex and Muse share one profile
//! and lifecycle core. The native CLI owns login, credentials, renewal, model
//! requests, and conversation persistence; these modules own profile metadata,
//! process environment, tmux ownership, and lifecycle records.

pub mod codex;
pub mod store;
