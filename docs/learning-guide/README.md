# Patchbay learning guide

This guide reads the Patchbay repository file by file and line by line. It is written for a junior developer who is learning Go and React and wants to understand how a small server owns state, runs work on goroutines, cancels it, and saves it. Each chapter covers a few files. It explains the role of each file, the language and library concepts the file depends on, every line in order, what state the code owns and which goroutine or component owns it, what happens on failure, and which test proves each claim. Each chapter ends with "Check yourself" questions and "Experiments".

The guide sits beside five other documents. Read them in this relation:

- `README.md` describes what the application does today: setup, API behavior, operating limits, and commands. When the guide and the README disagree, check the code, then fix whichever is wrong.
- `AGENTS.md` holds the working rules for changing the code: state ownership, concurrency, cancellation, and persistence. The chapters quote these rules and point at the lines that uphold them.
- `docs/m0-walkthrough.md` is the original narrative of one run through the M0 skeleton. Some of its claims are historical. For example, it says only one workflow can be active and that a restart clears history. The current code admits two workflows and keeps history in SQLite. Treat it as context, not as a description of the present code.
- `docs/verification.md` records which checks were actually run, on which date, and which environment limits remain. It separates "tests that exist" from "tests that were run".
- `docs/roadmap.md` is the agreed plan for later milestones. It is not a list of implemented features. The last section of this page uses it to say what is still missing.

## Revision note

Every `L123` and `path:line` reference in this guide refers to commit `1c4786e`. Each chapter repeats that commit in its "Revision" line. Before trusting a line number, check whether the file changed after that commit:

```sh
git log --oneline 1c4786e..HEAD -- internal/engine/runner.go
```

No output means the file is unchanged and the line numbers still hold. Any output means the chapter describes an older version of that file. Read the diff with `git diff 1c4786e..HEAD -- <path>` and adjust as you go.

## How to use it

Open the source file next to the chapter. Read the file with line numbers, so the chapter's `L` references land on the right line:

```sh
cat -n internal/engine/runner.go
```

An editor that shows line numbers works the same way. Read the chapter's "Role in the system" and "Concepts you need" sections first, then walk the file with the "Walkthrough" section open beside it. Each walkthrough entry cites a line range and says what it does and why.

Answer each "Check yourself" question in writing before you open its answer. The answers sit inside collapsed `<details>` blocks and cite the lines and tests that justify them. If your answer cites different lines, go back to the file and find out which of you is right.

Run the "Experiments" yourself. Each experiment names one edit, one command, and the exact failure or output you should see. Revert the edit afterwards. The experiments are the only place where the guide asks you to run tests or the server; `AGENTS.md` asks an assistant not to run them unless delegated, but that rule is for the assistant, not for you.

## Setup

These are the commands from the "Develop without Docker" section of `README.md`, stated as the README gives them. The prerequisites are Go 1.26+, Node 20.19+ or 22.12+, and npm.

```sh
make install
```

Use two terminals, both in the project directory:

```sh
make api      # Terminal 1: Go API on 127.0.0.1:8080; loads workflows/
make web      # Terminal 2: React/Vite on http://127.0.0.1:5173
```

To run the built frontend through Go instead of Vite:

```sh
make build
./bin/patchbay             # Open http://127.0.0.1:8080
```

The README's "Verify changes" section gives the test command the experiments rely on:

```sh
make test              # Go race tests + TypeScript checking (app, browser tests, configs)
```

The browser tests in chapter 28 need Chromium installed once and `make e2e`; the README gives those two commands under "Verify changes".

## Reading order

The chapters go from configuration, through the Go backend in the order a request travels, to the React frontend, and end with the tests. Each test chapter proves the claims of the production chapters before it.

| # | Title | Files | What you will understand after it | Chapter |
| --- | --- | --- | --- | --- |
| 1 | Project layout, build, and packaging | `go.mod`, `Makefile`, `Dockerfile`, `compose.yaml`, `.gitignore`, `.dockerignore`, `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/.prettierrc.json` | Which tool reads each build file, why the image binds to loopback, runs as non-root on a read-only filesystem with a 20-second stop grace, and why `make e2e` depends on `make build`. | [01](01-project-layout-and-build.md) |
| 2 | Workflow JSON files and templates | `workflows/01-patchbay.json`, `workflows/http-check.json`, `workflows/scheduled-check.json`, `workflows/sequence.json`, `workflows/tcp-check.json`, `examples/http-check.json`, `examples/scheduled-check.json`, `examples/sequence.json`, `examples/tcp-check.json` | How each JSON key maps to a Go field, a validation rule, and a runtime consumer; why the files are strict; why `01-patchbay.json` sorts first; why edits need a restart while past runs keep their snapshot. | [02](02-workflow-files.md) |
| 3 | Server startup, wiring, shutdown, and the database lock | `cmd/server/main.go`, `cmd/server/database_lock.go` | How `main` validates flags, takes the flock lock before migrations, checks a bounded write, wires the runner to SQLite through closures, and tears down in order: scheduler, runner, HTTP, SQLite, lock. | [03](03-server-main-and-database-lock.md) |
| 4 | Workflow definitions, validation, and loading | `internal/workflow/workflow.go` | Why the four types are both the authoring contract and the permanent history contract, why `Load` runs once and `Validate` runs on every start, and how the strict decoder and `Validate` split the work. | [04](04-workflow-definitions.md) |
| 5 | Strict step decoding and cron schedules | `internal/workflow/step_json.go`, `internal/workflow/schedule.go` | How the `Step` decoder keeps the flat file shape while rejecting unknown, mistyped, other-type, and duplicate keys, and how a cron string plus a zone becomes a `Timetable` with ordered checks. | [05](05-workflow-step-json-and-schedule.md) |
| 6 | The runner: admission, queueing, execution, and saves | `internal/engine/runner.go` | How `Start` makes admission, overlap, capacity, and the initial save one atomic decision under one mutex, how each run goroutine owns a working copy, and how progress saves, the retrying final save, queue promotion, and bounded `Close` fit together. | [06](06-engine-runner.md) |
| 7 | The HTTP and TCP check executors | `internal/nodes/httpcheck/http.go`, `internal/nodes/tcpcheck/tcp.go`, `internal/nodes/resulttext/reason.go` | Why a timeout or refusal is unhealthy data with a nil error, why parent cancellation is checked before the request error, and why headers, proxies, bodies, connections, and reason text are bounded. | [07](07-node-executors.md) |
| 8 | Opening SQLite, the write check, and startup reconciliation | `internal/store/sqlite.go`, `internal/store/writable.go`, `internal/store/run_interruptions.go` | How `Open` creates files privately, uses `mode=rw`, limits the pool to one connection, and pings; how `CheckWritable` proves one commit; how leftover queued and running runs become interrupted in one transaction. | [08](08-store-open-and-reconcile.md) |
| 9 | Schema migrations and recognizing a Patchbay database | `internal/store/schema.go`, `internal/store/identity.go` | How one transaction reads `application_id` and `user_version`, refuses foreign, newer, non-empty, or wrongly shaped files, verifies the layout, and applies versions 1 to 3 with the PTBY marker. | [09](09-store-schema-and-identity.md) |
| 10 | Saving and reading runs | `internal/store/runs.go`, `internal/store/run_reads.go` | How an `engine.Run` becomes rows in `runs` and `run_steps` inside one transaction, why `UpdateRun` names four columns and matches steps by position, and why reads return a complete value or nothing. | [10](10-store-runs.md) |
| 11 | The HTTP API and the request-host guard | `internal/httpapi/api.go`, `internal/httpapi/host_guard.go` | How five routes map to `runner.Start`, `store.ListRuns`, `store.GetRun`, and `NextRun`, how each sentinel error becomes a status code, why the start handler ignores the request context, and why the Content-Type rule and Host allowlist protect a local server. | [11](11-httpapi.md) |
| 12 | The cron scheduler | `internal/schedule/scheduler.go` | How one goroutine per scheduled workflow waits on a cancelable timer, calls the same `Runner.Start` as the API, logs busy and failed starts without retrying, skips missed occurrences, and stops before the runner. | [12](12-scheduler.md) |
| 13 | Frontend entry, routing, and shared workspace state | `web/index.html`, `web/src/main.tsx`, `web/src/router.tsx`, `web/src/App.tsx`, `web/src/workspace.ts`, `web/src/pages/NotFoundPage.tsx`, `web/src/pages/ErrorPage.tsx` | How the page mounts, why the selected workflow and drafts live in `App` while the open view lives in the URL, and how request errors, render errors, and unknown routes reach the user by three separate paths. | [13](13-web-app-shell-and-routing.md) |
| 14 | Talking to the API: types, request helper, and query defaults | `web/src/api.ts`, `web/src/queryClient.ts` | How `api.ts` mirrors Go's JSON shapes field by field, how `request` turns any non-2xx status into a rejected promise carrying the server's text, and why the query defaults fail fast, poll in the background, and ignore the offline flag. | [14](14-web-api-and-query-client.md) |
| 15 | The Workflows page: polling, starting runs, selection | `web/src/pages/WorkflowsPage.tsx` | How two one-second polls, one POST with the required content type, and a cache patch drive the page, where each piece of state lives, and why the Run button trusts the server's admission check. | [15](15-web-workflows-page.md) |
| 16 | Sidebar, step list, history, run details, and view tabs | `web/src/components/Sidebar.tsx`, `web/src/components/StepList.tsx`, `web/src/components/ExecutionHistory.tsx`, `web/src/components/RunDetails.tsx`, `web/src/components/ViewTabs.tsx` | How five stateless components render arrays and callbacks from the page, and which backend rule each badge, banner, and note makes visible, including the memory-only `finalSaveFailed` warning. | [16](16-web-run-view-components.md) |
| 17 | Small presentational components and formatting helpers | `web/src/components/AppHeader.tsx`, `web/src/components/Breadcrumb.tsx`, `web/src/components/PageHeading.tsx`, `web/src/components/Badge.tsx`, `web/src/components/StatusBadge.tsx`, `web/src/components/ErrorBanner.tsx`, `web/src/components/JsonDetails.tsx`, `web/src/components/ScrollColumn.tsx`, `web/src/components/classes.ts`, `web/src/format.ts` | Which Go field each display string describes, which Playwright assertion makes it a contract, and where the little frontend state that exists lives. | [17](17-web-chrome-components-and-format.md) |
| 18 | The canvas: drafts, React Flow, and the New workflow page | `web/src/canvasDraft.ts`, `web/src/components/WorkflowCanvas.tsx`, `web/src/components/CanvasNode.tsx`, `web/src/pages/CanvasPage.tsx` | How the canvas is a controlled React Flow surface whose nodes live in `App`, flow down as a prop, and flow back up as updater functions, with no request ever reaching Go. | [18](18-web-canvas.md) |
| 19 | The stylesheet: Tailwind theme tokens and React Flow bindings | `web/src/index.css` | How one file holds the Tailwind entry and layer order, a role-named palette, the Preflight defaults the layout restores, and the React Flow variable bindings, and which component consumes each token. | [19](19-web-styles.md) |
| 20 | Tests: workflow validation, step decoding, schedules | `internal/workflow/workflow_test.go`, `internal/workflow/tcp_test.go`, `internal/workflow/schedule_test.go` | How each assertion pins the 64 KiB limit, a validation rule, a decoder guard, or a schedule case, using temporary directories, documentation IP ranges, and fixed instants instead of live services. | [20](20-tests-workflow.md) |
| 21 | Tests: runner execution, persistence callbacks, and progress saves | `internal/engine/runner_test.go`, `internal/engine/runner_persistence_test.go`, `internal/engine/runner_updates_test.go` | How closures, channel handshakes, and the `synctest` fake clock prove sequential steps, save ordering, failed saves that stop later steps, the `finalSaveFailed` flag, and shutdown's fresh save context. | [21](21-tests-engine-execution-and-saves.md) |
| 22 | Tests: runner concurrency, queueing, and shutdown | `internal/engine/runner_concurrency_test.go`, `internal/engine/runner_queue_test.go`, `internal/engine/runner_shutdown_test.go` | How eleven tests pin admission under the mutex, queued entries owning no goroutine, slot release after the final save, FIFO promotion, concurrent `Close`, and which outcomes survive shutdown. | [22](22-tests-engine-concurrency-queue-shutdown.md) |
| 23 | Tests: HTTP and TCP executors and result text | `internal/nodes/httpcheck/http_test.go`, `internal/nodes/httpcheck/http_limits_test.go`, `internal/nodes/httpcheck/http_proxy_test.go`, `internal/nodes/tcpcheck/tcp_test.go`, `internal/nodes/resulttext/reason_test.go` | How loopback servers, hijacked connections, a child process with a scrubbed environment, fake dials, `net.Pipe`, and `synctest` prove the healthy, unhealthy, and canceled outcomes and the text limit. | [23](23-tests-nodes.md) |
| 24 | Tests: opening, permissions, migrations, identity, write check | `internal/store/sqlite_test.go`, `internal/store/sqlite_permissions_test.go`, `internal/store/schema_test.go`, `internal/store/identity_test.go`, `internal/store/writable_test.go` | How umask changes, dangling symlinks, canceled contexts, near-miss schemas, `query_only`, and a held connection prove that each startup guard writes nothing when it refuses. | [24](24-tests-store-open-schema-identity.md) |
| 25 | Tests: run persistence, reads, lists, interruptions, and integration | `internal/store/runs_test.go`, `internal/store/run_reads_test.go`, `internal/store/run_list_test.go`, `internal/store/run_updates_test.go`, `internal/store/run_queue_test.go`, `internal/store/run_interruptions_test.go`, `internal/store/tcp_results_test.go`, `internal/store/runner_integration_test.go` | How tests prove atomic inserts and rollbacks, ordering and limits, position-matched updates, interruption cleanup, the version 2 to 3 migration, and the real runner wired to SQLite. | [25](25-tests-store-runs.md) |
| 26 | Tests: API responses, history overlay, host guard, schedules | `internal/httpapi/api_test.go`, `internal/httpapi/history_test.go`, `internal/httpapi/host_guard_test.go`, `internal/httpapi/schedule_test.go` | How tests pin every status code and error body, request-independent run lifetime, the overlay that keeps SQLite order and still surfaces read failures, log capture, and each host guard branch. | [26](26-tests-httpapi.md) |
| 27 | Tests: scheduler timing and server startup | `internal/schedule/scheduler_test.go`, `cmd/server/main_test.go`, `cmd/server/checks_test.go`, `cmd/server/database_lock_test.go`, `cmd/server/database_write_test.go` | How a channel-based fake clock proves the cron loop, and how a port-less listen address drives `runServer` far enough to prove lock, open, write check, and cleanup order and lock release on every failure path. | [27](27-tests-scheduler-and-server.md) |
| 28 | Tests: Playwright end-to-end checks | `web/tests/workflows.spec.ts`, `web/tests/tcp.spec.ts`, `web/playwright.config.ts`, `web/tests/fixtures/workflows/checks.json` | How one test drives the real click-to-SQLite path against a fresh database while eleven others inject queued, canceled, interrupted, save-failure, and TCP states at the network layer. | [28](28-tests-web-e2e.md) |

