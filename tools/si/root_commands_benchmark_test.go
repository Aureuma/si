package main

import "testing"

func BenchmarkBuildRootCommandHandlers(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = buildRootCommandHandlers()
	}
}

func BenchmarkLazyProvidersDispatch(b *testing.B) {
	resetRootCommandHandlersForTest()
	originalLoader := loadProvidersRootHandler
	defer func() {
		loadProvidersRootHandler = originalLoader
		resetRootCommandHandlersForTest()
	}()

	loaded := 0
	loadProvidersRootHandler = func() rootCommandHandler {
		loaded++
		return func(_ string, _ []string) {}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !dispatchRootCommand("providers", nil) {
			b.Fatalf("providers command did not dispatch")
		}
	}
	b.StopTimer()
	if loaded != 1 {
		b.Fatalf("expected lazy loader to initialize once, got %d", loaded)
	}
}
