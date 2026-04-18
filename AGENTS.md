# Development Philosophy

This document captures high-level preferences that apply across every project
bootstrapped from this repo. Language- and framework-specific patterns live in
the individual skills under `skills/` — this file is about the values that
should shape decisions before any code gets written.

## Unix Philosophy

- **Do one thing well.** Each component has a single, well-defined responsibility.
- **Write programs that compose.** Design for piping and standard interfaces.
  Outputs should be valid inputs for other programs.
- **Be quiet by default.** No news is good news. Only output when there's
  something unexpected to report.
- **Text streams are the universal interface.** JSON on stdout, human-readable
  status and errors on stderr, exit codes for success/failure.

## Stdout vs stderr

- **stdout is for machine-readable output.** Default to JSON. Pipe it, parse it,
  compose it.
- **stderr is for humans.** Progress messages, warnings, errors, and anything
  else that shouldn't contaminate a pipeline.
- **Interactive CLIs may use friendly formatting on stdout,** but always offer a
  `--json` / `--format=json` flag for scripting.

## Favor Simplicity

Complex solutions need complex problems to justify them. Reach for the simple
tool first:

- **SQLite over a dedicated database server** when a single process is enough.
- **Standard library HTTP over frameworks** — handlers as functions, middleware
  as composition.
- **Plain HTML (with a sprinkle of HTMX or similar) over heavy frontend stacks**
  when the task doesn't demand a SPA.
- **Environment variables over elaborate config DSLs.**
- **Standard SQL over ORM magic.**
- **Plain JSON over custom binary protocols.**

## Simple CLIs

- One command, one job. Compose with pipes, not flags.
- Subcommands only when you genuinely have a family of related operations.
- Sensible defaults; require no flags for the common case.
- `-h` / `--help` always works. Help text shows real usage, not marketing.
- Exit 0 on success, non-zero on failure. No "successfully completed" banners.

## Hot Reloading

Development should feel instant. Every project should have a hot-reload story:
Air for Go, `uvicorn --reload` for Python, equivalent tooling for whatever
stack. Edit file, see result. If hot reload isn't working, fix it before
writing more code.

## Makefiles as the Front Door

Every project needs a `Makefile` with the primary targets (`build`, `test`,
`lint`, `run-*`, `deploy-*`) so that humans and agents share a single entry
point. No one should have to dig through READMEs to figure out the right
command — `make help` lists everything.

**Dev targets must tee stdout and stderr to files in `logs/`.** A coding agent
can't iterate on a bug it can't see. When `make run-server` dumps output into
a separate terminal, the agent is blind; when it tees to `logs/server.log`,
the agent can `tail` / `grep` / `jq` the output, diagnose what's broken, fix
the code, and watch the reload pick it up.

```makefile
.PHONY: run-server
run-server: ## Run server with hot reload, tee to logs/
	@mkdir -p logs
	$(call setup_env, .env.server)
	uv run uvicorn server.main:app --reload 2>&1 | tee logs/server.log
	# Go equivalent:
	# air 2>&1 | tee logs/server.log
```

Keep `logs/` gitignored. The value is the feedback loop, not the artifacts.

## Project Bookkeeping (for the Ralph Wiggum loop)

Every project keeps four living documents at the root so an agent with empty
context can bootstrap itself in a few reads. Update them after every major
task — they are the state handoff between sessions.

- **`README.md`** — For humans and agents. Describes the interface: what this
  project does, how to build/run/test it, the primary commands, and the
  public API surface. If someone (or a future agent) can't figure out how to
  use the project from the README, it's broken.

- **`TODO.md`** — Tracked work items. Agents may edit freely to mark tasks
  complete, add new tasks, record dependencies between tasks, or reshuffle
  priority. Keep it flat and scannable; one line per task with status
  markers (`[ ]`, `[x]`, `[blocked: ...]`).

