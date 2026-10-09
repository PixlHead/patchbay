# 24. Tests: opening, permissions, migrations, identity, write check

Files: [internal/store/sqlite_test.go](../../internal/store/sqlite_test.go), [internal/store/sqlite_permissions_test.go](../../internal/store/sqlite_permissions_test.go), [internal/store/schema_test.go](../../internal/store/schema_test.go), [internal/store/identity_test.go](../../internal/store/identity_test.go), [internal/store/writable_test.go](../../internal/store/writable_test.go)

Revision: commit 1c4786e

## Role in the system

These five files test the part of startup that runs before any workflow can be saved. In `cmd/server/main.go` the order is fixed: create the data directory (`cmd/server/main.go:62`), take the ownership lock (`cmd/server/main.go:66`, `cmd/server/database_lock.go:18`), open and migrate the database with `store.Open` (`cmd/server/main.go:75`), prove a write can commit with `store.CheckWritable` (`cmd/server/main.go:88`), mark leftover runs interrupted (`cmd/server/main.go:96`), and only then build the runner that will call `store.CreateRun` and `store.UpdateRun` (`cmd/server/main.go:107-111`). The production functions under test are `Open` (`internal/store/sqlite.go:20`), `migrate` (`internal/store/schema.go:56`), `recognizeSchema` with its two helpers (`internal/store/identity.go:12`, `:101`, `:137`), and `CheckWritable` (`internal/store/writable.go:12`). Chapters 8 and 9 walk that production code line by line; this chapter shows what each guard refuses, what it accepts, and what it leaves untouched on disk.

Data comes in as a file path and a context. Data goes out as a `*sql.DB` or an error, plus side effects on disk: a new file with mode 0600, migrated tables, the "PTBY" application marker in the file header, and one committed header write. The tests observe those side effects directly rather than through the API: `os.Stat` for permission bits, `os.ReadFile` and `bytes.Equal` for "nothing changed", `PRAGMA` reads for the header, `sqlite_schema` for the list of objects, and the shared helpers `runFixture` (`internal/store/runs_test.go:145`) and `assertStoredRun` (`internal/store/runs_test.go:32`) to prove a saved run survives each operation field for field. On the path from a browser click to a saved run, these functions are the gate at the start of the path: the `CreateRun` call wired at `cmd/server/main.go:108` can only ever run against a database that passed every check here.

No test here touches a live service, and none replaces SQLite with a fake. Each test gets its own file from `t.TempDir()` and the real `modernc.org/sqlite` driver runs against it. Failure is injected by shaping the file or the process: wrong columns, a foreign marker, a dangling symlink, a umask of zero, a canceled context, `PRAGMA query_only`, or a held connection. That is the style `AGENTS.md:134-137` asks for: "temporary databases, local test servers, and controlled clocks/channels" with "failure injection in tests".

## Concepts you need

