package vault

import "testing"

func TestValidateEnvNameAcceptsCommonNames(t *testing.T) {
	ok := []string{
		"dev",
		"prod",
		"staging-us",
		"qa_2",
		"team.alpha",
	}
	for _, name := range ok {
		if err := ValidateEnvName(name); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
}

func TestValidateEnvNameRejectsUnsafeNames(t *testing.T) {
	cases := []string{
		"",
		"../prod",
		"..\\prod",
		"prod/dev",
		"prod\\dev",
		"bad env",
		"bad\nenv",
	}
	for _, name := range cases {
		if err := ValidateEnvName(name); err == nil {
			t.Fatalf("expected error for %q", name)
		}
	}
}