- **`CHANGELOG.md`** — Append-only. Every meaningful change lands as a new
  entry, newest first, grouped by version or date. Follow the [Keep a
  Changelog](https://keepachangelog.com/) format. Never rewrite history —
  if something was wrong, add a correcting entry.

- **`LEARNINGS.md`** — Hard-won knowledge. When an agent discovers a pitfall
  the hard way — a subtle bug, a misleading API, a tool that silently does
  the wrong thing, a pattern that looks right but isn't — record it here
  with enough context that the next agent avoids the same ditch. Frame each
  entry as: *what happened, why it was surprising, how to avoid it.*

**After every major task, update all four files as needed.** An agent starting
fresh should be able to read `README.md` → `TODO.md` → `LEARNINGS.md` (in that
order) and be immediately productive. That's the Ralph Wiggum loop: any agent,
any time, zero prior context, still shipping.

## Language Idioms

Honor the culture of the language you're in.

### The Zen of Python

> Beautiful is better than ugly. Explicit is better than implicit. Simple is
> better than complex. Flat is better than nested. Readability counts. There
> should be one — and preferably only one — obvious way to do it.

Use dataclasses and type hints. Prefer composition over inheritance. Let
exceptions propagate to the right layer. Don't fight the GIL; use async or
processes when it matters.

### Go Idioms

Accept interfaces, return structs. Pass `context.Context` as the first
parameter. Handle every error with `fmt.Errorf("...: %w", err)`. Goroutines
have owners that know when they stop. The stdlib is the framework — reach for
it first, and only adopt a dependency when the stdlib genuinely falls short.

## Explicit Dependencies

Never hide dependencies in global state, singletons, or package-level
variables. Constructors take what they need as parameters. This applies in
every language:

- Dependencies are visible in type signatures.
- Testing becomes trivial — swap real for fake at construction time.
- Lifecycle is clear — whoever constructs something owns its cleanup.

## Production-Ready Defaults

Every service should ship with:

- Structured logging (JSON in prod, pretty in dev), level controlled by
  `LOG_LEVEL`.
- A `/healthz` endpoint.
- Prometheus metrics on `/metrics`.
- Graceful shutdown on SIGTERM.
- Secrets from environment variables. Never commit `.env` files.

## Error Handling

- Handle errors at the layer that can do something about them. Don't swallow,
  don't blindly re-raise.
- Wrap errors with context as they propagate. The final message should tell a
  story: what was being attempted, what went wrong, and why.
- Validate at system boundaries (user input, external APIs). Trust internal
  code.

## Project Bootstrapping: Installing Skills

When initializing a new project, install **only the skills that are relevant
to what the README describes**. The blanket `./install-skills.sh` at the root
of this repo pulls in everything as a convenience for contributors; a fresh
project should stay lean so the agent's skill-selection step isn't polluted
by irrelevant options.

Once a `README.md` exists for the new project, do the following before any
code is written:

1. Read the README and enumerate the concrete capabilities the project needs
   (e.g. "FastAPI service talking to Postgres", "Temporal workflows in Go",
   "marimo notebook for exploratory analysis").
2. Cross-reference each capability against the skill catalog in this repo's
   top-level `README.md` (both "Skills in this repo" and "Third-party skills I
   like"). For anything not covered, run `npx skills find <keyword>` to
   discover candidates.
3. Write an executable `install-skills.sh` at the project root that installs
   **only the matching skills**, one skill per line, pinned by name with the
   `-s` flag:

   ```bash
   #!/bin/bash
   # Per-skill installs, selected to match the capabilities in README.md.
   # Commit the resulting skills-lock.json.
   set -e

   npx skills add brojonat/llmsrules -s fastapi-service -y
   npx skills add brojonat/llmsrules -s pyproject-config -y
   npx skills add supabase/agent-skills -s supabase-postgres-best-practices -y
   npx skills add obra/superpowers -s systematic-debugging -y
   npx skills add obra/superpowers -s test-driven-development -y
   ```

4. Run the script and commit both `install-skills.sh` and the generated
   `skills-lock.json`. On another machine, `npx skills experimental_install`
   will restore the exact set from the lockfile.

**Always use the `-s <skill>` form, one skill per line.** The `npx skills add
<owner/repo>` form without `-s` installs every skill in that repo, which
drags in unrelated skills and forces the agent to wade through noise on every
skill-selection step. Explicit per-skill lines are self-documenting, stay
honest as requirements evolve, and give future-you (or a reviewer) a clear
answer to "why is this skill here?"

If the project later grows a new capability, add the corresponding
`npx skills add ... -s ... -y` line and re-run the script — don't reach for
the whole-repo shortcut.

## When In Doubt

Ask: *"Does this add essential value, or does it just add complexity?"* If you
can't articulate the essential value in one sentence, it's probably
complexity.
