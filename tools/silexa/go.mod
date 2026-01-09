module silexa/tools/silexa

go 1.22.12

require (
	github.com/docker/docker v26.1.4+incompatible
	github.com/docker/go-connections v0.5.0
	silexa/agents/shared v0.0.0
)

replace silexa/agents/shared => ../../agents/shared
