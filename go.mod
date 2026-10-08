module github.com/jroedel/google-scripts-mcp

// One directive and no separate toolchain line, with the patch version
// pinned: the rule in .claude/skills/writing-go, and the reasons are there.
// Raise it when a Go patch release fixes an advisory govulncheck reports.
go 1.26.8

require (
	github.com/modelcontextprotocol/go-sdk v1.8.0
	golang.org/x/oauth2 v0.37.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	golang.org/x/vuln v1.8.0 // indirect
)

tool golang.org/x/vuln/cmd/govulncheck
