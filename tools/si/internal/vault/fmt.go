package vault

import (
	"fmt"
	"strings"
)

func FormatVaultDotenv(input DotenvFile) (DotenvFile, bool, error) {
	recipients := ParseRecipientsFromDotenv(input)
	if len(recipients) == 0 {
		return DotenvFile{}, false, fmt.Errorf("no recipients found (expected %q lines)", VaultRecipientPrefix)
	}

	nl := input.DefaultNL
	if nl == "" {
		nl = "\n"
	}
	out := DotenvFile{DefaultNL: nl}

	// Header.
	out.Lines = append(out.Lines, RawLine{Text: VaultHeaderVersionLine, NL: nl})
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out.Lines = append(out.Lines, RawLine{Text: VaultRecipientPrefix + r, NL: nl})
	}
	out.Lines = append(out.Lines, RawLine{Text: "", NL: nl})

	// Body: normalize KEY=value spacing and comment spacing, but preserve any
	// divider markers and section header lines exactly as-authored.
	body := stripVaultHeaderLines(input.Lines)
	body = trimLeadingBlank(body)

	pendingBlank := false
	for _, line := range body {
		text := line.Text
		trim := strings.TrimSpace(text)
		if trim == "" {
			pendingBlank = true
			continue
		}
		if isVaultHeaderLine(text) {
			continue
		}
		if _, ok := isSectionHeaderLine(text); ok {
			// Keep section markers exactly as-authored; only normalize surrounding whitespace.
			if len(out.Lines) > 0 {
				last := out.Lines[len(out.Lines)-1].Text
				// Allow "# ----" divider lines to sit directly above a section header.
				if strings.TrimSpace(last) != "" && !isDividerLine(last) {
					out.Lines = append(out.Lines, RawLine{Text: "", NL: nl})
				}
			}
			out.Lines = append(out.Lines, RawLine{Text: strings.TrimRight(text, "\r\n"), NL: nl})
			pendingBlank = false
			continue
		}
		if isDividerLine(text) {
			// Keep divider markers exactly as-authored.
			if pendingBlank {
				if len(out.Lines) > 0 && strings.TrimSpace(out.Lines[len(out.Lines)-1].Text) != "" {
					out.Lines = append(out.Lines, RawLine{Text: "", NL: nl})
				}
				pendingBlank = false
			}
			out.Lines = append(out.Lines, RawLine{Text: strings.TrimRight(text, "\r\n"), NL: nl})
			continue
		}
		if pendingBlank {
			// Collapse multiple blank lines to a single one.
			if len(out.Lines) > 0 && strings.TrimSpace(out.Lines[len(out.Lines)-1].Text) != "" {
				out.Lines = append(out.Lines, RawLine{Text: "", NL: nl})
			}
			pendingBlank = false
		}

		if assign, ok := parseAssignment(text); ok {
			val := strings.TrimSpace(assign.ValueRaw)
			out.Lines = append(out.Lines, RawLine{Text: renderAssignment("", assign.Export, assign.Key, val, normalizeInlineComment(assign.Comment)), NL: nl})
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			out.Lines = append(out.Lines, RawLine{Text: normalizeCommentLine(text), NL: nl})
			continue
		}
		// Unknown lines: keep as-is.
		out.Lines = append(out.Lines, RawLine{Text: strings.TrimRight(text, "\r\n"), NL: nl})
	}

	// Ensure trailing newline.
	if len(out.Lines) > 0 {
		out.Lines[len(out.Lines)-1].NL = nl
	}

	changed := string(input.Bytes()) != string(out.Bytes())
	return out, changed, nil
}

func stripVaultHeaderLines(lines []RawLine) []RawLine {
	out := make([]RawLine, 0, len(lines))
	for _, line := range lines {
		if isVaultHeaderLine(line.Text) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func isVaultHeaderLine(line string) bool {
	if isVersionLine(line) {
		return true
	}
	_, ok := parseRecipientLine(line)
	return ok
}

func trimLeadingBlank(lines []RawLine) []RawLine {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i].Text) == "" {
		i++
	}
	return lines[i:]
}

func normalizeCommentLine(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return ""
	}
	if strings.HasPrefix(trim, "#") {
		body := strings.TrimSpace(strings.TrimPrefix(trim, "#"))
		if body == "" {
			return "#"
		}
		return "# " + body
	}
	return "# " + strings.TrimSpace(trim)
}

func normalizeInlineComment(comment string) string {
	comment = strings.TrimRight(comment, "\r\n")
	if strings.TrimSpace(comment) == "" {
		return ""
	}
	// Keep the comment content but normalize to " # ...".
	trim := strings.TrimSpace(comment)
	if strings.HasPrefix(trim, "#") {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, "#"))
	}
	if trim == "" {
		return ""
	}
	return " # " + trim
}
