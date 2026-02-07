package stripebridge

import "encoding/json"

func decodeMap(src map[string]any, dst interface{}) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func stringField(item map[string]any, key string) (string, bool) {
	if item == nil {
		return "", false
	}
	value, ok := item[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}
