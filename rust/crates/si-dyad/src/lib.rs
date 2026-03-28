use si_rs_docker::{BindMount, ContainerSpec, PublishedPort, VolumeMount};
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

#[derive(Debug, Error, Eq, PartialEq)]
pub enum SpawnContainerSpecError {
    #[error("invalid forward port entry {entry:?}")]
    InvalidForwardPortEntry { entry: String },
    #[error("invalid forward port range {start}-{end}")]
    InvalidForwardPortRange { start: i32, end: i32 },
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum PeekPlanError {
    #[error("dyad name required")]
    MissingName,
    #[error("invalid member {member:?} (expected actor, critic, or both)")]
    InvalidMember { member: String },
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PeekPlan {
    pub dyad: String,
    pub member: String,
    pub actor_container_name: String,
    pub critic_container_name: String,
    pub actor_session_name: String,
    pub critic_session_name: String,
    pub peek_session_name: String,
    pub actor_attach_command: String,
    pub critic_attach_command: String,
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

pub fn build_peek_plan(
    dyad: &str,
    member: &str,
    host_session_name: Option<&str>,
) -> Result<PeekPlan, PeekPlanError> {
    let dyad = dyad.trim();
    if dyad.is_empty() {
        return Err(PeekPlanError::MissingName);
    }
    let member = normalize_peek_member(member)?;
    let suffix = sanitize_tmux_suffix(dyad);
    let actor_container_name = dyad_container_name(dyad, "actor");
    let critic_container_name = dyad_container_name(dyad, "critic");
    let actor_session_name = format!("si-dyad-{suffix}-actor");
    let critic_session_name = format!("si-dyad-{suffix}-critic");
    let peek_session_name = host_session_name
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .unwrap_or_else(|| format!("si-dyad-peek-{suffix}"));

    Ok(PeekPlan {
        dyad: dyad.to_owned(),
        member,
        actor_attach_command: build_peek_attach_command(&actor_container_name, &actor_session_name),
        critic_attach_command: build_peek_attach_command(
            &critic_container_name,
            &critic_session_name,
        ),
        actor_container_name,
        critic_container_name,
        actor_session_name,
        critic_session_name,
        peek_session_name,
    })
}

pub fn build_container_specs(
    plan: &SpawnPlan,
) -> Result<(ContainerSpec, ContainerSpec), SpawnContainerSpecError> {
    let mut actor = ContainerSpec::new(plan.actor.image.clone())
        .name(plan.actor.container_name.clone())
        .detach(true)
        .auto_remove(false)
        .network(plan.network_name.clone())
        .restart_policy("unless-stopped")
        .user("root")
        .workdir(PathBuf::from(DEFAULT_WORKSPACE_TARGET));
    for (key, value) in &plan.actor.labels {
        actor = actor.label(key.clone(), value.clone());
    }
    for entry in &plan.actor.env {
        if let Some((key, value)) = entry.split_once('=') {
            actor = actor.env(key.trim(), value);
        }
    }
    for mount in &plan.actor.bind_mounts {
        actor = actor.mount(
            BindMount::new(mount.source.clone(), mount.target.clone()).read_only(mount.read_only),
        );
    }
    for mount in &plan.actor.volume_mounts {
        actor = actor.volume_mount(
            VolumeMount::new(mount.source.clone(), mount.target.clone()).read_only(mount.read_only),
        );
    }
    for port in parse_forward_ports(&plan.forward_ports)? {
        actor = actor.published_port(port);
    }
    actor = actor.command(plan.actor.command.clone());

    let mut critic = ContainerSpec::new(plan.critic.image.clone())
        .name(plan.critic.container_name.clone())
        .detach(true)
        .auto_remove(false)
        .network(plan.network_name.clone())
        .restart_policy("unless-stopped")
        .user("root");
    for (key, value) in &plan.critic.labels {
        critic = critic.label(key.clone(), value.clone());
    }
    for entry in &plan.critic.env {
        if let Some((key, value)) = entry.split_once('=') {
            critic = critic.env(key.trim(), value);
        }
    }
    for mount in &plan.critic.bind_mounts {
        critic = critic.mount(
            BindMount::new(mount.source.clone(), mount.target.clone()).read_only(mount.read_only),
        );
    }
    for mount in &plan.critic.volume_mounts {
        critic = critic.volume_mount(
            VolumeMount::new(mount.source.clone(), mount.target.clone()).read_only(mount.read_only),
        );
    }
    critic = critic.command(plan.critic.command.clone());

    Ok((actor, critic))
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

fn parse_forward_ports(raw: &str) -> Result<Vec<PublishedPort>, SpawnContainerSpecError> {
    let raw = raw.trim();
    let raw = if raw.is_empty() { DEFAULT_FORWARD_PORTS } else { raw };
    let mut ports = Vec::new();
    for part in raw.split(',').map(str::trim).filter(|part| !part.is_empty()) {
        if let Some((start, end)) = part.split_once('-') {
            let start = parse_forward_port(start)?;
            let end = parse_forward_port(end)?;
            if end < start {
                return Err(SpawnContainerSpecError::InvalidForwardPortRange { start, end });
            }
            for port in start..=end {
                ports.push(PublishedPort::dynamic(port as u16));
            }
            continue;
        }
        let port = parse_forward_port(part)?;
        ports.push(PublishedPort::dynamic(port as u16));
    }
    Ok(ports)
}

fn parse_forward_port(raw: &str) -> Result<i32, SpawnContainerSpecError> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Err(SpawnContainerSpecError::InvalidForwardPortEntry { entry: raw.to_owned() });
    }
    let value = raw
        .parse::<i32>()
        .map_err(|_| SpawnContainerSpecError::InvalidForwardPortEntry { entry: raw.to_owned() })?;
    if !(1..=65535).contains(&value) {
        return Err(SpawnContainerSpecError::InvalidForwardPortEntry { entry: raw.to_owned() });
    }
    Ok(value)
}

fn normalize_peek_member(member: &str) -> Result<String, PeekPlanError> {
    let member = member.trim().to_ascii_lowercase();
    let member = if member.is_empty() { "both".to_owned() } else { member };
    match member.as_str() {
        "actor" | "critic" | "both" => Ok(member),
        _ => Err(PeekPlanError::InvalidMember { member }),
    }
}

fn sanitize_tmux_suffix(raw: &str) -> String {
    let raw = raw.trim().to_ascii_lowercase();
    if raw.is_empty() {
        return "unknown".to_owned();
    }
    let mut out = String::new();
    for ch in raw.chars() {
        if ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-' || ch == '_' {
            out.push(ch);
        }
    }
    if out.is_empty() { "unknown".to_owned() } else { out }
}

fn build_peek_attach_command(container: &str, session: &str) -> String {
    let container = container.trim();
    let session = session.trim();
    if container.is_empty() || session.is_empty() {
        return "echo missing dyad peek target; sleep 3".to_owned();
    }
    format!(
        "set -e\nwhile ! docker exec {container} tmux has-session -t {session} >/dev/null 2>&1; do\n  sleep 0.2\ndone\nexec docker exec -it {container} tmux attach -t {session}\n"
    )
}

#[cfg(test)]
mod tests {
    use super::{
        DEFAULT_FORWARD_PORTS, DEFAULT_SKILLS_VOLUME, PeekPlanError, SpawnContainerSpecError,
        SpawnRequest, build_container_specs, build_peek_plan, build_spawn_plan,
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
                profile_id: Some("profile-zeta".to_owned()),
                profile_name: Some("Profile Zeta".to_owned()),
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

        assert!(plan.actor.env.iter().any(|value| value == "SI_CODEX_PROFILE_ID=profile-zeta"));
        assert!(plan.critic.env.iter().any(|value| value == "DYAD_LOOP_ENABLED=true"));
        assert!(plan.critic.env.iter().any(|value| value == "DYAD_LOOP_GOAL=ship"));
        assert!(
            plan.critic.bind_mounts.iter().any(
                |mount| mount.source == configs.path() && mount.target == Path::new("/configs")
            )
        );
    }

