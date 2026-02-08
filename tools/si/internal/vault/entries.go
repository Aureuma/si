package vault

type Entry struct {
	Key       string
	ValueRaw  string
	Encrypted bool
}

func Entries(doc DotenvFile) []Entry {
	out := []Entry{}
	seen := map[string]bool{}
	for _, line := range doc.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if assign.Key == "" || seen[assign.Key] {
			continue
		}
		seen[assign.Key] = true
		val, err := NormalizeDotenvValue(assign.ValueRaw)
		if err != nil {
			val = ""
		}
		out = append(out, Entry{
			Key:       assign.Key,
			ValueRaw:  val,
			Encrypted: IsEncryptedValueV1(val),
		})
	}
	return out
}
