package vault

type Entry struct {
	Key       string
	ValueRaw  string
	Encrypted bool
}

func Entries(doc DotenvFile) ([]Entry, error) {
	lastByKey := map[string]Entry{}
	order := []string{}
	seenInOrder := map[string]bool{}
	for _, line := range doc.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if err := ValidateKeyName(assign.Key); err != nil {
			return nil, err
		}
		if !seenInOrder[assign.Key] {
			order = append(order, assign.Key)
			seenInOrder[assign.Key] = true
		}
		val, err := NormalizeDotenvValue(assign.ValueRaw)
		if err != nil {
			return nil, err
		}
		lastByKey[assign.Key] = Entry{
			Key:       assign.Key,
			ValueRaw:  val,
			Encrypted: IsEncryptedValueV1(val),
		}
	}
	out := make([]Entry, 0, len(order))
	for _, key := range order {
		out = append(out, lastByKey[key])
	}
	return out, nil
}
