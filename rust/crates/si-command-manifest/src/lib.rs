use serde::Serialize;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum CommandCategory {
    Meta,
    Codex,
    Muse,
    Provider,
    Runtime,
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
        name: "commands",
        aliases: &[],
        category: CommandCategory::Meta,
        summary: "List visible SI root commands.",
        hidden: false,
    },
    CommandSpec {
        name: "doctor",
        aliases: &[],
        category: CommandCategory::Runtime,
        summary: "Check fresh-machine SI distribution prerequisites.",
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
        name: "codex",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Manage profile-bound Codex workers and profile registry state.",
        hidden: false,
    },
    CommandSpec {
        name: "muse",
        aliases: &[],
        category: CommandCategory::Muse,
        summary: "Manage profile-bound Muse workers and profile registry state.",
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

    const DOCS: &[(&str, &str)] = &[
        ("CLI_REFERENCE.md", include_str!("../../../../docs/CLI_REFERENCE.md")),
        ("COMMAND_REFERENCE.md", include_str!("../../../../docs/COMMAND_REFERENCE.md")),
        ("SETTINGS.md", include_str!("../../../../docs/SETTINGS.md")),
        ("testing.md", include_str!("../../../../docs/testing.md")),
        ("index.mdx", include_str!("../../../../docs/index.mdx")),
    ];

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
        assert_eq!(find_root_command("--version").unwrap().name, "version");
        assert_eq!(find_root_command("--help").unwrap().name, "help");
    }

    #[test]
    fn visible_docs_do_not_advertise_removed_root_commands() {
        for (doc, source) in DOCS {
            for removed in [
                "si publish",
                "si social",
                "si remote-control",
                "si fort",
                "si surf",
                "si viva",
                "si image",
                "si build",
                "warmup",
            ] {
                assert!(
                    !source.contains(removed),
                    "{doc} advertises removed root command {removed}"
                );
            }
        }
    }

    #[test]
    fn command_reference_documents_visible_root_commands() {
        let command_reference = include_str!("../../../../docs/COMMAND_REFERENCE.md");
        for spec in root_commands().iter().filter(|spec| !spec.hidden) {
            let command = format!("si {}", spec.name);
            assert!(
                command_reference.contains(&command),
                "COMMAND_REFERENCE.md does not document visible command {command}"
            );
        }
    }

    #[test]
    fn settings_reference_documents_agent_profiles() {
        let settings_reference = include_str!("../../../../docs/SETTINGS.md");
        for heading in ["### `[codex]`", "#### `[codex.profiles]`"] {
            assert!(
                settings_reference.contains(heading),
                "SETTINGS.md missing agent settings heading {heading}"
            );
        }
        for removed in ["### `[fort]`", "### `[viva]`", "### `[surf]`", "fort.repo"] {
            assert!(
                !settings_reference.contains(removed),
                "SETTINGS.md still documents removed settings section {removed}"
            );
        }
    }
}
