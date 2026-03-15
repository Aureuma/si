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
        name: "spawn",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Start or attach to a codex runtime container.",
        hidden: false,
    },
    CommandSpec {
        name: "respawn",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Recreate a codex runtime container with the current profile context.",
        hidden: false,
    },
    CommandSpec {
        name: "list",
        aliases: &["ps"],
        category: CommandCategory::Codex,
        summary: "List codex runtimes and their profile bindings.",
        hidden: false,
    },
    CommandSpec {
        name: "status",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Show codex profile or runtime status.",
        hidden: false,
    },
    CommandSpec {
        name: "report",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Collect a codex runtime report.",
        hidden: false,
    },
    CommandSpec {
        name: "login",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Authenticate a codex profile.",
        hidden: false,
    },
    CommandSpec {
        name: "logout",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Clear codex profile auth state.",
        hidden: false,
    },
    CommandSpec {
        name: "swap",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Swap host codex auth to a configured profile.",
        hidden: false,
    },
    CommandSpec {
        name: "exec",
        aliases: &["run"],
        category: CommandCategory::Codex,
        summary: "Execute commands in an existing or one-off codex runtime.",
        hidden: false,
    },
    CommandSpec {
        name: "logs",
        aliases: &["tail"],
        category: CommandCategory::Codex,
        summary: "Read codex runtime logs.",
        hidden: false,
    },
    CommandSpec {
        name: "clone",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Clone a repository into a codex runtime.",
        hidden: false,
    },
    CommandSpec {
        name: "remove",
        aliases: &["rm", "delete"],
        category: CommandCategory::Codex,
        summary: "Remove codex runtimes and optional volumes.",
        hidden: false,
    },
    CommandSpec {
        name: "stop",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Stop a codex runtime.",
        hidden: false,
    },
    CommandSpec {
        name: "start",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Start a codex runtime.",
        hidden: false,
    },
    CommandSpec {
        name: "warmup",
        aliases: &[],
        category: CommandCategory::Codex,
        summary: "Manage codex warmup scheduling and reconciliation.",
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
    use regex::Regex;
    use std::collections::BTreeSet;

    #[test]
    fn manifest_expanded_names_match_go_root_registry() {
        let go_names = extract_go_root_names();
        let manifest_names = root_commands()
            .iter()
            .flat_map(|spec| std::iter::once(spec.name).chain(spec.aliases.iter().copied()))
            .map(str::to_owned)
            .collect::<BTreeSet<_>>();

        assert_eq!(manifest_names, go_names);
    }

    #[test]
    fn aliases_resolve_to_primary_command() {
        assert_eq!(find_root_command("cf"), find_root_command("cloudflare"));
        assert_eq!(find_root_command("release").unwrap().name, "releasemind");
        assert_eq!(find_root_command("rm").unwrap().name, "remove");
        assert_eq!(find_root_command("github").unwrap().name, "github");
    }

    fn extract_go_root_names() -> BTreeSet<String> {
        let source = include_str!("../../../../tools/si/root_commands.go");
        let string_literal = Regex::new(r#""([^"]+)""#).expect("regex");
        let mut names = BTreeSet::new();
        let mut offset = 0;

        while let Some(start) = source[offset..].find("register(") {
            let start = offset + start;
            let end = find_register_call_end(source, start).expect("complete register call");
            let call = &source[start..end];
            let names_region = register_name_region(call).expect("register name region");
            for capture in string_literal.captures_iter(names_region) {
                names.insert(capture[1].to_owned());
            }
            offset = end;
        }

        names
    }

    fn find_register_call_end(source: &str, start: usize) -> Option<usize> {
        let bytes = source.as_bytes();
        let mut depth = 0usize;
        let mut index = start;
        let mut in_string = false;
        let mut escaped = false;

        while index < bytes.len() {
            let byte = bytes[index];
            if in_string {
                if escaped {
                    escaped = false;
                } else if byte == b'\\' {
                    escaped = true;
                } else if byte == b'"' {
                    in_string = false;
                }
                index += 1;
                continue;
            }

            match byte {
                b'"' => in_string = true,
                b'(' => depth += 1,
                b')' => {
                    depth = depth.saturating_sub(1);
                    if depth == 0 {
                        return Some(index + 1);
                    }
                }
                _ => {}
            }
            index += 1;
        }

        None
    }

    fn register_name_region(call: &str) -> Option<&str> {
        let bytes = call.as_bytes();
        let mut depth = 0usize;
        let mut index = 0usize;
        let mut in_string = false;
        let mut escaped = false;

        while index < bytes.len() {
            let byte = bytes[index];
            if in_string {
                if escaped {
                    escaped = false;
                } else if byte == b'\\' {
                    escaped = true;
                } else if byte == b'"' {
                    in_string = false;
                }
                index += 1;
                continue;
            }

            match byte {
                b'"' => in_string = true,
                b'(' => depth += 1,
                b')' => depth = depth.saturating_sub(1),
                b',' if depth == 1 => return call.get(index + 1..call.len().saturating_sub(1)),
                _ => {}
            }
            index += 1;
        }

        None
    }
}
