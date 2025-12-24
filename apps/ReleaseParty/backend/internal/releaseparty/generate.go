package releaseparty

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ReleaseContext struct {
	RepoFullName string
	ProjectName  string
	ReleaseTag   string
	BaseTag      string

	ReleaseName string
	ReleaseBody string
	ReleaseURL  string

	Commits []Commit
}

type Commit struct {
	SHA     string
	Message string
	URL     string
	Author  string
}

type GeneratedPost struct {
	Title   string
	Summary string
	Tags    []string
	Content string
	Path    string
}

func GenerateMarkdown(ctx ReleaseContext, cfg RepoConfig, now time.Time) (GeneratedPost, error) {
	title := ctx.ReleaseName
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("%s %s", ctx.ProjectName, ctx.ReleaseTag)
	}

	highlights := summarizeCommits(ctx.Commits, 12)
	summary := fmt.Sprintf("Release %s for %s.", ctx.ReleaseTag, ctx.ProjectName)

	var b strings.Builder
	frontMatter := buildYAMLFrontMatter(map[string]any{
		"title":   title,
		"date":    now.UTC().Format(time.RFC3339),
		"summary": summary,
		"tags":    []string{"release", "automation"},
	})
	b.WriteString(frontMatter)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# %s\n\n", title))
	b.WriteString(fmt.Sprintf("_Tag:_ `%s`  \n", ctx.ReleaseTag))
	if ctx.BaseTag != "" {
		b.WriteString(fmt.Sprintf("_From:_ `%s` → `%s`  \n", ctx.BaseTag, ctx.ReleaseTag))
	}
	if ctx.ReleaseURL != "" {
		b.WriteString(fmt.Sprintf("_Release:_ %s\n\n", ctx.ReleaseURL))
	}
	if strings.TrimSpace(ctx.ReleaseBody) != "" {
		b.WriteString("## Release notes\n\n")
		b.WriteString(strings.TrimSpace(ctx.ReleaseBody) + "\n\n")
	}

	b.WriteString("## Highlights\n\n")
	if len(highlights) == 0 {
		b.WriteString("- (No highlights extracted)\n\n")
	} else {
		for _, h := range highlights {
			b.WriteString("- " + h + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Changelog\n\n")
	b.WriteString("### Commits\n\n")
	for _, c := range ctx.Commits {
		msg := firstLine(c.Message)
		if c.URL != "" {
			b.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", c.SHA, msg, c.URL))
		} else {
			b.WriteString(fmt.Sprintf("- %s %s\n", c.SHA, msg))
		}
	}
	b.WriteString("\n")
	b.WriteString("## Call to action\n\n")
	b.WriteString("If you try this release, we’d love feedback. Open an issue or discussion and tell us what broke, what delighted you, and what you want next.\n")

	path := RenderPath(cfg.PathTemplate, ctx.ProjectName, ctx.ReleaseTag, title, now)
	return GeneratedPost{
		Title:   title,
		Summary: summary,
		Tags:    []string{"release", "automation"},
		Content: b.String(),
		Path:    path,
	}, nil
}

func summarizeCommits(commits []Commit, max int) []string {
	type bucket struct {
		key   string
		items []Commit
	}
	seen := map[string]*bucket{}
	order := []string{}
	for _, c := range commits {
		msg := strings.ToLower(firstLine(c.Message))
		key := "other"
		switch {
		case strings.HasPrefix(msg, "feat"):
			key = "features"
		case strings.HasPrefix(msg, "fix"):
			key = "fixes"
		case strings.HasPrefix(msg, "docs"):
			key = "docs"
		case strings.HasPrefix(msg, "chore"), strings.HasPrefix(msg, "refactor"):
			key = "maintenance"
		}
		b := seen[key]
		if b == nil {
			b = &bucket{key: key}
			seen[key] = b
			order = append(order, key)
		}
		b.items = append(b.items, c)
	}
	sort.SliceStable(order, func(i, j int) bool {
		pi := bucketPriority(order[i])
		pj := bucketPriority(order[j])
		return pi < pj
	})
	lines := []string{}
	for _, k := range order {
		b := seen[k]
		if b == nil {
			continue
		}
		for _, c := range b.items {
			msg := firstLine(c.Message)
			lines = append(lines, msg)
			if len(lines) >= max {
				return lines
			}
		}
	}
	return lines
}

func bucketPriority(k string) int {
	switch k {
	case "features":
		return 0
	case "fixes":
		return 1
	case "docs":
		return 2
	case "maintenance":
		return 3
	default:
		return 9
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func buildYAMLFrontMatter(m map[string]any) string {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		switch vv := v.(type) {
		case string:
			b.WriteString(fmt.Sprintf("%s: %q\n", k, vv))
		case []string:
			b.WriteString(fmt.Sprintf("%s:\n", k))
			for _, item := range vv {
				b.WriteString(fmt.Sprintf("  - %q\n", item))
			}
		default:
			b.WriteString(fmt.Sprintf("%s: %v\n", k, vv))
		}
	}
	b.WriteString("---\n")
	return b.String()
}
