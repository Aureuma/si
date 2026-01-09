package beam

type Request struct {
	TaskID int    `json:"task_id"`
	Kind   string `json:"kind"`
	Dyad   string `json:"dyad"`
	Actor  string `json:"actor"`
	Critic string `json:"critic"`
}

type CodexLoginCheck struct {
	Dyad  string `json:"dyad"`
	Actor string `json:"actor"`
}

type CodexLoginStatus struct {
	LoggedIn bool   `json:"logged_in"`
	Raw      string `json:"raw,omitempty"`
}

type CodexLoginStart struct {
	AuthURL       string `json:"auth_url"`
	Port          int    `json:"port"`
	ForwardPort   int    `json:"forward_port"`
	HostPort      string `json:"host_port"`
	Container     string `json:"container"`
}

type CodexLoginRequest struct {
	Dyad        string `json:"dyad"`
	Actor       string `json:"actor"`
	Port        int    `json:"port"`
	ForwardPort int    `json:"forward_port"`
}

type SocatForwarderRequest struct {
	Dyad       string `json:"dyad"`
	Actor      string `json:"actor"`
	Name       string `json:"name"`
	ListenPort int    `json:"listen_port"`
	TargetPort int    `json:"target_port"`
}

type SocatForwarderStop struct {
	Dyad  string `json:"dyad"`
	Actor string `json:"actor"`
	Name  string `json:"name"`
}

type TelegramMessage struct {
	Message string `json:"message"`
}

type DyadBootstrapRequest struct {
	Dyad       string `json:"dyad"`
	Role       string `json:"role"`
	Department string `json:"department"`
	Actor      string `json:"actor,omitempty"`
	Critic     string `json:"critic,omitempty"`
}

type DyadBootstrapResult struct {
	ActorContainer  string `json:"actor_container"`
	CriticContainer string `json:"critic_container"`
}

const (
	KindCodexLogin         = "beam.codex_login"
	KindDyadBootstrap      = "beam.dyad_bootstrap"
	KindCodexAccountReset  = "beam.codex_account_reset"
)

type CodexResetRequest struct {
	Dyad    string   `json:"dyad"`
	Targets []string `json:"targets,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

type CodexResetResult struct {
	Targets []string `json:"targets"`
	Paths   []string `json:"paths"`
}

type DyadTaskCheck struct {
	Dyad string `json:"dyad"`
	Kind string `json:"kind"`
}
