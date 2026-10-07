pub mod adapter;

pub const TMUX_SESSION_PREFIX: &str = "si-muse-pane-";
pub const DEFAULT_MUSE_WORKER_SLOT: &str = "primary";

pub fn muse_worker_slot_name(slot: Option<&str>) -> String {
    let slot = slot.unwrap_or(DEFAULT_MUSE_WORKER_SLOT).trim();
    if slot.is_empty() { DEFAULT_MUSE_WORKER_SLOT.to_owned() } else { slot.to_ascii_lowercase() }
}

pub fn muse_worker_instance_name(profile_id: &str, worker_slot: &str) -> String {
    let profile = profile_id.trim();
    let slot = muse_worker_slot_name(Some(worker_slot));
    if slot == DEFAULT_MUSE_WORKER_SLOT {
        profile.to_owned()
    } else if profile.is_empty() {
        slot
    } else {
        format!("{profile}--{slot}")
    }
}

pub fn muse_tmux_session_name(profile_id: &str) -> String {
    muse_tmux_session_name_for_slot(profile_id, DEFAULT_MUSE_WORKER_SLOT)
}

pub fn muse_tmux_session_name_for_slot(profile_id: &str, worker_slot: &str) -> String {
    let profile = profile_id.trim();
    let slot = muse_worker_slot_name(Some(worker_slot));
    let suffix = if slot == DEFAULT_MUSE_WORKER_SLOT {
        profile.to_owned()
    } else if profile.is_empty() {
        slot
    } else {
        format!("{profile}-{slot}")
    };
    if suffix.is_empty() {
        TMUX_SESSION_PREFIX.to_owned()
    } else {
        format!("{TMUX_SESSION_PREFIX}{suffix}")
    }
}

#[cfg(test)]
mod tests {
    use super::{
        muse_tmux_session_name, muse_tmux_session_name_for_slot, muse_worker_instance_name,
        muse_worker_slot_name,
    };

    #[test]
    fn muse_tmux_session_name_uses_profile_slug() {
        assert_eq!(muse_tmux_session_name("profile-delta"), "si-muse-pane-profile-delta");
    }

    #[test]
    fn muse_worker_slot_name_normalizes_defaults() {
        assert_eq!(muse_worker_slot_name(None), "primary");
        assert_eq!(muse_worker_slot_name(Some("  ")), "primary");
        assert_eq!(muse_worker_slot_name(Some("Review")), "review");
    }

    #[test]
    fn muse_worker_instance_name_uses_profile_for_primary_slot() {
        assert_eq!(muse_worker_instance_name("ferma", "primary"), "ferma");
        assert_eq!(muse_worker_instance_name("ferma", "review"), "ferma--review");
    }

    #[test]
    fn muse_tmux_session_name_for_slot_is_backward_compatible_for_primary() {
        assert_eq!(muse_tmux_session_name_for_slot("ferma", "primary"), "si-muse-pane-ferma");
        assert_eq!(muse_tmux_session_name_for_slot("ferma", "review"), "si-muse-pane-ferma-review");
    }
}
