# 10. Saving and reading runs

Files: [internal/store/runs.go](../../internal/store/runs.go), [internal/store/run_reads.go](../../internal/store/run_reads.go)

Revision: commit 1c4786e

## Role in the system

These two files are the data half of the `store` package. Chapter 8 covered the startup half: opening the file, the write check, and reconciliation. This chapter covers the four exported functions that the running program calls for the rest of its life. `CreateRun` and `UpdateRun` (`runs.go:17`, `runs.go:68`) turn an `engine.Run` value into rows in the `runs` and `run_steps` tables (`internal/store/schema.go:17-40`). `GetRun` and `ListRuns` (`run_reads.go:16`, `run_reads.go:29`) turn those rows back into `engine.Run` values. Each function opens one transaction, does its work, and ends it. Nothing in these files runs a check, touches HTTP, holds a Go mutex, or keeps state between calls. The only shared object is the `*sql.DB` that `store.Open` returned, and that pool holds exactly one connection (`internal/store/sqlite.go:59`), so every call here takes its turn on it.

On the path from a browser click to a saved run, these functions are the last stop. The click becomes `POST /api/workflows/{id}/runs`, the handler calls `runner.Start` (`internal/httpapi/api.go:65`), and `Start` calls its `recordNewRun` callback while it still holds the runner's mutex (`internal/engine/runner.go:136-145`). That callback is the closure at `cmd/server/main.go:107-109`, which calls `store.CreateRun`. If `CreateRun` fails, `Start` returns `ErrRecordRun` before it records the run in memory (`runner.go:142-148`), the browser gets a 500, and no step ever runs. After admission, the run goroutine saves each step's start and each step's result through `saveRunUpdate` (`runner.go:233`, `runner.go:269`), and the final state through `saveFinalRun` with retries (`runner.go:309`, `runner.go:316-344`). All of those reach the closure at `main.go:109-111`, which calls `store.UpdateRun`. Meanwhile the browser polls `GET /api/runs` and `GET /api/runs/{id}`, whose handlers call `store.ListRuns` and `store.GetRun` (`api.go:92`, `api.go:107`) and encode the returned values as the JSON that `web/src/api.ts:52-63` describes.

The engine never imports `store`. It declares function types for the two saves (`runner.go:55`, `runner.go:60`) and `main` plugs the store functions in. That is why the engine's tests can pass `nil` callbacks and stay in memory, while the store's own integration tests wire the real functions to a real runner (`internal/store/runner_integration_test.go:39-41`, `internal/store/run_queue_test.go:49-51`). The roadmap fixed the storage approach these files follow: "SQLite through `database/sql`, explicit SQL queries, and versioned migrations" (`docs/roadmap.md:84`).

## Concepts you need

