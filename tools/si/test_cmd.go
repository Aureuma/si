package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const testUsageText = "usage: si test <workspace|vault|orbits|all> [args]"
const testOrbitsUsageText = "usage: si test orbits <unit|policy|catalog|e2e|all> [args]"

var testWorkspaceModules = []string{
	"./agents/critic/...",
	"./agents/shared/...",
	"./tools/codex-init/...",
	"./tools/codex-interactive-driver/...",
	"./tools/codex-stdout-parser/...",
	"./tools/si/...",
}

var runTestCommand = func(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var lookupTestPath = exec.LookPath

func cmdTest(args []string) {
	if isSingleHelpArg(args) {
		printUsage(testUsageText)
		return
	}
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, testUsageText)
	if !routedOK {
		return
	}
	sub := strings.ToLower(strings.TrimSpace(routedArgs[0]))
	rest := routedArgs[1:]
	switch sub {
	case "workspace":
		cmdTestWorkspace(rest)
	case "vault":
		cmdTestVault(rest)
	case "orbits":
		cmdTestOrbits(rest)
	case "all":
		cmdTestAll(rest)
	default:
		fatal(fmt.Errorf("unknown test command: %s", sub))
	}
}

func cmdTestWorkspace(args []string) {
	if isSingleHelpArg(args) {
		printUsage("usage: si test workspace [--list]")
		return
	}
	fs := flag.NewFlagSet("test workspace", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	list := fs.Bool("list", false, "print go.work module patterns without running tests")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si test workspace [--list]"))
	}

	root, goBin := mustPrepareGoWorkspace()
	timeout := envOr("SI_GO_TEST_TIMEOUT", "15m")
	printGoVersion(goBin)
	fmt.Printf("go test timeout: %s\n", timeout)
	if *list {
		for _, mod := range testWorkspaceModules {
			fmt.Println(mod)
		}
		return
	}
	fmt.Println("Running go test on:", strings.Join(testWorkspaceModules, " "))
	testArgs := append([]string{"test", "-timeout", timeout}, testWorkspaceModules...)
	if err := runTestCommand(root, goBin, testArgs...); err != nil {
		fatal(err)
	}
}

func cmdTestVault(args []string) {
	if isSingleHelpArg(args) {
		printUsage("usage: si test vault [--quick]")
		return
	}
	fs := flag.NewFlagSet("test vault", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	quick := fs.Bool("quick", false, "skip e2e subprocess vault tests")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si test vault [--quick]"))
	}

	root, goBin := mustPrepareGoWorkspace()
	timeout := envOr("SI_GO_TEST_TIMEOUT", "20m")
	printGoVersion(goBin)
	fmt.Printf("go test timeout: %s\n", timeout)

	fmt.Println("[1/3] vault command wiring + guardrail unit tests")
	if err := runTestCommand(root, goBin,
		"test", "-timeout", timeout, "-count=1", "-shuffle=on",
		"-run", "^(TestVaultCommandActionSetsArePopulated|TestVaultActionNamesMatchDispatchSwitches|TestVaultValidateImplicitTargetRepoScope.*)$",
		"./tools/si",
	); err != nil {
		fatal(err)
	}

	fmt.Println("[2/3] vault internal package tests")
	if err := runTestCommand(root, goBin,
		"test", "-timeout", timeout, "-count=1", "-shuffle=on",
		"./tools/si/internal/vault/...",
	); err != nil {
		fatal(err)
	}

	if *quick {
		fmt.Println("[3/3] skipped vault e2e subprocess tests (--quick)")
	} else {
		fmt.Println("[3/3] vault e2e subprocess tests")
		if err := runTestCommand(root, goBin,
			"test", "-timeout", timeout, "-count=1", "-shuffle=on",
			"-run", "^TestVaultE2E_",
			"./tools/si",
		); err != nil {
			fatal(err)
		}
	}
	fmt.Println("vault strict test suite: ok")
}

func cmdTestOrbits(args []string) {
	if isSingleHelpArg(args) {
		printUsage(testOrbitsUsageText)
		return
	}
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, testOrbitsUsageText)
	if !routedOK {
		return
	}
	sub := strings.ToLower(strings.TrimSpace(routedArgs[0]))
	rest := routedArgs[1:]
	switch sub {
	case "unit":
		if requireNoArgs(rest, "usage: si test orbits unit") {
			return
		}
		runTestOrbitLane("unit")
	case "policy":
		if requireNoArgs(rest, "usage: si test orbits policy") {
			return
		}
		runTestOrbitLane("policy")
	case "catalog":
		if requireNoArgs(rest, "usage: si test orbits catalog") {
			return
		}
		runTestOrbitLane("catalog")
	case "e2e":
		if requireNoArgs(rest, "usage: si test orbits e2e") {
			return
		}
		runTestOrbitLane("e2e")
	case "all":
		cmdTestOrbitsAll(rest)
	default:
		fatal(fmt.Errorf("unknown test orbits command: %s", sub))
	}
}

