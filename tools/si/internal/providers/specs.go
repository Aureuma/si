package providers

// Specs centralize provider defaults so new providers are consistent and existing
// clients don't duplicate constants.

type ID string

const (
	Cloudflare   ID = "cloudflare"
	GitHub       ID = "github"
	GooglePlaces ID = "google_places"
	YouTube      ID = "youtube"
)

type Spec struct {
	BaseURL       string
	UploadBaseURL string // Optional (e.g. YouTube uploads).
	UserAgent     string
	Accept        string

	// Candidate request-id headers used for correlation/debugging.
	RequestIDHeaders []string

	// Provider-specific headers that should be set by default for every request.
	// Callers may still override per request.
	DefaultHeaders map[string]string
}

var Specs = map[ID]Spec{
	Cloudflare: {
		BaseURL:          "https://api.cloudflare.com/client/v4",
		UserAgent:        "si-cloudflare/1.0",
		Accept:           "application/json",
		RequestIDHeaders: []string{"CF-Ray", "X-Request-ID"},
	},
	GitHub: {
		BaseURL:   "https://api.github.com",
		UserAgent: "si-github/1.0",
		Accept:    "application/vnd.github+json",
		RequestIDHeaders: []string{
			"X-GitHub-Request-Id",
		},
		DefaultHeaders: map[string]string{
			"X-GitHub-Api-Version": "2022-11-28",
		},
	},
	GooglePlaces: {
		BaseURL:          "https://places.googleapis.com",
		UserAgent:        "si-google-places/1.0",
		Accept:           "application/json",
		RequestIDHeaders: []string{"X-Request-Id", "X-Google-Request-Id"},
	},
	YouTube: {
		BaseURL:          "https://www.googleapis.com",
		UploadBaseURL:    "https://www.googleapis.com/upload",
		UserAgent:        "si-youtube/1.0",
		Accept:           "application/json",
		RequestIDHeaders: []string{"X-Google-Request-Id", "X-Request-Id"},
	},
}

