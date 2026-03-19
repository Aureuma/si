use std::env;
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    let args: Vec<String> = env::args().skip(1).collect();
    if args.is_empty() {
        eprintln!("usage: orbits-test-runner <unit|policy|catalog|e2e|all>");
        return ExitCode::from(2);
    }

    let go_bin = env::var("SI_GO_BIN")
        .unwrap_or_else(|_| "go".to_string())
        .trim()
        .to_string();
    let lane = args[0].trim().to_ascii_lowercase();
    let rest = &args[1..];

    match lane.as_str() {
        "unit" | "policy" | "catalog" | "e2e" => {
            if !rest.is_empty() {
                eprintln!("usage: orbits-test-runner {lane}");
                return ExitCode::from(2);
            }
            run_lane(&root, &go_bin, &lane)
        }
        "all" => run_all(&root, &go_bin, rest),
        _ => {
            eprintln!("unknown orbits lane: {lane}");
            ExitCode::from(2)
        }
    }
}

fn repo_root() -> Result<String, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("go.work").is_file() {
        Ok(cwd.display().to_string())
    } else {
        Err("go.work not found; run from repo root".to_string())
    }
}

fn run_all(root: &str, go_bin: &str, args: &[String]) -> ExitCode {
    let mut skip_unit = false;
    let mut skip_policy = false;
    let mut skip_catalog = false;
    let mut skip_e2e = false;

    for arg in args {
        match arg.as_str() {
            "--skip-unit" => skip_unit = true,
            "--skip-policy" => skip_policy = true,
            "--skip-catalog" => skip_catalog = true,
            "--skip-e2e" => skip_e2e = true,
            _ => {
                eprintln!(
                    "usage: orbits-test-runner all [--skip-unit] [--skip-policy] [--skip-catalog] [--skip-e2e]"
                );
                return ExitCode::from(2);
            }
        }
    }

    if !skip_unit {
        eprintln!("==> orbits unit");
        let status = run_lane(root, go_bin, "unit");
        if status != ExitCode::SUCCESS {
            return status;
        }
    }
    if !skip_policy {
        eprintln!("==> orbits policy");
        let status = run_lane(root, go_bin, "policy");
        if status != ExitCode::SUCCESS {
            return status;
        }
    }
    if !skip_catalog {
        eprintln!("==> orbits catalog");
        let status = run_lane(root, go_bin, "catalog");
        if status != ExitCode::SUCCESS {
            return status;
        }
    }
    if !skip_e2e {
        eprintln!("==> orbits e2e");
        let status = run_lane(root, go_bin, "e2e");
        if status != ExitCode::SUCCESS {
            return status;
        }
    }
    eprintln!("==> all requested orbit runners passed");
    ExitCode::SUCCESS
}

fn run_lane(root: &str, go_bin: &str, lane: &str) -> ExitCode {
    let args: Vec<&str> = match lane {
        "unit" => vec![
            "test",
            "-count=1",
            "./tools/si/internal/orbitals",
            "-run",
            "Test(Validate|Resolve|Parse|LoadCatalog|MergeCatalogs|InstallFromSourceRejectsUnsupportedFile|DiscoverManifestPathsFromTree|BuildCatalogFromSource|BuildCatalogFromSourceSkipsDuplicateIDs)",
        ],
        "policy" => vec![
            "test",
            "-count=1",
            "./tools/si",
            "-run",
            "TestOrbits(PolicyAffectsEffectiveState|PolicySetSupportsNamespaceWildcard)",
        ],
        "catalog" => vec![
            "test",
            "-count=1",
            "./tools/si",
            "-run",
            "TestOrbits(CatalogBuildAndValidateJSON|ListReadsEnvCatalogPaths|InfoIncludesCatalogSourceForBuiltin|ListCommandJSON|LifecycleViaCatalogJSON|UpdateCommandJSON)",
        ],
        "e2e" => vec!["test", "-count=1", "./tools/si", "-run", "TestOrbits"],
        _ => {
            eprintln!("unknown orbits lane: {lane}");
            return ExitCode::from(2);
        }
    };

    match Command::new(go_bin).args(args).current_dir(root).status() {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}