## Trace one run across the chapters

Follow one click of **Run workflow** on a saved workflow. Every line number below was checked with `cat -n` at commit `1c4786e`.

1. **The click** (chapter 15). `web/src/pages/WorkflowsPage.tsx:176-185` renders the button; `onClick={startRun}` is at `WorkflowsPage.tsx:179`. `startRun` at `WorkflowsPage.tsx:91-94` returns early for a local draft and calls `startMutation.mutate(savedWorkflow.id)` at `WorkflowsPage.tsx:93`.
2. **The POST** (chapters 15, 14, 1). The mutation at `WorkflowsPage.tsx:54-59` calls `request<Run>` with `method: 'POST'` and `Content-Type: application/json` (`WorkflowsPage.tsx:56-58`). `web/src/api.ts:66` calls `fetch` on `/api` plus the path. `api.ts:68-73` turn a non-2xx response into a thrown `Error` carrying the server's `error` text. `api.ts:78` returns the parsed body. In development Vite forwards `/api` to Go: `web/vite.config.ts:11`.
3. **The host guard** (chapters 3, 11). `cmd/server/main.go:123` wraps the whole API in `hostGuard.Wrap(...)`. `internal/httpapi/host_guard.go:39-50`: `host_guard.go:41` normalizes `r.Host`, `host_guard.go:42-46` answer 403 for a host that is not on the allowlist, and `host_guard.go:48` passes an allowed request on.
4. **The API handler** (chapter 11). `internal/httpapi/api.go:50` registers `POST /api/workflows/{id}/runs`. `api.go:53-56` require the JSON content type (415). `api.go:57-60` reject a body (400). `api.go:61-64` find the loaded definition by id. `api.go:65` calls `runner.Start(definition)`. `api.go:66-82` map errors: 409 for a busy workflow, 429 for capacity, 503 when closed, 500 with a fixed message when the save failed. `api.go:83-84` set `Location` and write 202 with the run.
5. **Admission and the initial save** (chapters 6, 3, 10). `internal/engine/runner.go:105` is `Start`. `runner.go:106` validates again. `runner.go:109-113` copy the steps and the schedule. `runner.go:114-115` take the mutex. `runner.go:116-125` check closed, busy workflow, and capacity. `runner.go:126-135` build the run; `runner.go:131` makes it `running` when a slot is free, otherwise it stays `queued`. `runner.go:139-140` save it through `recordNewRun` under a five-second context derived from the runner's own context, not the request. `runner.go:142-144` return `ErrRecordRun` on failure and register nothing. `runner.go:146-149` register the run and trim history. `runner.go:151-155` append to the queue or launch. `runner.go:156` returns a copy. The save callback is the closure at `cmd/server/main.go:107-108`, which calls `store.CreateRun` at `internal/store/runs.go:17`: `runs.go:23` begins a transaction, `runs.go:29-33` insert the `runs` row with the definition JSON, `runs.go:38-56` insert one `run_steps` row per step, `runs.go:58` commits.
6. **The goroutine** (chapter 6). `runner.go:194-198` is `launchLocked`: `runner.go:195` reserves the slot, `runner.go:196` adds to the wait group, `runner.go:197` starts `go r.run(...)`. `run` begins at `runner.go:216`. `runner.go:219` takes the goroutine's own copy. `runner.go:220-223` flip a queued run to running. `runner.go:225` opens the step loop.
7. **The step-start save** (chapters 6, 10). `runner.go:230-232` mark the step `running` with a start time. `runner.go:233` calls `saveRunUpdate`, defined at `runner.go:346-357` with a five-second timeout at `runner.go:350` and the callback at `runner.go:352`. That callback is `cmd/server/main.go:109-110`, which calls `store.UpdateRun` at `internal/store/runs.go:68`: `runs.go:75-77` update the `runs` row, `runs.go:89-94` check the step count, `runs.go:106-110` update each `run_steps` row by position, `runs.go:121` commits. A failed save at `runner.go:233-243` stops the run before the executor runs. `runner.go:244` publishes the snapshot for readers.
8. **The executor** (chapters 6, 3, 7). `runner.go:247` calls `r.execute(r.ctx, step)`. That is the switch at `cmd/server/main.go:151-161`; `main.go:153-157` dispatch by `step.Type`. For HTTP, `internal/nodes/httpcheck/http.go:37` is `Execute`: `http.go:40` makes the child timeout context, `http.go:42` builds the request, `http.go:48` sends it, `http.go:50-55` return the parent's error when shutdown canceled the run, `http.go:56-63` return an unhealthy result with a nil error when the request failed, `http.go:66-71` compare the status code. For TCP, `internal/nodes/tcpcheck/tcp.go:27` is `Execute` and `tcp.go:36` dials.
9. **The result save** (chapters 6, 10). `runner.go:248-258` record the finish time and the output or the error. `runner.go:261` notes whether this was the last step and it succeeded. `runner.go:262-268` break when shutdown began. `runner.go:269` saves the result through `saveRunUpdate` again; `runner.go:275-276` turn a failed result save into a failed run with a readable reason. `runner.go:281` publishes. `runner.go:282-284` stop the loop after an executor error.
10. **The final save** (chapter 6). `runner.go:286` calls `finishRun` with `context.WithoutCancel(r.ctx)`. `finishRun` at `runner.go:300-314`: `runner.go:302` set the status and finish time, `runner.go:303-307` mark pending steps skipped, `runner.go:309` call `saveFinalRun`. `saveFinalRun` at `runner.go:316-344`: `runner.go:322` gives one five-second budget, `runner.go:325` allows three attempts, `runner.go:327-333` wait 100 ms then 200 ms, `runner.go:338` calls the same `UpdateRun` callback. On failure `runner.go:310-311` set `FinalSaveFailed` in memory and log. Back in `run`, `runner.go:287-297` publish the result, release the busy mark, free the slot, promote the queue head, and trim.
11. **The poll** (chapters 15, 14, 11, 10). `WorkflowsPage.tsx:47-51` poll `GET /api/runs` every second; `web/src/queryClient.ts:7-11` set no retries, background polling, and `networkMode: 'always'`. Before the first poll lands, `WorkflowsPage.tsx:60-67` patched the cache with the 202 body and selected the new run id. On the server, `api.go:89-102` handle the list: `api.go:92` calls `store.ListRuns(ctx, db, 100)` at `internal/store/run_reads.go:29`, where `run_reads.go:33` begins a read transaction, `run_reads.go:39-40` select ids newest first, and `run_reads.go:62-68` read each run. `api.go:98-100` overlay retained unsaved results through `preferUnsavedRun` at `api.go:144-149`.
12. **The render** (chapters 15, 16, 17). `WorkflowsPage.tsx:81` filters the list for the selected workflow and `WorkflowsPage.tsx:82` picks the inspected run. `WorkflowsPage.tsx:193-197` render `ExecutionHistory`. `web/src/components/ExecutionHistory.tsx:47-72` draw one button per run with a `StatusBadge` at `ExecutionHistory.tsx:55` and the "Final result not saved" note at `ExecutionHistory.tsx:66-70`; `ExecutionHistory.tsx:74` renders `RunDetails`. `web/src/components/RunDetails.tsx:16` shows the run status, `RunDetails.tsx:19-20` show timing and summary (`runTiming` is `web/src/format.ts:94`), `RunDetails.tsx:22` shows a run-level error, `RunDetails.tsx:23-28` show the final-save warning, `RunDetails.tsx:29-31` render one `StepResult` per step, `RunDetails.tsx:52-54` pick the Healthy or Unhealthy badge, `RunDetails.tsx:69` prints the reason, and `RunDetails.tsx:85` prints the duration.