func cmdTestOrbitsAll(args []string) {
	if isSingleHelpArg(args) {
		printUsage("usage: si test orbits all [--skip-unit] [--skip-policy] [--skip-catalog] [--skip-e2e]")
		return
	}
	fs := flag.NewFlagSet("test orbits all", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	skipUnit := fs.Bool("skip-unit", false, "skip orbits unit lane")
	skipPolicy := fs.Bool("skip-policy", false, "skip orbits policy lane")
	skipCatalog := fs.Bool("skip-catalog", false, "skip orbits catalog lane")
	skipE2E := fs.Bool("skip-e2e", false, "skip orbits e2e lane")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si test orbits all [--skip-unit] [--skip-policy] [--skip-catalog] [--skip-e2e]"))
	}

	runStep := func(label string, fn func()) {
		fmt.Fprintf(os.Stderr, "==> %s\n", label)
		fn()
	}

	if !*skipUnit {
		runStep("orbits unit", func() { runTestOrbitLane("unit") })
	}
	if !*skipPolicy {
		runStep("orbits policy", func() { runTestOrbitLane("policy") })
	}
	if !*skipCatalog {
		runStep("orbits catalog", func() { runTestOrbitLane("catalog") })
	}
	if !*skipE2E {
		runStep("orbits e2e", func() { runTestOrbitLane("e2e") })
	}
	fmt.Fprintln(os.Stderr, "==> all requested orbit runners passed")
}

func runTestOrbitLane(lane string) {
	root, goBin := mustPrepareGoWorkspace()
	switch strings.ToLower(strings.TrimSpace(lane)) {
	case "unit":
		if err := runTestCommand(root, goBin, "test", "-count=1", "./tools/si/internal/orbitals", "-run", "Test(Validate|Resolve|Parse|LoadCatalog|MergeCatalogs|InstallFromSourceRejectsUnsupportedFile|DiscoverManifestPathsFromTree|BuildCatalogFromSource|BuildCatalogFromSourceSkipsDuplicateIDs)"); err != nil {
			fatal(err)
		}
	case "policy":
		if err := runTestCommand(root, goBin, "test", "-count=1", "./tools/si", "-run", "TestOrbits(PolicyAffectsEffectiveState|PolicySetSupportsNamespaceWildcard)"); err != nil {
			fatal(err)
		}
	case "catalog":
		if err := runTestCommand(root, goBin, "test", "-count=1", "./tools/si", "-run", "TestOrbits(CatalogBuildAndValidateJSON|ListReadsEnvCatalogPaths|InfoIncludesCatalogSourceForBuiltin|ListCommandJSON|LifecycleViaCatalogJSON|UpdateCommandJSON)"); err != nil {
			fatal(err)
		}
	case "e2e":
		if err := runTestCommand(root, goBin, "test", "-count=1", "./tools/si", "-run", "TestOrbits"); err != nil {
			fatal(err)
		}
	default:
		fatal(fmt.Errorf("unknown orbits lane: %s", lane))
	}
}

func cmdTestAll(args []string) {
	if isSingleHelpArg(args) {
		printUsage("usage: si test all [--skip-go] [--skip-vault] [--skip-installer] [--skip-npm] [--skip-docker]")
		return
	}
	fs := flag.NewFlagSet("test all", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	skipGo := fs.Bool("skip-go", false, "skip go workspace tests")
	skipVault := fs.Bool("skip-vault", false, "skip vault strict suite")
	skipInstaller := fs.Bool("skip-installer", false, "skip installer host smoke tests")
	skipNPM := fs.Bool("skip-npm", false, "skip npm installer smoke tests")
	skipDocker := fs.Bool("skip-docker", false, "skip installer docker smoke tests")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si test all [--skip-go] [--skip-vault] [--skip-installer] [--skip-npm] [--skip-docker]"))
	}

	root := mustGoWorkRoot()
	runStep := func(title string, fn func()) {
		fmt.Fprintf(os.Stderr, "==> %s\n", title)
		fn()
	}

	if !*skipGo {
		runStep("Go workspace tests", func() { cmdTestWorkspace(nil) })
	}
	if !*skipVault {
		runStep("Vault strict suite", func() { cmdTestVault(nil) })
	}
	if !*skipInstaller {
		runStep("Installer host smoke", func() {
			runShellScript(root, "./tools/test-install-si.sh")
		})
	}
	if !*skipNPM {
		runStep("npm installer smoke", func() {
			runShellScript(root, "./tools/test-install-si-npm.sh")
		})
	}
	if !*skipDocker {
		runStep("Installer docker smoke", func() {
			runShellScript(root, "./tools/test-install-si-docker.sh")
		})
	}
	fmt.Fprintln(os.Stderr, "==> all requested tests passed")
}

func runShellScript(root string, relPath string) {
	script := filepath.Clean(strings.TrimSpace(relPath))
	if script == "" {
		fatal(errors.New("script path required"))
	}
	if !filepath.IsAbs(script) {
		script = filepath.Join(root, script)
	}
	if err := runTestCommand(root, script); err != nil {
		fatal(err)
	}
}

func requireNoArgs(args []string, usage string) bool {
	if isSingleHelpArg(args) {
		printUsage(usage)
		return true
	}
	if len(args) == 0 {
		return false
	}
	fatal(fmt.Errorf("%s", usage))
	return true
}

func mustPrepareGoWorkspace() (string, string) {
	root := mustGoWorkRoot()
	goBin, err := lookupTestPath("go")
	if err != nil {
		fatal(errors.New("go is required but was not found on PATH"))
	}
	return root, goBin
}

func mustGoWorkRoot() string {
	root, err := repoRoot()
	if err != nil {
		fatal(errors.New("go.work not found. Run this command from the repo root."))
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		fatal(errors.New("go.work not found. Run this command from the repo root."))
	}
	return root
}

func printGoVersion(goBin string) {
	out, err := exec.Command(goBin, "version").Output()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("go version: %s\n", strings.TrimSpace(string(out)))
}
