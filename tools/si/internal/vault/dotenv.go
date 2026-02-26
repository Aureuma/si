package vault

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RawLine struct {
	Text string
	NL   string // "\n", "\r\n", or "" for final line with no newline
}

type DotenvFile struct {
	Lines     []RawLine
	DefaultNL string
}

type SetOptions struct {
	Section string
}

func ReadDotenvFile(path string) (DotenvFile, error) {
	data, err := readFileScoped(path)
	if err != nil {
		return DotenvFile{}, err
	}
	return ParseDotenv(data), nil
}

func WriteDotenvFileAtomic(path string, contents []byte) error {
	path = filepath.Clean(path)
	writePath, err := resolveDotenvWriteTarget(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(writePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(writePath); err == nil {
		mode = info.Mode() & os.ModePerm
	}
	tmp, err := os.CreateTemp(dir, ".env.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), writePath)
}

func ParseDotenv(data []byte) DotenvFile {
	lines := splitRawLines(data)
	nl := "\n"
	for _, line := range lines {
		if line.NL != "" {
			nl = line.NL
			break
		}
	}
	return DotenvFile{Lines: lines, DefaultNL: nl}
}

func (f DotenvFile) Bytes() []byte {
	var buf bytes.Buffer
	for _, line := range f.Lines {
		buf.WriteString(line.Text)
		buf.WriteString(line.NL)
	}
	return buf.Bytes()
}

func (f *DotenvFile) Lookup(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	found := false
	val := ""
	for _, line := range f.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if assign.Key == key {
			val = strings.TrimSpace(assign.ValueRaw)
			found = true
		}
	}
	return val, found
}

func (f *DotenvFile) Set(key, value string, opts SetOptions) (bool, error) {
	key = strings.TrimSpace(key)
	if err := ValidateKeyName(key); err != nil {
		return false, err
	}
	section := normalizeSectionName(opts.Section)
	if section != "" {
		return f.setInSection(key, value, section)
	}
	last := -1
	var lastAssign assignment
	for i, line := range f.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if assign.Key == key {
			last = i
			lastAssign = assign
		}
	}
	if last >= 0 {
		line := renderAssignmentPreserveLayout(lastAssign, key, value, lastAssign.Comment)
		if f.Lines[last].Text == line {
			return false, nil
		}
		f.Lines[last].Text = line
		return true, nil
	}

	f.ensureAppendable()
	f.Lines = append(f.Lines, RawLine{Text: renderAssignment("", false, key, value, ""), NL: f.DefaultNL})
	return true, nil
}

func (f *DotenvFile) Unset(key string) (bool, error) {
	key = strings.TrimSpace(key)
	if err := ValidateKeyName(key); err != nil {
		return false, err
	}
	changed := false
	out := make([]RawLine, 0, len(f.Lines))
	for _, line := range f.Lines {
		assign, ok := parseAssignment(line.Text)
		if ok && assign.Key == key {
			changed = true
			continue
		}
		out = append(out, line)
	}
	if !changed {
		return false, nil
	}
	f.Lines = out
	return true, nil
}

func (f *DotenvFile) setInSection(key, value, section string) (bool, error) {
	start, end, ok := findSectionRange(f.Lines, section)
	if !ok {
		changed := f.appendSection(section, []string{renderAssignment("", false, key, value, "")})
		return changed, nil
	}
	// Update existing key within section (last-wins within the section).
	last := -1
	var lastAssign assignment
	for i := start + 1; i < end; i++ {
		assign, ok := parseAssignment(f.Lines[i].Text)
		if !ok {
			continue
		}
		if assign.Key == key {
			last = i
			lastAssign = assign
		}
	}
	if last >= 0 {
		line := renderAssignmentPreserveLayout(lastAssign, key, value, lastAssign.Comment)
		if f.Lines[last].Text == line {
			return false, nil
		}
		f.Lines[last].Text = line
		return true, nil
	}

	// Insert before trailing blank lines at end of section.
	insertAt := end
	for insertAt > start+1 && strings.TrimSpace(f.Lines[insertAt-1].Text) == "" {
		insertAt--
	}
	f.ensureLineHasNL(insertAt - 1)
	line := RawLine{Text: renderAssignment("", false, key, value, ""), NL: f.DefaultNL}
	f.Lines = append(f.Lines[:insertAt], append([]RawLine{line}, f.Lines[insertAt:]...)...)
	return true, nil
}

func (f *DotenvFile) appendSection(section string, payloadLines []string) bool {
	f.ensureAppendable()

	needsLeadingBlank := false
	for i := len(f.Lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(f.Lines[i].Text) == "" {
			continue
		}
		needsLeadingBlank = true
		break
	}
	if needsLeadingBlank {
		f.Lines = append(f.Lines, RawLine{Text: "", NL: f.DefaultNL})
	}
	f.Lines = append(f.Lines, RawLine{Text: canonicalDividerLine(), NL: f.DefaultNL})
	f.Lines = append(f.Lines, RawLine{Text: renderSectionHeader(section), NL: f.DefaultNL})
	for _, payload := range payloadLines {
		payload = strings.TrimRight(payload, "\r\n")
		f.Lines = append(f.Lines, RawLine{Text: payload, NL: f.DefaultNL})
	}
	return true
}

func (f *DotenvFile) ensureAppendable() {
	if len(f.Lines) == 0 {
		return
	}
	f.ensureLineHasNL(len(f.Lines) - 1)
}

func (f *DotenvFile) ensureLineHasNL(i int) {
	if i < 0 || i >= len(f.Lines) {
		return
	}
	if f.Lines[i].NL == "" {
		f.Lines[i].NL = f.DefaultNL
	}
}

func splitRawLines(data []byte) []RawLine {
	if len(data) == 0 {
		return nil
	}
	out := []RawLine{}
	start := 0
	for start < len(data) {
		idx := bytes.IndexByte(data[start:], '\n')
		if idx < 0 {
			out = append(out, RawLine{Text: string(data[start:]), NL: ""})
			break
		}
		idx += start
		line := data[start:idx]
		nl := "\n"
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
			nl = "\r\n"
		}
		out = append(out, RawLine{Text: string(line), NL: nl})
		start = idx + 1
	}
	return out
}