## Trace a scheduled run

Follow one cron occurrence of a workflow whose file carries a `schedule` object.

1. **Wiring** (chapter 3). `cmd/server/main.go:116` builds the scheduler with `schedule.New(definitions, runner.Start, schedule.SystemClock())`. `main.go:130` calls `scheduler.Start()` after the listener goroutine starts.
2. **The timetable** (chapters 12, 5). `internal/schedule/scheduler.go:53-73` is `New`: `scheduler.go:58-61` skip definitions without a schedule, `scheduler.go:62` calls `workflow.NewTimetable`. `internal/workflow/schedule.go:29-56`: `schedule.go:30-34` reject an empty expression or a `TZ=` prefix, `schedule.go:36` parses with `cron.ParseStandard`, `schedule.go:40-45` require and load an explicit zone, `schedule.go:48-50` require an occurrence within five years, `schedule.go:52-54` require occurrences at least a minute apart. `Timetable.Next` at `schedule.go:60-62` converts the instant into the schedule's zone before asking cron.
3. **The first occurrence** (chapter 12). `scheduler.go:78-94` is `Start`: `scheduler.go:85` reads the clock, `scheduler.go:87` computes `Next(now)`, `scheduler.go:88` stores it for `NextRun`, `scheduler.go:91-92` add to the wait group and start `loop`. Occurrences that passed while the process was down are never computed.
4. **Waiting** (chapter 12). `scheduler.go:112-145` is `loop`. `scheduler.go:115` runs while an occurrence exists. `scheduler.go:116-120` wait on either the scheduler's `ctx.Done()` or the clock's timer. `scheduler.go:121-124` check the context again after the timer, so a `Close` that raced with the timer admits nothing.
5. **The start** (chapters 12, 6). `scheduler.go:125` calls `s.start(e.definition)`, which is `runner.Start` at `internal/engine/runner.go:105`. From here the path is step 5 of the manual trace: validate, lock, busy check, capacity check, initial save, launch. The scheduler never executes a step itself.
6. **When the workflow is busy** (chapters 6, 12). If the previous run is still queued or running, `runner.go:119-121` return `ErrWorkflowBusy` before anything is saved. `scheduler.go:129-130` log "scheduled run skipped while the previous run is active". Any other error, such as capacity, a closed runner, or a failed initial save, reaches `scheduler.go:131-132` and is logged as a warning. Nothing is retried.
7. **The next occurrence** (chapter 12). `scheduler.go:135-139` compute the next occurrence from the current time, clamped to the occurrence just handled at `scheduler.go:136-138`, so occurrences that passed during a long wait or a backward clock step are skipped and never doubled. `scheduler.go:140-142` publish it under the mutex.
8. **What the API and UI show** (chapters 11, 15, 17). `internal/httpapi/api.go:41-45` add `nextRunAt` to each workflow by calling `NextRun` at `scheduler.go:105-110`. `WorkflowsPage.tsx:83` and `scheduleSummary` at `web/src/format.ts:29` print the cron expression, zone, and next run.
9. **Shutdown** (chapters 3, 12). `main.go:139` calls `scheduler.Close()` (`scheduler.go:98-101`: cancel, then wait) before `runner.Close()` at `main.go:140`, so every loop exits at `scheduler.go:117` or `scheduler.go:121-124` and asks the runner for nothing more.

## Trace a restart

Follow a run that is executing a step when the process receives SIGTERM, and then watch the next startup.

1. **The signal** (chapter 3). `cmd/server/main.go:38` turns SIGINT and SIGTERM into a canceled context. `main.go:137-138` wake the `select` and log "shutting down".
2. **Scheduler first** (chapters 3, 12). `main.go:139` calls `scheduler.Close()`; `internal/schedule/scheduler.go:99-100` cancel the loops and wait for them.
3. **Runner** (chapters 3, 6). `main.go:140` calls `runner.Close()`. `internal/engine/runner.go:176-191`: `runner.go:178` refuses new starts, `runner.go:179` cancels the runner context, `runner.go:180-181` take the queue, `runner.go:184` and `runner.go:188` arrange queued-run cleanup, `runner.go:190` waits for every run goroutine.
4. **The running step** (chapters 7, 6). The executor sees the canceled context: `internal/nodes/httpcheck/http.go:50-55` return `ctx.Err()`. `runner.go:250-255` record the step as `canceled`. `runner.go:262-268` break out of the loop; a last step that already returned a result keeps `succeeded` because of `runner.go:261`. If the loop was between steps, `runner.go:226-229` set `canceled` before touching the next step.
5. **The final-save budget** (chapter 6). `runner.go:286` calls `finishRun` with `context.WithoutCancel(r.ctx)`, so the save ignores the cancellation that just happened. `runner.go:322` gives five fresh seconds for up to three attempts. Queued runs share one five-second budget created at `runner.go:203`. If every attempt fails, `runner.go:310-311` set `FinalSaveFailed` in memory only. That memory disappears when the process exits, and SQLite keeps the last progress snapshot with status `running`.
6. **HTTP, SQLite, and the lock** (chapters 3, 1). `main.go:141-145` give `server.Shutdown` five seconds. Then the deferred calls run in reverse, as the comment at `main.go:79` says: `main.go:127` closes the server, `main.go:121` and `main.go:115` close the scheduler and runner again (both are safe to repeat), `main.go:80-84` close SQLite, `main.go:70-74` release the lock file. Compose allows 20 seconds for all of this: `compose.yaml:10`.
7. **Startup: flags, host guard, workflows** (chapter 3). `main.go:48-53` validate the limits, `main.go:54-57` build the host allowlist, `main.go:58-61` load and validate every workflow file, `main.go:62-64` create the database directory.
8. **Startup: the lock** (chapter 3). `main.go:66` calls `lockDatabase`. `cmd/server/database_lock.go:22-43` resolve the path and any symlink, `database_lock.go:47` opens `<database>.lock`, `database_lock.go:51` takes an exclusive non-blocking flock, `database_lock.go:53-54` report `errDatabaseInUse` when another server holds it.
9. **Startup: open and migrate** (chapters 8, 9). `main.go:75` calls `store.Open`. `internal/store/sqlite.go:34` creates a missing file privately with `O_EXCL` and mode 0600, `sqlite.go:44-52` build the `file:` URI with `mode=rw` and the per-connection pragmas, `sqlite.go:59` limits the pool to one connection, `sqlite.go:63` pings, `sqlite.go:67` calls `migrate`. `internal/store/schema.go:56-118`: `schema.go:57` begins the transaction, `schema.go:64-69` read and check `application_id`, `schema.go:70-75` read and check `user_version`, `schema.go:76-86` require an empty unmarked file or call `recognizeSchema` (`internal/store/identity.go:12`), `schema.go:87-89` return without writing when the file is current and marked, `schema.go:92-106` apply versions 1 to 3, `schema.go:109-113` write the PTBY marker, `schema.go:114` commits.
10. **Startup: the write check** (chapter 8). `main.go:87-92` give `store.CheckWritable` five seconds. `internal/store/writable.go:17` commits one autocommit `PRAGMA main.user_version` statement, which proves the journal and commit path work even when the migration had nothing to write.
11. **Startup: interruption cleanup** (chapter 8). `main.go:95-103` give `store.MarkUnfinishedRunsInterrupted` five seconds and log the count. `internal/store/run_interruptions.go:16` begins the transaction, `run_interruptions.go:24-27` turn running steps of unfinished runs into `interrupted` and pending steps into `skipped`, `run_interruptions.go:31-32` turn queued and running runs into `interrupted`, `run_interruptions.go:40` commits. The run from step 5 now reads `interrupted` with its completed step outputs intact and no finish time.
12. **Startup: runner, scheduler, HTTP** (chapters 3, 17). `main.go:105-131` build the executors, the runner, the scheduler, and the server, then start listening and start the scheduler. Nothing resumes the interrupted run. The UI shows **Interrupted** with "Finish time unknown" (`web/src/format.ts:101`) for a run that had started, or "Never started" (`format.ts:96`) for a run that was still queued.

