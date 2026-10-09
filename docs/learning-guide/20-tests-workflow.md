# 20. Tests: workflow validation, step decoding, schedules

Files: [internal/workflow/workflow_test.go](../../internal/workflow/workflow_test.go), [internal/workflow/tcp_test.go](../../internal/workflow/tcp_test.go), [internal/workflow/schedule_test.go](../../internal/workflow/schedule_test.go)

Revision: commit 1c4786e

## Role in the system

These three files are the tests for the `workflow` package, which chapters 4 and 5 walked through. The package is pure data and validation: `Load` reads JSON files from a directory (`internal/workflow/workflow.go:147`), `Validate` checks a `Definition` against the rules the README documents (`workflow.go:63`), `Step.UnmarshalJSON` decodes one step strictly by its type (`internal/workflow/step_json.go:14`), and `NewTimetable` plus `Timetable.Next` turn a cron schedule into instants (`internal/workflow/schedule.go:29`, `schedule.go:60`). The tests feed those functions hand-built `Definition` values, JSON strings, temporary files, and fixed `time.Time` values, and check that each rule accepts exactly what the README promises and rejects everything else. Nothing here opens a socket. The URL `http://localhost/health` in the shared fixture is parsed, never fetched; `nas.home` is never resolved; the only clock read on any path is the five-year probe inside `NewTimetable` (`schedule.go:48`), and every expression in the tests gives the same answer whatever the date.

The tests are not on the path from a browser click to a saved run, but they pin the behavior of three gatekeepers on that path. First, startup: `cmd/server/main.go:58` calls `workflow.Load`, and a bad file stops the process before the database opens (`main.go:40-43`). The loader tests prove which files get through. Second, every run start: `Runner.Start` calls `workflow.Validate` before it takes its mutex (`internal/engine/runner.go:106`), for manual starts from `POST /api/workflows/{id}/runs` and for scheduled starts alike. The validation tests prove which definitions can become runs. Third, the scheduler: `schedule.New` builds one `Timetable` per scheduled workflow (`internal/schedule/scheduler.go:62`) and calls `Next` to decide when to fire (`scheduler.go:87`, `scheduler.go:139`); the same value is shown to the browser as `nextRunAt` (`internal/httpapi/api.go:42-43`). The timetable tests prove those instants are computed in the schedule's time zone, including across daylight-saving changes.

The files were added with the features they test: `workflow_test.go` with the first skeleton and the 64 KiB limit (commits a4ba35c and d14892c), `tcp_test.go` with TCP checks (14ba1b0), and `schedule_test.go` with cron scheduling (7bc8df5). They follow the testing rule in `AGENTS.md:134-137`: "Use fake executors, temporary databases, local test servers, and controlled clocks/channels; routine tests must not depend on live homelab services". Here the equivalents are `t.TempDir` for files, in-memory values for definitions, and `time.Date` literals for the clock.

## Concepts you need

