use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

pub const TMUX_SESSION_PREFIX: &str = "si-codex-pane-";
pub const CODEX_PROFILE_FORT_DIR_NAME: &str = "fort";
pub const CODEX_PROFILE_FORT_SESSION_FILE_NAME: &str = "session.json";
pub const CODEX_PROFILE_FORT_ACCESS_TOKEN_FILE_NAME: &str = "access.token";
pub const CODEX_PROFILE_FORT_REFRESH_TOKEN_FILE_NAME: &str = "refresh.token";
pub const CODEX_PROFILE_FORT_RUNTIME_LOCK_FILE_NAME: &str = "runtime.lock";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CodexProfileFortSessionPaths {
    pub dir: PathBuf,
    pub session_path: PathBuf,
    pub access_token_path: PathBuf,
    pub refresh_token_path: PathBuf,
    pub lock_path: PathBuf,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PromptSegment {
    pub prompt: String,
    pub lines: Vec<String>,
    pub raw: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReportParseResult {
    pub segments: Vec<PromptSegment>,
    pub report: String,
}

pub fn codex_worker_name(profile_id: &str) -> String {
    profile_id.trim().to_owned()
}

pub fn codex_tmux_session_name(profile_id: &str) -> String {
    let suffix = codex_worker_name(profile_id);
    if suffix.is_empty() {
        TMUX_SESSION_PREFIX.to_owned()
    } else {
        format!("{TMUX_SESSION_PREFIX}{suffix}")
    }
}

pub fn codex_profile_fort_session_paths(codex_home: &Path) -> CodexProfileFortSessionPaths {
    let dir = codex_home.join(CODEX_PROFILE_FORT_DIR_NAME);
    CodexProfileFortSessionPaths {
        session_path: dir.join(CODEX_PROFILE_FORT_SESSION_FILE_NAME),
        access_token_path: dir.join(CODEX_PROFILE_FORT_ACCESS_TOKEN_FILE_NAME),
        refresh_token_path: dir.join(CODEX_PROFILE_FORT_REFRESH_TOKEN_FILE_NAME),
        lock_path: dir.join(CODEX_PROFILE_FORT_RUNTIME_LOCK_FILE_NAME),
        dir,
    }
}

pub fn codex_profile_fort_runtime_env(codex_home: &Path) -> BTreeMap<String, String> {
    let paths = codex_profile_fort_session_paths(codex_home);
    BTreeMap::from([
        ("FORT_TOKEN_PATH".to_owned(), paths.access_token_path.display().to_string()),
        ("FORT_REFRESH_TOKEN_PATH".to_owned(), paths.refresh_token_path.display().to_string()),
    ])
}

pub fn parse_prompt_segments_dual(clean: &str, raw: &str) -> Vec<PromptSegment> {
    let mut clean_lines: Vec<String> = clean.split('\n').map(str::to_owned).collect();
    let mut raw_lines: Vec<String> = raw.split('\n').map(str::to_owned).collect();
    if raw_lines.len() < clean_lines.len() {
        raw_lines.resize(clean_lines.len(), String::new());
    }
    if clean_lines.len() < raw_lines.len() {
        clean_lines.resize(raw_lines.len(), String::new());
    }
    let mut segments = Vec::with_capacity(8);
    let mut current: Option<PromptSegment> = None;
    for (clean_line, raw_line) in clean_lines.into_iter().zip(raw_lines.into_iter()) {
        let trimmed = clean_line.trim_start();
        if let Some(prompt) = trimmed.strip_prefix('›') {
            if let Some(segment) = current.take() {
                segments.push(segment);
            }
            current = Some(PromptSegment {
                prompt: prompt.trim().to_owned(),
                lines: Vec::new(),
                raw: Vec::new(),
            });
            continue;
        }
        if let Some(segment) = current.as_mut() {
            segment.lines.push(clean_line);
            segment.raw.push(raw_line);
        }
    }
    if let Some(segment) = current {
        segments.push(segment);
    }
    segments
}

pub fn parse_report_capture(
    clean: &str,
    raw: &str,
    prompt_index: usize,
    ansi: bool,
) -> ReportParseResult {
    let segments = parse_prompt_segments_dual(clean, raw);
    let report = if prompt_index < segments.len() {
        extract_report_lines_from_lines(
            &segments[prompt_index].raw,
            &segments[prompt_index].lines,
            ansi,
        )
    } else {
        String::new()
    };
    ReportParseResult { segments, report }
}

fn extract_report_lines_from_lines(
    raw_lines: &[String],
    clean_lines: &[String],
    ansi: bool,
) -> String {
    let max = raw_lines.len().min(clean_lines.len());
    struct Block {
        raw: Vec<String>,
        clean: Vec<String>,
    }
    let mut blocks: Vec<Block> = Vec::new();
    let mut current = Block { raw: Vec::new(), clean: Vec::new() };
    let mut in_report = false;
    let mut worked_line_raw = String::new();
    let mut worked_line_clean = String::new();
    for i in 0..max {
        let raw = raw_lines[i].trim_end_matches([' ', '\t']).to_owned();
        let clean = clean_lines[i].trim_end_matches([' ', '\t']).to_owned();
        let clean_core = clean.trim_start().to_owned();
        if clean_core.to_ascii_lowercase().contains("worked for") {
            worked_line_raw = raw.clone();
            worked_line_clean = clean.clone();
        }
        if clean_core.starts_with("• ") {
            in_report = true;
            current.raw.push(raw);
            current.clean.push(clean);
            continue;
        }
        if !in_report {
            continue;
        }
        if clean.trim().is_empty() {
            if !current.raw.is_empty() {
                blocks.push(current);
                current = Block { raw: Vec::new(), clean: Vec::new() };
            }
            in_report = false;
            continue;
        }
        if clean.starts_with("  ") {
            current.raw.push(raw);
            current.clean.push(clean);
            continue;
        }
        let core = clean.trim().to_owned();
        if core.starts_with('⚠')
            || core.starts_with("Tip:")
            || core.starts_with('›')
            || core.starts_with("• Starting MCP")
            || core.starts_with("• Starting")
        {
            if !current.raw.is_empty() {
                blocks.push(current);
            }
            current = Block { raw: Vec::new(), clean: Vec::new() };
            break;
        }
        current.raw.push(raw);
        current.clean.push(clean);
    }
    if !current.raw.is_empty() {
        blocks.push(current);
    }
    for block in blocks.into_iter().rev() {
        if block.raw.is_empty() || is_transient_report(&block.clean) {
            continue;
        }
        let mut out = if ansi { block.raw } else { block.clean };
        let worked_line = if ansi { worked_line_raw.clone() } else { worked_line_clean.clone() };
        while out.last().is_some_and(|line| line.trim().is_empty()) {
            out.pop();
        }
        if !worked_line.is_empty() && !out.iter().any(|line| line == &worked_line) {
            out.push(worked_line);
        }
        return out.join("\n");
    }
    String::new()
}

fn is_transient_report(lines: &[String]) -> bool {
    if lines.is_empty() {
        return true;
    }
    let head = lines[0].trim();
    head.starts_with("• Working")
        || head.contains("esc to interrupt")
        || head.starts_with("• Starting MCP")
}

#[cfg(test)]
mod tests {
    use super::{
        codex_profile_fort_runtime_env, codex_profile_fort_session_paths, codex_tmux_session_name,
        parse_prompt_segments_dual, parse_report_capture,
    };
    use std::path::Path;

    #[test]
    fn codex_tmux_session_name_uses_profile_slug() {
        assert_eq!(codex_tmux_session_name("profile-delta"), "si-codex-pane-profile-delta");
    }

    #[test]
    fn codex_profile_fort_session_paths_are_under_codex_home() {
        let paths =
            codex_profile_fort_session_paths(Path::new("/tmp/home/.si/codex/profiles/cadma"));

        assert_eq!(paths.dir, Path::new("/tmp/home/.si/codex/profiles/cadma/fort"));
        assert_eq!(
            paths.session_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/session.json")
        );
        assert_eq!(
            paths.access_token_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/access.token")
        );
        assert_eq!(
            paths.refresh_token_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/refresh.token")
        );
        assert_eq!(
            paths.lock_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/runtime.lock")
        );
    }

    #[test]
    fn codex_profile_fort_runtime_env_exports_token_paths() {
        let env = codex_profile_fort_runtime_env(Path::new("/tmp/home/.si/codex/profiles/cadma"));

        assert_eq!(
            env.get("FORT_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/cadma/fort/access.token")
        );
        assert_eq!(
            env.get("FORT_REFRESH_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/cadma/fort/refresh.token")
        );
    }

    #[test]
    fn parse_prompt_segments_dual_pairs_clean_and_raw() {
        let clean = "› first\nline a\n› second\nline b";
        let raw = "› first\nraw a\n› second\nraw b";
        let parsed = parse_prompt_segments_dual(clean, raw);
        assert_eq!(parsed.len(), 2);
        assert_eq!(parsed[0].prompt, "first");
        assert_eq!(parsed[0].lines, vec!["line a".to_owned()]);
        assert_eq!(parsed[0].raw, vec!["raw a".to_owned()]);
        assert_eq!(parsed[1].prompt, "second");
    }

    #[test]
    fn parse_report_capture_extracts_report_block_for_prompt_index() {
        let clean = "› prompt\n• Did the thing\n  detail\n  worked for 42s\n";
        let raw = clean;
        let parsed = parse_report_capture(clean, raw, 0, false);
        assert_eq!(parsed.segments.len(), 1);
        assert!(parsed.report.contains("Did the thing"));
        assert!(parsed.report.contains("worked for 42s"));
    }
}
