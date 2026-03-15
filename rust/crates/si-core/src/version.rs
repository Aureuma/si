const GO_VERSION_SOURCE: &str = include_str!("../../../../tools/si/version.go");
const VERSION_PREFIX: &str = "const siVersion = \"";

pub fn current_version() -> &'static str {
    parse_go_version(GO_VERSION_SOURCE).expect("tools/si/version.go must define siVersion")
}

fn parse_go_version(source: &'static str) -> Option<&'static str> {
    let start = source.find(VERSION_PREFIX)? + VERSION_PREFIX.len();
    let rest = &source[start..];
    let end = rest.find('"')?;
    Some(&rest[..end])
}

#[cfg(test)]
mod tests {
    use super::{current_version, parse_go_version};

    #[test]
    fn parses_go_version_constant() {
        let parsed = parse_go_version("package main\n\nconst siVersion = \"v1.2.3\"\n");
        assert_eq!(parsed, Some("v1.2.3"));
    }

    #[test]
    fn returns_repo_version() {
        assert_eq!(current_version(), "v0.54.0");
    }

    #[test]
    fn rejects_missing_version_constant() {
        assert_eq!(parse_go_version("package main\n"), None);
    }
}
