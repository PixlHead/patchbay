# Patchbay · M0

A small local workflow runner, built with Go and React. Choose a sample workflow,
run its HTTP checks, and inspect the results in your browser.

M0 is a working skeleton. Its steps execute sequentially, one workflow
can run at a time, and the last 100 runs live in memory. Restarting the server
clears execution history. Workflow definitions live in JSON files and are loaded
at startup.

## Start with Docker

From this project directory, with Docker's engine running:

```sh
docker compose up --build --wait
```

Open **http://localhost:8080**. Compose builds the app and a small demo service.
The demo service provides healthy, unhealthy, and slow responses on the internal
Docker network. It needs no external API or account.

```sh
docker compose logs -f app     # Follow application logs
docker compose restart app     # Reload edited workflow files; clear run history
docker compose down            # Stop this project's containers
```

The published port binds to loopback. This M0 application has no login; keep it
local until administrator authentication is added in M1. The app uses a non-root
container user. The image contains its frontend assets and needs no CDN at runtime.
The initial image build downloads dependencies; subsequent execution can be offline.

## Develop without Docker

Prerequisites: Go 1.26+, Node 20.19+ or 22.12+, and npm. Node 24 LTS is a suitable
choice for a fresh installation. Dependencies are pinned in `web/package-lock.json`.

```sh
make install
```

Use three terminals, all in the project directory:

```sh
make demo     # Terminal 1: deterministic HTTP service on 127.0.0.1:9091
make api      # Terminal 2: Go API on 127.0.0.1:8080
make web      # Terminal 3: React/Vite on http://127.0.0.1:5173
```

Visit **http://127.0.0.1:5173** during frontend development. Vite proxies `/api`
requests to Go. Frontend edits update automatically; restart `make api` after
changing Go code or workflow files. Stop each process with Ctrl+C.

To run the built frontend through Go instead of Vite:

```sh
make build
./bin/patchbay-demo        # Terminal 1
./bin/patchbay             # Terminal 2; open http://127.0.0.1:8080
```

The Makefile keeps Go and npm caches in `.cache/`. This is local tooling state,
excluded from Git and the Docker build.

## Try the four examples

| Example | Expected result |
| --- | --- |
| Healthy service | Run completes; the service is healthy with HTTP 200. |
| Unhealthy service | Run completes; the service is unhealthy with HTTP 503. |
| Two services, in sequence | The 503 check completes before the healthy check starts; both results are shown. |
| Service timeout | Run completes; the service is unhealthy because it exceeds a 500 ms deadline. |

To observe an unreachable service, stop only the demo process (or run
`docker compose stop demo`) and run a workflow. It should report an unhealthy
result with a connection error. Restart the demo afterward with
`docker compose start demo` or `make demo`.

**“Completed” describes execution, while “Healthy” describes the service.**
A health check that correctly observes HTTP 503 has completed its job. An
execution failure, such as an executor returning an internal error, instead fails
the run and skips remaining steps. M0 makes this distinction visible in the UI.

Response time measures receipt of HTTP response headers. The executor does not
download or store response bodies. Redirects are reported as responses rather
than followed, and normal HTTPS certificate validation remains enabled.

## Change a workflow

Edit a file in `examples/`, or add another `.json` file using this structure:

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
Checks originate from the Go process: inside Docker, `localhost` means that
container. Use the target's LAN address or its address on a shared Docker network
when appropriate.

Only the sample files use `${DEMO_URL}`. The loader replaces that prefix with
the `-demo-url` flag, defaulting to `http://127.0.0.1:9091` for native development.
Compose sets it to `http://demo:9091`. Ordinary absolute URLs are left untouched.
This single demo convenience is not a general template language.

The loader rejects malformed JSON, unknown fields, multiple JSON documents,
unsupported schema/node types, invalid URLs, duplicate workflow/step IDs, empty
workflows, invalid HTTP status codes, and out-of-range timeouts. Steps must number
1–20 and timeouts must be 100–30,000 ms. An invalid file prevents startup and the
error identifies the file and problem. No workflow is executed during loading.

M0's `steps` array is execution order. There are no graph edges or parallel branches
yet. The version field gives us a place to introduce schema changes deliberately
as the later graph model develops.

## Read the implementation

```text
cmd/server/main.go              Wire dependencies, load files, start/shut down HTTP
cmd/demo/main.go                Deterministic local test service
internal/workflow/workflow.go   Workflow types, validation, JSON-file loading
internal/nodes/http.go          Execute one HTTP check
internal/engine/runner.go       Run steps sequentially; own in-memory history
internal/httpapi/api.go         Map HTTP routes to workflow/runner operations
web/src/api.ts                 TypeScript API types and fetch helper
web/src/App.tsx                Workflow selection, polling, run/result views
web/src/style.css              Responsive interface styling
examples/                     Four editable sample workflow definitions
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
| `GET /api/workflows` | Loaded workflow definitions with resolved URLs |
| `POST /api/workflows/{id}/runs` | `202` with a run ID and `Location` header |
| `GET /api/runs` | Latest runs first, up to 100 |
| `GET /api/runs/{id}` | Current run and individual step results |

Starting a run requires an empty body and `Content-Type: application/json`:

```sh
curl -X POST -H 'Content-Type: application/json' \
  http://127.0.0.1:8080/api/workflows/healthy-service/runs
```

Use the returned ID to poll `GET /api/runs/{id}`. A second start while a workflow
is active returns `409`. A missing workflow or run returns `404`. A missing
content type returns `415`; an unexpected request body returns `400`.
The run continues after the request finishes or the browser closes.

## Verify changes

```sh
make test              # Go tests with race detection + TypeScript checking
make build             # Production frontend and both native Go binaries
make fmt               # gofmt and Prettier
```

The Go tests use temporary local HTTP servers and controlled executors. They
cover validation, timeouts, unreachable services, redirects, cancellation,
sequential ordering, execution failure, run admission, history bounds, snapshots,
and the API lifecycle. They do not contact outside services.

For the two browser tests, install Chromium once and run:

```sh
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web exec -- playwright install chromium
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" make e2e
```

Playwright starts dedicated app/demo servers on ports 18080 and 19091. It checks
all four workflows, reload/history behavior, mobile layout, and disconnected
backend feedback. Screenshots go to `web/test-results/`. Linux installations may
also need Playwright's documented browser system dependencies.

## M0 completion and next steps

M0 includes the full browser → API → sequential runner → HTTP check → result path,
editable versioned sample files, bounded in-memory history, basic shutdown,
Docker packaging, and focused automated checks. The next small learning exercise
is to change an HTTP-check field and trace it through both languages.

M1 adds scheduling, SQLite, SSH, scripts, Discord, and administrator access. Visual
graph editing, worker pools, retries, YAML, and model integrations remain later
milestones. No extra packages or placeholder services have been created for them.
