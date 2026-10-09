# 4. Workflow definitions, validation, and loading

Files: [internal/workflow/workflow.go](../../internal/workflow/workflow.go)

Revision: commit 1c4786e

## Role in the system

`internal/workflow/workflow.go` is the bottom of Patchbay's dependency graph. It defines the four data types that every other backend package passes around (`Definition`, `Step`, `CheckConfig`, `CheckResult`), the single `Validate` function that decides whether a definition may ever run, and the `Load` function that turns a directory of `.json` files into a slice of validated definitions once at startup. The package comment on lines 1 and 2 states the boundary: data and validation only, no network calls, no background work. The engine (`internal/engine/runner.go:105`), the store (`internal/store/runs.go:17`), the HTTP API (`internal/httpapi/api.go:32`), the scheduler (`internal/schedule/scheduler.go:53`), and both executors (`internal/nodes/httpcheck/http.go:37`, `internal/nodes/tcpcheck/tcp.go:27`) import this package. It imports none of them.

Data enters through `Load`. `cmd/server/main.go:58` calls it with the `-workflows` directory before the database is opened, and any error ends the process (`main.go:59-61`, `main.go:40-43`). What comes out is `[]Definition`, held in memory for the life of the process and shared, read-only, by the API handler closure (`api.go:38-39`), the scheduler's entries (`scheduler.go:58-66`), and the run-start path (`api.go:61-65`). When a browser clicks Run, or a cron occurrence fires, `Runner.Start` receives one of those definitions, calls `Validate` again (`runner.go:106`), copies it, and hands it to `store.CreateRun`, which encodes the whole `Definition` as JSON into the `definition_json` column (`runs.go:18`, `internal/store/schema.go:21`) before any step executes. Later, each executor reads its `Step.Config` and returns a `CheckResult`, which the runner stores as `StepRun.Output` (`runner.go:30`, `runner.go:257`) and the store writes as `output_json` (`runs.go:41-45`, `schema.go:36`).

So the types in this file are both the configuration contract (what an author may write) and the history contract (what a saved run looks like forever). That double role explains several details you will meet below: why `CheckResult` carries a `type` field that is omitted for HTTP, why `CheckConfig` marks most fields `omitempty`, and why a file is capped at 64 KiB. The README names this file as the first thing to read (`README.md:416-417`), and the M0 walkthrough summarizes it in `docs/m0-walkthrough.md:32-35` and `docs/m0-walkthrough.md:41-43`.

## Concepts you need