    #[test]
    fn build_container_specs_renders_actor_ports_and_critic_configs_mount() {
        let workspace = tempdir().expect("tempdir");
        let configs = tempdir().expect("tempdir");

        let plan = build_spawn_plan(
            &SpawnRequest {
                name: "alpha".to_owned(),
                workspace_host: workspace.path().to_path_buf(),
                configs_host: Some(configs.path().to_path_buf()),
                forward_ports: Some("1455-1456".to_owned()),
                docker_socket: true,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("build dyad spawn plan");

        let (actor, critic) = build_container_specs(&plan).expect("build dyad container specs");

        assert_eq!(actor.name_ref(), Some("si-actor-alpha"));
        assert_eq!(actor.network_ref(), Some("si"));
        assert_eq!(actor.published_ports().len(), 2);
        assert_eq!(actor.working_dir(), Some(Path::new("/workspace")));
        assert_eq!(critic.name_ref(), Some("si-critic-alpha"));
        assert_eq!(critic.network_ref(), Some("si"));
        assert!(critic.bind_mounts().iter().any(|mount| mount.target() == Path::new("/configs")));
        assert_eq!(critic.command_args(), &["critic".to_owned()]);
    }

    #[test]
    fn build_container_specs_rejects_invalid_forward_port_range() {
        let workspace = tempdir().expect("tempdir");
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: "alpha".to_owned(),
                workspace_host: workspace.path().to_path_buf(),
                forward_ports: Some("1460-1455".to_owned()),
                docker_socket: true,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("build dyad spawn plan");

        let err = build_container_specs(&plan).expect_err("invalid port range should fail");
        assert_eq!(
            err,
            SpawnContainerSpecError::InvalidForwardPortRange { start: 1460, end: 1455 }
        );
    }

    #[test]
    fn build_peek_plan_defaults_names_and_attach_commands() {
        let plan = build_peek_plan("alpha", "both", None).expect("build peek plan");

        assert_eq!(plan.actor_container_name, "si-actor-alpha");
        assert_eq!(plan.critic_container_name, "si-critic-alpha");
        assert_eq!(plan.actor_session_name, "si-dyad-alpha-actor");
        assert_eq!(plan.critic_session_name, "si-dyad-alpha-critic");
        assert_eq!(plan.peek_session_name, "si-dyad-peek-alpha");
        assert!(
            plan.actor_attach_command
                .contains("docker exec si-actor-alpha tmux has-session -t si-dyad-alpha-actor")
        );
        assert!(
            plan.critic_attach_command
                .contains("docker exec -it si-critic-alpha tmux attach -t si-dyad-alpha-critic")
        );
    }

    #[test]
    fn build_peek_plan_honors_session_override_and_normalizes_member() {
        let plan = build_peek_plan("Alpha", "ACTOR", Some("peek-main")).expect("build peek plan");

        assert_eq!(plan.member, "actor");
        assert_eq!(plan.peek_session_name, "peek-main");
        assert_eq!(plan.actor_session_name, "si-dyad-alpha-actor");
    }

    #[test]
    fn build_peek_plan_rejects_invalid_member() {
        let err = build_peek_plan("alpha", "observer", None).expect_err("invalid member");
        assert_eq!(err, PeekPlanError::InvalidMember { member: "observer".to_owned() });
    }
}
