package vault

import "testing"

func TestEntriesDuplicateKeysUseLastValue(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"A=1\n" +
		"B=2\n" +
		"A=3\n"))
	entries := Entries(doc)
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].Key != "A" || entries[0].ValueRaw != "3" {
		t.Fatalf("entry0=%+v", entries[0])
	}
	if entries[1].Key != "B" || entries[1].ValueRaw != "2" {
		t.Fatalf("entry1=%+v", entries[1])
	}
}

func TestEntriesInvalidQuotedValueReturnsEmptyRaw(t *testing.T) {
	doc := ParseDotenv([]byte("A=\"unterminated\n"))
	entries := Entries(doc)
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].ValueRaw != "" {
		t.Fatalf("value=%q", entries[0].ValueRaw)
	}
}
