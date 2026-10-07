//! Provider, profile, and worker-slot identifiers.
//!
//! An SI profile means one account of one provider; a worker slot means one
//! running agent associated with that profile. New IDs use a bounded,
//! validated ASCII slug so they are safe as directory names and tmux labels.
//! Legacy display names stay free-form labels and are never parsed as IDs.

use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::fmt;
use thiserror::Error;

/// Provider whose native CLI owns login, credentials, and conversation state.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Provider {
    Codex,
    Muse,
}

impl Provider {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Codex => "codex",
            Self::Muse => "muse",
        }
    }

    pub fn all() -> [Self; 2] {
        [Self::Codex, Self::Muse]
    }

    pub fn parse(raw: &str) -> Result<Self, ProviderError> {
        match raw.trim() {
            "codex" => Ok(Self::Codex),
            "muse" => Ok(Self::Muse),
            other => Err(ProviderError::Unknown { value: other.to_owned() }),
        }
    }
}

impl fmt::Display for Provider {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ProviderError {
    #[error("unknown provider {value:?}; expected \"codex\" or \"muse\"")]
    Unknown { value: String },
}

/// Default worker slot, matching the long-standing Codex behavior.
pub const DEFAULT_WORKER_SLOT: &str = "primary";

/// Maximum validated ID length in bytes.
pub const MAX_ID_LEN: usize = 64;

#[derive(Debug, Error, PartialEq, Eq)]
pub enum IdError {
    #[error("{kind} id is required")]
    Empty { kind: &'static str },
    #[error("{kind} id is {len} bytes; maximum is {MAX_ID_LEN}")]
    TooLong { kind: &'static str, len: usize },
    #[error("{kind} id {value:?} must start with an ASCII letter or digit")]
    InvalidStart { kind: &'static str, value: String },
    #[error("{kind} id {value:?} must contain only ASCII letters, digits, '-' or '_'")]
    InvalidChar { kind: &'static str, value: String },
}

fn validate_slug(kind: &'static str, raw: &str) -> Result<String, IdError> {
    let id = raw.trim();
    if id.is_empty() {
        return Err(IdError::Empty { kind });
    }
    if id.len() > MAX_ID_LEN {
        return Err(IdError::TooLong { kind, len: id.len() });
    }
    if !id.as_bytes()[0].is_ascii_alphanumeric() {
        return Err(IdError::InvalidStart { kind, value: id.to_owned() });
    }
    if !id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_') {
        return Err(IdError::InvalidChar { kind, value: id.to_owned() });
    }
    Ok(id.to_owned())
}

macro_rules! slug_id {
    ($name:ident, $kind:expr) => {
        #[derive(Clone, Debug, Eq, PartialEq, Hash)]
        pub struct $name(String);

        impl $name {
            pub fn parse(raw: &str) -> Result<Self, IdError> {
                validate_slug($kind, raw).map(Self)
            }

            pub fn as_str(&self) -> &str {
                &self.0
            }
        }

        impl fmt::Display for $name {
            fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str(&self.0)
            }
        }

        impl AsRef<str> for $name {
            fn as_ref(&self) -> &str {
                &self.0
            }
        }

        impl Serialize for $name {
            fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
                serializer.serialize_str(&self.0)
            }
        }

        impl<'de> Deserialize<'de> for $name {
            fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
                let raw = String::deserialize(deserializer)?;
                Self::parse(&raw).map_err(serde::de::Error::custom)
            }
        }
    };
}

slug_id!(ProfileId, "profile");
slug_id!(SlotId, "slot");

impl SlotId {
    /// Resolve an optional slot argument, defaulting to `primary`.
    pub fn or_primary(raw: Option<&str>) -> Result<Self, IdError> {
        match raw.map(str::trim) {
            None => Self::parse(DEFAULT_WORKER_SLOT),
            Some("") => Self::parse(DEFAULT_WORKER_SLOT),
            Some(slot) => Self::parse(slot),
        }
    }

    pub fn is_primary(&self) -> bool {
        self.0 == DEFAULT_WORKER_SLOT
    }
}

impl Default for SlotId {
    fn default() -> Self {
        Self(DEFAULT_WORKER_SLOT.to_owned())
    }
}

#[cfg(test)]
mod tests {
    use super::{DEFAULT_WORKER_SLOT, IdError, ProfileId, Provider, SlotId};

    #[test]
    fn parses_known_providers() {
        assert_eq!(Provider::parse("codex"), Ok(Provider::Codex));
        assert_eq!(Provider::parse("muse"), Ok(Provider::Muse));
        assert_eq!(Provider::parse("  muse  "), Ok(Provider::Muse));
        assert!(Provider::parse("Muse").is_err());
        assert!(Provider::parse("fort").is_err());
    }

    #[test]
    fn accepts_bounded_ascii_slugs() {
        assert_eq!(ProfileId::parse("personal").unwrap().as_str(), "personal");
        assert_eq!(ProfileId::parse("work-2_x").unwrap().as_str(), "work-2_x");
        assert_eq!(ProfileId::parse("  padded  ").unwrap().as_str(), "padded");
        let max = "a".repeat(64);
        assert_eq!(ProfileId::parse(&max).unwrap().as_str(), max);
    }

    #[test]
    fn rejects_bad_ids_without_silent_repair() {
        assert_eq!(ProfileId::parse(""), Err(IdError::Empty { kind: "profile" }));
        assert_eq!(ProfileId::parse("   "), Err(IdError::Empty { kind: "profile" }));
        assert!(matches!(ProfileId::parse(&"a".repeat(65)), Err(IdError::TooLong { len: 65, .. })));
        assert!(matches!(ProfileId::parse("-lead"), Err(IdError::InvalidStart { .. })));
        assert!(matches!(ProfileId::parse("has space"), Err(IdError::InvalidChar { .. })));
        assert!(matches!(ProfileId::parse("a/b"), Err(IdError::InvalidChar { .. })));
        assert!(matches!(ProfileId::parse(".."), Err(IdError::InvalidStart { .. })));
        assert!(matches!(ProfileId::parse("uniçode"), Err(IdError::InvalidChar { .. })));
    }

    #[test]
    fn slot_defaults_to_primary() {
        assert_eq!(SlotId::or_primary(None).unwrap().as_str(), DEFAULT_WORKER_SLOT);
        assert_eq!(SlotId::or_primary(Some("")).unwrap().as_str(), DEFAULT_WORKER_SLOT);
        assert_eq!(SlotId::or_primary(Some("  ")).unwrap().as_str(), DEFAULT_WORKER_SLOT);
        assert_eq!(SlotId::or_primary(Some("gpu-1")).unwrap().as_str(), "gpu-1");
        assert!(SlotId::default().is_primary());
        assert!(!SlotId::or_primary(Some("other")).unwrap().is_primary());
    }
}
