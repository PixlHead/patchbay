# 3. Server startup, wiring, shutdown, and the database lock

Files: [cmd/server/main.go](../../cmd/server/main.go), [cmd/server/database_lock.go](../../cmd/server/database_lock.go)

Revision: commit 1c4786e

## Role in the system

`cmd/server/main.go` is the program's entry point and its composition root. The roadmap gives it exactly one job: "Construct dependencies and start/shut down the app" (`docs/roadmap.md:197`). It reads seven command-line flags, builds each component in a fixed order (host guard, workflow definitions, database lock, SQLite handle, runner, scheduler, HTTP server), connects them with small closures, and then waits for one of two events: the HTTP listener fails, or the operating system asks the process to stop. Every component it creates is released in reverse order by `defer`. Nothing in this file executes a workflow step, reads a run, or touches a table; it only decides who owns what, in which order things start, and in which order they stop.

Data comes in as flags (`main.go:29-35`), as JSON files read by `workflow.Load` (`main.go:58`), as a SQLite file opened by `store.Open` (`main.go:75`), and as `SIGINT`/`SIGTERM` signals turned into a context (`main.go:38`). Data goes out as structured log lines, as an exit code (`main.go:41-42`), and as the HTTP server that the browser talks to (`main.go:122-129`). On the path from a browser click to a saved run, this file sits at both ends. The click reaches `hostGuard.Wrap` first (`main.go:123`, `internal/httpapi/host_guard.go:39-50`), then the API mux (`internal/httpapi/api.go:50`), which calls `runner.Start` (`api.go:65`). The runner saves the new run through the closure built at `main.go:107-109`, saves progress through the closure at `main.go:109-111`, and executes each step through the dispatcher at `main.go:151-162`. At shutdown the same file decides that the scheduler stops first, then the runner, then HTTP, then SQLite, then the lock (`main.go:139-145`, `main.go:70-84`).

`cmd/server/database_lock.go` holds one function, `lockDatabase`. It makes sure that only one Patchbay process uses a given database file at a time, by taking an exclusive operating-system lock on a sibling file named `<database>.lock` (`README.md:94-98`). It runs before migrations and before startup cleanup can change saved runs (`main.go:65-66`), which is a rule in `AGENTS.md:95-97`. The file only compiles on Linux and macOS (`database_lock.go:1`); the roadmap limits development to those platforms (`docs/roadmap.md:11`).

## Concepts you need

