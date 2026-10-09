# 11. The HTTP API and the request-host guard

Files: [internal/httpapi/api.go](../../internal/httpapi/api.go), [internal/httpapi/host_guard.go](../../internal/httpapi/host_guard.go)

Revision: commit 1c4786e

## Role in the system

The `httpapi` package is the only part of the backend that speaks HTTP. Its doc comment says what it is for: it "translates HTTP requests into execution and saved-history operations" (`api.go:1`), and the roadmap gives it one line in the package map: "Request parsing and response mapping" (`docs/roadmap.md:201`). It owns no state. Every handler reads a request, calls one method on something another package owns (the runner, the store, the scheduler), and writes a JSON response. `api.go` builds the five routes the README documents (`README.md:430-436`) plus the static file server for the built frontend. `host_guard.go` is a wrapper that runs before all of that and refuses any request whose `Host` is not a name this server is willing to answer for.

On the path from a browser click to a saved run, these files are the front door. The click calls `startMutation` in `web/src/pages/WorkflowsPage.tsx:54-59`, which sends `POST /api/workflows/{id}/runs` with `Content-Type: application/json` and no body. The Go server built at `cmd/server/main.go:122-126` receives it and hands it to `hostGuard.Wrap(httpapi.New(...))` (`main.go:123`). The guard checks the host (`host_guard.go:41-42`), the wrapper at `api.go:135-139` adds two safety headers, the mux picks the handler at `api.go:50`, and that handler calls `runner.Start` (`api.go:65`). `Start` validates, admits, and saves the new run through `store.CreateRun` (`internal/engine/runner.go:105-157`, `main.go:107-109`), then returns a copy. The handler answers `202 Accepted` with a `Location` header (`api.go:83-84`). From then on the run belongs to the runner, not to this request. The browser polls `GET /api/runs` once a second (`WorkflowsPage.tsx:47-51`); that handler reads SQLite through `store.ListRuns` and overlays any result the runner could not save (`api.go:89-102`, `api.go:144-149`).

The host guard exists because this prototype has no login. A web page on another site cannot read responses from `127.0.0.1:8080` directly, but with DNS rebinding it can make the browser send requests there: the attacker's domain first resolves to the attacker's server, then its DNS record is switched to `127.0.0.1`, and the browser keeps sending requests with `Host: attacker.example` to what is now your local Patchbay. Checking `Host` against a fixed allowlist defeats that, which is why the README says the guard "helps protect the local application against DNS rebinding" (`README.md:109-111`) and why Go's own documentation for `Request.Host` recommends exactly this check. The guard is not authentication (`README.md:132-133`), and `AGENTS.md:113-114` makes keeping it a rule: "Keep the unauthenticated prototype local, host validation enabled, and the JSON requirement on run-start requests. Host validation is not authentication." Real authentication is a later milestone (`docs/roadmap.md:153`).

## Concepts you need