## Glossary

Every concept the chapters introduce, listed once with the first chapter in reading order that explains it. Later chapters repeat a concept only when a file uses it in a new way.

### Go language

- **`package main` and `func main`** (ch. 3): where a program starts; returning from `main` exits with code 0.
- **Package doc comments and exported names** (ch. 4): a comment starting with "Package x" documents the package; capitalized names are exported, lowercase names stay private to the package and its tests.
- **`internal/` directories** (ch. 4): Go refuses imports of `patchbay/internal/...` from any other module, so the whole backend is private to this repository.
- **One package namespace across files** (ch. 8): every file in a directory shares one package, so `schemaVersion` from `schema.go` is visible in `writable.go`.
- **Import direction and import cycles** (ch. 10): `store` imports `engine` for the `Run` type; `engine` declares function types that `main` fills with `store` calls, so there is no cycle.
- **Third-party modules and the `/v3` suffix** (ch. 5): `github.com/robfig/cron/v3` is pinned in `go.mod` and grouped separately from standard imports.
- **Blank imports** (ch. 1): `import _ "pkg"` runs the package's `init` side effects only; Patchbay uses it to register the SQLite driver and embed the time-zone database.
- **Build constraints (build tags)** (ch. 1): a `//go:build` line includes a file only for matching platforms; the lock file compiles only on Linux and macOS.
- **Typed and untyped constants** (ch. 4): `const maxDefinitionBytes = 64 * 1024` is computed at compile time and adapts its type to its use; `minimumInterval = time.Minute` carries a type.
- **Hexadecimal literals** (ch. 9): `0x50544259` spells the ASCII bytes P T B Y.
- **Zero values** (ch. 4): every variable starts as `0`, `""`, `false`, `nil`, or a zero `time.Time`; `Run{}` means "no run".
- **Pointers versus values** (ch. 3): a copy of a struct is independent; a pointer refers to one shared value and can express "not set" with `nil`.
- **Passing structs by value** (ch. 4): a copied struct still shares its slice elements and pointer targets, which is safe only when the callee reads.
- **Struct literals** (ch. 7): keyed literals name fields, positional literals list all fields in order (ch. 9), and `&T{}` builds a value and takes its address (ch. 12).
- **Comparable structs** (ch. 4): a struct of only comparable fields supports `==` and `!=`, so tests can write `got != original`.
- **Anonymous struct types** (ch. 5): unnamed structs inside a function act as per-type allowlists of legal JSON keys.
- **Interface-typed and unexported struct fields** (ch. 5): `Timetable` holds a `cron.Schedule` and a `*time.Location`; lowercase fields make the constructor the only way to build a usable value.
- **Returning a zero value with an error** (ch. 5): every failure returns `Timetable{}, err` by convention.
- **Multiple return values and the `(value, error)` contract** (ch. 7): `Execute` returns `(CheckResult, error)`; a nil error with `Healthy: false` is a successful check of a down service.
- **Errors as values, `fmt.Errorf`, and `%w`** (ch. 2): functions return `error` last; `%w` wraps a cause so `errors.Is` still finds it after context is added.
- **Sentinel errors and `errors.Is`** (ch. 3): fixed error values such as `errDatabaseInUse` survive wrapping so callers and tests can match them.
- **`errors.Join`** (ch. 6): combines a storage error and a context error into one value that `errors.Is` matches against either.
- **`errors.As`** (ch. 25): finds a typed error such as `*json.SyntaxError` or `*net.OpError` through a wrap chain.
- **fmt verbs `%q`, `%v`, `%+v`** (ch. 4): `%q` quotes a value in a message; `%v` and `%+v` print full values in test failures.
- **`if` with a short statement and the blank identifier** (ch. 4): `if _, err := f(); err != nil` scopes `err` to the `if` and discards an unneeded result.
- **`if`/`else if` chains with init statements** (ch. 3): a variable declared in the `if` header is visible in its `else if` branches.
- **`=` versus `:=` and shadowing** (ch. 3): `:=` declares a new variable in the current block and hides an outer one of the same name; `database_lock.go` uses `var` to avoid that.
- **Tagless `switch`** (ch. 3): `switch { case cond: }` is an `if`/`else` chain.
- **`switch` on a string with `default`** (ch. 4): the first matching case runs with no fall-through; `default` catches unsupported step types.
- **`for range` over slices and strings** (ch. 4): ranging a slice yields element copies; `for i := range` plus indexing edits the real element; ranging a string yields runes.
- **`for` with only a condition** (ch. 12): Go's while loop keeps the occurrence loop running until `Next` returns the zero time.
- **`for range N` over an integer** (ch. 21): Go 1.22 lets `for i := range 101` count and `for range 10` repeat without a variable.
- **Per-iteration loop variables** (ch. 20): since Go 1.22 each iteration has its own variable, so closures capture the right row.
- **`break` inside `for`** (ch. 6): every stop condition in the step loop breaks so finalization is written once after the loop.
- **Tuple assignment** (ch. 6): `run.Status, run.StartedAt = "running", run.CreatedAt` sets two fields in one statement.
- **Build locally, assign at the end** (ch. 5): fill a local `Step` and write `*s` only on success, leaving the caller's value untouched on failure.
- **Maps as sets and the comma-ok form** (ch. 4): `map[string]bool` or `map[string]struct{}` is the idiomatic set; `v, ok := m[k]` tells whether a key exists (ch. 6).
- **Nil slices versus empty slices** (ch. 4): `make([]T, 0)` is empty but non-nil; JSON writes a nil slice as `null` and an empty one as `[]`.
- **Slices share backing arrays; `slices.Clone`, `Delete`, `IndexFunc`, `append`** (ch. 6): copying a slice header shares elements, which is why `Start` clones steps and `copyRun` exists.
- **Slicing a slice literal** (ch. 22): `[]string{"one","two"}[:n]` yields an expected prefix.
- **Arrays versus slices for comparison** (ch. 21): `[3]string` arrays compare element-wise, so a slice of them `DeepEqual`s cleanly.
- **Short-circuit `||` and operator precedence** (ch. 9): `count >= len(expected) || column != expected[count]` guards the index first; `&&` binds tighter than `||` (ch. 11).
- **Strings, bytes, and runes** (ch. 4): `len` and `s[0]` count bytes; `range` yields runes.
- **UTF-8 validity and truncation** (ch. 7): `strings.ToValidUTF8` and `utf8.RuneStart` let `LimitReason` cut on character boundaries.
- **Raw string literals** (ch. 8): backtick strings span lines with no escapes, so SQL reads as it would in a shell.
- **Function literals (closures)** (ch. 3): an inline `func` captures surrounding variables; the save callbacks capture `db` so `engine` never imports `store`.
- **Named function types as dependencies** (ch. 3): `engine.ExecuteStep` and friends let tests substitute fakes without a plugin framework.
- **Method values** (ch. 3): `runner.Start` passed as a function so cron and the API share one admission path.
- **Methods and pointer receivers** (ch. 5): `Step.UnmarshalJSON` uses a pointer receiver to assign `*s`; `Runner` methods use `*Runner` because the struct holds a mutex.
- **Constructors (`New` functions)** (ch. 7): a `New` returning `*Executor` so every call shares one client or dialer.
- **Implicit interfaces** (ch. 3): `*schedule.Scheduler` satisfies `httpapi.NextRunSource` by having `NextRun`; a typed nil pointer inside an interface is not nil.
- **Methods on named map types** (ch. 26): a map with one method satisfies an interface, which is how a test fakes the scheduler.
- **Type assertions** (ch. 5): `token.(string)` with the ok form, and `http.DefaultTransport.(*http.Transport)` to recover a concrete type (ch. 7).
- **`any`, `new`, and the `io.EOF` sentinel** (ch. 4): `new(any)` is a catch-all decode target; a second `Decode` must hit `io.EOF` to prove one document.
- **`defer` and LIFO order** (ch. 3): deferred calls run in reverse, which is how the lock is released after SQLite and the runner; a `defer` inside a loop runs at function exit (ch. 4).
- **`os.Exit` skips deferred calls** (ch. 3): `main` exits only after `runServer` has returned and released everything.
- **Pointers to local variables** (ch. 10): returning `&instant` is safe because escape analysis moves it to the heap.
- **Built-in `min`** (ch. 25): `min(a, b)` slices expectations by limit.

