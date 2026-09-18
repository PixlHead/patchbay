# Patchbay · M0

A small local workflow runner, built with Go and React. Load a workflow,
run its HTTP checks, and inspect the results in your browser.

The M0 skeleton is gaining M1 persistence and concurrency. Steps within a run
execute sequentially; by default, two different workflows can run at once,
with ten more waiting in a first-in, first-out (FIFO) queue.
Run snapshots, step progress, and completed results are saved in SQLite. The
frontend displays the latest 100 runs across workflows, including saved history
from earlier server sessions. If a final save fails, a retained in-memory result
appears with a warning. Workflow definitions live in JSON files and are loaded at startup.

## Start with Docker

From this project directory, with Docker's engine running:

```sh
docker compose up --build --wait
```

Open **http://localhost:8080**. This starts only Patchbay and loads JSON files
from `workflows/`. The included **Patchbay health** workflow checks the app's own
`/api/health` endpoint, so it works without another service or internet access.
It demonstrates a check while the app is running; it cannot alert if Patchbay is down.

To clean up containers left by an older setup, run `make down` before starting
this version. It also removes orphaned containers belonging to this Compose
project. Stopping or recreating the app preserves saved run history.

```sh
docker compose logs -f app     # Follow application logs
docker compose restart app     # Reload edited workflow files; keep run history
make down                      # Stop this project's containers
```

The published port binds to loopback. This M0 application has no login; keep it
local until administrator authentication is added in M1. The app uses a non-root
container user. The image contains its frontend assets and needs no CDN at runtime.
The initial image build downloads dependencies; subsequent execution can be offline.

Compose mounts a writable named volume at `/app/data` for SQLite. The rest of the
container filesystem remains read-only. `make down` preserves this volume and
its saved history. Deleting the volume deletes that history.

## Develop without Docker

Prerequisites: Go 1.26+, Node 20.19+ or 22.12+, and npm. Node 24 LTS is a suitable
choice for a fresh installation. Dependencies are pinned in `web/package-lock.json`.

```sh
make install
```

Use two terminals, both in the project directory:

```sh
make api      # Terminal 1: Go API on 127.0.0.1:8080; loads workflows/
make web      # Terminal 2: React/Vite on http://127.0.0.1:5173
```

Visit **http://127.0.0.1:5173** during frontend development. Vite proxies `/api`
requests to Go. Frontend edits update automatically; restart `make api` after
changing Go code or workflow files. Stop each process with Ctrl+C.

To run the built frontend through Go instead of Vite:

```sh
make build
./bin/patchbay             # Open http://127.0.0.1:8080
```

The Makefile keeps Go and npm caches in `.cache/`. This is local tooling state,
excluded from Git and the Docker build.

The server opens `data/patchbay.db` at startup, creating its parent directory
when needed. The default `data/` directory is excluded from Git and Docker builds.
Use `-db` to choose another file, for example:

```sh
go run ./cmd/server -db ./data/development.db
```

