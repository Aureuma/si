package githubapp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v66/github"
)

type App struct {
	AppID     int64
	Slug      string
	Secret    string
	PrivateKeyPEM []byte
	BaseURL   string
}

func New(appID int64, slug, webhookSecret, privateKeyPEM, baseURL string) (*App, error) {
	keyBytes := []byte(privateKeyPEM)
	if len(bytesTrimSpace(keyBytes)) == 0 {
		return nil, fmt.Errorf("empty private key PEM")
	}
	return &App{
		AppID:   appID,
		Slug:   slug,
		Secret: webhookSecret,
		PrivateKeyPEM: keyBytes,
		BaseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

func (a *App) AppClient() (*github.Client, error) {
	tr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, a.AppID, a.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return github.NewClient(&http.Client{Transport: tr}), nil
}

func (a *App) InstallationClient(installationID int64) (*github.Client, error) {
	tr, err := ghinstallation.New(http.DefaultTransport, a.AppID, installationID, a.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return github.NewClient(&http.Client{Transport: tr}), nil
}

func (a *App) InstallURL() string {
	// GitHub App installation URL format:
	// https://github.com/apps/<slug>/installations/new
	return fmt.Sprintf("https://github.com/apps/%s/installations/new", a.Slug)
}

func bytesTrimSpace(b []byte) []byte {
	i := 0
	j := len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
