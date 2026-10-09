# 6. The runner: admission, queueing, execution, and saves

Files: [internal/engine/runner.go](../../internal/engine/runner.go)

Revision: commit 1c4786e

## Role in the system

`internal/engine/runner.go` is the whole `engine` package. Its first line says what it owns: "Package engine runs steps in order and owns in-memory execution state" (L1). The `Runner` type decides whether a requested run may start now, must wait in a queue, or is rejected; it runs each admitted workflow's steps one after another on its own goroutine; it saves the run before any step starts, after each step starts, after each step finishes, and once more at the end; and it keeps the last 100 runs in memory so the API can show a result whose final save failed. It does not know what an HTTP or TCP check is, and it does not know SQLite exists. Both come in as functions: `ExecuteStep` (L51) runs one step, and `RecordNewRun` and `UpdateSavedRun` (L55, L60) save a run. `cmd/server/main.go:107-111` supplies the real ones; the engine tests supply fakes.

Data comes in as a `workflow.Definition` (`internal/workflow/workflow.go:20-27`) passed to `Start` (L105) by the HTTP handler (`internal/httpapi/api.go:65`) or by the scheduler (`internal/schedule/scheduler.go:125`). Data goes out three ways. `Start` returns a `Run` value, which the handler encodes as the `202 Accepted` response (`api.go:83-84`). Every save callback receives a copied `Run` snapshot plus, for the first save, the definition, and `store.CreateRun` and `store.UpdateRun` write them into SQLite (`internal/store/runs.go:17`, `runs.go:68`). `Get` and `List` (L159, L166) hand out copies of the in-memory history; the history endpoints use `Get` to overlay a result whose final save failed on top of what SQLite returns (`api.go:144-149`).

On the path from a browser click to a saved run, this file is the middle. The click becomes `POST /api/workflows/{id}/runs` (`api.go:50`), the handler calls `runner.Start` (`api.go:65`), `Start` saves and admits the run and returns while the run goroutine keeps working under the runner's own context, not the request's. The roadmap sets this requirement: "A submitted background run belongs to the application's lifecycle, so closing the browser or completing the HTTP request must not cancel it" (`docs/roadmap.md:122`). Later, `Close` (L176) is called from `main.go:140` on shutdown, after the scheduler stops and before SQLite closes (`main.go:139-140`, `main.go:80`).

## Concepts you need

