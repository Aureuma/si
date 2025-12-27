package state

import "time"

type Heartbeat struct {
	Dyad       string    `json:"dyad"`
	Role       string    `json:"role"`
	Department string    `json:"department"`
	Actor      string    `json:"actor"`
	Critic     string    `json:"critic"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	When       time.Time `json:"when"`
}

type HumanTask struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Commands    string     `json:"commands"`
	URL         string     `json:"url"`
	Timeout     string     `json:"timeout"`
	RequestedBy string     `json:"requested_by"`
	Notes       string     `json:"notes"`
	ChatID      *int64     `json:"chat_id,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Feedback struct {
	ID        int       `json:"id"`
	Source    string    `json:"source"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Context   string    `json:"context"`
	CreatedAt time.Time `json:"created_at"`
}

type AccessRequest struct {
	ID         int        `json:"id"`
	Requester  string     `json:"requester"`
	Department string     `json:"department"`
	Resource   string     `json:"resource"`
	Action     string     `json:"action"`
	Reason     string     `json:"reason"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Notes      string     `json:"notes,omitempty"`
}

type Metric struct {
	ID         int       `json:"id"`
	Dyad       string    `json:"dyad"`
	Department string    `json:"department"`
	Name       string    `json:"name"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	RecordedBy string    `json:"recorded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type DyadTask struct {
	ID               int       `json:"id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Kind             string    `json:"kind"`
	Status           string    `json:"status"`
	Priority         string    `json:"priority"`
	Dyad             string    `json:"dyad"`
	Actor            string    `json:"actor"`
	Critic           string    `json:"critic"`
	RequestedBy      string    `json:"requested_by"`
	Notes            string    `json:"notes"`
	Link             string    `json:"link"`
	TelegramMessageID int      `json:"telegram_message_id,omitempty"`
	ClaimedBy        string    `json:"claimed_by"`
	ClaimedAt        time.Time `json:"claimed_at"`
	HeartbeatAt      time.Time `json:"heartbeat_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DyadRecord struct {
	Dyad          string    `json:"dyad"`
	Department    string    `json:"department,omitempty"`
	Role          string    `json:"role,omitempty"`
	Team          string    `json:"team,omitempty"`
	Assignment    string    `json:"assignment,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Actor         string    `json:"actor,omitempty"`
	Critic        string    `json:"critic,omitempty"`
	Status        string    `json:"status,omitempty"`
	Message       string    `json:"message,omitempty"`
	Available     bool      `json:"available"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type DyadUpdate struct {
	Dyad       string   `json:"dyad"`
	Department string   `json:"department,omitempty"`
	Role       string   `json:"role,omitempty"`
	Team       string   `json:"team,omitempty"`
	Assignment string   `json:"assignment,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Actor      string   `json:"actor,omitempty"`
	Critic     string   `json:"critic,omitempty"`
	Status     string   `json:"status,omitempty"`
	Message    string   `json:"message,omitempty"`
	Available  *bool    `json:"available,omitempty"`
}

type DyadSnapshot struct {
	Dyad                string    `json:"dyad"`
	Department          string    `json:"department,omitempty"`
	Role                string    `json:"role,omitempty"`
	Team                string    `json:"team,omitempty"`
	Assignment          string    `json:"assignment,omitempty"`
	Tags                []string  `json:"tags,omitempty"`
	Actor               string    `json:"actor,omitempty"`
	Critic              string    `json:"critic,omitempty"`
	Status              string    `json:"status,omitempty"`
	Message             string    `json:"message,omitempty"`
	Available           bool      `json:"available"`
	State               string    `json:"state"`
	LastHeartbeat       time.Time `json:"last_heartbeat,omitempty"`
	LastHeartbeatAgeSec int64     `json:"last_heartbeat_age_sec,omitempty"`
	UpdatedAt           time.Time `json:"updated_at,omitempty"`
}

type State struct {
	Beats          []Heartbeat
	Tasks          []HumanTask
	NextTaskID     int
	Feedbacks      []Feedback
	NextFeedbackID int
	Access         []AccessRequest
	NextAccessID   int
	Metrics        []Metric
	NextMetricID   int
	DyadTasks      []DyadTask
	NextDyadTaskID int
	Dyads          []DyadRecord
	DyadDigestMessageID int
	StartedAt      time.Time
}

type ClaimResult struct {
	Task DyadTask `json:"task"`
	Code int      `json:"code"`
	Found bool    `json:"found"`
}

type UpdateResult struct {
	Task  DyadTask `json:"task"`
	Found bool     `json:"found"`
}

type ResolveResult struct {
	Request AccessRequest `json:"request"`
	Found   bool          `json:"found"`
}

type DyadTaskClaim struct {
	ID     int    `json:"id"`
	Dyad   string `json:"dyad"`
	Critic string `json:"critic"`
}

type AccessResolve struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	By     string `json:"by"`
	Notes  string `json:"notes"`
}