### Goroutines, channels, contexts, and time

- **Goroutines and the `go` statement** (ch. 3): the listener goroutine lets the main goroutine wait on other events.
- **`sync.WaitGroup`** (ch. 6): every `wg.Add` happens under the mutex so `Close`'s `Wait` cannot miss a goroutine; Go 1.25's `wg.Go` starts and tracks one in a call (ch. 21).
- **`sync.Mutex`, `defer Unlock`, and the `Locked` suffix** (ch. 6): one mutex protects six runner fields; `...Locked` helpers assume the caller holds it because Go mutexes are not re-entrant.
- **`sync.Once` and `sync/atomic`** (ch. 22): `Once` signals exactly once across retries; `atomic.Int32` keeps a counter safe under `-race`.
- **Buffered channels** (ch. 3): `make(chan error, 1)` lets the listener's final send complete when nobody receives.
- **Receive-only channel types** (ch. 12): `<-chan time.Time` lets the scheduler wait on a timer it cannot send on or close.
- **Closing a channel as a broadcast** (ch. 21): `close(ch)` releases every current and future receiver; closing twice panics.
- **`select`** (ch. 3): waits on several channel operations and runs the first ready one; it picks randomly among several ready cases (ch. 12).
- **`select` with `time.After` and with `default`** (ch. 21): a timeout case turns a hang into a failed test; a `default` branch is a non-blocking probe.
- **`context.Context` and cancellation** (ch. 3): `signal.NotifyContext`, `WithCancel`, `WithTimeout`, `Done`, `Err`, and calling `cancel` to release timers.
- **Parent and child contexts** (ch. 7): the step deadline is a child of the runner context; only the parent says whether shutdown or the timer ended it.
- **Request contexts versus the runner's context** (ch. 11): history reads use `r.Context()` plus five seconds; the run start deliberately uses none.
- **`context.WithoutCancel`** (ch. 6): gives final saves a fresh budget after shutdown began.
- **Timers: `time.NewTimer` and `time.After`** (ch. 6): deliver once after a duration; retry backoff waits on the timer or the context, whichever comes first.
- **`time.Duration`, `time.Now`, `time.Since`** (ch. 5): typed durations compare without conversion; elapsed milliseconds are measured for every outcome (ch. 7).
- **`time.Time`, `time.Location`, `In`, `IsZero`, `Sub`** (ch. 5): an instant carries a zone; the zero time means no occurrence.
- **IANA time zones and `time/tzdata`** (ch. 2): `time.LoadLocation` resolves names like `Europe/Amsterdam`; the blank import embeds the database for minimal containers.
- **Wall clock versus monotonic clock** (ch. 12): timers use the monotonic reading while cron times are wall-clock only, which is why the loop clamps.
- **`time.Date`, `time.UTC`, `Time.Equal`** (ch. 20): fixed instants control time in tests; `Equal` compares instants across zones where `==` would not.
- **Unix milliseconds and `time.Time`** (ch. 10): `UnixMilli` round trips, `.UTC()` for equality, `IsZero` for unset value times.
- **`Truncate` and the monotonic reading** (ch. 26): `UTC()` and `Truncate` strip the monotonic reading so saved and in-memory times compare equal.
- **Location pointers in `DeepEqual`** (ch. 25): `reflect.DeepEqual` compares the location pointer, so fixtures and store reads must both use `.UTC()`.
- **The Go memory model and happens-before** (ch. 21): reading a slice a callback appended to is safe only after a mutex handoff or `WaitGroup.Wait`, so tests call `Close` before reading.
- **The race detector** (ch. 1): `go test -race` instruments memory access to catch unsynchronized concurrent use.
- **`testing/synctest` fake clock** (ch. 6): a bubble whose clock advances only when every goroutine blocks, so 100 ms and 200 ms backoffs and five-second budgets cost nothing.
- **`crypto/rand.Text`** (ch. 6): Go 1.24 random base32 id with at least 128 bits of randomness, used as the run id.
- **Structured logging with `log/slog`** (ch. 3): key-value log lines such as the startup configuration and the `run_id` on save failures.
- **`log` and `log/slog` defaults** (ch. 26): the default slog handler writes through the `log` package, so `log.SetOutput` captures production lines in tests.

### Files, processes, and the operating system

- **Command-line flags** (ch. 2): `flag.String` and `flag.Int` register flags and return pointers filled by `flag.Parse`.
- **`os.ReadDir`, `fs.DirEntry`, `path/filepath`** (ch. 2): entries load in file-name order, which sets sidebar order.
- **`io.LimitReader` and `io.ReadAll`** (ch. 2): reading limit plus one byte bounds memory and detects oversized files.
- **`bytes.NewReader` and `io.Reader`** (ch. 5): wrapping a byte slice as a stream for `json.NewDecoder`.
- **`os.OpenFile` flags and octal permissions** (ch. 3): `O_CREATE|O_RDWR` and `0o600` for the owner-only lock file.
- **`O_EXCL` and umask** (ch. 8): create-only-if-absent refuses existing files and symlinks; `0o600` minus umask is never more open than owner-only.
- **`filepath.Abs`, `os.Lstat`, `filepath.EvalSymlinks`, `filepath.ToSlash`** (ch. 3): resolving relative paths and symlinks so aliases of one database share one lock; `ToSlash` normalizes for the `file:` URI (ch. 8).
- **`os.Symlink`, `os.Readlink`, `os.SameFile`** (ch. 24): distinguish following a link from replacing it or recreating the file.
- **`os.Stat` and `filepath.Join`** (ch. 11): detect a missing built frontend and build the same path text the loader builds (ch. 20).
- **File descriptors and `syscall.Flock`** (ch. 3): an exclusive non-blocking advisory lock tied to the open file description, released on close or process death.
- **Unix permission bits, `os.Chmod`, `os.Geteuid`** (ch. 27): `0555` on a directory blocks file creation for non-root users, so the test skips as root.
- **Re-running the test binary as a child process** (ch. 23): `os.Args[0]` with `-test.run` and a scrubbed environment tests process-wide state such as proxy caching, umask, and a dying lock owner.
- **Reserved and documentation IP ranges** (ch. 2): `192.168.x.x` is private LAN space; `192.0.2.x` and `2001:db8::` are documentation-only, so fixtures never probe a stranger.

### JSON and validation

- **JSON documents** (ch. 2): one top-level object per file; no comments, so intent goes into the description field.
- **Struct tags for `encoding/json`** (ch. 2): a `json:"key"` tag maps each file key to one field.
- **`omitempty`** (ch. 2): drops a field holding its zero value from output; it cannot distinguish "wrote 0" from "wrote nothing".
- **`omitzero`** (ch. 6): Go 1.24 drops a zero `time.Time` so queued runs show no start time.
- **Strict decoding with `DisallowUnknownFields`** (ch. 2): unknown keys and a second JSON document fail startup instead of being ignored.
- **Custom `UnmarshalJSON`, `json.Unmarshaler`, and `json.RawMessage`** (ch. 2): `Step` reads `type` first, keeps `config` as raw bytes, then decodes it with the field set for that type.
- **Streaming `json.Decoder`: `Token`, `More`, `Delim`** (ch. 5): walking an object's keys token by token while skipping each value with `Decode`.
- **Case-insensitive JSON key matching** (ch. 5): `encoding/json` matches keys case-insensitively, so duplicate detection must lowercase names.
- **`json.Marshal`, `json.Unmarshal`, decoding into `any`** (ch. 20): encode fixtures and compare two JSON texts by meaning through generic maps.
- **`encoding/json` with pointer targets** (ch. 10): `Marshal` follows pointers; `Unmarshal` into a nil pointer allocates the target.
- **`*json.SyntaxError`** (ch. 25): corrupt saved JSON surfaces as a typed error rather than partial history.
- **Struct embedding and JSON flattening** (ch. 11): `workflowSummary` embeds `workflow.Definition` so its fields appear at the top level.
- **`json.NewEncoder(w).Encode`** (ch. 11): streams JSON to the writer and appends a newline, which tests trim.
- **Regular expressions** (ch. 2): `regexp.MustCompile` at package level compiles the identifier pattern once and is safe for concurrent use.
- **`net/url` parsing** (ch. 2): validation requires an absolute http or https URL with a host and no credentials or fragment.
- **`net/netip`** (ch. 4): `ParseAddr` handles IPv4, IPv6, and zones; `Is6`, `Unmap`, and `IsUnspecified` canonicalize host literals (ch. 11).
- **Cron expressions and `robfig/cron`** (ch. 2): five fields, minute first; `*/5` means every fifth minute; `ParseStandard` and a `Next` that searches five years ahead (ch. 5).
- **Daylight saving time in the cron library** (ch. 20): removed wall-clock times are skipped and repeated ones run twice, because `Next` searches the schedule's zone.
- **`strings` helpers** (ch. 5): `TrimSpace`, `Contains`, `Split`, `HasPrefix`, `TrimSuffix`, `ToLower`, `Repeat` on immutable strings.
- **`strconv.ParseUint` and `strconv.Itoa`** (ch. 7): formatting a port, and rejecting empty, non-numeric, or out-of-range ports (ch. 11).