type assignment struct {
	Leading  string
	Export   bool
	LeftRaw  string
	Key      string
	ValueRaw string
	ValueWS  string
	Comment  string
}

func parseAssignment(line string) (assignment, bool) {
	if strings.TrimSpace(line) == "" {
		return assignment{}, false
	}
	trimLeft := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimLeft, "#") {
		return assignment{}, false
	}
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return assignment{}, false
	}
	left := line[:eq]
	right := line[eq+1:]

	leading := left[:len(left)-len(strings.TrimLeft(left, " \t"))]
	leftTrim := strings.TrimSpace(left)
	export := false
	keyPart := leftTrim
	if strings.HasPrefix(keyPart, "export ") || strings.HasPrefix(keyPart, "export\t") {
		export = true
		keyPart = strings.TrimSpace(keyPart[len("export"):])
	}
	key := strings.TrimSpace(keyPart)
	if key == "" {
		return assignment{}, false
	}

	valueRaw, comment := splitValueAndComment(right)
	valueWS := leadingWhitespace(valueRaw)
	return assignment{
		Leading:  leading,
		Export:   export,
		LeftRaw:  left,
		Key:      key,
		ValueRaw: valueRaw,
		ValueWS:  valueWS,
		Comment:  comment,
	}, true
}

func splitValueAndComment(right string) (string, string) {
	if right == "" {
		return "", ""
	}
	start := 0
	for start < len(right) && (right[start] == ' ' || right[start] == '\t') {
		start++
	}
	if start >= len(right) {
		return right, ""
	}
	if right[start] == '#' {
		// Treat the whole RHS as a comment (including any leading whitespace).
		return "", right
	}
	if right[start] == '\'' {
		end := strings.IndexByte(right[start+1:], '\'')
		if end < 0 {
			return right, ""
		}
		end = start + 1 + end
		rest := right[end+1:]
		ws := 0
		for ws < len(rest) && (rest[ws] == ' ' || rest[ws] == '\t') {
			ws++
		}
		if ws < len(rest) && rest[ws] == '#' {
			cstart := end + 1
			return right[:cstart], right[cstart:]
		}
		return right, ""
	}
	if right[start] == '"' {
		escaped := false
		end := -1
		for i := start + 1; i < len(right); i++ {
			ch := right[i]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return right, ""
		}
		rest := right[end+1:]
		ws := 0
		for ws < len(rest) && (rest[ws] == ' ' || rest[ws] == '\t') {
			ws++
		}
		if ws < len(rest) && rest[ws] == '#' {
			cstart := end + 1
			return right[:cstart], right[cstart:]
		}
		return right, ""
	}
	for i := start; i < len(right); i++ {
		if right[i] != '#' {
			continue
		}
		if i == start {
			return "", right
		}
		prev := right[i-1]
		if prev == ' ' || prev == '\t' {
			cstart := i - 1
			for cstart > start && (right[cstart-1] == ' ' || right[cstart-1] == '\t') {
				cstart--
			}
			return right[:cstart], right[cstart:]
		}
	}
	return right, ""
}