- **Import direction and function-typed callbacks.** Go forbids import cycles: if `engine` imported `store` and `store` imported `engine`, the program would not compile. `store` imports `engine` for the `Run` type, and `engine` only declares function types (`RecordNewRun`, `UpdateSavedRun`) that `main` fills with store calls. Chapter 6 covered the engine side; this chapter is the other end of the wire. [Spec: Import declarations](https://go.dev/ref/spec#Import_declarations)
- **`database/sql` transactions.** `db.BeginTx(ctx, nil)` starts a transaction and returns a `*sql.Tx`. Statements run through the `Tx` become visible together at `Commit` or vanish together at `Rollback`. The idiom `defer tx.Rollback()` right after `BeginTx` means every early `return` undoes partial work; after a successful `Commit` the deferred `Rollback` is a harmless no-op that returns `sql.ErrTxDone`. If the context ends before `Commit`, `database/sql` rolls the transaction back itself. [DB.BeginTx](https://pkg.go.dev/database/sql#DB.BeginTx), [Tx.Commit](https://pkg.go.dev/database/sql#Tx.Commit), [Tx.Rollback](https://pkg.go.dev/database/sql#Tx.Rollback)
- **Placeholders and driver arguments.** SQL text uses `?` where a value goes, and the values follow as extra arguments to `ExecContext` or `QueryContext`. The driver sends them separately from the SQL, so a value can never change the statement's shape. Arguments are typed `any`, and a `nil` argument becomes SQL `NULL`. [Tx.ExecContext](https://pkg.go.dev/database/sql#Tx.ExecContext), [Go wiki: SQLInterface](https://go.dev/wiki/SQLInterface)
- **`any`.** `any` is an alias for the empty interface `interface{}`: a value of any type can be stored in it. These files use it where a variable must hold either `nil` or a concrete value (`int64`, `string`) that the driver will turn into a column value. [Spec: Predeclared identifiers](https://go.dev/ref/spec#Predeclared_identifiers)
- **`QueryRowContext`, `Scan`, and `sql.ErrNoRows`.** `QueryRowContext` runs a query expected to return at most one row. `Scan` copies that row's columns into the variables whose addresses you pass, in column order. If there is no row, `Scan` returns the sentinel `sql.ErrNoRows`. Wrapping it with `%w` lets callers test `errors.Is(err, sql.ErrNoRows)` several layers up. [Tx.QueryRowContext](https://pkg.go.dev/database/sql#Tx.QueryRowContext), [Row.Scan](https://pkg.go.dev/database/sql#Row.Scan), [sql.ErrNoRows](https://pkg.go.dev/database/sql#pkg-variables)
- **`QueryContext` and the `Rows` loop.** A multi-row query returns `*sql.Rows`, a cursor. The pattern is `for rows.Next() { rows.Scan(...) }`, then `rows.Err()` to learn whether the loop stopped because the data ended or because something failed, then `rows.Close()` to release the cursor. `Next` returning `false` does not tell you which; only `Err` does. [sql.Rows](https://pkg.go.dev/database/sql#Rows), [Rows.Err](https://pkg.go.dev/database/sql#Rows.Err), [Rows.Close](https://pkg.go.dev/database/sql#Rows.Close)
- **`sql.NullInt64` and `sql.NullString`.** A Go `int64` cannot be "absent", but a SQL column can be `NULL`. These structs carry a value plus a `Valid` flag: `Valid` is `false` when the column was `NULL`. Scanning a `NULL` into a plain `int64` is an error, so nullable columns need these types. [sql.NullInt64](https://pkg.go.dev/database/sql#NullInt64), [sql.NullString](https://pkg.go.dev/database/sql#NullString)
- **`sql.Result` and `RowsAffected`.** `ExecContext` returns a `Result`. `RowsAffected()` reports how many rows an `UPDATE` matched. SQLite counts a row even when the new values equal the old ones, so "matched one row" is a reliable existence check. [sql.Result](https://pkg.go.dev/database/sql#Result)
- **`encoding/json` with pointer targets.** `json.Marshal(v)` turns a value into JSON bytes using struct tags; given a pointer, it encodes what the pointer points at. `json.Unmarshal(data, &target)` fills `target`. If `target` is itself a `nil` pointer, `Unmarshal` allocates the pointed-to value for you. That is why `run_reads.go:107` can pass `&step.Output` where `step.Output` is a nil `*workflow.CheckResult`. [json.Marshal](https://pkg.go.dev/encoding/json#Marshal), [json.Unmarshal](https://pkg.go.dev/encoding/json#Unmarshal)
- **Unix milliseconds and `time.Time`.** `t.UnixMilli()` gives the number of milliseconds since 1970-01-01 UTC as an `int64`, which SQLite stores as an `INTEGER`. `time.UnixMilli(n)` reverses it, and `.UTC()` sets the time's location to UTC so values compare equal and print with a `Z`. `t.IsZero()` reports the zero time, the Go value that means "not set" for a non-pointer `time.Time`. [Time.UnixMilli](https://pkg.go.dev/time#Time.UnixMilli), [time.UnixMilli](https://pkg.go.dev/time#UnixMilli), [Time.IsZero](https://pkg.go.dev/time#Time.IsZero)
- **Value time fields versus pointer time fields.** `engine.Run.StartedAt` is a `time.Time` value with `omitzero`, so "not started" is the zero time. `Run.FinishedAt` and both `StepRun` times are `*time.Time` with `omitempty`, so "not finished" is `nil`. The store has to translate both styles to and from `NULL`, which is what the two helpers at the bottom of each file do. [encoding/json: struct tags](https://pkg.go.dev/encoding/json#Marshal)
- **`=` versus `:=`, redeclaration, and block scope.** `:=` declares new variables; `=` assigns to existing ones. Inside a new block (a loop body or an `if` body) `:=` creates fresh variables even when the outer function has one with the same name, and the outer one is untouched. In the same block, `a, err := f()` is allowed when at least one name on the left is new; `err` is then reused. `runs.go:29` uses `=` and `runs.go:48` uses `:=` for the same reason. [Spec: Short variable declarations](https://go.dev/ref/spec#Short_variable_declarations), [Spec: Declarations and scope](https://go.dev/ref/spec#Declarations_and_scope)
- **`if init; cond { } else if cond2 { }`.** An `if` may start with a short statement, and variables it declares are visible in every branch of the chain, including `else if`. `runs.go:81-85` declares `affected` and `err` once and checks both. [Spec: If statements](https://go.dev/ref/spec#If_statements)
- **`for i, v := range slice`.** `range` over a slice yields the index and a copy of each element. The index is what these files store as the step's `position`. Because `v` is a copy, changing it does not change the slice; the store only reads, so that does not matter here. [Spec: For statements with range clause](https://go.dev/ref/spec#For_range)
- **`make([]T, 0, n)` and nil versus empty slices.** `make` with length `0` and capacity `n` gives an empty slice that can grow to `n` elements without reallocating. It is also non-`nil`: `json.Marshal` turns a `nil` slice into `null` and an empty non-`nil` slice into `[]`. `reflect.DeepEqual` treats them as different too; the list tests check `nil` directly to tell the cases apart (`run_list_test.go:66`, `run_list_test.go:71`). [Go blog: Slices](https://go.dev/blog/slices-intro), [reflect.DeepEqual](https://pkg.go.dev/reflect#DeepEqual)
- **Exported and unexported identifiers.** A name that starts with a capital letter (`CreateRun`) is visible to other packages; a lowercase name (`readRunInTx`, `unixMilliOrNull`) is private to the package. The two helpers and the shared reader are unexported because nothing outside `store` should call them. [Spec: Exported identifiers](https://go.dev/ref/spec#Exported_identifiers)
- **Method chaining across lines.** A line may end with a `.` and continue on the next line: `tx.QueryRowContext(...).` then `Scan(...)`. Go inserts semicolons at line ends only after certain tokens, and `.` is not one of them, so the statement continues. [Effective Go: Semicolons](https://go.dev/doc/effective_go#semicolons)
- **`%q` and `%w` in `fmt.Errorf`.** `%q` prints a string in double quotes with escapes, so an ID with spaces reads clearly in a log. `%w` wraps an error so `errors.Is` and `errors.As` can still find the original. Every error these files return names the run, and often the step, this way. [fmt verbs](https://pkg.go.dev/fmt), [errors.Is](https://pkg.go.dev/errors#Is), [errors.As](https://pkg.go.dev/errors#As)
- **SQLite constraints as a safety net.** `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, `CHECK`, and `REFERENCES ... ON DELETE CASCADE` are rules the database enforces on every write. A violating `INSERT` or `UPDATE` fails with an error instead of storing bad data, and with a transaction open that error rolls everything back. The schema in `schema.go:17-40` is what makes some of the checks in this chapter unnecessary to write in Go. [SQLite: CREATE TABLE constraints](https://sqlite.org/lang_createtable.html#constraints), [SQLite: Foreign keys](https://sqlite.org/foreignkeys.html), [SQLite: NULL handling](https://sqlite.org/nulls.html)
- **Transactions as a consistent snapshot.** Inside one SQLite transaction, every statement sees the same committed state; a commit from elsewhere cannot appear between two `SELECT`s. That holds for a read-only transaction too, which is why the readers open one even though they never write. [SQLite: Transactions](https://sqlite.org/lang_transaction.html), [SQLite: Isolation](https://sqlite.org/isolation.html)
- **`ORDER BY` with a tie-break and `LIMIT`.** `ORDER BY a DESC, b DESC` sorts by `a`, then by `b` among equal `a` values, so the order is the same on every run of the query. `LIMIT ?` caps the row count and accepts a bound parameter. [SQLite: SELECT](https://sqlite.org/lang_select.html)
- **Pointers to local variables.** Go lets a function return the address of a local variable (`run_reads.go:123-124`). The compiler notices that the value escapes and allocates it on the heap, so the pointer stays valid. This is safe in Go, unlike C. [Go FAQ: stack or heap](https://go.dev/doc/faq#stack_or_heap)

## Walkthrough

### internal/store/runs.go

**L1–L1** — `package store`. The package doc comment lives in `sqlite.go:1`, so this file starts with the bare clause. All files in the directory share one namespace, which is why `unixMilliOrNull` (L127) is usable from `schema_test.go:32` without an import.

**L2–L2** — Blank line.

**L3–L12** — Imports. `context` for cancellation, `database/sql` for `*sql.DB`, `*sql.Tx`, and `sql.ErrNoRows`, `encoding/json` for the two JSON columns, `fmt` for error wrapping, and `time` for the `*time.Time` parameter of the helper at L127. The two project imports give the types this file translates: `engine.Run` and `engine.StepRun` (`internal/engine/runner.go:24-46`) and `workflow.Definition` (`internal/workflow/workflow.go:20-27`). `store` depends on `engine`; the reverse would be an import cycle.

**L13–L13** — Blank line.

**L14–L16** — The contract for `CreateRun` in three sentences. "Atomically" is met by one transaction (L23-L60). "A validated definition" is the caller's job: `runner.Start` validates first (`runner.go:106-108`), and the store trusts it. "An existing run ID is an error; this function never replaces saved history" is enforced by the `PRIMARY KEY` on `runs.id` (`schema.go:18`) rather than by an `INSERT OR REPLACE`, and tested at `internal/store/runs_test.go:112-116`.

**L17–L17** — The signature. Context first, then the database, then the run and the definition by value. By value matters: the runner already passed a copy made by `copyRun` (`runner.go:140`), and this function reads it without ever writing to it, so nothing it does can be seen by the run goroutine. The definition comes along because this is the one place the workflow snapshot is written. The engine declares this exact shape as `RecordNewRun` (`runner.go:55`), and `main.go:107-109` adapts it.

**L18–L21** — Encode the workflow definition to JSON before any database work. Two reasons. First, encoding cannot fail for the types involved today, but if it ever did, the function returns before opening a transaction, so there is nothing to roll back. Second, the roadmap asks for "short transactions" and says never to hold one while doing other work (`docs/roadmap.md:93`); encoding is work. `json.Marshal` uses the struct tags on `Definition`, `Step`, and `CheckConfig` (`workflow.go:20-45`); `CheckConfig`'s `omitempty` tags keep an HTTP step's `host` and `port` out of the snapshot. Why store the definition at all: "A historical run must remain understandable after a user edits its workflow" (`roadmap.md:105`). The snapshot is the only record of what the run actually executed once the file in `workflows/` changes.

**L22–L22** — Blank line.

**L23–L26** — Begin the transaction. `nil` options means defaults. With one pooled connection (`sqlite.go:59`), this call also waits until no other store call holds that connection; the wait is bounded by `ctx`, which the runner built with a five-second timeout (`runner.go:139`). A failure here is wrapped as `begin run insert`.

**L27–L27** — `defer tx.Rollback()` with its reason: "Any failed insert rolls back the entire run." Every `return` below L27, on any of the four error paths, undoes the run row and any step rows already inserted. After `Commit` at L58 succeeds, this `Rollback` is a no-op. `TestCreateRunRollsBackWithoutReplacingHistory` makes the second step insert fail and then counts rows: still one run and three steps from the first, successful call (`runs_test.go:118-130`). Without this line, a half-inserted run would sit in an open transaction that holds the single connection until the context expired and `database/sql` rolled it back.

**L28–L28** — Blank line.

**L29–L33** — Insert the run row. Nine columns, nine `?` placeholders, nine arguments. `_, err =` uses `=`, not `:=`, because `err` already exists from L18 and the result is not needed. The conversions are the whole point of the line. `string(definitionJSON)` makes the snapshot a `TEXT` value as the schema declares (`schema.go:21`) instead of a `BLOB`. `run.CreatedAt.UnixMilli()` is always a number because every run has a creation time (`runner.go:128`). `unixMilliOrNull(&run.StartedAt)` takes the address of the value field so the helper can treat a zero time as `NULL`; a queued run has no start (`runner.go:126-132`), and the README promises it appears "with their creation time and no start time" (`README.md:166-167`). `run.FinishedAt` is already a pointer. `run.Error` is the run-level reason (`runner.go:39`), stored in the column that migration 2 added (`schema.go:46-47`).

**L34–L36** — Wrap the insert error with the run ID. A duplicate ID lands here as a primary-key violation. The function never checks for duplicates itself; the database does it, atomically, which is the only way to be sure under concurrent callers.

**L37–L37** — Blank line.

**L38–L38** — Loop over the steps with `position` as the index. The index is saved as the step's `position` column (`schema.go:31`), which has a `CHECK (position >= 0)` and a `UNIQUE (run_id, position)` constraint (`schema.go:39`). Order is data here: `readRunInTx` restores it with `ORDER BY position` (`run_reads.go:91`), and `UpdateRun` demands the same position later (L108).

**L39–L47** — Encode the step output, or leave it `NULL`. The comment at L39 states the distinction: "A nil output is SQL NULL, distinct from an encoded result object." `var outputJSON any` starts as `nil`; a `nil` argument becomes `NULL` in the column. When `step.Output` is set (a succeeded check, `runner.go:257`), it is marshalled and stored as text. The inner `encoded, err :=` declares a new `err` scoped to the `if` body; the outer `err` from L18 is untouched. A check result's `type` field is omitted when empty (`workflow.go:50`), which is how "a missing result type means legacy HTTP" (`AGENTS.md:126`) survives in storage: the store writes exactly what the executor produced and never rewrites old JSON.

**L48–L52** — Insert the step row. `_, err :=` here is a declaration because `err` is not yet declared in the loop body block; it shadows the function-level `err` for the rest of this iteration. The arguments mirror the run row: ID, position, name, status, two nullable times through the helper (both already pointers on `StepRun`, `runner.go:28-29`), the output from L40, and the step error string. The `run_id` value ties the row to the run through the foreign key (`schema.go:29`), which `foreign_keys(1)` at `sqlite.go:50` makes SQLite enforce.

**L53–L55** — Wrap the step insert error with both IDs. A duplicate step ID within one run violates `PRIMARY KEY (run_id, step_id)` (`schema.go:38`) and ends up here; validation already rejects duplicate step IDs (`workflow.go:80-81`), so this is the backstop, not the first line of defence. The deferred `Rollback` then discards the run row from L29 and every earlier step row.

**L56–L56** — Closing brace of the loop.

**L57–L57** — Blank line.

**L58–L60** — Commit. This is the moment the run becomes history that survives a crash. Only now has `CreateRun` kept the promise "Save a new run and its workflow snapshot before acceptance" (`AGENTS.md:83`): the runner has not yet added the run to its maps (`runner.go:146-148`) when this returns. A commit can fail even after every insert succeeded, for example when the disk fills while SQLite writes the changed pages back; the startup write check in chapter 8 exercises that same journal and commit path early (`internal/store/writable.go:12-17`). Note that `database/sql` checks the context before committing, but the driver's own `COMMIT` runs without a context (`internal/store/writable.go:15-16`), so a cancellation that arrives during the commit itself cannot interrupt it.

**L61–L61** — `return nil`. The deferred `Rollback` runs now and does nothing.

**L62–L62** — Closing brace.

**L63–L63** — Blank line.

**L64–L67** — The contract for `UpdateRun`. Three things are fixed by it. What changes: "execution state for an existing run and its steps atomically". What never changes: "Workflow details, creation time, step names, and step order", which is why the `UPDATE` at L75-L77 names only four columns. What the caller guarantees: "every step in its original order" and one update at a time per run. The last sentence, "this function does not enforce execution status transitions", means there is no state machine in SQL: the run goroutine is the single writer for its run (`runner.go:218`), so the store trusts the order of its calls. The engine declares this shape as `UpdateSavedRun` (`runner.go:57-60`).

**L68–L68** — The signature. No definition parameter: a progress save must not be able to rewrite the snapshot, and the type system makes that impossible rather than merely discouraged.

**L69–L73** — Begin and defer rollback, as in `CreateRun`. The context is the runner's `saveCtx`: five seconds from `r.ctx` for a progress save (`runner.go:350`) or from a non-cancelable copy of `r.ctx` for a final save (`runner.go:286`, `runner.go:322`). If another store call holds the single connection, this `BeginTx` waits here.

**L74–L74** — Blank line.

**L75–L77** — Update the run row. Four columns: `status`, `started_at`, `finished_at`, `error`. The `WHERE id = ?` is what makes this an update of one run. `started_at` is written on every save because promotion from `queued` to `running` sets it (`runner.go:222`) and "Promotion is saved before the first step executes" (`README.md:167`). `error` is written on every save so a progress-save failure reason (`runner.go:238`, `runner.go:276`) reaches the file with the final save, and an empty string clears it; `TestUpdateRunSavesProgressAndPreservesWorkflow` writes the same reason twice and then an empty one (`internal/store/run_updates_test.go:40-47`). The columns not named here are the ones the doc comment protects; the same test edits `WorkflowID`, `WorkflowName`, and a step name before saving and then asserts the stored values are the originals (`run_updates_test.go:36-38`, `run_updates_test.go:43-47`).

**L78–L80** — Wrap a statement failure with the run ID.

**L81–L85** — Insist that exactly one row matched. `affected` and `err` are declared in the `if` header and visible in the `else if`. An `UPDATE` of a nonexistent ID is not a SQL error; it just matches zero rows, so without this check a save for a run that was never created would silently succeed. The error wraps `sql.ErrNoRows` even though no query returned it, so a caller could ask `errors.Is(err, sql.ErrNoRows)`; no current caller does (the runner treats every save error alike), but the wrapped sentinel documents the cause. The `missing run` case of `TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory` ends here (`run_updates_test.go:56`).

**L86–L86** — Blank line.

**L87–L94** — Count the saved steps and compare with the snapshot. The comment says why: "A full snapshot must describe exactly the steps saved when the run began." The per-step `UPDATE` below only touches rows that exist, so a snapshot with fewer steps would update some rows and silently leave the rest, and a snapshot with more steps would fail only at the extra one. Checking the count first turns both into a clear error before any step row changes. The `missing step` and `extra step` cases land here (`run_updates_test.go:57-58`). The count query runs inside the transaction, so it sees the same rows the updates will.

**L95–L95** — Blank line.

**L96–L96** — Loop over the snapshot's steps with the index as `position`, exactly as `CreateRun` did.

**L97–L104** — Encode the output or use `NULL`, the same code as L40-L47 without the comment. A step that failed has `Output == nil` and an `Error` string (`runner.go:255`); a step that succeeded has an output and an empty error (`runner.go:257`). Every save rewrites every step's output, which the roadmap acknowledges: "Each progress save currently rewrites every step row of the run" (`roadmap.md:107`), with per-attempt rows planned for later.

**L105–L110** — Update one step row by run ID, step ID, and position. The comment at L105 explains the third condition: "Matching the original position also rejects reordered or duplicate IDs." Without `position`, a snapshot with two steps swapped would still match one row each by ID and commit, so the caller's order could silently drift from the saved one, and a duplicated ID would update the same row twice, leave the other row untouched, and commit. With it, a swapped or duplicated step matches zero rows and the check at L116 fires. The five `SET` columns are the step's execution state; `name` and `position` are never written.

**L111–L113** — Wrap a statement failure with the run and step IDs.

**L114–L118** — Insist again on exactly one matched row, now with the position in the message. This is where `unknown step`, `duplicate step`, and `reordered steps` fail (`run_updates_test.go:59-61`). Because the transaction is still open, the error also undoes the run-row update at L75 and every earlier step update; the test comment at `run_updates_test.go:84` spells out that expectation ("A bad third step must undo updates to the run and earlier steps too") and `assertStoredRun` at `run_updates_test.go:88` verifies the original is intact.

**L119–L119** — Closing brace of the loop.

**L120–L120** — Blank line.

**L121–L124** — Commit. This is the save the runner was waiting for before executing the next step ("Save step start before execution and step result before proceeding", `AGENTS.md:86`). `TestRunnerPersistsExecutionToSQLite` proves the order from inside a fake executor: it calls `GetRun` while the step runs and expects the step to be `running` and the previous step `succeeded` already (`runner_integration_test.go:25-33`).

**L125–L125** — Closing brace.

**L126–L126** — Blank line.

**L127–L132** — `unixMilliOrNull`. One helper for both time styles on `Run` and `StepRun`. A `nil` pointer (unset `*time.Time`) and a zero time (unset value `time.Time`, reached through the `&` at L33 and L77) both become `nil`, which the driver writes as `NULL`. Anything else becomes an `int64` of milliseconds. The return type is `any` because the two outcomes have different types. The schema comment states the convention this helper implements: "Timestamps are Unix milliseconds; NULL means a run or step has not started/finished" (`schema.go:15`). Without the `IsZero` check, a queued run would be saved with a start time of 1 January 1970.

### internal/store/run_reads.go

**L1–L2** — Package clause and blank line.

**L3–L11** — Imports. The same standard packages as `runs.go`, but only `engine` from the project: reads never decode the workflow snapshot, so `workflow` is not needed. The roadmap plans to return the snapshot later "as stored bytes without strict decoding, so a removed node type cannot make history unreadable" (`roadmap.md:105`); today nothing reads `definition_json` except tests (`runs_test.go:56`).

**L12–L12** — Blank line.

**L13–L15** — The contract for `GetRun`. "One consistent database snapshot" is the transaction. "Wrapping `sql.ErrNoRows`" is what lets the API answer 404 (`api.go:108-111`). "A zero Run rather than partially decoded history" means a caller never has to wonder whether a returned run is complete: on error it is `engine.Run{}`, nothing else.

**L16–L16** — The signature returns an `engine.Run` value, not a pointer. The caller gets its own copy, which it can hand to `json.NewEncoder` or compare with `reflect.DeepEqual` without any sharing concerns. Callers: `api.go:107` and many tests, including fake executors that read the database mid-run (`runner_integration_test.go:25`, `run_queue_test.go:30`).

**L17–L20** — Begin a transaction for a read. With one connection, this also means `GetRun` waits for any in-flight save to finish, bounded by the handler's five-second context (`api.go:104`).

**L21–L22** — The comment and `defer tx.Rollback()`. "There are no writes to commit; Rollback ends the read transaction." Ending it matters: an open transaction would keep the single connection busy, and every later store call would wait on it until the context expired. `TestGetRunErrorsReturnNoPartialHistory` checks that after a failed read the next statement still works (`internal/store/run_reads_test.go:101-105`).

**L23–L23** — Delegate to `readRunInTx` and return whatever it returns. The deferred rollback runs after the return values are evaluated.

**L24–L24** — Closing brace.

**L25–L25** — Blank line.

**L26–L28** — The contract for `ListRuns`. "Complete runs" means each entry carries its steps, not just the run row. The order is "newest first by creation time, then ID descending". The limit is bounded. The two empty cases differ on purpose: an empty history is an empty slice, a failure is `nil`, so a caller can tell them apart.

**L29–L29** — The signature. The caller: `api.go:92`, always with `100`, matching the README's "Latest 100 runs across workflows, newest first" (`README.md:435`) and the runner's own memory bound (`runner.go:367`).

**L30–L32** — Reject a limit outside 1 to 100. The upper bound keeps the work per request bounded: each listed run costs two more queries (L63), so 100 runs means at most 201 statements on the single connection inside one transaction. The lower bound makes a zero or negative limit a programming error instead of an empty result that hides a bug. `TestListRunsEmptyHistoryAndInvalidLimits` tries `-1`, `0`, and `101` (`internal/store/run_list_test.go:69-74`).

**L33–L37** — Begin a read transaction and defer its rollback, as in `GetRun`. All the queries below see one committed state.

**L38–L38** — Blank line.

**L39–L43** — Select only the IDs, newest first. `created_at DESC` orders by creation time, not start time: a queued run that waited a long time still sorts by when it was accepted. `id DESC` breaks ties so two runs created in the same millisecond always list in the same order. `TestListRunsOrdersAndLimitsCompleteHistory` inserts `run-b` and then `run-a` with the same `created_at` and expects `run-b` first (`run_list_test.go:28-31`, `run_list_test.go:44`); the comment at `run_list_test.go:41` says "Creation order must win over ID, insertion order, and start time." `LIMIT ?` takes the bound limit as a parameter.

**L44–L44** — `defer rows.Close()`. Guarantees the cursor is released on every error return between here and L59.

**L45–L45** — Pre-size the ID slice with capacity `limit`. The slice can hold every possible result without growing.

**L46–L52** — Scan each row's single column into a fresh `id` and append it. `var id string` inside the loop is a new variable each iteration, which keeps the loop simple to read; a `Scan` error is wrapped as `read listed run ID`.

**L53–L55** — Check `rows.Err()`. The loop at L46 ends when `Next` returns `false`, which happens both at the end of data and on a failure. This call tells the two apart. Skipping it is a common Go bug: a cursor error would look like a shorter history.

**L56–L59** — Close the ID cursor explicitly before the next queries, with the reason in the comment: "Finish this result set before reading runs on the same connection." The transaction has one connection, and the per-run reads at L63 run their own queries on it. Closing first guarantees nothing of the ID cursor is still pending while those queries run, and it reports a close error instead of discarding it, which the deferred `Close` at L44 would do. The deferred call still runs later and is a no-op on an already closed `Rows`.

**L60–L60** — Blank line.

**L61–L61** — `runs := make([]engine.Run, 0, len(ids))`. Non-`nil` even when `ids` is empty, so an empty history encodes as `[]`, which the README promises ("An empty list is `[]`", `README.md:562`) and `TestListRunsEmptyHistoryAndInvalidLimits` asserts with `got == nil` (`run_list_test.go:65-68`). The frontend reads it through `runsQuery.data ?? []` (`web/src/pages/WorkflowsPage.tsx:71`) before filtering by workflow (`WorkflowsPage.tsx:81`), so a `null` would silently show an empty history; the API test `TestEmptyAndMissingHistory` expects the literal body `[]` (`internal/httpapi/history_test.go:195`).

**L62–L68** — Read each run in full with the shared reader and the same transaction. On any failure, return `nil, err`: not the runs read so far. `TestListRunsErrorsReturnNoPartialHistory` corrupts one run's step JSON and checks two things: with `limit` 1 the corrupt run is outside the window and the list succeeds (`run_list_test.go:98-101`), and with `limit` 2 the result is `nil` plus a `*json.SyntaxError` (`run_list_test.go:103-107`). Returning a partial list would make the browser show a history that silently stops short.

**L69–L69** — Return the complete list. The deferred rollback ends the read transaction.

**L70–L70** — Closing brace.

**L71–L71** — Blank line.

**L72–L73** — `readRunInTx` is unexported and takes a `*sql.Tx` rather than a `*sql.DB`. That signature is the design: both public readers own the transaction and this function only uses it, so one run and a list of runs decode through the same code and the same snapshot.

**L74–L76** — Declare the scan targets. `run` is the value being filled. `created` is a plain `int64` because `created_at` is `NOT NULL` (`schema.go:23`). `started` and `finished` are `sql.NullInt64` because those columns may be `NULL` (`schema.go:24-25`); scanning a `NULL` into a plain `int64` would be an error.

**L77–L79** — Query the run row and scan it in one chained statement. The eight column names in the `SELECT` and the eight `Scan` targets must line up by position: `id` into `&run.ID`, and so on, with the three timestamps going to the local variables for conversion. `error` scans straight into `run.Error` because that column is `NOT NULL DEFAULT ''` (`schema.go:47`). The trailing `.` at the end of L78 continues the statement onto L79.

**L80–L82** — Wrap any failure with the run ID and return a zero `Run`. When no row matches, `Scan` returns `sql.ErrNoRows`, and `%w` keeps it reachable: `api.go:108` tests `errors.Is(err, sql.ErrNoRows)` and answers 404, and `TestGetRunErrorsReturnNoPartialHistory` asserts the same with a zero run (`run_reads_test.go:87-90`).

**L83–L83** — Convert the creation time. `.UTC()` matters: `time.UnixMilli` returns a time in the local zone, and the runner stored a UTC value (`runner.go:128`). With `.UTC()`, the read-back value equals the original under `reflect.DeepEqual` and encodes with a `Z` suffix for the browser.

**L84–L87** — Convert the start time only when it is not `NULL`. The comment says what happens otherwise: "A missing start uses zero time, omitted from JSON by the Run type." `Run.StartedAt` is a value with `omitzero` (`runner.go:41`), so a queued run's JSON simply lacks `startedAt`, the frontend type marks it optional (`web/src/api.ts:60`), and the timing label reads "Never started" (`web/src/format.ts:96`). The `not started` case of `TestGetRunReadsPersistedHistory` checks this round trip (`run_reads_test.go:41-43`, `run_reads_test.go:63-69`).

**L88–L88** — Convert the finish time through the pointer helper. `nil` for a run still in progress, a pointer otherwise.

**L89–L89** — Blank line.

**L90–L94** — Query the step rows in position order. `ORDER BY position` is what restores the sequence that `CreateRun` saved at `runs.go:38`; the `UNIQUE (run_id, position)` constraint (`schema.go:39`) guarantees there are no ties. The `name` column is read here and never from the current workflow file, so a renamed step in `workflows/` does not relabel old history ("Keep historical results independent of the currently loaded workflow", `AGENTS.md:126-127`).

**L95–L95** — `defer rows.Close()`. Releases the cursor on every return path below.

**L96–L96** — `run.Steps = make([]engine.StepRun, 0)`. Non-`nil`, so `steps` encodes as `[]` rather than `null` even for a run with no step rows. The frontend type declares `steps: StepRun[]` (`web/src/api.ts:62`).

**L97–L103** — Scan each step row. `var step engine.StepRun` is a fresh zero value each iteration, so a previous row's `Output` pointer cannot leak into the next step. `start` and `finish` are nullable integers; `output` is a `sql.NullString` because `output_json` is nullable (`schema.go:36`). `error` scans directly into `step.Error` thanks to its `NOT NULL DEFAULT ''` (`schema.go:37`).

**L104–L105** — Convert both step times with the pointer helper. Both `StepRun` time fields are pointers (`runner.go:28-29`), so `nil` means "not yet".

**L106–L110** — Decode the output only when the column was not `NULL`. `&step.Output` is a pointer to a nil pointer; `json.Unmarshal` allocates the `CheckResult` and sets `step.Output` to it. A `NULL` column leaves `step.Output` as `nil`, which is exactly what `CreateRun` wrote for a step without a result (`runs.go:39-41`). Corrupt JSON here fails the whole read: `TestGetRunErrorsReturnNoPartialHistory` writes `'{'` into the second step and expects a zero run plus a `*json.SyntaxError` (`run_reads_test.go:91-100`). The decoder does not care whether the result is HTTP or TCP; a missing `type` stays missing, and `format.ts` decides how to label it later.

**L111–L111** — Append the finished step.

**L112–L112** — Closing brace of the loop.

**L113–L115** — Check `rows.Err()` after the loop, for the same reason as L53-L55. A cursor failure after some steps were read must not look like a run with fewer steps.

**L116–L116** — Return the complete run.

**L117–L117** — Closing brace.

**L118–L118** — Blank line.

**L119–L125** — `timeFromUnixMilli`. The inverse of `unixMilliOrNull` for pointer fields: `NULL` becomes `nil`, otherwise a pointer to a UTC time. Returning `&instant` where `instant` is a local variable is fine in Go; the compiler places it on the heap because its address escapes. The value-typed `Run.StartedAt` cannot use this helper, which is why L84-L87 handle it inline.

## State, concurrency, and cancellation

**Who calls what, and on which goroutine.** `CreateRun` runs on whichever goroutine called `runner.Start`: an HTTP handler goroutine (`api.go:65`) or the scheduler's goroutine, which uses the same method (`main.go:116`). It runs while that caller holds the runner's mutex (`runner.go:114-115`, `runner.go:136-145`); the comment there says "Keep admission locked during the save; bound how long other calls wait." `UpdateRun` runs on a run's own goroutine (`runner.go:216` onward) for progress and final saves, and on the goroutine that called `runner.Close` for queued-run cleanup (`runner.go:200-214`). Those goroutines never hold the runner's mutex while saving; they take it only afterwards, in `publishRun` (`runner.go:359-363`) and at the end of `run` (`runner.go:287`). `GetRun` and `ListRuns` run on HTTP handler goroutines (`api.go:89-118`).

**What data these functions own.** Nothing between calls. Every parameter is a value: the runner passes copies made by `copyRun` (`runner.go:140`, `runner.go:338`, `runner.go:352`), and the readers build new `engine.Run` values from rows. There are no package-level variables in `store`. This is the store's side of the rule "Executors and update callbacks may run concurrently for different workflows. ... Return copied snapshots" (`AGENTS.md:80-82`): the callbacks can run concurrently because they share no Go state, and the only shared resource, the connection, is handed out by `database/sql`.

**Mutexes and lock order.** These files take no Go lock. The single pooled connection (`sqlite.go:59`) acts as one: `BeginTx` waits until the connection is free, then the transaction owns it until `Commit` or `Rollback`. The only nesting in the program is runner mutex, then connection, inside `Start`. The reverse never happens: nobody holds a transaction while asking for the runner's mutex. `preferUnsavedRun` calls `runner.Get` only after `ListRuns` or `GetRun` has returned and its transaction has ended (`api.go:92` then `api.go:99`; `api.go:107` then `api.go:117`). So a deadlock between the mutex and the connection is not possible. Progress and final saves never hold the mutex at all, which is the rule "Execution and progress/final writes stay outside the history mutex; initial creation currently holds it to serialize admission" (`AGENTS.md:81-82`).

**Which context each call uses.**

| Call | Context | Where it is built | What ends it |
| --- | --- | --- | --- |
| `CreateRun` | `saveCtx` | `runner.go:139`: `r.ctx` plus 5 s | `runner.Close` canceling `r.ctx` (`runner.go:179`), or the deadline |
| `UpdateRun` (step start, step result) | `saveCtx` | `runner.go:350`: `r.ctx` plus 5 s | shutdown via `r.ctx`, or the deadline |
| `UpdateRun` (final save, active run) | `saveCtx` | `runner.go:322`: `WithoutCancel(r.ctx)` plus 5 s (`runner.go:286`) | only the deadline; shutdown cannot cancel it |
| `UpdateRun` (final save, queued cleanup) | `saveCtx` | `runner.go:322` under the shared 5 s budget from `runner.go:203` | the shared deadline |
| `ListRuns`, `GetRun` | request context plus 5 s | `api.go:90`, `api.go:104` | the client disconnecting, or the deadline |

Accepted work never uses the HTTP request's context, which upholds "Accepted runs belong to the runner's context, not the originating HTTP request" (`AGENTS.md:84-85`). Inside `database/sql`, the context bounds the wait for the connection, each statement, and the life of the transaction: if it ends before `Commit`, the package rolls back and later calls on the `Tx` fail. The one gap is the commit itself. `database/sql` checks the context first, but the driver's `COMMIT` has no context (`writable.go:15-16`), so a cancellation that lands during the commit cannot stop it. A save the runner reports as canceled may therefore have reached the file. That is harmless for `UpdateRun`, because every call writes the complete snapshot and writing it again changes nothing. It is the reason the retry policy is safe: "Final-save retries repeat only the completed snapshot, never node actions" (`AGENTS.md:89`), and the test at `run_updates_test.go:40-47` saves the same state twice to prove it.

**Failure paths, one by one.**

- `CreateRun` fails (begin, encode, insert, or commit): the deferred rollback leaves the file unchanged. `Start` returns `ErrRecordRun` wrapped around the store error (`runner.go:142-144`) before touching `r.order`, `r.runs`, or `r.busyWorkflows`, and before `launchLocked`. The handler logs it and answers 500 with "Could not save run. Check server logs." (`api.go:75-79`). No step ran and no slot was taken: "Failed initial saves must not execute actions or consume capacity" (`AGENTS.md:83-84`).
- A progress `UpdateRun` fails: rollback; the runner marks the run `failed` with a reason naming the step (`runner.go:233-243`, `runner.go:269-280`), executes no more steps, keeps outputs it already has, and lets `finishRun` mark the rest `skipped` (`runner.go:303-307`). That is "A failed progress save stops later steps. Preserve completed outputs and skip steps that never ran" (`AGENTS.md:86-88`). If the failure was a cancellation, the status is `canceled` (unless the final step had already succeeded, `runner.go:261-273`) and no storage reason is added.
- A final `UpdateRun` fails three times or runs out of its five seconds: SQLite keeps the last snapshot that did commit, the runner sets `FinalSaveFailed` in memory only (`runner.go:310`), and the API overlays that memory copy on the saved rows while it is retained (`api.go:144-149`). "`finalSaveFailed` is a memory-only warning, not a new execution status" (`AGENTS.md:92`). After a restart, the saved `running` row becomes `interrupted` (`internal/store/run_interruptions.go:15`).
- `GetRun` finds no row: `sql.ErrNoRows` passes through the wrap and the handler answers 404 (`api.go:108-111`).
- `GetRun` or `ListRuns` fails any other way (cursor error, corrupt JSON, deadline): the caller gets a zero value or `nil` plus the error, the handler answers 500 (`api.go:93-97`, `api.go:112-116`), and the overlay never runs. "History overlays that result only while retained; database read errors still surface" (`AGENTS.md:92-93`). `TestHistoryShowsUnsavedFinalResultWithoutHidingReadFailures` (`internal/httpapi/history_test.go:346`) covers the API side.
- A read transaction fails: its deferred `Rollback` releases the connection, so the next save is not stuck behind it (`run_reads_test.go:101-105`, `run_list_test.go:108-112`).

**Rules this code implements.** "Save a new run and its workflow snapshot before acceptance" (`AGENTS.md:83`): `CreateRun` writes `definition_json` and the step rows in one commit, and the runner only admits after it returns. "Save step start before execution and step result before proceeding" (`AGENTS.md:86`): `UpdateRun` is the function both saves call, and the runner waits for its commit. "Preserve atomic run/step snapshots and existing data" (`AGENTS.md:103`): every function is one transaction, and `UpdateRun` never names the columns that hold the snapshot, the creation time, step names, or positions. "Keep historical results independent of the currently loaded workflow" (`AGENTS.md:126-127`): the readers take names and outputs from rows, never from `definitions`.

## Check yourself

1. `CreateRun` encodes the definition at L18 before `BeginTx` at L23, and `UpdateRun` has no definition parameter at all. What do those two facts protect, and which roadmap sentence explains why the snapshot exists?

<details><summary>Answer</summary>Encoding first keeps the transaction short and means an encoding failure returns before any database work, so there is nothing to roll back; the roadmap asks for short transactions and never holding one while doing other work (`docs/roadmap.md:93`). Leaving the definition out of `UpdateRun` makes it impossible for a progress save to rewrite `definition_json`; the `UPDATE` at `runs.go:75-77` cannot even name that column. The snapshot exists because "A historical run must remain understandable after a user edits its workflow" (`roadmap.md:105`): once the file in `workflows/` changes, the row is the only record of what ran.</details>

2. `CreateRun` inserts the run row and the first step row, then the second step insert fails because its ID duplicates the first. What is in the database afterwards, what does the browser receive, and did the run consume an active slot or queue space?

<details><summary>Answer</summary>Nothing from this call: the deferred `tx.Rollback()` at `runs.go:27` discards the run row and the first step row when the function returns at L54. `runner.Start` gets the error at `runner.go:140-144` and returns `ErrRecordRun` before `runner.go:146-148`, so the run is never added to `r.order`, `r.runs`, or `r.busyWorkflows`, and `launchLocked` never runs, so no slot or queue entry is taken. The handler maps `ErrRecordRun` to 500 with "Could not save run. Check server logs." (`api.go:75-79`). `TestCreateRunRollsBackWithoutReplacingHistory` proves the row counts are unchanged (`runs_test.go:118-130`).</details>

3. A caller passes `UpdateRun` a snapshot with the right number of steps but with the first two swapped. Which line rejects it, what happened to the run row before that line, and why would the count check at L87-L94 alone not be enough?

<details><summary>Answer</summary>The count check passes (three saved, three given). The first step's `UPDATE` at `runs.go:106-110` looks for `step_id = 'broken'` at `position = 0`, which does not exist, so `RowsAffected` is 0 and L116-L117 return an error wrapping `sql.ErrNoRows`. The run row was already updated at L75-L77 inside the same transaction, so the deferred rollback at L73 undoes that too; the test comment at `run_updates_test.go:84` states this and `assertStoredRun` at L88 verifies the original. Counting alone cannot see a swap or a duplicate because both keep the count at three; only matching `position` per row catches them, which is what the comment at `runs.go:105` says.</details>

4. `GetRun` reads a run whose first step decodes fine and whose second step has corrupt `output_json`. What does the API send, why does the function not return the run with the first step filled in, and what guarantees the next save is not blocked?

<details><summary>Answer</summary>`readRunInTx` returns `engine.Run{}` and a wrapped `*json.SyntaxError` at `run_reads.go:108`; `GetRun` passes both through. The handler's `errors.Is(err, sql.ErrNoRows)` at `api.go:108` is false, so it logs the error and answers 500 "could not load run" (`api.go:112-116`); the overlay at L117 is never reached, which is the rule "database read errors still surface" (`AGENTS.md:93`). A partial run would look complete to the browser and hide the corruption. The deferred `tx.Rollback()` at `run_reads.go:22` ends the transaction on that return, freeing the single connection; `TestGetRunErrorsReturnNoPartialHistory` repairs the row and reads again to prove it (`run_reads_test.go:101-109`).</details>

5. A browser poll calls `ListRuns` on an HTTP goroutine at the same moment a run goroutine calls `UpdateRun` to save a step result. Who waits, where, with which deadline, and what does the runner do if the wait exceeds that deadline?

<details><summary>Answer</summary>There is one pooled connection (`sqlite.go:59`). Whichever `BeginTx` gets it first runs; the other blocks inside `database/sql` waiting for the connection. The handler waits with the request context plus five seconds (`api.go:90`); the runner waits with `saveCtx`, the runner's context plus five seconds (`runner.go:350`). If the save's wait exceeds its deadline, `BeginTx` returns a context error wrapped as "begin run update", `saveRunUpdate` logs it, and the run loop marks the run `failed` with "Could not save the result of step ... Execution stopped." (`runner.go:269-280`), skips the remaining steps, and saves the terminal state with a fresh non-cancelable context. If the handler's wait exceeds its deadline, the browser gets 500 "could not load run history". In practice `ListRuns` finishes its at most 201 statements in milliseconds, so neither happens on a healthy disk.</details>

6. `saveFinalRun` may call `UpdateRun` up to three times with the same snapshot. Why is that safe for the database, and what would not be safe to repeat?

<details><summary>Answer</summary>`UpdateRun` writes the complete state, including the original finish time and every step's output, and the same values twice produce the same rows; `run_updates_test.go:40-47` saves the same reason twice and then clears it. Even a first attempt that was reported as canceled but actually committed (the driver's `COMMIT` cannot be interrupted, `writable.go:15-16`) leaves nothing for the retry to break. What must never be repeated is the node action: an HTTP or TCP check, and later an SSH command. The rule is "Final-save retries repeat only the completed snapshot, never node actions" (`AGENTS.md:89`), and `README.md:555` restates it: "External actions are never retried because a database write failed."</details>

## Experiments

1. **Break the tie-break.** In `internal/store/run_reads.go:40` change `id DESC` to `id ASC`. Run `go test ./internal/store -run TestListRunsOrdersAndLimitsCompleteHistory`. Expected: the test fails at `limit 1: history order or step results changed`, because `run-a` now sorts before `run-b` although both were created at the same millisecond and `run-b` was inserted first. Revert and rerun to confirm the pass.

2. **Delete the step count check.** Remove `internal/store/runs.go:87-94` (the `stepCount` query and comparison). Run `go test ./internal/store -run TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory -v`. Expected: exactly one subtest fails, `missing_step`, with `expected the mismatched snapshot to be rejected`, because two per-step updates each match one row and the commit succeeds. `extra_step` still passes: the extra ID matches no row at position 3, so L116-L117 reject it. Revert.

3. **Drop the position match.** In `internal/store/runs.go:108` delete `AND position = ?`, and in L110 delete the trailing `, position` argument. Run the same test as experiment 2. Expected: the `duplicate_step` and `reordered_steps` subtests fail with `expected the mismatched snapshot to be rejected`, because each swapped or duplicated step now matches one row by ID alone; the other four subtests still pass. Revert.

4. **Return a nil list.** In `internal/store/run_reads.go:61` replace `runs := make([]engine.Run, 0, len(ids))` with `var runs []engine.Run`. Run `go test ./internal/store -run TestListRunsEmptyHistoryAndInvalidLimits`. Expected: the test fails with `expected a non-nil empty list, got []engine.Run(nil), <nil>`. That `nil` would have encoded as `null` in `GET /api/runs`; the frontend's `runsQuery.data ?? []` at `web/src/pages/WorkflowsPage.tsx:71` would hide it as an empty history, but `go test ./internal/httpapi -run TestEmptyAndMissingHistory` fails because it expects the body `[]` (`internal/httpapi/history_test.go:195`). Revert.

## Next

Chapter 11, "The HTTP API and the request-host guard" ([11-httpapi.md](11-httpapi.md))
