package vault

import (
	"strings"
)

func EnsureVaultHeader(doc *DotenvFile, recipients []string) (bool, error) {
	if doc == nil {
		return false, nil
	}
	nl := doc.DefaultNL
	if nl == "" {
		nl = "\n"
		doc.DefaultNL = nl
	}

	// Normalize recipients: keep order, drop empties, trim.
	seen := map[string]bool{}
	normalized := []string{}
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		normalized = append(normalized, r)
	}
	if len(normalized) == 0 {
		return false, nil
	}

	// If the file already has a vault header block at the very top, only ensure
	// missing recipients are added to that block (minimal diff).
	hasVersion := false
	existingRecipients := map[string]bool{}
	headerEnd := 0
	for headerEnd < len(doc.Lines) {
		line := doc.Lines[headerEnd].Text
		if strings.TrimSpace(line) == "" {
			if hasVersion || len(existingRecipients) > 0 {
				headerEnd++
			}
			break
		}
		if isVersionLine(line) {
			hasVersion = true
			headerEnd++
			continue
		}
		if r, ok := parseRecipientLine(line); ok {
			existingRecipients[r] = true
			headerEnd++
			continue
		}
		break
	}
	if hasVersion || len(existingRecipients) > 0 {
		changed := false
		// Insert after the last recipient line in the header block.
		insertAt := headerEnd
		for i := 0; i < headerEnd; i++ {
			if _, ok := parseRecipientLine(doc.Lines[i].Text); ok {
				insertAt = i + 1
			}
		}
		for _, r := range normalized {
			if existingRecipients[r] {
				continue
			}
			doc.ensureLineHasNL(insertAt - 1)
			doc.Lines = append(doc.Lines[:insertAt], append([]RawLine{{Text: VaultRecipientPrefix + r, NL: nl}}, doc.Lines[insertAt:]...)...)
			insertAt++
			changed = true
		}
		if !hasVersion {
			doc.Lines = append([]RawLine{{Text: VaultHeaderVersionLine, NL: nl}}, doc.Lines...)
			changed = true
		}

		// Ensure at least one blank line after the header block.
		headerLinesEnd := 0
		for headerLinesEnd < len(doc.Lines) {
			if isVersionLine(doc.Lines[headerLinesEnd].Text) {
				headerLinesEnd++
				continue
			}
			if _, ok := parseRecipientLine(doc.Lines[headerLinesEnd].Text); ok {
				headerLinesEnd++
				continue
			}
			break
		}
		if headerLinesEnd >= len(doc.Lines) {
			doc.Lines = append(doc.Lines, RawLine{Text: "", NL: nl})
			changed = true
		} else if strings.TrimSpace(doc.Lines[headerLinesEnd].Text) != "" {
			doc.ensureLineHasNL(headerLinesEnd - 1)
			doc.Lines = append(doc.Lines[:headerLinesEnd], append([]RawLine{{Text: "", NL: nl}}, doc.Lines[headerLinesEnd:]...)...)
			changed = true
		}
		return changed, nil
	}

	// No header at top: prepend one.
	lines := []RawLine{{Text: VaultHeaderVersionLine, NL: nl}}
	for _, r := range normalized {
		lines = append(lines, RawLine{Text: VaultRecipientPrefix + r, NL: nl})
	}
	lines = append(lines, RawLine{Text: "", NL: nl})
	doc.Lines = append(lines, doc.Lines...)
	return true, nil
}

func isVersionLine(line string) bool {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "#") {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, "#"))
	}
	return trim == "si-vault:v1"
}

func parseRecipientLine(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "#") {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, "#"))
	}
	if !strings.HasPrefix(trim, "si-vault:recipient") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trim, "si-vault:recipient"))
	if rest == "" {
		return "", false
	}
	return rest, true
}

func RemoveRecipient(doc *DotenvFile, recipient string) bool {
	if doc == nil {
		return false
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return false
	}
	changed := false
	out := make([]RawLine, 0, len(doc.Lines))
	for _, line := range doc.Lines {
		r, ok := parseRecipientLine(line.Text)
		if ok && strings.TrimSpace(r) == recipient {
			changed = true
			continue
		}
		out = append(out, line)
	}
	if changed {
		doc.Lines = out
	}
	return changed
}