- **Internal test packages.** A `_test.go` file that declares `package workflow` compiles into the same package as the production code, so it can read unexported names such as `maxDefinitionBytes` (`workflow.go:18`). The alternative, `package workflow_test`, would see only the exported API. These tests use the internal form because the size-limit test needs the constant. [Go: Testing (How to Write Go Code)](https://go.dev/doc/code#Testing), [testing package](https://pkg.go.dev/testing)
- **`testing.T`, `t.Fatal`, and `t.Fatalf`.** Every test is a function named `TestXxx` taking `*testing.T`. `t.Fatal(err)` and `t.Fatalf(format, ...)` mark the test failed and stop it immediately, which is right when a later line would make no sense without the earlier one succeeding. `t.Errorf` would record the failure and continue; these files never need that. [testing: T.Fatalf](https://pkg.go.dev/testing#T.Fatalf)
- **Subtests with `t.Run`.** `t.Run(name, func(t *testing.T) {...})` runs a named child test. Its name becomes `TestParent/child_name` in output, with spaces turned into underscores, and `go test -run 'TestValidateTCP/long'` can select just a few children. Subtests run one at a time unless they call `t.Parallel`, which is why these tests may safely edit a shared value between them. [Go blog: Using Subtests and Sub-benchmarks](https://go.dev/blog/subtests), [testing: T.Run](https://pkg.go.dev/testing#T.Run)
- **Table-driven tests.** A slice of anonymous structs, each holding a name, an input, and an expectation, followed by one loop that runs the same checks for every row. Adding a case means adding one line. Seven of the eleven tests here loop over such a table; the other four (`TestLoadMixedChecksAndPreserveJSON` and the three `schedule_test.go` tests that are not `TestValidateSchedule`) build their inputs inline. [Go wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)
- **Function values and closures as table entries.** A struct field of type `func(*Definition)` holds a small function. Each row supplies its own, such as `func(d *Definition) { d.Steps = nil }`, and the loop calls `test.edit(&d)` to apply it. The function receives a pointer so its one assignment changes the caller's value. [Go Tour: function values](https://go.dev/tour/moretypes/24), [Go Tour: pointers](https://go.dev/tour/moretypes/1)
- **Value copies versus pointers.** `definition := validDefinition()` gives each test its own `Definition`; writing into it cannot affect another test. `schedule := test.schedule; d.Schedule = &schedule` copies the table's `Schedule` and points the definition at the copy, so the definition owns its data. `Runner.Start` makes the same copy for the same reason (`runner.go:110-113`). [Go Tour: pointers](https://go.dev/tour/moretypes/1), [Effective Go: pointers vs. values](https://go.dev/doc/effective_go#pointers_vs_values)
- **Per-iteration loop variables.** Since Go 1.22, each iteration of a `for` loop gets a fresh copy of the loop variable, so a closure that captures `test` inside `t.Run` sees that iteration's row even if it ran later. `go.mod:3` sets `go 1.26.0`, so this applies here. [Go 1.22 release notes: language changes](https://go.dev/doc/go1.22#language)
- **Variable shadowing.** `dir := t.TempDir()` inside the loop at `workflow_test.go:111` declares a new `dir` that hides the outer one until the block ends. Go allows it; the outer directory is simply no longer needed there. [Go spec: declarations and scope](https://go.dev/ref/spec#Declarations_and_scope)
- **`t.TempDir`, `os.WriteFile`, and octal file modes.** `t.TempDir()` creates an empty directory that the test framework deletes when the test ends, so no test leaves files behind or sees another test's files. `os.WriteFile(path, data, 0600)` creates the file with owner-only read and write permission. `0600` and `0o600` are the same octal number in two spellings; the `0o` prefix was added in Go 1.13 and `tcp_test.go:80` uses it. [testing: T.TempDir](https://pkg.go.dev/testing#T.TempDir), [os: WriteFile](https://pkg.go.dev/os#WriteFile), [Go spec: integer literals](https://go.dev/ref/spec#Integer_literals)
- **`filepath.Join`.** Joins path elements with the host's separator and cleans the result. The tests build the expected error text with it so the comparison at `workflow_test.go:46` matches what `Load` builds at `workflow.go:158` on every operating system. [path/filepath: Join](https://pkg.go.dev/path/filepath#Join)
- **`json.Marshal`, `json.Unmarshal`, and decoding into `any`.** `Marshal` turns a Go value into JSON bytes following the struct tags; `Unmarshal` does the reverse, calling a type's `UnmarshalJSON` when it has one. Decoding into a variable of type `any` produces generic `map[string]any`, `[]any`, `float64`, `string`, and `bool` values, which lets two JSON texts be compared by meaning rather than by bytes. [encoding/json: Marshal](https://pkg.go.dev/encoding/json#Marshal), [encoding/json: Unmarshal](https://pkg.go.dev/encoding/json#Unmarshal)
- **`reflect.DeepEqual`.** Compares two values field by field, following pointers and walking slices and maps. It is how the tests compare a loaded `[]Definition` with the expected one, including the `*Schedule` pointer, which plain `==` could not do for a slice. [reflect: DeepEqual](https://pkg.go.dev/reflect#DeepEqual)
- **Comparable structs.** `Step` contains only strings and ints, so two `Step` values can be compared with `!=`. The decoding tests rely on that to prove a failed decode left the previous value untouched. [Go spec: comparison operators](https://go.dev/ref/spec#Comparison_operators)
- **Strings are bytes.** `len(s)` counts bytes, not characters. An em dash is three bytes in UTF-8 and `é` is two. The size-limit test puts both in a description so the byte count and the character count differ, matching how the loader measures a file. [Go blog: Strings, bytes, runes and characters](https://go.dev/blog/strings)
- **`strings.Repeat` and `strings.Contains`.** `Repeat(" ", n)` builds the padding that brings a file to an exact size. `Contains(err.Error(), want)` checks that an error message mentions the field at fault without pinning the whole sentence. [strings: Repeat](https://pkg.go.dev/strings#Repeat), [strings: Contains](https://pkg.go.dev/strings#Contains)
- **`fmt` verbs in failure messages.** `%v` prints a value, `%+v` prints a struct with field names, and `%q` prints a quoted string. The tests use `%+v` for definitions so a failure shows every field. [fmt: printing](https://pkg.go.dev/fmt#hdr-Printing)
- **`time.Date`, `time.UTC`, `Time.Equal`, and `Time.Location`.** `time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)` builds a fixed instant, so a test never depends on the clock. `a.Equal(b)` is true when both name the same instant, whatever zone each carries; `==` would also compare the zone pointer and would fail for equal instants in different zones. `t.Location()` reports the zone a time carries. [time: Date](https://pkg.go.dev/time#Date), [time: Time.Equal](https://pkg.go.dev/time#Time.Equal), [time: LoadLocation](https://pkg.go.dev/time#LoadLocation)
- **Daylight saving time in the cron library.** `robfig/cron` searches forward on the wall clock of the location it is given. Its documentation says "Be aware that jobs scheduled during daylight-savings leap-ahead transitions will not be run!", and when a wall-clock time repeats, the search finds it twice. The library also states that a schedule without its own zone is "treated as local to the time provided", which is why `schedule.go:61` converts the input with `after.In(t.location)`. [cron package documentation](https://pkg.go.dev/github.com/robfig/cron/v3), [cron: ParseStandard](https://pkg.go.dev/github.com/robfig/cron/v3#ParseStandard)
- **Documentation IP ranges.** `192.0.2.10` is in TEST-NET-1 and `2001:db8::1` is in the IPv6 documentation prefix. Both are reserved for examples and never route to a real host, so a fixture can hold a realistic address without pointing at anyone's machine. [RFC 5737](https://www.rfc-editor.org/rfc/rfc5737), [RFC 3849](https://www.rfc-editor.org/rfc/rfc3849)
- **Selecting tests with `go test -run`.** `-run` takes a regular expression matched against test names, with `/` separating parent and subtest. `-v` prints each test and subtest as it runs. The Experiments section uses both. [cmd/go: testing flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags)

## Walkthrough

### internal/workflow/workflow_test.go

**L1–L2** — `package workflow` and a blank line. This is an internal test file: it shares the package with `workflow.go`, so L30-L33 can use the unexported constant `maxDefinitionBytes` from `workflow.go:18`. Every file in this chapter declares the same package, which is also why `validDefinition` (L12) is visible to `tcp_test.go` and `schedule_test.go` without an import.

**L3–L11** — The import block and a blank line, standard library only. `encoding/json` encodes fixtures (L21, L90). `os` writes files (L41, L94, L112). `path/filepath` joins paths (L37, L94, L112). `reflect` compares loaded definitions (L51, L101). `strings` builds padding (L40). `testing` supplies `*testing.T`. No `net`, no `time`, and no third-party package: these tests touch only the filesystem.

**L12–L17** — `validDefinition` helper and a blank line. It returns a `Definition` that passes every rule in `Validate`: schema version 1 (`workflow.go:64`), ID `example` and name `Example` (`workflow.go:67`), and one `http.check` step with ID `check`, URL `http://localhost/health`, expected status 200, and a 1000 ms timeout (`workflow.go:88-98`, `workflow.go:112`). `Description` is empty and `Schedule` is nil. It is a function, not a package-level `var`, so each call builds a fresh value with a fresh `Steps` slice. That matters: tests edit the result in place (L65 appends to `Steps`; `tcp_test.go:20` writes a host), and a shared variable would leak those edits from one test into the next. Eleven call sites use it across the three files, for example L19, L73, L78, L89, `tcp_test.go:13`, and `schedule_test.go:35`. The URL names `localhost`, but `Validate` only parses it (`workflow.go:92`); nothing in this package connects.

**L18–L57** — `TestLoadDefinitionSizeLimit` (L18–L56) and a blank line. It proves the 64 KiB file limit that `README.md:335-337` documents: "Each workflow JSON file may contain up to 64 KiB, including whitespace. The loader reads at most 64 KiB plus one byte to detect oversized files... Files exactly at the limit remain supported."

- L19–L20: the expected definition gets the description `"Service status — café"`. The comment says why: "The limit counts bytes, not characters." The em dash and the accented letter make the byte length larger than the character count, so a loader that measured runes instead of bytes would disagree with the test's sizes. The loader measures `len(data)` on a byte slice (`workflow.go:169`), and `len(encoded)` at L29 measures bytes too.
- L21–L24: `json.Marshal` produces the canonical bytes of that definition. `t.Fatal` stops the test if encoding fails; it cannot for this struct, but checking is the habit.
- L25–L34: a table of five sizes. "ordinary file" is the encoded length itself, so no padding is added. "one byte below limit", "exact limit", and "one byte over limit" sit on each side of `maxDefinitionBytes`, which is 65536 (`workflow.go:18`). "much larger file" is sixteen times the limit, one mebibyte, to exercise the path where the file is far bigger than what the loader reads.
- L35: each size runs as a subtest named after the row, so a failure reads `TestLoadDefinitionSizeLimit/one_byte_over_limit`.
- L36–L37: a fresh temporary directory per subtest and a path ending in `workflow.json`. `Load` reads a whole directory (`workflow.go:148`) and skips anything that is not a `.json` file (`workflow.go:155`), so each subtest needs its own directory with exactly one correctly named file. The `path` variable is built with `filepath.Join` exactly as `Load` builds its own at `workflow.go:158`, because L46 compares the two as text.
- L38–L40: the file content is the encoded JSON followed by enough spaces to reach `test.size` bytes. The comment explains both halves of the trick. JSON decoders ignore trailing whitespace, so every fixture is a valid document that differs only in size. And the one-byte-over case "catches silently accepting a limited prefix": if `Load` read only 64 KiB without the extra byte at `workflow.go:164` and without the check at `workflow.go:169`, the oversized file would be cut to a valid prefix (the JSON plus some spaces) and accepted. Padding with spaces is what makes that silent truncation produce a valid document, so the test can prove the loader does not do it.
- L41–L43: write the file with mode `0600`; stop on error.
- L44: call `Load`, the function under test and the one `cmd/server/main.go:58` calls at startup.
- L45–L50: for sizes over the limit, the error must be exactly `path + ": definition exceeds 64 KiB"` and `definitions` must be nil. The text pins `workflow.go:170`, which formats `%s: definition exceeds 64 KiB` with the file path, because `README.md:332-333` promises "the error identifies the file and problem". The nil check pins the `return nil, ...` on that same line: a caller must not receive a partial list next to an error. `return` ends the subtest here; the success assertions below do not apply.
- L51–L53: for sizes at or under the limit, `Load` must succeed and return exactly `[]Definition{want}`. `reflect.DeepEqual` compares every field, including the multi-byte description, so this also proves the decoder at `workflow.go:172-177` did not truncate or alter the text and that trailing whitespace was accepted as part of the single document check at `workflow.go:178-180`. The `exact limit` case passing here is the README's "Files exactly at the limit remain supported".
- L54–L56: close the subtest, the loop, and the function.

**L58–L86** — `TestValidate` (L58–L85) and a blank line. It proves the rejection list in `README.md:329-331` for HTTP workflows: "unsupported schema/node types, invalid URLs, duplicate workflow/step IDs, empty workflows, invalid HTTP status codes, and out-of-range timeouts".

- L59–L72: a table of nine edits. Each row holds a name and a closure that breaks one rule in a valid definition it receives by pointer. The rule each row hits in `Validate`:
  - "unknown version" sets `SchemaVersion` to 2: `workflow.go:64-66`.
  - "empty workflow" sets `Steps` to nil: `workflow.go:75-77`.
  - "duplicate step" appends a copy of the first step: the `seen` map at `workflow.go:78-83` catches the repeated ID on the second pass. The append is safe because `validDefinition` returned a fresh slice.
  - "unsupported node" sets the type to `ssh.command`: `workflow.go:109-110`. That is the node type `docs/roadmap.md:45` sketches for the SSH milestone; today it must be refused.
  - "relative URL" sets `/health`: `url.Parse` succeeds, but the scheme is empty, so `workflow.go:93-94` rejects it.
  - "non HTTP URL" sets `file:///etc/passwd`: the scheme is neither `http` nor `https` (`workflow.go:93`). This keeps the HTTP executor from being pointed at local files.
  - "credentials in URL" sets `https://user:pass@example.com`: `u.User != nil` at `workflow.go:93`.
  - "unbounded timeout" sets `TimeoutMS` to 0: `workflow.go:112-114`.
  - "invalid status" sets `ExpectedStatus` to 999: `workflow.go:96-98`.
- L73–L75: the positive control. The untouched valid definition must pass. Without this line, a `Validate` that rejected everything would pass all nine negative subtests, and the test would prove nothing.
- L76–L84: each subtest takes a fresh definition, applies the row's edit through `&d`, and requires a non-nil error. The test does not inspect the message; each edit breaks exactly one rule, so "rejected" is the whole claim.
- L85: closing brace.

**L87–L119** — `TestLoadJSONAndRejectBadFiles`. It proves the file round trip and four more entries from the README's rejection list: "malformed JSON, unknown fields, multiple JSON documents" and "empty workflows" (`README.md:329-331`).

- L88–L96: a temporary directory, the encoded valid definition, and a `workflow.json` written with mode `0600`.
- L97–L103: `Load` must succeed and return exactly `[]Definition{want}`. This is the encoder and decoder agreeing through a file: the struct tags at `workflow.go:20-45` write the shape, `Load` reads it back (`workflow.go:172-177`), each step goes through `Step.UnmarshalJSON` (`step_json.go:14`), and the result is appended to the slice created at `workflow.go:152`. `%+v` in the failure message prints every field of what was loaded.
- L104–L109: four file contents that must be refused, each hitting a different line of `Load`:
  - `{"schemaVersion":1,"unknown":true}`: an unknown top-level key, refused by `DisallowUnknownFields` at `workflow.go:174-176`.
  - `{"schemaVersion":1} {"id":"second-document"}`: two JSON documents. The first decodes without error, since missing keys simply leave zero values; the second `Decode` at `workflow.go:178-180` finds more input instead of `io.EOF`.
  - `{"schemaVersion":1,"id":"empty","name":"Empty","steps":[]}`: decodes cleanly and fails `Validate` at `workflow.go:75-77`, wrapped with the path at `workflow.go:181-182`.
  - `this is not JSON`: the first `Decode` fails at `workflow.go:175-176`.
- L110–L118: each content gets its own temporary directory (the inner `dir` shadows the outer one from L88) and is written as `bad.json`. `Load` must return an error; on failure the message prints the offending content. Only failure is checked, not the text: two of the four messages are written by `encoding/json`, and pinning them would tie the test to a Go release.
- L119: closing brace.

### internal/workflow/tcp_test.go

**L1–L11** — Package clause, imports, and a blank line. Same package as above. `encoding/json` is used for the round trip (L90, L95, L98) and for direct step decoding (L120, L139). `strings` supplies `Repeat` for the length fixtures (L40-L41) and `Contains` for the duplicate-field message (L140). The rest matches `workflow_test.go`.

**L12–L72** — `TestValidateTCP` (L12–L71) and a blank line. It proves the TCP host, port, and timeout rules from `README.md:316-322` and the "no fields from the other check type" rule stated at `README.md:321-322` and in `AGENTS.md:124-125`.

- L13–L17: start from the valid HTTP definition and append a second step: ID `port`, type `tcp.check`, host `nas.home`, port 22, timeout 1000. The definition now mixes both check types, which `README.md:321` says is allowed, so every `Validate` call below also re-checks the HTTP step.
- L18–L25: seven hosts that must be accepted, each as a subtest named by the host. The subtest writes the host into `definition.Steps[1].Config.Host` and calls `Validate`. Each one exercises a branch of `validTCPHost`: `nas` is a single local name (`workflow.go:128-129`); `NAS.home` has uppercase letters, which `workflow.go:138` allows; `nas.home.` ends with the absolute-name dot that `workflow.go:129` trims; `192.0.2.10` and `::1` and `2001:db8::1` are parsed by `netip.ParseAddr` at `workflow.go:125`; `fe80::1%eth0` carries an interface zone, which the comment at `workflow.go:126` says is included. The two numeric addresses come from documentation ranges, so the fixture names no real machine. The subtests edit the shared `definition` in place; that is safe because subtests run one after another, and the next loop resets the config anyway.
- L26–L49: nineteen edits that must be rejected, each a closure over `*CheckConfig`. The line in `Validate` or `validTCPHost` that refuses each:
  - "missing host" (`""`): `ParseAddr` fails and the trimmed name has length 0, `workflow.go:130-131`, surfacing as `workflow.go:103-104`.
  - "URL", "path": contain `/`, refused by `ContainsAny` at `workflow.go:120`.
  - "bracketed IPv6" (`[::1]`) and "credentials" (`user@nas.home`): `[`, `]`, and `@` are in the same forbidden set at `workflow.go:120`.
  - "port in host" (`nas.home:22`): not an address, and `:` is not a legal label character at `workflow.go:137-140`.
  - "whitespace" and "zone whitespace": `unicode.IsSpace` at `workflow.go:121`. The whitespace check runs before `ParseAddr`, which is why `fe80::1%bad zone` cannot slip through as an address with an odd zone name.
  - "empty DNS label" (`nas..home`), "invalid DNS label" (`-nas.home`), and "long DNS label" (64 letters): the label rules at `workflow.go:134`.
  - "long hostname" (`a.` repeated 127 times plus `a`, 255 bytes): the 253-byte name limit at `workflow.go:130`.
  - "missing port", "negative port", "large port" (0, -1, 65536): `workflow.go:106-108`.
  - "short timeout", "long timeout" (99, 30001): `workflow.go:112-114`, the same rule HTTP steps obey.
  - "HTTP URL field" and "HTTP status field": `workflow.go:100-102`. Even a plausible value like status 200 is refused on a TCP step, which is the "even when empty or zero" promise of `README.md:322` seen from the value side.
- L50–L56: each subtest first resets `Steps[1].Config` to the valid TCP config (L51), so edits never accumulate, then applies the row's edit and requires an error. Without the reset at L51, the 255-byte host left by "long hostname" would still be in place when "missing port" ran, and the test could pass for the wrong reason.
- L58–L65: the four corners of the numeric ranges, ports 1 and 65535 with timeouts 100 and 30000, must all be accepted. This pins the bounds at `workflow.go:106` and `workflow.go:112` as inclusive; an off-by-one that wrote `<= 1` or `>= 30000` would fail here. These are plain loops, not subtests, so a failure names the limits in its message instead.
- L66–L70: a fresh valid HTTP definition gets `Host` set to `nas` in Go code and must be rejected. The message says "supplied programmatically" because no JSON file can produce this state: `step_json.go:29-38` never fills `Host` for an HTTP step. The rule at `workflow.go:89-91` exists for definitions built in code rather than loaded from files, since `Runner.Start` validates whatever it is handed (`runner.go:106`). This is `AGENTS.md:131`, "Keep backend validation authoritative", applied to the second entrance.
- L71: closing brace.

**L73–L105** — `TestLoadMixedChecksAndPreserveJSON` (L73–L104) and a blank line. It proves the promise in the comment at `step_json.go:11-12`, "preserving the existing flat JSON shape", by loading a hand-written file and encoding the result back.

- L74–L78: a raw JSON constant with one HTTP step and one TCP step, written in the documented file shape (`README.md:307-313`). It spells out `"description":""` and omits `schedule`, matching what the encoder writes for an empty description (no `omitempty` at `workflow.go:24`) and a nil schedule (`omitempty` at `workflow.go:25`).
- L79–L82: a temporary directory and the file `checks.json`, written with `0o600`, the newer spelling of the same octal mode the other files write as `0600`.
- L83–L89: `Load` must succeed with one definition of two steps, and the TCP step's host must be `::1`. That proves the `tcp.check` branch of `Step.UnmarshalJSON` filled `CheckConfig.Host` (`step_json.go:39-48`) and that an unbracketed IPv6 host survives both decoding and `Validate` (`README.md:316-317`).
- L90–L93: `json.Marshal` encodes the loaded definition. `Step` has no `MarshalJSON`, so this uses the tags at `workflow.go:29-34` and `workflow.go:40-44`.
- L94–L100: both the original text and the re-encoded bytes are decoded into `any`, giving generic maps and slices. That comparison ignores key order and whitespace and looks only at keys and values.
- L101–L103: `reflect.DeepEqual` on the two generic values. For this to hold, the HTTP step must come back with no `host` or `port` keys and the TCP step with no `url` or `expectedStatus` keys, which is what the `omitempty` tags on `workflow.go:40-43` do, while `timeoutMs` (`workflow.go:44`) and `description` are always present. The store saves the definition in this same shape with each run, and `GET /api/workflows` returns it to the browser (`internal/httpapi/api.go:37-49`), so a change here would show up in saved history and in the web types.
- L104: closing brace.

**L106–L128** — `TestStepJSONRejectsWrongFields` (L106–L127) and a blank line. It calls `Step.UnmarshalJSON` directly, without a file or `Load`, and proves that each decoder in `step_json.go` is strict and that a failed decode leaves the target untouched.

- L107–L117: nine step objects. The line that refuses each:
  - L108, `"extra":true` at the step level: the outer `decodeStrict` at `step_json.go:24-26`, through `DisallowUnknownFields` at `step_json.go:94`.
  - L109 and L110, an HTTP step with `"host":""` or `"port":0`: the HTTP config struct at `step_json.go:30-34` has no such fields, so `decodeStrict` at `step_json.go:35-37` fails even though the values are zero. This is the "even a zero-valued field for the other check type is rejected" promise at `step_json.go:12-13`.
  - L111, L112, L113, a TCP step with `"url":""`, `"expectedStatus":0`, or `"extra":true`: the TCP config struct at `step_json.go:40-44` and `decodeStrict` at `step_json.go:45-47`.
  - L114 and L115, `"port":"22"` and `"port":22.5`: type errors from the same decoder, because `Port` is an `int` and `encoding/json` does not coerce a string or a fraction into one.
  - L116, type `ssh.command`: the `default` branch at `step_json.go:49-50`.
- L118–L119: `original` is a copy of the valid HTTP step, and `got` is a second copy. Both are plain values; `Step` holds only strings and ints.
- L120–L122: `json.Unmarshal` into `&got` reaches `Step.UnmarshalJSON` through the `json.Unmarshaler` interface (`step_json.go:14`). It must fail, and the failure message prints the input.
- L123–L125: `got` must still equal `original`. This pins the "build locally, assign at the end" structure of the decoder: the method fills a local `step` at `step_json.go:27` and writes `*s = step` only at `step_json.go:52`, after every check passed. A decoder that wrote fields into `*s` as it went would fail this line.
- L126–L127: close the loop and the function.

**L129–L147** — `TestStepJSONRejectsDuplicateMembers`. It proves `rejectDuplicateStepFields` (`step_json.go:58-90`), the guard whose comment at `step_json.go:56-57` warns that "accepting a second config here could hide unknown fields in the first one".

- L130–L136: five step objects with a repeated key:
  - L131, `config` twice with the bad one first: without the guard, `encoding/json` keeps the last value, so the clean second `config` would be decoded and the bad first one never seen.
  - L132, `config` twice with the bad one second: without the guard this would still fail, but at `step_json.go:45` for an unknown field rather than for the duplicate. The message check at L140 tells the two apart.
  - L133, `config` then `Config`: `encoding/json` matches keys case-insensitively, so these are the same field. The guard lowercases names at `step_json.go:78` before checking `seen`.
  - L134, `type` twice, `http.check` then `tcp.check`: without the guard the last type would win and the TCP config would decode cleanly, silently changing the step's type.
  - L135, `id` then `ID`: the same case-folding rule on a plain string field.
- L137–L139: the same two copies as the previous test, and a direct `json.Unmarshal`.
- L140–L142: the error must be non-nil and contain `duplicate step field`, the text at `step_json.go:80`. Checking the substring proves the rejection came from the guard, which runs first at `step_json.go:15`, and not from a later decoder.
- L143–L146: `got` is unchanged. Because the guard runs before any decoding, nothing could have been written, and the assertion documents that contract.
- L147: closing brace.

### internal/workflow/schedule_test.go

**L1–L11** — Package clause, imports, and a blank line. `strings` supplies `Contains` (L39, L120). `time` supplies `Date`, `UTC`, and `Time.Equal` (L52-L59, L71-L91). `os`, `path/filepath`, and `reflect` serve the file test at L96. There is no fake clock: the timetable is a pure function of its input, so fixed instants are enough.

**L12–L45** — `TestValidateSchedule` (L12–L44) and a blank line. It proves every rejection `README.md:366-372` lists for the `schedule` object and that each error names the field at fault, as the comment at `schedule.go:28` promises.

- L13–L17: the positive control. A valid definition with `Schedule{Cron: "*/5 * * * *", Timezone: "UTC"}` must pass `Validate`, which calls `NewTimetable` at `workflow.go:70-73`. This is the expression from `README.md:354` and `examples/scheduled-check.json`.
- L18–L32: a table of nine schedules with the substring each error must contain. The line in `NewTimetable` that produces each:
  - "empty object": `Schedule{}` has no cron and no timezone, and the error must mention `cron`. That pins the order of checks: the cron check at `schedule.go:30-32` runs before the timezone check at `schedule.go:40-42`.
  - "words instead of fields" and "six fields": `cron.ParseStandard` requires exactly five fields and reports `expected exactly 5 fields`; `schedule.go:36-38` wraps that as `schedule cron %q is invalid`, which contains `cron`.
  - "timezone inside cron" (`CRON_TZ=UTC */5 * * * *`): the `strings.Contains(s.Cron, "TZ=")` check at `schedule.go:33-34`, which runs before the parser. The cron library documents that it accepts a `CRON_TZ=` or `TZ=` prefix, so without this check the file would load and carry its zone in two places.
  - "missing timezone" and "implicit local timezone" (`Local`): `schedule.go:40-42`. `time.LoadLocation` would accept both, returning UTC and the host zone, which would make the same file mean different things on different hosts; `README.md:371-372` states that goal.
  - "unknown timezone" (`Mars/Olympus_Mons`): `time.LoadLocation` fails at `schedule.go:43-45`, and the message contains `timezone`.
  - "sub-minute interval" (`@every 10s`): the parser accepts it, so it reaches the gap check at `schedule.go:52-54` and fails with `once a minute`. This is the floor `minimumInterval` at `schedule.go:26`.
  - "date that never exists" (`0 0 30 2 *`): February 30 never occurs, so the cron library's five-year search returns the zero time and `schedule.go:49-51` reports `no occurrence`. This is the one check that reads `time.Now()` (`schedule.go:48`), and the test tolerates it because the answer is the same in every year.
- L33–L43: each subtest builds a fresh definition, copies the row's `Schedule` into a local variable, and points `d.Schedule` at that copy. The copy means the definition owns its schedule rather than aliasing the table, the same discipline `Runner.Start` applies at `runner.go:110-113`. `Validate` must fail and the message must contain the expected word; `%q` in the failure prints the expected substring in quotes.
- L44: closing brace.

**L46–L61** — `TestTimetableNextUsesTheScheduleTimezone` (L46–L60) and a blank line. It proves the one line of `Timetable.Next`, `return t.schedule.Next(after.In(t.location))` at `schedule.go:61`, and the doc comment above it: "expressed in the schedule's time zone".

- L47–L50: a timetable for 09:00 every day in `America/New_York`. `NewTimetable` parses the expression and loads the zone (`schedule.go:36`, `schedule.go:43`).
- L51–L53: the input is a fixed instant, 12:00 UTC on 15 January 2026, and the expected result is 14:00 UTC the same day. The comment explains: 12:00 UTC is 07:00 in New York, so 09:00 New York time is still ahead that day, and in January New York is five hours behind UTC. Using `time.Date` literals is how the test controls time; no clock is consulted on this path except the five-year probe inside `NewTimetable`, which does not affect the result.
- L54–L56: `got.Equal(want)` compares instants. `got` carries the New York zone and `want` carries UTC, so `==` would be false even though they name the same moment.
- L57–L59: the result's location must be `America/New_York`. The cron library treats a schedule with no zone of its own "as local to the time provided" and returns its answer in the zone of the input. `schedule.go:61` therefore converts the input with `after.In(t.location)`; this assertion would fail if that call were dropped, and the first assertion would fail too, since 09:00 would then be read as UTC. The zone matters downstream: the scheduler logs the occurrence (`scheduler.go:89-90`) and `GET /api/workflows` returns it as `nextRunAt`, which `README.md:388-389` says is "expressed in the schedule's zone".
- L60: closing brace.

**L62–L63** — A comment for the next test. It fixes the facts the expectations depend on: in 2026 New York moves clocks forward on March 8 and back on November 1. Both are correct for the United States rule (second Sunday in March, first Sunday in November). It also says the expected instants "follow the cron library's documented wall-clock search", which is the behavior `README.md:381-383` describes to users.

**L64–L95** — `TestTimetableNextAcrossDaylightSavingChanges` (L64–L94) and a blank line. It proves the two sentences at `README.md:381-383`: "When a daylight-saving change removes a wall-clock time, that day's occurrence is skipped; when a change repeats a wall-clock time, the occurrence runs twice." Neither behavior is implemented in Patchbay code; both come from the cron library through `schedule.go:61`. The test exists so that a change of library, or of the `In` conversion, would be noticed.

- L65–L76: the skipped time. The schedule is 02:30 daily in New York. The input is 05:00 UTC on March 8, which the comment identifies as midnight EST that day. Clocks jump from 02:00 to 03:00 that morning, so no instant on March 8 reads 02:30. The expected result is 06:30 UTC on March 9, which is 02:30 EDT with the new four-hour offset. The cron documentation warns that jobs "scheduled during daylight-savings leap-ahead transitions will not be run", and this subtest pins that outcome for Patchbay's wrapper.
- L77–L93: the repeated time. The schedule is 01:30 daily in New York, and the input is 04:00 UTC on November 1, midnight EDT. Clocks fall back from 02:00 EDT to 01:00 EST, so 01:30 happens twice. The first `Next` must return 05:30 UTC, which is 01:30 EDT; the second call, with `first` as its input, must return 06:30 UTC, which is 01:30 EST one hour later. Feeding the previous occurrence back into `Next` is what the scheduler does after each firing, using the later of the clock and that occurrence (`scheduler.go:135-139`), so this subtest also shows that a scheduled workflow fires twice on that night.
- L94: closing brace.

**L96–L123** — `TestLoadPreservesScheduleAndRejectsEmptyOne`. It proves that the `schedule` object survives a file load unchanged and that an empty one is a validation error rather than "no schedule".

- L97: the `steps` JSON, shared by both halves, with the same values as `validDefinition` so the expected definition can be derived from the helper.
- L98–L102: a temporary directory and a file `scheduled.json` whose schedule is `*/5 * * * *` in `Europe/Amsterdam`, the zone from the README example (`README.md:354`).
- L103–L106: `Load` must succeed. Inside, `Validate` ran `NewTimetable` on the schedule (`workflow.go:70-73`).
- L107–L112: the expected value is `validDefinition()` with the ID, name, and a `*Schedule` set. `reflect.DeepEqual` follows the pointer and compares the two `Schedule` values, so this pins the JSON tags at `schedule.go:14-15` and the pointer field at `workflow.go:25`. On failure the message prints `definitions[0]` with field names.
- L113: blank line.
- L114–L122: the comment states the rule: "An empty schedule object is a configuration mistake, not a manual workflow." The file carries `"schedule":{}`. Decoding produces a non-nil `*Schedule` with two empty strings. `Validate` checks only whether the pointer is nil (`workflow.go:70`), so it hands the empty schedule to `NewTimetable`, which fails at `schedule.go:30-32` with `schedule needs a cron expression`; `Load` prefixes the path at `workflow.go:181-182`. The test requires an error containing `cron`. Treating `{}` as "unscheduled" would silently turn a workflow its author meant to schedule into a manual one. Note also that this file omits `description` and still decodes: `DisallowUnknownFields` refuses extra keys, not missing ones.
- L123: closing brace.

## What these tests prove

| test function | production rule it proves | how it injects failure or controls time | test file:line |
| --- | --- | --- | --- |
| `TestLoadDefinitionSizeLimit` | Files up to and including 64 KiB load unchanged; one byte over is rejected with the file path and a nil list (`workflow.go:164`, `workflow.go:169-170`) | Temporary directory per subtest; files padded with spaces to exact byte sizes around `maxDefinitionBytes` | `workflow_test.go:18` |
| `TestValidate` | Schema version, step count, duplicate IDs, node type, URL scheme and credentials, timeout, and status bounds (`workflow.go:64-114`) | Closures that break one field of an in-memory valid definition; a positive control first | `workflow_test.go:58` |
| `TestLoadJSONAndRejectBadFiles` | File round trip equals the encoded struct; unknown fields, two documents, empty steps, and non-JSON are refused (`workflow.go:174-182`) | Temporary directories; four hand-written bad files; no network | `workflow_test.go:87` |
| `TestValidateTCP` | TCP host syntax, port and timeout bounds inclusive at both ends, and no HTTP fields on a TCP step or TCP fields on an HTTP step (`workflow.go:89-91`, `workflow.go:100-114`, `workflow.go:119-144`) | In-memory definition with a mixed HTTP and TCP pair; hosts from documentation IP ranges; config reset before each edit | `tcp_test.go:12` |
| `TestLoadMixedChecksAndPreserveJSON` | A mixed file loads, the TCP branch fills `Host`, and encoding the result reproduces the file's flat shape (`step_json.go:11-12`, `step_json.go:39-48`, `workflow.go:40-44`) | Temporary file; decode both texts into `any` and compare by meaning | `tcp_test.go:73` |
| `TestStepJSONRejectsWrongFields` | Each config decoder refuses the other type's fields even at zero, type mismatches, and unknown types, and a failed decode leaves the target untouched (`step_json.go:24-52`, `step_json.go:94`) | Direct `json.Unmarshal` of nine step objects; comparable `Step` copies | `tcp_test.go:106` |
| `TestStepJSONRejectsDuplicateMembers` | Repeated or case-variant step keys are refused before any decoding (`step_json.go:15`, `step_json.go:78-80`) | Five step objects with duplicate keys; substring check on the error | `tcp_test.go:129` |
| `TestValidateSchedule` | Cron must be present, five fields or a descriptor, without `TZ=`; timezone must be an explicit IANA name; gaps at least one minute; some occurrence within five years; errors name the field (`schedule.go:30-54`) | In-memory schedules copied from a table; expressions chosen so the one `time.Now()` probe cannot change the outcome | `schedule_test.go:12` |
| `TestTimetableNextUsesTheScheduleTimezone` | `Next` evaluates the cron fields in the schedule's zone and returns a time in that zone (`schedule.go:61`) | Fixed `time.Date` input in UTC; `Equal` and `Location()` assertions | `schedule_test.go:46` |
| `TestTimetableNextAcrossDaylightSavingChanges` | A removed wall-clock time is skipped to the next day; a repeated one yields two consecutive occurrences (`schedule.go:61`, documented at `README.md:381-383`) | Fixed instants on the 2026 New York transition days; the previous occurrence fed back into `Next` | `schedule_test.go:64` |
| `TestLoadPreservesScheduleAndRejectsEmptyOne` | The `schedule` object round-trips through a file; `"schedule":{}` fails with a cron error instead of meaning no schedule (`workflow.go:25`, `workflow.go:70-73`, `schedule.go:30-32`) | Temporary files; `DeepEqual` through the `*Schedule` pointer | `schedule_test.go:96` |

## Check yourself

1. `TestLoadDefinitionSizeLimit` pads each file with spaces rather than making the description longer. Name two things that padding makes possible.

<details><summary>Answer</summary>
First, every fixture decodes to the same value, so L51 can compare each successful load with the one <code>want</code> value; a longer description would need a different expected value per size. Second, trailing whitespace is exactly what a loader that read only a 64 KiB prefix would cut off, leaving a valid document. The one-byte-over case at L32 therefore proves that <code>workflow.go:164</code> reads the extra byte and <code>workflow.go:169-170</code> rejects the file, instead of silently accepting a truncated prefix (the comment at L38-L39).
</details>

2. In `TestValidateTCP`, the subtests at L19-L24 write into the shared `definition` variable. Why is that safe, and what single change to the test would make it unsafe?

<details><summary>Answer</summary>
Subtests started with <code>t.Run</code> run one at a time and each returns before the next begins, so the writes never overlap, and the next loop resets the config at L51 before using it. Calling <code>t.Parallel()</code> inside the subtest closure would make it unsafe: the parent loop would continue while subtests ran concurrently, and several goroutines would read and write <code>definition.Steps[1].Config.Host</code> at once, a data race that <code>go test -race</code> (part of <code>make test</code>) would report.
</details>

3. `TestStepJSONRejectsDuplicateMembers` requires the error to contain `duplicate step field`, while `TestStepJSONRejectsWrongFields` accepts any error. Why does the duplicate test need the stronger check?

<details><summary>Answer</summary>
Some of its inputs would still fail without the guard, but for the wrong reason. L132 repeats <code>config</code> with the bad object second; with <code>rejectDuplicateStepFields</code> removed, <code>encoding/json</code> would keep the second object and <code>decodeStrict</code> at <code>step_json.go:45</code> would reject its unknown field. A test that only checked for an error would pass for that row while the guard was missing. Requiring the text from <code>step_json.go:80</code> proves the rejection happened at <code>step_json.go:15</code>, before any decoding. The wrong-fields test does not need this because each of its inputs has exactly one way to fail.
</details>

4. Which line of `NewTimetable` reads the real clock, and why can `TestValidateSchedule` still run at any date without a fake clock?

<details><summary>Answer</summary>
<code>schedule.go:48</code> calls <code>timetable.Next(time.Now())</code> to find the first occurrence, and <code>schedule.go:52</code> uses that occurrence for the one-minute gap check. The test's expressions are chosen so the date cannot change the verdict: <code>*/5 * * * *</code> and <code>@every 10s</code> have the same gap in every year, and <code>0 0 30 2 *</code> never matches because February has no 30th, so the five-year search returns the zero time from any starting point.
</details>

5. L36-L37 of `schedule_test.go` copy the table's schedule before taking its address. Who owns the `Schedule` that `Validate` receives, and what does `Runner.Start` do with the same field?

<details><summary>Answer</summary>
The subtest's local variable <code>schedule</code> owns it; <code>d.Schedule</code> points at that local copy, not at the table row, so nothing that reads or later edits the definition can reach the table. <code>Runner.Start</code> applies the same rule when it accepts a definition: <code>runner.go:110-113</code> copies <code>*definition.Schedule</code> into a new value and repoints the field, so the run keeps its own schedule even if the caller's changes. <code>AGENTS.md:82</code> states the general rule, "Return copied snapshots."
</details>

6. A workflow file contains `"schedule": {}`. Trace which lines decide its fate, and say why the project treats it as an error rather than as a manual workflow.

<details><summary>Answer</summary>
Decoding produces a non-nil <code>*Schedule</code> with empty <code>Cron</code> and <code>Timezone</code>. <code>Validate</code> at <code>workflow.go:70</code> tests only for nil, so it calls <code>NewTimetable</code> (<code>workflow.go:71</code>), which fails at <code>schedule.go:30-32</code> with <code>schedule needs a cron expression</code>. <code>Load</code> wraps that with the file path at <code>workflow.go:181-182</code>, and <code>main.go:58-61</code> stops startup. The comment at <code>schedule_test.go:114</code> gives the reason: an empty object is a mistake by someone who meant to add a schedule, and accepting it as "no schedule" would hide that mistake until nobody noticed the workflow never ran on its own.
</details>

## Experiments

1. **Drop the extra byte from the size check.** In `internal/workflow/workflow.go:164` change `maxDefinitionBytes+1` to `maxDefinitionBytes`. Run `go test ./internal/workflow -run TestLoadDefinitionSizeLimit -v`. Expected: `TestLoadDefinitionSizeLimit/one_byte_over_limit` and `TestLoadDefinitionSizeLimit/much_larger_file` fail with `expected size rejection with the file path`, because the loader now reads a 64 KiB prefix, finds valid JSON followed by spaces, and accepts it. The three cases at or under the limit still pass. Restore `+1` and rerun; all five pass.

2. **Remove the unknown-type branch from the step decoder.** In `internal/workflow/step_json.go` delete lines 49-50, the `default:` case. The file still compiles. Run `go test ./internal/workflow -run TestStepJSONRejectsWrongFields -v`. Expected: the test fails with `accepted invalid step:` followed by the `ssh.command` input from `tcp_test.go:116`, because the switch now falls through with an empty config and `*s = step` runs. With the lines still removed, run `go test ./internal/workflow -run TestValidate -v` (the pattern also matches `TestValidateTCP` and `TestValidateSchedule`): `TestValidate/unsupported_node` still passes, because `Validate` catches the type at `workflow.go:110` for an in-memory definition, which is why both switches exist. Restore the two lines; the first command passes again.

3. **Stop converting the input to the schedule's zone.** In `internal/workflow/schedule.go:61` change `after.In(t.location)` to `after`. Run `go test ./internal/workflow -run TestTimetableNext -v`. Expected: `TestTimetableNextUsesTheScheduleTimezone` fails with `want 2026-01-15 14:00:00 +0000 UTC, got 2026-01-16 09:00:00 +0000 UTC`, because the cron library now reads 09:00 as UTC, which has already passed at 12:00 UTC, and moves to the next day. Both subtests of `TestTimetableNextAcrossDaylightSavingChanges` fail as well, since UTC has no daylight-saving change. Restore the call; all pass.

4. **Select subtests by name.** Run `go test ./internal/workflow -run 'TestValidateTCP/long' -v`. Expected: only `TestValidateTCP/long_DNS_label`, `TestValidateTCP/long_hostname`, and `TestValidateTCP/long_timeout` appear, all `PASS`, and the parent reports `PASS` too. Spaces in subtest names become underscores, and the pattern after `/` matches subtests. Now run `go test ./internal/workflow -run 'TestValidateSchedule/timezone' -v`. Expected: four subtests run, `timezone_inside_cron`, `missing_timezone`, `implicit_local_timezone`, and `unknown_timezone`, because `-run` matches a substring anywhere in the name, and all four `PASS`.

## Next

Chapter 21, "Tests: runner execution, persistence callbacks, and progress saves" ([21-tests-engine-execution-and-saves.md](21-tests-engine-execution-and-saves.md))
