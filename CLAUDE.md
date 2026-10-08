# Working in this repository

A stdio MCP server over the Apps Script and Drive REST APIs, for one person's
Google account. README.md says what it does and how it is set up; this file
says how to change it.

**How to write Go here** follows `/opt/projects/dropin-forms`, which follows
ardanlabs/kronk by way of eumaeus. Its directives are section 0 below and
`.claude/skills/writing-go/SKILL.md`, copied unchanged. The web-server skill
and the Ardan layering were not imported: there is no server, no database and
no deploy, so `internal/` with one package per concern is the whole shape.

## 0. Touching Go: load the skill, then run the check

**Before reading, writing or modifying any `.go` file**, load
`.claude/skills/writing-go/SKILL.md`.

**After modifying any `.go` file**, on the package you changed:

```sh
make go-check PKG=./internal/inventory
```

`gofmt -s -w`, `go vet`, `staticcheck`, `go build ./...` and the package's
tests. All must pass. **Fix the code; do not suppress the diagnostic.**
`make test` is the whole module plus govulncheck. `make dev-tools` installs
staticcheck and gopls, pinned.

Verify an API from the live toolchain or the docs rather than from recall. For
Google's endpoints that means the generated client in the module cache
(`go doc google.golang.org/api/script/v1`, after a `go get` you then tidy
away): it is the reference for every field name in `internal/gas/types.go`.

## 1. Navigation

A diagnostic is an address (`file.go:line:col`); read the region it names
rather than searching for it. Find a Go declaration with `make sym NAME=…` or
`make outline FILE=…`, which are gopls, not with grep. grep is right for
everything that is not a Go symbol.

## 2. Where things live

```
cmd/google-scripts-mcp/   main: login, whoami, and the server (the default)
internal/auth/            sign-in, the token file, refresh
internal/gas/             the REST client: types, calls, Google's errors with a hint
internal/gas/gastest/     a fake of those endpoints, for tests
internal/inventory/       putting the three sources together; the remembered-ids file
internal/source/          pulling into a directory, and the record that protects edits
internal/tools/           the MCP tools: names, descriptions, argument schemas
```

`gas` knows the wire and nothing about MCP. `tools` knows MCP and calls the
others; it decides nothing a test of `inventory` or `source` could not check.

## 3. Rules that are about this repository

- **No google.golang.org/api.** See the package comment on `internal/gas`.
  Eight GETs do not justify thirty modules.
- **Nothing on Google's side changes in this phase.** The scopes in
  `internal/auth` are read-only on purpose. A write tool (push, delete a
  deployment, trash a script) adds its scope there, and adding one means the
  person signs in again, which is the moment they should see it.
- **A pull never overwrites an edit.** `.gas-pull.json` holds the hash of
  every file as written; keep that check when changing `source`, and give a
  push the same respect for edits made in the web editor since the pull
  (compare the project's update time with `RemoteUpdated`).
- **Stdout is the protocol.** Nothing in the server path may print to it.
- **Pulled code and credentials never enter this repository**; `.gitignore`
  covers both. It may be public.

## House style

Comments explain **why**, at length, in prose, including what was tried and
rejected. Error sentences that reach a person say what to do about it and
never name a Go package. Everything in git is a pull request on a branch;
nothing is committed to `main` directly.