- **Package doc comments.** The comment directly above `package engine` (L1) is the package's documentation; `go doc patchbay/internal/engine` prints it. By convention it starts with "Package engine". [Go Doc Comments](https://go.dev/doc/comment)
- **Sentinel errors, `%w`, and `errors.Is`.** `errors.New("...")` makes one fixed error value, called a sentinel. `fmt.Errorf("%w (...)", ErrCapacity, ...)` builds a new message that still "wraps" the sentinel, and `errors.Is(err, ErrCapacity)` finds it through any number of wrappers. Since Go 1.20 one `Errorf` may use several `%w` verbs, as L143 does, so a caller can match either wrapped error. [errors](https://pkg.go.dev/errors), [fmt.Errorf](https://pkg.go.dev/fmt#Errorf)
- **`errors.Join`.** Combines several errors into one value that `errors.Is` can match against each of them. L331 and L336 join a storage error with a context error so a caller can ask both "did storage fail?" and "did the deadline pass?". [errors.Join](https://pkg.go.dev/errors#Join)
- **Struct tags, `omitempty`, and `omitzero`.** The backtick string after a field, such as `` `json:"error,omitempty"` ``, is metadata that `encoding/json` reads with reflection. `omitempty` drops a field whose value is empty (zero number, empty string, nil pointer, empty slice). `omitzero` (Go 1.24) drops a field whose value is its zero value, and for a type with an `IsZero` method, such as `time.Time`, it calls that method. [encoding/json Marshal](https://pkg.go.dev/encoding/json#Marshal), [Spec: Struct types](https://go.dev/ref/spec#Struct_types)
- **Values versus pointers.** Assigning or passing a struct copies it; a pointer (`*time.Time`, `*workflow.CheckResult`, `*Runner`) refers to one shared value. This file uses pointers for two reasons: to say "not set yet" with `nil`, and to let `stepRun := &run.Steps[i]` (L230) edit one element of a slice in place. Copying a struct that holds a pointer copies the pointer, not the thing it points to, which is why `copyRun` exists (L380-390). [A Tour of Go: Pointers](https://go.dev/tour/moretypes/1)
- **Named function types as dependencies.** `type ExecuteStep func(context.Context, workflow.Step) (workflow.CheckResult, error)` (L51) names a function signature. Any function or closure with that shape can be stored in a field of that type. The engine takes three such functions instead of an interface or a plugin system, so tests can hand it a fake in one line (`internal/engine/runner_test.go:44`). [Spec: Function types](https://go.dev/ref/spec#Function_types)
- **Methods and pointer receivers.** `func (r *Runner) Start(...)` declares a method on `*Runner`. The receiver `r` is a pointer, so the method sees and changes the one shared runner rather than a copy. All methods in this file use pointer receivers because the runner holds a mutex and a wait group, which must never be copied. [A Tour of Go: Pointer receivers](https://go.dev/tour/methods/4)
- **`sync.Mutex`, `defer Unlock`, and the `Locked` suffix.** A mutex lets one goroutine at a time hold a lock; others block in `Lock()` until `Unlock()`. `defer r.mu.Unlock()` right after `Lock()` guarantees release on every return path. A helper named `...Locked` (L194, L366) is a convention meaning "the caller must already hold the lock"; the helper does not lock again, because Go mutexes are not re-entrant and locking twice from one goroutine deadlocks. [sync.Mutex](https://pkg.go.dev/sync#Mutex), [The Go Memory Model](https://go.dev/ref/mem)
- **Goroutines and `sync.WaitGroup`.** `go r.run(...)` (L197) starts `run` on a new goroutine and returns at once. A `WaitGroup` counts work in flight: `Add(1)` before starting, `Done()` when finished, `Wait()` blocks until the count is zero. The rule that matters here: an `Add` that could raise the count from zero must happen before the `Wait` it is meant to delay, which is why every `Add` in this file runs under the mutex. [A Tour of Go: Goroutines](https://go.dev/tour/concurrency/1), [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
- **`context.Context`: `WithCancel`, `WithTimeout`, `WithoutCancel`, `Err`, `Done`.** A context carries a cancellation signal down a call tree. `WithCancel(parent)` gives a child plus a `cancel` function; `WithTimeout(parent, d)` also cancels after `d`; a child is canceled whenever its parent is. `ctx.Err()` is `nil` until cancellation, then `context.Canceled` or `context.DeadlineExceeded`; `ctx.Done()` is a channel that closes at the same moment. `WithoutCancel(parent)` (Go 1.21) makes a child that keeps the parent's values but ignores its cancellation, which this file uses for cleanup that must still run after shutdown began. Always call the returned `cancel` to free the timer. [context](https://pkg.go.dev/context), [context.WithoutCancel](https://pkg.go.dev/context#WithoutCancel), [Go blog: Context](https://go.dev/blog/context)
- **`select` and `time.NewTimer`.** `select` waits on several channel operations and runs the first one that is ready. `time.NewTimer(d)` returns a timer whose channel `C` delivers once after `d`; `Stop()` releases it early. L328-333 waits for either the timer or the context's `Done` channel, whichever comes first. [A Tour of Go: Select](https://go.dev/tour/concurrency/5), [time.NewTimer](https://pkg.go.dev/time#NewTimer)
- **Slices share backing arrays; `slices.Clone`, `slices.Delete`, `slices.IndexFunc`.** A slice value is a small header (pointer, length, capacity) that points at an array. Copying the header does not copy the elements, so two slices can see each other's writes. `slices.Clone` makes an independent copy. `slices.Delete(s, i, j)` removes elements `i` up to `j` and returns the shorter slice. `slices.IndexFunc` returns the first index whose element satisfies a function, or `-1`. [slices](https://pkg.go.dev/slices), [Go blog: Slices](https://go.dev/blog/slices-intro)
- **Maps, the comma-ok form, and `map[string]struct{}` as a set.** `v, ok := m[k]` tells you whether a key exists; reading a missing key gives the value type's zero value. `struct{}` is a type with no fields and no size, so `map[string]struct{}` is the idiomatic set: only the keys matter. `make` creates the map; writing to a nil map panics. [A Tour of Go: Maps](https://go.dev/tour/moretypes/19), [Effective Go: Maps](https://go.dev/doc/effective_go#maps)
- **Zero values.** Every variable starts as its type's zero value: `0`, `""`, `false`, `nil` for pointers, slices, and maps, and a `time.Time` whose `IsZero()` is true. `Run{}` at L107 is "no run"; `Get` for a missing id returns that zero value plus `false`. [A Tour of Go: Zero values](https://go.dev/tour/basics/12)
- **Tuple assignment.** `run.Status, run.StartedAt = "running", run.CreatedAt` (L131) assigns two fields in one statement. The right side is evaluated fully before any assignment happens. [Spec: Assignment statements](https://go.dev/ref/spec#Assignment_statements)
- **Shadowing with `:=` inside `if` and loops.** A variable declared with `:=` inside an `if` header or a block lives only there, and it hides any outer variable of the same name. At L269 `if err := r.saveRunUpdate(...)` creates a new `err`; the `err` tested at L282 is the executor's error from L247. Reading this file correctly depends on noticing which `err` is which. [Spec: Declarations and scope](https://go.dev/ref/spec#Declarations_and_scope)
- **`for ... range` over a slice yields copies.** `for i, step := range definition.Steps` (L225) gives `step` as a copy of the element; changing `step` would not change the slice. `for i := range run.Steps` (L303) iterates indexes only, so `run.Steps[i]` edits the real element. [A Tour of Go: Range](https://go.dev/tour/moretypes/16)
- **`break` inside `for`.** `break` leaves the innermost loop at once. The run loop uses it on every stop condition so that "stop executing later steps" is written in one place, the loop, and finalization follows after it. [Spec: Break statements](https://go.dev/ref/spec#Break_statements)
- **`crypto/rand.Text`.** Added in Go 1.24, it returns a random string from the base32 alphabet with at least 128 bits of randomness, suitable for ids and tokens. It cannot fail; a random source failure crashes the process rather than returning a weak id. [crypto/rand.Text](https://pkg.go.dev/crypto/rand#Text)
- **Closures.** A function literal can use variables from the surrounding function. The save callbacks built at `cmd/server/main.go:107-111` capture the `db` handle, which is how the runner saves to SQLite without importing `store`. The `func(id string) bool` at L368 captures `r`. [A Tour of Go: Function closures](https://go.dev/tour/moretypes/25)
- **Structured logging with `log/slog`.** `slog.Error("msg", "key", value, ...)` writes a message plus key-value pairs. Keys such as `run_id` make server logs searchable, which the README relies on when it tells the administrator to "Check server logs" (L238, L276). [log/slog](https://pkg.go.dev/log/slog)
- **The race detector and `testing/synctest`.** `go test -race` instruments memory accesses and reports two goroutines touching the same data without synchronization. `testing/synctest` (Go 1.25) runs a test in a bubble with a fake clock, so retry delays of 100 ms and 200 ms pass instantly (`runner_updates_test.go:294-295`). You will use both in the experiments. [Race detector](https://go.dev/doc/articles/race_detector), [testing/synctest](https://pkg.go.dev/testing/synctest)

## Walkthrough

### internal/engine/runner.go

**L1–L2** — Package comment and clause. "Package engine runs steps in order and owns in-memory execution state." Two promises: steps run sequentially within a run (`AGENTS.md:67`), and the engine, not the API or the store, owns the live state of runs. `main.go:107` is the only production caller that constructs one.

**L3–L3** — Blank line.

**L4–L15** — Imports. `context` for cancellation and deadlines everywhere; `crypto/rand` for `rand.Text()` at L127; `errors` for the sentinels at L17-22 and `errors.Join` at L331; `fmt` for error wrapping and the two user-facing messages at L238 and L276; `log/slog` for the two error logs at L311 and L354; `slices` for `Clone`, `Delete`, and `IndexFunc`; `sync` for the mutex and wait group; `time` for timestamps, timeouts, and the retry timer. The one project import, `patchbay/internal/workflow`, supplies `Definition`, `Step`, `CheckResult`, and `Validate`. Nothing from `store` or `httpapi` is imported: the engine sits below both.

**L16–L16** — Blank line.

**L17–L22** — Four sentinel errors, one per way a start can be refused. `ErrWorkflowBusy`: the same workflow is already queued or running. `ErrCapacity`: all active slots and all queue slots are taken. `ErrClosed`: `Close` has begun. `ErrRecordRun`: the initial save failed. They are exported so callers can map each to a response without parsing text: `api.go:69-78` turns them into `409`, `429`, `503`, and `500`, and `scheduler.go:129-130` logs a busy skip at `Info` level while other failures log at `Warn` (`scheduler.go:131-132`). The messages are written for a person; the API sends them through unchanged except for `ErrRecordRun`, which is replaced at `api.go:78` so storage details stay in the server log.

**L23–L23** — Blank line.

**L24–L32** — `StepRun`, the per-step record inside a run. `Status` moves through `pending`, `running`, then `succeeded`, `failed`, `canceled`, or `skipped` (and `interrupted`, written only by startup cleanup at `internal/store/run_interruptions.go:15`). `StartedAt` and `FinishedAt` are `*time.Time` so an unset time is `nil` and `omitempty` leaves it out of JSON; a plain `time.Time` would encode as the year 0001. `Output` is a pointer for the same reason: `nil` means "no result", which `store.CreateRun` writes as SQL `NULL` (`runs.go:39-47`). `Error` holds the executor's error text when a step fails. The frontend reads these exact JSON names (`web/src/api.ts`).

**L33–L33** — Blank line.

**L34–L38** — `Run` begins: `ID` (random, L127), `WorkflowID` and `WorkflowName` copied from the definition so history still names a workflow after its file is edited or removed (`AGENTS.md:126-127`), and `Status`, which is `queued`, `running`, `succeeded`, `failed`, or `canceled` in this file.

**L39–L39** — `Error` with the comment "Run-level reason, separate from step errors." This holds the two save-failure messages from L238 and L276. A step's own failure is in `StepRun.Error`; the run-level field explains why the run stopped early for a reason that was not a step's fault. The README describes the display: "Run details display this reason above the step results" (`README.md:538-539`).

**L40–L40** — `CreatedAt` is a plain `time.Time` because every run has one: it is set at admission (L128) for both queued and running runs. `store.CreateRun` stores it at `runs.go:33`.

**L41–L41** — `StartedAt` is also a plain `time.Time`, but tagged `omitzero`: "Zero until started; omit it from queued JSON." A queued run has not started, so its JSON must not show a start time (`README.md:166-167`). The store turns a zero time into `NULL` at `runs.go:127-130`. A pointer would have worked too; the author chose a value here and a pointer for `FinishedAt`, which shows both idioms side by side.

**L42–L43** — `FinishedAt` as `*time.Time`, set once by `finishRun` (L302). `Steps` is a slice in definition order, one entry per step, created at L128 and filled at L133-135.

**L44–L46** — `FinalSaveFailed` with its comment: "Memory-only warning after final-save retries fail; independent of execution status." It is set at L310 only after every save attempt, so no saved snapshot ever carries it (`runner_updates_test.go:132-137` checks this). This upholds `AGENTS.md:91-94`: "`finalSaveFailed` is a memory-only warning, not a new execution status." `api.go:145` reads it to decide whether the in-memory copy should replace the SQLite row.

**L47–L47** — Blank line.

**L48–L51** — `ExecuteStep`. The comment states the design choice: a function dependency "letting engine tests use a controlled executor without a network server or a plugin framework", matching the roadmap's "Start with normal function calls and small concrete types" (`docs/roadmap.md:78`). "Implementations must support concurrent calls from different runs" is a contract the engine relies on at L247, because two run goroutines may call it at the same moment. Production supplies `checkExecutor` (`main.go:151-162`), which dispatches on `step.Type`.

**L52–L52** — Blank line.

**L53–L55** — `RecordNewRun`. The first save, which receives the definition as well as the run so the store can write the workflow snapshot in the same transaction (`runs.go:18-20`, `runs.go:29-33`, `runs.go:58`). "Save the snapshot atomically" is the engine's requirement; `store.CreateRun` meets it with one transaction and `defer tx.Rollback()` (`runs.go:23-27`).

**L56–L56** — Blank line.

**L57–L60** — `UpdateSavedRun`. Every later save: step start, step result, and final state. "Updates for different runs can happen concurrently" because run goroutines call it without any shared lock (L352, L338). `store.UpdateRun` (`runs.go:68`) rewrites the run row and every step row in one transaction and refuses a snapshot whose step count differs from what was saved (`runs.go:87-94`).

**L61–L61** — Blank line.

**L62–L63** — `Runner` begins with `mu sync.Mutex`. This one mutex protects every field from L64 to L71. The README names it: "Capacity and overlap checks happen together under the runner's mutex" (`README.md:155`).

**L64–L65** — `runs map[string]Run` holds the latest published snapshot of each retained run, by id. `order []string` holds the same ids in admission order, oldest first, so `List` can return newest first (L170) and `trimHistoryLocked` can find the oldest evictable entry (L368). Two structures for one history: the map gives O(1) lookup by id, the slice gives order.

**L66–L66** — `busyWorkflows map[string]struct{}`: the set of workflow ids that are queued or running. A key is added at L148 and removed at L210 or L290. It implements "a workflow already queued or running cannot be submitted again" (`README.md:154`).

**L67–L67** — `activeRuns int`: how many run goroutines currently hold a slot. Incremented at L195, decremented at L291. Compared with `maxActiveRuns` at L122.

**L68–L70** — `pending []queuedRun` is the FIFO queue of admitted runs that have no slot yet; `maxQueuedRuns` bounds its length (L123); `maxActiveRuns` bounds `activeRuns`. Both limits come from the `-max-active-runs` and `-max-queued-runs` flags (`README.md:137-140`).

**L71–L71** — `closed bool`: set once by `Close` (L178). After it, `Start` refuses (L116) and a finishing run no longer promotes queued work (L292).

**L72–L74** — `ctx` and `cancel` are the runner's own lifetime context, made at L97 and canceled only at L179. `wg` counts run goroutines and the queued-run cleanup so `Close` can wait for all of them (L190).

**L75–L75** — Blank line inside the struct, separating mutable state from the injected functions.

**L76–L79** — The three dependencies, set at construction and never changed. Each may be `nil` only as the comment at L89 allows: "Nil persistence callbacks keep the runner in memory for executor tests." `execute` is never nil-checked; a runner without an executor is not a valid configuration.

**L80–L80** — Blank line.

**L81–L85** — `queuedRun` pairs a run id with the definition captured at admission. The comment says why the definition is stored and not re-read: "retains the definition captured at admission; waiting work owns no goroutine." A queued run must execute exactly what was validated and saved, not whatever the workflow file says later. The second half states the rule from `AGENTS.md:79`: "Queued work owns no goroutine until promoted." A goroutine blocked on a channel for each waiting run would be simpler to write but would mean the queue limit also bounded goroutines, and shutdown would have to wake each one.

**L86–L86** — Blank line.

**L87–L89** — Doc comment for `New`, stating the two numeric contracts and the nil-callback allowance. "A queue limit of zero rejects starts whenever all active slots are occupied" is tested by `TestSimultaneousStartsRespectCapacityAndWorkflowOverlap` with `queued: 0` (`runner_concurrency_test.go:101`).

**L90–L90** — Signature. Five parameters, returns `(*Runner, error)`. Returning an error instead of panicking lets `main.go:112-114` report a bad flag value as a startup error.

**L91–L96** — Validate the limits. `maxActiveRuns < 1` would mean nothing could ever run; `maxQueuedRuns < 0` is meaningless. These are checked again earlier at startup before storage is touched (`main.go:48-53`), so this is defense in depth for tests and any future caller. `TestNewRejectsInvalidActiveRunLimits` and `TestNewRejectsNegativeQueueLimit` (`runner_concurrency_test.go:15`, `:304`) cover both.

**L97–L97** — `ctx, cancel := context.WithCancel(context.Background())`. The root of every context used by execution and saves. It derives from `Background`, not from any request, so only `Close` can cancel it. This is the mechanism behind `AGENTS.md:84-85`: "Accepted runs belong to the runner's context, not the originating HTTP request."

**L98–L102** — Build and return the runner. `runs` and `busyWorkflows` are created with `make` because writing to a nil map panics. `order` and `pending` are left `nil`; `append` on a nil slice works, and `len(nil)` is 0, so they need no initialization. `activeRuns` and `closed` start at their zero values, which are the correct initial states.

**L103–L103** — Closing brace of `New`.

**L104–L104** — Blank line.

**L105–L105** — `Start` signature. Takes a definition by value, returns the admitted run snapshot or an error. Called by the HTTP handler (`api.go:65`), passed as a method value to the scheduler (`main.go:116`, typed at `scheduler.go:19`), and called by tests.

**L106–L108** — `workflow.Validate(definition)` runs first, before any lock. Startup already validated loaded files (`main.go:58-61`), but the engine validates again because it must never execute an invalid definition from any caller, and future callers (a canvas save, an import) will not have gone through startup. The error is returned unwrapped, so the API's default `400` applies (`api.go:67`). `TestInvalidDefinitionNeverRunsAndHistoryIsBounded` sends a definition with no steps and expects rejection (`runner_test.go:144-148`); `Validate` requires 1 to 20 steps (`workflow.go:75-76`).

**L109–L109** — `definition.Steps = slices.Clone(definition.Steps)`. The definition arrived by value, but its `Steps` field is a slice header that still points at the caller's array. The scheduler reuses one definition for every occurrence, and a test could edit its slice after calling `Start`. Cloning gives the queued entry, the save at L140, and the run goroutine their own array. Without it, a later edit by the caller could change which steps a queued run executes.

**L110–L113** — The same independence for `Schedule`, a pointer (`workflow.go:25`). `schedule := *definition.Schedule` copies the struct; `definition.Schedule = &schedule` points at the copy. Now the definition that `recordNewRun` serializes as the workflow snapshot (`runs.go:18`) shares nothing with the caller. If a later node type adds another pointer or slice to `Definition`, this is the place to copy it; `AGENTS.md:130` warns to "revisit snapshot copying if adding nested pointers."

**L114–L115** — Take the mutex and defer its release. Everything from here to L156 runs under the lock, including the database save. That is deliberate and documented at L137 and in `README.md:160-161`: "Execution and progress saves run outside that mutex; initial run creation stays serialized." The cost is that `Get`, `List`, `publishRun`, and other `Start` calls wait while one initial save runs, for up to the five-second timeout at L139.

**L116–L118** — Refuse after `Close` began. Without this, a start arriving during shutdown could launch a goroutine after `Close` had already passed its `wg.Wait`, and that goroutine would outlive the SQLite handle. The scheduler also guards against this on its side (`scheduler.go:121-124`), but the runner cannot rely on callers.

**L119–L121** — Same-workflow overlap check. If `definition.ID` is in `busyWorkflows`, return `ErrWorkflowBusy`. It runs before the capacity check, so a duplicate of a queued workflow is reported as busy, not as a capacity problem, even when the queue is full. The roadmap asks for exactly this split: "Keep overlap policy separate from global capacity" (`docs/roadmap.md:113`). The scheduler treats this error as a normal skip (`scheduler.go:129-130`).

**L122–L122** — `queued := r.activeRuns >= r.maxActiveRuns`. Decide now, under the lock, whether this run gets a slot or waits. Because the lock is held from L114 to L156 and `launchLocked` increments `activeRuns` at L195 before the lock is released, two simultaneous starts cannot both see a free slot.

**L123–L125** — If the run would have to wait and the queue is already full, reject with `ErrCapacity` wrapped by `%w` together with both limits. The limits are in the message because an administrator seeing `429` needs to know which flag to raise (`README.md:447-448`). `errors.Is` still matches through the wrapper (`api.go:71`). The roadmap's requirement: "expose capacity rejection rather than silently dropping work" (`docs/roadmap.md:112`).

**L126–L129** — Build the `Run` value. `rand.Text()` gives a 26-character id that is also the SQLite primary key; a collision is "vanishingly small" by the function's contract. `Status` starts as `queued` and `CreatedAt` as the current UTC time; UTC avoids a local-time zone leaking into stored timestamps. `Steps` is allocated at the final length so L134 can assign by index.

**L130–L132** — If a slot is free, the run is `running` from the start and `StartedAt` equals `CreatedAt`. A tuple assignment sets both. For a queued run, `StartedAt` stays zero and is omitted from JSON (L41) and stored as `NULL` (`runs.go:33`, `runs.go:127-130`).

**L133–L135** — One `pending` `StepRun` per definition step, carrying the step's id and name so the frontend can list steps before any has run. `step` is a copy of each element; only `ID` and `Name` are read, so that is fine.

**L136–L139** — If a save callback was supplied, build a five-second context from `r.ctx`. The two comments say everything: "Keep admission locked during the save; bound how long other calls wait" and "This context belongs to the runner, not the originating HTTP request." A request that times out or a browser that disconnects does not cancel this save. `README.md:517-518` states the same guarantee.

**L140–L141** — Call `recordNewRun` with `copyRun(run)` and the definition, then `cancel()` immediately rather than deferring it. Deferring would keep the timer alive until `Start` returns; calling it here releases it as soon as the save ends. The copy means the store's own goroutine-free code cannot keep a reference to the runner's slice. In production this is `store.CreateRun` (`main.go:107-109`), which inserts the run, the definition JSON, and every step row in one transaction (`runs.go:29-58`).

**L142–L144** — On failure, return `ErrRecordRun` wrapped around the storage error with two `%w` verbs. Nothing has been added to `runs`, `order`, `busyWorkflows`, or `pending` yet, and `activeRuns` is unchanged, so the failed run consumes nothing and the same workflow can be retried at once. This is the rule at `AGENTS.md:83-84`: "Failed initial saves must not execute actions or consume capacity." `TestStartRejectsFailedSaveAndAllowsRetry` (`runner_persistence_test.go:47`) proves it, and the API maps the error to `500` with a generic message (`api.go:75-78`).

**L145–L145** — Closing brace of the `if r.recordNewRun != nil` block.

**L146–L148** — The run now exists in memory: append its id to `order`, store the snapshot in `runs`, and mark the workflow busy. All three happen only after the save succeeded, so memory never shows a run that SQLite does not have.

**L149–L149** — `trimHistoryLocked()` right after adding, so memory never exceeds 100 retained runs. The new run is `queued` or `running`, which the trimmer never evicts (L365).

**L150–L150** — Build the queue entry from the id and the now-independent definition.

**L151–L155** — Either append to `pending`, which starts no goroutine, or call `launchLocked` while still holding the mutex, which reserves the slot and starts the goroutine. The queue is FIFO because `append` adds at the end and promotion takes index 0 (L293). `TestQueueIsFIFOAndWaitsForFinalSave` (`runner_queue_test.go:15`) checks the order.

**L156–L157** — Return a copy and let the deferred `Unlock` run. The caller gets `running` or `queued`, which the handler sends as `202` with a `Location` header (`api.go:83-84`, `README.md:164`).

**L158–L158** — Blank line.

**L159–L164** — `Get`. Lock, look up by id with the comma-ok form, return `copyRun(run), ok`. For a missing id `run` is the zero `Run`, and `copyRun` of it is harmless (`slices.Clone(nil)` is `nil`). The copy is required by `AGENTS.md:82`: "Return copied snapshots." Callers: the run goroutine itself to obtain its working copy (L219), queued cleanup (L206), the API overlay (`api.go:145`), and the tests' `awaitRun` helper (`runner_test.go:27`).

**L165–L165** — Blank line.

**L166–L174** — `List`. Lock, allocate a slice with the right capacity, walk `order` backwards so the newest run comes first, copying each. Production does not call this: the history endpoints read SQLite (`api.go:92`, `api.go:107`) and overlay with `Get`. It exists for tests, including the history-bound assertions (`runner_test.go:163`, `runner_concurrency_test.go:215`) and `cmd/server/checks_test.go:158`.

**L175–L175** — Blank line.

**L176–L179** — `Close` begins. Under the lock: set `closed` so no new start is admitted and no promotion happens, then `r.cancel()`. Cancellation reaches every executor through `r.ctx` (L247) and every in-flight progress save through its derived context (L350). Setting `closed` and canceling under the same lock means a run goroutine that checks `!r.closed` at L292 sees a consistent picture. `main.go:140` calls this on a signal; `main.go:115` defers it as well, so `Close` must be safe to call twice.

**L180–L181** — Take ownership of the queue: copy the slice header into a local and set `r.pending = nil`. After this, no finishing run can promote a queued entry (there is nothing to promote), so each waiting run will be canceled exactly once, by this `Close`.

**L182–L185** — If there is queued work, `r.wg.Add(1)` for the cleanup that follows. The comment explains the subtle point: "Other concurrent Close calls must also wait for queued-run cleanup." A second `Close` (the deferred one at `main.go:115`) finds `pending` nil, skips the cleanup, and goes straight to `Wait`; because this `Add` happened under the lock before either `Close` reached `Wait`, the second caller still waits for the first one's cleanup to finish. `TestCloseWaitsForQueuedCancellationSave` (`runner_queue_test.go:196`) covers this.

**L186–L186** — Release the mutex before doing any saves. `cancelQueued` and every run goroutine need the lock to finish (L208, L287), and `wg.Wait` must not hold it, or shutdown would deadlock.

**L187–L189** — Run the queued cleanup on this goroutine, synchronously. It is not started with `go` because the caller of `Close` is already waiting; a goroutine would add nothing.

**L190–L191** — `r.wg.Wait()` blocks until every run goroutine has returned (each calls `Done` at L217) and the cleanup has returned (L201). Only then does `main.go` go on to close SQLite (`main.go:80`), which is the rule at `AGENTS.md:96`: "Stop the runner before closing SQLite and releasing the lock."

**L192–L192** — Blank line.

**L193–L198** — `launchLocked`. "Caller holds mu. Reserve the slot before another submission can take it." Increment `activeRuns`, `wg.Add(1)`, start the goroutine. The order matters: the slot and the wait-group count are taken under the mutex, so by the time anyone else can run `Start` or `Close`, the goroutine is already accounted for. Called from `Start` (L154) and from a finishing run's promotion (L295).

**L199–L199** — Blank line.

**L200–L201** — `cancelQueued` takes the queue that `Close` detached. `defer r.wg.Done()` pairs with the `Add` at L184.

**L202–L204** — One five-second budget for the entire queue. The comment states the alternative that was rejected: "rather than adding five seconds per entry." A full queue of ten could otherwise hold shutdown for fifty seconds, longer than the twenty seconds Compose allows (`README.md:526`). `context.WithoutCancel(r.ctx)` is needed because `r.ctx` was canceled at L179; a child of it would already be dead and no save could run. The fresh context keeps the runner's values but not its cancellation, then `WithTimeout` adds the bound. `TestCloseBoundsActiveAndQueuedFinalSaves` (`runner_updates_test.go:351`) checks that `Close` takes exactly five seconds when every save hangs.

**L205–L207** — For each entry, fetch its current snapshot (status `queued`, no `StartedAt`) and call `finishRun` with `canceled`. `finishRun` marks every `pending` step `skipped` and saves with the retry policy (L303-312), using the shared context so the queue's deadline wins over the per-save five seconds (L320-321). The result matches `README.md:176-177`: "A canceled waiting run has skipped steps and no start time."

**L208–L212** — Under the lock: publish the final snapshot, release the workflow's busy mark, and trim. `activeRuns` is not touched because a queued run never held a slot (L151-152). The lock is taken and released per entry so other finishing runs are not blocked for the whole cleanup.

**L213–L214** — Closing braces of the loop and the function.

**L215–L215** — Blank line.

**L216–L217** — `run`, the goroutine body for one admitted run. `defer r.wg.Done()` pairs with the `Add` at L196 and runs even if a step's code panics, so `Close` can still finish waiting.

**L218–L219** — Fetch a working copy: "This goroutine owns its working copy; readers only see published snapshots." From here on this goroutine edits `run` freely and no other goroutine can see those edits until `publishRun` copies them into the map (L244, L281, L289). The `_` discards the boolean; the run was stored at L147 before the goroutine was started, so it exists.

**L220–L223** — A promoted run arrives with `Status == "queued"`. If the runner is not already canceled, mark it `running` with a fresh `StartedAt`. The comment says when this becomes durable: "The first step-start save persists this transition before any action runs." That is the save at L233. The `r.ctx.Err() == nil` guard matters for a run promoted just before shutdown: it stays `queued`, the loop exits at once at L226, and the final record shows a canceled run that never started, which is the truth.

**L224–L224** — `status := "succeeded"`. The loop only ever lowers this; a run that leaves the loop without any `break` succeeded.

**L225–L225** — Loop over the definition's steps in order. `i` indexes `run.Steps`, which was built with the same length and order at L128-135.

**L226–L229** — Before each step, check the runner's context. If shutdown has begun, mark `canceled` and leave the loop without starting the step. This is the roadmap's rule "Canceling a run must stop admission of new steps" (`docs/roadmap.md:119`) and it is why `README.md:175-176` can promise that remaining steps are skipped.

**L230–L230** — `stepRun := &run.Steps[i]`. A pointer into the working copy's own slice (cloned by `copyRun` at L382), so writes through `stepRun` edit `run.Steps[i]` in place. Because this slice belongs only to this goroutine, no lock is needed.

**L231–L232** — Record the start time and mark the step `running`. The time is taken into a local variable so its address can be stored; each iteration gets a fresh `started`, so earlier steps' pointers are never overwritten.

**L233–L233** — Save the step start: `if err := r.saveRunUpdate(r.ctx, run); err != nil || r.ctx.Err() != nil`. Two ways in: the save failed, or the context was canceled (even if the save happened to succeed). The second condition prevents starting an executor after shutdown has been requested, which the save's success alone would not reveal. This is the first half of `AGENTS.md:86`: "Save step start before execution."

**L234–L239** — Decide the status. Default to `failed`; if the context is canceled, use `canceled` and add no message, because cancellation is not a storage problem ("Cancellation during shutdown does not add a storage-error reason", `README.md:540`). Otherwise set the run-level `Error` naming the step whose progress could not be saved and pointing to the logs. `TestSaveFailureStopsLaterStepsWithoutReplayingActions` expects this exact text (`runner_updates_test.go:94`, `:96`).

**L240–L242** — Undo the in-memory start: "The executor never started; finalization will mark this step skipped." Putting the step back to `pending` with a nil start time lets `finishRun`'s one rule at L304-306 handle it. Then `break`.

**L243–L243** — Closing brace of the step-start save block.

**L244–L244** — `r.publishRun(run)`. Now that the step start is saved, copy the working state into the map so `Get` shows the step as `running`. Publishing after the save means memory never claims more progress than the database has.

**L245–L245** — Blank line.

**L246–L247** — "Neither external work nor database writes hold the history mutex." Call the executor with `r.ctx` and the step. For `http.check` and `tcp.check` this ends in `httpcheck.Execute` or `tcpcheck.Execute` through `main.go:151-162`. The executor receives the runner's context, so `Close` cancels a check in progress. This is the rule at `AGENTS.md:80-81`: "Executors and update callbacks may run concurrently for different workflows. Execution and progress/final writes stay outside the history mutex."

**L248–L249** — Record the finish time whether the step succeeded or failed.

**L250–L255** — Executor error. The run becomes `failed`, or `canceled` if the context ended, and the step takes that same status plus the error text. The distinction between an unhealthy service and a failed step lives inside the executor: an HTTP 503, a refused connection, or a check that times out is a successful check with `Healthy: false` and a reason (`README.md:237`, `runner_shutdown_test.go:54-55`, `internal/nodes/httpcheck/http.go:56-63`, `internal/nodes/tcpcheck/tcp.go:44-51`), while an unsupported step type (`main.go:159`) or the runner's own context ending during the check (`http.go:50-55`, `tcp.go:41-43`) returns an error. `AGENTS.md:91` asks the engine to "Distinguish execution outcome, monitored-service health, and save failure."

**L256–L258** — Executor success. The step is `succeeded` and `Output` points at the local `output`. `output` is declared fresh by `:=` at L247 on every iteration, so each step's pointer is distinct.

**L259–L261** — `allStepsSucceeded := err == nil && i == len(definition.Steps)-1`. The comment explains the rule this flag enforces: "Earlier steps already succeeded to reach this point. Shutdown cannot undo a successful final step, even if its progress save is canceled." Once the last executor returned without error, the run's outcome is `succeeded` no matter what shutdown does next; `README.md:173-175` promises this. `TestShutdownKeepsCompletedExecutionOutcome` (`runner_shutdown_test.go:14-47`) lists the cases.

**L262–L268** — If cancellation arrived during or right after the executor, do not attempt the progress save with a dead context: "Finalization saves these results with a fresh cleanup context." Status becomes `canceled` unless the whole workflow just completed. For an executor error with a canceled context, `status` is already `canceled` from L252-253. Then `break`; the final save at L286 writes the step result that was just recorded.

**L269–L269** — Save the step result with `if err := r.saveRunUpdate(r.ctx, run); err != nil`. This `err` is new and shadows the executor's `err` from L247 inside this block only. This is the second half of `AGENTS.md:86`: "step result before proceeding."

**L270–L273** — The save failed because the context was canceled meanwhile: `canceled`, unless all steps succeeded.

**L274–L277** — The save failed for a storage reason while the runner is alive: `failed`, with the run-level `Error` naming the step whose result could not be saved. If the executor had also failed, the step keeps its own execution error in `StepRun.Error` and the run-level field explains the save problem; `TestProgressSaveFailurePreservesExecutionError` (`runner_updates_test.go:154`) checks both survive.

**L278–L280** — "Preserve completed outputs, but do not execute any more steps." Then `break` and the block's closing brace. The step's `Output` from L257 stays in the working copy, and the final save will retry writing it. This is `AGENTS.md:87-88`: "A failed progress save stops later steps. Preserve completed outputs and skip steps that never ran."

**L281–L281** — Publish the saved result so `Get` shows the finished step.

**L282–L284** — `if err != nil { break }`. This is the executor's `err` again (the inner one from L269 is out of scope). A failed step stops the run; `status` is already `failed` or `canceled` from L251-254. `TestExecutionFailureSkipsLaterSteps` (`runner_test.go:70`) checks that the later steps end `skipped`.

**L285–L285** — Closing brace of the step loop.

**L286–L286** — `run = r.finishRun(context.WithoutCancel(r.ctx), run, status)`. The final save gets a context that ignores the runner's cancellation, so a run that was interrupted by shutdown can still record what happened; `saveFinalRun` adds its own five seconds (L322). `README.md:524-525`: "Shutdown cancels execution, then allows up to five fresh seconds per active run's final save (including retries)."

**L287–L288** — Take the mutex for the last time and defer its release to the function's end, so L289-297 run as one atomic update.

**L289–L289** — Publish the final snapshot, including `FinalSaveFailed` if it was set at L310. From now on `Get` returns this, and the API's overlay at `api.go:145` can see the flag.

**L290–L290** — Remove the workflow from `busyWorkflows`. Another run of the same workflow may now be admitted.

**L291–L291** — `r.activeRuns--`. The slot is released only now, after the final save returned, so the configured number of active runs also bounds the number of final saves in flight. `README.md:158-159`: "A slot is released after the final save succeeds or its retry limit/deadline is reached." `TestRunKeepsItsSlotUntilFinalSaveReturns` (`runner_concurrency_test.go:220`) checks this.

**L292–L296** — Promotion. If the runner is open and the queue is not empty, take the head (`r.pending[0]`), remove it with `slices.Delete`, and `launchLocked` it while still holding the mutex. Because `Start` needs the same mutex to see a free slot, "the oldest entry reserv[es] each freed slot before a new submission can take it" (`README.md:156-157`). This is the queue-promotion half of `AGENTS.md:78-79`. The `!r.closed` check means a run finishing during shutdown leaves the queue to `Close`, which has already detached it (L180-181).

**L297–L298** — Trim and the closing brace. The run just finished may now be evictable, and the newly launched one is `queued` in memory until its goroutine publishes `running`, so it is protected.

**L299–L299** — Blank line.

**L300–L302** — `finishRun` sets the terminal status and a `FinishedAt` on the working copy it received by value. Called from `run` (L286) and from `cancelQueued` (L207). It never touches the mutex, so it is safe to call from either goroutine.

**L303–L307** — Every step still `pending` becomes `skipped`. That covers steps after a failure, after a cancellation, after a reset at L241, and all steps of a canceled queued run. `skipped` is distinct from `interrupted`, which only startup cleanup writes (`AGENTS.md:98`); the roadmap wants a step that "execution never reached" to be `skipped` (`docs/roadmap.md:124`).

**L308–L312** — "Retry only this completed snapshot, never the actions that produced it." Call `saveFinalRun`; if it fails after all attempts, set the memory-only `FinalSaveFailed` and log with the run id and status. Nothing here re-runs an executor, which is the rule at `AGENTS.md:89`. Because the flag is set after the last attempt, the snapshot passed to storage never carried it.

**L313–L314** — Return the finished run and the closing brace.

**L315–L315** — Blank line.

**L316–L319** — `saveFinalRun`. With no callback (in-memory test runner) there is nothing to do.

**L320–L323** — One five-second budget for all attempts and delays, derived from the caller's context. The comment states the precedence: "A shorter parent deadline, such as queued-run shutdown cleanup, still takes precedence." A `WithTimeout` child can only end sooner than its parent, never later. `TestFinalSaveRespectsParentDeadlineDuringBackoff` (`runner_updates_test.go:333`) gives a 50 ms parent and expects exactly one attempt.

**L324–L325** — `var err error` outside the loop so the last failure survives to the `return` at L343. At most three attempts (`README.md:528`).

**L326–L334** — Before the second and third attempts, wait `attempt * 100 ms` (100 ms, then 200 ms) with a timer, but give up early if the context ends during the wait. `timer.Stop()` releases the timer on the early path. The early return joins the previous storage error with the context's error so `errors.Is` finds both (`runner_updates_test.go:345`). The timings are checked by `TestFinalSaveRetriesUnchangedResult` (`runner_updates_test.go:327-328`).

**L335–L337** — Even before the first attempt, a context that has already expired (a queue cleanup that ran out of budget on an earlier entry) means no call is made. Without this check the store would be asked to begin a transaction with a dead context and the error would be less clear.

**L338–L338** — The attempt, with `copyRun(run)`. Every attempt sends the identical completed snapshot, including the original `FinishedAt` (`runner_updates_test.go:319-325`). `README.md:531-532`: "Retries write the same completed snapshot, including its original finish time and step outputs."

**L339–L341** — Return on success, or if the context ended during the attempt. In the second case the callback's own error is returned as it is; there is no point retrying with a dead context.

**L342–L344** — Loop end, the final `return err` after three storage failures, and the closing brace. "Three attempts are a limit, not a guarantee" (`README.md:531`): a slow attempt can consume the whole budget.

**L345–L345** — Blank line.

**L346–L349** — `saveRunUpdate`, the single-attempt progress save. Same nil guard for test runners.

**L350–L351** — Five seconds per progress write (`README.md:523-524`), derived from the runner's context so shutdown cancels a write in progress.

**L352–L352** — One call with a copy of the working state.

**L353–L356** — Log the failure only when the parent context is still alive. A save that failed because shutdown canceled it is expected and would otherwise produce a misleading storage error for every active run on every shutdown. The caller still sees the error and decides the status (L233-239, L269-277).

**L357–L357** — Closing brace.

**L358–L358** — Blank line.

**L359–L363** — `publishRun`. Lock, store a copy, unlock. This is the only way a run goroutine's progress becomes visible to `Get`. Storing a copy, not the working value, is what keeps the goroutine's later writes through `stepRun` (L230) from racing with readers.

**L364–L364** — Blank line.

**L365–L367** — `trimHistoryLocked`: "Caller holds mu. Never evict queued or running work from memory." A `for` loop runs while more than 100 ids are retained. The number 100 matches the API's SQLite page (`api.go:92`) and the README's "The frontend displays the latest 100 runs" (`README.md:10`). The memory history exists for the overlay of unsaved results, which must stay bounded: "they use the existing bounded memory history rather than an unbounded collection of failed saves" (`README.md:549-550`).

**L368–L371** — Find the oldest id (lowest index in `order`) whose run is neither `running` nor `queued`. The closure captures `r` to read `r.runs`.

**L372–L374** — If every retained run is unfinished, stop: "More than 100 unfinished runs: trim again when one finishes." With the default limits (2 active, 10 queued) this cannot happen, but the code must not evict live work for any configuration. `TestActiveRunSurvivesMemoryHistoryTrimming` and `TestQueuedRunsSurviveHistoryTrimming` (`runner_concurrency_test.go:192`, `:281`) check both kinds.

**L375–L376** — Evict: delete from the map and remove the id from `order`. Both structures stay in sync. After eviction a result whose final save failed is gone from memory, and the API falls back to SQLite's last snapshot; `README.md:550-551` and `AGENTS.md:94` both say not to promise more.

**L377–L378** — Closing braces of the loop and the function.

**L379–L379** — Blank line.

**L380–L382** — `copyRun`: "Return snapshots so JSON encoding never races with the execution goroutine." The `Run` arrived by value, which already copied every plain field. `slices.Clone` gives the copy its own `Steps` array.

**L383–L388** — For each step with an `Output`, copy the pointed-to `CheckResult` and point the copy's step at it. `CheckResult` holds only plain values (`workflow.go:49-59`), so one level of copying is enough. `StartedAt` and `FinishedAt` pointers are shared on purpose: each points at a `time.Time` that is written once (L231, L248, L301) and never changed afterwards, so sharing is safe. If a future result type gains a slice or pointer, this loop must grow (`AGENTS.md:130`).

**L389–L390** — Return the independent copy and the closing brace. Every reader of history (`Get`, `List`, the save callbacks, the `Start` response) gets its data through this function.

## State, concurrency, and cancellation

**Who owns what.** The caller of `Start` (an HTTP handler goroutine or a scheduler loop goroutine, `scheduler.go:112`) owns the definition it passes; `Start` cuts every shared reference at L109-113 before anything else sees it. The runner's mutex `mu` protects six fields: `runs`, `order`, `busyWorkflows`, `activeRuns`, `pending`, and `closed`. The remaining fields (`ctx`, `cancel`, `wg`, the limits, the three callbacks) are written in `New` and only read afterwards; `cancel` and `wg` are safe for concurrent use by design. Each run goroutine owns one working `Run` value (L219) and one `Definition`; nothing else can reach them. Readers only ever receive copies made by `copyRun` while the lock is held. `Close` takes ownership of the pending slice at L180-181 and then nothing else can touch those entries.

**Lock order.** There is one mutex in this file, so there is no ordering between runner locks. The one place the runner holds `mu` while calling outside itself is the initial save at L140, which in production opens a SQLite transaction (`runs.go:23`). The store never calls back into the runner, so the order is always runner mutex, then database, never the reverse. The scheduler has its own mutex but calls `Start` without holding it (`scheduler.go:125` versus `scheduler.go:140-142`), and the API holds no lock, so no caller nests a lock around `mu`. `wg.Wait` at L190 is called with `mu` released; if it held `mu`, every run goroutine would block at L287 and `Close` would never return.

**Which context, from where, canceled by whom.**

| Operation | Context | Parent | What ends it |
| --- | --- | --- | --- |
| Initial save (L139) | `r.ctx` + 5 s | `context.Background()` (L97) | `Close` (L179) or the timeout |
| Executor call (L247) | `r.ctx` | `Background` | `Close` only |
| Progress saves (L233, L269 via L350) | `r.ctx` + 5 s each | `Background` | `Close` or the timeout |
| Final save of an active run (L286, L322) | `WithoutCancel(r.ctx)` + 5 s | detached from `r.ctx` | the timeout only |
| Final saves of the whole queue (L203, L322) | `WithoutCancel(r.ctx)` + 5 s shared, then + 5 s nested | detached | the shared timeout, which always wins |

The HTTP request context never enters this file; `api.go:65` passes only the definition. That is `AGENTS.md:84-85` in practice: "Accepted runs belong to the runner's context, not the originating HTTP request." `Close` is the only caller of `cancel`, and `main.go:139-140` calls `Close` after `scheduler.Close`, honoring `AGENTS.md:102`: "Stop the scheduler before the runner."

**What happens on each failure path.**

- *Invalid definition* (L106): rejected before the lock; nothing saved, nothing counted.
- *Busy, full, or closed* (L116-125): rejected under the lock; nothing saved, nothing counted. `README.md:165`: "rejected starts are not saved as executions."
- *Initial save fails* (L142-144): `ErrRecordRun`; no memory entry, no busy mark, no slot, no goroutine. The workflow can be started again immediately.
- *Step-start save fails* (L233-242): the step is reset to `pending`, the run becomes `failed` with a run-level message, and no executor runs for that step. Finalization marks it `skipped` and saves with retries.
- *Executor returns an error* (L250-255, L282-284): the step is `failed`, its result is saved (L269), and later steps are `skipped`. Health is not involved: an unhealthy check is a successful execution.
- *Step-result save fails* (L269-280): the run becomes `failed` with a run-level message, the completed output stays in the working copy, and the final save retries writing it. No step is re-executed.
- *Final save fails after retries* (L308-312): `FinalSaveFailed` is set in memory only, the error is logged, SQLite keeps its last progress snapshot, and the API overlays the memory copy until eviction or restart (`api.go:144-149`, `README.md:541-552`).
- *Shutdown during a step* (L226, L233, L262): no new step starts; the run is `canceled` unless its final step already succeeded; the final save runs under a fresh five-second budget.
- *Shutdown with queued runs* (L180-189, L200-214): each is finished as `canceled` with every step `skipped` and no start time, under one shared five-second budget.
- *Process dies before any of this* : nothing in this file runs; startup marks the leftover `queued`/`running` rows `interrupted` (`main.go:95-96`, `store/run_interruptions.go:15`), per `AGENTS.md:98-99`.

**Rules from AGENTS.md that this file implements.**

- "Keep admission, capacity, queue promotion, and same-workflow overlap checks under the runner's mutex. Queued work owns no goroutine until promoted." (`AGENTS.md:78-79`): L114-155 for admission, L292-296 for promotion, L152 for the goroutine-free queue.
- "Executors and update callbacks may run concurrently for different workflows. Execution and progress/final writes stay outside the history mutex; initial creation currently holds it to serialize admission. Return copied snapshots." (`AGENTS.md:80-82`): L246-247, L233, L269, L286 run without the lock; L140 runs with it; `copyRun` at every boundary.
- "Save a new run and its workflow snapshot before acceptance. Failed initial saves must not execute actions or consume capacity. Accepted runs belong to the runner's context, not the originating HTTP request." (`AGENTS.md:83-85`): L136-148 and L97.
- "Save step start before execution and step result before proceeding. A failed progress save stops later steps. Preserve completed outputs and skip steps that never ran." (`AGENTS.md:86-88`): L233, L269, L278-279, L303-307.
- "Final-save retries repeat only the completed snapshot, never node actions. Keep retries and shutdown cleanup bounded by their existing deadlines." (`AGENTS.md:89-90`): L308-312, L316-344, L202-204.
- "Distinguish execution outcome, monitored-service health, and save failure. `finalSaveFailed` is a memory-only warning, not a new execution status." (`AGENTS.md:91-94`): L44-46, L250-258, L310.
- "Stop the runner before closing SQLite and releasing the lock." (`AGENTS.md:96`): `Close` waits for every save at L190 so `main.go:80` closes SQLite afterwards.

## Check yourself

1. `Start` holds the mutex while it calls `recordNewRun`, which can take up to five seconds. Why is that accepted, and what does it cost?

<details><summary>Answer</summary>The overlap check (L119), the capacity decision (L122-125), the save (L140), and the registration (L146-155) must be one atomic decision. If the lock were released around the save, two starts for the same workflow could both pass the busy check, or two starts could both take the last slot. The comment at L137 and `README.md:160-161` state the trade: initial creation stays serialized. The cost is that every other runner call (`Get`, `List`, `publishRun`, other `Start`s) waits during a slow initial save, bounded by the five-second timeout at L139. Progress and final saves do not pay this cost because they run outside the lock (L246).</details>

2. A two-step workflow is running. Its second executor returns a healthy result, and in the same instant `Close` cancels the runner's context, so the result save at L269 fails with `context.Canceled`. What status is saved, and which lines decide it?

<details><summary>Answer</summary>`succeeded`. At L261 `allStepsSucceeded` is true (no error, last step). If the cancellation is visible at L262, the loop breaks without changing `status`; if it becomes visible only when the save fails, L270-271 keep `status` because `allStepsSucceeded` is true. `finishRun` at L286 then saves with `context.WithoutCancel(r.ctx)` plus its own five seconds, so the final save still runs. `README.md:173-175` promises this and `runner_shutdown_test.go:27-30` ("final result save canceled") tests it. The comment at L259-260 states the reasoning: shutdown cannot undo work that already completed.</details>

3. Why is `r.activeRuns--` at L291 placed after `finishRun` rather than right after the step loop ends at L285? What would change if you moved it?

<details><summary>Answer</summary>The slot bounds the whole life of a run, including its final save with retries. Releasing it before the final save would let a queued run be promoted and start writing while the finishing run's save (up to three attempts in five seconds) is still in progress, so the configured active limit would no longer bound concurrent database writers or executors plus savers. `README.md:158-159` states the rule and `TestRunKeepsItsSlotUntilFinalSaveReturns` (`runner_concurrency_test.go:220`) asserts it. Moving the decrement would also require moving the promotion at L292-296 and the busy-release at L290, since all three happen under one lock acquisition.</details>

4. `wg.Add(1)` appears at L184 and L196, both under the mutex. Why under the mutex, and what could go wrong otherwise?

<details><summary>Answer</summary>`WaitGroup` requires that an `Add` which raises the counter from zero happens before the `Wait` it must delay. `Close` sets `closed` under the mutex (L178) and calls `Wait` after releasing it (L190). Every `Add` happens inside a lock region that also checks `closed` (L116 for `Start`, L292 for promotion) or that precedes the `closed` write (L184 inside `Close` itself). So any `Add` either completed before `Close` took the lock, and thus before `Wait`, or never happens. Without the lock, a promotion could call `Add` after `Wait` returned, and `Close` would return while a run goroutine was still writing to a SQLite handle that `main.go:80` is about to close.</details>

5. A run is queued when `Close` is called. List what happens to it, in order, and say why `activeRuns` is not decremented for it.

<details><summary>Answer</summary>`Close` sets `closed`, cancels `r.ctx`, and moves the queue into a local slice (L178-181), then `Add(1)` for the cleanup (L184). After unlocking, `cancelQueued` builds a shared five-second context detached from the canceled `r.ctx` (L203), fetches the run's snapshot (L206, still `queued`, zero `StartedAt`), and calls `finishRun` with `canceled` (L207): `FinishedAt` is set, every `pending` step becomes `skipped`, and `saveFinalRun` retries within the shared budget. Under the lock the snapshot is published, the busy mark is removed, and history is trimmed (L208-212). `activeRuns` is untouched because a queued run was never launched: `launchLocked` (L195) is the only place that increments it, and the queued entry went to L152 instead. The run ends `canceled` with no start time, as `README.md:176-177` describes.</details>

6. Suppose `copyRun` returned `run` unchanged (no `slices.Clone`). Which two goroutines would race, on which memory, and which test would the race detector most likely flag?

<details><summary>Answer</summary>`publishRun` (L362) would store a `Run` whose `Steps` header points at the run goroutine's own array. The run goroutine keeps writing to that array through `stepRun` (L232, L249, L255, L257). Meanwhile an HTTP handler calling `Get` (`api.go:145`) or a test calling `List` would read the same elements, and `json.Encoder` would read them during encoding. That is a write/read race on `run.Steps[i]`. `TestBusyShutdownAndConcurrentReads` (`runner_test.go:88`) and `TestSequentialChecksAndSnapshots` (`runner_test.go:42`) read history while a run is in progress, so `go test -race ./internal/engine` would report it there first. The comment at L380 names this exact hazard.</details>

## Experiments

1. **Shrink the memory history and predict the failures.** In `internal/engine/runner.go:367` change `100` to `50`. Run `go test -race ./internal/engine -run 'HistoryIsBounded|HistoryTrimming'`. Expected: three failures. `TestInvalidDefinitionNeverRunsAndHistoryIsBounded` fails with `history bound was not enforced` (`runner_test.go:163-164`), `TestActiveRunSurvivesMemoryHistoryTrimming` fails with `history trimming discarded the active run or exceeded the limit` (`runner_concurrency_test.go:215-216`), and `TestQueuedRunsSurviveHistoryTrimming` fails after `Close` with `shutdown left unfinished work or unbounded history` (`runner_concurrency_test.go:299-300`), because its earlier check at `:295` still passes: all 102 entries are unfinished, so none can be evicted until shutdown cancels them. Revert the change and rerun; all three pass.

2. **Reduce the final-save attempts.** In `runner.go:325` change `attempt < 3` to `attempt < 2`. Run `go test ./internal/engine -run 'TestFinalSaveRetriesUnchangedResult|TestSaveFailureStopsLaterStepsWithoutReplayingActions'`. Expected: `TestFinalSaveRetriesUnchangedResult` fails with `unexpected finalization: 2 attempts, ...` (`runner_updates_test.go:319-320`), and the two `keepFailing` subtests fail at `runner_updates_test.go:126-127`: `progress_and_final_save` with `got 4 saves, 1 executions, status failed` (it expected five writes, `:98`) and `all_final_saves_fail` with `got 6 saves, 2 executions, status succeeded` (it expected seven, `:100`). The other five subtests still pass, which shows that the attempt count only matters once storage keeps failing. Revert.

3. **Forget to release the busy mark.** Delete `runner.go:290` (`delete(r.busyWorkflows, definition.ID)`). Run `go test ./internal/engine -run TestInvalidDefinitionNeverRunsAndHistoryIsBounded`. Expected: the test fails on its second `Start` of the same workflow with `this workflow is already queued or running; wait for it to finish` (`runner_test.go:151-154`), because the first run finished but its workflow id stayed in the set checked at L119. Restore the line.

4. **Watch one run move through the loop.** Add `slog.Info("step started", "run_id", run.ID, "step", step.ID)` after `runner.go:244` and `slog.Info("step finished", "run_id", run.ID, "step", step.ID, "status", stepRun.Status)` after `runner.go:281`. Run `go run ./cmd/server` in one terminal. In another, run `curl -i -X POST -H 'Content-Type: application/json' http://127.0.0.1:8080/api/workflows/patchbay-health/runs`. Expected: the response is `202 Accepted` with `"status":"running"` and a `Location` header; the server terminal prints `step started` and then `step finished ... status=succeeded` for step `health`, in that order, after the `202` was already sent. Then `curl -s http://127.0.0.1:8080/api/runs/<id from Location>` shows `"status":"succeeded"` and no `finalSaveFailed` field. Press Ctrl+C in the server terminal, and remove the two log lines afterwards.

## Next

Chapter 7, "The HTTP and TCP check executors" ([07-node-executors.md](07-node-executors.md))
