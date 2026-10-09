# 9. Schema migrations and recognizing a Patchbay database

Files: [internal/store/schema.go](../../internal/store/schema.go), [internal/store/identity.go](../../internal/store/identity.go)

Revision: commit 1c4786e

## Role in the system

These two files are the part of `store.Open` that decides whether a file may be used at all. Chapter 8 left off at `sqlite.go:67`, where `Open` calls `migrate(ctx, db)` after it has created or found the file, limited the pool to one connection, and pinged it. `migrate` (`schema.go:56`) opens one transaction and answers three questions in order. Whose file is this? (The `application_id` header field.) Which Patchbay layout does it have? (The `user_version` header field.) Does the layout really match what that version is supposed to look like? (`recognizeSchema` in `identity.go:12`, which inspects tables, columns, indexes, and foreign keys.) Only then does it create or upgrade the tables, up to schema version 3, and write Patchbay's marker into the header. All of that commits as one unit or not at all. If the file is someone else's, or newer than this build, or shaped wrong, the transaction rolls back and the file is left byte for byte as it was.

Data comes in as the `*sql.DB` from `sqlite.go:54` and the signal context `stop` from `cmd/server/main.go:38`, passed through `Open`. Nothing else: no run, no workflow, no request. Data goes out as a `nil` error or a wrapped error, plus side effects on the file: possibly two new tables, one added column, a new `user_version`, and a new `application_id`. These files never read or write a run row. What they do fix is the shape every later file depends on. The `INSERT INTO runs` at `internal/store/runs.go:29-33` and `INSERT INTO run_steps` at `runs.go:48-51` name exactly the columns that `initialSchema` and `runErrorMigration` create; the `SELECT`s at `internal/store/run_reads.go:77-78` and `run_reads.go:90-91` read them back; the startup cleanup from chapter 8 rewrites the `status` columns. If `migrate` lets a wrongly shaped file through, every one of those statements fails later, in a goroutine, in the middle of a run.

On the path from a browser click to a saved run, these files sit before the click, and before the write check and reconciliation of chapter 8. `runServer` takes the ownership lock at `main.go:66`, calls `store.Open` at `main.go:75`, and on any error returns at `main.go:76-78` before `CheckWritable` (`main.go:88`), before cleanup (`main.go:96`), and before the runner and listener exist. That ordering is the rule "Verify database identity before migration or startup cleanup. Only initialize an empty, unmarked database; recognize supported v1–v3 layouts before assigning Patchbay's application ID" (`AGENTS.md:105-107`). The README states the operator's view: "A foreign marker, unsupported version, or unrecognized layout prevents startup before Patchbay changes the database. Choose a separate database path for a new instance" (`README.md:613-615`).

## Concepts you need

