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
        name: "build",
        aliases: &[],
        category: CommandCategory::Build,
        summary: "Build binaries and release assets.",
        hidden: false,
    },
    CommandSpec {
        name: "commands",
        aliases: &[],
        category: CommandCategory::Meta,
        summary: "List visible SI root commands.",
        hidden: false,
    },
    CommandSpec {
        name: "settings",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Show resolved SI settings.",
        hidden: false,
    },
    CommandSpec {
        name: "orbit",
        aliases: &[],
        category: CommandCategory::Provider,
        summary: "Manage first-party provider integrations.",
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
        name: "codex",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Manage profile-bound Codex workers and profile registry state.",
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
        name: "vault",
        aliases: &["creds"],
        category: CommandCategory::Runtime,
        summary: "Vault encryption, secret validation, and secure env flows.",
        hidden: false,
    },
    CommandSpec {
        name: "__fort-runtime-agent",
        aliases: &[],
        category: CommandCategory::Internal,
        summary: "Internal Fort runtime refresher entrypoint.",
        hidden: true,
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
        assert_eq!(find_root_command("creds").unwrap().name, "vault");
        assert_eq!(find_root_command("images").unwrap().name, "image");
        assert_eq!(find_root_command("orbit").unwrap().name, "orbit");
    }
}