- **`package main` and `func main`.** A Go program starts in the function `main` of the package `main`. When `main` returns, the process exits with code 0. Everything else in the program runs because `main` called it or started it. [The Go Programming Language Specification: Program execution](https://go.dev/ref/spec#Program_execution)
- **Command-line flags with `flag`.** `flag.String("addr", "127.0.0.1:8080", "help text")` registers a flag and returns a pointer to a string that holds the default until `flag.Parse()` reads the real command line. You read the value with `*addr`. `-h` prints the help text of every registered flag. [flag](https://pkg.go.dev/flag)
- **Pointers versus values.** A pointer (`*string`, `*os.File`, `*Runner`) is the address of a value. Reading through it is written `*p`. `flag` returns pointers so it can fill them in later; `lockDatabase` returns `*os.File` because the open file is a resource you must close, not a value you copy. [A Tour of Go: Pointers](https://go.dev/tour/moretypes/1)
- **`context.Context` and cancellation.** A context carries a cancellation signal and an optional deadline down a call tree. `signal.NotifyContext` returns a context that is canceled when the process receives a signal. `context.WithTimeout(parent, 5*time.Second)` returns a child that is also canceled after five seconds, plus a `cancel` function you must call to release its timer. Functions that may block take a context as their first parameter and stop early when `ctx.Done()` closes. [context](https://pkg.go.dev/context), [os/signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext), [Go blog: Context](https://go.dev/blog/context)
- **`defer` and last-in, first-out order.** `defer f()` schedules `f` to run when the surrounding function returns, whether by `return` or by panic. Deferred calls run in reverse order of registration. `runServer` relies on this to close the lock last, because it was deferred first. [A Tour of Go: Defer](https://go.dev/tour/flowcontrol/12), [Spec: Defer statements](https://go.dev/ref/spec#Defer_statements)
- **`os.Exit` skips deferred calls.** `os.Exit(1)` ends the process immediately. No deferred function runs. That is why `main` only logs and exits, while all cleanup lives in `runServer`, which returns an error instead. [os.Exit](https://pkg.go.dev/os#Exit)
- **Sentinel errors, `%w` wrapping, and `errors.Is`.** `errors.New("...")` creates one fixed error value; comparing against it tells you which failure happened. `fmt.Errorf("lock database: %w", err)` builds a new error that still contains `err`, and `errors.Is(outer, sentinel)` walks that chain. `lockDatabase` returns the sentinel `errDatabaseInUse`; `runServer` wraps it; tests still recognize it. [errors](https://pkg.go.dev/errors), [fmt.Errorf](https://pkg.go.dev/fmt#Errorf)
- **`if err := f(); err != nil` and shadowing.** An `if` may start with a short statement; a variable declared there exists only inside the `if`. Inside any inner block, `:=` creates a new variable even if an outer one has the same name. This is called shadowing. `database_lock.go:37` uses `var directory string` precisely to avoid shadowing `err` inside a `case`. [Spec: Declarations and scope](https://go.dev/ref/spec#Declarations_and_scope)
- **Goroutines and the `go` statement.** `go f()` starts `f` on a new lightweight thread and returns at once. `main.go:129` starts the HTTP listener this way so the main goroutine can wait on other events. [A Tour of Go: Goroutines](https://go.dev/tour/concurrency/1)
- **Channels, including buffered channels.** A channel passes values between goroutines. `make(chan error, 1)` creates a buffer of one, so one send completes even when nobody is receiving. Without that buffer the listener goroutine would block forever on the shutdown path. [A Tour of Go: Buffered channels](https://go.dev/tour/concurrency/3)
- **`select`.** `select` waits until one of several channel operations can proceed and runs only that case. `main.go:132-146` waits for either the listener's error or the signal context's `Done()` channel. [A Tour of Go: Select](https://go.dev/tour/concurrency/5)
- **Closures.** A function literal (`func(...) {...}`) can use variables from the surrounding function. The two callbacks at `main.go:107-111` capture `db`, which is how the runner saves runs without ever being told about SQLite. [A Tour of Go: Function closures](https://go.dev/tour/moretypes/25)
- **Function types as dependencies.** `engine.ExecuteStep` is a named function type (`internal/engine/runner.go:51`). Any function with the same signature is assignable to it. The engine takes functions instead of interfaces so tests can pass a fake executor without a plugin framework (`runner.go:48-50`, `docs/roadmap.md:78`). [Spec: Function types](https://go.dev/ref/spec#Function_types)
- **Method values.** `runner.Start` without parentheses is a method bound to the receiver `runner`. It is a plain function value, so it fits the `schedule.StartRun` type at `internal/schedule/scheduler.go:19`. [Spec: Method values](https://go.dev/ref/spec#Method_values)
- **Implicit interfaces.** A type satisfies an interface by having the right methods; no `implements` keyword exists. `*schedule.Scheduler` satisfies `httpapi.NextRunSource` (`internal/httpapi/api.go:21-23`) because it has `NextRun` (`scheduler.go:105`). A nil interface value and a nil pointer stored in an interface are different things; `checks_test.go:80` passes an untyped `nil` on purpose. [A Tour of Go: Interfaces are implemented implicitly](https://go.dev/tour/methods/10)
- **Blank imports.** `import _ "time/tzdata"` imports a package only for its side effect at startup. `time/tzdata` embeds a time zone database into the binary; `internal/store/sqlite.go:13` uses the same trick to register the SQLite driver. [Spec: Import declarations](https://go.dev/ref/spec#Import_declarations), [time/tzdata](https://pkg.go.dev/time/tzdata)
- **Structured logging with `log/slog`.** `slog.Info("msg", "key", value, ...)` writes a message plus key-value pairs. The default handler prints to standard error. Keys such as `run_id` make log lines searchable. [log/slog](https://pkg.go.dev/log/slog)
- **`http.Server`, its timeouts, and `Shutdown`.** `http.Server` holds the listen address, the handler, and read/write/idle deadlines that stop a slow client from holding a connection open forever. `ListenAndServe` blocks until the server stops. `Shutdown(ctx)` stops accepting, lets in-flight requests finish until `ctx` expires, and makes `ListenAndServe` return `http.ErrServerClosed`. `Close` cuts connections immediately. [net/http.Server](https://pkg.go.dev/net/http#Server), [Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
- **Build constraints.** A `//go:build darwin || linux` line before the package clause tells the Go tool to compile the file only on those operating systems. The same expression must then be satisfied by any file that calls the functions inside. [Go command: Build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- **File descriptors and `syscall.Flock`.** An open file has a small integer, the descriptor, that the kernel uses to identify it. `flock(fd, LOCK_EX|LOCK_NB)` asks for an exclusive advisory lock on that open file and fails at once with `EWOULDBLOCK` if another open file already holds one. The lock belongs to the open file, not the process, and the kernel releases it when the file is closed or the process dies. [syscall.Flock](https://pkg.go.dev/syscall#Flock), [flock(2)](https://man7.org/linux/man-pages/man2/flock.2.html)
- **Paths and symlinks: `filepath.Abs`, `os.Lstat`, `filepath.EvalSymlinks`.** `Abs` turns a relative path into an absolute one using the working directory. `Lstat` describes a path without following a symbolic link. `EvalSymlinks` follows every link in a path and returns the real location, or an error if a link points nowhere. Together they let two spellings of one file map to one lock. [path/filepath](https://pkg.go.dev/path/filepath#EvalSymlinks), [os.Lstat](https://pkg.go.dev/os#Lstat)
- **`os.OpenFile` flags and octal permission literals.** `os.O_CREATE|os.O_RDWR` means "create if missing, open for reading and writing"; the flags are combined with bitwise OR. `0o600` is an octal literal: owner read and write, nobody else. [os.OpenFile](https://pkg.go.dev/os#OpenFile), [Spec: Integer literals](https://go.dev/ref/spec#Integer_literals)
- **Tagless `switch`.** `switch { case cond1: ... case cond2: ... }` with no value after `switch` is an `if`/`else if` chain written as cases. Only the first true case runs. [A Tour of Go: Switch with no condition](https://go.dev/tour/flowcontrol/11)

## Walkthrough

Keep these signatures in view while reading: `engine.New` at `internal/engine/runner.go:90`, `Runner.Start` at `runner.go:105`, `Runner.Close` at `runner.go:176`, `schedule.New` at `internal/schedule/scheduler.go:53`, `store.Open` at `internal/store/sqlite.go:20`, `store.CheckWritable` at `internal/store/writable.go:12`, `store.MarkUnfinishedRunsInterrupted` at `internal/store/run_interruptions.go:15`, `httpapi.NewHostGuard` at `internal/httpapi/host_guard.go:20`, and `httpapi.New` at `internal/httpapi/api.go:32`.

### cmd/server/main.go

**L1–L1** — `package main`. This package builds into an executable. `make build` compiles it to `bin/patchbay` (`Makefile:13`), the Dockerfile compiles it with `CGO_ENABLED=0` (`Dockerfile:13`), and `make api` runs it directly (`Makefile:27`).

**L2–L2** — Blank line.

**L3–L14** — Standard library imports. Each one is used below: `context` for cancellation, `errors` for `errors.Is` at L134, `flag` for L29-36, `fmt` for error wrapping, `log/slog` for every log line, `net/http` for the server at L122, `os` for `os.Interrupt`, `os.Exit`, and `os.MkdirAll`, `os/signal` for L38, `path/filepath` for L62, `syscall` for `SIGTERM`, and `time` for the deadlines. Go refuses to compile an unused import, so this list is also an inventory of what the file does.

**L15–L17** — A blank line, a comment, and `_ "time/tzdata"`. The blank import embeds a copy of the IANA time zone database in the binary. It exists because workflow schedules name a time zone such as `America/New_York` (`README.md:368-369`), and `time.LoadLocation` must resolve it even in a container with no `/usr/share/zoneinfo`. The Docker image also installs `tzdata` (`Dockerfile:16`); the embedded copy is the fallback the README promises at `README.md:389-390`. Without this line, a scheduled workflow with a non-UTC zone would fail validation on a minimal host (`internal/workflow/schedule.go:43-46`).

**L18–L26** — Imports of Patchbay's own packages, then the closing parenthesis. The module is named `patchbay` (`go.mod:1`), so `patchbay/internal/engine` is the engine package in this repository. Packages under `internal/` can only be imported from inside this module; that is a Go rule, not a Patchbay convention. The list shows the dependency direction: `main` knows every package, and no package under `internal/` imports `main`.

**L27–L27** — Blank line.

**L28–L28** — `func main()` begins. It has no parameters and no return value; the process exit code is set by L42 or by returning normally (exit 0).

**L29–L29** — `addr := flag.String("addr", "127.0.0.1:8080", ...)`. Where the HTTP server listens. The default binds loopback only, which is part of keeping the unauthenticated prototype local (`README.md:37-38`, `AGENTS.md:113-114`). Docker overrides it to `0.0.0.0:8080` so the container can be reached (`Dockerfile:24`), and Compose then publishes the port on `127.0.0.1` only (`compose.yaml:6`). Playwright uses port 18080 (`web/playwright.config.ts:12`). `addr` is a `*string`; L40 reads `*addr`.

**L30–L30** — `directory := flag.String("workflows", "workflows", ...)`. The directory `workflow.Load` scans at L58. Chapter 2 covered what lives there. Playwright points it at test fixtures (`web/playwright.config.ts:12`).

**L31–L31** — `webDir := flag.String("web", "web/dist", ...)`. The built frontend directory served by the API's catch-all route (`internal/httpapi/api.go:122-134`). If `index.html` is missing, that route answers 503 with build instructions (`api.go:129-132`), so a wrong value here does not stop startup. Tests pass `""` (`cmd/server/main_test.go:25`).

**L32–L32** — `dbPath := flag.String("db", "data/patchbay.db", ...)`. The SQLite file. `README.md:78-83` documents the default and the `-db` override. The same string names the lock file at `database_lock.go:47`.

**L33–L33** — `maxActiveRuns := flag.Int("max-active-runs", 2, ...)`. How many workflows may execute at once (`README.md:137-138`). It is validated at L48 and again inside `engine.New` (`runner.go:91-93`).

**L34–L34** — `maxQueuedRuns := flag.Int("max-queued-runs", 10, ...)`. How many accepted runs may wait for a slot (`README.md:139-140`). Zero disables queuing, as the help text says and `runner.go:88` repeats.

**L35–L35** — `allowedHosts := flag.String("allowed-hosts", "", ...)`. Extra hostnames the host guard will accept beyond `localhost`, `127.0.0.1`, and `::1` (`README.md:117-124`). Parsed at L54.

**L36–L36** — `flag.Parse()`. Reads `os.Args` and fills the pointers above. Until this line, every flag still holds its default. An unknown flag or a malformed integer makes `flag` print usage and exit with code 2 before any of the code below runs.

**L37–L37** — Blank line.

**L38–L38** — `stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. Creates the root context of the whole process and ties it to two signals: Ctrl+C (`os.Interrupt`) and `SIGTERM`, which `docker compose stop` sends first (`compose.yaml:10` gives it 20 seconds before `SIGKILL`). From here on, a signal does not kill the process; it cancels `stop`, and the `select` at L137 turns that into an orderly shutdown. The M0 walkthrough names this as the design: "`main` listens for Ctrl+C or SIGTERM" (`docs/m0-walkthrough.md:112`). A second Ctrl+C during shutdown does nothing extra, because the signal behavior stays overridden until `cancel` runs at L39; shutdown is bounded by deadlines instead.

**L39–L39** — `defer cancel()`. Releases the signal registration and its goroutine when `main` returns. It is the only defer in `main`, and it is safe here because L42's `os.Exit` only runs on the error path, after `runServer` has already released everything that matters.

**L40–L43** — `if err := runServer(stop, *addr, ...); err != nil { slog.Error(...); os.Exit(1) }`. Dereferences every flag pointer and hands the values to `runServer`. On error it logs one line, `patchbay failed`, and exits with code 1. The comment at L46 explains the split: `os.Exit` skips deferred calls, so cleanup must finish inside `runServer` before control gets back here. If the database lock were released in `main` instead, a startup failure would leave it held until the process exited, and nothing would log a close failure. Tests exercise `runServer` directly, never `main` (`main_test.go:25`, `main_test.go:143`).

**L44–L44** — Closing brace of `main`. Reaching it means `runServer` returned `nil`; the deferred `cancel` runs and the process exits 0.

**L45–L45** — Blank line.

**L46–L46** — The comment `Returning errors lets deferred cleanup finish before main exits the process.` This is the rule that shapes the next hundred lines: every failure is a `return fmt.Errorf(...)`, never an exit.

**L47–L47** — `func runServer(ctx context.Context, addr, directory, webDir, dbPath string, maxActiveRuns, maxQueuedRuns int, allowedHosts string) error`. Go lets you list several parameter names before one type, so `addr, directory, webDir, dbPath string` declares four strings. `ctx` is the signal context from L38. Taking plain values instead of reading flags again is what makes the function testable: `main_test.go:25` calls it with an invalid address on purpose so no network listener opens.

**L48–L50** — `if maxActiveRuns < 1 { return fmt.Errorf("max-active-runs must be at least 1") }`. The first check, before any file or directory is touched. `engine.New` would reject the same value at `runner.go:91-93`, but by then the database directory, lock, and SQLite file would already exist. `TestRunServerRejectsInvalidActiveRunLimitBeforeTouchingStorage` (`main_test.go:200-211`) asserts both the message and that the `data` directory was never created (`main_test.go:207-209`). The message uses the flag's spelling with hyphens so the operator can find it in `-h` output.

**L51–L53** — `if maxQueuedRuns < 0 { return fmt.Errorf("max-queued-runs must be at least 0") }`. Same idea for the queue limit; zero is allowed (`README.md:139-140`). `TestRunServerRejectsNegativeQueueLimitBeforeTouchingStorage` (`main_test.go:213-222`) covers it.

**L54–L57** — `hostGuard, err := httpapi.NewHostGuard(allowedHosts)`, wrapped as `invalid allowed-hosts configuration`. Builds the allowlist used by L123. The constructor seeds the three loopback names and then parses each comma-separated entry (`host_guard.go:21-33`); an entry with a scheme, port, wildcard, or IPv6 brackets is an error (`host_guard.go:30`). This also runs before storage is touched; `TestRunServerRejectsInvalidAllowedHostsBeforeTouchingStorage` passes `"*"` and checks that (`main_test.go:224-233`). `err` is declared here with `:=` and reused by `=` or re-declared with `:=` alongside a new variable through the rest of the function.

**L58–L61** — `definitions, err := workflow.Load(directory)`, wrapped as `invalid workflow configuration`. Reads, size-limits, strictly decodes, and validates every `.json` file (`internal/workflow/workflow.go:147-194`). An empty directory is an error too (`workflow.go:190-191`), so the server never starts with nothing to run. The slice is shared by the scheduler (L116) and the API (L123); neither mutates it. Loading before the lock means a typo in a workflow file is reported without ever claiming the database.

**L62–L64** — `os.MkdirAll(filepath.Dir(dbPath), 0o700)`, wrapped as `create database directory`. Creates the parent of the database file, for example `data/`, with owner-only permissions (`README.md:78-79`). `MkdirAll` leaves an existing directory and its permissions alone, which upholds "Preserve existing file and directory permissions" (`AGENTS.md:109`). Both `lockDatabase` and `store.Open` require the parent to exist (`database_lock.go:16`, `sqlite.go:17`). When the parent path is a regular file, this is where startup fails; `main_test.go:60` expects `create database directory` for that case. Inside Docker the directory is pre-made and owned by the unprivileged user (`Dockerfile:21-22`), and the rest of the filesystem is read-only (`compose.yaml:11`), so this call must be a no-op there.

**L65–L69** — The comment `Take ownership before migrations or startup cleanup can change saved runs.`, then `databaseLock, err := lockDatabase(dbPath)` wrapped as `lock database %q`. This is the rule "Take the database ownership lock before migrations or interruption cleanup" (`AGENTS.md:95-96`) made concrete. `store.Open` at L75 runs migrations and `MarkUnfinishedRunsInterrupted` at L96 rewrites run statuses; if two servers did either at the same time, one could mark the other's live runs interrupted. The `%q` verb quotes the path so an empty or odd path is visible in the log. `errDatabaseInUse` travels inside the `%w` so `errors.Is` still finds it (`main_test.go:147`).

**L70–L74** — `defer func() { if err := databaseLock.Close(); err != nil { slog.Error(...) } }()`. The first deferred cleanup, so it runs last. Closing the file releases the `flock`. The function literal exists because `Close` returns an error and a bare `defer databaseLock.Close()` would discard it silently; here a failed close is logged. The inner `err` shadows the outer one on purpose; it lives only inside the closure. "Stop the runner before closing SQLite and releasing the lock" (`AGENTS.md:96`) is satisfied by defer order alone: the runner's defer at L115 and the database's at L80 are registered later, so they run earlier. Tests prove the lock is released after every later failure (`main_test.go:161-166`, `main_test.go:193`, `cmd/server/database_write_test.go:116`).

**L75–L78** — `db, err := store.Open(ctx, dbPath)`, wrapped as `open database %q`. Creates the file privately with mode `0600` if it is new (`sqlite.go:34`), opens SQLite in existing-file mode (`sqlite.go:46-48`), limits the pool to one connection (`sqlite.go:59-60`), pings it (`sqlite.go:63`), and runs the identity check and migrations in one transaction (`sqlite.go:67`, `internal/store/schema.go:56-118`). It receives the signal context, so Ctrl+C during a migration cancels it (`sqlite.go:28`, `schema.go:57`). A file that is not SQLite or belongs to another application fails here (`schema.go:67-69`); `TestRunServerReleasesDatabaseLockAfterOpenFailure` checks that the lock from L66 is released afterwards (`main_test.go:184-198`).

**L79–L84** — The comment `Defers run in reverse order: stop the runner, close SQLite, release the lock.` and `defer func() { if err := db.Close(); ... }()`. Same shape as L70-74. `store.Open`'s doc says "The caller owns the returned database and must close it during shutdown" (`sqlite.go:19`). The comment spells out the order that the defers at L70, L80, and L115 produce. Closing SQLite while a run was still saving would turn a good run into a save failure, which is why the runner must be closed first.

**L85–L85** — Blank line.

**L86–L89** — The comment `Opening an existing database can succeed even when it cannot commit writes.`, then `writeCtx, cancelWrite := context.WithTimeout(ctx, 5*time.Second)`, `err = store.CheckWritable(writeCtx, db)`, and `cancelWrite()`. The check commits one `PRAGMA user_version` write that changes nothing (`writable.go:17`). It exists because SQLite opens a read-only directory happily and only fails when it needs to create a journal file; `TestRunServerRejectsUnwritableJournalDirectoryBeforeCleanup` builds exactly that situation (`database_write_test.go:21-126`). This is the rule "After opening and recognizing the database, commit a bounded write check before interruption cleanup or HTTP startup" (`AGENTS.md:110-111`), documented at `README.md:499-511`. Note two idioms. `err =` with a single `=` reuses the existing variable, because the `if` form cannot be used here: `cancelWrite()` must run between the call and the check so the timer is released now rather than at function exit, which for a server is hours later. And the deadline is a child of `ctx`, so a signal also cancels the check.

**L90–L92** — `if err != nil { return fmt.Errorf("check database writeability: %w", err) }`. On failure the deferred closes release SQLite and the lock, and no history has changed (`database_write_test.go:113-123`). The README adds the limit of the guarantee: "a successful startup check does not guarantee future writes" (`README.md:509-511`, `AGENTS.md:112`).

**L93–L93** — Blank line.

**L94–L97** — The comment `Reconcile saved history before creating a runner or accepting requests.`, then `interruptionCtx, cancelInterruption := context.WithTimeout(ctx, 5*time.Second)`, `interrupted, err := store.MarkUnfinishedRunsInterrupted(interruptionCtx, db)`, `cancelInterruption()`. In one transaction, every saved run still marked `queued` or `running` becomes `interrupted`, its running steps become `interrupted`, and its pending steps become `skipped` (`run_interruptions.go:24-32`). Such rows can only exist because a previous process died or was killed before its final save. The function's own doc warns "Call only at startup before any runner uses this database" (`run_interruptions.go:12`); placing it before `engine.New` at L107 is what honors that. This is the rule "Startup marks leftover queued/running records interrupted; it does not resume actions" (`AGENTS.md:98-99`), explained for operators at `README.md:569-584`. `:=` is legal here because `interrupted` is new.

**L98–L100** — `if err != nil { return fmt.Errorf("mark unfinished runs interrupted: %w", err) }`. "The cleanup is one transaction. Failure prevents startup" (`README.md:577`). `TestRunServerReconcilesHistoryBeforeListening` installs a SQLite trigger that rejects the update and asserts the error arrives before any listen attempt (`main_test.go:122-130`, `main_test.go:150-153`).

**L101–L103** — `if interrupted > 0 { slog.Info("marked unfinished runs interrupted", "runs", interrupted) }`. Logs only when something changed, so a clean restart stays quiet. The count comes from `RowsAffected` on the `runs` update (`run_interruptions.go:36-39`).

**L104–L104** — Blank line.

**L105–L106** — `httpNode := httpcheck.New()` and `tcpNode := tcpcheck.New()`. One executor of each kind for the whole process. The HTTP one owns a cloned transport with no proxy and bounded headers (`internal/nodes/httpcheck/http.go:22-35`); the TCP one owns a dialer (`internal/nodes/tcpcheck/tcp.go:22-25`). Both are documented as safe for concurrent checks (`http.go:16`, `tcp.go:15`), which the runner requires because different workflows run at the same time (`runner.go:49-50`). Creating them once also means their connection pools are shared across runs.

**L107–L111** — `runner, err := engine.New(maxActiveRuns, maxQueuedRuns, checkExecutor(httpNode, tcpNode), func(ctx, run, definition) error { return store.CreateRun(ctx, db, run, definition) }, func(ctx, run) error { return store.UpdateRun(ctx, db, run) })`. This is the wiring that the whole chapter is named for. The engine package never imports `store`; instead `main` hands it three functions. The first is the step dispatcher from L151. The second saves a new run and its workflow snapshot (`internal/store/runs.go:17-62`) and is called while the runner holds its admission mutex (`runner.go:136-145`). The third saves progress and final results (`runs.go:68`) and is called from the run goroutine (`runner.go:233`, `runner.go:269`, `runner.go:338`). Both closures capture `db`. The `ctx` each receives is derived from the runner's own context, never from an HTTP request (`runner.go:138-139`, `runner.go:350`), which implements "Accepted runs belong to the runner's context, not the originating HTTP request" (`AGENTS.md:84-85`). `checks_test.go:63-75` builds the same wiring by hand and wraps the callbacks to observe them.

**L112–L114** — `if err != nil { return fmt.Errorf("create runner: %w", err) }`. With the checks at L48-53 already passed, `engine.New` cannot fail today (`runner.go:91-96`), but it returns an error so a future constructor rule is handled in one place.

**L115–L115** — `defer runner.Close()`. Registered after the database's defer, so it runs before it. `Close` marks the runner closed, cancels its context, finishes every queued run as `canceled`, and waits for all run goroutines (`runner.go:176-191`). That wait is what keeps "Stop the runner before closing SQLite" true (`AGENTS.md:96`): no save can be in flight when L80's defer closes `db`. `Close` is idempotent, which matters because L140 also calls it explicitly.

**L116–L119** — `scheduler, err := schedule.New(definitions, runner.Start, schedule.SystemClock())`, wrapped as `create scheduler`. Keeps only the definitions that carry a `schedule` and precomputes their timetables (`scheduler.go:57-67`). `runner.Start` is passed as a method value, so a cron occurrence is admitted by exactly the same function the API calls (`api.go:65`); that is the rule "Scheduled starts call `Runner.Start` exactly as the API does" (`AGENTS.md:100`). `SystemClock()` supplies real `time.Now` and `time.After` (`scheduler.go:28-30`); tests swap in a fake clock. The constructor returns a struct with its own cancelable context (`scheduler.go:68-72`) but starts no goroutine yet.

**L120–L121** — The comment `Deferred after the runner, so it stops asking for runs before the runner closes.` and `defer scheduler.Close()`. Registered after L115, so it runs first. `Close` cancels the scheduler's context and waits for every waiting goroutine (`scheduler.go:98-101`); a loop that wakes during that window checks `ctx.Err()` before calling `Start` (`scheduler.go:121-124`). If the order were reversed, a timer could fire after the runner closed and the scheduler would log a skipped occurrence with `ErrClosed` (`scheduler.go:131-132`). The README states the same order (`README.md:383`) and so does `AGENTS.md:102`.

**L122–L126** — `server := &http.Server{ Addr: addr, Handler: hostGuard.Wrap(httpapi.New(definitions, runner, scheduler, db, webDir)), ReadHeaderTimeout: 5s, ReadTimeout: 10s, WriteTimeout: 10s, IdleTimeout: 60s }`. `httpapi.New` builds the route mux and wraps it so every response carries `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` (`api.go:135-139`). `hostGuard.Wrap` sits outside that, so a request with an unexpected `Host` gets 403 before any route runs (`host_guard.go:37-48`, `README.md:109-111`); this is "host validation enabled" from `AGENTS.md:113`. The `scheduler` argument is accepted as the `NextRunSource` interface so `GET /api/workflows` can show `nextRunAt` (`api.go:41-44`). The four timeouts bound slow clients: a client that never finishes its headers is dropped after five seconds, and no response may take longer than ten. The roadmap notes that a future event stream must get its own write deadline because of this `WriteTimeout` (`docs/roadmap.md:88`). Workflow runs are not affected by these numbers; `POST /api/workflows/{id}/runs` answers 202 immediately (`api.go:83-84`) and the run continues in the runner's goroutine.

**L127–L127** — `defer server.Close() // Also closes connections if graceful shutdown times out.` Registered last, so it runs first of all the defers. On the signal path, L143 asks for a graceful `Shutdown`; if that times out, this `Close` forcibly ends the remaining connections. On the listener-failure path, it is harmless. The returned error is discarded, which `defer` allows.

**L128–L128** — `errorsCh := make(chan error, 1)`. A channel with room for one error. `ListenAndServe` sends into it exactly once (L129). On the shutdown path nobody reads the channel, because the `select` took the other case; the buffer lets that final send complete so the listener goroutine can exit instead of leaking.

**L129–L129** — `go func() { errorsCh <- server.ListenAndServe() }()`. Starts the HTTP server on its own goroutine. `ListenAndServe` binds `addr`, accepts connections until `Shutdown` or `Close`, and then returns `http.ErrServerClosed`; if the bind fails, for example because the port is taken or the address does not parse, it returns that error at once. Tests rely on the second behavior: `"invalid-listen-address"` makes the goroutine send a `*net.AddrError` right away (`main_test.go:24-29`), which proves that everything above this line ran first.

**L130–L130** — `scheduler.Start()`. Computes the first occurrence of every scheduled workflow and starts one waiting goroutine per entry (`scheduler.go:78-94`). It is called after the listener goroutine is launched and after the runner exists, so the first occurrence can be admitted as soon as it arrives. It is a plain call with no return value; `Start` is a no-op the second time (`scheduler.go:81-83`).

**L131–L131** — `slog.Info("patchbay M0 starting", "address", addr, "workflows", len(definitions), ...)`. One log line with the effective configuration: address, workflow count, storage kind, database path, and both run limits. The `M0` label is the project's milestone name (`README.md:1`). This is the line to look for when checking which `-db` or limits a running server actually used.

**L132–L132** — `select {`. The main goroutine now blocks until one of the two cases below can proceed. Nothing else runs on this goroutine for the server's whole lifetime.

**L133–L136** — `case err := <-errorsCh: if !errors.Is(err, http.ErrServerClosed) { return fmt.Errorf("HTTP server failed: %w", err) }`. The listener stopped on its own. A real error (port in use, bad address) is returned and `main` exits 1; because this is a `return`, every deferred close still runs, which `TestRunServerReconcilesHistoryBeforeListening` checks by re-locking the database afterwards (`main_test.go:161-166`). `ErrServerClosed` is treated as success; it cannot normally arrive in this branch, but handling it keeps the code correct if `Close` were ever called from elsewhere.

**L137–L140** — `case <-ctx.Done(): slog.Info("shutting down"); scheduler.Close(); runner.Close()`. A signal arrived. The order is deliberate and explicit rather than left to defers: first stop admitting new runs, then cancel and drain the running ones. `scheduler.Close` returns once no scheduler goroutine can call `Start` again (`scheduler.go:98-101`). `runner.Close` cancels the runner's context, which every executor observes through its per-step context (`http.go:40`, `tcp.go:30`), finishes queued runs as `canceled` under one shared five-second budget (`runner.go:200-214`), lets each active run finish its final save with a fresh five-second budget (`runner.go:286`, `runner.go:322`), and waits for all of them (`runner.go:190`). The README describes the visible outcome (`README.md:172-180`) and `docs/m0-walkthrough.md:112-115` describes the design. "Keep retries and shutdown cleanup bounded by their existing deadlines" (`AGENTS.md:90`) is why this step cannot hang forever on a healthy executor; the roadmap plans a hard runner deadline once SSH executors exist (`docs/roadmap.md:119`).

**L141–L142** — `shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)` and `defer cancelShutdown()`. A deadline for HTTP shutdown. It is derived from `context.Background()`, not from `ctx`, on purpose: `ctx` is already canceled at this point, and a child of it would expire immediately, giving in-flight requests no time at all. A `defer` inside a `case` still runs at function return, which is a few lines away.

**L143–L145** — `if err := server.Shutdown(shutdownCtx); err != nil { return fmt.Errorf("HTTP shutdown failed: %w", err) }`. Stops the listener, waits up to five seconds for active requests, and makes the goroutine at L129 send `ErrServerClosed` into the buffered channel. Requests that start a run during this window get 503, because the runner is already closed (`runner.go:116-118`, `api.go:73-74`); history reads still work because SQLite is still open. If the deadline passes, the error is returned, `main` exits 1, and the deferred `server.Close` from L127 cuts the stragglers. The README sums the budgets: shutdown may take runner cleanup plus HTTP shutdown, and Compose allows 20 seconds (`README.md:524-526`, `compose.yaml:10`).

**L146–L146** — Closing brace of the `select`.

**L147–L147** — `return nil`. The normal end. The deferred functions now run in reverse: `cancelShutdown`, `server.Close`, `scheduler.Close` (already closed, returns at once), `runner.Close` (already closed, returns at once), `db.Close`, `databaseLock.Close`. Only after the last one does `main` get control and exit 0.

**L148–L148** — Closing brace of `runServer`.

**L149–L149** — Blank line.

**L150–L150** — The comment `Keep dispatch explicit while the app has just two check executors.` The roadmap says a node registry will own the type-to-executor mapping later, and that until then "explicit switches hold that mapping. Today three switches hold it: step decoding, validation, and executor dispatch" (`docs/roadmap.md:99`). This function is the third switch. `AGENTS.md:120` names it as the place where a new node type gets its "dispatch wiring in `cmd/server`".

**L151–L151** — `func checkExecutor(httpNode *httpcheck.Executor, tcpNode *tcpcheck.Executor) engine.ExecuteStep`. Takes the two executors and returns a value of the engine's named function type (`runner.go:51`). Because the result is a plain function, `engine` compiles without importing either node package.

**L152–L152** — `return func(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {`. The closure captures `httpNode` and `tcpNode`. The runner calls it once per step with its own context (`runner.go:247`), possibly from several goroutines at once, which is safe because the captured pointers are never reassigned and the executors are concurrency-safe.

**L153–L157** — `switch step.Type { case "http.check": return httpNode.Execute(ctx, step) case "tcp.check": return tcpNode.Execute(ctx, step)`. The same two strings that the step decoder accepts (`internal/workflow/step_json.go:29`, `step_json.go:39`) and that `Validate` accepts (`workflow.go:87-108`). Each executor derives a per-step deadline from `step.Config.TimeoutMS` (`http.go:40`, `tcp.go:30`) and returns a `CheckResult`; an unhealthy target is a result, not an error (`README.md:236-239`).

**L158–L160** — `default: return workflow.CheckResult{}, fmt.Errorf("unsupported step type %q", step.Type)`. Unreachable through the API today, because `Runner.Start` validates the definition first (`runner.go:106`, `workflow.go:109-110`). It exists so that a future type added to the decoder but not here fails one step with a clear error (`runner.go:250-255`) instead of panicking. `workflow.CheckResult{}` is the zero value of the struct, returned alongside the error because Go requires every return value.

**L161–L162** — Closing braces of the function literal and of `checkExecutor`.

### cmd/server/database_lock.go

**L1–L1** — `//go:build darwin || linux`. A build constraint: this file is compiled only on macOS and Linux. `syscall.Flock` does not exist on Windows. Because `main.go:66` calls `lockDatabase` unconditionally and no other file in `cmd/server` defines it, the server package only builds on those two systems; that matches the roadmap's development platforms (`docs/roadmap.md:11`) and the README's "On Linux and macOS" wording (`README.md:94-95`). Two of the three test files that call `lockDatabase` carry the same line (`cmd/server/database_lock_test.go:1`, `database_write_test.go:1`); `main_test.go` calls it without one, which only works because the package cannot compile anywhere else anyway. The constraint must come before the package clause, with a blank line between.

**L2–L2** — Blank line, required after the build constraint.

**L3–L3** — `package main`. Same package as `main.go`, so `lockDatabase` and `errDatabaseInUse` are visible there without an import, and unexported (lowercase) names are enough.

**L4–L4** — Blank line.

**L5–L11** — Imports: `errors` for the sentinel and `errors.Is`, `fmt` for wrapping, `os` for `Lstat`, `OpenFile`, and `ErrNotExist`, `path/filepath` for `Abs`, `EvalSymlinks`, `Dir`, `Base`, and `Join`, and `syscall` for `Flock` and its flags.

**L12–L12** — Blank line.

**L13–L13** — `var errDatabaseInUse = errors.New("database is already in use by another Patchbay instance")`. A package-level sentinel error. Callers compare against it with `errors.Is` rather than parsing text: `main_test.go:147` does so through `runServer`'s `%w` wrapping, and `database_lock_test.go:27` directly. The text is what the README tells operators to expect from a second server (`README.md:96-97`). Creating it once at package load means every occurrence is the same value, which is what makes identity comparison work.

**L14–L14** — Blank line.

**L15–L17** — The doc comment. It states the contract in three sentences: the function reserves a database for one server on Linux/macOS; the parent directory must exist (which `main.go:62` guarantees); and the returned file must be closed only after the runner and SQLite have stopped, which `main.go:70-84` and `main.go:115` arrange through defer order. The last sentence, "The OS also releases the lock on process exit", is the crash guarantee: no stale lock survives a `kill -9`, and `TestDatabaseLockReleasedAfterProcessExit` proves it with a real child process (`database_lock_test.go:96-153`).

**L18–L18** — `func lockDatabase(dbPath string) (*os.File, error)`. Returns the open lock file itself, because the lock lives on that open file. Dropping the `*os.File` without closing it would keep the lock until garbage collection or exit, so the caller must hold it.

**L19–L21** — `if dbPath == "" { return nil, fmt.Errorf("database path must not be empty") }`. An empty path would otherwise become the working directory after `Abs`, and L32-34 would reject it with the misleading `database path is a directory` instead of a message that names the real mistake. `main_test.go:58` expects the wrapped message `lock database` for this case. `store.Open` repeats the same check (`sqlite.go:21-23`) because it can be called on its own.

**L22–L25** — `absolute, err := filepath.Abs(dbPath)`, wrapped as `resolve database path`. Turns `data/patchbay.db` into `/home/you/patchbay/data/patchbay.db`. Two servers started from different directories with the same relative path get different absolute paths and therefore different locks, which is correct: they are different files. The relative-path alias in `database_lock_test.go:44-51` checks the reverse: the relative and absolute spelling of one file share one lock. `Abs` fails only if the working directory cannot be determined.

**L26–L26** — Blank line.

**L27–L28** — The comment explaining the switch below. It names the three cases the code must get right: an existing database may itself be a symlink; a new database does not exist yet, so only its parent directory can be resolved; and a dangling database symlink must fail rather than get its own second lock.

**L29–L29** — `info, err := os.Lstat(absolute)`. `Lstat` describes the path without following a final symlink. With `os.Stat`, a dangling symlink would look like "does not exist", the code would take the second case, and it would create an independent `alias.db.lock` next to the link; `TestDatabaseLockRejectsDanglingSymlink` asserts that no such file appears (`database_lock_test.go:91-93`). With `Lstat`, the dangling link exists, the first case runs, and `EvalSymlinks` reports the missing target.

**L30–L31** — `switch {` and `case err == nil:`. A tagless switch; the path exists in some form.

**L32–L34** — `if info.IsDir() { return nil, fmt.Errorf("database path is a directory") }`. A directory can never be a SQLite file, so fail now with a clear message instead of creating `<dir>.lock` and letting SQLite fail later. `main_test.go:59` passes a temporary directory and expects `lock database` in the error.

**L35–L35** — `absolute, err = filepath.EvalSymlinks(absolute)`. Follows every link in the path to the real file, so `alias.db -> history.db` and `aliasdir/history.db` where `aliasdir -> dir` both become `dir/history.db` and share one lock (`database_lock_test.go:36-59`). It uses `=` so the outer `err` is set and checked at L41. A link whose target is missing makes this return an error wrapping `os.ErrNotExist`, which `database_lock_test.go:88` checks for.

**L36–L39** — `case errors.Is(err, os.ErrNotExist): var directory string; directory, err = filepath.EvalSymlinks(filepath.Dir(absolute)); absolute = filepath.Join(directory, filepath.Base(absolute))`. The database does not exist yet, which is the first start. Only the parent can be resolved, so the code resolves it and re-attaches the file name. `var directory string` followed by `=` is deliberate: writing `directory, err := ...` inside the `case` would declare a new `err` scoped to the case, the outer `err` would stay `nil`, and L41 would miss a failed resolution. This is the shadowing trap from the concepts list, avoided by hand. There is no `default` case, so any other `Lstat` error, for example permission denied, leaves `err` set and falls through to L41.

**L40–L40** — Closing brace of the switch.

**L41–L43** — `if err != nil { return nil, fmt.Errorf("resolve database path: %w", err) }`. One check for both cases. The `%w` keeps `os.ErrNotExist` reachable through `errors.Is`.

**L44–L44** — Blank line.

**L45–L46** — The comment `Keep this file on disk after closing it. Unlinking it could let another process create a different file and acquire a second, independent lock.` A `flock` lock is attached to an inode. If server A deleted `patchbay.db.lock` on exit while server B was still waiting, or if an operator deleted it while A ran, a new server would create a brand-new inode under the same name, lock that, and both would write one database. This is why the README says the empty file stays and must not be deleted while Patchbay runs (`README.md:101-102`) and why `AGENTS.md:96-97` forbids deleting a lock file to bypass ownership. `database_lock_test.go:65-70` asserts the file survives `Close`.

**L47–L50** — `lock, err := os.OpenFile(absolute+".lock", os.O_CREATE|os.O_RDWR, 0o600)`, wrapped as `open database lock file`. The lock file sits beside the database with the suffix `.lock`, so it lands inside the same directory and, in Docker, inside the same named volume (`compose.yaml:9`), the only writable place in a read-only container (`compose.yaml:11`). `O_CREATE` without `O_EXCL` reuses the file across restarts; `database_write_test.go:74-76` pre-creates it to show that an existing lock file needs no directory write permission. Mode `0o600` follows the same owner-only rule the database file uses (`AGENTS.md:108`, `sqlite.go:34`); on an existing file the mode argument is ignored, which preserves its permissions. Because the name is derived from the path, a hard link to the database would get a different `.lock` name and a different lock; the README lists hard links and network filesystems as unsupported for this reason (`README.md:103-105`).

**L51–L51** — `if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {`. `lock.Fd()` exposes the descriptor as a `uintptr`; the conversion to `int` matches `Flock`'s signature. `LOCK_EX` asks for the exclusive lock; `LOCK_NB` says "do not wait, fail instead". Without `LOCK_NB` a second server would hang silently until the first exited, and the operator would see nothing. The lock belongs to this open file description, so even a second `lockDatabase` call inside the same process conflicts, which is what lets `database_lock_test.go:22-27` test exclusion without a second process.

**L52–L52** — `lock.Close()`. The descriptor is useless once the lock failed; closing it avoids leaking a file handle on every rejected start. Its own error is ignored because the function is already returning an error.

**L53–L55** — `if errors.Is(err, syscall.EWOULDBLOCK) { return nil, errDatabaseInUse }`. `EWOULDBLOCK` is the kernel's answer to `LOCK_NB` when another open file holds the lock; on both Linux and macOS it equals `EAGAIN`. Translating it into the sentinel turns a cryptic errno into the sentence operators read at `README.md:96-97`, and gives `main_test.go:147` and `database_lock_test.go:136` something exact to compare against.

**L56–L56** — `return nil, fmt.Errorf("acquire database lock: %w", err)`. Any other `flock` failure, for example a filesystem that does not support locks, is reported with its cause. A network filesystem may land here or, worse, grant the lock without enforcing it, which is why `README.md:104-105` says to keep the database local.

**L57–L57** — Closing brace of the `if`.

**L58–L58** — `return lock, nil`. The caller now owns the open file and, through it, the database. `main.go:70-74` closes it last.

**L59–L59** — Closing brace of `lockDatabase`.

## State, concurrency, and cancellation

**Goroutines.** `main.go` itself owns two: the main goroutine, which runs `runServer` and blocks in the `select` at L132, and the listener goroutine started at L129, which lives inside `ListenAndServe` until shutdown. Everything else is started by the components it wires: `net/http` starts one goroutine per connection; the scheduler starts one waiting goroutine per scheduled workflow (`scheduler.go:91-92`); the runner starts one goroutine per active run (`runner.go:194-198`); `signal.NotifyContext` runs a small goroutine that watches for signals. None of these goroutines share mutable data through `main.go`. The only values `main.go` hands out are `definitions` (read-only after `Load`), `db` (a `*sql.DB`, safe for concurrent use and limited to one connection at `sqlite.go:59`), and the two executor pointers (documented concurrency-safe).

**Mutexes and lock order.** `main.go` holds no mutex. The runner's `mu` (`runner.go:63`) protects admission state: `runs`, `order`, `busyWorkflows`, `activeRuns`, `pending`, and `closed`. The scheduler's `mu` (`scheduler.go:42`) protects `next` and `started`. These two locks are never nested: the scheduler calls `runner.Start` at `scheduler.go:125` while holding nothing, and only takes `s.mu` afterwards at `scheduler.go:140`. The runner holds `mu` during the initial save callback from L107-109 (`runner.go:137-141`) but never during execution or progress saves (`runner.go:246`), which is the rule "Execution and progress/final writes stay outside the history mutex; initial creation currently holds it to serialize admission" (`AGENTS.md:81-82`). SQLite's own file lock plus the single pooled connection serialize the actual database access; a busy database waits up to five seconds (`sqlite.go:51`).

**Contexts.** There are four roots.

| Context | Created | Cancels when | Used by |
| --- | --- | --- | --- |
| `stop` / `ctx` | `main.go:38` | `SIGINT` or `SIGTERM` | `store.Open` (L75), the write check (L87), interruption cleanup (L95), the `select` (L137) |
| runner `ctx` | `runner.go:97` from `Background` | `runner.Close` (`runner.go:179`) | initial saves (`runner.go:139`), step execution (`runner.go:247`), progress saves (`runner.go:233`, `runner.go:269`); final saves use `WithoutCancel` plus a fresh five-second budget (`runner.go:286`, `runner.go:322`) |
| scheduler `ctx` | `scheduler.go:68` from `Background` | `scheduler.Close` (`scheduler.go:99`) | the wait in every loop goroutine (`scheduler.go:117`) |
| `shutdownCtx` | `main.go:141` from `Background` | five seconds after a signal | `server.Shutdown` (L143) |

The runner and scheduler deliberately do not derive from `stop`. If they did, one signal would cancel executors, scheduler timers, and startup work all at the same instant, in no particular order. Instead the signal only wakes the main goroutine, which then closes the scheduler and the runner in sequence (L139-140). The roadmap states the intent: "A submitted background run belongs to the application's lifecycle, so closing the browser or completing the HTTP request must not cancel it" (`docs/roadmap.md:122`). HTTP request contexts are used only for history reads (`api.go:90`, `api.go:104`), never for runs.

**Failure paths, in order of appearance.**

| Fails at | Already created | Released by | Observable result |
| --- | --- | --- | --- |
| L48, L51, L54, L58 | nothing on disk | nothing to release | error message, no `data/` directory (`main_test.go:200-233`) |
| L62 | nothing | nothing | `create database directory` (`main_test.go:60`) |
| L66 | directory | nothing | `lock database ...: database is already in use by another Patchbay instance` (`main_test.go:146-149`) |
| L75 | lock | lock (L70) | `open database ...`; lock reacquirable (`main_test.go:184-198`) |
| L88 | lock, db | db then lock | `check database writeability`; history unchanged (`database_write_test.go:111-123`) |
| L96 | lock, db | db then lock | `mark unfinished runs interrupted`; history unchanged (`main_test.go:150-153`) |
| L129 listener error | lock, db, runner, scheduler, server | server, scheduler, runner, db, lock | `HTTP server failed`; lock reacquirable (`main_test.go:161-166`) |
| L143 shutdown timeout | all | `server.Close` forces connections closed, then the rest | `HTTP shutdown failed`, exit 1, runs already saved |

On every path the lock is released last and the exit code is 1; on the signal path with no timeout the exit code is 0.

**Rules from `AGENTS.md` that this code implements.**

- "Save a new run and its workflow snapshot before acceptance. Failed initial saves must not execute actions or consume capacity. Accepted runs belong to the runner's context, not the originating HTTP request." (`AGENTS.md:83-85`) The closure at L107-109 is that save; the runner calls it under its own context (`runner.go:139`).
- "Keep retries and shutdown cleanup bounded by their existing deadlines." (`AGENTS.md:90`) L139-145 rely on the runner's five-second budgets and add a five-second HTTP deadline.
- "Take the database ownership lock before migrations or interruption cleanup. Stop the runner before closing SQLite and releasing the lock. Do not delete a lock file to bypass another server's ownership." (`AGENTS.md:95-97`) L66 before L75 and L96; the defer order at L70, L80, L115; the comment at `database_lock.go:45-46`.
- "Startup marks leftover queued/running records interrupted; it does not resume actions." (`AGENTS.md:98-99`) L94-103, and nothing after it reads those rows back into the runner.
- "Scheduled starts call `Runner.Start` exactly as the API does. ... Stop the scheduler before the runner." (`AGENTS.md:100-102`) L116 passes `runner.Start`; L121 and L139 stop the scheduler first.
- "Create new database files with mode `0600` before SQLite opens them ... Preserve existing file and directory permissions." (`AGENTS.md:108-109`) L62's `MkdirAll` and the lock file's `0o600` at `database_lock.go:47`; `store.Open` does the database file itself.
- "After opening and recognizing the database, commit a bounded write check before interruption cleanup or HTTP startup." (`AGENTS.md:110-111`) L86-92.
- "Keep the unauthenticated prototype local, host validation enabled, and the JSON requirement on run-start requests." (`AGENTS.md:113-114`) The loopback default at L29 and `hostGuard.Wrap` at L123; the JSON requirement lives in `api.go:51-56`.

## Check yourself

1. `runServer` returns errors and `main` calls `os.Exit(1)`. Why not call `os.Exit` directly where the failure happens?

<details><summary>Answer</summary>`os.Exit` terminates the process without running deferred functions. The lock file, the SQLite handle, the runner, and the HTTP server are all released by `defer` statements inside `runServer` (L70, L80, L115, L121, L127). Returning an error lets those run before `main` exits; the comment at L46 states this. The tests at `main_test.go:161-166` and `main_test.go:193` would fail with a stale lock if `runServer` exited early.</details>

2. The operator presses Ctrl+C while two runs are executing and one is queued. List what happens, in order, until the process exits.

<details><summary>Answer</summary>`stop` is canceled; the `select` takes the L137 case and logs `shutting down`. `scheduler.Close` stops every waiting timer goroutine so no new occurrence can call `Start`. `runner.Close` marks the runner closed, cancels its context (both executors return promptly), finishes the queued run as `canceled` under one five-second budget, lets each active run write its final snapshot with a fresh five-second budget, and waits for all run goroutines. `server.Shutdown` then stops the listener and waits up to five seconds for in-flight requests. `runServer` returns `nil`, and the defers run: `cancelShutdown`, `server.Close`, `scheduler.Close` and `runner.Close` again (no-ops), `db.Close`, `databaseLock.Close`. `main`'s deferred `cancel` runs and the process exits 0.</details>

3. `store.CheckWritable` at L88 fails. What is on disk afterwards, and can a corrected server start against the same file?

<details><summary>Answer</summary>The database file exists with whatever schema `store.Open` committed, the `.lock` file exists, and no run row has changed, because the write check touches only the `user_version` header value and interruption cleanup has not run yet. `runServer` returns `check database writeability: ...`; the deferred `db.Close` and `databaseLock.Close` run, so the `flock` is released. Once the underlying problem (a read-only directory, in the test) is fixed, the next start acquires the same lock file again. `database_write_test.go:111-123` asserts the message, the released lock, and the unchanged history.</details>

4. A user clicks Run, the browser gets its 202, and the user immediately closes the tab. Does the run continue, and which context governs its saves?

<details><summary>Answer</summary>It continues. The HTTP handler calls `runner.Start`, which creates the run under the runner's own context (`runner.go:97`, `runner.go:139`) and returns. The request context ends with the response, but nothing in the run uses it. Progress and final saves go through the closure at L109-111 with a context derived from the runner's context (`runner.go:350`, `runner.go:322`). Only `runner.Close` (a signal, or a listener failure) cancels that context. This is the rule at `AGENTS.md:84-85` and the roadmap's requirement at `docs/roadmap.md:122`.</details>

5. Why is `errorsCh` created with a buffer of one, and what would go wrong with `make(chan error)`?

<details><summary>Answer</summary>On the signal path the `select` takes the `ctx.Done()` case and never receives from `errorsCh`. `server.Shutdown` then makes `ListenAndServe` return `http.ErrServerClosed`, and the goroutine at L129 tries to send it. With a buffer of one the send completes and the goroutine exits. With an unbuffered channel the send would block forever, leaking the goroutine until the process exits. It is harmless in practice because the process is about to exit, but it is still a leak, and the buffer makes the code correct rather than merely lucky.</details>

6. Two operators run `./bin/patchbay -db data/patchbay.db` from the same directory, and a third runs the server against a hard link of the same file. Which of them share the database safely?

<details><summary>Answer</summary>The first two resolve to the same absolute path and the same `data/patchbay.db.lock`; the second one fails at L66 with `database is already in use by another Patchbay instance`. The third resolves to a different path, so `lockDatabase` opens a different `.lock` file and succeeds; both servers then write one SQLite file through two inodes' worth of names, and the lock cannot see it. The README states that hard-link aliases and network filesystems are unsupported for this reason (`README.md:103-105`). Symlinks, by contrast, are resolved by `EvalSymlinks` at `database_lock.go:35-38` and do share one lock.</details>

## Experiments

1. **Weaken the lock and watch exclusion disappear.** In `cmd/server/database_lock.go:51` change `syscall.LOCK_EX` to `syscall.LOCK_SH` (a shared lock, which several holders may take at once). Run `go test ./cmd/server -run TestDatabaseLockExcludesAliasesAndAllowsOtherDatabases`. Expected: the test fails at its first assertion with `new database was not reserved before SQLite creation: <nil>`, because the second `lockDatabase` call now succeeds. Revert the change and rerun; the test passes.

2. **Move a flag check and see the "before touching storage" contract break.** Cut the three lines `main.go:48-50` and paste them directly after `main.go:61`, below `workflow.Load`. Run `go test ./cmd/server -run TestRunServerRejectsInvalidActiveRunLimitBeforeTouchingStorage`. Expected: failure with a message like `limit 0 was not rejected before loading workflows: invalid workflow configuration: read workflows: open missing-workflows: no such file or directory`. The test deliberately points at a missing workflow directory, so once the limit check runs after `Load`, the wrong error surfaces first. Revert.

3. **Hold the lock with a second server.** Run `make build`, then in terminal 1 `./bin/patchbay`, and in terminal 2 `./bin/patchbay -addr 127.0.0.1:8081`. Expected: the second process prints one `patchbay failed` line whose `error` value is `lock database "data/patchbay.db": database is already in use by another Patchbay instance` (the default `slog` handler quotes that value and escapes the inner quotes) and exits with status 1; `ls data` shows `patchbay.db` and the empty `patchbay.db.lock`. Now run `./bin/patchbay -addr 127.0.0.1:8081 -db data/other.db` and it starts, creating `other.db` and `other.db.lock`. Stop both with Ctrl+C; both `.lock` files remain, as `README.md:101-102` says they should.

4. **Compare an orderly shutdown with a crash.** Temporarily raise `timeoutMs` to `30000` in `workflows/http-check.json` so its step waits long enough to observe; the file's default target `192.168.1.50` must be an address that drops packets rather than refusing the connection, or the check returns at once. Run `make build` and `./bin/patchbay`. In another terminal start a run: `curl -i -X POST -H 'Content-Type: application/json' http://127.0.0.1:8080/api/workflows/my-service/runs`. Press Ctrl+C in the server terminal within a few seconds. Expected: `shutting down` is logged and the process exits well before 30 seconds, because the executor observes the canceled context; restart `./bin/patchbay` and note that no `marked unfinished runs interrupted` line appears, and `curl -s http://127.0.0.1:8080/api/runs` shows that run with `"status":"canceled"`. Repeat, but this time kill the server from another terminal with `pkill -9 -x patchbay`. Expected: the next start logs `marked unfinished runs interrupted runs=1`, and the run now reads `"status":"interrupted"` with its step `interrupted`. Restore the `timeoutMs` value afterwards.

## Next

Chapter 4, "Workflow definitions, validation, and loading" ([04-workflow-definitions.md](04-workflow-definitions.md))
