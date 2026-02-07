package stripebridge

import "testing"

func TestResolveObject(t *testing.T) {
	spec, err := ResolveObject("products")
	if err != nil {
		t.Fatalf("expected products alias to resolve: %v", err)
	}
	if spec.Name != "product" {
		t.Fatalf("unexpected object name: %q", spec.Name)
	}
	if !spec.SupportsOp(CRUDCreate) {
		t.Fatalf("expected product create support")
	}
}

func TestResolveObjectUnknown(t *testing.T) {
	if _, err := ResolveObject("does_not_exist"); err == nil {
		t.Fatalf("expected unknown object error")
	}
}

func TestBuildCRUDRequestUnsupported(t *testing.T) {
	spec, err := ResolveObject("price")
	if err != nil {
		t.Fatalf("resolve price: %v", err)
	}
	if _, err := BuildCRUDRequest(spec, CRUDDelete, "price_123", nil, ""); err == nil {
		t.Fatalf("expected unsupported delete for price")
	}
}
