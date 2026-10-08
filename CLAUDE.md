# Working in this repository

A stdio MCP server over the Apps Script and Drive REST APIs, for one person's
Google account: it takes stock of their Apps Script projects (which exist,
which still run, what starts them, what fails) and pulls the code into a local
directory to fix. README.md says what it does and how it is set up; this file
says how to change it.

The rules below were imported from `/opt/projects/stewards`, which took them
from mass-intentions and dropin-forms. Where a rule is about this repository
rather than about Go, it is marked **Here:**. Rules from stewards that have
nothing to act on yet -- the database conventions, the JavaScript tests --
are kept, in the form "the day this lands", so that nobody has to go back to
stewards to find them.

**How to write Go here** is section 0 below and
`.claude/skills/writing-go/SKILL.md`, following
[ardanlabs/kronk](https://github.com/ardanlabs/kronk) by way of
`/opt/projects/dropin-forms` and `/opt/projects/stewards`.

**How to move around this repository** is sections 1–5. They close loops that
are a property of Go and of agents rather than of any one repository: a
compiler diagnostic already knows where the problem is, and gopls already knows
where a symbol is declared. When one of them stops paying here, delete it
rather than obey it out of habit.

**Who does what** is section 6, and it is the part that is enforced rather
than asked for.

## 0. Touching Go: load the skill, then run the check

**Before reading, writing or modifying any `.go` file**, load
`.claude/skills/writing-go/SKILL.md` — the modern-Go rules for the version in
`go.mod`. This is kronk's mandatory-skill rule and it is the one directive here
that is not negotiable.

**After modifying any `.go` file**, on the package you changed:

```sh
make go-check PKG=./internal/inventory
```

which is `gofmt -s -w`, `go vet`, `staticcheck`, `go build ./...` and the
package's tests. All must pass. **Fix the code; do not suppress the
diagnostic.**

One package rather than `./...`, deliberately. This module is new enough that
`staticcheck ./...` is clean; keep it that way and the bound costs nothing, and
the day it is not clean the rule is already in place.

`make dev-tools` installs `staticcheck` and `gopls`, both pinned.

Three things kronk says that are worth repeating verbatim: be concise; verify an
API from the live toolchain or the docs rather than from recall; double-check
the arguments of a tool call before submitting it.

**Here:** for Google's endpoints, "the live toolchain" is the generated client
in the module cache (`go doc google.golang.org/api/script/v1`, after a
`go get` you then tidy away). It is the reference for every field name in
`internal/gas/types.go`, and it is not imported -- see section 7.

## 1. A diagnostic is an address. Go to it.

`go build`, `go vet`, `go test` and `gofmt` all report `file.go:line:col`. That
is the answer to "where", and no search is needed to find it. Read the region
around the reported line directly — `sed -n '<line-8>,<line+8>p'` on the file it
named — rather than grepping for the identifier it mentions.

## 2. Find a Go symbol with `make sym`, not with grep

```sh
make sym NAME=Summarise
make sym NAME=Client.ScriptProcesses
make outline FILE=internal/inventory/inventory.go
```

Both are `gopls`, which answers from the same type information the compiler
uses: it knows which `Project` is a method on which type, and it brings the doc
comment with it. There is also a gopls MCP server in some setups
(`mcp__gopls__go_search`, `go_symbol_references`, `go_file_context`); when it is
available it is the same answer without the shell.

**grep is still right** for everything that is not a Go symbol: a JSON field
name, a scope URL, a string in a tool description, a word in the README. The
rule is about the declaration of an identifier, which is what gopls is exact
about.

## 3. One verify command, and its output is already filtered

```sh
make test-unit     # the tests, with -race. No network.
make lint          # go vet + gofmt check
make test          # both, plus govulncheck
make vuln-check    # govulncheck alone. Needs the network.
make go-check PKG= # the after-editing-Go loop, on one package
```

`make test-unit` counts the passing packages instead of listing them, so **there
is nothing to pipe it through**. A filter written in a hurry is one that
eventually hides a `FAIL`; if pass-noise ever comes back, fix `scripts/go-test`
rather than the command line.

**The day JavaScript lands in this repository** (as distinct from the Apps
Script code it pulls, which never lands here): plain ES modules with no build
step and no npm; a unit test is `*_test.mjs` beside the module, run under
Node's own test runner by a `make test-js` target, found by name. Copy the
targets and `scripts/browser.mjs` from stewards rather than inventing new ones.

## 4. Formatting is part of verifying, not a step of its own

`make lint` fails when something is not gofmt-clean and names the files.
`make fmt` fixes them. Running `gofmt -l` speculatively is not useful.

## 5. Where things live

```
cmd/google-scripts-mcp/   main: login, whoami, and the server (the default)
internal/auth/            sign-in, the token file, refresh
internal/gas/             the REST client: types, calls, Google's errors with a hint
internal/gas/gastest/     a fake of those endpoints, for tests
internal/inventory/       putting the three sources together; the remembered-ids file
internal/source/          pulling into a directory, and the record that protects edits
internal/tools/           the MCP tools: names, descriptions, argument schemas
scripts/                  how this is built and checked
```

**Here:** not the Ardan Labs app/business/foundation layering stewards uses.
There is no HTTP server, no database and no deploy, so one package per concern
under `internal/` is the whole shape. The rule that layering exists to enforce
still holds in this form: **`gas` knows the wire and nothing about MCP;
`tools` knows MCP and decides nothing that a test of `inventory` or `source`
could not check.** When two packages need the same thing, it moves down to the
one that knows less.

A question about *what a tool says or accepts* starts in `internal/tools`. A
question about *what counts as a failure, a trigger or an unidentified script*
starts in `internal/inventory`. A question about *what Google sent* starts in
`internal/gas`.

## 6. Who does what: agents write PRs, people merge and hold the account

**Here, and not negotiable.**

- **The human in the loop is the pull request, at a minimum.** Everything in
  `git` is a pull request on a branch. An agent opens it; a person reviews and
  merges it. Nothing is committed or pushed to `main` directly, and an agent
  never merges its own PR.
- **There is no deploy, but `main` is what gets run.** The binary that Claude
  Code registers is built from this checkout, so a merge is what the next
  session's tools do. A PR description says what the change does to the
  tools -- what they can now read, write or refuse -- not only to the code.
- **The Google account is production.** Its scripts are live: they send
  email, write to sheets and answer web requests for other people. An agent
  reaches the account only through this server's tools, only when the person
  asks for that piece of work, and never as a test while developing -- tests
  run against `internal/gas/gastest`, which is what it is for.
- **Write tools act only on a named script, at the person's request, after
  they have seen what will change.** This phase has none: the scopes are
  read-only. When push, deployment deletion and trashing arrive, a push shows
  the diff against the pull first, and a retirement says what stops working
  (web app URLs, triggers in the run history) before it is done. Nothing in
  this server permanently deletes a script; trashing is recoverable from Drive
  for thirty days, and that is the furthest it goes.
- **Agents never sign in and never read the credentials.** `login` is run by
  the person, at a terminal, because the consent screen is theirs to read.
  `~/.config/google-scripts-mcp/` holds the OAuth client and the token; an
  agent does not open, print or copy either. `whoami` is how to ask which
  account is signed in.
- **Agents never set GitHub secrets or variables**, and never run a workflow
  by hand. There are none today; the day there are, a person sets them.

`.claude/settings.json` denies these commands, so the rule holds even when it
is forgotten. Do not work around a denial — a denied command is the answer.

## 7. Rules that are about this repository

- **No `google.golang.org/api`.** See the package comment on `internal/gas`.
  Eight GETs do not justify thirty modules. Reach for a dependency only when
  the standard library has no answer, and say in a comment which answer was
  missing.
- **Scopes are added one phase at a time.** The list in `internal/auth` is
  read-only on purpose. A write tool adds its scope there, and adding one
  means the person signs in again, which is the moment they should see it.
- **A pull never overwrites an edit.** `.gas-pull.json` holds the hash of
  every file as written; keep that check when changing `source`, and give a
  push the same respect for edits made in the web editor since the pull
  (compare the project's update time with `RemoteUpdated`).
- **Stdout is the protocol.** Nothing in the server path may print to it.

## When there is a database

There is none: the only local state is three small JSON files. These are the
storage conventions from mass-intentions by way of stewards, where each one
was paid for. They apply the day the first store lands (plain SQLite through
`modernc.org/sqlite`, as in the sibling projects, unless decided otherwise).

- **No migration tool.** Each store owns `Init(ctx, db)` holding idempotent
  `CREATE TABLE IF NOT EXISTS … STRICT` DDL, and exports
  `Expected sqldb.Expected`. `main` calls every `Init` in foreign-key order and
  then `sqldb.CheckSchema` once. A later column arrives as an `ALTER … DEFAULT`
  beside the `CREATE`, so a fresh database gets it from one and an existing one
  from the other.
- **Nothing that mentions a later column may sit in the `CREATE` block.** An
  index on one goes in a second `Exec` *after* the `AddColumn` call. On a fresh
  database it builds and every test passes; on a database that predates the
  column it runs against a column that is not there yet. This took
  mass-intentions' production down through four deploys, and no test could see
  it, because every test starts from an empty directory.
- **A constraint removed from a `CREATE` block is still there on every database
  that already exists.** SQLite has no `ALTER` that drops one; removing a
  `CHECK` means rebuilding the table, guarded by a `sqlite_master` lookup.
- **A store with a later column owns a test that runs `Init` over the schema as
  it stood before** — the old DDL written out literally, not derived from the
  new one.
- **A health check re-checks the schema**, rather than pinging, if this ever
  grows one.
- **Timestamps are Unix milliseconds in INTEGER columns**, never text.
- **A single-use claim is one statement**, `UPDATE … WHERE x IS NULL` or
  `INSERT … ON CONFLICT DO NOTHING`, never a read followed by a write.

## Scripts and people

**Here:** stewards' rule about photos, applied to what this tool handles. The
scripts are the account's, and they are full of other people: the addresses a
script mails, the names in the sheet it reads, the parish data in its
constants. **Test fixtures are invented**: never a real script's source, title
or id, and never a real person's name, email address or phone number. Pulled
code goes into its own repository (README.md, "Where pulled code goes"), never
into this one, and `.gitignore` refuses `.gs`, `.clasp.json` and
`.gas-pull.json` to make that hard to get wrong. This repository is public.

## House style, in one paragraph

Comments explain **why**, at length, and in prose — including what was tried and
rejected, and what a reader would otherwise assume. Match the density of the
file being edited rather than the density of a tutorial. Error sentences that
reach a person say what to do about it and never name a Go package. Tool
descriptions are written for the model that has to choose between them: say
when to call it, what it cannot see, and what to do next.