### Storage and SQLite

- **Driver registration (`modernc.org/sqlite`)** (ch. 8): the blank import's `init` registers the name `sqlite` that `sql.Open` looks up.
- **`*sql.DB` is a pool, not a connection** (ch. 8): `SetMaxOpenConns(1)` makes every statement take turns on one connection, so a leaked transaction blocks the next query (ch. 25).
- **`sql.Open` is lazy; `PingContext` is the real test** (ch. 8): a missing file or dangling symlink is reported by the ping.
- **`DB.Conn`** (ch. 24): a pinned connection that makes other callers wait until their context ends.
- **Contexts inside `database/sql`** (ch. 8): every `...Context` method honors cancellation while waiting, running, and until a transaction ends.
- **Transactions and `defer tx.Rollback()`** (ch. 8): `BeginTx` groups statements; the deferred rollback undoes partial work on any early return and is harmless after `Commit`.
- **Transactions as a consistent snapshot** (ch. 10): all statements inside one SQLite transaction, even a read-only one, see the same committed state.
- **Autocommit, explicit transactions, and the rollback journal** (ch. 8): a standalone statement commits itself; any commit needs a journal file beside the database.
- **`?` placeholders versus `fmt.Sprintf`** (ch. 8): pragmas cannot take bound parameters, so a compile-time constant is formatted in; user data goes through `?`; a nil argument stores NULL (ch. 10).
- **`QueryRowContext(...).Scan` and `sql.ErrNoRows`** (ch. 9): single-row reads with positional targets; the sentinel that `%w` keeps reachable.
- **The `Rows` loop: `Next`, `Scan`, `Err`, `Close`** (ch. 9): iterate a cursor, then `rows.Err()` to tell end-of-data from failure.
- **Scanning NULL and `sql.NullInt64`, `sql.NullString`** (ch. 9): `COALESCE` avoids scanning NULL into a string; value-plus-`Valid` structs hold nullable columns (ch. 10).
- **`sql.Result` and `RowsAffected`** (ch. 8): counting matched rows after an UPDATE as an existence check; SQLite counts rows even when values are unchanged.
- **Building a connection string with `net/url`** (ch. 8): `url.URL` and `url.Values` build a `file:` URI that percent-encodes `?` and `#`.
- **SQLite URI filenames and `mode=rw`** (ch. 8): opens an existing file for read and write but never creates one.
- **SQLite pragmas** (ch. 8): `foreign_keys` and `busy_timeout` are per-connection; `user_version` and `application_id` persist in the file header.
- **Header fields `user_version` and `application_id`** (ch. 9): two integers SQLite never changes, used as the migration counter and the Patchbay marker.
- **`PRAGMA data_version` and `query_only`** (ch. 24): a per-connection commit detector and a portable read-only switch for failure injection.
- **`PRAGMA schema_version`** (ch. 25): SQLite's own DDL counter, used to prove a migration changed no table.
- **`PRAGMA journal_mode`** (ch. 27): DELETE mode forces a journal file per commit, so a read-only directory makes a commit fail.
- **`sqlite_schema` and `GLOB` versus `LIKE`** (ch. 9): the object catalog; `GLOB` treats `_` literally so `sqlite_*` excludes only SQLite's own objects.
- **Table-valued pragma functions** (ch. 9): `pragma_table_xinfo`, `pragma_index_list`, and `pragma_foreign_key_list` expose the layout as rows.
- **`ANALYZE` and `sqlite_stat` tables** (ch. 24): SQLite's statistics tables must pass the `sqlite_*` filter.
- **Transactional DDL** (ch. 9): SQLite rolls back `CREATE TABLE` and `ALTER TABLE` with the enclosing transaction.
- **`ALTER TABLE ADD COLUMN` with `NOT NULL DEFAULT`** (ch. 9): adding `runs.error` without rewriting rows.
- **Rowid tables, automatic indexes, `WITHOUT ROWID`, `STRICT`, generated columns** (ch. 9): `PRIMARY KEY` and `UNIQUE` create `sqlite_autoindex_*`; the opt-in modes are rejected by recognition.
- **Foreign keys and `ON DELETE CASCADE`** (ch. 9): enforced only because `sqlite.go` turns `foreign_keys` on per connection.
- **SQLite constraints as a safety net** (ch. 10): `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, and `CHECK` turn bad writes into errors that roll back the transaction.
- **`ORDER BY` with a tie-break and `LIMIT`** (ch. 10): `created_at DESC, id DESC` gives deterministic order.
- **SQL `CASE` expressions and subqueries in `UPDATE`** (ch. 8): pick a new status per row and restrict the rewrite to steps of in-flight runs.
- **SQLite triggers and `RAISE(ABORT)`** (ch. 25): a `BEFORE UPDATE` trigger with a `WHEN` clause injects one specific save failure inside a real database.
- **Shaping fixtures with `strings.Replace` and comparing files with `bytes.Equal`** (ch. 24): near-miss schemas from the real constants, and whole-file equality as proof that a refusal wrote nothing.

### HTTP and networking in Go

- **`http.Handler` and `http.HandlerFunc`** (ch. 11): any value with `ServeHTTP` is a handler; `HandlerFunc` adapts a plain function.
- **Middleware** (ch. 11): a function that takes a handler and returns a wrapping handler; `HostGuard.Wrap` may skip the inner one.
- **`http.ServeMux` method patterns and wildcards** (ch. 11): Go 1.22 patterns like `GET /api/runs/{id}`; `PathValue` reads a wildcard.
- **`ResponseWriter` header ordering** (ch. 11): headers must be set before `WriteHeader` or the first `Write`.
- **`http.FileServer`, `http.Dir`, and `http.Error`** (ch. 11): serves `web/dist` with path cleaning; the plain-text error helper is used only on the frontend path.
- **`http.Server` timeouts and `Shutdown`** (ch. 3): read, write, and idle deadlines, graceful `Shutdown` with a context, and forced `Close`.
- **`http.Client`, `http.Transport`, and `Transport.Clone`** (ch. 7): `Clone` deep-copies settings so proxy and header limits change only this executor.
- **`http.ErrUseLastResponse`** (ch. 7): the `CheckRedirect` sentinel that stops redirect following.
- **`ProxyFromEnvironment` and its cache** (ch. 23): Go reads proxy variables once per process, which is why the proxy rule is tested in a child process.
- **`x509.CertPool` and `tls.Config`** (ch. 23): trusting a fixture's self-signed certificate while keeping verification on.
- **`net.Dialer.DialContext` and `net.JoinHostPort`** (ch. 7): one context covers DNS and connect; `JoinHostPort` brackets IPv6.
- **`net.SplitHostPort`** (ch. 11): splits `host:port` and `[v6]:port`, errors on no port or too many colons.
- **Read-only maps are safe to share** (ch. 11): the allowlist is written only at startup, so handlers read it concurrently without a lock.
- **`net/http/httptest`: `NewRequest`, `NewRecorder`** (ch. 11): in-memory requests and a recording `ResponseWriter` for handler tests.
- **`httptest.Server`** (ch. 23): loopback HTTP and TLS servers with `NewServer`, `StartTLS`, `EnableHTTP2`, and `Close`.
- **`http.Hijacker`** (ch. 23): taking the raw `net.Conn` from a handler so a fixture can write malformed bytes.
- **`net.ListenTCP` on port 0, `AcceptTCP`, and `SetDeadline`** (ch. 23): an ephemeral port for a TCP target, with deadlines that turn a hang into a bounded failure.
- **`net.Pipe` and `io.EOF`** (ch. 23): an in-memory connection pair that proves the executor closed its socket.

### Testing in Go

- **Internal (same-package) test packages** (ch. 7): `_test.go` files in the production package reach unexported fields and helpers.
- **`testing.T`: `t.Fatal`, `t.Fatalf`, `t.TempDir`** (ch. 20): fail and stop; per-test directories deleted automatically.
- **`t.Error`, `t.Helper`, `t.Run`** (ch. 21): `Error` continues (needed from callbacks); `Helper` fixes reported lines; `Run` makes named subtests.
- **`t.Setenv`** (ch. 23): scoped environment variables.
- **`t.Cleanup`** (ch. 26): cleanups run even after `Fatal`, in reverse order.
- **`t.Skip`** (ch. 27): skip a test when the environment cannot exercise it, such as running as root.
- **Table-driven tests and subtests** (ch. 4): cases live in a slice and run under `t.Run`, so `go test -run` can target one.
- **Function values and closures as table entries** (ch. 20): a `func(*Definition)` per row applies one edit through a pointer.
- **Closures as fakes** (ch. 21): every fake executor and save callback counts calls, records arguments, blocks on a channel, or fails on the Nth call.
- **A struct of functions as a test seam** (ch. 12): `Clock` holds `Now` and `After` as fields so tests substitute a fake clock.
- **A fake clock made of channels** (ch. 27): the fake `After` returns a channel the test holds the sending end of, so the test decides when a tick arrives.
- **Context cancellation inside a fake** (ch. 21): waiting on `ctx.Done()` makes a fake honor cancellation and guarantees it exits on shutdown.
- **Pre-canceled contexts as failure injection** (ch. 24): `cancel()` right after `WithCancel` proves a function stops before writing.
- **`reflect.DeepEqual`** (ch. 20): field-by-field comparison that follows pointers and walks slices.
- **Selecting tests with `go test -run`** (ch. 20): a regular expression over `Test/subtest` names, with `-v` to list each.
- **`t.TempDir`, `os.WriteFile`, octal file modes** (ch. 20): per-test files; `0600` and `0o600` are the same owner-only mode.
- **Composite literals for partial fakes** (ch. 21): `&Runner{updateSavedRun: f}` builds a runner with one field set to call private methods directly.

### TypeScript

- **ES modules** (ch. 1): `"type": "module"` makes config files load as ES modules; `export` and `import` connect source files (ch. 14).
- **`import type` under `verbatimModuleSyntax`** (ch. 13): type-only imports must carry the keyword so Vite can drop them without analysis.
- **TypeScript compiler options** (ch. 1): `strict`, `noEmit`, Bundler resolution, `react-jsx`, and `verbatimModuleSyntax` make `tsc` a checker that matches how Vite compiles each file alone.
- **Type aliases and object types** (ch. 14): naming an object shape so the compiler checks every value by that name; erased at build time.
- **Literal types and unions** (ch. 14): a type whose only values are exact strings such as `'queued' | 'running'`.
- **Discriminated unions and narrowing** (ch. 2): `WorkflowStep`'s `type` field tells the UI which config shape to read, mirroring the Go switch.
- **Intersection types** (ch. 14): `A & B` combines shared fields with a union of type-specific fields.
- **Optional properties** (ch. 14): a `?` on a field means the key may be absent; each one in `api.ts` mirrors a Go `omitempty` or `omitzero` tag.
- **Generics** (ch. 14): `request<T>` lets the caller name the response shape; generic type arguments also appear in JSX as `<ReactFlow<DraftNode>>` (ch. 18).
- **Type assertions (`as`)** (ch. 14): trusts outside data without a runtime check.
- **Non-null assertion (`!`)** (ch. 13): a compile-time promise backed by `index.html`, not a runtime check.
- **`Record`, `Dispatch<SetStateAction<T>>`, and `'in'` narrowing** (ch. 13): the setter type, the id-to-draft map, and `'steps' in workflow` to split saved workflows from drafts.
- **Indexed access types** (ch. 17): `Run['status']` reads the union from `api.ts` so a new status fails compilation until styled.
- **`unknown` and `instanceof` narrowing** (ch. 17): `errorMessage` narrows `unknown` with `instanceof Error`.
- **Optional chaining and nullish coalescing** (ch. 14): `?.` avoids throwing on null; `??` substitutes only for null or undefined.
- **Template literals and array predicates** (ch. 17): embedded expressions, `some`, and `filter` throughout `format.ts`.
- **Immutable updates with spread syntax** (ch. 18): state objects and arrays are copied and replaced, never mutated, because React detects change by reference.
- **Default values in destructuring** (ch. 17): decides what an omitted optional prop becomes.
- **Promises, `async`, and `await`** (ch. 14): an `async` function returns a Promise; `throw` inside it rejects the Promise.
- **The `void` operator on a promise** (ch. 13): `void navigate(...)` discards the returned promise on purpose.
- **`fetch`, `Response`, and `RequestInit`** (ch. 14): `fetch` resolves on any HTTP status and rejects only on network failure or abort, so `response.ok` must be checked.
- **`Error` and `throw`** (ch. 14): `new Error(message)` carries the server's text to the page.
- **`AbortSignal`** (ch. 14): TanStack Query hands each query a signal and aborts it when the page unmounts.
- **`encodeURIComponent`** (ch. 15): the workflow id is escaped before entering the POST path.
- **`crypto.randomUUID` and secure contexts** (ch. 13): browsers expose Web Crypto only on https or localhost.
- **`Date`, `getTime`, and `toLocaleString`** (ch. 16): dates print in the viewer's locale from an RFC 3339 string; durations come from `getTime()` differences (ch. 17).
- **`JSON.stringify(value, null, 2)` drops `undefined`** (ch. 17): pretty-prints and omits undefined fields, mirroring Go's `omitempty`.

### React, routing, and queries

- **JSX and the automatic runtime** (ch. 13): tag syntax compiled to element-building calls; `react-jsx` means no React import.
- **Components, props, and one-way data flow** (ch. 13): pages read values from `App` and ask for changes through functions `App` hands down.
- **Callback props and lifting state up** (ch. 13): children report events through functions the parent passed in; the page owns selection, run id, and view.
- **Passing an updater function as a prop** (ch. 18): `WorkflowCanvas` never owns the nodes; `UpdateCanvasDraft` restricts `onChange` to the updater form.
- **`children` and `ReactNode`** (ch. 17): content between a component's tags, typed as anything React can draw.
- **Default and named exports** (ch. 17): one default export imported under any name, plus named exports by exact name.
- **`useState` and functional updates** (ch. 13): the updater form reads the latest value so batched updates do not overwrite each other.
- **Derived values instead of state** (ch. 15): compute `selected`, `history`, and `inspectedRun` on every render so they can never go stale.
- **Rules of hooks** (ch. 15): hooks are called at the top level in a fixed order before any condition.
- **`useRef` and DOM refs** (ch. 17): a mutable handle that survives renders without triggering them.
- **`useEffect` with an empty dependency array** (ch. 17): runs once after the first commit; StrictMode runs it twice in development.
- **React context and the React 19 provider syntax** (ch. 13): `createContext`, rendering `<WorkspaceContext value>` directly, and `useContext` to read it.
- **Custom hooks** (ch. 13): `useWorkspace` wraps `useContext` with a fail-fast null check.
- **`createRoot` and `StrictMode`** (ch. 13): how React 18+ mounts into `#root`, and why StrictMode double-renders in development.
- **Error boundaries** (ch. 13): rendering errors unwind to the router's boundary; handler and fetch errors are not caught.
- **Conditional rendering with `&&`, ternaries, and fragments** (ch. 15): `undefined`, `false`, and `''` draw nothing; `<>...</>` groups siblings.
- **Rendering lists and the `key` prop** (ch. 16): a stable unique key lets React update rows in place.
- **The `key` prop as a reset switch** (ch. 15): `key={selected.id}` remounts the canvas per workflow so React Flow's store starts over.
- **React synthetic `KeyboardEvent<T>` and `currentTarget`** (ch. 16): the generic event type makes sibling navigation type-check.
- **Controlled form elements and `preventDefault`** (ch. 18): inputs show only what React holds; without `preventDefault` the native submit reloads the page and wipes every draft.
- **Focus management: `tabIndex`, `focus()`, `preventScroll`** (ch. 17): `tabIndex={-1}` makes `<main>` focusable from code.
- **`<details>`/`<summary>`** (ch. 17): a native disclosure the browser toggles with no React state.
- **Vite's `index.html` entry** (ch. 13): the module script is transformed in dev and rewritten to a hashed bundle in `web/dist`.
- **TanStack Router: route tree and `Outlet`** (ch. 13): routes built in code, with the root drawing `<Outlet />` for the matched child.
- **TanStack Router: hash history** (ch. 13): everything after `#` stays in the browser, so the Go file server needs no fallback.
- **TanStack Router: search param validation** (ch. 13): `validateSearch` types `?view=` for links and pages; the open tab lives in the URL (ch. 16).
- **TanStack Router: `beforeLoad`, `redirect`, `notFoundComponent`, `defaultErrorComponent`** (ch. 13): the index redirect and the two fallbacks.
- **TanStack Router: type registration** (ch. 13): module augmentation of `Register` makes `Link` and `useNavigate` accept only real paths.
- **TanStack Router: `useSearch` and `useNavigate`** (ch. 15): read the validated view and write it back with `replace: true`.
- **TanStack Router: `Link` and `aria-current`** (ch. 17): a typed client-side `<a>` that marks the active route.
- **TanStack Query: `QueryClient` and `QueryClientProvider`** (ch. 13): one cache in context above the router, so lists survive page switches.
- **TanStack Query: `defaultOptions`, retries, and network mode** (ch. 14): `retry: false` reports the first failure; `networkMode: 'always'` ignores the browser's offline flag.
- **TanStack Query: `refetchInterval` and `refetchIntervalInBackground`** (ch. 14): polling on a timer, also in a hidden tab, reusing an in-flight fetch.
- **TanStack Query: `useQuery`, query keys, `isPending`, `error`** (ch. 15): caches under `['workflows']` and `['runs']`; `error` keeps the last good data.
- **TanStack Query: `useMutation`** (ch. 15): `mutate` never throws; failures land in `error` and `reset` clears them.
- **TanStack Query: `useQueryClient` and `setQueryData`** (ch. 15): `onSuccess` patches the cache so the new run shows before the next poll.
- **TanStack Query: structural sharing** (ch. 16): an idle page does not re-render every second; optimistic inserts are replaced by the next poll.
- **React Flow node objects** (ch. 13): `CanvasDraft.nodes` stores React Flow `Node` objects as-is; the roadmap replaces that shape.
- **React Flow: a controlled flow** (ch. 18): React Flow draws the `nodes` prop and reports changes to `onNodesChange`; `applyNodeChanges` returns the new array.
- **React Flow: `Node<Data, Type>` and `NodeProps`** (ch. 18): the generics pin the data shape and the `'canvas'` literal.
- **React Flow: custom node types and `nodeTypes`** (ch. 18): compared by reference, so it lives at module scope to avoid warning 002.
- **React Flow: `Handle` and `Position`** (ch. 18): the dots where edges attach; connections stay disabled.
- **React Flow: viewport, `fitView`, `Background`, `Controls`** (ch. 18): pan and zoom state; the container must have a real height.

