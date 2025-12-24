package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/go-github/v66/github"

	"releaseparty/internal/config"
	"releaseparty/internal/githubapp"
	"releaseparty/internal/githubops"
	"releaseparty/internal/releaseparty"
	"releaseparty/internal/store"
)

type Server struct {
	cfg   config.Config
	app   *githubapp.App
	store *store.Store
	log   *log.Logger
}

func New(cfg config.Config, app *githubapp.App, st *store.Store, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(log.Writer(), "releaseparty ", log.LstdFlags|log.LUTC)
	}
	return &Server{cfg: cfg, app: app, store: st, log: logger}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/install/url", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"url": s.app.InstallURL()})
		})
		r.Post("/github/webhooks", s.handleGitHubWebhook)
	})

	return r
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := s.app.VerifyWebhook(r)
	if err != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	eventType := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery != "" {
		s.log.Printf("webhook delivery=%s event=%s", delivery, eventType)
	}

	event, err := github.ParseWebHook(eventType, body)
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	switch e := event.(type) {
	case *github.InstallationEvent:
		s.handleInstallationEvent(r.Context(), e)
	case *github.InstallationRepositoriesEvent:
		s.handleInstallationReposEvent(r.Context(), e)
	case *github.ReleaseEvent:
		s.handleReleaseEvent(r.Context(), e)
	default:
		// ignore
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleInstallationEvent(ctx context.Context, e *github.InstallationEvent) {
	instID := e.GetInstallation().GetID()
	action := strings.ToLower(e.GetAction())
	account := e.GetInstallation().GetAccount()
	login := ""
	typ := ""
	if account != nil {
		login = account.GetLogin()
		typ = account.GetType()
	}

	switch action {
	case "created", "new_permissions_accepted":
		if instID == 0 || login == "" {
			return
		}
		if err := s.store.UpsertInstallation(ctx, instID, login, typ); err != nil {
			s.log.Printf("installation upsert error: %v", err)
		}
	case "deleted":
		if instID == 0 {
			return
		}
		_ = s.store.DeleteInstallation(ctx, instID)
	}
}

func (s *Server) handleInstallationReposEvent(ctx context.Context, e *github.InstallationRepositoriesEvent) {
	// MVP: just keep installation record fresh and rely on manual project config.
	instID := e.GetInstallation().GetID()
	if instID == 0 {
		return
	}
	account := e.GetInstallation().GetAccount()
	if account != nil {
		_ = s.store.UpsertInstallation(ctx, instID, account.GetLogin(), account.GetType())
	}
}

func (s *Server) handleReleaseEvent(ctx context.Context, e *github.ReleaseEvent) {
	action := strings.ToLower(e.GetAction())
	if action != "published" && action != "released" {
		return
	}
	instID := e.GetInstallation().GetID()
	repo := e.GetRepo()
	if instID == 0 || repo == nil {
		return
	}

	fullName := repo.GetFullName()
	owner := repo.GetOwner().GetLogin()
	name := repo.GetName()
	tag := e.GetRelease().GetTagName()
	if owner == "" || name == "" || tag == "" {
		return
	}

	now := time.Now().UTC()

	client, err := s.app.InstallationClient(instID)
	if err != nil {
		s.log.Printf("installation client error: %v", err)
		return
	}

	baseTag, err := findBaseTag(ctx, client, owner, name, tag, repo.GetDefaultBranch())
	if err != nil {
		s.log.Printf("base tag lookup error: %v", err)
	}

	projectCfg, err := s.loadProjectConfig(ctx, instID, fullName)
	if err != nil {
		s.log.Printf("project config load error repo=%s: %v", fullName, err)
		return
	}

	rel, err := githubops.GetRelease(ctx, client, owner, name, tag)
	if err != nil {
		s.log.Printf("get release error: %v", err)
		return
	}

	commits, err := githubops.CompareCommits(ctx, client, owner, name, baseTag, tag)
	if err != nil {
		s.log.Printf("compare commits error base=%s head=%s: %v", baseTag, tag, err)
		commits = nil
	}

	ctx2 := releaseparty.ReleaseContext{
		RepoFullName: fullName,
		ProjectName:  repo.GetName(),
		ReleaseTag:   tag,
		BaseTag:      baseTag,
		ReleaseName:  rel.Name,
		ReleaseBody:  rel.Body,
		ReleaseURL:   rel.HTMLURL,
		Commits:      toRPCommits(commits),
	}

	gen, err := releaseparty.GenerateMarkdown(ctx2, projectCfg, now)
	if err != nil {
		s.log.Printf("generate markdown error: %v", err)
		return
	}

	blogOwner, blogRepo, err := splitRepo(projectCfg.BlogRepo)
	if err != nil {
		s.log.Printf("invalid blog repo: %v", err)
		return
	}
	branch := githubops.BuildBranchName(repo.GetName(), tag, now)
	if err := githubops.EnsureBranch(ctx, client, blogOwner, blogRepo, branch, projectCfg.BaseBranch); err != nil {
		s.log.Printf("ensure branch error: %v", err)
		return
	}
	commitMsg := githubops.BuildCommitMessage(tag)
	if err := githubops.UpsertFile(ctx, client, blogOwner, blogRepo, branch, gen.Path, gen.Content, commitMsg); err != nil {
		s.log.Printf("upsert file error: %v", err)
		return
	}
	prTitle := "Add release notes for " + tag
	prBody := "Automated by ReleaseParty Acolyte.\n\nSource: " + fullName
	prURL, err := githubops.CreatePullRequest(ctx, client, blogOwner, blogRepo, branch, projectCfg.BaseBranch, prTitle, prBody)
	if err != nil {
		s.log.Printf("create pr error: %v", err)
		return
	}
	s.log.Printf("created PR %s for %s@%s", prURL, fullName, tag)
}

func toRPCommits(commits []githubops.CommitInfo) []releaseparty.Commit {
	out := make([]releaseparty.Commit, 0, len(commits))
	for _, c := range commits {
		out = append(out, releaseparty.Commit{
			SHA:     c.SHA,
			Message: c.Message,
			URL:     c.URL,
			Author:  c.Author,
		})
	}
	return out
}

func (s *Server) loadProjectConfig(ctx context.Context, installationID int64, repoFullName string) (releaseparty.RepoConfig, error) {
	// 1) DB project config if present
	p, err := s.store.GetProject(ctx, installationID, repoFullName)
	if err == nil {
		cfg := releaseparty.DefaultRepoConfig()
		cfg.BlogRepo = p.BlogRepoFullName
		cfg.PathTemplate = p.PathTemplate
		cfg.BaseBranch = p.DefaultBranch
		cfg.FrontMatterFormat = p.FrontMatterFormat
		if cfg.BlogRepo == "" {
			cfg.BlogRepo = repoFullName
		}
		return cfg, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return releaseparty.RepoConfig{}, err
	}
	// 2) fallback: same repo
	cfg := releaseparty.DefaultRepoConfig()
	cfg.BlogRepo = repoFullName
	return cfg, nil
}

func findBaseTag(ctx context.Context, client *github.Client, owner, repo, headTag, defaultBranch string) (string, error) {
	// Try releases first: ordered by created date descending.
	releases, _, err := client.Repositories.ListReleases(ctx, owner, repo, &github.ListOptions{PerPage: 20})
	if err == nil && len(releases) > 0 {
		for i := range releases {
			if releases[i].GetTagName() == headTag {
				// Next release in the list is the previous release (older).
				if i+1 < len(releases) {
					return releases[i+1].GetTagName(), nil
				}
				break
			}
		}
	}

	// Fallback to tags list.
	tags, _, err := client.Repositories.ListTags(ctx, owner, repo, &github.ListOptions{PerPage: 50})
	if err != nil {
		return defaultBranch, nil
	}
	for i := range tags {
		if tags[i].GetName() == headTag {
			if i+1 < len(tags) {
				return tags[i+1].GetName(), nil
			}
			break
		}
	}
	return defaultBranch, nil
}

func splitRepo(full string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(full), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("repo must be owner/name")
	}
	return parts[0], parts[1], nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