- **Internal test packages.** Every file here declares `package store`, so it can read unexported names such as `applicationID`, `schemaVersion`, `initialSchema`, `migrate`, and `unixMilliOrNull`. The price is coupling to private names; the gain is that the tests can build the exact byte layout production expects. [cmd/go: test packages](https://pkg.go.dev/cmd/go#hdr-Test_packages)
- **The `testing` package: `t.Run`, `t.Fatal`, `t.Errorf`, `t.Helper`, `t.TempDir`.** `t.Fatal` stops the test at once; `t.Errorf` records a failure and continues, which `identity_test.go:71` uses so a later check still runs. `t.Helper` makes a failure inside a helper report the caller's line. `t.TempDir` returns a fresh directory that is removed when the test ends, which is how every test gets a private database. [pkg.go.dev/testing](https://pkg.go.dev/testing)
- **Table-driven tests.** A slice of anonymous structs lists cases and a loop runs each one in `t.Run`. The identity test is a 23-row table where each row is one deliberately wrong database. Subtest names have spaces replaced by underscores when you pass them to `-run`. [Go wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)
- **Build constraints.** A `//go:build darwin || linux` line before the package clause tells the compiler to include the file only on those systems. `sqlite_permissions_test.go` needs it because `syscall.Umask` does not exist on Windows. [cmd/go: build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- **Re-running the test binary with `os/exec`.** `os.Args[0]` is the path of the program that is running, which during `go test` is the compiled test binary. `exec.CommandContext` starts it again with chosen arguments and environment, and `CombinedOutput` collects what it prints. The `-test.run` flag limits which tests the child runs. [pkg.go.dev/os/exec](https://pkg.go.dev/os/exec), [cmd/go: testing flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
- **umask and permission bits.** A Unix file mode such as `0o600` means owner read and write, nobody else. The process umask removes bits from every mode a program requests, and it is a property of the whole process, not of one goroutine. `syscall.Umask(new)` sets it and returns the old value. `info.Mode().Perm()` extracts just the nine permission bits from a file's mode. [pkg.go.dev/syscall#Umask](https://pkg.go.dev/syscall#Umask), [pkg.go.dev/io/fs#FileMode](https://pkg.go.dev/io/fs#FileMode)
- **`os.OpenFile` with `O_CREATE|O_EXCL`.** Together these flags mean "create this file, and fail if anything is already at this path". The kernel applies that rule to symlinks too: a link, even a dangling one, counts as "something is there". That is what lets `Open` create new files privately while never touching an existing file or link. [pkg.go.dev/os#OpenFile](https://pkg.go.dev/os#OpenFile), [open(2)](https://man7.org/linux/man-pages/man2/open.2.html)
- **Symlinks and file identity: `os.Symlink`, `os.Readlink`, `os.Stat`, `os.SameFile`.** `Symlink` creates a link, `Readlink` returns where a link points without following it, `Stat` follows links to the real file, and `SameFile` tells whether two `FileInfo` values describe the same file object (same device and inode on Unix). A deleted and recreated file is a different object even at the same path. [pkg.go.dev/os#SameFile](https://pkg.go.dev/os#SameFile)
- **`context` cancellation and deadlines.** A context carries a cancel signal and an optional deadline. `context.WithCancel` plus an immediate `cancel()` gives a context that is already dead; `context.WithTimeout` gives one that dies on its own. Well-behaved functions check `ctx.Err()` early and return `context.Canceled` or `context.DeadlineExceeded`. [pkg.go.dev/context](https://pkg.go.dev/context)
- **Error wrapping: `%w` and `errors.Is`.** `fmt.Errorf("...: %w", err)` keeps `err` reachable inside the new error. `errors.Is(err, target)` walks that chain. The tests check `context.Canceled`, `context.DeadlineExceeded`, and `os.ErrNotExist` this way, never by comparing strings. [pkg.go.dev/errors](https://pkg.go.dev/errors)
- **`defer`, including inside loops.** A deferred call runs when the surrounding function returns, even after `t.Fatal`. A `defer` inside a loop body queues one call per iteration, all of which run at function exit. `*sql.DB.Close` is safe to call twice, so a deferred close after an explicit close is a harmless safety net. [Spec: defer statements](https://go.dev/ref/spec#Defer_statements)
- **`database/sql`: `sql.Open` is lazy, pools, `DB.Conn`.** `sql.Open` validates nothing; the first query or `Ping` opens a real connection. A `*sql.DB` is a pool, and `SetMaxOpenConns(1)` makes it a pool of one. `DB.Conn(ctx)` hands you one dedicated connection until you `Close` it; while you hold the only connection, every other request on that `*sql.DB` waits. `ExecContext` and `QueryRowContext(...).Scan` are the two call shapes used here. [pkg.go.dev/database/sql](https://pkg.go.dev/database/sql), [DB.Conn](https://pkg.go.dev/database/sql#DB.Conn)
- **`database/sql` transactions.** `BeginTx` starts one, `Commit` makes it permanent, `Rollback` discards it. The production pattern `defer tx.Rollback()` right after `BeginTx` means every early return discards the work; after a successful `Commit`, the deferred `Rollback` is a no-op. The migration tests exist to prove that pattern holds. [pkg.go.dev/database/sql#Tx](https://pkg.go.dev/database/sql#Tx)
- **The `modernc.org/sqlite` DSN.** `Open` builds a `file:` URI with a query string. `mode=rw` is a standard SQLite URI parameter meaning "open for read and write but never create". `_pragma=...` is a driver parameter that runs a pragma on every new connection. The tests that call `sql.Open("sqlite", path)` with a bare path skip all of that on purpose, to build fixtures the way an old build or a foreign program would. [pkg.go.dev/modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), [SQLite URI filenames](https://sqlite.org/uri.html)
- **SQLite header pragmas: `application_id` and `user_version`.** Both are 32-bit integers stored in the 100-byte header at the start of the file. `user_version` is Patchbay's schema version (1, 2, or 3); `application_id` is the "PTBY" marker. Reading them is free; setting one is a real write that dirties page 1. [PRAGMA application_id](https://sqlite.org/pragma.html#pragma_application_id), [PRAGMA user_version](https://sqlite.org/pragma.html#pragma_user_version), [file format: header](https://sqlite.org/fileformat.html#the_database_header)
- **`PRAGMA data_version` and `PRAGMA query_only`.** `data_version` is a per-connection counter that changes only when a different connection commits; it is a clean "did anyone really commit?" detector. `query_only = ON` makes a connection refuse every write with a read-only error, which is a portable stand-in for a read-only disk. [PRAGMA data_version](https://sqlite.org/pragma.html#pragma_data_version), [PRAGMA query_only](https://sqlite.org/pragma.html#pragma_query_only)
- **`sqlite_schema` and the pragma table-valued functions.** `sqlite_schema` is the table that lists every table, index, view, and trigger with its `type`, `name`, `tbl_name`, and the `sql` that created it. `pragma_table_xinfo`, `pragma_index_list`, `pragma_index_info`, `pragma_table_list`, and `pragma_foreign_key_list` expose column, index, and foreign-key details as queryable rows; `table_xinfo` also reports hidden and generated columns, which `table_info` omits. [sqlite_schema](https://sqlite.org/schematab.html), [pragma functions](https://sqlite.org/pragma.html#pragfunc), [generated columns](https://sqlite.org/gencol.html)
- **`GLOB` versus `LIKE`.** In SQLite, `GLOB` uses shell wildcards (`*`, `?`) and treats `_` as a plain underscore; `LIKE` uses `%` and `_`, where `_` matches any single character. Production filters SQLite's own objects with `NOT GLOB 'sqlite_*'`, and one identity row exists only to prove that choice. [SQLite LIKE and GLOB](https://sqlite.org/lang_expr.html#like)
- **`ANALYZE` and `sqlite_stat` tables.** `ANALYZE` gathers index statistics into tables named `sqlite_stat1` and friends. They are SQLite's, not Patchbay's, and recognition must ignore them like it ignores the `sqlite_autoindex_*` indexes that back primary keys and `UNIQUE` constraints. [ANALYZE](https://sqlite.org/lang_analyze.html)
- **Shaping fixtures with `strings.Replace` and checking files with `bytes.Equal`.** `strings.Replace(s, old, new, 1)` returns `s` with the first `old` swapped for `new`; the identity test uses it to derive "almost right" schemas from the real constants. `os.ReadFile` plus `bytes.Equal` compares two whole files. [pkg.go.dev/strings#Replace](https://pkg.go.dev/strings#Replace), [pkg.go.dev/bytes#Equal](https://pkg.go.dev/bytes#Equal)
- **`for range N` and `for i := range N`.** Since Go 1.22, ranging over an integer repeats N times; with a variable you also get 0, 1, ... The two "open twice" tests use it. [Spec: for statements with range clause](https://go.dev/ref/spec#For_range)
- **`any` and SQL NULL.** A variable of type `any` (alias for `interface{}`) can hold a value or `nil`. Passing `nil` as a query argument stores SQL NULL; passing a string stores text. `unixMilliOrNull` (`internal/store/runs.go:127`) and the step-output code use this to distinguish "no value" from a value. [Tour: interfaces](https://go.dev/tour/methods/14)

## Walkthrough

### internal/store/sqlite_test.go

**L1–L8** — `package store` makes this an internal test file, which is why L21 can compare against the unexported constant `applicationID` (`internal/store/schema.go:12`). The imports are the minimum: `context` because `Open` takes one (`internal/store/sqlite.go:20`), `path/filepath` to build a path inside a temporary directory, and `testing`. There is no `os` and no `database/sql` import; this file uses only `Open` and the `*sql.DB` it returns.

**L9–L16** — `TestOpenPersistsAcrossReopen` (L9–L50) proves that what one `Open` writes, a second `Open` on the same path reads back, and that a brand-new file receives the Patchbay marker. `t.TempDir()` supplies a directory the testing package deletes afterward. The filename `history ?#.db` is chosen to contain a space, `?`, and `#`. `Open` builds a `file:` URI (`internal/store/sqlite.go:43-44`), and in a URI `?` begins the query and `#` begins a fragment; the `url.URL` encoding keeps those characters inside the filename so the `mode=rw` and `_pragma` settings in the query (`internal/store/sqlite.go:45-52`) are not swallowed. `Open` creates the file (`internal/store/sqlite.go:34`), pings it (`:63`), and migrates it (`:67`). `defer db.Close()` covers every `t.Fatal` below; `*sql.DB.Close` is idempotent, so the explicit close at L34 is safe as well.

**L17–L23** — Read `PRAGMA application_id` from the header into `marker` and compare it with `applicationID` (`internal/store/schema.go:12`, 0x50544259, the bytes "PTBY"). On a fresh file `migrate` sees `owner == 0` and writes the marker at `internal/store/schema.go:109-113`, inside the same transaction as `initialSchema`. That is the "assigning Patchbay's application ID" half of `AGENTS.md:105-107`. If the marker were missing, every later `Open` would treat the file as an unmarked legacy database and run adoption again (`internal/store/schema.go:84`), and a future build that trusted the marker alone would refuse its own file.

**L24–L33** — Two raw `INSERT` statements that bypass `CreateRun`, so this test depends on the schema and nothing else. The `runs` insert names six columns: `started_at` and `finished_at` are nullable (`internal/store/schema.go:24-25`) and `error` has `DEFAULT ''` from `runErrorMigration` (`internal/store/schema.go:47`), so leaving them out is legal. The `run_steps` insert relies on the same defaults (`internal/store/schema.go:34-37`). The order matters: `run_steps.run_id` references `runs(id)` (`internal/store/schema.go:29`) and `Open` turns on `foreign_keys(1)` for every connection (`internal/store/sqlite.go:50`), so inserting the step before its run would fail.

**L34–L37** — Close the first handle and check the error. Each statement above already committed in autocommit mode, but a close error would mean the connection was in a bad state, and the reopen below should not run on a broken fixture.

**L38–L42** — Reopen the same path. This time `os.OpenFile` with `O_EXCL` fails with `ErrExist` and `Open` moves on without truncating (`internal/store/sqlite.go:34-41`). `migrate` reads owner PTBY and version 3, runs `recognizeSchema` with `legacy == false` (`internal/store/schema.go:84`), and returns at `internal/store/schema.go:87-89` without writing anything.

**L43–L50** — Read the step row back by `run_id`. A status of `pending` proves two things at once: the rows committed before the first close, and the second `Open` neither recreated the tables nor rewrote the rows. `README.md:566` states the user-facing promise: "Restarting with the same database preserves saved history". L50 closes the function.

**L51–L56** — `TestOpenRejectsUnavailablePaths` (L52–L70) is a two-row table of paths that must fail. Row one is the empty string, which `Open` refuses before touching the filesystem (`internal/store/sqlite.go:21-23`). Row two is a file inside a directory that does not exist. `Open` never calls `MkdirAll`; the server does that at `cmd/server/main.go:62`, and the doc comment at `internal/store/sqlite.go:17` says "Its parent directory must already exist". So `os.OpenFile` fails with a not-found error, which is not `ErrExist`, and `internal/store/sqlite.go:39-41` returns "create database file".

**L57–L61** — Each row is a subtest. `db` is closed only if it is non-nil, because a failed `Open` must return `nil` (the assertion below checks exactly that) and calling `Close` on a nil `*sql.DB` would panic and hide the real failure.

**L62–L70** — Two assertions: an error came back, and no handle came with it. The second matters because `cmd/server/main.go:75-78` returns on error without closing anything; a non-nil handle on the error path would leak an open file. Both failure returns, `internal/store/sqlite.go:22` and `:40`, are `nil, err`. L68–L70 close the subtest, loop, and function.

### internal/store/sqlite_permissions_test.go

**L1–L2** — The build constraint: this file compiles only on Darwin and Linux. `syscall.Umask` at L19 does not exist on Windows, and POSIX permission bits are what the file checks. `README.md:86` scopes the promise the same way, "On Linux and macOS". Without the constraint, `go test ./...` would fail to compile the package on Windows.

**L3–L15** — Package clause and imports. `errors` is for `errors.Is` against `os.ErrNotExist` and `context.Canceled`; `os` supplies `Mkdir`, `Chmod`, `Stat`, `Symlink`, `Readlink`, `SameFile`, `Getenv`, `Environ`, and `Args`; `os/exec` runs the test binary again; `syscall` supplies `Umask`; `time` is for the child's timeout.

**L16–L18** — `TestOpenCreatesPrivateDatabase` (L16–L49) proves `AGENTS.md:108` ("Create new database files with mode `0600` before SQLite opens them") under the weakest possible umask. The comment at L17 names the design problem: umask belongs to the whole process, so changing it in the parent would change the mode of every file any other test creates meanwhile, including `t.TempDir()` directories. The function therefore has two roles, chosen by an environment variable. When `PATCHBAY_PERMISSIONS_TEST_DATABASE` is set, this process is the child, and the value is the path it should create.

**L19–L20** — In the child, set umask to zero and restore the previous value on return. With umask 0 the kernel removes no bits: a file requested at 0600 comes out 0600, and a file requested at 0644 (SQLite's own default when it creates files) or 0666 would come out exactly that. This is the only setting under which a wrong mode at `internal/store/sqlite.go:34` cannot hide. Under a developer's usual 022 umask a request for 0666 would already become 0644, and under 077 it would become 0600 and the test would pass for the wrong reason.

**L21–L29** — The child calls the real `Open`, which creates the file at `internal/store/sqlite.go:34` with `os.O_RDWR|os.O_CREATE|os.O_EXCL` and mode `0o600`, then migrates it. It closes the handle and returns at L28, so it never reaches the spawning code at L31–L46. The environment variable is both the input and the guard against spawning a third process.

**L30–L34** — The parent branch starts here. It creates a subdirectory named `shared` with 0755. A world-readable directory is needed so the test can later prove `Open` did not make it more private; a 0700 temporary directory could not reveal a wrongful `Chmod` to 0700.

**L35–L38** — `os.Mkdir` is itself subject to the parent's umask, so an explicit `Chmod` pins the directory at 0755 whatever the environment. The comment says so. Without it, a developer running with umask 077 would get a 0700 directory and L48 would fail for a reason unrelated to `Open`.

**L39–L41** — Build the child's database path and a ten-second context. `exec.CommandContext` kills the child when the context ends, so a hung child cannot hang the whole suite.

**L42–L43** — `os.Args[0]` is the running test binary. Running it with `-test.run=^TestOpenCreatesPrivateDatabase$` executes only this function; the `^` and `$` anchors stop the regular expression from matching a longer test name that happens to contain this one. `cmd.Env` is the parent's environment plus the variable, which sends the child down the L18 branch.

**L44–L46** — `CombinedOutput` runs the child and returns everything it printed. A non-zero exit, which is what the child's `t.Fatal` produces, becomes `err`, and the captured output goes into the failure message so the child's reason is visible in the parent's log.

**L47–L49** — Two assertions through the helper at L161. The file must be exactly 0600: that exercises the mode argument at `internal/store/sqlite.go:34` and the fact that creation happens before SQLite ever sees the path. The directory must still be 0755: `Open` never calls `Chmod` (comment at `internal/store/sqlite.go:32-33`, "never truncate or chmod them here"), which is the "Preserve existing file and directory permissions" half of `AGENTS.md:109`. L49 closes the function.

**L50–L60** — `TestOpenPreservesExistingDatabasePermissionsAndHistory` (L51–L121) is a three-row table: a 0640 file, a 0644 file, and a 0640 file reached through a symlink. `README.md:88-89` promises that "reopening an older database does not make its existing permissions more private". The symlink row checks that `Open` follows the link instead of replacing it with a regular file.

**L61–L68** — Each row gets its own temporary directory, and `os.Stat` records the directory's starting mode before anything happens. L106 compares against that recorded value instead of a constant, so the test does not need to know what mode `t.TempDir` uses.

**L69–L73** — Create the database with `Open`, so the file starts at 0600 with the current schema and marker.

**L74–L78** — Save a real run with `CreateRun` (`internal/store/runs.go:17`) from the shared fixture at `internal/store/runs_test.go:145`. Setting `original.Error` gives the run-level `error` column (`internal/store/schema.go:47`) a non-default value, so the comparison at L101 checks something a rewrite would lose.

**L79–L84** — Close, then `Chmod` the file to the row's mode. The chmod happens after close so the fixture models a user who widened permissions on a file Patchbay was not holding.

**L85–L88** — `os.Stat` after the chmod. `before` carries the file's identity for the `SameFile` check at L111.

**L89–L95** — For the symlink row, create `alias.db` pointing at `history.db` and open through the alias. `Open` calls `filepath.Abs` (`internal/store/sqlite.go:24`), which does not resolve links, so the DSN names the alias and SQLite follows it. The `O_EXCL` create at `internal/store/sqlite.go:34` fails with "exists" for a symlink whatever its target, which is what lets the code treat `ErrExist` as "leave it alone" (`internal/store/sqlite.go:32-33`).

**L96–L101** — Reopen and compare the entire run, including the error text, with `assertStoredRun` (`internal/store/runs_test.go:32`). For an existing current-version file, `migrate` recognizes it and returns without writing (`internal/store/schema.go:84-89`).

**L102–L106** — Close before inspecting the file, so any write SQLite makes on close has already happened. The file keeps the row's mode and the directory keeps its recorded mode. `Open` passes a mode only when creating, and only on the `O_EXCL` success path (`internal/store/sqlite.go:34-38`).

**L107–L113** — `os.Stat` again and `os.SameFile`. Equal means the same file object: `Open` did not delete and recreate the database, which would have reset the mode to 0600 and, on a crash in between, lost the history.

**L114–L121** — For the symlink row, `os.Readlink` confirms the alias is still a link to the same target. A naive "remove and create" implementation would have turned the link into a plain file. L119–L121 close the subtest, loop, and function.

**L122–L129** — `TestOpenDoesNotCreateThroughDanglingDatabaseSymlink` (L123–L143) builds a link whose target does not exist. `README.md:90-91` promises that "a missing symlink target is rejected rather than created".

**L130–L136** — `Open` must fail and return no handle. The mechanism has two parts. `os.OpenFile` with `O_EXCL` on a symlink path fails with "exists" even though the target is missing, so `Open` moves on (`internal/store/sqlite.go:39`) and leaves the open to SQLite. The DSN carries `mode=rw` (`internal/store/sqlite.go:46-48`), which tells SQLite to open without creating, so `PingContext` fails (`internal/store/sqlite.go:63-66`). Without `mode=rw`, SQLite's default flags would create `missing.db` through the link, with SQLite's default permissions rather than 0600.

**L137–L143** — Two more facts: the link itself is unchanged, and the target still does not exist (`errors.Is(err, os.ErrNotExist)`). The second assertion is what pins `mode=rw`; the first guards against an implementation that "repairs" a dangling link by replacing it. L143 closes the function.

**L144–L148** — `TestOpenWithCanceledContextDoesNotCreateDatabase` (L145–L159) cancels a context before calling `Open`. That happens for real when a SIGINT arrives in the short window between `cmd/server/main.go:38` and `cmd/server/main.go:75`.

**L149–L155** — `Open` must return an error that `errors.Is` recognizes as `context.Canceled`, and no handle. `internal/store/sqlite.go:28-30` checks `ctx.Err()` and wraps it with `%w` before the `os.OpenFile` at `:34`. Without that early check, `PingContext` at `internal/store/sqlite.go:63` would still report the cancellation, but only after `:34` had created an empty file.

**L156–L159** — The file must not exist. This is the observable difference between "checked the context first" and "checked it after creating the file". `README.md:91-92` tolerates a private file left behind by a failed initialization, but a start that was canceled before doing anything should leave nothing. L159 closes the function.

**L160–L170** — `assertFilePermissions` stats a path and compares only the permission bits. `Mode().Perm()` drops the type bits (directory, symlink), so the same helper works for the file and its directory. `t.Helper()` makes a failure point at the caller's line. `%04o` prints octal padded to four digits, matching how modes are written in the source. L170 closes the file.

### internal/store/schema_test.go

**L1–L10** — Imports. `database/sql` appears because these tests open files with the raw driver (`sql.Open("sqlite", path)`) to build legacy fixtures without `Open`'s guards. `encoding/json` encodes the fixture the same way `CreateRun` does. `context`, `path/filepath`, and `testing` as before.

**L11–L18** — `TestOpenUpgradesVersionOneWithoutChangingHistory` (L11–L82). The raw `sql.Open` is the point: it has no `mode=rw`, no pragmas, and no migration, so the test can write exactly the layout an old Patchbay build left behind. `README.md:587-588` states the promise: "An existing version-1 database gains a `runs.error` text column with an empty default; its history is preserved." `defer legacy.Close()` is a safety net for the fatals below; the explicit close is at L52.

**L19–L21** — Execute `initialSchema` (`internal/store/schema.go:16-43`) as one multi-statement script. It ends with `PRAGMA user_version = 1`, so the file now looks like a version-1 database with no application marker. The constant is shared with production, which is why `internal/store/schema.go:14` says "Keep this first migration unchanged": it is both the version-1 upgrade step and the only faithful version-1 fixture.

**L22–L26** — Take the shared fixture and encode its definition exactly as `CreateRun` does at `internal/store/runs.go:18`.

**L27–L34** — Insert the run row by hand. The comment says why: `CreateRun` (`internal/store/runs.go:29-33`) writes the `error` column, which version 1 does not have. The column list stops at `finished_at`. `unixMilliOrNull` (`internal/store/runs.go:127`) converts the times the way production does, so the stored values are the ones `assertStoredRun` expects.

**L35–L51** — Insert each step row, mirroring `CreateRun` at `internal/store/runs.go:38-56`: a `nil` output becomes SQL NULL, a non-nil output becomes JSON text in an `any` variable. `run_steps.error` existed from version 1 (`internal/store/schema.go:37`), so the step insert can include it.

**L52–L55** — Close the raw handle, so `Open` below is the only connection, and a blank line.

**L56–L62** — Open twice in a `for range 2` loop. The comment states the contract: the first open upgrades version 1, the second must find a recognized current database and leave it alone. On the first pass `migrate` reads owner 0 and version 1, recognizes the version-1 layout with `legacy == true` (`internal/store/schema.go:84`; `internal/store/identity.go:57-59` expects no `error` column on `runs` for version 1), then applies `runErrorMigration` (`internal/store/schema.go:97-101`), `tcpCheckMigration` (`:102-106`), and the marker (`:109-113`) in one transaction. On the second pass it returns at `internal/store/schema.go:87-89`. The `defer db.Close()` inside the loop queues one close per iteration; both run at function exit after the explicit close at L78 already ran, which is harmless because `Close` is idempotent.

**L63–L69** — `user_version` must now equal `schemaVersion` (3, `internal/store/schema.go:9`). `AGENTS.md:103-104`: "Introduce deliberate versioned migrations when stored structure changes."

**L70–L75** — `application_id` must be PTBY. This is the success side of `AGENTS.md:106-107`, "recognize supported v1–v3 layouts before assigning Patchbay's application ID".

**L76–L82** — `assertStoredRun` reads the whole run back and compares every field: the `error` column, which the fixture never set and the migration defaulted to the empty string (`internal/store/schema.go:47`), the definition JSON, the timestamps, and each step's output. On the second pass it proves the no-op open did not touch a row. Close, then L80–L82 close the loop and function.

**L83–L90** — `TestMigrateVersionOneConflictPreservesVersionAndData` (L84–L116) calls `migrate` directly on a raw handle. It and the next test are the two in this chapter that do not go through `Open`, which isolates the migration transaction from file creation.

**L91–L98** — The fixture is version 1 plus an `error` column that someone else added, with the default `'keep'` and one row. The comment: it "must not be overwritten or marked migrated". Before recognition existed, `runErrorMigration`'s `ALTER TABLE` would have failed with a duplicate-column error; now `recognizeColumns` refuses earlier, because the table has nine columns where version 1 expects eight (`internal/store/identity.go:123-124`, reached through `internal/store/schema.go:84-86`).

**L99–L101** — `migrate` must return an error.

**L102–L112** — Read `user_version`, `application_id`, and the row's `error` text through the same handle.

**L113–L116** — All three are unchanged: version 1, marker 0, text `keep`. The transaction begun at `internal/store/schema.go:57` was discarded by the deferred `tx.Rollback()` at `:61`, and in fact nothing had been written, because recognition runs before the first `Exec` at `:93`. This is `AGENTS.md:105`, "Verify database identity before migration". L116 closes the function.

**L117–L125** — `TestMigrateLeavesUnsupportedOrConflictingSchemasUnchanged` (L118–L154) has two rows. "newer version" sets `user_version = 4` on an otherwise empty file. "table conflict" creates a `run_steps` table of the wrong shape at version 0. Each row records the version it expects to still see afterward.

**L126–L135** — Raw open and setup per row.

**L136–L138** — `migrate` must refuse. Row one hits `internal/store/schema.go:73-75` ("unsupported database schema version 4"), the guard that keeps an older build from opening a newer file (`README.md:596-598`). Row two hits `internal/store/schema.go:76-83`: version 0 with any object present is "not empty and unmarked".

**L139–L145** — `user_version` is unchanged (4 and 0). A migration that wrote the version first and failed later would leave a lie in the header, and the next start would skip steps that never ran.

**L146–L154** — No `runs` table exists. For row two this is the interesting assertion: had `migrate` tried `initialSchema`, `CREATE TABLE runs` would succeed and `CREATE TABLE run_steps` would fail, and only the rollback at `internal/store/schema.go:61` would remove `runs`. Counting in `sqlite_schema` proves that either the early refusal or the rollback held. L152–L154 close the subtest, loop, and function.

### internal/store/identity_test.go

**L1–L12** — Imports. `bytes` is for the byte-for-byte file comparison, `os` for `ReadFile`, and `strings` for `strings.Replace`, which edits the production schema constants into near-miss fixtures.

**L13–L21** — `TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles` (L13–L84). The constants are the Patchbay marker as SQL, a foreign marker, and one extra object of each kind: a table with a row, a view, an index, and a trigger. `versionTwoSchema` and `currentSchema` are built from the real constants (`internal/store/schema.go:16`, `:46`, `:54`), so every fixture is "the real layout plus one deliberate difference". Every row must be refused, and the file must be byte-identical afterward, which is `README.md:613-615`: a bad file "prevents startup before Patchbay changes the database".

**L22–L25** — The table begins with three version-0 rows: no marker, `user_version` 0, but not empty. Row 1 holds a user table. Row 2 holds a table named `sqliteXnotes`. SQLite's own objects are named `sqlite_*`, and `internal/store/schema.go:78` filters them with `GLOB`, where `_` is a literal underscore; with `LIKE 'sqlite_%'` the `_` would match the `X` and this table would wrongly pass as internal. Row 3 holds only a view, to show that views count as objects. All three are refused at `internal/store/schema.go:81-83`.

**L26–L27** — A foreign `application_id`, once on an empty file and once on a perfect current layout. Both are refused at `internal/store/schema.go:67-69`, before the version is even read. The marker check comes first because a matching layout under another application's marker is still another application's file.

**L28–L29** — Version 1 and version 2 with both tables present but the wrong columns. `recognizeSchema` finds the tables (`internal/store/identity.go:41-43` passes) and `recognizeColumns` rejects the first column (`internal/store/identity.go:123-124`): `id` is declared without `NOT NULL` or `PRIMARY KEY`.

**L30–L31** — Version-1 fixtures that are one constraint short. Without the primary key, `pk` is 0 instead of 1 for `id`, caught at `internal/store/identity.go:123` against the expectation at `:48`. Without `UNIQUE (run_id, position)`, the index query at `internal/store/identity.go:77-89` finds no non-partial unique index on exactly those two columns.

**L32–L35** — Version-2 fixtures. No foreign key: `recognizeForeignKeys` counts zero (`internal/store/identity.go:165-167`). `ON DELETE RESTRICT` instead of `CASCADE`: `internal/store/identity.go:157-159`. `BLOB` instead of `TEXT`: `internal/store/identity.go:123`. A nullable `workflow_name`: `notNull` 0 instead of 1, again `:123`.

**L36–L37** — Two ways the `error` column that version 2 adds can be wrong. A default of `'keep'` fails against `internal/store/identity.go:58`, where the expected default SQL text is `''`. A generated column reports `hidden` 2 from `pragma_table_xinfo` (`internal/store/identity.go:111-112`) where 0 is expected; the plain `table_info` pragma omits generated columns, which is why production uses `xinfo`.

**L38–L41** — An unmarked version-2 file with one extra object of each kind. For an unmarked file `legacy` is true, so `internal/store/identity.go:28` does not take the index-and-trigger allowance, and every extra object lands at `internal/store/identity.go:31-32`. `README.md:607`: "Extra user-defined schema objects prevent adoption of an unmarked file".

**L42–L46** — Marked current files that are still wrong. Only a `runs` table: `internal/store/identity.go:41-43`. Missing uniqueness: `internal/store/identity.go:87-89`. An extra table or view: `internal/store/identity.go:31-32`, because the allowance at `:28` covers only indexes and triggers on the known tables. `docs/roadmap.md:107` records that keeping this full layout check for marked files is a choice to revisit at the first migration that adds a table. L46 closes the table and opens the loop body.

**L47–L59** — Per row: open the path with the raw driver, run the setup script, close. The raw driver creates the file and writes whatever the script says, with none of `Open`'s checks, which is exactly what a foreign program or an old build would do.

**L60–L63** — Read every byte of the file. This is the "before" image.

**L64–L72** — Call the real `Open`. Close any handle that came back, then assert with `t.Errorf` rather than `t.Fatal`. That choice lets the byte comparison still run for a row that was wrongly accepted, so the log says whether the wrong acceptance also wrote to the file.

**L73–L84** — Read the file again and compare with `bytes.Equal`. The comment at L77–L78 explains why this is strong: with no connection open, the file holds the schema, every row, and the 100-byte header where `user_version` and `application_id` live, so one comparison covers all of them. Three production facts make it hold. `Open` never truncates or chmods an existing file (`internal/store/sqlite.go:32-41`). Every check in `migrate` runs inside the transaction begun at `internal/store/schema.go:57` and the deferred `Rollback` at `:61` discards it. And `recognizeSchema` only reads. L82–L84 close the subtest, loop, and function.

**L85–L96** — `TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory` (L86–L155) is the acceptance side: a real version-2 file from before the marker existed. Raw open, then `initialSchema+runErrorMigration`, so the file has the `error` column and `user_version` 2 and no marker.

**L97–L101** — Save a run with the real `CreateRun`; the `error` column now exists, so the production insert works. The long run-level error text means the preservation claim covers that column with a non-default value.

**L102–L106** — `ANALYZE` creates `sqlite_stat1` and related statistics tables. The comment says why: SQLite's own statistics and automatic indexes must not make a Patchbay file look foreign. `internal/store/identity.go:13-14` and `internal/store/schema.go:78` filter `sqlite_*` names, which also covers the `sqlite_autoindex_*` indexes that back the primary keys and the `UNIQUE` constraint. `README.md:608` states the allowance.

**L107–L116** — Confirm the fixture is unmarked (`application_id` 0), so the test cannot pass by accident on a file that was already adopted. Close the raw handle.

**L117–L123** — Open twice with `for attempt := range 2`. First pass: owner 0, version 2, `recognizeSchema(legacy == true)` passes, then `tcpCheckMigration` (`internal/store/schema.go:102-106`) and the marker (`:109-113`) commit together, as the comment at `:107-108` requires. Second pass: owner PTBY, version 3, early return at `internal/store/schema.go:87-89`.

**L124–L133** — After each open, marker and version must be PTBY and 3. The message includes `attempt+1` so a failure names which open regressed.

**L134–L141** — `assertStoredRun` after each open. On the first pass, also create an index and a trigger on `runs`. The comment at L136–L137 names the reason: a marked file may carry diagnostic indexes and the triggers that other tests use to force save failures, such as `internal/store/run_interruptions_test.go:87-90` and `internal/httpapi/history_test.go:82-86`.

**L142–L150** — On the second pass, count those two objects in `sqlite_schema`. Both must remain: `internal/store/identity.go:28-30` skips an index or trigger whose `tbl_name` is a known table when the file is marked, and `migrate` writes nothing on a current file. `README.md:612-613`: "additional indexes and triggers on Patchbay's tables are allowed".

**L151–L155** — Close the handle; L153–L155 close the loop and function.

### internal/store/writable_test.go

**L1–L11** — Imports. `database/sql` is for a second, independent handle and a pinned `*sql.Conn`; `errors` for `errors.Is`; `time` for the 20 ms deadline.

**L12–L18** — `TestCheckWritableCommitsWithoutChangingSchemaOrHistory` (L12–L106) runs twice, on an empty database and on one holding a saved run, because `internal/store/writable.go:14` claims the check works "even when history is empty" and `AGENTS.md:111-112` requires it to "Preserve schema metadata and run history". The subtest name is derived from the boolean.

**L19–L25** — Open a fresh database the normal way.

**L26–L32** — Optionally save the fixture run, with a run-level error so the `error` column has a non-default value that a careless write would lose.

**L33–L38** — Capture the schema as one string: every `sql` entry in `sqlite_schema`, ordered by name and joined with `;`. This is the "before" image of the schema metadata. The query is a constant so the "after" read at L85 is guaranteed to be the same query.

**L39–L44** — A second `sql.Open` on the same path: an observer with its own connection pool, separate from the single connection `Open` configured at `internal/store/sqlite.go:59-60`.

**L45–L49** — Pin one `*sql.Conn` from the observer. `PRAGMA data_version` is per connection; reading it through the pool could use a different connection each time and compare two unrelated counters.

**L50–L55** — Read `data_version` on the pinned connection. The comment at L50–L51 is the design of the test: this value changes on one connection only when another connection commits. A probe that only reads, or that writes inside a transaction and rolls back, leaves it unchanged. That makes `data_version` a detector for "a real commit happened" without inspecting what was written.

**L56–L58** — Run the production check (`internal/store/writable.go:17`): `PRAGMA main.user_version = 3`, the value the header already holds.

**L59–L64** — `data_version` must have changed. This is the assertion behind the comment at `internal/store/writable.go:13-14`: SQLite marks page 1 dirty when a header pragma is set, even to its current value, so the statement goes through the journal and the commit path.

**L65–L67** — Close the checked handle.

**L68–L73** — Reopen with `Open`. This means the file must still pass marker, version, and layout recognition; a write check that damaged the header would fail here, before any of the assertions below.

**L74–L83** — Version still 3, marker still PTBY: the check wrote the current version, not some other number, and did not touch `application_id`.

**L84–L90** — Schema text identical to the "before" image: no table, index, view, or trigger was created or dropped.

**L91–L95** — Count runs and steps in one query.

**L96–L106** — In the populated case there is exactly one run with the fixture's step count, and `assertStoredRun` shows every field unchanged. In the empty case there are still zero rows. L104–L106 close the subtest, loop, and function.

**L107–L114** — `TestCheckWritableRejectsQueryOnlyConnection` (L108–L128) injects the "cannot commit" failure. The comment at `cmd/server/main.go:86` names the scenario, "Opening an existing database can succeed even when it cannot commit writes", for example on a full or read-only disk, but `chmod` is not a reliable way to simulate it, since a root user ignores file modes, so the test uses SQLite's own switch.

**L115–L118** — Save a run so the test can show reads keep working while writes are refused.

**L119–L122** — `PRAGMA query_only = ON`. The comment at L119 is the reason it works: `Open` keeps one connection (`internal/store/sqlite.go:59-60`), so a connection-scoped pragma set through `db` applies to the very connection the probe will use. With a larger pool the pragma might land on a connection the check never sees.

**L123–L128** — `assertStoredRun` still succeeds (reads are allowed). `CheckWritable` must fail: `internal/store/writable.go:17-18` returns "commit database write check" wrapping SQLite's read-only error. The history is still intact afterward. At startup this failure becomes "check database writeability" at `cmd/server/main.go:91`, and the deferred close and lock release run (`README.md:505-507`). L128 closes the function.

**L129–L135** — `TestCheckWritableHonorsContext` (L130–L159) proves the doc comment at `internal/store/writable.go:11`: "The caller supplies a context deadline for the entire check". Open normally.

**L136–L140** — A context canceled before the call. `database/sql` checks `ctx.Done()` before taking a connection, so `ExecContext` returns `context.Canceled`, and the `%w` at `internal/store/writable.go:18` lets `errors.Is` find it.

**L141–L147** — Hold the pool's only connection with `db.Conn`. The comment at L141–L142 explains the choice: with the pool exhausted, the next `ExecContext` must wait for a connection, and `database/sql` gives up as soon as the caller's context ends. Simulating a busy database with a second writer would instead depend on SQLite's `busy_timeout(5000)` sleep loop (`internal/store/sqlite.go:51`), which is slow and timing-sensitive.

**L148–L152** — A 20 ms deadline. The check must return an error wrapping `context.DeadlineExceeded` rather than block. This is what makes the five-second budget at `cmd/server/main.go:87-89` meaningful: without context propagation, a stuck database would hang startup forever and the ownership lock would never be released.

**L153–L159** — Release the connection; the same check now succeeds. That confirms the earlier failure was the wait, not a broken database. L159 closes the file.

## What these tests prove

| test function | production rule it proves | how it injects failure or controls time | test file:line |
| --- | --- | --- | --- |
| `TestOpenPersistsAcrossReopen` | A new file gets the PTBY marker (`internal/store/schema.go:109-113`); a URI-hostile filename is encoded (`internal/store/sqlite.go:43-44`); reopening a current file writes nothing and keeps rows (`internal/store/schema.go:87-89`, `internal/store/sqlite.go:34-41`) | Temporary file named `history ?#.db`; raw inserts; close and reopen | `internal/store/sqlite_test.go:9` |
| `TestOpenRejectsUnavailablePaths` | Empty path refused before I/O (`internal/store/sqlite.go:21-23`); missing parent refused with no handle (`internal/store/sqlite.go:39-41`) | Table of bad paths inside `t.TempDir()` | `internal/store/sqlite_test.go:52` |
| `TestOpenCreatesPrivateDatabase` | New files are created at 0600 before SQLite opens them and the directory is not chmodded (`internal/store/sqlite.go:32-38`; `AGENTS.md:108-109`) | Re-executes the test binary with `-test.run` and an environment variable; child sets umask 0 | `internal/store/sqlite_permissions_test.go:16` |
| `TestOpenPreservesExistingDatabasePermissionsAndHistory` | Existing files keep mode, identity, and history; symlinks are followed, not replaced (`internal/store/sqlite.go:32-41`, `internal/store/schema.go:87-89`) | `Chmod` to 0640/0644, a symlink alias, `os.SameFile`, `os.Readlink` | `internal/store/sqlite_permissions_test.go:51` |
| `TestOpenDoesNotCreateThroughDanglingDatabaseSymlink` | `mode=rw` keeps SQLite in existing-file mode (`internal/store/sqlite.go:46-48`); `O_EXCL` leaves the link alone (`internal/store/sqlite.go:32-34`) | Dangling symlink in a temp directory | `internal/store/sqlite_permissions_test.go:123` |
| `TestOpenWithCanceledContextDoesNotCreateDatabase` | Context is checked before any file is created and the error wraps `context.Canceled` (`internal/store/sqlite.go:28-30`) | Pre-canceled `context.WithCancel` | `internal/store/sqlite_permissions_test.go:145` |
| `assertFilePermissions` (helper) | Compares only permission bits via `Mode().Perm()` | None; `os.Stat` | `internal/store/sqlite_permissions_test.go:161` |
| `TestOpenUpgradesVersionOneWithoutChangingHistory` | A version-1 file is recognized without an `error` column (`internal/store/identity.go:57-59`), upgraded to 3 and marked in one transaction (`internal/store/schema.go:97-113`), and left alone on the next open (`:87-89`) | Raw `sql.Open` fixture built from `initialSchema` with hand-written inserts; two opens in a loop | `internal/store/schema_test.go:11` |
| `TestMigrateVersionOneConflictPreservesVersionAndData` | A preexisting conflicting column is refused by recognition (`internal/store/identity.go:123-124` via `internal/store/schema.go:84-86`) and the rollback keeps version, marker, and data (`internal/store/schema.go:61`) | Raw fixture with an extra `error` column defaulting to `'keep'`; calls `migrate` directly | `internal/store/schema_test.go:84` |
| `TestMigrateLeavesUnsupportedOrConflictingSchemasUnchanged` | Newer versions are refused (`internal/store/schema.go:73-75`); a non-empty unmarked version-0 file is refused (`:76-83`); no partial tables survive (`:61`) | `PRAGMA user_version = 4`; a stray `run_steps` table | `internal/store/schema_test.go:118` |
| `TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles` | Foreign markers (`internal/store/schema.go:67-69`), non-empty version 0 (`:76-83`), and every layout defect (`internal/store/identity.go:31-32`, `:41-43`, `:87-89`, `:123-124`, `:157-167`) are refused before any write; `GLOB` not `LIKE` (`internal/store/schema.go:78`) | 23 raw fixtures derived from the real constants with `strings.Replace`; `bytes.Equal` on the whole file | `internal/store/identity_test.go:13` |
| `TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory` | An unmarked version-2 file is adopted, migrated to 3, and marked together (`internal/store/schema.go:102-113`); `sqlite_*` objects are ignored (`internal/store/identity.go:13-14`); marked files may keep indexes and triggers (`internal/store/identity.go:28-30`) | Raw version-2 fixture, real `CreateRun`, `ANALYZE`, index and trigger added between two opens | `internal/store/identity_test.go:86` |
| `TestCheckWritableCommitsWithoutChangingSchemaOrHistory` | The check commits a real write (`internal/store/writable.go:13-17`) yet leaves version, marker, schema, and rows unchanged (`AGENTS.md:110-112`) | Second `sql.Open` with a pinned `*sql.Conn` watching `PRAGMA data_version`; reopen through `Open` | `internal/store/writable_test.go:12` |
| `TestCheckWritableRejectsQueryOnlyConnection` | A connection that cannot write makes the check fail without harming history (`internal/store/writable.go:17-18`); the single-connection pool makes the pragma reach the probe (`internal/store/sqlite.go:59-60`) | `PRAGMA query_only = ON` | `internal/store/writable_test.go:108` |
| `TestCheckWritableHonorsContext` | The check returns on cancellation and deadline with `errors.Is`-visible causes and adds no timeout of its own (`internal/store/writable.go:11`, `:18`; `cmd/server/main.go:87-89`) | Pre-canceled context; the only pooled connection held via `db.Conn`; 20 ms `context.WithTimeout` | `internal/store/writable_test.go:130` |
| `runFixture`, `assertStoredRun` (helpers used throughout; Chapter 25) | A full run with three step outcomes, and a field-by-field read-back of `runs` and `run_steps` | None; real reads through `database/sql` | `internal/store/runs_test.go:145`, `internal/store/runs_test.go:32` |

## Check yourself

1. Why does `TestOpenCreatesPrivateDatabase` start a second copy of the test binary instead of calling `syscall.Umask(0)` in the test itself, and how does the child avoid starting a third process?

<details><summary>Answer</summary>The umask belongs to the whole process. Changing it in the parent would change the mode of every file and directory any other test creates while this one runs, including `t.TempDir()` directories, and a crash before the deferred restore would leave the suite in that state (comment at `sqlite_permissions_test.go:17`). The child started at L42–L43 is the same binary restricted to this one test by `-test.run=^TestOpenCreatesPrivateDatabase$`. It takes the L18 branch because `PATCHBAY_PERMISSIONS_TEST_DATABASE` is in its environment, opens that path under umask 0, and returns at L28 before reaching the spawning code at L31–L46. The variable is the input and the recursion guard at once. Under umask 0, the only thing that can make the file 0600 is the mode argument at `internal/store/sqlite.go:34`.</details>

2. In `TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles`, why is `bytes.Equal` on the whole file stronger than reading `user_version` and `application_id` back, and which production facts make it hold?

<details><summary>Answer</summary>The two header pragmas live in the first 100 bytes, but `bytes.Equal` also compares every page that holds the schema and the rows, so it catches a half-applied `initialSchema`, a dropped view, or a rewritten row that two `PRAGMA` reads would miss. It holds because `Open` never truncates or chmods an existing file, since the `O_EXCL` create fails with `ErrExist` and the code moves on (`internal/store/sqlite.go:32-41`); because every check in `migrate` runs inside the transaction begun at `internal/store/schema.go:57` and the deferred `Rollback` at `:61` discards it on any early return; and because all four refusals (`internal/store/schema.go:67-69`, `:73-75`, `:81-83`, `:84-86`) come before the first `Exec` at `:93`. The comment at L77–L78 adds the precondition: every connection must be closed first, which L57 and L66 guarantee.</details>

3. Why does `TestCheckWritableCommitsWithoutChangingSchemaOrHistory` open a second `*sql.DB` and pin a `*sql.Conn` instead of reading `PRAGMA data_version` through `db`?

<details><summary>Answer</summary>`data_version` changes on a connection only when a different connection commits; commits made on the same connection leave it unchanged. `db` has exactly one connection (`internal/store/sqlite.go:59-60`), and that is the connection `CheckWritable` will use, so reading `data_version` through `db` could never change. The observer must be a second connection, and it must be the same connection both times, because the counter is per connection: a pooled `observer.QueryRowContext` could hand back a fresh connection with its own baseline and the comparison at L62 would be meaningless. `observer.Conn(ctx)` at L45 pins one connection for both reads.</details>

4. The row "version two generated error column" replaces `error TEXT NOT NULL DEFAULT ''` with a generated column. Which production query sees the difference, and why would the plain `table_info` pragma be weaker?

<details><summary>Answer</summary>`recognizeColumns` reads `pragma_table_xinfo` (`internal/store/identity.go:111-112`) and compares the whole `schemaColumn` value, including `hidden`, with the expectation `{"error", "TEXT", 1, "''", 0, 0}` from `internal/store/identity.go:58`. A `GENERATED ALWAYS AS ('') VIRTUAL` column reports `hidden` 2, `notnull` 0, and no default, so `column != expected[count]` at `internal/store/identity.go:123` fails with "unexpected definition". The plain `table_info` pragma omits generated columns entirely, so recognition would see only eight columns and could say nothing about the ninth; `xinfo` reports it and refuses it for the right reason.</details>

5. The first identity test refuses "unmarked legacy extra index", yet the second test creates an index and a trigger on `runs` and expects both to survive a reopen. What flips the outcome, and why is the asymmetry deliberate?

<details><summary>Answer</summary>The `legacy` argument of `recognizeSchema` (`internal/store/identity.go:12`), which `migrate` passes as `owner == 0` (`internal/store/schema.go:84`). For an unmarked file the condition at `internal/store/identity.go:28` is false and the index falls through to `:31-32`. For a marked file, an index or trigger whose `tbl_name` is `runs` or `run_steps` is skipped at `:28-30`. The second test creates its objects after the first `Open` has marked the file (`internal/store/schema.go:109-113`), so the second `Open` sees `owner == applicationID`. The asymmetry is explained at `internal/store/identity.go:9-11` and `:29`: adopting an unmarked file is where picking the wrong database by mistake is most likely, so anything unexpected blocks adoption, while a marked file is already known to be Patchbay's and may legitimately carry diagnostic indexes or the failure-injection triggers used at `internal/store/run_interruptions_test.go:87` and `internal/httpapi/history_test.go:82`.</details>

6. `TestCheckWritableHonorsContext` holds the pool's only connection to make the check wait. What does the 20 ms deadline prove about `cmd/server/main.go:87-89`, and why not simulate a busy database with a second writer?

<details><summary>Answer</summary>It proves that `CheckWritable` returns as soon as the caller's context ends, that the cause is reachable with `errors.Is(err, context.DeadlineExceeded)` through the `%w` at `internal/store/writable.go:18`, and that nothing inside the function waits on a timer of its own (`internal/store/writable.go:11`). So the five-second `context.WithTimeout` at `cmd/server/main.go:87` is the whole budget: a hung database becomes "check database writeability: ... deadline exceeded" at `:91`, and the deferred `db.Close` and lock release still run. A second writer would make SQLite's busy handler retry for up to `busy_timeout(5000)` (`internal/store/sqlite.go:51`); the test would then depend on how the driver interleaves the context with that sleep and would take seconds. Exhausting the `database/sql` pool (`SetMaxOpenConns(1)` at `internal/store/sqlite.go:59`) is deterministic and fast, and the release and successful recheck at L153–L158 show the database itself was fine.</details>

## Experiments

1. **Loosen the creation mode and predict the failure.** In `internal/store/sqlite.go:34` change `0o600` to `0o644`. Run `go test ./internal/store -run TestOpenCreatesPrivateDatabase`. Expected: one failure reported at `sqlite_permissions_test.go:47`, because the `t.Helper()` at L162 makes the `t.Fatalf` at L168 name its caller, reading `.../shared/history.db: got permissions 0644, want 0600`. The child process ran under umask 0, so nothing masked the wider mode. On Windows this test does not exist because of the build constraint at L1. Revert the change.

2. **Swap `GLOB` for `LIKE` and watch two rows fail.** In `internal/store/schema.go:78` change `NOT GLOB 'sqlite_*'` to `NOT LIKE 'sqlite_%'`. Run `go test ./internal/store -run TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles -v`. Expected: 21 subtests pass and two fail, `sqliteX_table_is_not_internal` and `view-only_version_zero`, each printing "unrecognized database was not refused" (`identity_test.go:71`) and then "refused database was modified" (`identity_test.go:80`). With `LIKE`, the `_` matches the `X`, so the user's objects look internal, `migrate` initializes the file at `schema.go:92-96`, and the commit changes its bytes. Revert the change.

3. **Turn the write check into a read.** In `internal/store/writable.go:17` replace `"PRAGMA main.user_version = %d"` with `"SELECT %d"`. Run `go test ./internal/store -run TestCheckWritable -v`. Expected: both subtests of `TestCheckWritableCommitsWithoutChangingSchemaOrHistory` fail with "write check did not commit a write visible to another connection" (`writable_test.go:63`), because `data_version` on the observer never changes; `TestCheckWritableRejectsQueryOnlyConnection` fails with "write check accepted a connection that permits only reads" (`writable_test.go:125`), because a `SELECT` is allowed under `query_only`; `TestCheckWritableHonorsContext` still passes, because cancellation and pool waiting happen before any statement runs. Revert the change.

4. **Log which guard refuses each bad database.** In `internal/store/identity_test.go`, directly after L64, add `t.Logf("refused: %v", openErr)`. Run `go test ./internal/store -run TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles -v`. Expected: 23 log lines. The first three read `refuse to initialize a database that is not empty and unmarked` (`schema.go:82`); the two foreign-marker rows read `refuse database belonging to another application (application ID 305419896)` (`schema.go:68`; 305419896 is 0x12345678 in decimal); every other row begins with `database is not a recognized Patchbay schema version N:` (`schema.go:85`) followed by the specific reason from `identity.go`, such as `unexpected definition for runs column "id"` or `expected one cascading foreign key from run_steps to runs`. Remove the line afterward.

## Next

Chapter 25, "Tests: run persistence, reads, lists, interruptions, and integration" ([25-tests-store-runs.md](25-tests-store-runs.md))