On Linux and macOS, new database files are created with owner-only read/write
permissions (`0600`, subject to a more restrictive process umask) before SQLite
opens them. Existing database files and directories keep their permissions;
reopening an older database does not make its existing permissions more private.
SQLite opens in [existing-file mode](https://sqlite.org/uri.html), so a missing
symlink target is rejected rather than created. A failed initialization can leave
the new private file in place for a later retry.

Only one Patchbay server may use a database at a time, even on different ports.
On Linux and macOS, startup takes an operating-system lock on `<database>.lock`
before migrations or interrupted-run cleanup. A second server exits with
`database is already in use by another Patchbay instance`. Use a different `-db`
file to run an independent instance.

The lock lasts until the runner stops and SQLite closes; the OS also releases it
if the process crashes. The empty `.lock` file remains for reuse and does not
mean a server is still running. Do not delete it while Patchbay is running.
Relative paths and symlinks resolve to the same lock. Keep the database on a
local filesystem (including Docker's local named volume); network filesystems
and hard-link aliases are not supported.

## Allowed request hosts

The server accepts requests for `localhost`, `127.0.0.1`, and `::1` by default.
An unexpected or malformed Host receives HTTP 403 before any API or frontend
handler runs. This helps protect the local application against DNS rebinding.
The hostname is compared without its port, so direct access, Vite's proxy on
port 5173, Playwright on port 18080, and Docker's health check remain supported.
DNS names are case-insensitive and may have a trailing dot; equivalent IPv6
spellings are normalized. The check does not resolve incoming hostnames through DNS.

Use `-allowed-hosts` to add exact hostnames or IP addresses, for example when
using a hostname you control that resolves to the local server:

```sh
go run ./cmd/server -allowed-hosts patchbay.home.example
```

Separate additional entries with commas. Supply hostnames or IPs only: no URL
scheme, port, wildcard, or IPv6 brackets. The default local hosts stay allowed.
`-addr` controls where the server listens; it does not add allowed hosts, and
binding to `0.0.0.0` or `::` does not permit every hostname. Docker needs no change
for its existing loopback access. Only add names whose DNS you control.

The guard uses the request's Host, ignoring `Forwarded` and `X-Forwarded-Host`.
A reverse proxy must also reject unexpected public hostnames before rewriting
Host to an allowed backend name. Host validation does not authenticate callers;
the existing requirement to keep this prototype local until M1 login remains.

## Concurrent workflow runs

`-max-active-runs` sets the maximum number of active workflows (default 2).
It must be at least 1; use 1 to keep execution limited to one workflow at a time.
`-max-queued-runs` sets the number of waiting runs (default 10). It must be
nonnegative; use 0 to reject new starts immediately when all active slots are full.
For native development:

```sh
go run ./cmd/server -max-active-runs 4 -max-queued-runs 10
```

For Docker Compose, set the app service's command when changing the limit:

```yaml
command: ["/app/patchbay", "-addr", "0.0.0.0:8080", "-max-active-runs", "4", "-max-queued-runs", "10"]
```

Different workflows can execute concurrently. Each workflow still runs its steps
in order, and a workflow already queued or running cannot be submitted again.
Capacity and overlap checks happen together under the runner's mutex. Waiting
runs start in admission order, with the oldest entry reserving each freed slot
before a new submission can take it. Waiting entries do not own goroutines.
A slot is released after the final save succeeds or its retry limit/deadline is
reached, including failed or canceled runs. Failed initial saves do
not consume a slot. Execution and progress saves run outside that mutex; initial
run creation stays serialized. Node executors and update callbacks must support
concurrent calls from different runs. SQLite still serializes database access.

An accepted start returns HTTP 202 with either `running` or `queued` status.
A full queue returns HTTP 429; rejected starts are not saved as executions.
Queued runs are saved before acceptance and appear in history with their creation
time and no start time. Promotion is saved before the first step executes.
The frontend shows **Queued** while waiting. The runner retains both queued and
running entries when trimming memory; the history API lists the latest 100 runs
from SQLite, replacing stale snapshots with retained unsaved final results when available.

Shutdown stops queue promotion, requests cancellation of active and waiting runs,
and waits for cleanup. A run whose final step returns successfully during
shutdown remains **Completed**, even if shutdown cancels that step's progress
save. Its final save uses a fresh cleanup context. If shutdown prevents remaining
steps from executing, the run is **Canceled** and those steps are skipped. A
canceled waiting run has skipped steps and no start time. Queued-run cleanup
shares a five-second save budget. If shutdown or a save fails, startup marks
leftover queued/running records interrupted. Automatic resumption of saved queued
work is a later increment; this queue currently lives within one process.

## Explore workflow canvases

Each workflow page has a [React Flow](https://reactflow.dev/learn) canvas seeded
from that workflow's existing step names. Layouts and added or removed placeholder
nodes belong to that workflow only. They remain in memory while you switch
workflows or visit the New workflow page; normal API polling does not reset them.

Use **New workflow** in the top navigation, or open
`http://localhost:8080/#/canvas` (`http://127.0.0.1:5173/#/canvas` in development).
Start with a blank canvas, enter a name, and add HTTP, SSH, or Discord placeholder
nodes. **Create draft** adds the workflow to the sidebar and opens its own page.
It creates frontend state only: local drafts have no Run button and never send
a workflow-creation request to the backend.

Drag nodes, pan and zoom, use **Reset layout**, or select a node and press Delete
to remove it. Node connections remain disabled. Reloading clears all canvas
edits, unfinished new workflows, and locally created drafts.

Canvas editing does not modify or execute the saved workflows. For existing
workflows, **Run workflow** still uses the saved steps shown below the canvas.
The existing backend requests and execution behavior are unchanged.

`web/src/WorkflowCanvas.tsx` is the shared canvas component.
`web/src/canvasDraft.ts` defines draft data and maps saved steps into nodes.
`web/src/App.tsx` owns draft state by workflow ID across page navigation.
`web/src/CanvasPage.tsx` provides the New workflow form.

## Example workflows

`examples/http-check.json` is a single HTTP check; `examples/sequence.json`
checks two services in order. Copy a template into `workflows/`, change its URLs
to services you control, and restart the app. These files are configuration
templates; no target service is bundled or started for them.

For manual local testing, start your own app and point a workflow at its health
endpoint. The included **Patchbay health** workflow is also ready to run against
Patchbay itself.

**“Completed” describes execution, while “Healthy” describes the service.**
A health check that correctly observes HTTP 503 has completed its job. An
execution failure, such as an executor returning an internal error, instead fails
the run and skips remaining steps. M0 makes this distinction visible in the UI.

Response time measures receipt of HTTP response headers. The executor does not
download or store response bodies. Redirects are reported as responses rather
than followed, and normal HTTPS certificate validation remains enabled.

HTTP and HTTPS checks connect directly to the configured endpoint. They ignore
`HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` (including lowercase variants) in the
server's environment, so proxy settings do not change the route being checked.
The endpoint must be reachable directly from the Go process or container;
configurable proxy support is not currently available.

HTTP checks configure a 64 KiB response-header limit on their transport; Go
applies the corresponding protocol-specific accounting for HTTP/1 and HTTP/2.
Oversized or malformed responses produce an unhealthy result rather than an
engine failure. Request-failure reasons are capped at 4 KiB of UTF-8 text,
including the prefix and an `... [truncated]` marker when needed. Invalid UTF-8
is replaced and truncation preserves character boundaries. The text limit applies
before JSON escaping; previously saved history is not rewritten by this change.

## Change a workflow

Edit a file in `workflows/`, or add another `.json` file using this structure:

```json
{
  "schemaVersion": 1,
  "id": "my-service",
  "name": "My service",
  "description": "Check my service's health endpoint.",
  "steps": [
    {
      "id": "health",
      "name": "Check health",
      "type": "http.check",
      "config": {
        "url": "http://192.168.1.50:8080/health",
        "expectedStatus": 200,
        "timeoutMs": 3000
      }
    }
  ]
}
```

Replace that example address with a service you control, then restart the app.
Keep at least one valid workflow in the selected directory; M0 still rejects
an empty workflow directory.
Checks originate from the Go process: inside Docker, `localhost` means that
container. Use the target's LAN address or its address on a shared Docker network
when appropriate.

URLs are used as written. The loader does not expand variables or templates.

The server defaults to `-workflows workflows`; pass a different directory when
needed. The starter health check uses port 8080. If you change the native server's
`-addr` port, update that check's URL too. Changing only Docker's published host
port does not change the app's internal port or the starter check.

The loader rejects malformed JSON, unknown fields, multiple JSON documents,
unsupported schema/node types, invalid URLs, duplicate workflow/step IDs, empty
workflows, invalid HTTP status codes, and out-of-range timeouts. Steps must number
1–20 and timeouts must be 100–30,000 ms. An invalid file prevents startup and the
error identifies the file and problem. No workflow is executed during loading.

Each workflow JSON file may contain up to 64 KiB, including whitespace. The
loader reads at most 64 KiB plus one byte to detect oversized files, then closes
the file before validation. Files exactly at the limit remain supported.

M0's `steps` array is execution order. There are no graph edges or parallel branches
yet. The version field gives us a place to introduce schema changes deliberately
as the later graph model develops.

## Read the implementation

```text
cmd/server/main.go              Wire dependencies, load files, start/shut down HTTP
internal/workflow/workflow.go   Workflow types, validation, JSON-file loading
internal/nodes/http.go          Execute one HTTP check
internal/engine/runner.go       Run steps sequentially; save execution progress
internal/httpapi/api.go         Map HTTP routes to execution and saved history
web/src/api.ts                 TypeScript API types and fetch helper
web/src/App.tsx                Workflow selection, polling, run/result views
web/src/style.css              Responsive interface styling
workflows/                    Normal workflow definitions (one starter check)
examples/                     Templates for checking your own services
web/tests/fixtures/           Workflow data used only by browser tests
```

For a guided explanation, read [the M0 walkthrough](docs/m0-walkthrough.md).
Start with `workflow.go`, then the HTTP executor, then the runner; the UI and
HTTP handlers are adapters around those pieces.

The [longer-term roadmap](docs/roadmap.md) preserves the agreed plan. See
[the handoff verification record](docs/verification.md) for checks actually run
and the remaining environment limitations.

## API

| Method and route | Response |
| --- | --- |
| `GET /api/health` | App readiness, not monitored-service health |
| `GET /api/workflows` | Loaded workflow definitions |
| `POST /api/workflows/{id}/runs` | `202` with a run ID and `Location` header |
| `GET /api/runs` | Latest 100 runs across workflows, newest first; retained unsaved final results include a warning |
| `GET /api/runs/{id}` | Saved run and step results, or a retained unsaved final result with a warning |

Starting a run requires an empty body and `Content-Type: application/json`:

```sh
curl -X POST -H 'Content-Type: application/json' \
  http://127.0.0.1:8080/api/workflows/patchbay-health/runs
```

Use the returned ID to poll `GET /api/runs/{id}`. Starting the same workflow
while it is queued or running returns `409`. A different workflow waits when
all active slots are full; once the queue is also full, starts return `429` with
both configured limits in the error message.
A missing workflow or run returns `404`. A missing content type returns `415`;
an unexpected request body returns `400`.
The run continues after the request finishes or the browser closes.

## Verify changes

```sh
make test              # Go race tests + TypeScript checking (app, browser tests, configs)
make build             # Production frontend and native app binary
make fmt               # gofmt and Prettier
```

The Go tests use temporary local HTTP servers and controlled executors. They
cover validation, timeouts, unreachable services, redirects, cancellation,
sequential ordering, concurrent admission, execution failure, history bounds, snapshots,
and the API lifecycle. They do not contact outside services.

For the browser tests, install Chromium once and run:

```sh
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web exec -- playwright install chromium
make e2e
```

The Makefile defaults `PLAYWRIGHT_BROWSERS_PATH` to `.cache/playwright`; an
explicit environment override is respected. Playwright starts only Patchbay on
port 18080, with workflow data from `web/tests/fixtures/workflows/`. Each server
start gets a fresh `.cache/e2e/run.XXXXXX/patchbay.db`, so previous test runs
cannot supply old results. These generated directories are retained for debugging
and can be removed when no browser tests are running.

Both checks call that test instance's own health endpoint: one expects HTTP 200
and one deliberately expects HTTP 204 to exercise unhealthy result rendering.
The execution test requires a successful POST and verifies that the newly created
run ID completes and survives a browser reload. Browser tests also cover mobile
layout and disconnected backend feedback.
Timeouts, HTTP 503, and unreachable targets remain covered by Go's temporary
HTTP test servers. Screenshots go to `web/test-results/`. Linux installations may
also need Playwright's documented browser system dependencies.

## M1 storage foundation

`internal/store/sqlite.go` introduces `store.Open(ctx, path)` for a SQLite file.
Its parent directory must exist, and the caller closes the returned database.
The helper keeps one reusable connection, enables foreign-key checks on every
connection, and waits up to five seconds when SQLite encounters a database lock.
It verifies the connection before returning so path errors are reported early.
The [modernc.org/sqlite driver](https://pkg.go.dev/modernc.org/sqlite) supports the
existing build with CGO disabled.

The server now opens SQLite before starting HTTP and closes it after the runner
stops. Database path, migration, or write-check errors prevent startup. After
identity validation and migration, startup performs a write check with a
five-second context deadline before interruption cleanup or HTTP startup. It
commits the current [`user_version`](https://sqlite.org/pragma.html#pragma_user_version) back
to SQLite's header, exercising a real write even when history is empty. The
version, application marker, tables, and saved runs stay unchanged. A failure
returns `check database writeability` with the underlying storage error; the
database closes and its ownership lock is released.

This small check confirms a write can commit at startup. It does not reserve
space for future runs or guarantee later writes will succeed; runtime save
failures retain their existing handling.

The runner saves a new run and its workflow snapshot before admitting it for
execution. A failed save returns HTTP 500, starts no steps, and consumes neither an active slot nor
queue space. The client receives `Could not save run. Check server logs.`;
the server logs the underlying storage error with the workflow ID.
The save uses a five-second timeout tied to the runner, so ending an HTTP request
does not cancel an accepted run. Tests can pass nil persistence callbacks for an
in-memory runner.

The runner saves each step's start before executing it, then saves its result or
error before moving on. A final update saves the run status and any skipped
steps. Progress writes happen outside the history mutex and use five-second
timeouts. Shutdown cancels execution, then allows up to five fresh seconds per
active run's final save (including retries) and one shared five-second budget for queued cleanup
before closing SQLite. Compose allows 20 seconds for graceful shutdown, including database cleanup and HTTP shutdown.

Final updates make at most three attempts, with delays of 100 ms and 200 ms before
the retries. All attempts and delays share the same five-second deadline, or the
earlier queued-cleanup deadline. A slow attempt can consume the whole budget;
three attempts are a limit, not a guarantee. Retries write the same completed
snapshot, including its original finish time and step outputs.

If a progress save fails, the runner logs the error with the run ID, stops later
steps, and saves the terminal state using that same final-save retry policy.
Completed step outputs are preserved; steps that never executed are skipped.
The run's `error` identifies the step whose start or result could not be saved
and directs the administrator to server logs. Run details display this reason
above the step results. A successful final save preserves it in history, even
after a restart. Cancellation during shutdown does not add a storage-error reason.
If all final-save attempts fail or time runs out, the failure is logged and
SQLite retains its last saved snapshot. While the completed result remains in
the runner's memory history, both history endpoints return it with
`finalSaveFailed: true`. Execution status, step outputs, errors, and finish times
are preserved. The flag is separate from execution success: a successful run
still appears as **Completed**, with **Final result not saved** in the history
list and a warning in its details. Raw storage errors stay in server logs.

This warning and result are temporary; they use the existing bounded memory
history rather than an unbounded collection of failed saves. Eviction or a server
restart loses them. After eviction, the API again returns SQLite's last snapshot;
on restart, unfinished saved records become interrupted as described below.
Reloading the browser alone does not clear the server's memory. No database
migration or background save-recovery loop is added by this warning.
External actions are never retried because a database write failed.

Both history endpoints read SQLite using the request context and a
five-second timeout, then overlay retained results whose final save failed.
The overlay preserves list membership and order; normally saved runs continue
to come from SQLite. The list returns the newest 100 runs across workflows;
the frontend filters that list for the selected workflow. Older runs remain in
the database and can be fetched by ID. An empty list is `[]`, a missing ID returns
404, and a database read failure returns 500 with details in server logs, even
when a result is available in memory. The overlay does not hide read failures.

Restarting with the same database preserves saved history. Native development
uses `data/patchbay.db`; Docker uses its named volume, so those installations
have separate histories. A workflow page shows runs matching its current ID.
Before creating the runner or accepting HTTP requests, startup calls
`store.MarkUnfinishedRunsInterrupted(ctx, db)` with a five-second timeout.
Saved `queued` and `running` runs become `interrupted`; active steps become
`interrupted` and pending steps become `skipped`. Completed results, errors,
workflow snapshots, and timestamps are preserved. Missing finish times stay
unknown, so the frontend shows **Interrupted** and **Finish time unknown** for
started runs, or **Never started** for waiting runs, without inventing a duration.

The cleanup is one transaction. Failure prevents startup; success logs the
number of affected runs when nonzero. Repeating it leaves completed and already
interrupted runs unchanged. This is startup-only cleanup for one server using
the database, not a way to cancel active executors. No steps are automatically
resumed or retried. The Run button relies on the server's current admission
check rather than treating saved statuses as proof that an execution is active.
After review, the focused tests can be run with
`go test -race ./internal/engine ./internal/httpapi ./cmd/server ./internal/store`.
Opening the database also applies migrations from `internal/store/schema.go`
up to database schema version 2. A new database applies versions 1 and 2 in one
transaction. An existing version-1 database gains a `runs.error` text column
with an empty default; its history is preserved.
The `runs` table holds run metadata and a JSON copy of the workflow definition;
`run_steps` holds ordered step results. Timestamps use Unix milliseconds.
Schema changes and SQLite's `user_version` number are committed together.
Reopening the current version preserves existing data; unsupported versions are
rejected. The migration tests cover upgrading existing history, reopening,
conflicts, and version rejection. Older Patchbay builds that only support schema
version 1 cannot open an upgraded database; there is no automatic downgrade.
The workflow definition format remains at version 1.

Before migration or startup cleanup, Patchbay checks SQLite's
[`application_id`](https://sqlite.org/pragma.html#pragma_application_id) marker
and the expected table layout. New databases must be empty and unmarked.
Unmarked version-1 and version-2 files are recognized by their two tables,
column definitions, primary keys, unique step positions, and cascading foreign
key. Extra user-defined schema objects prevent adoption of an unmarked file;
SQLite's internal indexes and statistics are allowed.

A recognized legacy file receives the Patchbay marker (`0x50544259`, "PTBY")
in the same transaction as any needed migration. This header marker leaves the
database schema version at 2. Marked files still have their table layout checked;
additional indexes and triggers on Patchbay's tables are allowed. A foreign
marker, unsupported version, or unrecognized layout prevents startup before
Patchbay changes the database. Choose a separate database path for a new instance.
Recognition protects against mistakenly selecting an unrelated database; it
does not prove the origin of a file with an identical layout or check data integrity.

`engine.Run.Error` stores a run-level reason separately from each step's error.
Create/update operations save it with the snapshot, and get/list operations
restore it. Its JSON field is `error`, omitted when empty. The runner does not
copy raw storage errors into it: progress-save failures use a readable message,
while each step retains its own execution error. A final-save failure alone uses
the separate memory-only `finalSaveFailed` warning.

`store.CreateRun(ctx, db, run, definition)` inserts a run, its workflow snapshot,
and its ordered step results in one transaction. Duplicate run IDs are rejected;
a failed step insert rolls back the entire save. Missing timestamps and outputs
are stored as SQL NULL. `createdAt` records admission; `startedAt` is absent
until execution begins. Updates preserve the original creation time.

`store.UpdateRun(ctx, db, run)` saves execution statuses, timestamps, outputs,
and errors together. It preserves the workflow snapshot, creation time, names,
and step order. The caller supplies all original steps and serializes updates
for each run. Missing runs or mismatched steps reject the entire update.

`store.GetRun(ctx, db, id)` reads one saved run and its ordered step results in
a single read transaction, keeping them consistent during concurrent updates.
It restores UTC timestamps and decoded outputs. A missing run returns an error
wrapping `sql.ErrNoRows`; malformed output JSON returns an error instead of
partial history.

`store.ListRuns(ctx, db, limit)` accepts limits from 1 to 100 and returns complete
runs ordered by creation time newest first, then by run ID descending for ties.
The list and its step results share one read transaction. An empty history
returns an empty list; invalid limits or read errors return an error without
partial results. Run creation, execution updates, and history API reads are
connected to storage.

## M0 completion and next steps

M0 includes the full browser → API → sequential runner → HTTP check → result path,
editable versioned sample files, bounded in-memory history, basic shutdown,
Docker packaging, and focused automated checks. The next small learning exercise
is to change an HTTP-check field and trace it through both languages.

M1 adds scheduling, SQLite, SSH, scripts, Discord, and administrator access. Visual
graph editing, worker pools, retries, YAML, and model integrations remain later
milestones. No extra packages or placeholder services have been created for them.
