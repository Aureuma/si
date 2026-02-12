package vault

import (
	"strings"
	"testing"
)

func TestEncryptDotenvValuesIdempotentWithoutReencrypt(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"A=hello\n"))

	res1, err := EncryptDotenvValues(&doc, id, false)
	if err != nil {
		t.Fatalf("EncryptDotenvValues: %v", err)
	}
	if !res1.Changed {
		t.Fatalf("expected change")
	}
	out1 := string(doc.Bytes())

	res2, err := EncryptDotenvValues(&doc, id, false)
	if err != nil {
		t.Fatalf("EncryptDotenvValues2: %v", err)
	}
	if res2.Changed {
		t.Fatalf("expected no change")
	}
	out2 := string(doc.Bytes())
	if out1 != out2 {
		t.Fatalf("expected byte-identical output")
	}
}

func TestEncryptDotenvValuesReencryptChangesCiphertextButNotPlaintext(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"A=hello\n"))

	_, err = EncryptDotenvValues(&doc, id, false)
	if err != nil {
		t.Fatalf("EncryptDotenvValues: %v", err)
	}
	c1, _ := doc.Lookup("A")

	res, err := EncryptDotenvValues(&doc, id, true)
	if err != nil {
		t.Fatalf("EncryptDotenvValues reencrypt: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected change")
	}
	c2, _ := doc.Lookup("A")
	if c1 == c2 {
		t.Fatalf("expected ciphertext to change on reencrypt")
	}
	dec, err := DecryptEnv(doc, id)
	if err != nil {
		t.Fatalf("DecryptEnv: %v", err)
	}
	if dec.Values["A"] != "hello" {
		t.Fatalf("got %q want %q", dec.Values["A"], "hello")
	}
}

func TestEncryptDotenvValuesPreservesAssignmentLayout(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"\texport API_KEY   =   \"abc\" # keep me\n"))

	_, err = EncryptDotenvValues(&doc, id, false)
	if err != nil {
		t.Fatalf("EncryptDotenvValues: %v", err)
	}
	line := doc.Lines[len(doc.Lines)-1].Text
	if !strings.HasPrefix(line, "\texport API_KEY   =   ") {
		t.Fatalf("layout prefix changed: %q", line)
	}
	if !strings.HasSuffix(line, " # keep me") {
		t.Fatalf("layout suffix changed: %q", line)
	}
}

func TestEncryptDotenvValuesErrorsWithoutRecipients(t *testing.T) {
	doc := ParseDotenv([]byte("A=1\n"))
	if _, err := EncryptDotenvValues(&doc, nil, false); err == nil {
		t.Fatalf("expected error")
	}
}

func TestDecryptEnvWithMixedValuesAndQuotes(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	cipher, err := EncryptStringV1("secret", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	doc := ParseDotenv([]byte("" +
		"PLAIN=abc\n" +
		"SINGLE='hello world'\n" +
		"DOUBLE=\"line1\\nline2\"\n" +
		"ENC=" + cipher + "\n"))
	dec, err := DecryptEnv(doc, id)
	if err != nil {
		t.Fatalf("DecryptEnv: %v", err)
	}
	if dec.Values["PLAIN"] != "abc" {
		t.Fatalf("PLAIN=%q", dec.Values["PLAIN"])
	}
	if dec.Values["SINGLE"] != "hello world" {
		t.Fatalf("SINGLE=%q", dec.Values["SINGLE"])
	}
	if dec.Values["DOUBLE"] != "line1\nline2" {
		t.Fatalf("DOUBLE=%q", dec.Values["DOUBLE"])
	}
	if dec.Values["ENC"] != "secret" {
		t.Fatalf("ENC=%q", dec.Values["ENC"])
	}
}

func TestDecryptEnvRequiresIdentityForEncryptedValue(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	cipher, err := EncryptStringV1("secret", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	doc := ParseDotenv([]byte("ENC=" + cipher + "\n"))
	if _, err := DecryptEnv(doc, nil); err == nil {
		t.Fatalf("expected identity error")
	}
}

func TestDecryptEnvDuplicateKeysLastValueWins(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"A=1\n" +
		"A=2\n"))
	dec, err := DecryptEnv(doc, nil)
	if err != nil {
		t.Fatalf("DecryptEnv: %v", err)
	}
	if dec.Values["A"] != "2" {
		t.Fatalf("A=%q", dec.Values["A"])
	}
}

func TestDecryptEnvErrorsOnInvalidQuotedPlaintext(t *testing.T) {
	doc := ParseDotenv([]byte("A=\"unterminated\n"))
	if _, err := DecryptEnv(doc, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEncryptDotenvValuesErrorsOnInvalidQuotedPlaintext(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"A=\"unterminated\n"))
	if _, err := EncryptDotenvValues(&doc, nil, false); err == nil {
		t.Fatalf("expected error")
	}
}

func TestDecryptEnvErrorsOnInvalidKeyName(t *testing.T) {
	doc := ParseDotenv([]byte("BAD KEY=1\n"))
	if _, err := DecryptEnv(doc, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEncryptDotenvValuesErrorsOnInvalidKeyName(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"BAD KEY=1\n"))
	if _, err := EncryptDotenvValues(&doc, nil, false); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEncryptDotenvValuesPreservesLayoutMatrix(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	cases := []struct {
		name string
		line string
	}{
		{name: "plain", line: "A=abc"},
		{name: "hash in unquoted value", line: "A=abc#def"},
		{name: "single-quoted", line: "A='hello world'"},
		{name: "double-quoted escapes", line: "A=\"line1\\nline2\\tq\""},
		{name: "export with spacing and comment", line: "\texport API_KEY   =   \"a b\"   # keep"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := ParseDotenv([]byte("" +
				"# si-vault:v2\n" +
				"# si-vault:recipient " + recipient + "\n" +
				"\n" +
				tc.line + "\n"))
			before := tc.line
			assign, ok := parseAssignment(before)
			if !ok {
				t.Fatalf("parseAssignment failed for %q", before)
			}
			plain, err := NormalizeDotenvValue(assign.ValueRaw)
			if err != nil {
				t.Fatalf("NormalizeDotenvValue: %v", err)
			}
			prefix := assign.LeftRaw + "=" + assign.ValueWS
			suffix := assign.Comment

			res, err := EncryptDotenvValues(&doc, id, false)
			if err != nil {
				t.Fatalf("EncryptDotenvValues: %v", err)
			}
			if !res.Changed {
				t.Fatalf("expected change")
			}

			out := doc.Lines[len(doc.Lines)-1].Text
			if !strings.HasPrefix(out, prefix) {
				t.Fatalf("prefix changed: got %q want prefix %q", out, prefix)
			}
			if suffix != "" && !strings.HasSuffix(out, suffix) {
				t.Fatalf("suffix changed: got %q want suffix %q", out, suffix)
			}

			dec, err := DecryptEnv(doc, id)
			if err != nil {
				t.Fatalf("DecryptEnv: %v", err)
			}
			if dec.Values[assign.Key] != plain {
				t.Fatalf("decrypted value mismatch: got %q want %q", dec.Values[assign.Key], plain)
			}
		})
	}
}
