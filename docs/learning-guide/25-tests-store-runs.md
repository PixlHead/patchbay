# 25. Tests: run persistence, reads, lists, interruptions, and integration

Files: [internal/store/runs_test.go](../../internal/store/runs_test.go), [internal/store/run_reads_test.go](../../internal/store/run_reads_test.go), [internal/store/run_list_test.go](../../internal/store/run_list_test.go), [internal/store/run_updates_test.go](../../internal/store/run_updates_test.go), [internal/store/run_queue_test.go](../../internal/store/run_queue_test.go), [internal/store/run_interruptions_test.go](../../internal/store/run_interruptions_test.go), [internal/store/tcp_results_test.go](../../internal/store/tcp_results_test.go), [internal/store/runner_integration_test.go](../../internal/store/runner_integration_test.go)

Revision: commit 1c4786e

## Role in the system

These eight files test the run-history half of the `store` package: `CreateRun` and `UpdateRun` in `internal/store/runs.go` (Chapter 10), `GetRun` and `ListRuns` in `internal/store/run_reads.go` (Chapter 10), `MarkUnfinishedRunsInterrupted` in `internal/store/run_interruptions.go` (Chapter 8), and the version 2 to version 3 migration in `internal/store/schema.go` (Chapter 9). Chapter 24 covers the other store tests, which open files, check permissions, and recognize database identity. Every test here opens a real SQLite file in a temporary directory with `store.Open` (`internal/store/sqlite.go:20`), writes real rows, and then reads them back, either through the production functions or with raw SQL. There are no mocks of the database. What the tests fake is everything around it: fixtures replace the runner's output, raw `UPDATE` statements corrupt stored JSON, a SQLite trigger makes a statement fail on purpose, and canceled contexts stand in for a slow disk.

The path these tests sit on starts with a browser click. `POST /api/workflows/{id}/runs` (`internal/httpapi/api.go:50`) calls `runner.Start` (`api.go:65`), which calls the `RecordNewRun` callback (`internal/engine/runner.go:140`); `cmd/server/main.go:107-108` wires that callback to `store.CreateRun`. The run goroutine then calls `UpdateSavedRun` before and after each step and once at the end (`runner.go:233`, `269`, `309`); `main.go:109-110` wires it to `store.UpdateRun`. The frontend polls `GET /api/runs` and `GET /api/runs/{id}`, which call `store.ListRuns(ctx, db, 100)` and `store.GetRun` (`api.go:92`, `api.go:107`). At the next startup, `main.go:96` calls `store.MarkUnfinishedRunsInterrupted` before the runner exists. Two files, `run_queue_test.go` and `runner_integration_test.go`, wire a real `engine.Runner` to these same store functions exactly as `main.go` does, so they prove the engine's save ordering against SQLite rather than against a fake callback (the fakes are in Chapters 21 and 22).

Data comes in as `engine.Run` and `workflow.Definition` values built by `runFixture` (`runs_test.go:145`). Data goes out as `engine.Run` values from `GetRun` and `ListRuns`, or as raw column values read by `assertStoredRun` (`runs_test.go:32`), the helper that most tests end with. Reading the raw rows rather than calling `GetRun` matters: a bug that made `CreateRun` and `GetRun` wrong in the same way would hide if the tests only compared one against the other.

## Concepts you need

