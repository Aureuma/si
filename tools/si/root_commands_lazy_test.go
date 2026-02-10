package main

import "testing"

func TestLazyRootHandlerLoadsOnDispatch(t *testing.T) {
	resetRootCommandHandlersForTest()
	originalLoader := loadSocialRootHandler
	defer func() {
		loadSocialRootHandler = originalLoader
		resetRootCommandHandlersForTest()
	}()

	loaded := 0
	invoked := 0
	loadSocialRootHandler = func() rootCommandHandler {
		loaded++
		return func(_ string, _ []string) {
			invoked++
		}
	}

	if loaded != 0 || invoked != 0 {
		t.Fatalf("unexpected eager execution")
	}
	if !dispatchRootCommand("social", []string{"help"}) {
		t.Fatalf("expected social command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected exactly one lazy load, got %d", loaded)
	}
	if invoked != 1 {
		t.Fatalf("expected one invocation, got %d", invoked)
	}
	if !dispatchRootCommand("social", []string{"facebook"}) {
		t.Fatalf("expected social command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected lazy load once, got %d", loaded)
	}
	if invoked != 2 {
		t.Fatalf("expected second invocation, got %d", invoked)
	}
}