- **Package doc comments and exported names.** A comment that starts with `Package workflow` right above `package workflow` is the package's documentation. In Go, a name that starts with a capital letter (`Definition`, `Validate`, `Load`) is exported and usable from other packages; a lowercase name (`identifier`, `validTCPHost`, `maxDefinitionBytes`) is private to the package, though test files in the same package can still use it. [Go Doc Comments](https://go.dev/doc/comment), [Effective Go: names](https://go.dev/doc/effective_go#names)
- **`internal/` directories.** Go refuses to compile an import of `patchbay/internal/workflow` from any module other than `patchbay` itself. That is why the whole backend lives under `internal/`: nothing outside this repository can depend on these types. [Go Modules Reference: internal](https://go.dev/doc/modules/layout#internal)
- **Typed and untyped constants.** `const maxDefinitionBytes = 64 * 1024` is an untyped integer constant, computed at compile time. It can be compared with a `len(...)` result or passed to `io.LimitReader` without conversions because untyped constants take the type the context needs. [Go spec: Constants](https://go.dev/ref/spec#Constants)
- **Struct tags for `encoding/json`.** The text in backticks after a field, such as `` `json:"schemaVersion"` ``, tells the JSON package which key fills that field and which key to write. Without a tag, the package matches the Go field name case-insensitively. Every key in a workflow file maps to one tagged field here or in `schedule.go`. [encoding/json: Marshal](https://pkg.go.dev/encoding/json#Marshal)
- **`omitempty` and the zero value.** Every Go type has a zero value: `""` for strings, `0` for ints, `false` for bools, `nil` for pointers and slices. The `omitempty` tag option leaves a field out of the JSON output when it holds its zero value. It cannot distinguish "the author wrote 0" from "the author wrote nothing", which is why this file sometimes uses a pointer for an optional object. [encoding/json: Marshal](https://pkg.go.dev/encoding/json#Marshal), [Go Tour: zero values](https://go.dev/tour/basics/12)
- **Value fields versus pointer fields.** `Config CheckConfig` embeds a whole struct by value: copying a `Step` copies the config. `Schedule *Schedule` holds a pointer: copying a `Definition` copies the address, and both copies point at the same `Schedule`. A pointer can be `nil`, which gives optional data a third state that a value field lacks. [Go Tour: pointers](https://go.dev/tour/moretypes/1)
- **Passing structs by value.** `func Validate(d Definition)` receives a copy of the caller's struct. The copy includes the slice header for `Steps` and the pointer for `Schedule`, so the copy still reads the same step array; it does not duplicate it. Since `Validate` only reads, that is safe and cheap. [Go FAQ: pass by value](https://go.dev/doc/faq#pass_by_value)
- **Package-level variables and `regexp.MustCompile`.** A `var` at file scope is initialized once, before `main` runs. `MustCompile` panics if the pattern is invalid, which is the right behavior for a constant pattern: a typo fails at startup instead of on the first request. A compiled `*regexp.Regexp` is safe to use from many goroutines at once. [regexp: MustCompile](https://pkg.go.dev/regexp#MustCompile), [regexp: Regexp](https://pkg.go.dev/regexp#Regexp)
- **Errors as values, `fmt.Errorf`, `%q`, and `%w`.** A Go function reports failure by returning an `error` as its last result; `nil` means success. `fmt.Errorf("step %q ...", id)` quotes the value, so an empty ID prints as `""` instead of vanishing. `%w` wraps an inner error so the message gains context while `errors.Is` can still find the cause. [fmt: Errorf](https://pkg.go.dev/fmt#Errorf), [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)
- **`if` with a short statement and the blank identifier.** `if _, err := NewTimetable(...); err != nil { ... }` runs a statement, then tests a condition; variables declared there exist only inside the `if`. The blank identifier `_` discards a result you do not need. [Go Tour: if with a short statement](https://go.dev/tour/flowcontrol/6), [Effective Go: blank identifier](https://go.dev/doc/effective_go#blank)
- **Maps as sets.** `seen := make(map[string]bool)` plus `seen[id] = true` is the idiomatic Go set. Reading a missing key returns the zero value `false`, so `seen[id]` is a safe "already seen?" test without a separate existence check. [Go Blog: maps](https://go.dev/blog/maps)
- **`for range` over slices and strings.** Ranging over a slice yields a copy of each element. Ranging over a string yields Unicode code points (`rune` values), not bytes, while indexing a string with `s[0]` yields a byte. This file uses both forms on purpose. [Go Blog: strings, bytes, runes](https://go.dev/blog/strings)
- **`switch` on a string with `default`.** A `switch` compares one value against each `case` in order and runs the first match; `default` runs when nothing matches. Go does not fall through between cases unless told to. [Go Tour: switch](https://go.dev/tour/flowcontrol/9)
- **Function literals (closures).** `func(c rune) bool { ... }` written inline is an anonymous function value. It can be passed straight into `strings.ContainsFunc`. [Go Tour: function closures](https://go.dev/tour/moretypes/25)
- **`net/url` parsing.** `url.Parse` splits text into `Scheme`, user info (`User`), host, path, query, and `Fragment`. `Hostname()` returns the host without a port and without IPv6 brackets. [net/url: URL](https://pkg.go.dev/net/url#URL)
- **`net/netip.ParseAddr`.** The newer IP address type in the standard library. `ParseAddr` returns an error for anything that is not an IPv4 or IPv6 address, accepts an IPv6 zone such as `%eth0`, and rejects brackets and ports. [net/netip: ParseAddr](https://pkg.go.dev/net/netip#ParseAddr)
- **`os.ReadDir`, `fs.DirEntry`, and `path/filepath`.** `os.ReadDir` returns directory entries sorted by file name. Each entry can tell you its name and whether it is a directory without another system call. `filepath.Ext` and `filepath.Join` handle extension and path separators for the host OS. [os: ReadDir](https://pkg.go.dev/os#ReadDir), [path/filepath](https://pkg.go.dev/path/filepath)
- **`defer`, and why not to defer inside a loop.** `defer f.Close()` schedules the call for when the surrounding function returns, not when the loop iteration ends. Inside a loop that opens many files, every file would stay open until the function finished. This file closes each file explicitly instead. [Effective Go: defer](https://go.dev/doc/effective_go#defer)
- **`io.LimitReader` and `io.ReadAll`.** `LimitReader(r, n)` wraps a reader so it reports end-of-file after `n` bytes. `ReadAll` reads until end-of-file into a byte slice. Together they bound memory for an untrusted file size. [io: LimitReader](https://pkg.go.dev/io#LimitReader), [io: ReadAll](https://pkg.go.dev/io#ReadAll)
- **`json.Decoder`, `DisallowUnknownFields`, `io.EOF`, and `new(any)`.** A `Decoder` reads JSON from a stream. `DisallowUnknownFields` turns unexpected keys into errors instead of ignoring them. Calling `Decode` again after the document returns `io.EOF` if nothing remains. `new(any)` allocates a pointer to an empty interface, a target that accepts any JSON value, used here only to detect trailing data. [encoding/json: Decoder](https://pkg.go.dev/encoding/json#Decoder), [encoding/json: DisallowUnknownFields](https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields)
- **Custom `UnmarshalJSON`.** A type that implements `UnmarshalJSON([]byte) error` takes over its own decoding. The outer decoder hands it raw bytes, and the outer decoder's settings such as `DisallowUnknownFields` do not reach inside. `Step` has such a method in `step_json.go`, which is why that file repeats the strictness itself. [encoding/json: Unmarshaler](https://pkg.go.dev/encoding/json#Unmarshaler)
- **Nil slices versus empty slices.** `var s []T` is `nil`; `make([]T, 0)` is an empty, non-nil slice. Both have length 0 and both work with `append`, but `json` writes `nil` as `null` and an empty slice as `[]`. [Go Blog: slices](https://go.dev/blog/slices-intro)
- **Comparable structs.** A struct whose fields are all comparable (strings, ints, bools, nested comparable structs) can be compared with `==` and `!=`. `Step` is comparable, so a test can write `got != original`. Adding a slice or map field would remove that property. [Go spec: comparison operators](https://go.dev/ref/spec#Comparison_operators)
- **Table-driven tests and subtests.** The tests for this file list cases in a slice and call `t.Run(name, ...)` for each, so `go test -run 'TestValidate$/unknown_version'` can target one case and failures name the case. `t.TempDir()` gives each test a fresh directory that is deleted afterwards. [Go Wiki: table-driven tests](https://go.dev/wiki/TableDrivenTests), [testing: T.TempDir](https://pkg.go.dev/testing#T.TempDir)

## Walkthrough

### internal/workflow/workflow.go

**L1–L3** — The package comment and `package workflow`. The two comment lines are a promise that the rest of the backend relies on: this package holds "data and validation, with no network calls or background execution". `Validate` never resolves a host name or opens a socket; those happen in the executors (`internal/nodes/httpcheck/http.go:48`, `internal/nodes/tcpcheck/tcp.go:36`). Because the package starts no goroutines and touches no shared state, every other package can call it freely from any goroutine. The directory is `internal/workflow`, so only code inside the `patchbay` module can import it.

**L4–L16** — The import block, standard library only. Each name is used below: `encoding/json` for decoding files (L173), `fmt` for error messages, `io` for `LimitReader`, `ReadAll`, and `EOF` (L164, L178), `net/netip` for IP parsing (L125), `net/url` for URL parsing (L92), `os` for the directory listing and file opening (L148, L159), `path/filepath` for `Ext` and `Join` (L155, L158), `regexp` for the identifier pattern (L61), `strings` for trimming, searching, splitting, and the reader at L173, and `unicode` for the whitespace and control classes (L121). Go rejects an unused import at compile time, so this list is an exact inventory of what the file needs. The package's one third-party dependency, the cron parser, is imported by `schedule.go:8`, not here.

**L17–L18** — `const maxDefinitionBytes = 64 * 1024` is the size cap for one workflow file, 65,536 bytes. It is lowercase, so only this package and its tests see it (`workflow_test.go:30-33` build fixtures around it). It is used at L164 and L169. Commit d14892c added it ("fix: bound workflow definition file reads to 64kb"), and `README.md:335-337` documents it. The bound matters twice: `Load` reads each file fully into memory, and every run later copies the entire definition into the `definition_json` column (`internal/store/runs.go:18`). Without the cap, one huge file could exhaust memory at startup or bloat every row in the `runs` table.

**L19–L20** — `type Definition struct {` opens the top-level workflow type. One `Definition` is one `.json` file. It is the parameter of `Validate` (L63), the element type of `Load`'s result (L147), the argument to `Runner.Start` (`internal/engine/runner.go:105`), to the `RecordNewRun` callback (`runner.go:55`) and `store.CreateRun` (`internal/store/runs.go:17`), the embedded value in the API's `workflowSummary` (`internal/httpapi/api.go:25-27`), and the payload the scheduler keeps per entry (`internal/schedule/scheduler.go:33`). The TypeScript mirror is `Workflow` in `web/src/api.ts:16-25`.

**L21–L21** — `SchemaVersion int` with key `schemaVersion`. `Validate` accepts only `1` (L64-66). `README.md:339-341` explains the purpose: a place to introduce format changes deliberately when the graph model arrives. Do not confuse it with the SQLite schema version, which is 3 while this stays 1 (`README.md:600`). A file without this key decodes to `0` and is rejected, so every author has to state which format they wrote.

**L22–L22** — `ID string` with key `id`. It must match the `identifier` pattern (L67) and be unique across the directory (L184-187). The ID is the key for everything downstream: the browser posts to `/workflows/${id}/runs` (`web/src/pages/WorkflowsPage.tsx:56`), the route matches `r.PathValue("id")` against it with an exact string comparison (`api.go:62`), the runner refuses an overlapping run by this key (`runner.go:119`), the scheduler keys its next-run map by it (`scheduler.go:88`), and the store writes it into `workflow_id` (`runs.go:30-32`).

**L23–L24** — `Name` and `Description`. `Name` must be non-blank (L67) and is copied into `Run.WorkflowName` at admission (`runner.go:127`), so history keeps the name the workflow had when it ran. `Description` has no rule beyond the file size. It exists because JSON has no comments; the roadmap chose JSON as the canonical format and told authors to put notes in description fields (`docs/roadmap.md:30`, `docs/roadmap.md:38`).

**L25–L25** — `Schedule *Schedule` with `schedule,omitempty`. This is a pointer for a reason: a missing `schedule` key leaves it `nil`, which means "manual only", while a present key produces a non-nil pointer that `Validate` must check (L70-74). A plain value field could not tell those apart, because `Schedule{}` and "absent" would look the same. `omitempty` drops a nil pointer when the definition is encoded for the API or the database, which is why `web/src/api.ts:21` marks `schedule?` optional. The type lives in `schedule.go:13-16`. Because this is a nested pointer, `Runner.Start` copies the pointed-to struct before keeping the definition (`runner.go:110-113`); `AGENTS.md:130` warns to "revisit snapshot copying if adding nested pointers".

**L26–L26** — `Steps []Step` with key `steps`. Slice order is execution order: the run loop is `for i, step := range definition.Steps` (`runner.go:225`), and `README.md:339` says there are no graph edges yet. `Validate` requires 1 to 20 entries (L75-77), so a loaded definition never has a nil slice here and the API never writes `null` for it.

**L27–L27** — Closes `Definition`.

**L28–L29** — `type Step struct {` opens one entry of the `steps` array. `Step` has a custom `UnmarshalJSON` in `step_json.go:14-54`, so when the decoder in `Load` reaches a step object it hands the raw bytes to that method instead of filling fields by tag. Every field of `Step` is comparable, so a `Step` can be compared with `!=`; `tcp_test.go:123` relies on that to prove a failed decode left the old value untouched. Executors receive a `Step` by value (`runner.go:51`, `http.go:37`, `tcp.go:27`).

**L30–L30** — `ID string`, key `id`. Same pattern as the workflow ID, but unique only within this workflow (L80-83). The runner copies it into `StepRun.ID` (`runner.go:134`) and the store writes it as `step_id`, where `PRIMARY KEY (run_id, step_id)` lives (`runs.go:48-51`, `internal/store/schema.go:38`). A duplicate step ID that slipped past validation would make every `CreateRun` for that workflow fail on the second insert.

**L31–L31** — `Name string`, key `name`. Non-blank (L84-86). It is shown in the step list (`web/src/components/StepList.tsx:23`), copied into each `StepRun` (`runner.go:134`), and quoted in the run-level error when a progress save fails before the step (`runner.go:238`).

**L32–L32** — `Type string`, key `type`. One string drives three switches: the step decoder picks the config shape (`step_json.go:28`), `Validate` picks the rules (L87), and `checkExecutor` picks the executor (`cmd/server/main.go:153`). `docs/roadmap.md:99` names exactly these three switches as today's substitute for a node registry, and `AGENTS.md:129` says "There is no generic node registry."

**L33–L33** — `Config CheckConfig`, key `config`. A plain value, not a pointer and not an interface. Both check types share this one struct, and the decoder fills only the fields allowed for the step's `type` (`step_json.go:29-48`). The frontend expresses the same idea as a discriminated union (`api.ts:3-12`).

**L34–L34** — Closes `Step`.

**L35–L38** — The comment on `CheckConfig`. It records a deliberate, temporary design: HTTP and TCP fields sit side by side "as plain values", and two separate layers reject fields that belong to the other type. `AGENTS.md:124-125` states the same rule for anyone extending the app, and `docs/roadmap.md:101` plans to replace the shared struct with per-node structs inside the SSH increment, once real SSH fields are known. The last sentence names the two guards you will meet: `Step.UnmarshalJSON` for JSON input and `Validate` for programmatic input.

**L39–L39** — `type CheckConfig struct {` opens the shared configuration struct.

**L40–L41** — `URL` and `ExpectedStatus`, the HTTP fields, both `omitempty`. For a TCP step they hold zero values, and `omitempty` keeps them out of the encoded JSON, so the snapshot saved with a run and the API response keep the flat shape an author wrote. `tcp_test.go:90-103` round-trips a mixed file through `Load` and `json.Marshal` and requires the shape to be unchanged. The HTTP executor reads both (`http.go:39`, `http.go:42`, `http.go:67`).

**L42–L43** — `Host` and `Port`, the TCP fields, both `omitempty` for the mirror-image reason. The TCP executor reads them (`tcp.go:29`) and joins them with `net.JoinHostPort` (`tcp.go:34`), which adds IPv6 brackets itself. That is why `Validate` forbids brackets in `Host` (L120).

**L44–L44** — `TimeoutMS int` with key `timeoutMs` and no `omitempty`, because both check types require it and a timeout of 0 is never valid (L112-114). Each executor turns it into a context deadline (`http.go:40`, `tcp.go:30`), and the UI prints it (`StepList.tsx:41`).

**L45–L45** — Closes `CheckConfig`.

**L46–L48** — The comment on `CheckResult`. Two facts: the result is copied with each run snapshot (it is `StepRun.Output`, a `*workflow.CheckResult` at `runner.go:30`, encoded at `runs.go:42` and decoded back at `internal/store/run_reads.go:107`), and an absent `type` means HTTP. The second fact is the compatibility rule for history written before TCP existed. `AGENTS.md:125-126` repeats it ("a missing result type means legacy HTTP"), `README.md:588-591` explains the schema-version-3 marker, and the frontend applies it in `web/src/format.ts:71-73` and `api.ts:33-34`.

**L49–L49** — `type CheckResult struct {` opens the result type both executors return.

**L50–L50** — `Type string` with `type,omitempty`. The TCP executor sets it to `"tcp.check"` (`tcp.go:29`); the HTTP executor never sets it (`http.go:39`), so HTTP results encode exactly as they did before TCP support. This one tag is how schema version 3 could record TCP support "without rewriting saved JSON" (`AGENTS.md:127-129`). Removing `omitempty` here would make new HTTP rows differ from old ones for no reason.

**L51–L51** — `Healthy bool` with no `omitempty`. A `false` value must be written, because an unhealthy check is a real observation, not missing data. The README insists on the distinction between "Completed" (execution) and "Healthy" (service) (`README.md:236-239`), and the TypeScript type requires the key (`api.ts:28`).

**L52–L54** — `URL`, `ExpectedStatus`, and `StatusCode`, the HTTP result fields. The executor copies the first two from the config into the result (`http.go:39`), so a saved result still says what it checked after the file is edited. That upholds `AGENTS.md:126-127`: "Keep historical results independent of the currently loaded workflow." `StatusCode` is `omitempty`, so `0` (no response received) is omitted, and the UI shows `No response` when the key is absent (`format.ts:85`).

**L55–L56** — `Host` and `Port`, the TCP result fields, filled at `tcp.go:29` for the same self-description reason. `README.md:257-260` describes what a TCP result carries and why the UI reads the type from the saved result rather than the current workflow.

**L57–L57** — `DurationMS int64`. It is `int64` rather than `int` because `time.Duration.Milliseconds()` returns `int64` (`http.go:49`, `tcp.go:37`). No `omitempty`: a measured 0 ms is still a measurement.

**L58–L58** — `Reason string`, always written. Executors fill it with a short explanation (`http.go:58-71`, `tcp.go:46-53`); the one branch that embeds a network error's text passes through `resulttext.LimitReason`, which caps it at 4 KiB (`http.go:60`, `tcp.go:48`, `README.md:265-268`), so a long error cannot inflate a row.

**L59–L59** — Closes `CheckResult`.

**L60–L61** — `var identifier = regexp.MustCompile(...)`. The pattern `^[a-z0-9][a-z0-9_-]{0,63}$` means: one lowercase letter or digit, then up to 63 more lowercase letters, digits, underscores, or hyphens, so 1 to 64 characters in all. The anchors `^` and `$` require the whole string to match; Go's `$` matches only at the very end of the text, so a trailing newline fails. Compiled once at package initialization and used at L67 and L80. Lowercase-only IDs avoid two workflows that differ only by case, which would still be two different strings to `api.go:62` and to the `busyWorkflows` map (`runner.go:119`) but look identical to a person. A `*regexp.Regexp` is safe for concurrent use, which matters because `Validate` runs in HTTP handler goroutines and scheduler goroutines at the same time.

**L62–L63** — `func Validate(d Definition) error`. It takes the definition by value and returns `nil` for a valid one or the first problem found. Callers: `Load` (L181), `Runner.Start` on every start (`runner.go:106`), whether the start came from the API (`api.go:65`) or the scheduler (`scheduler.go:125`, wired to `runner.Start` at `main.go:116`), and the tests (`workflow_test.go:73`, `tcp_test.go:21`, `schedule_test.go:15`). Validating twice looks redundant, but it keeps the runner correct for any caller, which is what `AGENTS.md:131` means by "Keep backend validation authoritative." The function stops at the first error on purpose: one clear message naming the file and step is easier to act on than a list.

**L64–L66** — The schema version must be exactly `1`. Not `>= 1`, because a future version 2 file would carry fields this code does not understand, and the strict decoder would already have failed on them; the explicit check gives a clearer message. `workflow_test.go:63` covers a version of 2.

**L67–L69** — The workflow ID must match `identifier`, and the name must contain something other than whitespace. `strings.TrimSpace` rejects `"   "` as a name. Both checks share one error message because they are both "this file is not identifiable".

**L70–L74** — If a schedule is present, `NewTimetable(*d.Schedule)` must succeed. The `*` dereferences the pointer to pass the `Schedule` value; `_` discards the resulting `Timetable` because only the error matters here (the scheduler builds its own timetable later at `scheduler.go:62`). Any error from `schedule.go:29-56` is returned as is, since its messages already name the field at fault. Note that `NewTimetable` calls `time.LoadLocation` and `time.Now()` (`schedule.go:43`, `schedule.go:48`), so `Validate` is not a pure function; it can read the time zone database. An empty `"schedule": {}` object reaches this line as a non-nil pointer to a zero `Schedule` and is rejected with a `cron` error (`schedule_test.go:114-122`). Chapter 5 covers `NewTimetable` itself.

**L75–L77** — Between 1 and 20 steps. `len` of a nil slice is 0, so a missing `steps` key fails here with the same message as `"steps": []` (`workflow_test.go:64`, `workflow_test.go:107`). The upper bound caps how large a run's `StepRun` slice (`runner.go:128`) and its `run_steps` rows (`runs.go:38-56`) can get.

**L78–L78** — `seen := make(map[string]bool)` is a set of step IDs for this one workflow. It is created inside `Validate`, so step IDs only need to be unique within a file; two files may both have a step called `health` (`workflows/01-patchbay.json:8` and `workflows/scheduled-check.json:12` do).

**L79–L79** — `for _, step := range d.Steps {` visits each step in file order. `step` is a copy of the element; the loop only reads it, so that is fine.

**L80–L83** — The step ID must match `identifier` and not have been seen. The two conditions share one `||` so one message covers both, and `%q` prints the offending ID in quotes. L83 records the ID after the check. Without this, a duplicated step ID would reach the store and break the `(run_id, step_id)` primary key (`schema.go:38`) on every run of that workflow. `workflow_test.go:65` covers the duplicate case.

**L84–L86** — The step name must be non-blank, by the same `TrimSpace` test as the workflow name.

**L87–L87** — `switch step.Type {` selects the rules for this step's type. It is the second of the three type switches (`step_json.go:28`, L87, `main.go:153`).

**L88–L91** — For `http.check`, reject a non-empty `Host` or non-zero `Port`. Note the comparisons: `!= ""` and `!= 0`. A zero-valued TCP field passes this line, so this guard alone would not catch `"host": ""` in a file. That case is caught earlier, by the strict per-type decoder in `step_json.go:30-37`, which does not even have a `host` field for HTTP (`tcp_test.go:109-110`). This line exists for definitions built in Go code rather than decoded from JSON; `tcp_test.go:66-70` constructs one and expects rejection. Without it, an executor would silently ignore a stray field, hiding an author's mistake.

**L92–L95** — Parse the URL and demand five things: it parses; its scheme is `http` or `https` (so `/health` with no scheme and `file:///etc/passwd` both fail); `Hostname()` is non-empty (a scheme alone is not a destination); `u.User` is nil (no `user:pass@` credentials, which would otherwise be sent to the target and saved into history with the snapshot); and no `#fragment` (never sent to a server, so it could only mislead). `Hostname()` strips any port and IPv6 brackets, so `http://[::1]:8080/` is fine. The HTTP executor builds its GET from this string unchanged (`http.go:42`). `workflow_test.go:67-69` covers the relative, non-HTTP, and credential cases.

**L96–L98** — `ExpectedStatus` must be 100 to 599, the range of HTTP status codes. The executor compares the real status to it (`http.go:67`). `workflow_test.go:71` covers 999.

**L99–L102** — For `tcp.check`, reject a non-empty `URL` or non-zero `ExpectedStatus`, the mirror of L89-91. Same split of duties: the JSON decoder rejects even empty values (`tcp_test.go:111-112`), and this line guards programmatic construction (`tcp_test.go:47-48`).

**L103–L105** — The host must pass `validTCPHost` (L119-144). The error message lists exactly the forms the helper refuses, and `README.md:316-320` documents the same rules for authors. The helper returns a `bool` rather than an error because this one message describes every failure.

**L106–L108** — `Port` must be 1 to 65535. Port 0 means "any port" to the OS, which is meaningless for a check. The executor converts it with `strconv.Itoa` for `JoinHostPort` (`tcp.go:34`). `tcp_test.go:42-44` covers 0, -1, and 65536, and `tcp_test.go:58-64` confirms both bounds are accepted.

**L109–L111** — `default` rejects any other type. For a file this branch is unreachable, because `step_json.go:49-50` already rejected the type during decoding. It remains for definitions built in code (`workflow_test.go:66` uses `ssh.command`) and keeps the executor dispatch's own `default` at `main.go:158-159` unreachable in practice. L111 closes the switch.

**L112–L114** — The shared rule, after the switch: `TimeoutMS` must be 100 to 30,000 milliseconds. It sits outside the `switch` because both types need it. The lower bound avoids a check that can never succeed; the upper bound limits how long one step can hold an active-run slot (`README.md:137`) while a target stalls, since the executors wait up to this value (`http.go:40`, `tcp.go:30`). `workflow_test.go:70` and `tcp_test.go:45-46` cover 0, 99, and 30001.

**L115–L117** — End of the step loop, `return nil` for a valid definition, and the closing brace. Reaching L116 means every check above passed for every step.

**L118–L119** — `func validTCPHost(host string) bool` begins the TCP host helper. It is unexported, called only from L103, and tested through `Validate` in `TestValidateTCP` (`tcp_test.go:12-71`). It answers one question: can this string be handed to `net.JoinHostPort` and the dialer without surprising the author?

**L120–L124** — Two rejections in one condition. `strings.ContainsAny(host, "/\\[]@?#")` refuses any character that would turn the host into something else: `/` and `\` (a path), `[` and `]` (brackets that `JoinHostPort` adds itself, `tcp.go:32-34`), `@` (credentials), `?` and `#` (query and fragment). `strings.ContainsFunc` with an inline function literal refuses any whitespace or control character, including non-ASCII ones, which is why the file imports `unicode`. Either match returns `false`. `tcp_test.go:31-37` covers a URL, a port in the host, brackets, credentials, a path, leading whitespace, and whitespace in a zone.

**L125–L127** — If `netip.ParseAddr` succeeds, the host is an IP address and is accepted at once. The comment names two accepted forms: an unbracketed IPv6 address such as `::1`, and an address with an interface zone such as `fe80::1%eth0`, which the older `net.ParseIP` would reject. `tcp_test.go:18` lists the accepted addresses. `[::1]` cannot reach this line because L120 already rejected the brackets.

**L128–L132** — Everything else must be a DNS name. `strings.TrimSuffix(host, ".")` removes one trailing dot, the absolute-name form, so `nas.home.` is allowed (`tcp_test.go:18`). The remaining name must be 1 to 253 characters, the maximum length of a textual DNS name. `tcp_test.go:41` builds a 255-character name to hit the upper bound.

**L133–L136** — Split the name on dots and check each label: 1 to 63 characters, and neither the first nor the last byte is a hyphen. The order inside the condition matters: `len(label) == 0` is tested first, and `||` stops at the first true operand, so `label[0]` is never evaluated on an empty label and cannot panic. `label[0]` indexes a byte, which is correct here because only ASCII is allowed by the next loop. `tcp_test.go:38-40` covers an empty label (`nas..home`), a leading hyphen, and a 64-character label.

**L137–L141** — `for _, c := range label` iterates runes, and each must be an ASCII letter, digit, or hyphen. `&&` binds tighter than `||`, so the condition reads as four alternatives. Underscores, spaces, and non-ASCII letters all fail, so internationalized domain names are not supported; `README.md:318-319` states the ASCII rule. Uppercase is allowed because DNS is case-insensitive (`NAS.home` at `tcp_test.go:18`).

**L142–L144** — Close the label loop and the outer loop, `return true` for a well-formed name, and close the function. Note that nothing here resolves the name; a syntactically valid name that does not exist becomes an unhealthy result at run time (`tcp.go:44-50`), not a validation error.

**L145–L147** — The comment and `func Load(directory string) ([]Definition, error)`. "Once at startup" is the whole lifecycle: `main.go:58` is the only production caller, there is no file watcher or write API (`docs/m0-walkthrough.md:41-43`), and the function takes no `context.Context` because it runs before anything that could cancel it. Tests call it with a `t.TempDir()` (`workflow_test.go:44`, `workflow_test.go:97`, `workflow_test.go:115`, `tcp_test.go:83`, `schedule_test.go:103`, `schedule_test.go:120`, `cmd/server/checks_test.go:50`, `cmd/server/main_test.go:99`, `cmd/server/database_write_test.go:34`), so no test depends on the real `workflows/` directory.

**L148–L151** — `os.ReadDir` lists the directory, sorted by file name. That sort order is why `workflows/01-patchbay.json` is always the first workflow in the sidebar (chapter 2). The error is wrapped with `read workflows: %w`; the inner `*PathError` already names the directory, and `main.go:60` adds `invalid workflow configuration:` on top, so a wrong `-workflows` path produces a message with all three parts.

**L152–L153** — `definitions := make([]Definition, 0)` starts an empty, non-nil slice, and `seen` is a second set, this time of workflow IDs across all files. The function errors when `definitions` stays empty (L190), so the nil-versus-empty distinction does not reach a caller; the explicit `make` just states intent.

**L154–L157** — Loop over the entries and skip directories and anything whose extension is not `.json`. `entry.IsDir()` needs no extra system call. `filepath.Ext` returns the extension including the dot and is case-sensitive, so `backup.JSON`, `notes.md`, and `old.json.bak` are all silently ignored, and subdirectories are never entered. This lets the directory hold other files without breaking startup, but it also means a misspelled extension does not warn.

**L158–L162** — Build the full path with `filepath.Join` (correct separators on every OS) and open the file read-only. An `os.Open` error is returned unwrapped because the `*PathError` it returns already contains the path and operation; adding a prefix would repeat it.

**L163–L168** — The bounded read. `io.LimitReader(file, maxDefinitionBytes+1)` lets at most 65,537 bytes through, and `io.ReadAll` collects them. The comment explains the `+1`: reading one byte more than the limit is how L169 can tell "exactly 64 KiB" from "more than 64 KiB" without reading a large file to the end. `file.Close()` runs immediately on the next line, before the error check, so the file is closed on every path. The comment says why it is not deferred: `defer` would postpone every close until `Load` returns, holding one descriptor per file for the whole loop. The close error is ignored because a read-only file has nothing to flush. `README.md:336-337` describes this behavior to users.

**L169–L171** — If more than `maxDefinitionBytes` arrived, the file is too large. The message hard-codes `64 KiB` and includes the path; `TestLoadDefinitionSizeLimit` checks that exact string (`workflow_test.go:46`) and confirms a file of exactly 64 KiB still loads (`workflow_test.go:31`). The one-byte-over case exists to catch a tempting bug: reading exactly 64 KiB and accepting a truncated prefix that happens to be valid JSON (`workflow_test.go:38-39`).

**L172–L174** — Declare a zero `Definition`, wrap the bytes in a `json.Decoder`, and enable `DisallowUnknownFields`. A `Decoder` is used instead of `json.Unmarshal` because only the `Decoder` offers the unknown-field setting and the ability to look for trailing data (L178). The strictness is a product decision: a typo such as `"timeoutMS"` becomes a startup error rather than a silently dropped setting (`README.md:329-330`), matching the roadmap's "Reject duplicate keys and unknown fields" (`docs/roadmap.md:59`). Two limits to know: the setting does not reach into `Step.UnmarshalJSON`, which is why `step_json.go:94` enables it again for step objects; and `encoding/json` does not reject duplicate keys at this level, so only step objects get duplicate detection (`step_json.go:58-90`). `strings.NewReader(string(data))` copies the bytes into a string; the sibling helper in `step_json.go:93` uses `bytes.NewReader` for the same job without the copy. Both behave identically.

**L175–L177** — `decoder.Decode(&d)` fills the struct through a pointer; without `&`, the decoder would receive a copy and `d` would stay empty. Any decode error, including one raised inside `Step.UnmarshalJSON`, is wrapped with the file path so the startup log says which file failed. `workflow_test.go:105` (unknown field) and `workflow_test.go:108` (not JSON) exercise this line.

**L178–L180** — Decode again into `new(any)` and require `io.EOF`. If the file held a second JSON value after the first object, this `Decode` would succeed instead, and the file is rejected with `expected exactly one JSON document`. Without it, `{"schemaVersion":1,...} {"id":"second-document"}` would load the first object and silently ignore the second (`workflow_test.go:106`). Comparing `err != io.EOF` directly is correct here because `Decoder.Decode` documents returning `io.EOF` itself.

**L181–L183** — Run `Validate` on the decoded definition and wrap any error with the path. This is the first of the two validation passes; the second happens at every `Runner.Start` (`runner.go:106`). `README.md:329-333` lists what gets rejected and promises that "No workflow is executed during loading", which holds because nothing in this function or in `Validate` executes anything.

**L184–L187** — Reject a workflow ID already loaded from an earlier file, then record it. The error names the second file, the one that collided. This is the line that fails startup when a template from `examples/` is copied without changing its `id` (chapter 2 walks through that case).

**L188–L189** — Append the definition and close the loop. The slice grows in `os.ReadDir` order, which the API preserves (`api.go:38-39`).

**L190–L192** — If no file was loaded, fail. A directory with no `.json` files, or only directories, reaches here. `README.md:296-297` documents it: "M0 still rejects an empty workflow directory." A server with zero workflows could start but would be useless, and the most likely cause is a wrong `-workflows` path, so a loud failure is more helpful.

**L193–L194** — Return the definitions and close the function. From here the slice is owned by `runServer` in `main.go:58` and shared, unchanged, until the process exits.

## State, concurrency, and cancellation

This file creates no goroutines, holds no mutex, and takes no `context.Context`. That is the point of its package comment (L1-2). What it does determine is who owns the data it produces and under which conditions the rest of the system may read it.

**Ownership of the definitions slice.** `Load` runs in the main goroutine inside `runServer` (`main.go:58`), before the database lock, the runner, the scheduler, or the HTTP server exist. The returned `[]Definition` is then captured by three readers: the API handler closures (`api.go:32`, read at `api.go:38-39` and `api.go:61-65`), the scheduler, which copies each scheduled definition into its own entry (`scheduler.go:58-66`), and, indirectly, every run. Nobody writes to the slice after `Load` returns. That is the only reason the sharing is safe without a lock: the Go race detector would flag any later mutation, and nothing in the repository performs one. If a future write API ever replaced definitions at run time, this slice would need an owner and a lock.

**Copies at the run boundary.** `Runner.Start` receives a `Definition` by value, but the value still shares the `Steps` backing array and the `Schedule` pointer with the loaded slice. So the runner clones the steps and copies the schedule struct before keeping the definition in a `queuedRun` (`runner.go:109-113`, `runner.go:82-85`). This upholds the rule from `AGENTS.md:80-82`: "Return copied snapshots." The reader should notice that the copy is needed precisely because `Definition` contains a slice and a pointer, which is why `AGENTS.md:130` says to "revisit snapshot copying if adding nested pointers" to these types.

**Validate under concurrency.** `Validate` runs concurrently in several goroutines: each HTTP request handler that starts a run (`api.go:65` to `runner.go:106`) and each scheduler loop (`scheduler.go:125` to the same line). It is safe because it reads only its argument, allocates its `seen` map locally, and uses the package-level `identifier`, which `regexp` documents as safe for concurrent use. `Runner.Start` calls `Validate` before taking `r.mu` (`runner.go:106` versus `runner.go:114`). That keeps parsing and regex work out of the critical section, which is what the rule in `AGENTS.md:78-79` protects: "Keep admission, capacity, queue promotion, and same-workflow overlap checks under the runner's mutex." Validation is not one of those checks, so it stays outside the lock.

**Contexts and cancellation.** `Load` has no context. Its file reads cannot be canceled, and the signal context created at `main.go:38` is not consulted until later in `runServer`. In practice the work is a few small files and finishes in milliseconds; a Ctrl+C during `Load` takes effect as soon as `Load` returns. `Validate` has no context either, but `NewTimetable` inside it calls `time.LoadLocation` and `time.Now()` (`schedule.go:43`, `schedule.go:48`), so a scheduled definition's validation depends on the host's time zone database being readable. The executors that later consume `CheckConfig` do take a context and derive a per-step deadline from `TimeoutMS` (`http.go:40`, `tcp.go:30`); the bounds enforced at L112-114 are what make that deadline sane.

**Failure paths.** Every `return nil, err` in `Load` ends the process: `main.go:59-61` wraps the error, `main.go:40-43` logs `patchbay failed` and exits with status 1. Because `Load` runs before `os.MkdirAll` for the database directory (`main.go:62`) and before `lockDatabase`, a bad workflow file never touches the database. The specific failures, in the order the code can hit them: the directory cannot be read (L149-151); a file cannot be opened (L160-162); a read fails (L166-168); the file exceeds 64 KiB (L169-171); the JSON is malformed, has an unknown field, or a step fails its own decoder (L175-177); the file holds more than one JSON value (L178-180); `Validate` rejects it (L181-183); the ID repeats an earlier file (L184-187); or no file loaded at all (L190-192). At run time, a `Validate` failure in `Runner.Start` returns the error to the caller; the API maps it to HTTP 400 (`api.go:66-67`) and the scheduler logs a skip. Both are theoretical for file-loaded definitions, which already passed `Load`, but they protect any future caller that builds a definition in code.

**Rules from "Execution and persistence rules" that this code implements.**

- `AGENTS.md:83-84`: "Save a new run and its workflow snapshot before acceptance." The snapshot is a `Definition` from this file, encoded whole by `store.CreateRun` (`runs.go:18`). The 64 KiB cap at L18 keeps that snapshot bounded for every run.
- `AGENTS.md:100`: "Scheduled starts call `Runner.Start` exactly as the API does." Because both paths converge on `runner.go:106`, both receive the same `Validate` pass; this file does not need to know which caller it serves.
- `AGENTS.md:103-104`: "Preserve atomic run/step snapshots and existing data. Introduce deliberate versioned migrations when stored structure changes." The `omitempty` on `CheckResult.Type` (L50) is the design that let TCP results join history without rewriting a single saved HTTP row; schema version 3 only changed the version marker (`README.md:588-591`).
- `AGENTS.md:80-82`: "Return copied snapshots." Satisfied by the runner's copies of the slice and pointer fields defined at L25-26, as described above.

## Check yourself

1. A workflow file contains an HTTP step with `"host": ""` inside its `config`. Line 89 only rejects a non-empty host. Why is the file still refused, and which line does it?

<details><summary>Answer</summary>
The strict per-type decoder refuses it first. <code>Step.UnmarshalJSON</code> decodes an HTTP config into a struct that has only <code>url</code>, <code>expectedStatus</code>, and <code>timeoutMs</code> (<code>step_json.go:30-34</code>) with <code>DisallowUnknownFields</code> on (<code>step_json.go:94</code>), so <code>host</code> is an unknown field regardless of its value. <code>tcp_test.go:109</code> covers exactly this input. Line 89 exists for definitions built in Go code, which never pass through the decoder; <code>tcp_test.go:66-70</code> tests that path. The comment at L36-38 names both guards.
</details>

2. Why does `Load` read `maxDefinitionBytes+1` bytes rather than `maxDefinitionBytes`, and what bug would the shorter read introduce?

<details><summary>Answer</summary>
With a limit of exactly 64 KiB, <code>io.ReadAll</code> would return 65,536 bytes for any file of that size or larger, and the check <code>len(data) &gt; maxDefinitionBytes</code> at L169 could never be true. A 70 KiB file whose JSON object ends before the cut, followed by whitespace, would decode successfully from its truncated prefix and be accepted silently. Reading one extra byte makes "more than the limit" observable without reading the whole file. The test comment at <code>workflow_test.go:38-39</code> describes this trap, and the <code>one byte over limit</code> case guards it.
</details>

3. The server is running. You edit `workflows/scheduled-check.json`, and two minutes later its schedule fires. Which definition runs, and which validation runs?

<details><summary>Answer</summary>
The definition loaded at startup. <code>Load</code> runs once (<code>main.go:58</code>), the scheduler copied that definition into its entry at <code>scheduler.go:66</code>, and nothing re-reads the file. When the occurrence fires, the scheduler calls <code>runner.Start</code> with the old definition (<code>scheduler.go:125</code>), which calls <code>Validate</code> again at <code>runner.go:106</code>. The edited file is only read, and only validated by <code>Load</code>, at the next restart.
</details>

4. Which line stops two steps in one workflow from sharing an ID, and what would fail downstream if that line were deleted?

<details><summary>Answer</summary>
L80-83: <code>seen[step.ID]</code> rejects the second occurrence. Without it, <code>Load</code> would accept the file, and every <code>Runner.Start</code> for that workflow would build a <code>Run</code> with two <code>StepRun</code> entries carrying the same ID (<code>runner.go:133-135</code>). <code>store.CreateRun</code> would then insert both into <code>run_steps</code>, whose primary key is <code>(run_id, step_id)</code> (<code>schema.go:38</code>), so the second insert fails, the transaction rolls back (<code>runs.go:27</code>), and the API answers 500 with <code>Could not save run. Check server logs.</code> (<code>api.go:75-78</code>) for every start. The workflow would be loadable but never runnable.
</details>

5. `CheckResult.Type` has `omitempty` but `CheckResult.Healthy` does not. Explain both choices.

<details><summary>Answer</summary>
<code>Type</code> is omitted when empty so that an HTTP result encodes exactly as it did before TCP support existed; a saved row without <code>type</code> means HTTP (L47-48, <code>AGENTS.md:125-126</code>), and new HTTP rows must look the same so old and new history are read by one rule (<code>format.ts:71-73</code>). <code>Healthy</code> must always be written because <code>false</code> is a real observation: an unhealthy check completed its job (<code>README.md:236-239</code>). With <code>omitempty</code>, an unhealthy result would lose the key, and a reader could not tell "unhealthy" from "no result".
</details>

6. Why does `Load` call `file.Close()` on L165 instead of writing `defer file.Close()` after `os.Open`?

<details><summary>Answer</summary>
<code>defer</code> runs when <code>Load</code> returns, not when the loop iteration ends. With a deferred close, every file in the directory would stay open until the last one was processed, which wastes descriptors and, for a large directory, could hit the process limit. Closing immediately after the read, before the error check, guarantees each file is closed on every path. The comment on L165 states this. For a read-only file the close error carries no information, so it is ignored.
</details>

## Experiments

1. **Remove the detection byte.** On line 164 change `maxDefinitionBytes+1` to `maxDefinitionBytes`. Run `go test ./internal/workflow -run TestLoadDefinitionSizeLimit`. Expected: the subtests `one_byte_over_limit` and `much_larger_file` fail with `expected size rejection with the file path`, because the truncated prefix is still valid JSON and L169 can no longer see the overflow. The three smaller cases keep passing. Restore the `+1` and rerun; all five pass.

2. **Loosen the DNS length bound.** On line 130 change `253` to `255`. Run `go test ./internal/workflow -run TestValidateTCP`. Expected: `TestValidateTCP/long_hostname` fails with `invalid TCP config accepted`, since its 255-character name (`tcp_test.go:41`) now passes. Restore `253`; the test passes again.

3. **Point the server at an empty directory.** Run `mkdir -p /tmp/empty-workflows && go run ./cmd/server -workflows /tmp/empty-workflows`. Expected: the process exits with status 1 after one `patchbay failed` log line whose `error` value is `invalid workflow configuration: no workflow JSON files found in /tmp/empty-workflows` (the default `slog` handler prints the level, the message, then `error="..."`), produced by L190-192, wrapped by `main.go:60`, and logged by `main.go:41`. No database is opened, because `Load` runs before `main.go:62`. Remove the directory afterwards.

4. **Watch the load order.** Insert `fmt.Println("loaded", path, d.ID)` after line 187. Run `go run ./cmd/server`. Expected: five lines appear before the server starts, in alphabetical file order (`01-patchbay.json`, `http-check.json`, `scheduled-check.json`, `sequence.json`, `tcp-check.json`), matching the sidebar order in the browser. Stop the server with Ctrl+C and delete the line.

## Next

Chapter 5, "Strict step decoding and cron schedules" ([05-workflow-step-json-and-schedule.md](05-workflow-step-json-and-schedule.md))