- **`http.Handler` and `http.HandlerFunc`.** A handler is any value with a `ServeHTTP(w http.ResponseWriter, r *http.Request)` method. `http.HandlerFunc` is a function type that has that method, so you can turn a plain function into a handler by converting it: `http.HandlerFunc(func(w, r) {...})`. `New` returns one (`api.go:135`) and `Wrap` returns one (`host_guard.go:40`). [http.Handler](https://pkg.go.dev/net/http#Handler), [http.HandlerFunc](https://pkg.go.dev/net/http#HandlerFunc)
- **Middleware: wrapping one handler in another.** A function that takes a handler and returns a handler can run code before or after the inner one, or skip it. `HostGuard.Wrap` is middleware: it checks the host, and only then calls `next.ServeHTTP` (`host_guard.go:48`). The order of wrapping decides the order of execution; `main.go:123` puts the guard outside everything else. [Go blog: HTTP middleware (Effective Go on handlers)](https://go.dev/doc/effective_go#web_server)
- **`http.ServeMux` method patterns and wildcards.** Since Go 1.22 a route pattern can start with a method (`GET /api/health`) and contain `{name}` wildcards that match one path segment. `r.PathValue("id")` returns what the wildcard matched. When several patterns match, the most specific wins; a pattern ending in `/` matches the whole subtree; a `GET` pattern also matches `HEAD`. [http.ServeMux](https://pkg.go.dev/net/http#ServeMux), [Request.PathValue](https://pkg.go.dev/net/http#Request.PathValue)
- **Closures capturing variables.** Every handler in `New` is a function literal that uses `definitions`, `runner`, `schedules`, `db`, and `webDir` from the enclosing call. Those variables stay alive as long as the handlers do, so `New` is called once at startup and the handlers keep their dependencies without a global. [A Tour of Go: Function closures](https://go.dev/tour/moretypes/25)
- **Interfaces and the nil interface.** An interface value holds a type and a value. `NextRunSource` (`api.go:21`) has one method, so `*schedule.Scheduler` satisfies it without saying so. Comparing an interface to `nil` is `true` only when both parts are nil: a nil `*Scheduler` stored in the interface is not nil, and calling a method on it would panic. Tests pass a plain `nil`, and `main` never passes a nil pointer. [A Tour of Go: Interfaces](https://go.dev/tour/methods/9), [Go FAQ: Why is my nil error value not equal to nil?](https://go.dev/doc/faq#nil_error)
- **Struct embedding and JSON flattening.** Writing `workflow.Definition` inside a struct with no field name embeds it: its fields are promoted to the outer struct, and `encoding/json` writes them at the top level of the object. `workflowSummary` (`api.go:25-28`) is a `Definition` plus one extra key. [Effective Go: Embedding](https://go.dev/doc/effective_go#embedding), [encoding/json: Marshal](https://pkg.go.dev/encoding/json#Marshal)
- **Struct tags, `omitempty`, and pointer fields.** The string after a field, like `` `json:"nextRunAt,omitempty"` ``, tells `encoding/json` the key name and that an empty value (nil pointer, empty string, zero number, empty slice) should be left out. A zero `time.Time` is a struct and is never "empty" for `omitempty`; that is why `NextRunAt` is a `*time.Time`, so "no next run" can be `nil`. The engine uses the newer `omitzero` for a value field (`runner.go:41`). [encoding/json: Marshal](https://pkg.go.dev/encoding/json#Marshal)
- **`errors.Is` and `%w` wrapping.** `fmt.Errorf("...: %w", err)` builds a new error that still contains `err`. `errors.Is(err, target)` walks that chain, so a sentinel like `engine.ErrCapacity` is found even when `Start` wrapped it with extra text (`runner.go:124`). `==` would miss it. [errors.Is](https://pkg.go.dev/errors#Is), [fmt: Printing (the %w verb)](https://pkg.go.dev/fmt#hdr-Printing)
- **`context.WithTimeout`, `defer cancel()`, and `r.Context()`.** A request's context is canceled when the client disconnects, when the request is canceled (HTTP/2), or when the handler returns. `context.WithTimeout` derives a child that also ends after a deadline, and `defer cancel()` releases its timer when the handler returns. The reads use this; the run start deliberately does not. [context.WithTimeout](https://pkg.go.dev/context#WithTimeout), [Request.Context](https://pkg.go.dev/net/http#Request.Context)
- **`defer`.** A deferred call runs when the surrounding function returns, on every path. `defer cancel()` right after creating a context is the standard way to make sure the context's resources are freed. [A Tour of Go: Defer](https://go.dev/tour/flowcontrol/12)
- **`log/slog` structured logging.** `slog.Error("message", "key", value, ...)` writes a message plus key-value pairs. The default handler prints through the standard `log` package, which is why a test can capture it with `log.SetOutput` (`internal/httpapi/api_test.go:111-114`). [log/slog](https://pkg.go.dev/log/slog)
- **`ResponseWriter` header ordering.** `w.Header().Set(...)` edits a map; `w.WriteHeader(status)` sends the status line and that map. Header changes after `WriteHeader` or the first `Write` are ignored. Every handler here sets headers first, and `writeJSON` sets `Content-Type` before `WriteHeader`. [http.ResponseWriter](https://pkg.go.dev/net/http#ResponseWriter)
- **`json.NewEncoder(w).Encode(v)`.** An encoder writes JSON straight to an `io.Writer` and appends a newline. Tests trim that newline before comparing bodies. [json.NewEncoder](https://pkg.go.dev/encoding/json#NewEncoder)
- **`http.FileServer` and `http.Dir`.** `http.Dir("web/dist")` is a filesystem rooted at that directory; it cleans paths so `..` cannot climb out. `http.FileServer` serves files from it, serves `index.html` for a directory, and lists a directory that has no `index.html`. [http.FileServer](https://pkg.go.dev/net/http#FileServer), [http.Dir](https://pkg.go.dev/net/http#Dir)
- **`http.Error`.** Writes a plain-text error with the given status and sets `Content-Type: text/plain`. The frontend handler uses it; the API handlers use the package's own JSON `writeError` instead. [http.Error](https://pkg.go.dev/net/http#Error)
- **`os.Stat` and `filepath.Join`.** `Stat` returns file information or an error if the file does not exist. `filepath.Join` joins path parts with the operating system's separator. [os.Stat](https://pkg.go.dev/os#Stat), [filepath.Join](https://pkg.go.dev/path/filepath#Join)
- **`for i := range slice` to write back.** `for _, v := range` gives you a copy of each element; assigning to `v` changes nothing. `for i := range` with `runs[i] = ...` changes the slice. `api.go:98-100` needs the second form. [A Tour of Go: Range](https://go.dev/tour/moretypes/16)
- **The comma-ok idiom.** A map lookup, a type assertion, and some functions return a value plus a `bool` that says whether the value is real: `next, ok := schedules.NextRun(id)`, `live, ok := runner.Get(id)`, `host, ok := requestHostname(...)`. Reading a missing map key gives the zero value and `false`. [A Tour of Go: Maps](https://go.dev/tour/moretypes/22)
- **Maps as sets, and concurrent reads.** `map[string]bool` with `true` values is a set; a missing key reads as `false`. A map may be read by many goroutines at once as long as nobody writes to it; a concurrent write is a fatal race. `HostGuard.allowed` is written only during construction. [Go blog: Go maps in action (concurrency)](https://go.dev/blog/maps)
- **Pointer receivers.** `func (guard *HostGuard) Wrap(...)` declares a method on `*HostGuard`. A pointer receiver avoids copying the struct and is the usual choice for types that hold a map. [A Tour of Go: Pointer receivers](https://go.dev/tour/methods/4)
- **`net.SplitHostPort`.** Splits `"host:port"` or `"[v6]:port"` into host and port, removing brackets. It returns an error when there is no port or too many colons; it does not validate the host's characters. [net.SplitHostPort](https://pkg.go.dev/net#SplitHostPort)
- **`net/netip`.** `netip.ParseAddr` parses an IPv4 or IPv6 literal, including a zone suffix like `%lo0`. `Addr.Is6`, `Addr.Zone`, `Addr.Unmap` (turn `::ffff:1.2.3.4` into `1.2.3.4`), `Addr.IsUnspecified` (`0.0.0.0` or `::`), and `Addr.String` (the canonical spelling) are the methods used here. [netip.ParseAddr](https://pkg.go.dev/net/netip#ParseAddr), [netip.Addr](https://pkg.go.dev/net/netip#Addr)
- **`strconv.ParseUint`.** Parses a string as an unsigned integer in the given base and bit size; `ParseUint(port, 10, 16)` fails for an empty string, letters, or anything above 65535. [strconv.ParseUint](https://pkg.go.dev/strconv#ParseUint)
- **`strings` helpers.** `TrimSpace`, `Split`, `HasPrefix`, `HasSuffix`, `TrimSuffix`, `Contains`, `ContainsAny`, and `ToLower` do what their names say and never modify their input; strings are immutable, so each returns a new string. [strings](https://pkg.go.dev/strings)
- **Bytes versus runes in strings.** `label[0]` is a byte; `for _, char := range label` yields Unicode code points (runes). A non-ASCII character is several bytes but one rune, and the rune check at `host_guard.go:100` rejects it. Slicing `host[1 : len(host)-1]` works on bytes. [Go blog: Strings, bytes, runes and characters](https://go.dev/blog/strings)
- **Operator precedence.** `&&` binds tighter than `||`, so `a && b || c && d || e` means `(a && b) || (c && d) || e`. `host_guard.go:100` relies on this. [Spec: Operator precedence](https://go.dev/ref/spec#Operator_precedence)
- **Table-driven tests and `httptest`.** The guard's tests are a slice of cases run through `t.Run`, each building a request with `httptest.NewRequest` and recording the response with `httptest.NewRecorder`. The experiments below use them. [Go wiki: Table-driven tests](https://go.dev/wiki/TableDrivenTests), [net/http/httptest](https://pkg.go.dev/net/http/httptest)

## Walkthrough

### internal/httpapi/api.go

**L1–L2** — The package doc comment and clause. "Translates HTTP requests into execution and saved-history operations" is the whole contract: this package maps, it does not run checks or write rows. The roadmap's package map says the same (`docs/roadmap.md:201`). Both source files and the four test files share the package, which is why `host_guard.go:45` can call `writeError` from this file without an import.

**L3–L3** — Blank line.

**L4–L18** — Imports. `context` builds the read timeouts (L90, L104). `database/sql` gives the `*sql.DB` parameter (L32) and the `sql.ErrNoRows` sentinel (L108). `encoding/json` encodes responses (L154). `errors` is for `errors.Is` (L69-75, L108). `log/slog` logs the failures whose details must stay out of responses (L77, L94, L113). `net/http` is the server library. `os` and `path/filepath` check for `index.html` (L129). `time` appears in the interface (L22) and the timeouts. The project imports are `engine` (the runner, the `Run` type, the four sentinel errors), `store` (`ListRuns`, `GetRun`), and `workflow` (`Definition`). The import graph has no cycles: `httpapi` imports `engine`, `store`, `workflow`; `store` imports `engine` and `workflow`; `engine` imports `workflow`. Only the `main` package imports `httpapi` (`cmd/server/main.go:20`, `cmd/server/checks_test.go:18`). Note what is missing: `schedule`. L20-23 explains how the scheduler still gets in.

**L19–L19** — Blank line.

**L20–L23** — `NextRunSource` is an interface with one method, `NextRun(workflowID string) (time.Time, bool)`. `*schedule.Scheduler` has exactly that method (`internal/schedule/scheduler.go:105`), so `main.go:123` passes `scheduler` here without `httpapi` importing `schedule`. The payoff is testability: `schedule_test.go:15-20` defines `fakeNextRuns`, a map with a `NextRun` method, and other tests pass `nil`. The doc comment at L31 promises that `nil` is fine. The `(time.Time, bool)` shape is the comma-ok idiom: `false` means "this workflow has no next occurrence", which the scheduler reports for unscheduled workflows and for schedules with no occurrence left (`scheduler.go:103-110`).

**L24–L24** — Blank line.

**L25–L28** — `workflowSummary` is what `GET /api/workflows` returns for each workflow. The embedded `workflow.Definition` (no field name) promotes the definition's fields, and `encoding/json` writes them at the top level: `schemaVersion`, `id`, `name`, `description`, `schedule` (omitted when nil, `internal/workflow/workflow.go:25`), `steps`. The one extra field, `NextRunAt *time.Time` with `json:"nextRunAt,omitempty"`, is a pointer so that "no next run" is `nil` and the key disappears; a `time.Time` value would always be written, zero or not, because `omitempty` does not treat a zero struct as empty. The frontend type mirrors this exactly: `web/src/api.ts:16-25`, with the comment at `api.ts:23` "Present only while the server has a next occurrence for the schedule." The type is unexported because nothing outside this package builds one.

**L29–L29** — Blank line.

**L30–L32** — The doc comment and signature of `New`. The parameters are the dependencies every handler needs: `definitions` loaded once at startup (`main.go:58`), the single `runner` (`main.go:107`), the `schedules` source that may be nil, the open `db` (`main.go:75`), and `webDir` from the `-web` flag (`main.go:31`). It returns the `http.Handler` interface rather than `*http.ServeMux`; the caller only needs `ServeHTTP`, and the value actually returned is the wrapper at L135, not the mux. `main.go:123` calls it once. Every API test calls it too (`api_test.go:54`, `api_test.go:128`, `history_test.go:189`).

**L33–L33** — `http.NewServeMux()` creates the router. The mux is a local variable; after `New` returns, only the closure at L135 can reach it, so no code elsewhere can add routes. The patterns registered below use Go 1.22 syntax: method, space, path, with `{id}` wildcards.

**L34–L36** — `GET /api/health` answers a fixed `{"status":"ok"}`. The README is precise about what that means: "App readiness, not monitored-service health" (`README.md:432`). Three things depend on it: Docker's health check (`compose.yaml:14`), Playwright's wait for the server (`web/playwright.config.ts:15`), and the bundled `patchbay-health` workflow, which checks the app's own endpoint so the demo works offline (`README.md:23-24`). The `map[string]string` encodes as a JSON object. `r` is unused; Go allows unused parameters, only unused local variables are errors. Because the pattern says `GET`, a `HEAD` request is served by this handler as well.

**L37–L38** — `GET /api/workflows` begins. `make([]workflowSummary, 0, len(definitions))` creates an empty but non-nil slice with room for every workflow. Non-nil matters: a nil slice encodes as `null`, an empty one as `[]`, and the frontend's request is typed as `Workflow[]` (`WorkflowsPage.tsx:44`). Chapter 10 made the same point for `run_reads.go:61`.

**L39–L40** — Loop over the definitions. `definition` is a copy of each `Definition` struct; the `Steps` slice header and the `Schedule` pointer inside it still point at the loaded data, which is fine because this handler only reads. `workflowSummary{Definition: definition}` fills the embedded field, which is named after its type.

**L41–L45** — Ask the schedule source, if there is one. `schedules != nil` is the nil-interface check from the concepts list; passing a plain `nil` makes it false, and the summary keeps `NextRunAt == nil`. Otherwise `next, ok := schedules.NextRun(definition.ID)` and, when `ok`, `summary.NextRunAt = &next`. `next` is declared fresh by `:=` inside this block on every iteration, so each summary gets a pointer to its own time. The scheduler reports the time in the schedule's own zone (`scheduler.go:103`), and `time.Time` encodes as RFC 3339 with that offset; the README describes the result (`README.md:388-389`) and `web/src/format.ts:32-34` turns it into the browser's local time. `TestWorkflowListIncludesScheduleAndNextRun` checks both the scheduled and the unscheduled case (`schedule_test.go:58-67`) and the nil source (`schedule_test.go:69-74`).

**L46–L47** — Append the summary; close the loop.

**L48–L49** — Encode the list with status 200 and close the handler. The browser refetches this once a second while the workflows page is open (`WorkflowsPage.tsx:42-46`).

**L50–L52** — `POST /api/workflows/{id}/runs` and its comment: "Start accepts no payload. Requiring JSON prevents cross-site HTML forms from triggering checks on this unauthenticated localhost prototype." This is the only route that changes anything, so it is the one a hostile page would aim at. An HTML form can only submit three content types (`application/x-www-form-urlencoded`, `multipart/form-data`, `text/plain`); it cannot say `application/json`. A cross-origin `fetch` can, but then the browser sends a CORS preflight first, and this server answers no CORS headers, so the browser refuses to send the real request. The rule that keeps this check in place is `AGENTS.md:113-114`: "the JSON requirement on run-start requests".

**L53–L56** — Compare the `Content-Type` header to `application/json` and answer `415 Unsupported Media Type` otherwise. `Header.Get` canonicalizes the header name, so `content-type` works, but the value comparison is exact: `application/json; charset=utf-8` is refused too. The frontend sends the exact string (`WorkflowsPage.tsx:58`) and so does the README's curl example (`README.md:441`). The message tells the caller what to send instead. The `return` after `writeError` is essential: `writeError` only writes bytes, it does not stop the function. Without the `return`, the handler would print the error and then start the run anyway. `TestAPIErrorResponses` sends `text/plain` and expects 415 (`api_test.go:171`).

**L57–L60** — Refuse any body with `400 Bad Request`. `r.ContentLength` is the declared length, or `-1` when unknown (chunked encoding), so `!= 0` rejects both; the body is never read. A client that sends a body expects it to mean something, and this endpoint has no parameters, so silently ignoring it would hide the client's mistake. The test sends `{}` and expects 400 (`api_test.go:172`); the README states both codes (`README.md:449-450`).

**L61–L64** — Find the workflow whose `ID` equals `r.PathValue("id")`, the text the `{id}` wildcard matched. A linear scan with `continue` is deliberate: a handful of workflows does not need a map, and `AGENTS.md` asks for "explicit code over a framework or an abstraction". Every branch after a match returns, so the function reaches L87 only when no definition matched. Order matters: the content-type checks at L53-60 run before the lookup, so an unknown id with the wrong header gets 415, not 404.

**L65–L65** — `run, err := runner.Start(definition)`. This is the hand-off to the engine, on the HTTP goroutine. `Start` validates again (`runner.go:106`, `workflow.go:63`), clones `Steps` and `Schedule` so the run goroutine never shares memory with the `definitions` slice (`runner.go:109-113`), takes the runner's mutex, checks closed, busy, and capacity (`runner.go:114-125`), saves the new run through `recordNewRun` with the runner's own context (`runner.go:136-145`), and launches or queues it (`runner.go:150-155`). The handler blocks here for all of that, including the initial save, which is bounded to five seconds. The scheduler calls the same method with the same rules (`main.go:116`, `AGENTS.md:100`). Validation lives in the backend, not here: "Keep backend validation authoritative" (`AGENTS.md:131`). The returned `run` is a copy (`runner.go:156`), so encoding it later cannot race with the execution goroutine.

**L66–L68** — On error, start from `400` and the error's own text. A validation error from `workflow.Validate` lands here as 400; because every loaded definition was already validated at startup (`workflow.go:181`), this is a backstop, not the normal path.

**L69–L74** — Map three sentinel errors to HTTP codes with `errors.Is`. `ErrWorkflowBusy` becomes `409 Conflict` ("this workflow is already queued or running"), `ErrCapacity` becomes `429 Too Many Requests`, and `ErrClosed` becomes `503 Service Unavailable`. The sentinels are declared at `runner.go:17-22`. `Start` returns `ErrWorkflowBusy` bare (`runner.go:120`) but wraps `ErrCapacity` with the configured limits (`runner.go:124`), so `err == engine.ErrCapacity` would be false; `errors.Is` unwraps. The 429 body therefore carries "active limit 2, queue limit 10" with the defaults from `main.go:33-34`, which the README promises (`README.md:447-448`) and `TestConcurrentAdmissionErrorsAndShutdownPersistence` checks with "queue limit 1" (`api_test.go:235`). The 409 case is tested at `api_test.go:76-83` and `api_test.go:232`. The 503 case happens during shutdown: `main.go:140` closes the runner, which sets `closed` (`runner.go:178`), while `server.Shutdown` at `main.go:143` is still draining open connections.

**L75–L79** — `ErrRecordRun` means the initial save failed. The handler logs the full error with the workflow id and replaces the message with fixed text: "Could not save run. Check server logs." The real error contains storage details, such as a file path or a SQLite message, and the README's rule is "Raw storage errors stay in server logs" (`README.md:547`). `TestRunSaveFailureReturnsServerError` injects "write /private/patchbay-test.db: disk full" and asserts the body is exactly the fixed text while the log contains the path (`api_test.go:134-148`). It also asserts that nothing executed and nothing was admitted (`api_test.go:149-151`), which the runner guarantees by returning at `runner.go:142-144` before it touches its maps. That is the rule "Failed initial saves must not execute actions or consume capacity" (`AGENTS.md:83-84`).

**L80–L82** — Write the error and return; the brace closes `if err != nil`. No `Location` header is set on any error path; `api_test.go:244-246` checks that only accepted runs carry one.

**L83–L85** — Success. Set `Location: /api/runs/<id>` first, then answer `202 Accepted` with the run as JSON, then return. 202 means "accepted for processing, not finished"; the status inside is `running` or `queued` (`README.md:164`). The header must be set before `writeJSON` calls `WriteHeader` (L153), because headers set afterwards are dropped. `TestRunLifecycleSurvivesRequestEnd` checks both the code and the header (`api_test.go:69`). The frontend puts the returned run at the top of its list immediately and lets the next poll replace it with the server's order (`WorkflowsPage.tsx:60-67`).

**L86–L86** — Closes the `for` loop over definitions.

**L87–L88** — No definition matched: `404 Not Found` with "workflow not found". Tested at `api_test.go:170`.

**L89–L91** — `GET /api/runs` begins by deriving a context from `r.Context()` with a five-second deadline and deferring `cancel`. The request context ends if the client disconnects, so an abandoned poll stops its own database read. The deadline exists so a stuck database cannot pin a handler goroutine forever: the server's `WriteTimeout` is ten seconds (`main.go:125`), and five seconds for the read leaves time to write the response. The README states this contract: "Both history endpoints read SQLite using the request context and a five-second timeout" (`README.md:557-558`).

**L92–L97** — `store.ListRuns(ctx, db, 100)` returns the newest 100 runs across all workflows (`internal/store/run_reads.go:26-70`, `README.md:435`); the roadmap notes that a per-workflow filter is future work (`docs/roadmap.md:209`). On any error the handler logs the details and answers `500` with "could not load run history" and nothing else. `TestHistoryReadFailuresReturnServerErrors` closes the database in one case and corrupts saved JSON in another, and expects exactly that body both times (`history_test.go:206-243`).

**L98–L100** — Overlay retained unsaved results. The loop uses the index form, `for i := range runs`, so `runs[i] = ...` writes back into the slice; `for _, run := range runs` would have modified a copy and changed nothing. `preferUnsavedRun` (L144) takes the runner's mutex briefly for each run. By this point `ListRuns` has returned, so its read transaction has already ended; the handler never holds a database transaction and the runner's mutex at the same time.

**L101–L102** — Encode the list with 200 and close the handler. An empty history is `[]` (`history_test.go:195`). The browser polls this every second (`WorkflowsPage.tsx:47-51`) and filters it for the selected workflow (`README.md:560-561`).

**L103–L106** — `GET /api/runs/{id}` uses the same timeout pattern and reads the id from the wildcard.

**L107–L107** — `store.GetRun` reads one run and its steps in one transaction (`run_reads.go:16-24`).

**L108–L111** — A missing run is `404` with "run not found". The store wraps `sql.ErrNoRows` with `%w` (`run_reads.go:14`, `run_reads.go:81`), so `errors.Is` is required; `err == sql.ErrNoRows` would never be true. This check comes before the general error check so that "missing" is not reported as a server failure. Tested at `history_test.go:196` and `api_test.go:169`.

**L112–L116** — Any other error, including a deadline or corrupt saved output (`history_test.go:218`), is logged and answered with `500` "could not load run". The overlay below is not consulted on this path: "database read errors still surface" (`AGENTS.md:92-93`), and the README adds that this holds "even when a result is available in memory" (`README.md:563-564`).

**L117–L118** — Overlay the single run and answer 200; close the handler.

**L119–L121** — A catch-all for `/api/`. The trailing slash makes the pattern match the whole subtree, for every method. Any API path that no earlier pattern matched, and any wrong method on a known path (such as `POST /api/workflows`), lands here and gets a JSON `404` "API endpoint not found". This matters for the frontend: `web/src/api.ts:67-73` shows the server's `error` text only when the response is JSON, and otherwise tells the user to check that the Go server is running. Because this pattern matches all methods, the mux's own plain-text 404 and 405 responses are never produced under `/api/`. `GET /api/workflows` is more specific than `/api/`, so it still wins for GET. `TestAPIErrorResponses` requests `/api/missing` and asserts both the 404 and the JSON content type (`api_test.go:173`, `api_test.go:183-185`).

**L122–L122** — Build the static file server once, over `http.Dir(webDir)`. `http.Dir` cleans the request path before joining it to the directory, so `../` cannot reach outside `web/dist`. `FileServer` serves `index.html` for `/`.

**L123–L128** — The `/` catch-all handles everything that is not under `/api/`. It allows only `GET` and `HEAD`; for any other method it sets `Allow: GET, HEAD` and answers a plain-text `405` through `http.Error`. `FileServer` itself does not check the method and would happily serve a file to a `POST`, so this check is what makes the frontend read-only. Plain text is correct here because this is the browser-facing part, not the API.

**L129–L132** — Check that `index.html` exists before serving. If the frontend has not been built, `FileServer` would answer a 404 or a directory listing, which a newcomer would read as a routing bug. Instead the handler answers `503` with the exact commands to run, matching the README's setup instructions. The check runs on every request, which is cheap. Tests that do not care about the frontend pass `t.TempDir()` as `webDir`, so `/` answers 503 in them; `TestHostGuardProtectsAPIAndFrontend` writes an `index.html` only to prove that a rejected host never reaches it (`host_guard_test.go:120`); no Go test serves the file, which is left to the Playwright suite (`web/playwright.config.ts:12`). There is no single-page-app fallback that rewrites unknown paths to `index.html`, and none is needed: the router uses hash history (`AGENTS.md:55`, `web/src/router.tsx`), so every app URL is `/#/...` and the server only ever sees `/` and asset paths.

**L133–L134** — Hand the request to the file server; close the handler.

**L135–L139** — Return the real handler: a `HandlerFunc` that sets two headers on every response and then dispatches to the mux. `X-Content-Type-Options: nosniff` tells browsers to trust the declared `Content-Type` and never guess, so a JSON body or an asset can never be interpreted as HTML or script. `Cache-Control: no-store` tells browsers and proxies never to cache; the run list changes every second, and a rebuilt frontend is picked up on the next reload. They are set here, before `mux.ServeHTTP`, because a handler may write immediately and headers set after that are lost. The host guard repeats both headers at `host_guard.go:43-44` because it runs outside this wrapper. `TestHostGuard` checks `Cache-Control` on rejections (`host_guard_test.go:86-88`).

**L140–L140** — Closes `New`.

**L141–L141** — Blank line.

**L142–L144** — The comment and signature of `preferUnsavedRun`. "Keep database errors, list membership/order, and normal saved history intact. An unsaved result is available only until restart or memory-history eviction." It takes the saved run by value and returns a run by value, so neither the caller's slice nor the runner's map is touched through it.

**L145–L147** — `runner.Get(saved.ID)` locks the runner, copies the run, and unlocks (`runner.go:159-164`). Only when the copy exists and `live.FinalSaveFailed` is true does the memory copy replace the saved one. That flag is set in exactly one place, after all final-save retries fail (`runner.go:310`), and it is a JSON field the frontend shows as a warning (`web/src/api.ts:58`, `web/src/components/ExecutionHistory.tsx:66`, `web/src/components/RunDetails.tsx:23`). The rules this upholds are "`finalSaveFailed` is a memory-only warning, not a new execution status. History overlays that result only while retained" (`AGENTS.md:91-93`). "While retained" is literal: the runner trims its memory list back to 100 entries, evicting only finished runs (`runner.go:365-378`), and loses them all on restart; after eviction the API shows SQLite's last snapshot again, and after a restart that snapshot is marked interrupted (`README.md:549-552`). `TestHistoryShowsUnsavedFinalResultWithoutHidingReadFailures` builds exactly this state and asserts that the list equals `[live, older]`: membership and order from SQLite, content from memory (`history_test.go:406-420`).

**L148–L149** — Otherwise return the saved run. On the normal path a run with a successful final save is identical in both places, and the database copy is the one that survives.

**L150–L150** — Blank line.

**L151–L153** — `writeJSON` sets `Content-Type: application/json` and then `WriteHeader(status)`. The order is the rule from the concepts list: `WriteHeader` sends the header map, so the content type has to be in it first. The `value any` parameter accepts a map (L35), a slice (L48), or a struct (L84).

**L154–L157** — Encode straight to the response. The only realistic failure is a write error because the client went away or the server's `WriteTimeout` hit. There is nothing useful to do then: the status is already on the wire, and, as the comment says, "A disconnected client cannot affect an already-admitted background run." The run started at L65 belongs to the runner and keeps going. Tests trim the trailing newline the encoder adds (`history_test.go:200`, `host_guard_test.go:141`).

**L158–L158** — Closes `writeJSON`.

**L159–L159** — Blank line.

**L160–L162** — `writeError` wraps a message as `{"error": message}` and reuses `writeJSON`. The frontend reads `data.error` from every non-2xx JSON response (`web/src/api.ts:70-71`). The host guard uses the same function (`host_guard.go:45`), so every refusal in the package has the same shape, and `api_test.go:141` checks that an error body has exactly one key.

### internal/httpapi/host_guard.go

**L1–L1** — The package clause. The package doc comment is in `api.go:1`, so this file starts bare, like `runs.go` in chapter 10.

**L2–L2** — Blank line.

**L3–L10** — Imports, all from the standard library: `fmt` for the configuration error (L30), `net` for `SplitHostPort` (L53), `net/http` for the handler types (L39-40), `net/netip` for IP parsing (L69, L81), `strconv` for the port (L56), and `strings` throughout. There is no project import. The guard knows nothing about workflows or runs; it could wrap any handler.

**L11–L11** — Blank line.

**L12–L16** — `HostGuard` holds one field, `allowed map[string]bool`, used as a set. The comment makes three promises: it "restricts the hostnames served", it "does not authenticate callers", and "Its allowlist is fixed at startup and safe for concurrent reads." The last promise is why there is no mutex. The map is written only inside `NewHostGuard` (L21-33), which `main.go:54` calls before the server starts listening at `main.go:129`; after that every handler goroutine only reads it, and concurrent reads of a map are safe. The field is unexported, so nothing outside this file can add a key later. The README repeats the second promise (`README.md:132-133`).

**L17–L17** — Blank line.

**L18–L20** — The doc comment and signature. The input is the raw `-allowed-hosts` flag (`main.go:35`), "comma-separated exact hostnames/IPs". The doc comment fixes the format: "no scheme, port, or wildcard; IPv6 entries are unbracketed." It returns a pointer and an error. `main.go:54-57` checks the error before loading workflows or touching the database and exits with "invalid allowed-hosts configuration". Failing at startup is the point: a typo must not quietly allow a wrong name or quietly lock the administrator out.

**L21–L23** — The defaults: `localhost`, `127.0.0.1`, and `::1` (`README.md:109`). `&HostGuard{...}` takes the address of a composite literal, which is how Go builds a pointer to a new struct in one expression. The keys are already in the canonical form that `normalizeHostname` produces (lowercase, unmapped, no port), because lookups at L42 compare normalized strings. If a default were spelled `LOCALHOST`, no request would ever match it.

**L24–L26** — An empty or all-whitespace flag returns the defaults. `TestHostGuard` passes `"  "` and expects `localhost:8080` to be allowed (`host_guard_test.go:35`).

**L27–L28** — Split on commas, trim each entry, and normalize it with the same function the request path uses (L74). Using one function for both sides is what makes `" Patchbay.Home.Example. "` in the flag match `patchbay.home.example:8080` in a request (`host_guard_test.go:31`): both end up as `patchbay.home.example`.

**L29–L31** — Any entry that does not normalize is a configuration error. The message uses `%q` so the offending entry is quoted, and lists what is not allowed. This rejects empty entries from stray commas (`localhost,`), ports (`patchbay.example:8080`), schemes, wildcards, brackets, zones, and the unspecified addresses `0.0.0.0` and `::`; `TestHostGuardRejectsInvalidConfiguration` lists them all (`host_guard_test.go:94-100`). Ports are refused because the guard compares hostnames without ports (L19, `README.md:124-125`); accepting `host:8080` would create an entry that could never match. Returning `nil` for the guard, not a partly filled one, means a caller that ignores the error cannot accidentally serve with a half-built allowlist.

**L32–L33** — Add the normalized entry to the set; close the loop.

**L34–L35** — Return the guard; close the function.

**L36–L36** — Blank line.

**L37–L40** — `Wrap` turns the guard into middleware. The comment states the two rules it enforces: it "checks the request's Host before any API or frontend handler runs", and "Forwarded headers and DNS lookups must not override the requested hostname." It takes a handler and returns a `HandlerFunc` closure that captures `guard` and `next`. `main.go:123` wraps `httpapi.New(...)` with it, so the order of execution for every request is: `http.Server`, this closure, the wrapper at `api.go:135`, the mux, the route handler.

**L41–L42** — Normalize `r.Host` and look it up. `r.Host` is the authority the client asked for: the `Host` header on HTTP/1.1 or the `:authority` pseudo-header on HTTP/2. Go moves it out of `r.Header`, so `r.Header.Get("Host")` is empty, and `X-Forwarded-Host` and `Forwarded` are just headers nobody reads here. `TestHostGuard` sets all three to `localhost:8080` on every case and still expects the rejections to hold (`host_guard_test.go:73-76`); the README documents the choice and tells reverse-proxy operators to do their own filtering (`README.md:130-132`). `!ok ||` short-circuits, so a malformed host never reaches the map, and a missing key reads as `false`.

**L43–L46** — On rejection, set the two safety headers, answer `403 Forbidden` with the JSON body "request host is not allowed", and return without calling `next`. The headers are repeated here because the wrapper at `api.go:135-139` is inside `next` and will not run. `host_guard_test.go:86-88` asserts both the JSON content type and `Cache-Control: no-store` on a rejection. `TestHostGuardProtectsAPIAndFrontend` sends every route, including the `POST` that starts runs, with `Host: attacker.example:8080` and asserts the 403 body and that `runner.List()` is still empty (`host_guard_test.go:128-147`): a rejected host cannot start a workflow.

**L47–L49** — Close the `if`; on an allowed host call `next.ServeHTTP(w, r)`; close the closure.

**L50–L50** — Closes `Wrap`.

**L51–L51** — Blank line.

**L52–L53** — `requestHostname` takes the raw authority and tries `net.SplitHostPort`. That succeeds for `localhost:8080` and `[::1]:8080` (brackets removed), and fails for `localhost` (no port), `[::1]` (no port), `::1` (too many colons), `http://localhost:8080` (too many colons), and `localhost:80:90`.

**L54–L59** — If a port was present it must be a valid one: `strconv.ParseUint(port, 10, 16)` fails for an empty port (`localhost:`), letters (`localhost:http`), and anything above 65535, and the explicit `number == 0` check rejects port 0. The comment says why ports are otherwise ignored: "Ports do not affect the allowlist, but a supplied port must be valid." Ignoring the port is what keeps development working: Vite's proxy forwards the browser's `Host: 127.0.0.1:5173` unchanged (`web/vite.config.ts:8-11`), Playwright starts the server on 18080 (`web/playwright.config.ts:12`), and Docker's health check uses 8080 inside the container (`compose.yaml:14`); the README lists these (`README.md:112-113`) and the test table covers them (`host_guard_test.go:22-25`). Validating the port still matters because a browser never sends a nonsense port, so one means a malformed or hand-crafted request, and the guard refuses rather than guesses (`host_guard_test.go:46-50`). The inner `number, err :=` declares a new `err` scoped to this block.

**L60–L66** — No port: the whole authority is the host. A bracketed IPv6 literal `[::1]` has its brackets sliced off with `host[1 : len(host)-1]`. Anything else containing a colon or a bracket is malformed and rejected: a bare `::1`, a URL, a double port. Tests: `[::1]` allowed (`host_guard_test.go:27`), `::1` rejected (`host_guard_test.go:58`), URL rejected (`host_guard_test.go:52`).

**L67–L67** — Closes the `else`.

**L68–L73** — If the original authority began with `[`, whatever was inside the brackets must parse as an IPv6 address. Brackets are only legal around IPv6 literals, so `[localhost]:8080` is malformed and rejected (`host_guard_test.go:57`). `Is6()` is also true for an IPv4-mapped address like `::ffff:127.0.0.1`, which L85 will unmap. A zoned address such as `[::1%lo0]` passes this check and is rejected later at L82. The condition uses `authority`, not `host`, because by now `SplitHostPort` may already have removed the brackets from `host`.

**L74–L75** — Normalize and return; close the function.

**L76–L76** — Blank line.

**L77–L80** — `normalizeHostname` is shared by configuration (L28) and requests (L74). A trailing dot is DNS notation for a fully qualified name, so `LOCALHOST.` means `localhost` (`host_guard_test.go:26`, `README.md:114`). The dot is trimmed only when the string has no colon: an IPv6 literal never has a trailing dot, and trimming one from `::1.` would turn an invalid entry into a valid one. The configuration test expects `::1.` to be rejected (`host_guard_test.go:98`).

**L81–L90** — The IP-literal branch. `netip.ParseAddr` succeeds for IPv4 and IPv6, including a zone. A zone (`%lo0`, `%en0`) is rejected: a link-local scoped address is not a stable name for a server (`host_guard_test.go:59`, `host_guard_test.go:98`). `Unmap()` turns `::ffff:127.0.0.1` into `127.0.0.1`, so the mapped spelling matches the default entry (`host_guard_test.go:30`). `IsUnspecified()` rejects `0.0.0.0` and `::`: those are addresses a server binds to, never a name a client asks for, and the README is explicit that `-addr 0.0.0.0:8080` "does not permit every hostname" (`README.md:126-128`; tests at `host_guard_test.go:42-44`). `String()` returns the canonical spelling, which is why `[0:0:0:0:0:0:0:1]:8080` matches `::1` (`host_guard_test.go:29`). The `if address, err := ...; err == nil` form scopes both variables to this branch.

**L91–L94** — The DNS-name branch. Lowercase first, because DNS names are case-insensitive (`README.md:114`). Then reject an empty name (which also catches an empty authority and the empty entries from `localhost,`) and anything over 253 characters, the DNS limit for a full name.

**L95–L98** — Split into labels on `.` and check each: one to 63 characters, and no leading or trailing hyphen. `label[0]` and `label[len(label)-1]` index bytes, which is safe because the hyphen check only cares about ASCII. An empty label catches `localhost..` and `.example` (`host_guard_test.go:56`, `host_guard_test.go:95`), and a 64-character label is rejected (`host_guard_test.go:99`).

**L99–L103** — Every rune in a label must be a lowercase letter, a digit, or a hyphen. `&&` binds tighter than `||`, so the condition reads as three alternatives. `range` over a string yields runes, so a non-ASCII character fails here; an internationalized name would have to be given in its `xn--` punycode form. This is what rejects `@` (userinfo), `/` (a path), spaces, commas, `*`, and `_` (`host_guard_test.go:51`, `host_guard_test.go:53-55`, `host_guard_test.go:95-97`). A strict whitelist is the right shape for this check: the value is only ever used as a map key, so being conservative cannot break a legitimate client, while it stops anything odd before it reaches a handler.

**L104–L104** — Closes the label loop.

**L105–L106** — Return the normalized name with `true`; close the function.

## State, concurrency, and cancellation

**Goroutines.** `New` and `NewHostGuard` run once on the main goroutine during startup (`main.go:54`, `main.go:123`). After `server.ListenAndServe()` starts on its own goroutine (`main.go:129`), `net/http` serves each connection on a fresh goroutine, and every handler in this chapter runs there. Many handlers run at the same time: one browser tab alone produces two polls a second.

**What the handlers share, and who owns it.** The package itself owns nothing mutable. `definitions` is a slice loaded at `main.go:58` that nobody writes after startup; handlers and the scheduler read it concurrently, which is safe. `guard.allowed` is the same: written only in `NewHostGuard`, read by every request, no lock needed (`host_guard.go:13`). The runner owns its maps and protects them with `r.mu`; the API touches them only through `Start` and `Get`, and both return copies made by `copyRun` (`runner.go:156`, `runner.go:163`, `runner.go:380-390`), so encoding a run as JSON never reads memory the execution goroutine is writing. That is the rule "Return copied snapshots" (`AGENTS.md:82`). The scheduler owns its `next` map under its own mutex, taken inside `NextRun` (`scheduler.go:106-107`). The `*sql.DB` is safe for concurrent use; its single pooled connection makes database calls take turns.

**Locks and their order.** A handler goroutine holds at most one lock at a time. In `POST .../runs`, `Start` takes `r.mu` and, while holding it, calls `CreateRun`, which waits for the database connection (`runner.go:114-115`, `runner.go:136-145`): the order is mutex, then connection, and it is the only nesting in the program. In the two reads, the database transaction runs first and ends when `ListRuns` or `GetRun` returns; only then does `preferUnsavedRun` take `r.mu` inside `Get` (`api.go:92` then `api.go:99`; `api.go:107` then `api.go:117`). Nobody holds a transaction while asking for the mutex, so the two orders cannot deadlock. `NextRun` takes the scheduler's mutex with nothing else held. The guard takes no lock.

**Which context each operation uses.**

| Operation | Context | Built at | What ends it |
| --- | --- | --- | --- |
| `GET /api/health`, `GET /api/workflows`, the host check | none needed | | |
| `runner.Start` admission and `CreateRun` | `r.ctx` plus 5 s, inside the runner | `runner.go:139` | `runner.Close` (`runner.go:179`) or the deadline; never the request |
| The run's steps and saves after admission | the runner's context | `runner.go:216` onward | `runner.Close`; never the request |
| `store.ListRuns` | `r.Context()` plus 5 s | `api.go:90` | client disconnect, the handler returning, or the deadline |
| `store.GetRun` | `r.Context()` plus 5 s | `api.go:104` | same |

The start handler never passes `r.Context()` to the runner; `Start` does not even take a context. The comment at `runner.go:138` says why: "This context belongs to the runner, not the originating HTTP request." The rule is "Accepted runs belong to the runner's context, not the originating HTTP request" (`AGENTS.md:84-85`), and `TestRunLifecycleSurvivesRequestEnd` proves it by canceling the request context after the 202 and checking the run is still running (`api_test.go:56-74`). The README states the user-facing promise: "The run continues after the request finishes or the browser closes" (`README.md:451`).

**Failure paths.**

- Host not allowed or malformed: `403` with a JSON error before any handler runs; no run starts (`host_guard.go:42-46`, `host_guard_test.go:145-147`).
- Wrong content type or a body on the start request: `415` or `400`; `Start` is never called (`api.go:53-60`).
- Unknown workflow id: `404` (`api.go:87`).
- Workflow already queued or running: `409`; capacity and queue full: `429`; runner closed during shutdown: `503` (`api.go:69-74`). None of these save anything: "rejected starts are not saved as executions" (`README.md:165`).
- Initial save fails: `500` with fixed text, details in the log, nothing executed, no slot or queue entry taken (`api.go:75-79`, `runner.go:142-144`).
- Client disconnects after the 202: `Encode` fails and is ignored; the run continues (`api.go:154-157`).
- Client disconnects during a history read: the request context cancels the transaction; nothing to respond to.
- Database read fails or times out: `500` with a generic message, overlay skipped (`api.go:93-97`, `api.go:112-116`). "History overlays that result only while retained; database read errors still surface" (`AGENTS.md:92-93`).
- Final save failed earlier and the run is still in memory: the read succeeds and the memory copy replaces the stale saved row (`api.go:145-147`). After eviction or restart the saved row shows instead: "Do not promise unsaved results survive eviction or process restart" (`AGENTS.md:94`).
- Frontend not built: `503` with build instructions (`api.go:129-132`).

**Shutdown.** `main.go:137-145` closes the scheduler, then the runner, then calls `server.Shutdown` with a five-second budget. In that window a start request on an already-open connection gets `503` from `ErrClosed`, while reads keep working because the database closes only after `runServer` returns (`main.go:80-84`). The deferred `server.Close()` (`main.go:127`) cuts any connection still open after the budget. The guard and the API hold no goroutines of their own, so there is nothing for them to stop.

**Rules this code implements.** "Accepted runs belong to the runner's context, not the originating HTTP request" (`AGENTS.md:84-85`): the start handler passes no context and returns as soon as admission ends. "Distinguish execution outcome, monitored-service health, and save failure. `finalSaveFailed` is a memory-only warning, not a new execution status. History overlays that result only while retained; database read errors still surface" (`AGENTS.md:91-93`): `preferUnsavedRun` substitutes only on that flag, and both reads return before reaching it on error. "Scheduled starts call `Runner.Start` exactly as the API does" (`AGENTS.md:100`): the handler adds no admission logic of its own, so the two callers cannot drift. "Keep the unauthenticated prototype local, host validation enabled, and the JSON requirement on run-start requests. Host validation is not authentication" (`AGENTS.md:113-114`): `Wrap` and L53-60 are those two checks. "Keep backend validation authoritative" (`AGENTS.md:131`): the handler maps `Start`'s validation error to 400 instead of validating itself.

## Check yourself

1. The start handler calls `runner.Start(definition)` with no context at all, even though it has `r.Context()`. What would go wrong if `Start` took the request's context, and which test would catch it?

<details><summary>Answer</summary>The request context ends when the handler returns, which happens right after the 202 is written, and earlier if the browser navigates away. If the runner used it, the initial save inside `Start` (`runner.go:139`) or the run's own steps would be canceled by the client's behavior, and a finished response would kill the work it just accepted. The run must belong to the runner's context (`runner.go:138`, `AGENTS.md:84-85`). `TestRunLifecycleSurvivesRequestEnd` cancels the request context after decoding the 202 and then asserts `runner.Get(started.ID)` still reports `running` (`api_test.go:68-74`).</details>

2. Why is the `Location` header set at `api.go:83` before `writeJSON` at `api.go:84`? What would a client see if the two lines were swapped, and which assertions would fail?

<details><summary>Answer</summary>`writeJSON` calls `w.WriteHeader(status)` at `api.go:153`, which sends the status line and the header map. Headers set after that are ignored. Swapped, the 202 would still arrive, but without `Location`. `api_test.go:69` checks `response.Header.Get("Location") == "/api/runs/"+started.ID` and would fail; `api_test.go:244-246` would also fail because it expects every accepted run, and only accepted runs, to carry the header.</details>

3. A run's final save failed three times, so the runner holds it in memory with `FinalSaveFailed` set. Then the database file becomes unreadable. What does `GET /api/runs/{id}` return for that run, and why does the handler not fall back to the memory copy?

<details><summary>Answer</summary>`500` with `{"error":"could not load run"}`. `store.GetRun` fails with something other than `sql.ErrNoRows`, so the handler logs it and returns at `api.go:112-116` before reaching `preferUnsavedRun` at L117. The rule is "database read errors still surface" (`AGENTS.md:92-93`); the README adds "even when a result is available in memory" (`README.md:563-564`). Falling back would hide a broken database behind a result that itself will vanish on restart. `TestHistoryReadFailuresReturnServerErrors` closes the database and expects exactly this body (`history_test.go:214-240`).</details>

4. A request arrives with `Host: attacker.example:8080`, `X-Forwarded-Host: localhost:8080`, and `Forwarded: host=localhost:8080`, aimed at `POST /api/workflows/patchbay-health/runs` with the right content type. Which code runs, which code never runs, and why does the guard set `Cache-Control` and `X-Content-Type-Options` itself?

<details><summary>Answer</summary>`Wrap`'s closure runs first (`host_guard.go:40`). `requestHostname` returns `attacker.example`, the map lookup is false, and L43-46 answer 403. `next.ServeHTTP` is never called, so neither the header wrapper at `api.go:135-139` nor the mux nor the start handler runs, and `runner.Start` is never reached. The forwarded headers are irrelevant because the guard reads only `r.Host`, which Go fills from the real `Host` header or `:authority` (`host_guard.go:41`, `README.md:130`). The guard sets the two headers because the wrapper that normally adds them is inside the handler it just skipped. `TestHostGuard` sets all three headers on every case (`host_guard_test.go:73-76`) and checks the rejection headers (`host_guard_test.go:86-88`); `TestHostGuardProtectsAPIAndFrontend` confirms no run was started (`host_guard_test.go:145-147`).</details>

5. Explain, line by line in `requestHostname` and `normalizeHostname`, why `127.0.0.1:5173` is allowed but `localhost:0` is not. Then say what `-addr 0.0.0.0:8080` changes for the guard.

<details><summary>Answer</summary>`127.0.0.1:5173`: `SplitHostPort` succeeds (L53), `ParseUint("5173", 10, 16)` gives 5173 (L56-57), the authority does not start with `[` (L68), `normalizeHostname` parses it as an IP with no zone (L81-83), `Unmap` leaves it alone, it is not unspecified (L86), and `String()` returns `127.0.0.1` (L89), which is a default key (L22). `localhost:0`: `SplitHostPort` succeeds, `ParseUint` returns 0, and `number == 0` at L57 returns false before any lookup. `-addr` only changes the listening socket (`main.go:29`); the allowlist is unchanged, and a request with `Host: 0.0.0.0:8080` is rejected at L86-88 because the address is unspecified. The README says so directly (`README.md:126-128`). Docker runs the binary with `-addr 0.0.0.0:8080` inside the container (`Dockerfile:24`) and the browser still sends `localhost:8080`, which is allowed.</details>

6. `Start` returns `fmt.Errorf("%w (active limit %d, queue limit %d)", ErrCapacity, ...)` at `runner.go:124`. Why does the handler use `errors.Is` instead of `==`, what exact body does the client receive with the default flags, and why does the `ErrRecordRun` case not send the error's text?

<details><summary>Answer</summary>The wrapped error is a new value, so `err == engine.ErrCapacity` is false; `errors.Is` walks the `%w` chain and finds the sentinel (`api.go:71`). The body is `{"error":"workflow capacity reached; wait for a run to finish (active limit 2, queue limit 10)"}`, built from `err.Error()` at `api.go:68` with the defaults at `main.go:33-34`; `api_test.go:235` checks the limits appear. `ErrRecordRun` is wrapped around the store's error (`runner.go:143`), whose text can contain a file path or SQLite detail. The handler logs that (`api.go:77`) and sends only "Could not save run. Check server logs." (`api.go:78`), because "Raw storage errors stay in server logs" (`README.md:547`); `api_test.go:141-148` asserts both halves.</details>

## Experiments

1. **Change the content-type status.** In `internal/httpapi/api.go:54` replace `http.StatusUnsupportedMediaType` with `http.StatusBadRequest`. Run `go test ./internal/httpapi -run TestAPIErrorResponses`. Expected: the test fails with `POST /api/workflows/test-workflow/runs: want 415, got 400`. The check still protects the endpoint, but the README's documented code (`README.md:449`) and the test disagree with it. Revert.

2. **Remove the IPv6 loopback default.** In `internal/httpapi/host_guard.go:22` delete `"::1": true,`. Run `go test ./internal/httpapi -run 'TestHostGuard$' -v`. Expected: three subtests fail, `TestHostGuard/IPv6_without_port`, `TestHostGuard/IPv6_with_port`, and `TestHostGuard/expanded_IPv6`, each reporting `status=403 handler called=false; want status=204 called=true`. `TestHostGuard/mapped_IPv4` still passes, because `Unmap` at L85 turns `[::ffff:127.0.0.1]` into `127.0.0.1`, which is still allowed. Revert.

3. **Drop the unmapping step.** In `internal/httpapi/host_guard.go:85` delete the line `address = address.Unmap()`. Run `go test ./internal/httpapi -run TestHostGuard -v`. Expected: `TestHostGuard/mapped_IPv4` fails, because `String()` now returns `::ffff:127.0.0.1`, which is not a key in the map. Watch `TestHostGuardRejectsInvalidConfiguration/::ffff:0.0.0.0` as well: `IsUnspecified` recognizes only the plain `0.0.0.0` and `::`, so without `Unmap` the mapped zero address is accepted as configuration and that subtest fails with `invalid host configuration "::ffff:0.0.0.0" was accepted`. Revert.

4. **Ask the store for too many runs.** In `internal/httpapi/api.go:92` change `100` to `101`. Run `go test ./internal/httpapi -run TestEmptyAndMissingHistory -v`. Expected: the test fails with `/api/runs: unexpected history response: 500 {"error":"could not load run history"}`, and the output contains the logged detail `run history limit must be between 1 and 100 (got 101)` from `run_reads.go:30-32`. This shows the two halves of the error path: the precise reason goes to the log, the generic message goes to the client. Revert.

## Next

Chapter 12, "The cron scheduler" ([12-scheduler.md](12-scheduler.md))
