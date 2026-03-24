use serde::Serialize;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum CommandCategory {
    Meta,
    Codex,
    Provider,
    Runtime,
    Build,
    Developer,
    Profile,
    Internal,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
pub struct CommandSpec {
    pub name: &'static str,
    pub aliases: &'static [&'static str],
    pub category: CommandCategory,
    pub summary: &'static str,
    pub hidden: bool,
}

const ROOT_COMMANDS: &[CommandSpec] = &[
    CommandSpec {
        name: "version",
        aliases: &["--version", "-v"],
        category: CommandCategory::Meta,
        summary: "Print the current si version.",
        hidden: false,
    },
    CommandSpec {
        name: "help",
        aliases: &["-h", "--help"],
        category: CommandCategory::Meta,
        summary: "Show top-level command help.",
        hidden: false,
    },
    CommandSpec {
        name: "codex",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Manage profile-bound Codex containers and profile registry state.",
        hidden: false,
    },
    CommandSpec {
        name: "analyze",
        aliases: &["lint"],
        category: CommandCategory::Developer,
        summary: "Run SI static analysis lanes.",
        hidden: false,
    },
    CommandSpec {
        name: "stripe",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "Stripe provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "vault",
        aliases: &["creds"],
        category: CommandCategory::Runtime,
        summary: "Vault encryption, secret validation, and secure env flows.",
        hidden: false,
    },
    CommandSpec {
        name: "github",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "GitHub provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "cloudflare",
        aliases: &["cf"],
        category: CommandCategory::Provider,
        summary: "Cloudflare provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "google",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "Google provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "releasemind",
        aliases: &["release"],
        category: CommandCategory::Provider,
        summary: "Release planning and repository cutover commands.",
        hidden: false,
    },
    CommandSpec {
        name: "apple",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "Apple provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "social",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "Social platform bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "workos",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "WorkOS provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "aws",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "AWS provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "gcp",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "GCP provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "openai",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "OpenAI provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "oci",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "OCI provider bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "image",
        aliases: &["images"],
        category: CommandCategory::Provider,
        summary: "Image pipeline and generation bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "publish",
        aliases: &["pub"],
        category: CommandCategory::Provider,
        summary: "Distribution publishing bridge commands.",
        hidden: false,
    },
    CommandSpec {
        name: "providers",
        aliases: &["provider", "integrations", "apis"],
        category: CommandCategory::Provider,
        summary: "Inspect provider characteristics and health.",
        hidden: false,
    },
    CommandSpec {
        name: "docker",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Pass through to Docker-oriented helper commands.",
        hidden: false,
    },
    CommandSpec {
        name: "surf",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Manage the Surf browser runtime bridge.",
        hidden: false,
    },
    CommandSpec {
        name: "dyad",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Manage actor/critic dyad runtimes.",
        hidden: false,
    },
    CommandSpec {
        name: "build",
        aliases: &[],
        category: CommandCategory::Build,
        summary: "Build images, binaries, and release assets.",
        hidden: false,
    },
    CommandSpec {
        name: "mintlify",
        aliases: &[],
        category: CommandCategory::Developer,
        summary: "Manage docs workflows through Mintlify.",
        hidden: false,
    },
    CommandSpec {
        name: "persona",
        aliases: &[],
        category: CommandCategory::Profile,
        summary: "Manage markdown persona profiles.",
        hidden: false,
    },
    CommandSpec {
        name: "skill",
        aliases: &[],
        category: CommandCategory::Profile,
        summary: "Inspect or select coding skills.",
        hidden: false,
    },
    CommandSpec {
        name: "orbits",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Manage Orbitals and integration registry entries.",
        hidden: false,
    },
    CommandSpec {
        name: "viva",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Manage Viva runtime and node helper commands.",
        hidden: false,
    },
    CommandSpec {
        name: "fort",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Access Fort runtime and configuration flows.",
        hidden: false,
    },
    CommandSpec {
        name: "remote-control",
        aliases: &["rc"],
        category: CommandCategory::Runtime,
        summary: "Expose local terminal sessions through the browser bridge.",
        hidden: false,
    },
    CommandSpec {
        name: "__fort-runtime-agent",
        aliases: &[],
        category: CommandCategory::Internal,
        summary: "Internal Fort runtime refresher entrypoint.",
        hidden: true,
    },
    CommandSpec {
        name: "test",
        aliases: &[],
        category: CommandCategory::Developer,
        summary: "Run grouped SI test lanes.",
        hidden: false,
    },
];

pub fn root_commands() -> &'static [CommandSpec] {
    ROOT_COMMANDS
}

pub fn find_root_command(name: &str) -> Option<&'static CommandSpec> {
    let candidate = name.trim();
    ROOT_COMMANDS.iter().find(|spec| spec.name == candidate || spec.aliases.contains(&candidate))
}

pub fn visible_root_commands() -> impl Iterator<Item = &'static CommandSpec> {
    ROOT_COMMANDS.iter().filter(|spec| !spec.hidden)
}

#[cfg(test)]
mod tests {
    use super::{find_root_command, root_commands};
    use std::collections::BTreeSet;

    #[test]
    fn manifest_expanded_names_are_unique() {
        let manifest_names = root_commands()
            .iter()
            .flat_map(|spec| std::iter::once(spec.name).chain(spec.aliases.iter().copied()))
            .map(str::to_owned)
            .collect::<BTreeSet<_>>();

        let expanded_len = root_commands().iter().map(|spec| 1 + spec.aliases.len()).sum::<usize>();
        assert_eq!(manifest_names.len(), expanded_len);
    }

    #[test]
    fn aliases_resolve_to_primary_command() {
        assert_eq!(find_root_command("cf"), find_root_command("cloudflare"));
        assert_eq!(find_root_command("release").unwrap().name, "releasemind");
        assert_eq!(find_root_command("creds").unwrap().name, "vault");
        assert_eq!(find_root_command("rc").unwrap().name, "remote-control");
        assert_eq!(find_root_command("github").unwrap().name, "github");
    }
}