### Accessibility and browser tests

- **Landmarks and accessible names** (ch. 16): `aside`, `nav`, and named regions give screen readers and Playwright the same handles.
- **ARIA roles and attributes** (ch. 16): `aria-current`, `aria-pressed`, `aria-live`, `role="alert"`, `aria-hidden`, and `aria-label`.
- **The ARIA tabs pattern and live regions** (ch. 15): `tablist`, `tab`, and `tabpanel` linked by shared ids, plus `role=status` and `role=alert`.
- **Roving tabindex** (ch. 16): one tab in the Tab order; arrow keys move focus and select via the shared click handler.
- **Playwright role locators** (ch. 16): tests query roles and accessible names, so ARIA attributes are part of the test contract.
- **Playwright locator strictness** (ch. 17): a locator matching several elements throws, which is why only one `<pre>` per panel exists.
- **Playwright test runner and `defineConfig`** (ch. 28): each test gets a fresh browser context; the typed config sets `testDir`, workers, and defaults.
- **Playwright `webServer`** (ch. 1): starts `bin/patchbay` on port 18080 with fixture workflows and a fresh database.
- **Auto-retrying assertions and `expect.poll`** (ch. 28): `expect(locator)` retries until timeout; `toHaveCount(0)` asserts absence.
- **Network interception with `page.route`** (ch. 28): `route.fulfill` fakes JSON, `route.abort` simulates a dead backend.
- **`page.request` and `page.waitForResponse`** (ch. 28): test-side HTTP calls, and a listener registered before the click so a fast 202 is not missed.
- **Viewport, screenshots, and `pageerror`** (ch. 28): phone and tall layouts, `test-results` screenshots, uncaught exceptions.
- **Parameterized tests with a `for` loop** (ch. 28): calling `test()` in a loop registers one titled test per value.
- **Shell `&&`, `mktemp -d`, and `exec`** (ch. 28): the webServer command chains steps, isolates a database per start, and makes the server the signaled process.

