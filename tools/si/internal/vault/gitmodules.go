package vault

import (
	"fmt"
	"path/filepath"
	"strings"
)

func EnsureGitmodulesIgnoreDirty(repoRoot, submodulePathRel string) (bool, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	submodulePathRel = filepath.Clean(strings.TrimSpace(submodulePathRel))
	if repoRoot == "" || submodulePathRel == "" {
		return false, fmt.Errorf("repo root and submodule path required")
	}
	path := filepath.Join(repoRoot, ".gitmodules")
	data, err := readFileScoped(path)
	if err != nil {
		return false, err
	}
	doc := ParseDotenv(data) // line splitting + newline preservation
	changed := false

	type section struct{ start, end int }
	sections := []section{}
	secStart := -1
	for i := range doc.Lines {
		trim := strings.TrimSpace(doc.Lines[i].Text)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			if secStart >= 0 {
				sections = append(sections, section{start: secStart, end: i})
			}
			secStart = i
		}
	}
	if secStart >= 0 {
		sections = append(sections, section{start: secStart, end: len(doc.Lines)})
	}

	for _, sec := range sections {
		pathIdx := -1
		urlIdx := -1
		ignoreIdx := -1
		indent := "\t"
		for i := sec.start + 1; i < sec.end; i++ {
			line := doc.Lines[i].Text
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
				continue
			}
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			}
			if key, val, ok := parseGitmodulesKV(trim); ok && key == "path" && filepath.Clean(val) == submodulePathRel {
				pathIdx = i
			}
			if key, _, ok := parseGitmodulesKV(trim); ok && key == "url" {
				urlIdx = i
			}
			if key, _, ok := parseGitmodulesKV(trim); ok && key == "ignore" {
				ignoreIdx = i
			}
		}
		if pathIdx < 0 {
			continue
		}
		if ignoreIdx >= 0 {
			key, val, ok := parseGitmodulesKV(strings.TrimSpace(doc.Lines[ignoreIdx].Text))
			if ok && key == "ignore" && val == "dirty" {
				return false, nil
			}
			doc.Lines[ignoreIdx].Text = indent + "ignore = dirty"
			changed = true
			break
		}
		insertAfter := pathIdx
		if urlIdx >= 0 {
			insertAfter = urlIdx
		}
		doc.ensureLineHasNL(insertAfter)
		line := RawLine{Text: indent + "ignore = dirty", NL: doc.DefaultNL}
		insertAt := insertAfter + 1
		doc.Lines = append(doc.Lines[:insertAt], append([]RawLine{line}, doc.Lines[insertAt:]...)...)
		changed = true
		break
	}

	if !changed {
		return false, nil
	}
	if err := WriteDotenvFileAtomic(path, doc.Bytes()); err != nil {
		return false, err
	}
	return true, nil
}

func parseGitmodulesKV(line string) (string, string, bool) {
	key, val, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)
	if key == "" {
		return "", "", false
	}
	return strings.ToLower(key), val, true
}
