package vault

import "testing"

func TestIsTruthyEnv(t *testing.T) {
	t.Setenv("SI_TEST_ENV_FLAG", "")
	if isTruthyEnv("SI_TEST_ENV_FLAG") {
		t.Fatalf("empty should be false")
	}

	truthy := []string{"1", "true", "TRUE", " yes ", "On"}
	for _, v := range truthy {
		t.Setenv("SI_TEST_ENV_FLAG", v)
		if !isTruthyEnv("SI_TEST_ENV_FLAG") {
			t.Fatalf("expected %q to be truthy", v)
		}
	}

	falsy := []string{"0", "false", "no", "off", "random"}
	for _, v := range falsy {
		t.Setenv("SI_TEST_ENV_FLAG", v)
		if isTruthyEnv("SI_TEST_ENV_FLAG") {
			t.Fatalf("expected %q to be falsy", v)
		}
	}
}
