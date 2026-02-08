package githubbridge

import "encoding/json"

func decodeMap(src map[string]any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
