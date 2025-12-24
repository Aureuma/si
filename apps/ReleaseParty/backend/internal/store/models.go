package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Installation struct {
	ID             int64
	InstallationID int64
	AccountLogin   string
	AccountType    string
	CreatedAt      time.Time
}

type Project struct {
	ID               int64
	InstallationID   int64
	RepoFullName     string
	BlogRepoFullName string
	DefaultBranch    string
	PathTemplate     string
	FrontMatterFormat string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (s *Store) UpsertInstallation(ctx context.Context, installationID int64, accountLogin, accountType string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO installations (installation_id, account_login, account_type, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(installation_id) DO UPDATE SET
			account_login=excluded.account_login,
			account_type=excluded.account_type
	`, installationID, accountLogin, accountType, now)
	return err
}

func (s *Store) DeleteInstallation(ctx context.Context, installationID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM installations WHERE installation_id = ?`, installationID)
	return err
}

func (s *Store) UpsertProject(ctx context.Context, p Project) (Project, error) {
	if p.InstallationID == 0 || p.RepoFullName == "" || p.BlogRepoFullName == "" {
		return Project{}, fmt.Errorf("invalid project")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (
			installation_id, repo_full_name, blog_repo_full_name, default_branch, path_template, front_matter_format, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(installation_id, repo_full_name) DO UPDATE SET
			blog_repo_full_name=excluded.blog_repo_full_name,
			default_branch=excluded.default_branch,
			path_template=excluded.path_template,
			front_matter_format=excluded.front_matter_format,
			updated_at=excluded.updated_at
	`, p.InstallationID, p.RepoFullName, p.BlogRepoFullName, p.DefaultBranch, p.PathTemplate, p.FrontMatterFormat, now, now)
	if err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx, p.InstallationID, p.RepoFullName)
}

func (s *Store) GetProject(ctx context.Context, installationID int64, repoFullName string) (Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, installation_id, repo_full_name, blog_repo_full_name, default_branch, path_template, front_matter_format, created_at, updated_at
		FROM projects
		WHERE installation_id = ? AND repo_full_name = ?
	`, installationID, repoFullName)
	var p Project
	var created, updated string
	if err := row.Scan(&p.ID, &p.InstallationID, &p.RepoFullName, &p.BlogRepoFullName, &p.DefaultBranch, &p.PathTemplate, &p.FrontMatterFormat, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return Project{}, sql.ErrNoRows
		}
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

