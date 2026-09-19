# Patchbay M0: follow one workflow run

The simplest path through this code is:

```text
React: startRun()
    → POST /api/workflows/{id}/runs
    → runner.Start(definition)
    → background run() loop
    → httpNode.Execute(context, step)
    → store each result under the runner mutex
    → React polls GET /api/runs
    → RunDetails renders the result
```

## 1. Startup creates the dependencies

Open `cmd/server/main.go`. `workflow.Load` reads and validates the JSON definitions.
Startup loads `workflows/`; its starter check calls the app's own health
endpoint. Use `make api` during native development or `make up` with Docker.
The loader uses each configured URL as written.
`httpcheck.New()` constructs the HTTP executor with its reusable client;
`tcpcheck.New()` constructs the TCP executor. The server's `checkExecutor`
switch selects the executor by step type and supplies that function to
`engine.New`. `httpapi.New` connects those values to HTTP handlers.

Nothing is looked up through a global service container. You can see which value
depends on which other value in `main`.

## 2. A definition describes work; a run records an attempt

In `internal/workflow/workflow.go`, `Definition` holds the workflow ID, name,
schema version, and ordered steps. `CheckConfig` describes what one HTTP or TCP
check expects. Each executor lives in its own package under `internal/nodes`,
with its tests and private helpers beside it.

In `internal/engine/runner.go`, `Run` holds one execution ID, timestamps, overall
status, and a result record for each step. Clicking Run twice produces two
different run IDs. It does not change the definition.

The loader reads definitions once. There is no file watcher or write API. Editing
a JSON file in the selected directory and restarting the server is the entire
authoring loop for M0.

## 3. The browser requests execution

`web/src/App.tsx` has a `startRun` function. It POSTs to the selected workflow's run
endpoint, saves the returned run ID, and displays the execution. The button is
disabled while a run is active, but the backend also enforces that limit because
there could be another browser tab or a direct API client.

The shared `request` helper in `web/src/api.ts` handles JSON responses and errors.
The small TypeScript types mirror Go's JSON fields; they are not a second workflow
engine. Keep these types updated when changing the response shape.

## 4. The handler admits work and returns immediately

The POST handler finds the saved definition and calls `runner.Start`. A valid
admitted run returns HTTP 202. No URL or code submitted in this request is executed;
it selects an already-loaded definition.

The runner creates its own application-lifetime context in `New`. It deliberately
does not use `r.Context()` from the HTTP request for background work: that context
is canceled when the request ends. Closing the page must not cancel an admitted run.
The API test `TestRunLifecycleSurvivesRequestEnd` exercises that distinction.

## 5. One goroutine owns the sequential execution loop

`Start` launches `go r.run(...)`. That keeps the HTTP server responsive while checks
are running. It does **not** mean the workflow steps execute in parallel: the loop
calls the executor once per step, waiting for each call to return.

There is at most one active workflow in M0. `ErrBusy` rejects additional runs.
This makes the first concurrency boundary small enough to understand before a
future worker-pool implementation.

HTTP handlers and the execution goroutine share run history, so the runner uses a
mutex. It holds the lock only while reading or updating in-memory data, and releases
it before the network request. Reads return copies of step slices and result data
so JSON encoding does not race with the running workflow.

## 6. The node answers a health question

`internal/nodes/httpcheck/http.go` creates a timeout context for the step, performs a GET,
and compares the response status to the expected one. It measures time to response
headers and closes the body without storing it. It does not follow redirects.

There are two different outcomes:

- A check returns health data, including HTTP 503, an unreachable endpoint, or a
  timeout. The step completed, and its output may be unhealthy.
- The executor returns an execution error. The engine fails the run and skips the
  remaining steps. Server shutdown is recorded as cancellation.

That is why the UI can show both **Completed** and **Unhealthy** for one run.

## 7. Polling reads the execution trail

The React effect refreshes the definitions and recent run list once per second.
It uses a recursive timeout, so a slow refresh does not start overlapping refreshes.
Cleanup aborts its requests when the component unmounts. Connection failures remain
visible and retry automatically.

The run list is newest first. Selecting another workflow filters that history;
selecting a past run displays its results. A browser reload preserves history while
the Go process is alive. Restarting the Go process clears it.

## 8. Shutdown owns cancellation

`main` listens for Ctrl+C or SIGTERM. `runner.Close` refuses new runs, cancels the
application context, and waits for the active execution goroutine to finish. The
HTTP node observes that context, so a blocked request can stop. Then the HTTP
server shuts down. No persistent recovery is implied: this is in-memory M0.

## Exercises before adding M1

Copy a template from `examples/` into `workflows/` and point it at an app you
control. Restart Patchbay after changing the file. For timeout experiments, add
a deliberately delayed health endpoint to that app.

1. Change a sample's expected HTTP status to 503. Predict the result, restart,
   and check your prediction.
2. Set a delayed endpoint's timeout first below and then above its response
   delay. Explain the change in its health result.
3. Stop your target app and inspect the difference between a connection failure
   and a deliberately returned HTTP 503 response.
4. Put two steps into a new JSON workflow and verify their timestamps show order.
5. Add an invalid timeout and read the startup validation error.
6. Add a small response field, such as the HTTP protocol, from the executor through
   the Go result type, TypeScript type, and result card.

Before parallel execution, answer: who owns the state, which operations need a
lock, which context owns the work, and what will happen if an operation fails?

Useful primary references: [Go context](https://go.dev/blog/context),
[Go HTTP client](https://pkg.go.dev/net/http),
[Go race detector](https://go.dev/doc/articles/race_detector), and
[React effects](https://react.dev/learn/synchronizing-with-effects).
