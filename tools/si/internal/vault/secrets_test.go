package vault

import "testing"

func TestEncryptDotenvValuesIdempotentWithoutReencrypt(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	doc := ParseDotenv([]byte("" +
		"# si-vault:v1\n" +
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
		"# si-vault:v1\n" +
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