func renderAssignment(leading string, export bool, key, value, comment string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return strings.TrimRight(leading, " \t")
	}
	var b strings.Builder
	b.WriteString(leading)
	if export {
		b.WriteString("export ")
	}
	b.WriteString(key)
	b.WriteString("=")
	b.WriteString(strings.TrimSpace(value))
	b.WriteString(comment)
	return b.String()
}

func renderAssignmentPreserveLayout(existing assignment, key, value, comment string) string {
	if strings.TrimSpace(existing.LeftRaw) == "" {
		return renderAssignment(existing.Leading, existing.Export, key, value, comment)
	}
	var b strings.Builder
	b.WriteString(existing.LeftRaw)
	b.WriteString("=")
	b.WriteString(existing.ValueWS)
	b.WriteString(strings.TrimSpace(value))
	b.WriteString(comment)
	return b.String()
}

func leadingWhitespace(s string) string {
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

func resolveDotenvWriteTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !isTruthyEnv("SI_VAULT_ALLOW_SYMLINK_ENV_FILE") {
			return "", fmt.Errorf("refusing to write vault env file through symlink: %s (set SI_VAULT_ALLOW_SYMLINK_ENV_FILE=1 to override)", filepath.Clean(path))
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve vault env symlink %s: %w", filepath.Clean(path), err)
		}
		resolved = filepath.Clean(resolved)
		targetInfo, err := os.Stat(resolved)
		if err != nil {
			return "", err
		}
		if targetInfo.IsDir() {
			return "", fmt.Errorf("vault env symlink resolves to directory: %s", resolved)
		}
		return resolved, nil
	}
	return path, nil
}

func normalizeSectionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToLower(name)
}

func renderSectionHeader(name string) string {
	name = normalizeSectionName(name)
	return "# [" + name + "]"
}

func canonicalDividerLine() string {
	return "# ------------------------------------------------------------------------------"
}

func isDividerLine(line string) bool {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "#") {
		return false
	}
	trim = strings.TrimSpace(strings.TrimPrefix(trim, "#"))
	if len(trim) < 10 {
		return false
	}
	for _, ch := range trim {
		if ch != '-' {
			return false
		}
	}
	return true
}

func isSectionHeaderLine(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "#") {
		return "", false
	}
	trim = strings.TrimSpace(strings.TrimPrefix(trim, "#"))
	if !strings.HasPrefix(trim, "[") || !strings.HasSuffix(trim, "]") {
		return "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "["), "]"))
	if inner == "" {
		return "", false
	}
	return normalizeSectionName(inner), true
}

func findSectionRange(lines []RawLine, section string) (int, int, bool) {
	section = normalizeSectionName(section)
	if section == "" {
		return -1, -1, false
	}
	start := -1
	for i, line := range lines {
		name, ok := isSectionHeaderLine(line.Text)
		if !ok {
			continue
		}
		if name == section {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if _, ok := isSectionHeaderLine(lines[i].Text); ok {
			end = i
			// If the next section is in canonical form (divider line immediately
			// above the section header, possibly separated by blank lines), treat
			// that divider line as belonging to the next section.
			j := i - 1
			for j >= 0 && strings.TrimSpace(lines[j].Text) == "" {
				j--
			}
			if j >= 0 && isDividerLine(lines[j].Text) {
				end = j
			}
			break
		}
	}
	return start, end, true
}
