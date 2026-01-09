module silexa/dashboard

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	silexa/agents/shared v0.0.0
)

replace silexa/agents/shared => ../shared
