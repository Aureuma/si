pub fn current_version() -> &'static str {
    concat!("v", env!("CARGO_PKG_VERSION"))
}

#[cfg(test)]
mod tests {
    use super::current_version;

    #[test]
    fn returns_repo_version() {
        assert_eq!(current_version(), format!("v{}", env!("CARGO_PKG_VERSION")));
    }
}
