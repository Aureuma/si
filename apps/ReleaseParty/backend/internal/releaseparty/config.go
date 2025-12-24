package releaseparty

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RepoConfig can be stored in DB and optionally overridden by a file in the repo (.releaseparty.yaml).
type RepoConfig struct {
	// Destination repo where the blog post should be opened as PR (often a docs/blog repo).
	BlogRepo string `yaml:"blog_repo" json:"blog_repo"`
	// Output path template, supports {date}, {project_slug}, {release_tag}, {title_slug}.
	PathTemplate string `yaml:"path_template" json:"path_template"`
	// Base branch in the destination repo.
	BaseBranch string `yaml:"base_branch" json:"base_branch"`
	// Front matter format: "yaml" or "toml" (MVP supports yaml).
	FrontMatterFormat string `yaml:"front_matter_format" json:"front_matter_format"`
}

func DefaultRepoConfig() RepoConfig {
	return RepoConfig{
		PathTemplate:      "posts/{date}-{release_tag}.md",
		BaseBranch:        "main",
		FrontMatterFormat: "yaml",
	}
}

func ParseRepoConfigYAML(b []byte) (RepoConfig, error) {
	cfg := DefaultRepoConfig()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return RepoConfig{}, err
	}
	cfg.PathTemplate = strings.TrimSpace(cfg.PathTemplate)
	cfg.BlogRepo = strings.TrimSpace(cfg.BlogRepo)
	cfg.BaseBranch = strings.TrimSpace(cfg.BaseBranch)
	cfg.FrontMatterFormat = strings.ToLower(strings.TrimSpace(cfg.FrontMatterFormat))
	if cfg.PathTemplate == "" {
		cfg.PathTemplate = DefaultRepoConfig().PathTemplate
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.FrontMatterFormat == "" {
		cfg.FrontMatterFormat = "yaml"
	}
	if cfg.FrontMatterFormat != "yaml" && cfg.FrontMatterFormat != "toml" {
		return RepoConfig{}, fmt.Errorf("unsupported front_matter_format: %s", cfg.FrontMatterFormat)
	}
	return cfg, nil
}

func RenderPath(tpl string, projectName string, releaseTag string, title string, now time.Time) string {
	s := tpl
	s = strings.ReplaceAll(s, "{date}", now.UTC().Format("2006-01-02"))
	s = strings.ReplaceAll(s, "{project_slug}", slugify(projectName, "project"))
	s = strings.ReplaceAll(s, "{release_tag}", slugify(releaseTag, "release"))
	s = strings.ReplaceAll(s, "{title_slug}", slugify(title, "post"))
	return s
}

func slugify(value string, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	out := make([]rune, 0, len(v))
	lastDash := false
	for _, r := range v {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			out = append(out, r)
			lastDash = false
			continue
		}
		if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	s := strings.Trim(string(out), "-")
	if s == "" {
		return fallback
	}
	return s
}