### Styling

- **Tailwind utility classes** (ch. 13): small single-purpose classes generated from the source, with project colors from `@theme`.
- **Tailwind variants and responsive prefixes** (ch. 15): `md:`, `lg:`, `hover:`, `focus-visible:`, and `aria-[current=page]:` apply a class under a DOM condition.
- **Tailwind ARIA state variants** (ch. 16): `aria-pressed:` and `aria-selected:` are built in, so visuals derive from announced state.
- **Tailwind arbitrary values** (ch. 16): bracketed one-off values such as `max-w-[1320px]`.
- **Tailwind class detection** (ch. 17): CSS is generated only for complete class strings found in source, which is why tone maps hold full literals.
- **Tailwind CSS v4 configured in CSS** (ch. 1): no `tailwind.config.js`; one `@import` and a `@theme` block, compiled by the Vite plugin.
- **Prettier and the Tailwind class sorter** (ch. 1): one style, with the plugin reading `index.css` to recognize project tokens.
- **Importing CSS from JavaScript** (ch. 19): `main.tsx` imports the stylesheet so Vite bundles it.
- **What `@import 'tailwindcss'` expands to** (ch. 19): a layer order declaration plus three layered imports.
- **Cascade layers** (ch. 19): later layers beat earlier ones regardless of specificity; React Flow's CSS is imported into `layer(base)` so utilities win.
- **Preflight** (ch. 19): Tailwind's base reset; every rule in the file's `@layer base` block restores one default the layout depends on.
- **`@theme` and theme variable namespaces** (ch. 19): `--color-*` generates `bg-`, `text-`, and `border-` utilities; `--text-*` generates sizes.
- **Overriding and removing default theme values** (ch. 19): `--color-*: initial` wipes the default palette.
- **Which theme variables reach the output** (ch. 19): only variables a utility or `var()` uses are emitted.
- **CSS custom properties and inheritance** (ch. 19): `body` sets ink and canvas once; `.react-flow` scopes the `--xy-*` variables.
- **Font size tokens and paired line heights** (ch. 19): `text-2xs` without a `--line-height` companion inherits `line-height: normal`.
- **`rem` units and eight-digit hex colours** (ch. 19): `0.625rem` is 10px; `#17352612` carries alpha in its last two digits.
- **`:disabled`, `:focus-visible`, `outline-offset`** (ch. 19): the wait cursor, the keyboard-only focus ring, and its offset.
- **Text rendering properties** (ch. 19): `font-synthesis`, `text-rendering`, and tap-highlight tuning with no layout effect.
- **React Flow theming with `--xy-*` variables** (ch. 19): declaring them on `.react-flow` recolours the pane, grid, handles, and controls.

### Build and tooling

- **Go modules (`go.mod` and `go.sum`)** (ch. 1): the module line sets the import-path prefix; `require` pins versions; `go.sum` holds hashes.
- **Indirect dependencies** (ch. 1): `// indirect` entries are modules only a dependency needs, recorded for reproducible builds.
- **cgo and `CGO_ENABLED=0`** (ch. 1): a statically linked binary that runs on bare Alpine, possible because the SQLite driver is pure Go.
- **GNU Make targets, prerequisites, and `.PHONY`** (ch. 1): `.PHONY` marks targets as commands; without it the `web/` directory would make `make web` report up to date.
- **Make variable flavors and `export`** (ch. 1): `:=` assigns unconditionally, `?=` respects the environment, `export` passes the variable to recipes.
- **Docker multi-stage builds and layer caching** (ch. 1): separate stages build the frontend and backend; manifests are copied before source to keep install layers cached.
- **Build context and `.dockerignore`** (ch. 1): keeps host `node_modules` and a stale `dist` out of `COPY web/ ./`.
- **Docker Compose services, ports, volumes, and health checks** (ch. 1): loopback-only port, read-only workflows mount, a named volume for SQLite, and `/api/health` for `up --wait`.
- **Docker `COPY` versus Compose bind mount** (ch. 2): the image bakes `workflows/` in; Compose mounts the local directory on top so a restart reloads edits.
- **gitignore patterns** (ch. 1): a leading slash anchors to the repository root; a trailing slash matches only directories.
- **npm `package.json`, lockfile, and `npm ci`** (ch. 1): scripts alias the toolchain; `npm ci` installs precisely the lockfile or fails.
- **Vite** (ch. 1): the dev server with Fast Refresh and the `/api` proxy to Go on 8080, plus the production bundler that writes `web/dist`.

## After the guide

`docs/roadmap.md` lists what the repository does not do yet. As of commit `1c4786e`, these remain planned work:

- **The rest of milestone 1.** SSH commands over `golang.org/x/crypto/ssh` with verified host keys and saved connections, local scripts over `os/exec` with bounded output, Discord webhook delivery, single-administrator authentication, encrypted credential storage, saved configuration, and the two preset workflows with forms. The roadmap also wants each node type to get its own configuration and result structs inside the SSH increment, and a runner shutdown deadline with the first SSH or script executor.
- **Milestone 2.** A real visual editor: node palette, connection handles, a configuration panel, save, validate, and run controls, JSON import and export, a run inspector that reads each run's saved snapshot, and workflow revisions stored separately from runs. Today the canvas stores React Flow node objects, drops step configuration, and never sends a request to Go.
- **Milestone 3.** Concurrent dependency-node execution inside one run, global and per-run step limits, user-requested cancellation, retries, per-run cancellation contexts, server-sent events for run updates, and progress saves that write one node attempt instead of every step row.
- **Milestone 4.** Monitoring thresholds, persisted notification delivery, consistent backups, retention limits, resource bounds, and persisted occurrence identifiers.
- **Milestones 5 to 9.** Restricted diagnostic agents with the four provider adapters, YAML import and export with reusable script nodes, recovery of interrupted runs, an optional PostgreSQL backend, and optional high availability.
- **Smaller items the roadmap names.** Restoring queued, never-started runs from their saved snapshots after a restart; a workflow filter on `GET /api/runs`; and a decision on whether the startup layout check stays full for marked databases once a migration adds a table.

To add a new node type, follow the "Extending the app" section of `AGENTS.md` and start from **chapter 4**. The order is:

1. Chapters 4 and 5: add the validated configuration and result contract in `internal/workflow/workflow.go` and extend the per-type allowlist in `internal/workflow/step_json.go`, so the loader rejects wrong-type fields before anything runs. Chapter 20 shows the tests that pin those rules.
2. Chapter 7: create the executor's own package under `internal/nodes` with a `New` constructor and an `Execute` method, keeping its tests and private helpers beside it. Chapter 23 shows how to prove the healthy, unhealthy, and canceled outcomes without a live service, and `resulttext` supplies the message limit.
3. Chapter 3: add the dispatch case in `checkExecutor` at `cmd/server/main.go:151-161`.
4. Chapter 9: decide whether the stored result needs a new schema version, as version 3 did for TCP, so older builds refuse history they cannot read. Chapters 10 and 25 cover how results round-trip through SQLite.
5. Chapters 2, 14, 16, and 17: add an example file, extend the `WorkflowStep` and `CheckOutput` unions in `web/src/api.ts`, and teach `StepList`, `RunDetails`, and `format.ts` the new type's labels. Chapter 28 shows how the browser tests fake a run of the new type.

Backend validation stays authoritative. A canvas placeholder alone, as chapter 18 explains, does not make a node executable.
