package githubbridge

import "strings"

func parseNextLink(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pieces := strings.Split(part, ";")
		if len(pieces) < 2 {
			continue
		}
		urlPart := strings.TrimSpace(pieces[0])
		relPart := strings.TrimSpace(strings.Join(pieces[1:], ";"))
		if !strings.Contains(relPart, `rel="next"`) {
			continue
		}
		if strings.HasPrefix(urlPart, "<") && strings.HasSuffix(urlPart, ">") {
			return strings.TrimSuffix(strings.TrimPrefix(urlPart, "<"), ">")
		}
		return urlPart
	}
	return ""
}
