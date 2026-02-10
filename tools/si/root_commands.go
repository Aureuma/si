package main

import (
	"os"
	"sync"
)

type rootCommandHandler func(cmd string, args []string)

var (
	rootCommandsMu      sync.Mutex
	rootCommandHandlers map[string]rootCommandHandler

	loadStripeRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdStripe(args) }
	}
	loadVaultRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdVault(args) }
	}
	loadGithubRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdGithub(args) }
	}
	loadCloudflareRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdCloudflare(args) }
	}
	loadGoogleRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdGoogle(args) }
	}
	loadSocialRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdSocial(args) }
	}
	loadProvidersRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdProviders(args) }
	}
	loadDockerRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdDocker(args) }
	}
	loadDyadRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdDyad(args) }
	}
	loadBuildRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdBuild(args) }
	}
	loadPersonaRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdPersona(args) }
	}
	loadSkillRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdSkill(args) }
	}
)

func dispatchRootCommand(cmd string, args []string) bool {
	handlers := getRootCommandHandlers()
	handler, ok := handlers[cmd]
	if !ok {
		return false
	}
	handler(cmd, args)
	return true
}

func buildRootCommandHandlers() map[string]rootCommandHandler {
	handlers := map[string]rootCommandHandler{}
	register := func(handler rootCommandHandler, names ...string) {
		for _, name := range names {
			handlers[name] = handler
		}
	}

	register(func(_ string, _ []string) { printVersion() }, "version", "--version", "-v")
	register(func(cmd string, args []string) {
		if !dispatchCodexCommand(cmd, args) {
			printUnknown("", cmd)
			usage()
			os.Exit(1)
		}
	}, "spawn", "respawn", "list", "ps", "status", "report", "login", "logout", "exec", "run", "logs", "tail", "clone", "remove", "rm", "delete", "stop", "start", "warmup")
	register(func(_ string, args []string) { cmdAnalyze(args) }, "analyze", "lint")
	register(newLazyRootHandler(loadStripeRootHandler), "stripe")
	register(newLazyRootHandler(loadVaultRootHandler), "vault", "creds")
	register(newLazyRootHandler(loadGithubRootHandler), "github")
	register(newLazyRootHandler(loadCloudflareRootHandler), "cloudflare", "cf")
	register(newLazyRootHandler(loadGoogleRootHandler), "google")
	register(newLazyRootHandler(loadSocialRootHandler), "social")
	register(newLazyRootHandler(loadProvidersRootHandler), "providers", "provider", "integrations", "apis")
	register(newLazyRootHandler(loadDockerRootHandler), "docker")
	register(newLazyRootHandler(loadDyadRootHandler), "dyad")
	register(newLazyRootHandler(loadBuildRootHandler), "build")
	register(newLazyRootHandler(loadPersonaRootHandler), "persona")
	register(newLazyRootHandler(loadSkillRootHandler), "skill")
	register(func(_ string, _ []string) { usage() }, "help", "-h", "--help")

	return handlers
}

func getRootCommandHandlers() map[string]rootCommandHandler {
	rootCommandsMu.Lock()
	defer rootCommandsMu.Unlock()
	if rootCommandHandlers == nil {
		rootCommandHandlers = buildRootCommandHandlers()
	}
	return rootCommandHandlers
}

func resetRootCommandHandlersForTest() {
	rootCommandsMu.Lock()
	rootCommandHandlers = nil
	rootCommandsMu.Unlock()
}

func newLazyRootHandler(loader func() rootCommandHandler) rootCommandHandler {
	var (
		once    sync.Once
		handler rootCommandHandler
	)
	return func(cmd string, args []string) {
		once.Do(func() {
			handler = loader()
		})
		handler(cmd, args)
	}
}
