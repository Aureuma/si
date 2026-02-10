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

func TestLazyWorkOSRootHandlerLoadsOnDispatch(t *testing.T) {
	resetRootCommandHandlersForTest()
	originalLoader := loadWorkOSRootHandler
	defer func() {
		loadWorkOSRootHandler = originalLoader
		resetRootCommandHandlersForTest()
	}()

	loaded := 0
	invoked := 0
	loadWorkOSRootHandler = func() rootCommandHandler {
		loaded++
		return func(_ string, _ []string) {
			invoked++
		}
	}

	if loaded != 0 || invoked != 0 {
		t.Fatalf("unexpected eager execution")
	}
	if !dispatchRootCommand("workos", []string{"help"}) {
		t.Fatalf("expected workos command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected exactly one lazy load, got %d", loaded)
	}
	if invoked != 1 {
		t.Fatalf("expected one invocation, got %d", invoked)
	}
	if !dispatchRootCommand("workos", []string{"auth"}) {
		t.Fatalf("expected workos command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected lazy load once, got %d", loaded)
	}
	if invoked != 2 {
		t.Fatalf("expected second invocation, got %d", invoked)
	}
}

func TestLazyAWSRootHandlerLoadsOnDispatch(t *testing.T) {
	resetRootCommandHandlersForTest()
	originalLoader := loadAWSRootHandler
	defer func() {
		loadAWSRootHandler = originalLoader
		resetRootCommandHandlersForTest()
	}()

	loaded := 0
	invoked := 0
	loadAWSRootHandler = func() rootCommandHandler {
		loaded++
		return func(_ string, _ []string) {
			invoked++
		}
	}

	if loaded != 0 || invoked != 0 {
		t.Fatalf("unexpected eager execution")
	}
	if !dispatchRootCommand("aws", []string{"help"}) {
		t.Fatalf("expected aws command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected exactly one lazy load, got %d", loaded)
	}
	if invoked != 1 {
		t.Fatalf("expected one invocation, got %d", invoked)
	}
	if !dispatchRootCommand("aws", []string{"auth"}) {
		t.Fatalf("expected aws command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected lazy load once, got %d", loaded)
	}
	if invoked != 2 {
		t.Fatalf("expected second invocation, got %d", invoked)
	}
}

func TestLazyPublishRootHandlerLoadsOnDispatch(t *testing.T) {
	resetRootCommandHandlersForTest()
	originalLoader := loadPublishRootHandler
	defer func() {
		loadPublishRootHandler = originalLoader
		resetRootCommandHandlersForTest()
	}()

	loaded := 0
	invoked := 0
	loadPublishRootHandler = func() rootCommandHandler {
		loaded++
		return func(_ string, _ []string) {
			invoked++
		}
	}

	if loaded != 0 || invoked != 0 {
		t.Fatalf("unexpected eager execution")
	}
	if !dispatchRootCommand("publish", []string{"help"}) {
		t.Fatalf("expected publish command dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected exactly one lazy load, got %d", loaded)
	}
	if invoked != 1 {
		t.Fatalf("expected one invocation, got %d", invoked)
	}
	if !dispatchRootCommand("pub", []string{"catalog"}) {
		t.Fatalf("expected publish alias dispatch")
	}
	if loaded != 1 {
		t.Fatalf("expected lazy load once, got %d", loaded)
	}
	if invoked != 2 {
		t.Fatalf("expected second invocation, got %d", invoked)
	}
}
