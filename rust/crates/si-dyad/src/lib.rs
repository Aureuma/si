use si_rs_docker::BindMount;
use si_rs_runtime::{ContainerCoreMountPlan, HostMountContext, build_container_core_mounts};
use std::path::PathBuf;
use thiserror::Error;

pub const DEFAULT_ACTOR_IMAGE: &str = "aureuma/si:local";
pub const DEFAULT_CRITIC_IMAGE: &str = "aureuma/si:local";
pub const DEFAULT_NETWORK: &str = "si";
pub const DEFAULT_CONTAINER_HOME: &str = "/home/si";
pub const DEFAULT_WORKSPACE_TARGET: &str = "/workspace";
pub const DEFAULT_CONFIGS_TARGET: &str = "/configs";
pub const DEFAULT_SKILLS_VOLUME: &str = "si-codex-skills";
pub const DEFAULT_FORWARD_PORTS: &str = "1455-1465";

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct SpawnRequest {
    pub name: String,
    pub role: Option<String>,
    pub actor_image: Option<String>,
    pub critic_image: Option<String>,
    pub codex_model: Option<String>,
    pub codex_effort_actor: Option<String>,
    pub codex_effort_critic: Option<String>,
    pub codex_model_low: Option<String>,
    pub codex_model_medium: Option<String>,
    pub codex_model_high: Option<String>,
    pub codex_effort_low: Option<String>,
    pub codex_effort_medium: Option<String>,
    pub codex_effort_high: Option<String>,
    pub workspace_host: PathBuf,
    pub configs_host: Option<PathBuf>,
    pub vault_env_file: Option<PathBuf>,
    pub codex_volume: Option<String>,
    pub skills_volume: Option<String>,
    pub network_name: Option<String>,
    pub forward_ports: Option<String>,
    pub docker_socket: bool,
    pub profile_id: Option<String>,
    pub profile_name: Option<String>,
    pub loop_enabled: Option<bool>,
    pub loop_goal: Option<String>,
    pub loop_seed_prompt: Option<String>,
    pub loop_max_turns: Option<i32>,
    pub loop_sleep_seconds: Option<i32>,
    pub loop_startup_delay_seconds: Option<i32>,
    pub loop_turn_timeout_seconds: Option<i32>,
    pub loop_retry_max: Option<i32>,
    pub loop_retry_base_seconds: Option<i32>,
    pub loop_prompt_lines: Option<i32>,
    pub loop_allow_mcp_startup: Option<bool>,
    pub loop_tmux_capture: Option<String>,
    pub loop_pause_poll_seconds: Option<i32>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SpawnPlan {
    pub dyad: String,
    pub role: String,
    pub network_name: String,
    pub workspace_host: PathBuf,
    pub configs_host: PathBuf,
    pub codex_volume: String,
    pub skills_volume: String,
    pub forward_ports: String,
    pub docker_socket: bool,
    pub actor: MemberPlan,
    pub critic: MemberPlan,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MemberPlan {
    pub member: String,
    pub container_name: String,
    pub image: String,
    pub workdir: Option<PathBuf>,
    pub env: Vec<String>,
    pub bind_mounts: Vec<PlanBindMount>,
    pub volume_mounts: Vec<PlanVolumeMount>,
    pub labels: Vec<(String, String)>,
    pub command: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlanBindMount {
    pub source: PathBuf,
    pub target: PathBuf,
    pub read_only: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlanVolumeMount {
    pub source: String,
    pub target: PathBuf,
    pub read_only: bool,
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum SpawnPlanError {
    #[error("dyad name required")]
    MissingName,
    #[error("workspace host must be an absolute directory")]
    InvalidWorkspace,
    #[error("configs host must be an absolute path")]
    InvalidConfigs,
}

pub fn build_spawn_plan(
    request: &SpawnRequest,
    host_ctx: &HostMountContext,
) -> Result<SpawnPlan, SpawnPlanError> {
    let dyad = request.name.trim();
    if dyad.is_empty() {
        return Err(SpawnPlanError::MissingName);
    }
    if !request.workspace_host.is_absolute() || !request.workspace_host.is_dir() {
        return Err(SpawnPlanError::InvalidWorkspace);
    }
    let configs_host =
        request.configs_host.clone().unwrap_or_else(|| request.workspace_host.join("configs"));
    if !configs_host.is_absolute() {
        return Err(SpawnPlanError::InvalidConfigs);
    }

    let role = non_empty_or_default(request.role.as_deref(), "generic");
    let codex_volume =
        non_empty_or_default(request.codex_volume.as_deref(), &format!("si-codex-{dyad}"));
    let skills_volume =
        non_empty_or_default(request.skills_volume.as_deref(), DEFAULT_SKILLS_VOLUME);
    let network_name = non_empty_or_default(request.network_name.as_deref(), DEFAULT_NETWORK);
    let forward_ports =
        non_empty_or_default(request.forward_ports.as_deref(), DEFAULT_FORWARD_PORTS);
    let container_home = PathBuf::from(DEFAULT_CONTAINER_HOME);

    let actor_core_mounts = build_container_core_mounts(
        &ContainerCoreMountPlan {
            workspace_host: request.workspace_host.clone(),
            workspace_primary_target: PathBuf::from(DEFAULT_WORKSPACE_TARGET),
            workspace_mirror_target: Some(request.workspace_host.clone()),
            container_home: container_home.clone(),
            include_host_si: true,
            host_vault_env_file: request.vault_env_file.clone(),
        },
        host_ctx,
    );
    let critic_core_mounts = build_container_core_mounts(
        &ContainerCoreMountPlan {
            workspace_host: request.workspace_host.clone(),
            workspace_primary_target: PathBuf::from(DEFAULT_WORKSPACE_TARGET),
            workspace_mirror_target: Some(request.workspace_host.clone()),
            container_home: container_home.clone(),
            include_host_si: true,
            host_vault_env_file: request.vault_env_file.clone(),
        },
        host_ctx,
    );

    let actor = MemberPlan {
        member: "actor".to_owned(),
        container_name: dyad_container_name(dyad, "actor"),
        image: non_empty_or_default(request.actor_image.as_deref(), DEFAULT_ACTOR_IMAGE),
        workdir: Some(PathBuf::from(DEFAULT_WORKSPACE_TARGET)),
        env: build_member_env(request, dyad, &role, "actor"),
        bind_mounts: actor_core_mounts.into_iter().map(plan_bind_mount).collect(),
        volume_mounts: vec![
            PlanVolumeMount {
                source: codex_volume.clone(),
                target: container_home.join(".codex"),
                read_only: false,
            },
            PlanVolumeMount {
                source: skills_volume.clone(),
                target: container_home.join(".codex").join("skills"),
                read_only: false,
            },
        ],
        labels: base_labels(dyad, &role, "actor"),
        command: vec![
            "/usr/local/bin/si-codex-init".to_owned(),
            "--exec".to_owned(),
            "tail".to_owned(),
            "-f".to_owned(),
            "/dev/null".to_owned(),
        ],
    };

    let mut critic_bind_mounts: Vec<PlanBindMount> =
        critic_core_mounts.into_iter().map(plan_bind_mount).collect();
    critic_bind_mounts.push(PlanBindMount {
        source: configs_host.clone(),
        target: PathBuf::from(DEFAULT_CONFIGS_TARGET),
        read_only: true,
    });
    let critic = MemberPlan {
        member: "critic".to_owned(),
        container_name: dyad_container_name(dyad, "critic"),
        image: non_empty_or_default(request.critic_image.as_deref(), DEFAULT_CRITIC_IMAGE),
        workdir: None,
        env: build_member_env(request, dyad, &role, "critic"),
        bind_mounts: critic_bind_mounts,
        volume_mounts: vec![
            PlanVolumeMount {
                source: codex_volume.clone(),
                target: container_home.join(".codex"),
                read_only: false,
            },
            PlanVolumeMount {
                source: skills_volume.clone(),
                target: container_home.join(".codex").join("skills"),
                read_only: false,
            },
        ],
        labels: base_labels(dyad, &role, "critic"),
        command: vec!["critic".to_owned()],
    };

    Ok(SpawnPlan {
        dyad: dyad.to_owned(),
        role,
        network_name,
        workspace_host: request.workspace_host.clone(),
        configs_host,
        codex_volume,
        skills_volume,
        forward_ports,
        docker_socket: request.docker_socket,
        actor,
        critic,
    })
}

pub fn dyad_container_name(dyad: &str, member: &str) -> String {
    let dyad = dyad.trim();
    let member = member.trim();
    if dyad.is_empty() || member.is_empty() {
        return String::new();
    }
    format!("si-{member}-{dyad}")
}

fn build_member_env(request: &SpawnRequest, dyad: &str, role: &str, member: &str) -> Vec<String> {
    let mut env = vec![
        format!("ROLE={role}"),
        format!("DYAD_NAME={dyad}"),
        format!("DYAD_MEMBER={member}"),
        "CODEX_INIT_FORCE=1".to_owned(),
        format!("HOME={DEFAULT_CONTAINER_HOME}"),
        format!("CODEX_HOME={DEFAULT_CONTAINER_HOME}/.codex"),
    ];
    if member == "actor" {
        env.push("SI_TERM_TITLE=🪢 ".to_owned() + dyad + " 🛩️ actor");
    } else if member == "critic" {
        env.push("SI_TERM_TITLE=🪢 ".to_owned() + dyad + " 🧠 critic");
        env.push(format!("ACTOR_CONTAINER={}", dyad_container_name(dyad, "actor")));
    }
    append_optional_env(&mut env, "CODEX_MODEL", request.codex_model.as_deref());
    append_optional_env(
        &mut env,
        "CODEX_REASONING_EFFORT",
        if member == "actor" {
            request.codex_effort_actor.as_deref()
        } else {
            request.codex_effort_critic.as_deref()
        },
    );
    append_optional_env(&mut env, "SI_CODEX_PROFILE_ID", request.profile_id.as_deref());
    append_optional_env(&mut env, "SI_CODEX_PROFILE_NAME", request.profile_name.as_deref());
    append_optional_env(&mut env, "CODEX_MODEL_LOW", request.codex_model_low.as_deref());
    append_optional_env(&mut env, "CODEX_MODEL_MEDIUM", request.codex_model_medium.as_deref());
    append_optional_env(&mut env, "CODEX_MODEL_HIGH", request.codex_model_high.as_deref());
    append_optional_env(
        &mut env,
        "CODEX_REASONING_EFFORT_LOW",
        request.codex_effort_low.as_deref(),
    );
    append_optional_env(
        &mut env,
        "CODEX_REASONING_EFFORT_MEDIUM",
        request.codex_effort_medium.as_deref(),
    );
    append_optional_env(
        &mut env,
        "CODEX_REASONING_EFFORT_HIGH",
        request.codex_effort_high.as_deref(),
    );
    if member == "critic" {
        append_optional_bool_env(&mut env, "DYAD_LOOP_ENABLED", request.loop_enabled);
        append_optional_env(&mut env, "DYAD_LOOP_GOAL", request.loop_goal.as_deref());
        append_optional_env(
            &mut env,
            "DYAD_LOOP_SEED_CRITIC_PROMPT",
            request.loop_seed_prompt.as_deref(),
        );
        append_optional_int_env(&mut env, "DYAD_LOOP_MAX_TURNS", request.loop_max_turns);
        append_optional_int_env(&mut env, "DYAD_LOOP_SLEEP_SECONDS", request.loop_sleep_seconds);
        append_optional_int_env(
            &mut env,
            "DYAD_LOOP_STARTUP_DELAY_SECONDS",
            request.loop_startup_delay_seconds,
        );
        append_optional_int_env(
            &mut env,
            "DYAD_LOOP_TURN_TIMEOUT_SECONDS",
            request.loop_turn_timeout_seconds,
        );
        append_optional_int_env(&mut env, "DYAD_LOOP_RETRY_MAX", request.loop_retry_max);
        append_optional_int_env(
            &mut env,
            "DYAD_LOOP_RETRY_BASE_SECONDS",
            request.loop_retry_base_seconds,
        );
        append_optional_int_env(&mut env, "DYAD_LOOP_PROMPT_LINES", request.loop_prompt_lines);
        append_optional_bool_env(
            &mut env,
            "DYAD_LOOP_ALLOW_MCP_STARTUP",
            request.loop_allow_mcp_startup,
        );
        append_optional_env(
            &mut env,
            "DYAD_LOOP_TMUX_CAPTURE",
            request.loop_tmux_capture.as_deref(),
        );
        append_optional_int_env(
            &mut env,
            "DYAD_LOOP_PAUSE_POLL_SECONDS",
            request.loop_pause_poll_seconds,
        );
    }
    env
}

fn base_labels(dyad: &str, role: &str, member: &str) -> Vec<(String, String)> {
    vec![
        ("app".to_owned(), "si-dyad".to_owned()),
        ("si.dyad".to_owned(), dyad.to_owned()),
        ("si.role".to_owned(), role.to_owned()),
        ("si.member".to_owned(), member.to_owned()),
    ]
}

fn plan_bind_mount(mount: BindMount) -> PlanBindMount {
    PlanBindMount {
        source: mount.source().to_path_buf(),
        target: mount.target().to_path_buf(),
        read_only: mount.is_read_only(),
    }
}

fn non_empty_or_default(raw: Option<&str>, default: &str) -> String {
    raw.map(str::trim).filter(|value| !value.is_empty()).unwrap_or(default).to_owned()
}

fn append_optional_env(env: &mut Vec<String>, key: &str, value: Option<&str>) {
    if let Some(value) = value.map(str::trim).filter(|value| !value.is_empty()) {
        env.push(format!("{key}={value}"));
    }
}

fn append_optional_int_env(env: &mut Vec<String>, key: &str, value: Option<i32>) {
    if let Some(value) = value {
        env.push(format!("{key}={value}"));
    }
}

fn append_optional_bool_env(env: &mut Vec<String>, key: &str, value: Option<bool>) {
    if let Some(value) = value {
        env.push(format!("{key}={value}"));
    }
}

#[cfg(test)]
mod tests {
    use super::{
        DEFAULT_FORWARD_PORTS, DEFAULT_SKILLS_VOLUME, SpawnRequest, build_spawn_plan,
        dyad_container_name,
    };
    use si_rs_runtime::HostMountContext;
    use std::path::Path;
    use tempfile::tempdir;

    #[test]
    fn dyad_container_name_builds_expected_members() {
        assert_eq!(dyad_container_name("alpha", "actor"), "si-actor-alpha");
        assert_eq!(dyad_container_name("alpha", "critic"), "si-critic-alpha");
    }

    #[test]
    fn build_spawn_plan_defaults_configs_and_volumes() {
        let workspace = tempdir().expect("tempdir");
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };

        let plan = build_spawn_plan(
            &SpawnRequest {
                name: "alpha".to_owned(),
                workspace_host: workspace.path().to_path_buf(),
                docker_socket: true,
                ..SpawnRequest::default()
            },
            &ctx,
        )
        .expect("build dyad spawn plan");

        assert_eq!(plan.role, "generic");
        assert_eq!(plan.codex_volume, "si-codex-alpha");
        assert_eq!(plan.skills_volume, DEFAULT_SKILLS_VOLUME);
        assert_eq!(plan.forward_ports, DEFAULT_FORWARD_PORTS);
        assert_eq!(plan.actor.container_name, "si-actor-alpha");
        assert_eq!(plan.critic.container_name, "si-critic-alpha");
        assert_eq!(plan.configs_host, workspace.path().join("configs"));
    }

    #[test]
    fn build_spawn_plan_includes_critic_configs_mount_and_profile_env() {
        let workspace = tempdir().expect("tempdir");
        let configs = tempdir().expect("tempdir");
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };

        let plan = build_spawn_plan(
            &SpawnRequest {
                name: "alpha".to_owned(),
                role: Some("ios".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                configs_host: Some(configs.path().to_path_buf()),
                profile_id: Some("ferma".to_owned()),
                profile_name: Some("Ferma".to_owned()),
                codex_model: Some("gpt-5.2-codex".to_owned()),
                codex_effort_actor: Some("high".to_owned()),
                codex_effort_critic: Some("medium".to_owned()),
                loop_enabled: Some(true),
                loop_goal: Some("ship".to_owned()),
                docker_socket: true,
                ..SpawnRequest::default()
            },
            &ctx,
        )
        .expect("build dyad spawn plan");

        assert!(plan.actor.env.iter().any(|value| value == "SI_CODEX_PROFILE_ID=ferma"));
        assert!(plan.critic.env.iter().any(|value| value == "DYAD_LOOP_ENABLED=true"));
        assert!(plan.critic.env.iter().any(|value| value == "DYAD_LOOP_GOAL=ship"));
        assert!(
            plan.critic.bind_mounts.iter().any(
                |mount| mount.source == configs.path() && mount.target == Path::new("/configs")
            )
        );
    }
}
