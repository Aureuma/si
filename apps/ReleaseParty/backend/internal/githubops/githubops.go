package githubops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
)

type ReleaseInfo struct {
	TagName string
	Name    string
	Body    string
	HTMLURL string
}

type CommitInfo struct {
	SHA     string
	Message string
	URL     string
	Author  string
}

func GetRelease(ctx context.Context, client *github.Client, owner, repo, tag string) (ReleaseInfo, error) {
	rel, _, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		return ReleaseInfo{}, err
	}
	return ReleaseInfo{
		TagName: rel.GetTagName(),
		Name:    rel.GetName(),
		Body:    rel.GetBody(),
		HTMLURL: rel.GetHTMLURL(),
	}, nil
}

func CompareCommits(ctx context.Context, client *github.Client, owner, repo, base, head string) ([]CommitInfo, error) {
	cmp, _, err := client.Repositories.CompareCommits(ctx, owner, repo, base, head, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, err
	}
	out := make([]CommitInfo, 0, len(cmp.Commits))
	for _, c := range cmp.Commits {
		msg := ""
		if c.Commit != nil {
			msg = c.Commit.GetMessage()
		}
		author := ""
		if c.Author != nil {
			author = c.Author.GetLogin()
		}
		out = append(out, CommitInfo{
			SHA:     shortSHA(c.GetSHA()),
			Message: msg,
			URL:     c.GetHTMLURL(),
			Author:  author,
		})
	}
	return out, nil
}

func EnsureBranch(ctx context.Context, client *github.Client, owner, repo, branch, baseBranch string) error {
	// Check branch exists.
	_, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err == nil {
		return nil
	}
	// Create from base.
	baseRef, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("base branch %s not found: %w", baseBranch, err)
	}
	newRef := &github.Reference{
		Ref: github.String("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: baseRef.Object.SHA,
		},
	}
	_, _, err = client.Git.CreateRef(ctx, owner, repo, newRef)
	return err
}

func UpsertFile(ctx context.Context, client *github.Client, owner, repo, branch, path, content, message string) error {
	// Get existing sha if present.
	var sha *string
	file, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: branch})
	if err == nil && file != nil {
		s := file.GetSHA()
		sha = &s
	} else if resp != nil && resp.StatusCode != 404 {
		return err
	}

	opts := &github.RepositoryContentFileOptions{
		Message: github.String(message),
		Content: []byte(content),
		Branch:  github.String(branch),
		SHA:     sha,
	}
	if sha == nil {
		_, _, err = client.Repositories.CreateFile(ctx, owner, repo, path, opts)
		return err
	}
	_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, path, opts)
	return err
}

func CreatePullRequest(ctx context.Context, client *github.Client, owner, repo, headBranch, baseBranch, title, body string) (string, error) {
	pr, _, err := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(headBranch),
		Base:  github.String(baseBranch),
		Body:  github.String(body),
	})
	if err != nil {
		return "", err
	}
	return pr.GetHTMLURL(), nil
}

func BuildBranchName(project string, tag string, now time.Time) string {
	ts := now.UTC().Format("20060102150405")
	return fmt.Sprintf("release-party/%s-%s-%s", slugify(project, "project"), slugify(tag, "release"), ts)
}

func BuildCommitMessage(tag string) string {
	if strings.TrimSpace(tag) == "" {
		tag = "release"
	}
	return "chore: add release post for " + strings.TrimSpace(tag)
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
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
