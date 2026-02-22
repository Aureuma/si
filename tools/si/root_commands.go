package main

import (
	"os"
	"sync"
	"sync/atomic"
)

type rootCommandHandler func(cmd string, args []string)

var (
	rootCommandsMu      sync.Mutex
	rootCommandHandlers map[string]rootCommandHandler
	rootCommandsPtr     atomic.Pointer[map[string]rootCommandHandler]

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
	loadAppleRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdApple(args) }
	}
	loadSocialRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdSocial(args) }
	}
	loadWorkOSRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdWorkOS(args) }
	}
	loadAWSRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdAWS(args) }
	}
	loadGCPRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdGCP(args) }
	}
	loadOpenAIRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdOpenAI(args) }
	}
	loadOCIRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdOCI(args) }
	}
	loadImageRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdImage(args) }
	}
	loadPublishRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdPublish(args) }
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
	loadMintlifyRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdMintlify(args) }
	}
	loadBrowserRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdBrowser(args) }
	}
	loadPaasRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdPaas(args) }
	}
	loadSunRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdSun(args) }
	}
	loadPersonaRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdPersona(args) }
	}
	loadSkillRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdSkill(args) }
	}
	loadPluginsRootHandler = func() rootCommandHandler {
		return func(_ string, args []string) { cmdPlugins(args) }
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
	handlers := make(map[string]rootCommandHandler, 64)
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
	}, "spawn", "respawn", "list", "ps", "status", "report", "login", "logout", "swap", "exec", "run", "logs", "tail", "clone", "remove", "rm", "delete", "stop", "start", "warmup")
	register(func(_ string, args []string) { cmdAnalyze(args) }, "analyze", "lint")
	register(newLazyRootHandler(loadStripeRootHandler), "stripe")
	register(newLazyRootHandler(loadVaultRootHandler), "vault", "creds")
	register(newLazyRootHandler(loadGithubRootHandler), "github")
	register(newLazyRootHandler(loadCloudflareRootHandler), "cloudflare", "cf")
	register(newLazyRootHandler(loadGoogleRootHandler), "google")
	register(newLazyRootHandler(loadAppleRootHandler), "apple")
	register(newLazyRootHandler(loadSocialRootHandler), "social")
	register(newLazyRootHandler(loadWorkOSRootHandler), "workos")
	register(newLazyRootHandler(loadAWSRootHandler), "aws")
	register(newLazyRootHandler(loadGCPRootHandler), "gcp")
	register(newLazyRootHandler(loadOpenAIRootHandler), "openai")
	register(newLazyRootHandler(loadOCIRootHandler), "oci")
	register(newLazyRootHandler(loadImageRootHandler), "image", "images")
	register(newLazyRootHandler(loadPublishRootHandler), "publish", "pub")
	register(newLazyRootHandler(loadProvidersRootHandler), "providers", "provider", "integrations", "apis")
	register(newLazyRootHandler(loadDockerRootHandler), "docker")
	register(newLazyRootHandler(loadBrowserRootHandler), "browser")
	register(newLazyRootHandler(loadDyadRootHandler), "dyad")
	register(newLazyRootHandler(loadBuildRootHandler), "build")
	register(newLazyRootHandler(loadMintlifyRootHandler), "mintlify")
	register(newLazyRootHandler(loadPaasRootHandler), "paas")
	register(newLazyRootHandler(loadSunRootHandler), "sun")
	register(newLazyRootHandler(loadPersonaRootHandler), "persona")
	register(newLazyRootHandler(loadSkillRootHandler), "skill")
	register(newLazyRootHandler(loadPluginsRootHandler), "plugins", "plugin", "marketplace")
	register(func(_ string, _ []string) { usage() }, "help", "-h", "--help")

	return handlers
}

func getRootCommandHandlers() map[string]rootCommandHandler {
	if ptr := rootCommandsPtr.Load(); ptr != nil {
		return *ptr
	}
	rootCommandsMu.Lock()
	defer rootCommandsMu.Unlock()
	if ptr := rootCommandsPtr.Load(); ptr != nil {
		return *ptr
	}
	if rootCommandHandlers == nil {
		handlers := buildRootCommandHandlers()
		rootCommandHandlers = handlers
		rootCommandsPtr.Store(&rootCommandHandlers)
	}
	return rootCommandHandlers
}

func resetRootCommandHandlersForTest() {
	rootCommandsMu.Lock()
	rootCommandHandlers = nil
	rootCommandsPtr.Store(nil)
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
