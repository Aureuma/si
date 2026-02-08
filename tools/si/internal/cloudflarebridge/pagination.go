package cloudflarebridge

import "strings"

func hasMoreResultInfo(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	info, ok := data["result_info"].(map[string]any)
	if !ok {
		return false
	}
	page := toInt(info["page"])
	total := toInt(info["total_pages"])
	if page <= 0 || total <= 0 {
		return false
	}
	return page < total
}

func normalizedPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