- **Unexported names and in-package tests.** A Go identifier that starts with a lowercase letter, such as `migrate`, `recognizeSchema`, or `schemaVersion`, is visible only inside its own package. Test files that declare `package store` are inside that package, which is why `schema_test.go:99` can call `migrate` directly and `identity_test.go:20` can concatenate `initialSchema` and `runErrorMigration`. Nothing outside `internal/store` can reach these names; `Open` (`sqlite.go:20`) is the only public door. [Spec: Exported identifiers](https://go.dev/ref/spec#Exported_identifiers)
- **Untyped constants and hexadecimal literals.** `const schemaVersion = 3` has no declared type; Go gives it a type when it is used, so it can be an `int` in a comparison and a `%d` argument without conversion. `0x50544259` is an integer written in base 16; each pair of hex digits is one byte, and `50 54 42 59` are the ASCII codes of `P`, `T`, `B`, `Y`. In decimal it is `1347699289`. [Spec: Constants](https://go.dev/ref/spec#Constants), [Spec: Integer literals](https://go.dev/ref/spec#Integer_literals)
- **Raw string literals as SQL scripts.** Text between backticks keeps its line breaks and needs no escaping, so a whole `CREATE TABLE` script can be one constant. The SQLite driver executes every statement in the string in order, which is how `initialSchema` creates two tables and sets a pragma in one `ExecContext` call. Chapter 8 introduced the literal form. [Spec: String literals](https://go.dev/ref/spec#String_literals)
- **Transactions and `defer tx.Rollback()`.** `db.BeginTx(ctx, nil)` starts a transaction; every statement on `tx` belongs to it until `Commit` or `Rollback`. Deferring `Rollback` right after `BeginTx` means any early `return`, including a successful read-only one, ends the transaction cleanly; after `Commit` the deferred call returns `sql.ErrTxDone` and does nothing. If `ctx` is canceled before `Commit`, `database/sql` rolls the transaction back itself and `Commit` returns an error. [database/sql.Tx](https://pkg.go.dev/database/sql#Tx), [DB.BeginTx](https://pkg.go.dev/database/sql#DB.BeginTx)
- **`QueryRowContext(...).Scan(&x)` for one value.** `QueryRowContext` runs a query expected to return one row; `Scan` copies that row's columns into the pointers you pass and releases the row. If the query returns no row, `Scan` returns `sql.ErrNoRows`. Both files use this shape for every single-value read, such as `PRAGMA user_version` or `SELECT COUNT(*)`. [Tx.QueryRowContext](https://pkg.go.dev/database/sql#Tx.QueryRowContext), [Row.Scan](https://pkg.go.dev/database/sql#Row.Scan)
- **The `Rows` loop: `Next`, `Scan`, `Err`, `Close`.** `QueryContext` returns a `*sql.Rows` cursor. `for rows.Next()` advances one row at a time and returns `false` at the end, or when something went wrong. Because those two cases look the same, you must call `rows.Err()` after the loop to tell them apart. `rows.Close()` releases the cursor and is idempotent, so a deferred close and an explicit close can coexist. [database/sql.Rows](https://pkg.go.dev/database/sql#Rows), [Rows.Next](https://pkg.go.dev/database/sql#Rows.Next), [Rows.Err](https://pkg.go.dev/database/sql#Rows.Err), [Rows.Close](https://pkg.go.dev/database/sql#Rows.Close)
- **Scanning NULL and type conversion.** `Scan` refuses to put SQL `NULL` into a plain Go `string` or `int`; the options are a nullable type such as `sql.NullString`, or `COALESCE(column, '')` in the SQL so the driver never sees `NULL`. `Scan` also converts between compatible types: SQLite returns `0` or `1` for `EXISTS (...)`, and `Scan` turns that into a Go `bool`. [Rows.Scan](https://pkg.go.dev/database/sql#Rows.Scan), [sql.NullString](https://pkg.go.dev/database/sql#NullString)
- **`?` placeholders versus `fmt.Sprintf`.** Values normally enter SQL through `?` so they can never change the statement's structure. A `PRAGMA name = value` statement cannot take a placeholder, so `schema.go:110` formats a compile-time constant into the text, exactly as `writable.go:17` does. SQLite's table-valued pragma functions, such as `pragma_table_xinfo(?, 'main')`, are ordinary table expressions and do accept placeholders, which `identity.go:105` and `:112` use for the table name. [fmt.Sprintf](https://pkg.go.dev/fmt#Sprintf), [SQLite: pragma functions](https://sqlite.org/pragma.html#pragfunc)
- **Maps as sets.** `make(map[string]bool)` creates an empty map. Setting `tables[name] = true` records a name; reading `tables["runs"]` for a name that was never set returns the zero value `false`, with no error. This is the usual Go way to say "the set of names I have seen". [A Tour of Go: Maps](https://go.dev/tour/moretypes/19), [Go blog: Maps](https://go.dev/blog/maps)
- **Struct types, positional literals, and struct equality.** A `struct` groups named fields. A composite literal may list values by position, as in `{"id", "TEXT", 1, "", 1, 0}`, as long as every field is given in declaration order. A struct whose fields are all comparable (strings and ints here) is itself comparable, so `column != expected[count]` compares all six fields at once. [Spec: Composite literals](https://go.dev/ref/spec#Composite_literals), [Spec: Comparison operators](https://go.dev/ref/spec#Comparison_operators)
- **Slices and `append`.** `[]schemaColumn{...}` is a slice literal. `append(runColumns, x)` returns a slice with one more element, possibly backed by a new array, which is why the result is assigned back to `runColumns`. [A Tour of Go: Slices](https://go.dev/tour/moretypes/7), [builtin.append](https://pkg.go.dev/builtin#append)
- **Short-circuit `||`.** In `a || b`, Go evaluates `b` only when `a` is false. `count >= len(expected) || column != expected[count]` relies on this: the index on the right is only touched when the left side has proved it is in range. [Spec: Logical operators](https://go.dev/ref/spec#Logical_operators)
- **`if`/`else if` with init statements.** `else if err := f(); err != nil {` runs `f` only when the earlier conditions were false, and the `err` it declares is scoped to that branch. `schema.go:84` uses this to call `recognizeSchema` only for versions 1 to 3. [Spec: If statements](https://go.dev/ref/spec#If_statements)
- **Error wrapping with `%w`.** `fmt.Errorf("read schema objects: %w", err)` adds a short verb phrase in front of the cause and keeps the cause reachable for `errors.Is`. Both files wrap every error, so a refusal reads as a chain: `database is not a recognized Patchbay schema version 2: unexpected index "run_status"`. [fmt.Errorf](https://pkg.go.dev/fmt#Errorf), [errors.Is](https://pkg.go.dev/errors#Is)
- **SQLite header fields: `user_version` and `application_id`.** Both are 32-bit integers stored in the first 100 bytes of the database file, both default to `0`, and SQLite itself never changes them. Applications use `user_version` for a migration counter and `application_id` to say which program the file belongs to; the `file` command on Linux can print the latter. Writing either one marks the header page dirty, so it is a real, journaled write that a transaction can roll back. [PRAGMA user_version](https://sqlite.org/pragma.html#pragma_user_version), [PRAGMA application_id](https://sqlite.org/pragma.html#pragma_application_id), [SQLite file format](https://sqlite.org/fileformat.html)
- **`sqlite_schema` and `GLOB` versus `LIKE`.** Every table, index, view, and trigger has a row in the `sqlite_schema` table with columns `type`, `name`, `tbl_name`, `rootpage`, and `sql`; for indexes and triggers, `tbl_name` names the table they belong to. Objects SQLite creates for itself (`sqlite_autoindex_*`, `sqlite_stat1`, `sqlite_sequence`) have names starting with `sqlite_`. `GLOB` matches case-sensitively with `*` and `?` as wildcards and treats `_` literally; `LIKE` is case-insensitive and treats `_` as "any one character", which would wrongly hide a user table named `sqliteXnotes`. [SQLite: The schema table](https://sqlite.org/schematab.html), [SQLite: GLOB and LIKE](https://sqlite.org/lang_expr.html#like)
- **Table-valued pragma functions.** `pragma_table_list` reports each table's kind and flags, `pragma_table_xinfo` its columns including hidden and generated ones, `pragma_index_list` and `pragma_index_info` its indexes and their columns, and `pragma_foreign_key_list` its foreign keys. Unlike `PRAGMA` statements they can be joined, filtered with `WHERE`, and given parameters. [PRAGMA table_list](https://sqlite.org/pragma.html#pragma_table_list), [PRAGMA table_xinfo](https://sqlite.org/pragma.html#pragma_table_xinfo), [PRAGMA index_list](https://sqlite.org/pragma.html#pragma_index_list), [PRAGMA index_info](https://sqlite.org/pragma.html#pragma_index_info), [PRAGMA foreign_key_list](https://sqlite.org/pragma.html#pragma_foreign_key_list)
- **Transactional DDL.** In SQLite, `CREATE TABLE` and `ALTER TABLE` take part in the enclosing transaction and are undone by `ROLLBACK`, which many other databases do not guarantee. This is what lets `migrate` promise that tables, the version number, and the marker change together. [SQLite: Transactions](https://sqlite.org/lang_transaction.html)
- **`ALTER TABLE ... ADD COLUMN` with `NOT NULL DEFAULT`.** SQLite can add a column to a populated table without rewriting rows; existing rows read the declared default. A `NOT NULL` column may be added only if it has a non-NULL default, which is why the migration says `DEFAULT ''`. [SQLite: ALTER TABLE](https://sqlite.org/lang_altertable.html)
- **Rowid tables, automatic indexes, `WITHOUT ROWID`, `STRICT`, and generated columns.** An ordinary SQLite table has a hidden integer `rowid`. A `PRIMARY KEY` or `UNIQUE` constraint on other columns is enforced by an automatic index named `sqlite_autoindex_<table>_<n>`. `WITHOUT ROWID` and `STRICT` are opt-in table modes that change storage and typing, and a generated column is computed from others; Patchbay uses none of these, so recognition rejects them. [SQLite: CREATE TABLE](https://sqlite.org/lang_createtable.html), [WITHOUT ROWID](https://sqlite.org/withoutrowid.html), [STRICT tables](https://sqlite.org/stricttables.html), [Generated columns](https://sqlite.org/gencol.html)
- **Foreign keys and `ON DELETE CASCADE`.** `REFERENCES runs(id) ON DELETE CASCADE` means a `run_steps` row must point at an existing `runs` row, and deleting the run deletes its steps. SQLite enforces this only when `PRAGMA foreign_keys` is on, which `sqlite.go:50` sets for every connection. [SQLite: Foreign key support](https://sqlite.org/foreignkeys.html)

## Walkthrough

### internal/store/schema.go

**L1–L1** — `package store`. No doc comment; `sqlite.go:1` documents the package. Everything in this file shares one namespace with `sqlite.go`, `writable.go`, and `identity.go`, which is why `migrate` can call `recognizeSchema` and `writable.go:17` can use `schemaVersion` without imports.

**L2–L2** — Blank line.

**L3–L7** — Imports: `context` for the cancellation parameter, `database/sql` for `*sql.DB` and `*sql.Tx`, `fmt` for `Errorf` on every error path and `Sprintf` at L110. The file needs nothing from the engine or workflow packages because it works purely in SQL.

**L8–L8** — Blank line.

**L9–L9** — `const schemaVersion = 3`. The newest database layout this build understands and the value it writes into `user_version`. It is compared at L73 and L87, and `writable.go:17` writes it back during the startup write check. Do not confuse it with `workflow.Definition.SchemaVersion` (`internal/workflow/workflow.go:21`), which is the JSON file format and stays at 1 (`README.md:600`). Raising this constant without adding a migration step that sets `PRAGMA user_version` to the new value leaves the file one version behind; Experiment 1 shows which tests catch that.

**L10–L10** — Blank line.

**L11–L12** — The marker. The comment says what it is for: "PTBY" identifies Patchbay files independently of their migration version. The marker answers "whose file?" and `user_version` answers "which layout?", and the README insists they are separate: "This application marker is separate from the schema version" (`README.md:611-612`). `identity_test.go:14` hardcodes the same hex value, so a change here breaks that test on purpose. The `AGENTS.md` rule adds the limit: "The marker is a file-selection guard, not authentication" (`AGENTS.md:107`); any program can write this number into any file.

**L13–L13** — Blank line.

**L14–L15** — Two comments that govern the next constant. "Keep this first migration unchanged when adding later schema versions" is the discipline every versioned-migration scheme needs: a version-1 file in the field was built by this exact text, and `recognizeSchema` describes version 1 by it (`identity.go:47-56`). Changing it would make real version-1 files unrecognizable. "Timestamps are Unix milliseconds; NULL means a run or step has not started/finished" is the contract `runs.go` and `run_reads.go` follow, and the reason the browser can show **Never started** and **Finish time unknown** (chapter 8).

**L16–L16** — Start of the raw string literal `initialSchema`. One constant holds the whole version-1 script.

**L17–L26** — `CREATE TABLE runs`. One row per run: `id` is the text primary key the engine generates; `workflow_id` and `workflow_name` are copied so history survives renaming a workflow file; `definition_json` is the full workflow snapshot taken at admission (`runs.go:17-18`), which is what keeps "historical results independent of the currently loaded workflow" (`AGENTS.md:126-127`); `status` is the engine's string; `created_at` is `NOT NULL` because every run has been created, while `started_at` and `finished_at` are nullable per the comment at L15. `TEXT PRIMARY KEY` on a rowid table makes SQLite create `sqlite_autoindex_runs_1`, which is why recognition filters `sqlite_*` names (`identity.go:14`).

**L27–L27** — Blank line inside the script.

**L28–L40** — `CREATE TABLE run_steps`. One row per step of a run. `run_id ... REFERENCES runs(id) ON DELETE CASCADE` (L29) ties each step to its run and is enforced because `sqlite.go:50` turns `foreign_keys` on; `recognizeForeignKeys` later demands exactly this clause (`identity.go:157`). `position` with `CHECK (position >= 0)` (L31) is the step's index in the definition, and `UNIQUE (run_id, position)` (L39) means no two steps of one run can claim the same slot; `UpdateRun` relies on that when it matches rows by `run_id`, `step_id`, and `position` together (`runs.go:106-110`). `PRIMARY KEY (run_id, step_id)` (L38) is why a duplicated step ID would fail at insert time, as chapter 4 noted. `output_json` is nullable because a step that never ran has no result; `error TEXT NOT NULL DEFAULT ''` (L37) means "no error" is the empty string, never `NULL`, so readers need no null handling.

**L41–L41** — Blank line inside the script.

**L42–L43** — `PRAGMA user_version = 1;` and the closing backtick. Setting the version inside the same script, and therefore inside the same transaction as the `CREATE TABLE`s, is what makes "Schema changes and SQLite's `user_version` number are committed together" true (`README.md:594`). A crash between creating the tables and recording the version cannot happen.

**L44–L44** — Blank line.

**L45–L49** — The version-2 migration: add `runs.error`. The comment fixes the semantics for existing rows: "Existing runs have no recorded run-level reason; leave that value empty." `NOT NULL DEFAULT ''` is what makes the `ALTER` legal on a populated table and gives old rows the empty string. `CreateRun` writes this column at `runs.go:29-33`, `UpdateRun` at `runs.go:75-76`, and `readRunInTx` reads it at `run_reads.go:77-78`; the README explains its purpose at `README.md:619-620`. `TestOpenUpgradesVersionOneWithoutChangingHistory` seeds a version-1 file by hand because `CreateRun` needs this column (`schema_test.go:27`), opens it, and asserts the run comes back with an empty error and all its old data (`schema_test.go:76-77`).

**L50–L50** — Blank line.

**L51–L54** — The version-3 migration, which is only `PRAGMA user_version = 3;`. The three comment lines explain a migration that changes no table. Version 3 means "the JSON columns may now contain `tcp.check` configuration and results". A TCP result carries `type: "tcp.check"` and an absent type means HTTP (`internal/workflow/workflow.go:47-50`), so saved HTTP results need no rewrite. The version bump exists for the other direction: an older HTTP-only build has `schemaVersion = 2`, so its own L73 check refuses a version-3 file with `unsupported database schema version 3 (expected 2)` instead of reading TCP JSON it cannot decode. That is the rule "Schema version 3 records TCP support without rewriting saved JSON, preventing older HTTP-only builds from reading TCP history" (`AGENTS.md:127-129`) and `README.md:588-591`, and the roadmap flags that this gating choice will be revisited (`docs/roadmap.md:101`).

**L55–L55** — Blank line.

**L56–L56** — `func migrate(ctx context.Context, db *sql.DB) error`. Unexported; the only non-test caller is `Open` at `sqlite.go:67`, which closes the pool and returns this function's error unchanged. Tests call it directly to probe failure paths (`schema_test.go:99`, `:136`). It takes the whole `*sql.DB` rather than a transaction because it owns the transaction's lifetime.

**L57–L60** — Begin the transaction, wrapped as `begin schema migration`. Everything below, reads included, happens inside it. With `SetMaxOpenConns(1)` (`sqlite.go:59`) this takes the pool's only connection, which is fine because nothing else in the process exists yet. A context that is already canceled fails here.

**L61–L61** — `defer tx.Rollback()`. The comment says the second half of its job: "Also releases the transaction on any early return." That includes the success path at L88, which returns without committing. Every `return fmt.Errorf(...)` below leaves through this line, so a refusal never leaves a half-created table or a half-set header behind; `TestMigrateLeavesUnsupportedOrConflictingSchemasUnchanged` asserts no `runs` table exists after a refused migration (`schema_test.go:146-151`), and `TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles` compares the file's bytes before and after (`identity_test.go:60`, `:73`, `:79-80`).

**L62–L62** — Blank line.

**L63–L63** — `var owner, version int`. The two header values, read next. Both are zero in a brand-new file.

**L64–L66** — Read `PRAGMA application_id` into `owner`. A pragma that returns a value works like a one-row query. Failure here, wrapped as `read database application ID`, usually means the file is not SQLite at all; `TestRunServerReleasesDatabaseLockAfterOpenFailure` (`cmd/server/main_test.go:184-198`) feeds a text file and expects startup to stop.

**L67–L69** — Refuse a file marked by another application. `owner != 0 && owner != applicationID` means "marked, and not by us". Zero is unmarked and continues. This is the first and cheapest identity test, and it runs before any other read. The message prints the foreign number in decimal, so the deliberately foreign test file says `application ID 305419896` (`identity_test.go:15`, `:26-27`). An unmarked stranger's file, which is what most SQLite files in the wild are, has `application_id` 0, passes this check without any message, and is caught later by the empty-file or layout checks. Note that the "foreign marker on matching schema" case (`identity_test.go:27`) has exactly Patchbay's tables and is still refused: the marker outranks the layout.

**L70–L72** — Read `PRAGMA user_version` into `version`, wrapped as `read database schema version`.

**L73–L75** — Range check: `version < 0 || version > schemaVersion`. A value above 3 was written by a newer Patchbay build, and this build must not touch a layout it does not know, so there is "no automatic downgrade" (`README.md:597-598`); the `newer version` case at `schema_test.go:123` expects a refusal with the version left at 4. A negative value cannot come from Patchbay (the header field is a signed 32-bit integer) and is treated as foreign. The message names both numbers so the operator knows which build to run.

**L76–L83** — The version-0 branch: decide whether this is a new, empty file. L78 asks `sqlite_schema` whether any object exists whose name does not start with `sqlite_`. The `GLOB` matters: `LIKE 'sqlite_%'` would also hide a user table named `sqliteXnotes`, which the `sqliteX table is not internal` case (`identity_test.go:24`) exists to catch. L81 then refuses if the file is marked (`owner != 0`, a marked file with version 0 is inconsistent) or has any user object. This is the literal implementation of "Only initialize an empty, unmarked database" (`AGENTS.md:105-106`) and "New databases must be empty and unmarked" (`README.md:604`). Without it, `initialSchema` would run inside somebody else's database: if their tables had different names it would succeed and mark their file as Patchbay's, and if they had a `runs` table it would fail halfway. The `table conflict` case (`schema_test.go:124`) and the `unrelated populated version zero` and `view-only version zero` cases (`identity_test.go:23`, `:25`) all stop here.

**L84–L86** — The other branch: versions 1 to 3 must be recognized before anything is written. `recognizeSchema(ctx, tx, version, owner == 0)` (`identity.go:12`) is told which column set to expect and whether the file is unmarked ("legacy"). The error is wrapped with the version so a refusal reads, for example, `database is not a recognized Patchbay schema version 2: unexpected foreign key on run_steps`. This is "recognize supported v1–v3 layouts before assigning Patchbay's application ID" (`AGENTS.md:106-107`). Note that marked files are checked too, with `legacy` false; `README.md:612-613` says so, and `docs/roadmap.md:107` records that keeping the full check for marked files is a decision to revisit at the first migration that adds a table.

**L87–L89** — The common restart path: current version, already marked, nothing to do. `return nil` runs the deferred `Rollback`, which ends a transaction that only read, so no journal is written and the file is untouched. The comment, "A recognized current database needs no writes", is also why chapter 8's `CheckWritable` exists: `Open` can succeed on this path even when the directory forbids creating a journal, so the write check has to prove separately that a commit can happen. `TestOpenUpgradesVersionOneWithoutChangingHistory` opens the same file twice for this reason (`schema_test.go:56-57`).

**L90–L90** — Blank line.

**L91–L91** — Comment: "Apply all needed changes in this transaction, including on a fresh database." A fresh file goes through all three steps below in one commit, which the README describes as "A new database applies versions 1–3 in one transaction" (`README.md:586-587`).

**L92–L96** — For version 0, create the version-1 layout by executing `initialSchema`, wrapped as `create initial database schema`. After this statement `user_version` is 1 inside the transaction, but the Go variable `version` is still 0, which is exactly what the next two checks need.

**L97–L101** — For versions below 2, add the `error` column and set the version to 2. A fresh file and a real version-1 file both pass through here. A version-1 file that already has an `error` column never reaches this line: `recognizeSchema` expects eight `runs` columns for version 1 (`identity.go:47-59`) and refuses on the ninth, which `TestMigrateVersionOneConflictPreservesVersionAndData` pins by asserting the version is still 1, the marker still 0, and the stranger's `DEFAULT 'keep'` value still in place (`schema_test.go:91-115`). The order of checks is the protection: inspect, then alter.

**L102–L106** — For versions below 3, record TCP support. A fresh file, a version-1 file, and a version-2 file all take this step; it is one pragma and cannot fail on data. `TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory` (`identity_test.go:86-155`) walks a version-2 file with a saved run through it and asserts the run is intact afterwards.

**L107–L108** — Two comments: "Adopt a recognized legacy database only after its migration succeeds. The marker and any schema changes commit or roll back together." The second sentence is guaranteed by the transaction, whatever the order; the first sentence is the author's statement of intent, that the marker is the last word and is never written for a file whose upgrade did not complete. `README.md:610-611` states the same contract to operators.

**L109–L113** — Write the marker if the file had none. `owner == 0` is true for a fresh file and for every unmarked legacy file. A `PRAGMA` cannot take a `?`, so `Sprintf` inserts the constant, producing the literal text `PRAGMA application_id = 1347699289`; this is safe only because the value is a compile-time constant, never input. An unmarked version-3 file lands here having skipped all three migration steps, so adoption can be the only change (`README.md:605`). The reopen loops in `schema_test.go:70-75` and `identity_test.go:124-133` assert the marker is present after the first open and unchanged after the second.

**L114–L116** — Commit, wrapped as `commit schema migration`. Until this line nothing is visible to any other connection or process, and a failure here (disk full, directory not writable for the journal) leaves the file as it was. The `stop` context has bounded `BeginTx` and every statement above; if it was canceled before this point, `database/sql` has already rolled the transaction back and `Commit` reports an error.

**L117–L118** — `return nil` and the closing brace. The deferred `Rollback` now finds a finished transaction and does nothing. `Open` returns the pool to `runServer`, which goes on to the write check.

### internal/store/identity.go

**L1–L1** — `package store`.

**L2–L2** — Blank line.

**L3–L7** — Imports: `context`, `database/sql`, `fmt`. The same three as `schema.go`; this file is all queries and error messages.

**L8–L8** — Blank line.

**L9–L11** — The doc comment, in three sentences. "Legacy files have no application ID. Recognize the known table layouts before adopting them" is the reason the function exists: Patchbay versions 1 and 2 shipped before the marker, so the only way to tell a legacy Patchbay file from a stranger's is its shape. "Also check marked files before allowing startup writes" says the check is not skipped once a file is marked. "This guards against selecting the wrong file; it is not a tamper/integrity check" sets the limit, matching `README.md:616-617`: a file with an identical layout passes, and corrupted row data is not examined.

**L12–L12** — `func recognizeSchema(ctx context.Context, tx *sql.Tx, version int, legacy bool) error`. Called once, from `schema.go:84`, inside `migrate`'s transaction, with `legacy` set to `owner == 0`. It takes the `*sql.Tx` rather than the `*sql.DB` so its reads are part of the same transaction as the writes that may follow.

**L13–L17** — List every user-visible schema object: its kind, its own name, and the table it belongs to. The `NOT GLOB 'sqlite_*'` filter drops SQLite's automatic indexes and the `sqlite_stat1` table that `ANALYZE` creates, because those appear in any Patchbay file that has been used; `identity_test.go:102-106` runs `ANALYZE` on a legacy fixture to prove it is still adopted. The error is wrapped as `read schema objects`.

**L18–L18** — `defer rows.Close()`. Covers every early `return` inside the loop below, so the cursor never leaks a connection.

**L19–L19** — `tables := make(map[string]bool)`. The set of known tables actually seen. Only two keys can ever be set (L27), and L41 reads both.

**L20–L24** — Iterate and scan each row into three strings, wrapped as `read schema object` on failure. None of these columns is nullable in `sqlite_schema`, so plain strings are safe.

**L25–L25** — `knownTable := table == "runs" || table == "run_steps"`. Whether this object belongs to one of Patchbay's two tables. For a table or view, `tbl_name` is its own name; for an index or trigger, it is the table it is attached to.

**L26–L27** — A table named `runs` or `run_steps` is recorded. The `name == table` condition is always true for tables and simply spells out that the object is the table itself and not something attached to it.

**L28–L30** — A marked file (`!legacy`) may carry extra indexes and triggers on the known tables; they are skipped. The comment gives the two sources: "diagnostic indexes" an operator might add, and "test-injected triggers". The second is concrete: `TestRunServerReconcilesHistoryBeforeListening` creates `reject_interruption` on `runs` in a marked database, closes it, and then starts `runServer`, which reopens the file through `Open` (`cmd/server/main_test.go:123-131`). Without this branch that test could not exist. `identity_test.go:135-149` pins the behaviour directly by adding an index and a trigger between two opens and checking both survive. For an unmarked file the same objects are refused (`identity_test.go:40-41`): a stranger's database with similar tables is more likely to have extras, and the README states the asymmetry: "Extra user-defined schema objects prevent adoption of an unmarked file" (`README.md:607`) versus "additional indexes and triggers on Patchbay's tables are allowed" on marked files (`README.md:613`).

**L31–L33** — Everything else is refused: a view, an extra table, an index on an unknown table, or an index or trigger on a legacy file. The message quotes the kind and name, so a refusal says, for example, `unexpected view "saved_names"`. The marked-file cases at `identity_test.go:44-45` show that even a marked file is refused for an extra table or view.

**L34–L34** — Closing brace of the `for` loop.

**L35–L37** — `rows.Err()`. A loop that ended because of a read error looks identical to one that reached the end; this call is the only way to know. Wrapped as `read schema objects`.

**L38–L40** — Explicit `rows.Close()` with its error checked. Two reasons to do this when L18 already deferred a close. The deferred call discards its error; this one reports it. And the function is about to run more queries on the same transaction (L60, L63, L78, L90), so finishing this cursor first keeps exactly one statement active on the connection at a time. The deferred close then does nothing, because `Close` is idempotent.

**L41–L43** — Both tables must have been seen. A Patchbay file of any supported version has both, so a file missing one is not ours. The `version one wrong columns` and `marked current wrong columns` cases (`identity_test.go:28`, `:42`) get past this line only when both tables exist; the second has only `runs` and stops here.

**L44–L44** — Blank line.

**L45–L46** — Comment describing what the column comparison covers: "names, declared types, nullability, defaults, primary-key order, and hidden/generated columns", and what it ignores: "SQL text need not have identical formatting". Comparing `pragma_table_xinfo` output instead of the `sql` text in `sqlite_schema` is what lets a file created by a differently indented script, or by an `ALTER` instead of a fresh `CREATE`, still be recognized. One thing this comparison cannot see is the `CHECK (position >= 0)` constraint (`schema.go:31`); the pragma does not report it, and recognition is a selection guard, not a proof of equality.

**L47–L56** — The expected `runs` columns for version 1, one positional `schemaColumn` literal per column, in `cid` order, matching `schema.go:17-26` field for field. Read one entry: `{"id", "TEXT", 1, "", 1, 0}` means name `id`, declared type `TEXT`, `NOT NULL`, no default, position 1 in the primary key, not hidden. `started_at` and `finished_at` have `notNull` 0 because they may be `NULL` (`schema.go:15`). The `version one missing primary key` case (`identity_test.go:30`) fails on the `pk` field of this first entry.

**L57–L59** — For version 2 and above, append the `error` column. Its default is the SQL text `''` (two quote characters), which is what `pragma_table_xinfo` reports for `DEFAULT ''`; a file whose column says `DEFAULT 'keep'` fails on this field (`identity_test.go:36`), and a `GENERATED ALWAYS` column is reported with a non-zero `hidden` code (it also lacks `NOT NULL` and the default, so three fields differ), which is why the query at L111-112 uses `table_xinfo` and not `table_info`: only `xinfo` lists such a column at all instead of silently omitting it (`identity_test.go:37`). Making the list depend on `version` is what lets a version-1 file have eight columns and a version-2 file nine, and what refuses a version-1 file with a ninth.

**L60–L62** — Check the `runs` table. `recognizeColumns` (L101) returns an already-wrapped error, so it is passed up unchanged.

**L63–L75** — Check `run_steps` with its nine columns, matching `schema.go:28-40`. The two `pk` values show composite primary-key order: `run_id` is position 1 and `step_id` position 2, so a file that declared `PRIMARY KEY (step_id, run_id)` would be refused. `output_json` is nullable; `error` is not and defaults to `''`. The `run_steps` layout never changed between versions, so this list has no version branch.

**L76–L76** — Blank line.

**L77–L86** — Confirm that `run_steps` has a unique, non-partial index over exactly `(run_id, position)` in that order. `UNIQUE (run_id, position)` at `schema.go:39` creates an automatic index that the L13 listing filtered out by name, so this is the place it is verified. The query walks `pragma_index_list('run_steps')`, keeps indexes with `"unique" = 1 AND partial = 0`, requires exactly two columns in `pragma_index_info`, and checks the column at `seqno` 0 is `run_id` and at `seqno` 1 is `position`. The primary-key index also has `unique = 1` but its second column is `step_id`, so it does not satisfy the test. `EXISTS` yields `0` or `1`, which `Scan` converts into the Go `bool`. Wrapped as `read step-position uniqueness`. Without this check, a file could accept two steps at one position, and `UpdateRun`'s match on `position` (`runs.go:106-110`) would stop meaning anything.

**L87–L89** — Refuse when no such index exists. The `version one missing position uniqueness` and `marked current missing constraint` cases (`identity_test.go:31`, `:43`) remove the `UNIQUE` clause and stop here.

**L90–L91** — Hand the last check to `recognizeForeignKeys` (L137) and return its result directly, then the closing brace. Returning a call's result is the idiomatic way to end a function whose last step is another function.

**L92–L92** — Blank line.

**L93–L99** — `type schemaColumn struct` with six fields: `name` and `dataType` as strings, then `notNull`, `defaultSQL`, `primaryKey`, and `hidden`. The ints are ints rather than bools on purpose: `pk` is a 1-based position within the primary key, `hidden` is a code (0 normal, 2 generated virtual, 3 generated stored), and `notnull` arrives from SQLite as an integer anyway. Grouping `name, dataType string` on one line is ordinary Go for fields of one type. Because every field is comparable, two values can be compared with `!=` at L123.

**L100–L100** — Blank line.

**L101–L101** — `func recognizeColumns(ctx context.Context, tx *sql.Tx, table string, expected []schemaColumn) error`. Called twice from `recognizeSchema` (L60, L63). It verifies both the table's kind and its exact column list.

**L102–L103** — Three variables for the first query: the object's type and its `WITHOUT ROWID` and `STRICT` flags.

**L104–L107** — Read `type`, `wr`, and `strict` from `pragma_table_list` for this table in the `main` schema, using a `?` placeholder for the name. `QueryRowContext` returns `sql.ErrNoRows` if the table is missing, but L41 has already guaranteed it exists. Wrapped as `read table %q`.

**L108–L110** — Require an ordinary table: kind `table`, rowid-based, not strict. A `WITHOUT ROWID` or `STRICT` table stores and types data differently from what `initialSchema` created, so a file with one is not a Patchbay file even if its column names match. Wrapped as `unexpected table kind`.

**L111–L115** — Read the column list from `pragma_table_xinfo(?, 'main')` ordered by `cid`, the declaration order. `"notnull"` is double-quoted because `notnull` is a keyword in SQLite. `COALESCE(dflt_value, '')` turns the `NULL` that columns without a default report into an empty string, because `Scan` cannot put `NULL` into `column.defaultSQL`, a plain string. `xinfo` rather than `info` so that hidden and generated columns are reported with their `hidden` code instead of being left out. Wrapped as `read columns of %q`.

**L116–L116** — `defer rows.Close()`. No explicit close follows in this function because no further query runs after the loop, so the deferred call is the one that releases the cursor.

**L117–L117** — `count := 0`. How many columns have matched so far; doubles as the index into `expected`.

**L118–L122** — Scan each row straight into the fields of a `schemaColumn`. The field order in the `Scan` call matches the `SELECT` list at L111, and the struct's field order matches the positional literals at L47-75. Wrapped as `read column of %q`.

**L123–L125** — The comparison. `count >= len(expected)` catches a file with more columns than expected, and the short-circuit `||` ensures `expected[count]` is only read when it is in range. `column != expected[count]` compares all six fields at once. The message names the table and column, so the version-1 file with a stray `error` column from `schema_test.go:91-98` is refused as `unexpected definition for runs column "error"`, and a `BLOB`-typed `workflow_name` (`identity_test.go:34`) or a nullable one (`identity_test.go:35`) is refused by name.

**L126–L127** — Advance the count, and the loop's closing brace.

**L128–L130** — `rows.Err()` after the loop, for the same reason as `identity.go:35-37`. Wrapped as `read columns of %q`.

**L131–L133** — Fewer columns than expected is also a mismatch. The loop can only catch "too many" and "wrong"; this line catches "too few". No test in the suite reaches it: the `version two wrong columns` case (`identity_test.go:29`), where `runs` has a single `id` column, is refused one row earlier at L123-125, because that lone `id` column lacks `NOT NULL` and the primary key. A `runs` table whose first eight columns were exact and whose ninth, `error`, was missing is the case this line exists for.

**L134–L135** — `return nil` and the closing brace. The deferred close releases the cursor on the way out.

**L136–L136** — Blank line.

**L137–L137** — `func recognizeForeignKeys(ctx context.Context, tx *sql.Tx) error`. Called once from L90. It checks both tables' foreign keys against `schema.go:29`: none on `runs`, exactly one cascading key on `run_steps`.

**L138–L141** — Count the foreign keys declared on `runs`, wrapped as `read run foreign keys`.

**L142–L144** — `runs` must have none. A `runs` table that references something else is not the one `initialSchema` created.

**L145–L149** — List `run_steps`' foreign keys with their target table, source and target columns, update and delete actions, and match clause. `"table"`, `"from"`, `"to"`, and `"match"` are quoted because they are SQL keywords. Wrapped as `read step foreign keys`.

**L150–L150** — `defer rows.Close()`, again the only close needed because the function ends after the loop.

**L151–L151** — `count := 0`, the number of foreign keys seen.

**L152–L156** — Scan each key into six strings, wrapped as `read step foreign key`.

**L157–L159** — Every key must be exactly the one from `schema.go:29`: pointing at `runs`, from `run_id` to `id`, `NO ACTION` on update (SQLite's default when nothing is written), `CASCADE` on delete, and `MATCH NONE` (also the default). The `version two non-cascading foreign key` case (`identity_test.go:33`) replaces `CASCADE` with `RESTRICT` and is refused here. Without this check a file could keep orphaned step rows after a run was deleted, or refuse to delete runs at all.

**L160–L161** — Advance the count, and the loop's closing brace.

**L162–L164** — `rows.Err()`, wrapped as `read step foreign keys`.

**L165–L167** — Exactly one key. Zero is the `version two missing foreign key` case (`identity_test.go:32`); the loop above would have refused a second, different key, and a second identical key is also refused here. The message says what was expected: `one cascading foreign key from run_steps to runs`.

**L168–L169** — `return nil` and the closing brace. This `nil` travels back through `recognizeSchema` (L90) to `migrate` (`schema.go:84`), which then decides whether any migration or marking is needed.

## State, concurrency, and cancellation

**Goroutines.** Both files run on the main goroutine, once per process, inside `store.Open` (`sqlite.go:67`), which `runServer` calls at `main.go:75`. At that moment there is no runner (`engine.New` is at `main.go:107`), no scheduler, and no HTTP listener (`main.go:129`), so no other goroutine in this process can touch the database. Across processes, the `flock` taken at `main.go:66` (chapter 3) keeps a second Patchbay out, and the rule behind that order is "Take the database ownership lock before migrations or interruption cleanup" (`AGENTS.md:95`). Another program with the file open, such as a `sqlite3` shell, is handled by SQLite's own locking plus the five-second `busy_timeout` from `sqlite.go:51`.

**Mutexes and lock order.** Neither file defines or takes a Go mutex. The one piece of exclusive state is the transaction started at `schema.go:57`, which holds the pool's only connection (`sqlite.go:59`) from `BeginTx` until `Commit` at L114 or the deferred `Rollback` at L61. Inside it, `recognizeSchema` opens at most one `Rows` cursor at a time: the listing at `identity.go:13` is closed explicitly at L38 before the next query, and `recognizeColumns` and `recognizeForeignKeys` each finish their cursor before returning. On the database side, SQLite begins with a read lock and escalates to a write lock only at the first `CREATE`, `ALTER`, or `PRAGMA ... =` statement, so a refusal or a current-and-marked restart never takes a write lock at all.

**Contexts.** Everything here uses one context: `stop`, created at `main.go:38` from `SIGINT` and `SIGTERM`, passed to `Open` and from there to `migrate`. There is no timeout on this path; `runServer` adds a five-second deadline only for the write check and the cleanup that follow (`main.go:87`, `main.go:95`). The context bounds `BeginTx` (`schema.go:57`) and every `QueryRowContext`, `QueryContext`, and `ExecContext` in both files. If a signal arrives mid-migration, the next statement returns a wrapped `context.Canceled`, `database/sql` rolls the transaction back, `Open` closes the pool (`sqlite.go:68`), and `runServer` returns; the file is as it was. Chapter 8 noted, from the comment at `writable.go:15-16`, that this driver's `Tx.Commit` runs under a background context, so the final commit at `schema.go:114` is the one step that a signal does not interrupt.

**Failure paths.**

| Fails at | Reason | What is on disk afterwards | Pinned by |
| --- | --- | --- | --- |
| `schema.go:57` | canceled context, or a connection error | unchanged | no test reaches `migrate` with a canceled context; `sqlite.go:28` stops earlier (`sqlite_permissions_test.go:145-159`) |
| `schema.go:64` or `:70` | not a SQLite file | unchanged | `cmd/server/main_test.go:184-198` |
| `schema.go:67` | marked by another application | unchanged, foreign marker kept | `identity_test.go:26-27` |
| `schema.go:73` | version above 3 or negative | unchanged, version kept | `schema_test.go:123`, `:143-144` |
| `schema.go:81` | version 0 but marked or not empty | unchanged | `schema_test.go:124`, `identity_test.go:23-25` |
| `schema.go:84` via `identity.go` | wrong tables, columns, index, or foreign key | unchanged, including the stray column's data | `schema_test.go:84-116`, `identity_test.go:28-45` |
| `schema.go:93`, `:98`, `:103`, `:110` | a statement fails (disk full, I/O error) | unchanged, rolled back by L61 | no test injects this; the transaction guarantees it |
| `schema.go:114` | commit fails (journal cannot be written) | unchanged | no test injects this for `migrate`; `cmd/server/database_write_test.go:21-126` covers the same directory state through the write check |

In every row `Open` closes the pool (`sqlite.go:68-70`), `runServer` returns the wrapped error (`main.go:76-78`), the deferred lock close runs (`main.go:70-74`), and no run row, step, or listener was ever created. The byte-comparison test at `identity_test.go:60-81` is the broadest statement of the contract: for every refused file, the on-disk bytes before and after `Open` are identical, which covers tables, rows, `user_version`, and `application_id` at once.

**Rules from `AGENTS.md` that this code implements.**

- "Preserve atomic run/step snapshots and existing data. Introduce deliberate versioned migrations when stored structure changes." (`AGENTS.md:103-104`) The three constants `initialSchema`, `runErrorMigration`, and `tcpCheckMigration` are the deliberate, numbered migrations; `migrate` applies only the ones a file lacks (`schema.go:92-106`) and never rewrites saved JSON (`schema.go:51-53`).
- "Verify database identity before migration or startup cleanup. Only initialize an empty, unmarked database; recognize supported v1–v3 layouts before assigning Patchbay's application ID. The marker is a file-selection guard, not authentication." (`AGENTS.md:105-107`) Identity first: `schema.go:64-69`. Empty and unmarked: `schema.go:76-83`. Recognize before marking: `schema.go:84-86` runs before `schema.go:109-113`. Not authentication: `identity.go:11`.
- "Take the database ownership lock before migrations or interruption cleanup." (`AGENTS.md:95`) Not enforced in these files, but relied on: `migrate` has no cross-process guard of its own and is correct only because `main.go:66` precedes `main.go:75`.
- "After opening and recognizing the database, commit a bounded write check before interruption cleanup or HTTP startup." (`AGENTS.md:110-111`) The "recognizing" half is this chapter; the write check exists because `schema.go:87-89` deliberately commits nothing on a normal restart.
- "Schema version 3 records TCP support without rewriting saved JSON, preventing older HTTP-only builds from reading TCP history." (`AGENTS.md:127-129`) `schema.go:51-54` and the range check at `schema.go:73-75` as seen from an older build.

## Check yourself

1. A version-1 file arrives with a `runs.error` column that someone added by hand with `DEFAULT 'keep'`, plus one saved run. Which line refuses it, what does the error message say, and what is in the file afterwards?

<details><summary>Answer</summary>`migrate` reads version 1 and marker 0, so it calls `recognizeSchema` at `schema.go:84` with `legacy` true. The tables pass; then `recognizeColumns` for `runs` is given the eight version-1 columns (`identity.go:47-56`, the `error` entry at L58 is only appended for version 2 and up). The ninth row from `pragma_table_xinfo` makes `count >= len(expected)` true at `identity.go:123`, so the function returns `unexpected definition for runs column "error"`, which `schema.go:85` wraps as `database is not a recognized Patchbay schema version 1: ...`. The deferred rollback at `schema.go:61` ends the read-only transaction. `TestMigrateVersionOneConflictPreservesVersionAndData` (`schema_test.go:84-116`) then reads back version 1, marker 0, and the value `keep`, all unchanged. The `ALTER TABLE` at `schema.go:98` was never reached, which is the point of inspecting before altering.</details>

2. `tcpCheckMigration` changes no table. Why does it exist, and what exactly happens when a Patchbay build with `schemaVersion = 2` opens a file this build has upgraded?

<details><summary>Answer</summary>Version 3 records that the JSON columns may hold `tcp.check` configuration and results (`schema.go:51-53`, `workflow.go:47-50`). The older build runs its own `migrate`: it reads marker `PTBY` (fine), reads `user_version` 3, and at its copy of `schema.go:73` finds `3 > 2`, so it returns `unsupported database schema version 3 (expected 2)` and never reads a row. That is the behaviour the rule asks for: "preventing older HTTP-only builds from reading TCP history" (`AGENTS.md:128-129`), and the README's "there is no automatic downgrade" (`README.md:598`). Without the bump, the old build would open the file, decode a TCP result into its HTTP-only struct, and show it wrongly.</details>

3. An unmarked version-2 file has an extra `CREATE INDEX run_status ON runs(status)`. Is it adopted? Now the same index on a marked, version-3 file. Is that opened? Explain the asymmetry.

<details><summary>Answer</summary>Unmarked: refused. `legacy` is true, so at `identity.go:28` the `!legacy` condition fails, the `else` at L31 returns `unexpected index "run_status"`, and `migrate` reports `database is not a recognized Patchbay schema version 2: ...` with the file untouched (`identity_test.go:40`). Marked: opened. `legacy` is false, the index belongs to a known table, and L28-30 skips it (`identity_test.go:135-149`). The reasoning in the comment at L29 and in `README.md:607` and `:613` is that an unmarked file is being judged purely by shape, so anything beyond the exact Patchbay shape counts against it, while a marked file has already said it is Patchbay's and may carry diagnostic indexes or the triggers that tests inject (`cmd/server/main_test.go:123-126`). Extra tables and views are refused in both cases (`identity_test.go:44-45`).</details>

4. Suppose the marker were written in its own committed transaction before the migrations, instead of at `schema.go:109-113` inside the same one. Walk through a version-1 file whose `ALTER TABLE` then fails. What state is left, and how would the next startup treat it?

<details><summary>Answer</summary>The file would now carry `PTBY` with `user_version` still 1 and no `error` column. On the next startup `migrate` would read marker `PTBY`, version 1, and call `recognizeSchema` with `legacy` false. The layout still matches version 1, so it would pass and the migration would be retried, which sounds harmless, but the file is now lying: it claims to be Patchbay's while it was never successfully upgraded, and from now on extra indexes and triggers on it are tolerated (`identity.go:28-30`) that the legacy rule would have refused. More importantly, if the failure had been a refusal rather than an I/O error, the marker would be stuck on a file Patchbay decided not to use. Keeping the marker in the same transaction (`schema.go:107-108`, `README.md:610-611`) means a file is marked only in the same instant it becomes a complete version-3 database.</details>

5. `migrate` returns `nil` at `schema.go:88` without calling `Commit`. What happens to the transaction, and why is that path the reason `CheckWritable` has to exist?

<details><summary>Answer</summary>The deferred `tx.Rollback()` at L61 runs. The transaction only read two pragmas and, through `recognizeSchema`, some schema tables, so SQLite ends a read transaction: no page was dirtied and no journal file was created. That is good for a normal restart, but it means `Open` succeeds even on a database whose directory forbids creating the rollback journal. `CheckWritable` (`writable.go:17`) writes `user_version` back to its own value as one autocommit statement, which forces a real journaled commit, so `runServer` finds out at `main.go:88` rather than during the first save. `TestOpenUpgradesVersionOneWithoutChangingHistory` opens the same file twice (`schema_test.go:56-81`) to cover both the upgrading path and this no-write path.</details>

6. A file has `user_version` 3 and `application_id` 0. Trace `migrate` statement by statement. Which writes happen, and which test covers the closest case?

<details><summary>Answer</summary>L64: owner 0. L67: not foreign. L70: version 3. L73: in range. L76: not zero, so L84 calls `recognizeSchema(ctx, tx, 3, true)`, which expects nine `runs` columns including `error` (L57-59) and refuses any extra object because `legacy` is true. L87: version is current but owner is 0, so it does not return. L92, L97, L102: all skipped, since 3 is not 0, not below 2, not below 3. L109: owner is 0, so `PRAGMA application_id = 1347699289` runs. L114: commit. The only change to the file is the marker, which is the "unmarked version-3" adoption that `README.md:605` promises. The nearest test is `TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory` (`identity_test.go:86-155`), which starts one version earlier and therefore also runs L103 before marking.</details>

## Experiments

1. **Bump the version constant without a migration.** In `internal/store/schema.go:9` change `3` to `4` and run `go test ./internal/store -run 'TestOpenUpgradesVersionOneWithoutChangingHistory|TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory'`. Expected: both fail. The first reports `expected schema version 4, got 3` (`schema_test.go:68`) and the second `wrong identity after open 1: marker=50544259 version=3` (`identity_test.go:132`), because `migrate` now accepts version 3 as "below current", runs no step that writes `user_version = 4`, and marks the file anyway. That gap is what a real version-4 migration constant must fill. Revert and rerun; both pass.

2. **Replace `GLOB` with `LIKE` in the empty-file check.** In `internal/store/schema.go:78` change `NOT GLOB 'sqlite_*'` to `NOT LIKE 'sqlite_%'`. Run `go test ./internal/store -run 'TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles' -v`. Expected: the subtests `sqliteX_table_is_not_internal` and `view-only_version_zero` fail with `unrecognized database was not refused` followed by `refused database was modified`, because `LIKE` treats `_` as "any one character" and hides `sqliteXnotes` and `sqliteXview`, so `migrate` believes the file is empty, creates Patchbay's tables beside the stranger's object, and marks the file. Every other subtest still passes. Revert.

3. **Read the header fields of a real database.** After running the server at least once (`go run ./cmd/server`, then Ctrl+C), run `sqlite3 data/patchbay.db 'PRAGMA application_id; PRAGMA user_version; SELECT type, name, tbl_name FROM sqlite_schema ORDER BY name;'`. Expected: `1347699289`, then `3`, then rows for `runs`, `run_steps`, and the automatic indexes `sqlite_autoindex_runs_1`, `sqlite_autoindex_run_steps_1`, and `sqlite_autoindex_run_steps_2`, which `identity.go:14` filters out by name. Then copy the file with `cp data/patchbay.db /tmp/foreign.db`, run `sqlite3 /tmp/foreign.db 'PRAGMA application_id = 1;'`, and start `go run ./cmd/server -db /tmp/foreign.db`. Expected: the process exits with status 1 after one `patchbay failed` log line (`main.go:41-42`) whose `error` value is `open database "/tmp/foreign.db": refuse database belonging to another application (application ID 1)`, no `patchbay M0 starting` line appears, and `sqlite3 /tmp/foreign.db 'PRAGMA application_id;'` still prints `1`.

4. **See every refusal message once.** In `internal/store/identity_test.go`, after line 64, add `t.Log(openErr)`, then run `go test ./internal/store -run TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles -v`. Expected: each subtest prints its chain, and you can map every one to a line in this chapter, for example `refuse to initialize a database that is not empty and unmarked` (`schema.go:82`), `refuse database belonging to another application (application ID 305419896)` (`schema.go:68`), `database is not a recognized Patchbay schema version 2: unexpected foreign key on run_steps` (`identity.go:158`), and `database is not a recognized Patchbay schema version 1: unexpected definition for runs column "id"` (`identity.go:124`, the missing-primary-key case). Remove the log line afterwards.

## Next

Chapter 10, "Saving and reading runs" ([10-store-runs.md](10-store-runs.md))