- **Internal test packages.** Every file declares `package store`, not `package store_test`, so the tests can use unexported names such as `initialSchema`, `runErrorMigration`, and `applicationID` (`tcp_results_test.go:100`, `104`). The cost is coupling to private names; the benefit is building a legacy database from the exact SQL the migration code uses. [cmd/go: test packages](https://pkg.go.dev/cmd/go#hdr-Test_packages)
- **The `testing` package: `t.Fatal`, `t.Helper`, `t.Run`, `t.TempDir`.** `t.Fatal` records a failure and stops the test function. `t.Helper` makes a failure inside a helper report the caller's line, which is why `assertStoredRun` starts with it. `t.Run` starts a named subtest with its own result. `t.TempDir` returns a fresh directory that the framework deletes when the test ends, so every test gets its own database file and no test can see another's rows. [pkg.go.dev/testing](https://pkg.go.dev/testing), [T.TempDir](https://pkg.go.dev/testing#T.TempDir)
- **Table-driven tests.** A slice of anonymous structs lists the cases and a loop runs the same body for each inside `t.Run`. `run_updates_test.go:52-62` lists six ways a snapshot can disagree with the saved steps, each as a small function that edits the run. [Go wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)
- **Closures as fakes.** A closure is a function value that reads and writes variables of the function that created it. The runner takes three functions as dependencies (`runner.go:51`, `55`, `60`), and the two integration tests pass closures that call the real store functions and also record IDs or signal a channel. [Tour: function closures](https://go.dev/tour/moretypes/25)
- **`database/sql`: `*sql.DB`, `QueryRowContext`, `QueryContext`, `ExecContext`, and `Scan`.** `*sql.DB` is a pool of connections. `QueryRowContext(...).Scan(&a, &b)` reads one row into Go variables; `QueryContext` returns `*sql.Rows` that you step through with `Next` and must `Close`; `ExecContext` runs a statement without rows. `sql.Result.RowsAffected` tells how many rows a statement matched. [pkg.go.dev/database/sql](https://pkg.go.dev/database/sql)
- **Transactions: `BeginTx`, `Commit`, and a deferred `Rollback`.** A transaction groups several statements so that either all of them or none of them become visible. The production pattern is `tx, err := db.BeginTx(ctx, nil)` followed by `defer tx.Rollback()`, which undoes everything on any early return; `Commit` makes the later `Rollback` a harmless no-op. The tests prove the rollback half by making a late statement fail and checking that earlier statements left no trace. [sql.Tx](https://pkg.go.dev/database/sql#Tx), [SQLite transactions](https://sqlite.org/lang_transaction.html)
- **One connection: `SetMaxOpenConns(1)`.** `store.Open` limits the pool to one connection (`sqlite.go:59`). Any transaction or `Rows` that is not closed holds that connection, so the next query waits. The read tests use a context with a five-second deadline so a leaked transaction becomes a reported failure instead of a hang. [DB.SetMaxOpenConns](https://pkg.go.dev/database/sql#DB.SetMaxOpenConns)
- **`sql.NullInt64` and `sql.NullString`.** A SQL `NULL` cannot be scanned into a plain `int64` or `string`. These types carry a `Valid` flag that is false for `NULL`. The schema uses `NULL` to mean "has not started or finished" (`schema.go:15`), and the tests scan timestamps and `output_json` through these types to check that distinction. [sql.NullInt64](https://pkg.go.dev/database/sql#NullInt64)
- **The SQLite driver and its blank import.** `sqlite.go:13` imports `modernc.org/sqlite` for its side effect of registering the `"sqlite"` driver name. Because the tests are in the same package, `tcp_results_test.go:95` can call `sql.Open("sqlite", path)` directly to build a legacy file without going through `store.Open`. [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), [Spec: import declarations](https://go.dev/ref/spec#Import_declarations)
- **SQLite constraints and `RowsAffected`.** `PRIMARY KEY` and `UNIQUE` reject a duplicate insert with an error, which is what the duplicate run and step tests rely on. SQLite's change count reports every row an `UPDATE` matched, even when the new values equal the old ones; that is why saving the same error text twice still reports one affected row. [sqlite3_changes](https://sqlite.org/c3ref/changes.html), [CREATE TABLE constraints](https://sqlite.org/lang_createtable.html#constraints)
- **SQLite triggers and `RAISE(ABORT)`.** A trigger runs SQL before or after a statement touches a table. `RAISE(ABORT, message)` inside it fails the statement that fired it. `run_interruptions_test.go:87-90` creates one that fails any update setting `runs.status` to `interrupted`, a way to inject a failure in the middle of a production transaction without changing production code. [CREATE TRIGGER](https://sqlite.org/lang_createtrigger.html), [RAISE](https://sqlite.org/lang_createtrigger.html#the_raise_function)
- **SQLite header pragmas: `user_version`, `application_id`, `schema_version`.** `user_version` is a number the application owns; Patchbay stores its migration version there. `application_id` is a second number used as a file marker. `schema_version` is SQLite's own counter that increments on every `CREATE`, `ALTER`, or `DROP`; comparing it before and after a migration proves no table changed. [PRAGMA user_version](https://sqlite.org/pragma.html#pragma_user_version), [PRAGMA application_id](https://sqlite.org/pragma.html#pragma_application_id), [PRAGMA schema_version](https://sqlite.org/pragma.html#pragma_schema_version)
- **`encoding/json`: `Marshal`, `Unmarshal`, `omitempty`, `*json.SyntaxError`, and custom `UnmarshalJSON`.** `Marshal` writes compact JSON with no whitespace; `Unmarshal` fills a struct and leaves omitted fields at their zero values. `omitempty` on a field tag drops zero values from the output, which is how an HTTP result has no `type` key while a TCP result does. Malformed input yields a `*json.SyntaxError`. `workflow.Step` has its own `UnmarshalJSON` (`internal/workflow/step_json.go:14`) that decoding a saved definition goes through. [pkg.go.dev/encoding/json](https://pkg.go.dev/encoding/json)
- **Error wrapping: `%w`, `errors.Is`, `errors.As`.** `fmt.Errorf("...: %w", err)` keeps `err` reachable inside the new error. `errors.Is(err, sql.ErrNoRows)` walks that chain looking for a specific value; `errors.As(err, &target)` looks for a specific type and stores it. The tests use both instead of comparing message strings. [pkg.go.dev/errors](https://pkg.go.dev/errors)
- **`reflect.DeepEqual` and `time.Time`.** `DeepEqual` compares values field by field, following pointers and slices. For `time.Time` it compares the internal fields, including the location pointer, so a time built with `time.UnixMilli(x)` (local location) is not `DeepEqual` to the same instant read back as UTC even though `Equal` says they match. That is why every fixture time ends with `.UTC()` and the store reads times back with `.UTC()` (`run_reads.go:83`, `86`, `123`). [reflect.DeepEqual](https://pkg.go.dev/reflect#DeepEqual), [Time.UTC](https://pkg.go.dev/time#Time.UTC)
- **Millisecond timestamps.** The schema stores times as Unix milliseconds (`schema.go:15`). A `time.Now()` value has nanoseconds, so a run created by the real runner and read back from SQLite differs below the millisecond. The queue test compares `UnixMilli()` values for that reason (`run_queue_test.go:91`). [Time.UnixMilli](https://pkg.go.dev/time#Time.UnixMilli)
- **Pointers for optional values.** `StepRun.StartedAt`, `StepRun.FinishedAt`, and `StepRun.Output` are pointers (`runner.go:28-30`) so that `nil` means "absent" and maps to SQL `NULL`. `Run.StartedAt` is a plain `time.Time` whose zero value means "not started" (`runner.go:41`). The fixtures set and clear these to describe each lifecycle state. [Tour: pointers](https://go.dev/tour/moretypes/1)
- **Struct copies share slices.** Assigning one struct to another copies every field, but a slice field is a small header pointing at a shared array. `run_updates_test.go:19-21` copies a run and then gives the copy its own `Steps` with `make` before editing, so the original stays intact. [Go blog: slices](https://go.dev/blog/slices-intro)
- **`context`: deadlines and pre-canceled contexts.** `context.WithTimeout` gives a deadline that `database/sql` checks before taking a connection. Calling `cancel()` immediately after `context.WithCancel` makes a context that is already dead, which is the simplest way to prove a function stops before writing. Executors receive the runner's context and return `ctx.Err()` when it is canceled. [pkg.go.dev/context](https://pkg.go.dev/context)
- **Goroutines, channels, `close`, buffered channels, and `select`.** The runner executes each run on its own goroutine (`runner.go:197`). The integration tests use a channel closed as a broadcast to release a blocked executor, a buffered channel so a save callback can signal completion without waiting, and `select` with `time.After` so a run that never finishes fails the test after two seconds. [Tour: channels](https://go.dev/tour/concurrency/2), [Tour: select](https://go.dev/tour/concurrency/5)
- **`sync.Mutex` and the race detector.** A map read on one goroutine and written on another is a data race. `run_queue_test.go:23-24` protects a map with a mutex because the record callback writes it on the test goroutine while an executor reads it on a run goroutine. `make test` runs `go test -race` (`Makefile:16`), which reports any such race. [sync.Mutex](https://pkg.go.dev/sync#Mutex), [Race detector](https://go.dev/doc/articles/race_detector)
- **Happens-before through `Close`.** Reading a variable another goroutine wrote is only safe when something orders the write before the read. `runner.Close()` returns after `wg.Wait` (`runner.go:190`), which runs after every run goroutine's deferred `wg.Done` (`runner.go:217`). The integration tests call `Close` before reading final results for that reason. [The Go memory model](https://go.dev/ref/mem)
- **`defer` ordering, including inside loops.** Deferred calls run when the surrounding function returns, last registered first. `defer db.Close()` written before `defer runner.Close()` means the runner stops before the database closes, the same order `main.go:79` requires. A `defer` inside a loop body runs at function exit, not at the end of the iteration (`tcp_results_test.go:135`). [Spec: defer statements](https://go.dev/ref/spec#Defer_statements)
- **`min` and `for range N`.** Since Go 1.21 `min(a, b)` is a built-in; since Go 1.22 `for range 2` repeats a body twice without a loop variable. [builtin.min](https://pkg.go.dev/builtin#min), [Spec: for statements with range clause](https://go.dev/ref/spec#For_range)

## Walkthrough

### internal/store/runs_test.go

**L1–L15** — `package store` makes this an internal test file. The imports: `context` for every store call; `database/sql` for `sql.DB`, `sql.NullInt64`, and `sql.NullString` in the raw-row helper; `encoding/json` to decode the stored snapshot and outputs; `path/filepath` to build the database path inside `t.TempDir()`; `reflect` for `DeepEqual`; `testing`; `time` for the fixture timestamps; and the two project packages whose types the store persists, `engine` (`Run`, `StepRun`) and `workflow` (`Definition`, `Step`, `CheckConfig`, `CheckResult`). This file also defines `assertStoredRun` and `runFixture`, which every other file in this chapter and `schema_test.go:22`, `identity_test.go:97`, `writable_test.go:26`, and `sqlite_permissions_test.go:74` reuse.

**L16–L22** — `TestCreateRunStoresSnapshotAndStepResults` opens a fresh database at `<tempdir>/history.db`. `Open` (`sqlite.go:20-72`) creates the file with mode `0600`, applies all three migrations in one transaction (`schema.go:92-106`), and marks the file (`schema.go:109-113`). `defer db.Close()` closes it even if an assertion fails; without it the test would leak a connection and the temporary directory cleanup could fail on some platforms.

**L23–L24** — `runFixture()` returns a three-step failed run (L145-168). The test adds a run-level `Error`, the field `runner.go:39` describes as "Run-level reason, separate from step errors", so the `error` column added by `runErrorMigration` (`schema.go:46-49`) is exercised and not left at its default.

**L25–L29** — `CreateRun` (`runs.go:17-62`) encodes the definition (`runs.go:18-21`), inserts the run row (`runs.go:29-36`) and one `run_steps` row per step with its position (`runs.go:38-56`), and commits (`runs.go:58-61`). `assertStoredRun` then reads the raw rows. This is the baseline proof for `AGENTS.md:83`: "Save a new run and its workflow snapshot before acceptance."

**L30–L34** — The comment explains why the helper reads raw rows: "so create and update tests check the same stored values." `t.Helper()` makes failures point at the calling test. The helper takes the `*sql.DB`, the definition the caller saved, and the run the caller expects to find.

**L35–L42** — One `SELECT` from `runs` by ID reads every column the production code writes (`runs.go:29-33`). `created` is a plain `int64` because `created_at` is `NOT NULL` (`schema.go:23`); `started` and `finished` are `sql.NullInt64` because those columns may be `NULL` (`schema.go:24-25`). `runError` is a plain string because the column has `NOT NULL DEFAULT ''` (`schema.go:47`).

**L43–L47** — The metadata comparison checks workflow ID and name, status, creation time in milliseconds, and the start time. `started.Valid != !run.StartedAt.IsZero()` says: the column is `NULL` exactly when the run's `StartedAt` is the zero time. That encodes `unixMilliOrNull` (`runs.go:127-132`), which returns `nil` for a nil or zero time, and the schema comment at `schema.go:15`: "NULL means a run or step has not started/finished." The second half compares the stored millisecond value when present.

**L48–L50** — The run-level error must round-trip exactly, including the empty string. A `%q` in the message shows whitespace differences.

**L51–L54** — Same shape for `finished_at`, but `Run.FinishedAt` is a pointer (`runner.go:42`), so "absent" is `nil` rather than a zero time. `CreateRun` passes the pointer straight to `unixMilliOrNull` (`runs.go:33`).

**L55–L61** — The `definition_json` column is decoded into a `workflow.Definition` and compared with `DeepEqual` against the caller's definition. Decoding goes through `Step.UnmarshalJSON` (`step_json.go:14-54`), so a snapshot that the production decoder would reject fails here too. This is the "workflow snapshot" of `AGENTS.md:103`: "Preserve atomic run/step snapshots."

**L62–L68** — The step query orders by `position`, the column `CreateRun` fills from the loop index (`runs.go:38`, `51`) and the `UNIQUE (run_id, position)` constraint protects (`schema.go:39`). `defer rows.Close()` releases the single connection (`sqlite.go:59`) when the helper returns; a leaked `Rows` would block the next query in the calling test.

**L69–L76** — Each row is scanned into a fresh `engine.StepRun`. `start`, `finish`, and `output` use the nullable types because `run_steps.started_at`, `finished_at`, and `output_json` allow `NULL` (`schema.go:34-36`); `step.Error` is a plain string because of `NOT NULL DEFAULT ''` (`schema.go:37`).

**L77–L84** — A valid timestamp becomes a `*time.Time` pointing at a UTC value. The `.UTC()` is required for `DeepEqual` at L96: the fixture's times are UTC (L146), and `time.UnixMilli` alone would carry the local location. This mirrors `timeFromUnixMilli` (`run_reads.go:119-125`).

**L85–L90** — A non-`NULL` `output_json` is decoded into a new `CheckResult`. A `NULL` leaves `step.Output` nil, which is how `CreateRun` encodes a step with no result: "A nil output is SQL NULL, distinct from an encoded result object" (`runs.go:39`). The fixture's failed and skipped steps have nil outputs, so the helper checks both branches on every call.

**L91–L92** — Append and loop. `savedSteps` is in position order because of the `ORDER BY`.

**L93–L99** — `rows.Err()` reports an error that ended the iteration early; skipping it would turn a read failure into "zero steps". The final `DeepEqual` against `run.Steps` checks IDs, names, statuses, both timestamps, outputs, and errors of every step in order.

**L100–L107** — `TestCreateRunRollsBackWithoutReplacingHistory` opens a fresh database. It targets the sentence at `runs.go:16`: "An existing run ID is an error; this function never replaces saved history."

**L108–L111** — The fixture run `run-1` is saved once.

**L112–L116** — The definition name and the run's workflow name are changed, then `CreateRun` is called again with the same `run.ID`. The `INSERT INTO runs` hits the `PRIMARY KEY` on `id` (`schema.go:18`) and fails at `runs.go:34-35`; the deferred `tx.Rollback()` (`runs.go:27`) ends the transaction. The expected error proves the insert is a plain `INSERT`, not `INSERT OR REPLACE`.

**L117–L122** — A second run `run-2` is given two steps with the same ID. The comment says why: "Fail after the run and first step were inserted." The run row and the step at position 0 insert successfully; the step at position 1 violates `PRIMARY KEY (run_id, step_id)` (`schema.go:38`) and `runs.go:53-54` returns the error. Everything already written in this transaction must disappear.

**L123–L130** — One query counts both tables. Exactly one run and three steps means neither failed call left a row behind: the rollback at `runs.go:27` covered the partially inserted `run-2`. Without the transaction, `run-2` would exist with one step and `ListRuns` would later show a run missing two of its steps.

**L131–L143** — The stored snapshot of `run-1` is decoded and compared with a fresh `runFixture()` definition, whose name is still `"Service checks"`. If the duplicate insert had replaced the row, the name would read `"Edited workflow"`. This is the history-preservation half of `AGENTS.md:103`: "Preserve atomic run/step snapshots and existing data."

**L144–L148** — `runFixture` builds the shared test data. `started` is a fixed instant converted to UTC (see the `DeepEqual` concept), `finished` is one second later, and `config` is one valid HTTP check config (`workflow.go:39-45`): an absolute URL, an expected status in range, and a timeout in range, so that `workflow.Validate` (`workflow.go:63-117`) accepts the definition when the integration tests pass it to the real runner.

**L149–L156** — The definition has ID `"checks"`, three `http.check` steps that share `config`, and no schedule. `SchemaVersion: 1` is required by `workflow.go:64-66`.

**L157–L166** — The run is a finished failure: `Status: "failed"`, `CreatedAt` and `StartedAt` equal, `FinishedAt` set. Its three steps cover the three terminal step shapes the store must handle: `healthy` succeeded with an output, `broken` failed with an error and no output, and `skipped` with no timestamps at all. These correspond to what `runner.go:257`, `runner.go:255`, and `runner.go:303-307` produce.

**L167–L168** — Return both values. Each call builds new slices and new `CheckResult` pointers, so a test that edits its fixture cannot affect another test.

### internal/store/run_reads_test.go

**L1–L15** — Imports for the read tests: `database/sql` for `sql.ErrNoRows`, `encoding/json` for `*json.SyntaxError`, `errors` for `Is` and `As`, `time` for the five-second contexts and the zero `time.Time{}`, and `engine` for `engine.Run{}` comparisons. `workflow` is not needed because the fixtures come from `runFixture`.

**L16–L18** — `TestGetRunReadsPersistedHistory` runs one subtest per lifecycle state: a finished run, a running run with one active step, and a queued run that never started. Each state stores a different pattern of `NULL` timestamps, so one subtest cannot stand in for the others.

**L19–L26** — Each subtest gets a five-second context and its own database. The deadline turns a stuck query into a failure with a message; with the single-connection pool (`sqlite.go:59`), a leaked transaction would otherwise wait forever.

**L27–L30** — The finished state keeps the fixture as is and adds a run-level error.

**L31–L40** — For the other two states the run becomes `running` with no `FinishedAt`, and every step is reset to `pending` with no timestamps, output, or error, the same shape `Start` builds at `runner.go:133-135`. For `running`, step 0 gets `Status: "running"` and a `StartedAt` pointing at the run's own start, which is what `runner.go:230-232` sets just before the first progress save.

**L41–L43** — The not-started state is `queued` with a zero `StartedAt`, the exact value `Start` returns for a queued admission (`runner.go:126-129`). `CreateRun` must store `started_at` as `NULL` here (`runs.go:33` through `unixMilliOrNull`).

**L44–L46** — Save the run under test.

**L47–L54** — A second run `run-2` is saved with the same step IDs, a different error, and a different output reason. The comment states the purpose: "Another run may reuse every step ID without mixing its results in." Step IDs are only unique per run, because the primary key is `(run_id, step_id)` (`schema.go:38`), and `readRunInTx` filters by `run_id` (`run_reads.go:91`).

**L55–L62** — The database is closed and opened again at the same path. This proves the rows were committed to the file, not just visible on the open connection, and that `Open` on a current, marked database performs no writes (`schema.go:87-89`: "A recognized current database needs no writes"). The explicit `Close` plus the earlier `defer db.Close()` is safe because `sql.DB.Close` is idempotent.

**L63–L72** — `GetRun` (`run_reads.go:16-24`) must return a value `DeepEqual` to `want`. That single comparison checks: `created_at` decoded at `run_reads.go:83`; `started_at` left as the zero time when `NULL` (`run_reads.go:84-87`, with the comment "A missing start uses zero time, omitted from JSON by the Run type"); `finished_at` through `timeFromUnixMilli` (`run_reads.go:88`); steps in position order (`run_reads.go:90-91`); nil versus decoded outputs (`run_reads.go:106-110`); and no contamination from `run-2`. The `%#v` format prints both values in full when they differ.

**L73–L81** — `TestGetRunErrorsReturnNoPartialHistory` targets the doc comment at `run_reads.go:14-15`: "A missing run returns an error wrapping sql.ErrNoRows. Any failure returns a zero Run rather than partially decoded history."

**L82–L85** — One fixture run is saved.

**L86–L90** — Reading ID `"missing"` must return exactly `engine.Run{}` and an error for which `errors.Is(err, sql.ErrNoRows)` is true. `readRunInTx` wraps the `Scan` error with `%w` at `run_reads.go:81`, which keeps the sentinel reachable. The HTTP layer depends on this: `api.go:108-110` turns that sentinel into `404 run not found`, and any other error into `500`. A store that returned a bare string error would make every missing run a 500.

**L91–L95** — Failure injection: a raw `UPDATE` replaces the `output_json` of the second step with `'{'`, invalid JSON. The comment explains the placement: the run row and the first step's result decode successfully before the failure, so a partial `Run` exists inside `readRunInTx` when the error occurs.

**L96–L100** — `GetRun` must now return `engine.Run{}` and an error that `errors.As` can unwrap to a `*json.SyntaxError`. The decode at `run_reads.go:107-108` returns `engine.Run{}` explicitly instead of the half-filled `run`. If it returned `run`, the API would serve a run with one step and no error, which the frontend would show as truth.

**L101–L105** — The comment says what this write checks: "The failed read must release its transaction so later operations can proceed." `GetRun` defers `tx.Rollback()` (`run_reads.go:22`, with the note that rollback is how a read transaction ends) and `readRunInTx` defers `rows.Close()` (`run_reads.go:95`). If either were missing, this `UPDATE` would wait for the only connection until the five-second context at L75 expired, and the test would fail with a deadline error.

**L106–L110** — After the repair, `GetRun` returns the original run again. This confirms the corruption and the repair both reached the row, and that the failed read did not leave the connection in a broken state.

### internal/store/run_list_test.go

**L1–L14** — Imports for the list tests: `encoding/json` and `errors` for the syntax-error check, `time` for `time.UnixMilli` creation times, and `engine` for `[]engine.Run` comparisons.

**L15–L22** — `TestListRunsOrdersAndLimitsCompleteHistory` opens a database under a five-second context, for the same single-connection reason as the read tests.

**L23–L32** — A map will hold each saved run by ID, and a table lists four runs with chosen creation times. The IDs are picked to make sorting by ID give a different order from sorting by creation time: `run-zz` sorts last by ID but is the oldest; `run-b` and `run-a` share the same millisecond, and the comment at L30 notes that `run-a` is inserted after `run-b`.

**L33–L43** — Each run is a fresh fixture with its ID, a run-level error naming the ID, the table's creation time, and a step output reason naming the ID, so a run returned in the wrong slot is visible in the failure output. The comment at L41 states the rule: "Creation order must win over ID, insertion order, and start time." Every fixture shares the same `StartedAt`, so start time carries no ordering information at all.

**L44** — The expected order is newest creation first, then ID descending for the tie: `run-b` before `run-a` because `"run-b" > "run-a"`, then `run-z`, then `run-zz`. That is the `ORDER BY created_at DESC, id DESC` at `run_reads.go:39-40`, documented at `run_reads.go:26`. The tiebreak on ID keeps the list stable between polls when two runs start in the same millisecond.

**L45–L55** — Three limits are tried. `want[:min(limit, len(want))]` slices the expectation to the first `limit` entries, using the built-in `min`, so limit 100 returns all four. The comparison is on complete runs, including steps, which proves `ListRuns` reads each listed run through `readRunInTx` (`run_reads.go:61-68`) rather than only the `runs` table. The `LIMIT ?` is applied in SQL (`run_reads.go:40`), so the database never reads more IDs than requested.

**L56–L64** — `TestListRunsEmptyHistoryAndInvalidLimits` opens an empty database.

**L65–L68** — With no runs, `ListRuns` must return a non-nil, empty slice and no error. `run_reads.go:61` builds it with `make([]engine.Run, 0, len(ids))`. The difference matters at the API: `json.Encoder` writes a nil slice as `null` and an empty slice as `[]`, and `README.md:562` promises "An empty list is `[]`". A `null` would break the frontend's array handling on a fresh install.

**L69–L75** — Limits `-1`, `0`, and `101` must return `nil` and an error. `run_reads.go:30-32` rejects anything outside 1 to 100 before opening a transaction. The API always passes 100 (`api.go:92`), so this check protects future callers and documents the bound that `README.md:560` states.

**L76–L84** — `TestListRunsErrorsReturnNoPartialHistory` mirrors the `GetRun` error test for lists; the doc comment at `run_reads.go:28` promises "any failure returns nil rather than a partial list."

**L85–L93** — Two runs with the same creation time are saved: `run-a` (which will be corrupted) and `run-b`. By the ID tiebreak, `run-b` is listed first.

**L94–L97** — The second step of `run-a` gets invalid JSON, as in the read test.

**L98–L102** — With limit 1 only `run-b` is listed, so the corrupt run is never read and the call succeeds. The comment: "A corrupt run outside the requested limit must not affect the result." This follows from collecting IDs first with `LIMIT` (`run_reads.go:39-55`) and only then reading those runs.

**L103–L107** — With limit 2 the read of `run-a` fails inside the loop, and `ListRuns` must return `nil` with a `*json.SyntaxError`. `run_reads.go:63-66` returns `nil, err` on the first failure instead of the runs collected so far. A partial list would make the frontend show `run-b` while silently dropping `run-a`, and the API's 500 at `api.go:93-97` depends on the error reaching it.

**L108–L112** — The repair statement again doubles as a check that the failed list released its transaction (`run_reads.go:37`) and the ID result set (`run_reads.go:44`, `57-59`). The comment at `run_reads.go:56` explains why the production code closes the ID rows explicitly before reading runs: "Finish this result set before reading runs on the same connection."

**L113–L117** — After the repair, both runs come back in the expected order.

### internal/store/run_updates_test.go

**L1–L10** — The smallest import list in the chapter: only `context`, `path/filepath`, `testing`, and `engine`. `assertStoredRun` does all the comparing.

**L11–L17** — `TestUpdateRunSavesProgressAndPreservesWorkflow` opens a database.

**L18–L24** — `finished` is the full fixture. `progress` starts as a struct copy of it, then becomes a running run with no finish time. L21 is essential: `progress := finished` copied the `Steps` slice header, so writing `progress.Steps[i]` would also overwrite `finished.Steps[i]`. `make` gives `progress` its own array before the loop fills it with pending steps, the shape of a run just admitted (`runner.go:133-135`).

**L25–L27** — The pending run is saved with `CreateRun`, as `Start` would do.

**L28–L34** — Step 0 is marked running with a start time, the state at `runner.go:230-232`, and `UpdateRun` (`runs.go:68-125`) saves it. `assertStoredRun` confirms the `UPDATE runs` at `runs.go:75-77` and the per-step `UPDATE run_steps` at `runs.go:106-110` wrote the new status and timestamp. This is the first half of `AGENTS.md:86`: "Save step start before execution."

**L35–L38** — Now `finished` is given a different workflow ID, workflow name, and first step name. The comment: "Execution updates cannot rewrite the workflow or its original step names." `UpdateRun`'s doc at `runs.go:65` lists what stays fixed: "Workflow details, creation time, step names, and step order." The SQL simply never sets those columns, and the step `WHERE` matches on `step_id` and `position`, not on `name`.

**L39–L49** — The loop saves the finished run three times with run errors `"Execution could not continue."`, the same string again, and `""`. Each call must succeed. The repeated value matters because `UpdateRun` fails when `RowsAffected` is not 1 (`runs.go:81-85`, `114-118`); SQLite counts matched rows, so an unchanged value still counts, and the final-save retry at `runner.go:325-342`, which rewrites an identical snapshot, keeps working. The empty string proves the error can be cleared. The expectation is a fresh fixture with only `Error` changed, so `assertStoredRun` fails if the edited workflow ID, name, or step name reached the database.

**L50–L62** — `TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory` is table-driven. Each case is a function that damages the update: a run ID that does not exist, one step too few, one step too many, a step ID that is not in the run, a duplicated step ID, and two steps swapped. These are the ways a caller could violate the contract at `runs.go:66-67`: "The caller supplies every step in its original order."

**L63–L69** — Each case gets its own subtest and database, so a leaked change in one case cannot hide in another.

**L70–L74** — The original run is saved with a distinctive run error and the fixture's terminal step results.

**L75–L82** — The update is a fresh fixture reset to running, no finish time, every step pending with no timestamps, output, or error, and a different run error. Every field differs from the original on purpose: if any part of the update leaks, `assertStoredRun` sees it.

**L83–L88** — `test.change(&update)` applies the damage through a pointer so the closure edits this variable. `UpdateRun` must return an error. Which line rejects each case: the missing run makes `UPDATE runs` affect zero rows (`runs.go:83-84`, wrapping `sql.ErrNoRows`); the missing and extra steps fail the count check (`runs.go:92-93`); the unknown, duplicate, and reordered steps fail the per-step `RowsAffected` check (`runs.go:116-117`), because the `WHERE run_id = ? AND step_id = ? AND position = ?` at `runs.go:108` matches nothing when the ID is not at that position. The comment at `runs.go:105` states that design: "Matching the original position also rejects reordered or duplicate IDs." The test comment at L84 adds the transactional point: in the unknown and duplicate cases, the run row and the first two steps were already updated before the third step failed, so the deferred rollback at `runs.go:73` must undo them. `assertStoredRun` then proves the original is intact.

**L89–L98** — A final count proves the missing-run case did not insert anything. There is exactly one run in every case.

### internal/store/run_queue_test.go

**L1–L14** — This file wires the real runner to the real store, so it imports `engine` and `workflow` plus `fmt` for an error message, `sync` for a mutex, and `time` for the completion timeout.

**L15–L20** — `TestQueuedRunPersistsBeforeExecutionAndKeepsCreationTime` opens `queue.db`. Note the defer order that follows: `db.Close` is deferred here and `runner.Close` at L62, so at exit the runner stops first and the database closes second, the order `AGENTS.md:96` requires and `main.go:79` comments on.

**L21–L24** — `release` is closed later to let workflow `a` finish. `completed` is buffered with capacity 1 so the save callback that signals it never blocks a run goroutine. `ids` maps a workflow ID to the run ID the runner assigned, and `idsMu` guards it: the record callback writes the map on the test goroutine while `a`'s executor may be reading it on a run goroutine, which would be a data race under `-race` without the lock.

**L25–L29** — `engine.New(1, 1, ...)` allows one active run and one queued run (`runner.go:87-88`). The executor looks up the run ID by `step.ID`; the comment explains the trick: "Both definitions have one step whose ID also identifies its workflow", so the step ID equals the workflow ID equals the map key.

**L30–L36** — Inside the executor, the test reads the run from SQLite. At the moment a step executes (`runner.go:247`), the saved run must already be `running` with a start time and a running first step. For the queued workflow this is the promotion save: `runner.go:220-223` sets `running` and `StartedAt` when a queued run is launched, with the comment "The first step-start save persists this transition before any action runs", and `runner.go:233` performs that save before `execute` is called. If the order were wrong, the executor returns an error, the run fails, and the final assertion at L91 reports it. `README.md:167` states the same rule: "Promotion is saved before the first step executes."

**L37–L44** — Workflow `a` blocks until `release` is closed or the runner context is canceled. This holds the single active slot so `b` must queue. Returning `ctx.Err()` on cancellation keeps the deferred `runner.Close()` from hanging if the test fails early. Both workflows report a healthy result.

**L45–L49** — The record callback stores the run ID under the workflow ID, under the mutex, then calls the real `CreateRun`. The runner calls it under its own mutex at `runner.go:136-145` with a five-second context that belongs to the runner (`runner.go:139`).

**L50–L58** — The update callback calls the real `UpdateRun`, and when workflow `b` saves a non-running status it signals `completed`. Progress saves always carry `running` because the run goroutine sets `running` at `runner.go:222` before its first save and changes `run.Status` again only in `finishRun` (`runner.go:302`), so the signal fires exactly once, on `b`'s successful final save.

**L59–L62** — Constructor error check and the deferred `Close`.

**L63–L67** — Two one-step definitions derived from the fixture. Each gets the workflow ID `a` or `b` and a single step with the same ID. `definition.Steps[:1]` shortens the slice; since `runFixture` returns a fresh slice each call, the edit affects no other test.

**L68–L76** — Both are started. `a` runs at once and blocks in its executor. `b` finds `activeRuns >= maxActiveRuns` (`runner.go:122`), fits in the queue (`runner.go:123`), and is saved as queued. `assertStoredRun` on `b` right after `Start` returns proves the save happened before acceptance (`AGENTS.md:83`) and that a queued run is stored with its creation time and a `NULL` start, as `README.md:166-167` describes.

**L77–L79** — The returned run must be `queued` with a zero `StartedAt`, which is what `runner.go:126-129` builds when `queued` is true and `runner.go:130-132` leaves alone.

**L80–L85** — Closing `release` lets `a` finish. Its run goroutine then promotes `b` (`runner.go:292-296`), and `b` executes, saves, and signals. The `select` turns a missing signal into a failure after two seconds instead of a hung test.

**L86–L90** — `runner.Close()` waits for both run goroutines to exit (`runner.go:190`). Reading `b` afterwards guarantees no later write can race the read. `GetRun` reads the final saved state.

**L91–L94** — Five facts in one condition: `b` succeeded; it has a start time; its `created_at` is unchanged since admission, compared in milliseconds because the runner's `time.Now()` carries nanoseconds the column drops; its start is not before its creation, so promotion time came after admission time; and its step has an output. `UpdateRun` never sets `created_at` (`runs.go:75-77`), which is the "creation time" promise at `runs.go:65`.

### internal/store/run_interruptions_test.go

**L1–L13** — Imports: `errors` for `errors.Is(err, context.Canceled)`, `time` for `time.Time{}`, and both project packages for the fixtures.

**L14–L20** — `TestMarkUnfinishedRunsInterruptedPreservesSavedResults` opens a database. The function under test runs at startup, before the runner exists (`main.go:94-103`), and `AGENTS.md:98-99` states its scope: "Startup marks leftover queued/running records interrupted; it does not resume actions."

**L21–L23** — On an empty history the helper must return zero changed runs and no error. Startup logs the count only when nonzero (`main.go:101-103`).

**L24–L30** — Four unfinished runs model the states a crash can leave. `active` comes from `unfinishedRunFixture` (L118-124): running, with a succeeded step, a running step, and a pending step. `beforeExecution` is running with every step still pending, the state between `Start` and the first progress save.

**L31–L35** — `queued` never started: status `queued`, zero `StartedAt`, every step pending.

**L36–L39** — `afterExecution` has every step in a terminal state (the fixture's succeeded, failed, and skipped steps) but the run is still `running` with no finish time. The comment explains: "Every step result was saved, but the final run update never completed." The failed step's `"connection refused"` error must survive.

**L40–L47** — Four terminal runs, one per status `succeeded`, `failed`, `canceled`, and `interrupted`. Each is given deliberately inconsistent steps, one running and one pending, with the comment "Even inconsistent steps in a terminal run are outside this helper's scope." The step update at `run_interruptions.go:24-27` restricts itself to steps whose run is queued or running, so these steps must stay exactly as saved.

**L48–L52** — All eight runs are saved with the same definition. `CreateRun` does not care that they share a workflow ID; only the runner enforces overlap (`runner.go:119-121`).

**L53–L62** — The expectations are built by editing the fixtures in place. `active`: run interrupted, the running step interrupted, the pending step skipped, the succeeded step untouched. `beforeExecution`: run interrupted, all steps skipped. `afterExecution`: only the run status changes. `queued`: run interrupted, all steps skipped. No `FinishedAt` is set anywhere, matching `run_interruptions.go:11`: "a missing finish time stays unknown", which the frontend shows as "Finish time unknown" (`README.md:573-575`).

**L63–L73** — The helper is called twice. The first call must report 4 changed runs (`run_interruptions.go:36-39` counts rows from the `UPDATE runs` at L31-32); the second must report 0, which is the comment's point: "The second call must not change already reconciled history." After each call every one of the eight runs is checked with `assertStoredRun`, which, as the comment notes, also verifies the definition JSON, creation time, step order, outputs, and errors. This is the preservation promise of `run_interruptions.go:10-11` and `README.md:572-573`.

**L74–L81** — `TestMarkUnfinishedRunsInterruptedRollsBackStepChanges` targets the transaction: the step update (`run_interruptions.go:24-30`) and the run update (`run_interruptions.go:31-35`) must commit or roll back together.

**L82–L85** — One active run is saved.

**L86–L93** — Failure injection without touching production code: a SQLite trigger fires before any update of `runs.status` whose new value is `interrupted` and raises `ABORT`. The step update at `run_interruptions.go:24` runs first and succeeds; the run update at `run_interruptions.go:31` then fails. The comment: "Force the run update to fail after the helper has updated its steps." `identity.go:28-30` is why a marked database may carry such a trigger without failing recognition: "Marked files may have diagnostic indexes or test-injected triggers."

**L94–L98** — The call must return an error and zero, the value `run_interruptions.go:14` promises "if the transaction fails", and the run must be unchanged. The deferred `tx.Rollback()` at `run_interruptions.go:20` undoes the step update that had already run. Without the transaction, the steps would read interrupted and skipped while the run still said running, a mixed state no code path produces.

**L99–L105** — `TestMarkUnfinishedRunsInterruptedHonorsCancellation` opens a database with a background context.

**L106–L109** — The same active run is saved.

**L110–L111** — A context is created and canceled at once. This stands in for the five-second deadline `main.go:95` puts on the call expiring before it starts.

**L112–L116** — The error must wrap `context.Canceled` and the count must be zero. `db.BeginTx` checks the context before taking a connection, and `run_interruptions.go:16-19` wraps its error with `%w`. The run is unchanged. A helper that ignored its context would keep writing after startup had given up on it.

**L117–L124** — `unfinishedRunFixture` turns the fixture into a run caught mid-execution: status running, no finish time, step 1 running with its finish time and error cleared, step 2 pending. Step 0 keeps its succeeded result and output. This is the exact shape the runner publishes between `runner.go:233` and `runner.go:247` for the second step.

### internal/store/tcp_results_test.go

**L1–L14** — Imports: `database/sql` for the direct `sql.Open` in the migration test, `fmt` for the `PRAGMA application_id` statement, `reflect` for `DeepEqual`, and both project packages.

**L15–L22** — `TestMixedHTTPAndTCPResultsSurviveUpdatesAndReopen` keeps the path in a variable because it reopens the file later.

**L23–L28** — The fixture definition is cut to two steps and the second becomes a `tcp.check` with a host, port, and timeout in `CheckConfig` (`workflow.go:36-45`, where the comment says "HTTP uses URL/ExpectedStatus; TCP uses Host/Port"). `AGENTS.md:124-125` states that both types share this one struct as plain values.

**L29–L38** — The run is a two-step success. The TCP step's output carries `Type: "tcp.check"`, `Healthy: true`, the host and port, and a reason. `CheckResult` (`workflow.go:47-59`) marks `Type` with `omitempty`, and its comment says why: "An absent Type means HTTP, preserving the original HTTP JSON format; TCP results identify themselves." The HTTP step's output, from the fixture, has no `Type`.

**L39–L45** — The run is saved and verified two ways: raw rows through `assertStoredRun` and the decoded value through `GetRun`. `CreateRun` encodes each output with `json.Marshal` (`runs.go:42-46`), which drops the empty `Type`, `Host`, and `Port` of the HTTP result and the empty `URL`, `ExpectedStatus`, and `StatusCode` of the TCP result; decoding brings them back as zero values, so `DeepEqual` passes only if every omitted field really was zero. The definition snapshot also round-trips through `Step.UnmarshalJSON` (`step_json.go:39-48`), which selects the TCP config fields by type.

**L46–L52** — The TCP result is replaced by an unhealthy one: `Healthy` false, "Connection refused". The run and step statuses stay `succeeded`. The comment explains: "Unreachable TCP is health data, so the step and run still succeeded." That is `AGENTS.md:91` ("Distinguish execution outcome, monitored-service health, and save failure") and `docs/roadmap.md:117`, which asks for a failed health check to be "a successful check operation with an `unhealthy` result and a reason."

**L53–L58** — `UpdateRun` saves the new output (`runs.go:96-110` re-encodes every step), and the database is closed.

**L59–L64** — Reopening runs `migrate`, which sees version 3 and the marker and returns without writing (`schema.go:87-89`). The raw rows must show the updated TCP result next to the untouched HTTP result and the untouched definition, as the comment at L48 requires.

**L65–L73** — `GetRun` and `ListRuns` both return the mixed run intact. The list check matters because `ListRuns` goes through the same `readRunInTx` (`run_reads.go:63`) and the API serves the list to the history panel (`api.go:92`).

**L74–L86** — `TestVersionTwoMigrationPreservesHTTPJSON` defines the stored JSON of a version 2 database by hand. The comment says these are "legacy flat HTTP objects, including their original whitespace." The whitespace is the detector: `json.Marshal` always writes compact JSON, so if the migration or any later code re-encoded these columns, even with identical data, the bytes would change. The output has no `type` key, which `AGENTS.md:126` calls "legacy HTTP".

**L87–L92** — Two subtests: an unmarked legacy file (no `application_id`) and a marked one. `migrate` treats them differently at `schema.go:84` (recognition is stricter for unmarked files, `identity.go:12`) and `schema.go:109-113` (only an unmarked file gets the marker written).

**L93–L99** — The legacy file is created with `sql.Open("sqlite", path)` directly, bypassing `store.Open` so that no version 3 migration runs yet. The driver name works because `sqlite.go:13` registered it for the whole package.

**L100–L107** — `initialSchema+runErrorMigration` creates the version 1 tables and applies the version 2 column, ending at `user_version = 2` (`schema.go:16-49`). The comment at `schema.go:14` asks that the first migration stay unchanged, which is what lets this test reuse the constants to build an authentic old file. The marked subtest sets `application_id` to the Patchbay value.

**L108–L114** — A one-step succeeded run is saved through `CreateRun`, which works on a version 2 file because every column it writes already exists.

**L115–L120** — Two raw `UPDATE` statements replace the generated JSON with the hand-written legacy text, so the file now holds exactly what a Patchbay build from before TCP support would have written.

**L121–L127** — `PRAGMA schema_version` is SQLite's schema change counter. Recording it before the upgrade gives the test a way to prove the version 3 migration touched no table, matching the comment at `schema.go:53`: "Existing tables, workflow snapshots, and result JSON remain unchanged." The handle is closed before `store.Open` takes over the file.

**L128–L135** — `for range 2` opens the file twice. The comment: "First open upgrades v2; reopening v3 must retain the same old JSON." The first `Open` runs `recognizeSchema` for version 2 (`schema.go:84-86`), applies `tcpCheckMigration` (`schema.go:102-106`), and in the unmarked case writes the marker (`schema.go:109-113`), all in one transaction. The second `Open` takes the no-write path (`schema.go:87-89`). The `defer db.Close()` inside the loop runs when the subtest returns, as a safety net; the explicit close at L160-162 is what separates the two opens.

**L136–L146** — Three pragmas are read through a map of destination pointers. `user_version` must be 3 (`schema.go:9`), `application_id` must be the Patchbay marker (`schema.go:12`), and `schema_version` must equal the recorded cookie. Setting `user_version` or `application_id` does not bump `schema_version`, so a changed cookie would mean an `ALTER` or `CREATE` ran. The message names both failure modes: "migration changed table structure or lost its version gate." That version gate is what stops older HTTP-only builds from opening the file (`AGENTS.md:127-129`, `README.md:596-598`).

**L147–L155** — The raw `definition_json` and `output_json` are read with a join and compared byte for byte with the constants. Any rewrite fails here.

**L156–L159** — `assertStoredRun` decodes the legacy JSON into the current structs and compares with the fixture, and `GetRun` does the same through the production reader. The legacy output has no `type`, so `CheckResult.Type` decodes as `""`, which the whole system reads as HTTP; `README.md:260` promises that adding TCP later "does not relabel older HTTP history as TCP."

**L160–L166** — The explicit close ends this iteration's handle so the next `Open` observes a committed, closed file. Closing braces for the loop, the subtest, the outer loop, and the function.

### internal/store/runner_integration_test.go

**L1–L13** — The same imports as the queue test minus `sync`: this test has one run, so no shared map needs a mutex.

**L14–L20** — `TestRunnerPersistsExecutionToSQLite` opens `runs.db`. Deferring `db.Close` before `runner.Close` again gives the shutdown order of `main.go:79`.

**L21–L23** — The three-step fixture definition is the workflow. `id` will hold the run ID the runner assigns; `completed` is a buffered channel for the final-save signal.

**L24–L28** — `engine.New(2, 0, ...)` has two active slots and no queue, so the single run starts immediately (`runner.go:122`, `130-132`). The executor begins by reading the run from SQLite. `id` is safe to read here without a lock: the record callback writes it under the runner's mutex before `Start` launches the goroutine (`runner.go:140` runs before `runner.go:154`), and starting a goroutine orders everything before it.

**L29–L36** — For the step being executed, the saved row must be `running` with a start time, and for any step after the first, the previous step must already be saved as `succeeded`. The first condition is the start save at `runner.go:230-233`; the second is the result save at `runner.go:269`, which happens before the loop moves on. Together they are `AGENTS.md:86`: "Save step start before execution and step result before proceeding." A violation makes the executor fail the step, so the final status would be `failed` and L66 would report it. The executor returns a healthy result with status code 200, something the assertions at L70 can recognize.

**L37–L39** — The record callback captures the run ID and calls the real `CreateRun`, exactly like `main.go:107-108`.

**L40–L48** — The update callback calls the real `UpdateRun`, like `main.go:109-110`, and signals once the saved status is no longer `running`, which only the final save in `finishRun` produces (`runner.go:302`, `309`).

**L49–L52** — Constructor check and deferred `Close`.

**L53–L55** — `Start` admits the run. The returned value is discarded because the test reads everything back from SQLite.

**L56–L60** — Wait for the final save or fail after two seconds with "no final snapshot was saved".

**L61–L65** — `Close` waits for the run goroutine to exit, then the final state is read from the database.

**L66–L68** — The run is `succeeded`, has a finish time (`runner.go:301-302`), and has as many saved steps as the definition has steps.

**L69–L74** — Every step is `succeeded` with both timestamps and an output whose status code is 200, the value the fake executor returned at L36. This closes the loop from `runner.go:257` (output stored on the working copy) through `UpdateRun` (`runs.go:96-110`) to `readRunInTx` (`run_reads.go:106-109`): what the executor produced is what SQLite returns. Compared with `TestRunSavesProgressAndCompletion` (`internal/engine/runner_updates_test.go:15-82`), which checks the same save sequence against an in-memory fake that appends each snapshot to a slice, this test proves the ordering holds with the real callbacks and real transactions.

## What these tests prove

| test function | production rule it proves | how it injects failure or controls time | test file:line |
| --- | --- | --- | --- |
| `assertStoredRun` (helper) | Columns written by `CreateRun` and `UpdateRun` (`runs.go:29-33`, `48-52`, `75-77`, `106-110`) hold exactly the run's fields, with `NULL` for unset times and outputs (`runs.go:39`, `127-132`) | Reads raw rows with `database/sql` instead of calling `GetRun` | `runs_test.go:32` |
| `runFixture` (helper) | A definition that passes `workflow.Validate` (`workflow.go:63-117`) and a run with succeeded, failed, and skipped steps as `runner.go:255`, `257`, `303-307` shape them | Fixed UTC instants from `time.UnixMilli`; fresh slices and pointers per call | `runs_test.go:145` |
| `TestCreateRunStoresSnapshotAndStepResults` | One transaction saves run, snapshot, and ordered steps (`runs.go:23-61`) | Temporary database from `t.TempDir` | `runs_test.go:16` |
| `TestCreateRunRollsBackWithoutReplacingHistory` | Duplicate run or step IDs fail on the primary keys (`schema.go:18`, `38`) and the deferred rollback removes partial inserts (`runs.go:27`, `53-54`) | Reuses an existing run ID; gives one run two steps with the same ID | `runs_test.go:101` |
| `TestGetRunReadsPersistedHistory` | `GetRun` rebuilds finished, running, and queued runs from a reopened file, filtering steps by run (`run_reads.go:77-116`, `schema.go:38`) | Closes and reopens the file; saves a second run with identical step IDs | `run_reads_test.go:16` |
| `TestGetRunErrorsReturnNoPartialHistory` | A missing run wraps `sql.ErrNoRows` (`run_reads.go:81`); a decode failure returns a zero run (`run_reads.go:107-108`); the read transaction is released (`run_reads.go:22`, `95`) | Raw `UPDATE` writes `'{'` into `output_json`; five-second context exposes a leaked connection | `run_reads_test.go:74` |
| `TestListRunsOrdersAndLimitsCompleteHistory` | Newest creation time first, ID descending on ties, SQL `LIMIT`, full steps per run (`run_reads.go:39-40`, `61-68`) | Chosen creation times and IDs that disagree; limits 1, 2, 100 | `run_list_test.go:15` |
| `TestListRunsEmptyHistoryAndInvalidLimits` | Empty history is a non-nil slice (`run_reads.go:61`); limits outside 1 to 100 are rejected before any query (`run_reads.go:30-32`) | Empty database; limits -1, 0, 101 | `run_list_test.go:57` |
| `TestListRunsErrorsReturnNoPartialHistory` | IDs are collected under the limit before runs are read; any failure returns nil (`run_reads.go:39-59`, `63-66`) | Corrupts the run that only limit 2 reaches | `run_list_test.go:77` |
| `TestUpdateRunSavesProgressAndPreservesWorkflow` | Updates write status, times, outputs, and error but never workflow fields, step names, or creation time (`runs.go:64-67`, `75-77`, `106-110`); identical rewrites count as one affected row (`runs.go:81-85`, `114-118`) | Edits workflow and step names on the run passed to `UpdateRun`; saves the same error twice and then clears it | `run_updates_test.go:11` |
| `TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory` | Missing run (`runs.go:83-84`), wrong step count (`runs.go:92-93`), and unknown, duplicate, or reordered step IDs (`runs.go:105-117`) are rejected and rolled back (`runs.go:73`) | Six table-driven mutations of a valid snapshot | `run_updates_test.go:51` |
| `TestQueuedRunPersistsBeforeExecutionAndKeepsCreationTime` | A queued run is saved before `Start` returns with no start time (`runner.go:126-145`, `runs.go:33`); promotion is saved before the step executes (`runner.go:220-233`, `247`); `created_at` is never updated (`runs.go:75-77`) | Real runner with limits 1/1; executor blocks on a channel and reads SQLite mid-step; mutex-guarded ID map; two-second `time.After` guard | `run_queue_test.go:15` |
| `TestMarkUnfinishedRunsInterruptedPreservesSavedResults` | Queued and running runs become interrupted, running steps interrupted, pending steps skipped, everything else untouched, and the call is idempotent (`run_interruptions.go:24-43`) | Eight hand-built runs covering unfinished and terminal states; the helper is called twice | `run_interruptions_test.go:14` |
| `TestMarkUnfinishedRunsInterruptedRollsBackStepChanges` | Step and run updates commit or roll back together (`run_interruptions.go:20`, `31-35`) | A SQLite trigger with `RAISE(ABORT)` fails the second statement | `run_interruptions_test.go:75` |
| `TestMarkUnfinishedRunsInterruptedHonorsCancellation` | A dead context stops the helper before any write (`run_interruptions.go:16-19`) | `context.WithCancel` canceled before the call | `run_interruptions_test.go:100` |
| `unfinishedRunFixture` (helper) | The run shape between a step's start save and its result save (`runner.go:233-247`) | Edits the shared fixture in place | `run_interruptions_test.go:118` |
| `TestMixedHTTPAndTCPResultsSurviveUpdatesAndReopen` | HTTP and TCP results share `CheckResult` with `omitempty` fields (`workflow.go:47-59`), survive `UpdateRun`, reopen, `GetRun`, and `ListRuns`; unhealthy is not a failure (`AGENTS.md:91`) | Hand-built TCP config and result; close and reopen | `tcp_results_test.go:15` |
| `TestVersionTwoMigrationPreservesHTTPJSON` | The version 3 migration changes only `user_version` and the marker (`schema.go:51-54`, `102-113`) and leaves legacy JSON byte-identical; reopening is a no-op (`schema.go:87-89`) | Builds a version 2 file with `sql.Open` and the migration constants; records `schema_version`; hand-written JSON with whitespace | `tcp_results_test.go:75` |
| `TestRunnerPersistsExecutionToSQLite` | Step start is saved before execution and step result before the next step (`runner.go:233`, `269`), and the final save completes the record (`runner.go:286`, `300-312`) with the `main.go:107-111` wiring | Real runner with real store callbacks; executor reads SQLite during each step; two-second guard | `runner_integration_test.go:14` |

## Check yourself

1. In `TestCreateRunRollsBackWithoutReplacingHistory`, the second failing call inserts the `run-2` row and its first step before failing. Which production line removes them, and what would the count query at L124-125 report if that line were deleted?

<details><summary>Answer</summary>`runs.go:27`, `defer tx.Rollback()`. When the step insert at `runs.go:48-52` fails on the `(run_id, step_id)` primary key, `CreateRun` returns at `runs.go:54` and the deferred rollback discards the whole transaction. Without it the function would return the error but leave the transaction open; with one connection in the pool the next query would block, and if SQLite eventually committed or the connection were reused, the database would hold two runs and four steps, so the test would report "failed inserts changed history: 2 runs, 4 steps" or time out.</details>

2. `TestGetRunReadsPersistedHistory` builds its fixture times with `time.UnixMilli(...).UTC()`. Why would dropping `.UTC()` from `runs_test.go:146` make the `DeepEqual` at `run_reads_test.go:67` fail even though the instants are the same?

<details><summary>Answer</summary>`reflect.DeepEqual` compares the fields inside `time.Time`, including its location pointer. `time.UnixMilli` returns a time in the local location; `readRunInTx` builds its times with `.UTC()` (`run_reads.go:83`, `86`, `123`), which stores a nil location. Two values with different location pointers are not `DeepEqual`, even though `Equal` would return true. The helper `assertStoredRun` has the same requirement at `runs_test.go:78` and `82`. The store always returns UTC so that JSON written by `api.go` is stable regardless of the server's time zone.</details>

3. `TestUpdateRunSavesProgressAndPreservesWorkflow` saves the error `"Execution could not continue."` twice in a row and expects both calls to succeed. Which check in `UpdateRun` would make the second call fail if SQLite reported only rows whose values changed, and why does the runner need the second call to succeed?

<details><summary>Answer</summary>`runs.go:81-85` returns an error wrapping `sql.ErrNoRows` unless `RowsAffected` is exactly 1, and `runs.go:114-118` does the same per step. SQLite counts every row an `UPDATE` matched, so an identical rewrite still counts as one. The runner relies on this in `saveFinalRun` (`runner.go:325-342`), which retries the same completed snapshot up to three times; if a retry after a partially successful attempt were reported as "no rows", the final save would be marked failed and the run would carry `FinalSaveFailed` even though SQLite holds the right data.</details>

4. In `TestQueuedRunPersistsBeforeExecutionAndKeepsCreationTime`, why does the executor for workflow `b` see `saved.Status == "running"` even though `b` was saved as `queued`, and which two production lines are responsible?

<details><summary>Answer</summary>When `a` finishes, its goroutine promotes `b` under the mutex at `runner.go:292-296`, which launches `b`'s goroutine. That goroutine starts by copying the queued snapshot, sets `running` and a fresh `StartedAt` at `runner.go:220-223`, and then performs the first step-start save at `runner.go:233` before calling `execute` at `runner.go:247`. The save goes through the update callback to `UpdateRun`, which writes `status` and `started_at` (`runs.go:75-77`). The executor's `GetRun` therefore sees the promoted state. If the save were moved after `execute`, the executor would read `queued` and return the "execution began before promotion was saved" error.</details>

5. `TestMarkUnfinishedRunsInterruptedPreservesSavedResults` includes a run with status `interrupted` whose steps are `running` and `pending`. After the helper runs, those steps are still `running` and `pending`. Which part of the SQL guarantees that, and why is leaving them alone the right behavior?

<details><summary>Answer</summary>The subquery at `run_interruptions.go:27`: `run_id IN (SELECT id FROM runs WHERE status IN ('queued', 'running'))`. Steps are only touched when their parent run is unfinished, and the run update at `run_interruptions.go:31-32` only touches queued and running runs. A terminal run is history; the helper's comment at `run_interruptions.go:22-23` says "Terminal steps and steps belonging to other runs keep their saved state." Rewriting steps of a finished run would change saved history, which `AGENTS.md:103` forbids, and would make the second call non-idempotent, which L63-67 checks by expecting zero changes.</details>

6. `TestVersionTwoMigrationPreservesHTTPJSON` records `PRAGMA schema_version` before the upgrade and compares it afterwards. What change to `tcpCheckMigration` at `schema.go:54` would this comparison catch that the `user_version` and raw-JSON checks would miss?

<details><summary>Answer</summary>Any DDL, for example adding a column such as `ALTER TABLE run_steps ADD COLUMN result_type TEXT`. `user_version` would still read 3, and the JSON columns would still hold the original bytes, so the other two checks would pass. But SQLite increments `schema_version` on every `CREATE`, `ALTER`, or `DROP`, so `reopenedCookie != schemaCookie` would fail with "migration changed table structure". That enforces the comment at `schema.go:51-53`: version 3 "records that capability" in the version number only, so older builds refuse the file while the tables stay identical.</details>

## Experiments

1. **Break the list tiebreak.** In `internal/store/run_reads.go:40` change `ORDER BY created_at DESC, id DESC` to `ORDER BY created_at DESC, id ASC`. Run `go test ./internal/store -run 'TestListRuns'`. Expected: `TestListRunsOrdersAndLimitsCompleteHistory` fails at `run_list_test.go:52` on the first limit, 1, with `run-a` listed where `run-b` was expected (the `t.Fatalf` ends the test, so limits 2 and 100 are never tried), and `TestListRunsErrorsReturnNoPartialHistory` fails at `run_list_test.go:101`, because limit 1 now reaches the corrupt `run-a` first and returns a JSON error. Revert the change.

2. **Return the partial run on a decode failure.** In `internal/store/run_reads.go:108` change `return engine.Run{}, fmt.Errorf(...)` so that it returns `run` instead of `engine.Run{}` (keep the error). Run `go test ./internal/store -run 'TestGetRunErrorsReturnNoPartialHistory|TestListRunsErrorsReturnNoPartialHistory'`. Expected: the `GetRun` test fails at `run_reads_test.go:99` with "expected an empty run and a JSON error" and prints a run with one decoded step; the list test still passes, because `ListRuns` discards the run at `run_reads.go:64-66`. Revert.

3. **Drop the position match.** In `internal/store/runs.go:108` change `WHERE run_id = ? AND step_id = ? AND position = ?` to `WHERE run_id = ? AND step_id = ?` and remove the trailing `position` argument on `runs.go:110`. Run `go test ./internal/store -run TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory -v`. Expected: the subtests `missing_run`, `missing_step`, `extra_step`, and `unknown_step` still pass, but `duplicate_step` and `reordered_steps` fail at `run_updates_test.go:86` with "expected the mismatched snapshot to be rejected", because a step ID now matches regardless of its position. Revert.

4. **Watch the saves land in SQLite.** In `internal/store/runner_integration_test.go:41`, before `if err := UpdateRun(...)`, add `t.Logf("save: run=%s steps=%s/%s/%s", run.Status, run.Steps[0].Status, run.Steps[1].Status, run.Steps[2].Status)`. Run `go test ./internal/store -run TestRunnerPersistsExecutionToSQLite -v`. Expected: seven log lines, three pairs of `running` with a step going `running` then `succeeded`, and a final line with `succeeded/succeeded/succeeded`, matching the order of `runner.go:233`, `269`, and `309`. Remove the line afterwards.

## Next

Chapter 26, "Tests: API responses, history overlay, host guard, schedules" ([26-tests-httpapi.md](26-tests-httpapi.md))
