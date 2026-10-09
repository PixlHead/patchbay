# 2. Workflow JSON files and templates

Files: [workflows/01-patchbay.json](../../workflows/01-patchbay.json), [workflows/http-check.json](../../workflows/http-check.json), [workflows/scheduled-check.json](../../workflows/scheduled-check.json), [workflows/sequence.json](../../workflows/sequence.json), [workflows/tcp-check.json](../../workflows/tcp-check.json), [examples/http-check.json](../../examples/http-check.json), [examples/scheduled-check.json](../../examples/scheduled-check.json), [examples/sequence.json](../../examples/sequence.json), [examples/tcp-check.json](../../examples/tcp-check.json)

Revision: commit 1c4786e

## Role in the system

These nine files are the only way to author a workflow in Patchbay today. There is no write API and no file watcher. The canvas in the browser creates drafts that live in React state and vanish on reload (`AGENTS.md:70`). So a workflow exists because a `.json` file sits in the directory named by the `-workflows` flag (`cmd/server/main.go:30`, default `workflows`). The five files under `workflows/` are loaded at every startup. The four files under `examples/` are copied by nobody: no Makefile target, Dockerfile line, Compose mount, or Go function reads them. They are templates that the README tells you to copy into `workflows/` and edit (`README.md:223-230`). Three of the five loaded files are byte-for-byte copies of those templates and a fourth differs from its template only in a URL (`workflows/scheduled-check.json` line 16), so the two directories mostly overlap; only `workflows/01-patchbay.json` has no template.

Data flows in one direction. At startup `workflow.Load` reads each file as bytes, caps it at 64 KiB, decodes it strictly into a `workflow.Definition`, validates it, and rejects the whole startup on the first problem (`internal/workflow/workflow.go:146-194`, called from `cmd/server/main.go:58-61`). The resulting slice of definitions is held in memory and handed to two consumers: the scheduler, which keeps only definitions that carry a `schedule` (`internal/schedule/scheduler.go:58-67`), and the HTTP API, which serves the list on `GET /api/workflows` and looks a definition up by `id` when the browser posts to `/api/workflows/{id}/runs` (`internal/httpapi/api.go:37-49`, `api.go:61-65`). From there `Runner.Start` validates the definition a second time, copies it, builds a `Run` whose steps mirror the file's `steps` array, and asks the store to save the run together with a JSON snapshot of the definition before anything executes (`internal/engine/runner.go:105-157`, `internal/store/runs.go:18-33`). The execution loop then walks `definition.Steps` in file order and hands each `Step` to the executor chosen by its `type` (`runner.go:225`, `runner.go:247`, `cmd/server/main.go:151-162`).

So on the path from a browser click to a saved run, these files sit at the very beginning: they are parsed once, before the HTTP server listens, and every later object (the sidebar entry, the run record, the `definition_json` column, the executor's timeout) is a copy derived from them. Editing a file changes nothing until the next restart, and a run that already happened keeps the snapshot of the file as it was at that moment (`docs/m0-walkthrough.md:41-43`, `README.md:65-66`).

## Concepts you need

- **JSON documents.** JSON has objects (`{}` with string keys), arrays (`[]`), strings, numbers, booleans and `null`. It has no comments and no trailing commas, which is why the roadmap tells authors to put notes into an explicit `description` field instead (`docs/roadmap.md:38`). Each workflow file is exactly one top-level object. [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259)
- **Go struct tags for `encoding/json`.** A tag such as `` `json:"schemaVersion"` `` on a struct field tells the decoder which JSON key fills that field. Without a tag Go would match the Go field name case-insensitively. Every key in these files maps to one tagged field in `internal/workflow/workflow.go` or `schedule.go`. [encoding/json Marshal](https://pkg.go.dev/encoding/json#Marshal)
- **`omitempty` and pointer fields for optional objects.** `Schedule *Schedule` with `omitempty` means a missing `schedule` key leaves the pointer `nil`, and `nil` is written back as "no key" when the definition is encoded for the API or the database. A plain struct value could not express "absent", only "all fields empty". [encoding/json Marshal](https://pkg.go.dev/encoding/json#Marshal)
- **Strict decoding with `Decoder.DisallowUnknownFields`.** By default `encoding/json` ignores keys it does not recognize. Patchbay turns that off so a typo like `"timeoutMS"` becomes a startup error instead of a silently dropped setting. The loader also checks that only one JSON value exists in the file. [Decoder.DisallowUnknownFields](https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields)
- **Custom `UnmarshalJSON` and `json.RawMessage`.** A type can take over its own decoding by implementing `UnmarshalJSON`. `Step` does this so it can read `type` first, keep `config` as raw bytes, and then decode those bytes with the field set that belongs to that type. [json.Unmarshaler](https://pkg.go.dev/encoding/json#Unmarshaler), [json.RawMessage](https://pkg.go.dev/encoding/json#RawMessage)
- **Error wrapping with `%w`.** `fmt.Errorf("%s: %w", path, err)` builds a new error that still contains the original one, so the message names the file and `errors.Is` can still inspect the cause. This is how a startup failure tells you which file broke. [fmt.Errorf](https://pkg.go.dev/fmt#Errorf)
- **Regular expressions.** `regexp.MustCompile` builds a matcher once at package load. The identifier pattern limits IDs to lowercase letters, digits, `_` and `-`, so they are safe inside URLs and SQL keys. [regexp](https://pkg.go.dev/regexp)
- **`net/url` parsing.** `url.Parse` splits a URL into scheme, host, user info, path and fragment, so validation can insist on `http`/`https`, a hostname, and no embedded credentials. [url.Parse](https://pkg.go.dev/net/url#Parse)
- **Cron expressions (robfig/cron v3).** A standard expression has five fields: minute, hour, day of month, month, day of week. `*/5` in the minute field means "every fifth minute". `ParseStandard` also accepts descriptors like `@hourly` and `@every 30m`. [Cron expression format](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format)
- **IANA time zones in Go.** `time.LoadLocation("Europe/Amsterdam")` reads zone rules from the host's zoneinfo database. Importing `time/tzdata` embeds a copy in the binary so the lookup also works in a minimal container. [time.LoadLocation](https://pkg.go.dev/time#LoadLocation), [time/tzdata](https://pkg.go.dev/time/tzdata)
- **Command-line flags.** `flag.String("workflows", "workflows", ...)` defines a flag with a default value. Running the binary without `-workflows` uses `workflows`, relative to the current directory. [flag](https://pkg.go.dev/flag)
- **`os.ReadDir` and `io.LimitReader`.** `os.ReadDir` returns a directory's entries sorted by file name, which is why load order equals alphabetical order. `io.LimitReader` wraps a file so a read stops after N bytes, which bounds memory for a huge file. [os.ReadDir](https://pkg.go.dev/os#ReadDir), [io.LimitReader](https://pkg.go.dev/io#LimitReader)
- **Docker `COPY` versus a Compose bind mount.** The Dockerfile bakes `workflows/` into the image; Compose then mounts your local `./workflows` on top of that path read-only. The mount wins while the container runs, so editing a local file and restarting the container reloads it without rebuilding the image. [Dockerfile COPY](https://docs.docker.com/reference/dockerfile/#copy), [Compose volumes](https://docs.docker.com/reference/compose-file/services/#volumes)
- **Make targets.** A Makefile target is a named recipe. `make api` runs `go run ./cmd/server` with no flags, so it loads the default `workflows` directory. [GNU Make manual](https://www.gnu.org/software/make/manual/make.html)
- **TypeScript discriminated unions.** `WorkflowStep` in `web/src/api.ts` is a union whose `type` field tells TypeScript which `config` shape is present. This mirrors the Go switch on `type` so the UI can read `config.url` for HTTP steps and `config.host` for TCP steps without casts. [Discriminated unions](https://www.typescriptlang.org/docs/handbook/2/narrowing.html#discriminated-unions)
- **Reserved IP address ranges.** `192.168.x.x` is private LAN space that you may own; `192.0.2.x` is reserved for documentation and never routes anywhere. The templates use these so a copied file cannot accidentally probe a stranger's server. [RFC 1918](https://www.rfc-editor.org/rfc/rfc1918), [RFC 5737](https://www.rfc-editor.org/rfc/rfc5737)

## Walkthrough

The Go types behind every file are small, so keep them in view while reading: `Definition` at `internal/workflow/workflow.go:20-27`, `Step` at `workflow.go:29-34`, `CheckConfig` at `workflow.go:39-45`, and `Schedule` at `internal/workflow/schedule.go:13-16`. Validation lives in `Validate` at `workflow.go:63-117`, loading in `Load` at `workflow.go:146-194`, and the type-aware step decoder in `internal/workflow/step_json.go:14-54`.

### workflows/01-patchbay.json

This is the starter workflow that works on a fresh install with no other service (`README.md:22-25`). It checks Patchbay's own health endpoint. The `01-` prefix is not required by the loader; it exists because `os.ReadDir` sorts entries by name (`workflow.go:148`, `workflow.go:154`), the API keeps that order (`internal/httpapi/api.go:39`), the sidebar numbers workflows in that order (`web/src/components/Sidebar.tsx:39`), and the page selects the first one by default (`web/src/pages/WorkflowsPage.tsx:79`). Digits sort before letters, so this file is always workflow `01` and the one you see first.

**L1–L1** — `{` opens the single top-level object. `Load` decodes one value and then requires `io.EOF` (`workflow.go:175-180`); a second object after this one fails with `expected exactly one JSON document` (`workflow_test.go:106` covers that case).

**L2–L2** — `"schemaVersion": 1` fills `Definition.SchemaVersion` (`workflow.go:21`). `Validate` accepts only `1` (`workflow.go:64-66`). The README explains why the field exists before anything needs it: it gives a place to introduce format changes deliberately as the graph model develops (`README.md:339-341`). Do not confuse it with the database schema version, which is already 3 while the file format stays at 1 (`README.md:600`). Without this line the decoder leaves the field `0` and startup fails with `schemaVersion must be 1`.

**L3–L3** — `"id": "patchbay-health"` fills `Definition.ID` (`workflow.go:22`). It must match the identifier regex at `workflow.go:61` (1 to 64 characters of lowercase letters, digits, `_`, `-`) via `workflow.go:67-69`, and it must be unique across the whole directory (`workflow.go:184-187`). The ID is the key for everything that follows: the browser posts to `/workflows/${encodeURIComponent(id)}/runs` (`web/src/pages/WorkflowsPage.tsx:56`), the route handler matches `r.PathValue("id")` against it (`api.go:62`), the runner uses it to refuse an overlapping run of the same workflow (`runner.go:119`, `runner.go:148`), the scheduler keys its next-run map by it (`scheduler.go:88`), and the store writes it to the `workflow_id` column (`internal/store/runs.go:29-32`). The lowercase rule keeps it safe in a URL path without encoding.

**L4–L4** — `"name": "Patchbay health"` fills `Definition.Name` (`workflow.go:23`). It must not be blank after trimming (`workflow.go:67-69`). It is display text: the sidebar shows it (`Sidebar.tsx:43`), the page heading uses it (`WorkflowsPage.tsx:161`), and `Runner.Start` copies it into `Run.WorkflowName` (`runner.go:127`) so history still reads well after you rename the file.

**L5–L5** — `"description": "Check this Patchbay server's health endpoint."` fills `Definition.Description` (`workflow.go:24`). No validation rule touches it; the only bound is the 64 KiB file limit (`workflow.go:18`, `workflow.go:169-171`). The heading renders it under the title (`WorkflowsPage.tsx:165`). The roadmap chose JSON as the canonical format and said comments must become description fields (`docs/roadmap.md:30`, `docs/roadmap.md:38`), so this is where an author explains intent.

**L6–L6** — `"steps": [` opens the array that fills `Definition.Steps` (`workflow.go:26`). The array must have 1 to 20 entries (`workflow.go:75-77`). Its order is the execution order: the run loop is `for i, step := range definition.Steps` (`runner.go:225`), and `README.md:339` says there are no graph edges yet.

**L7–L7** — `{` opens the first (and only) step. Because `Step` has its own `UnmarshalJSON`, the decoder calls `step_json.go:14` for this object. The first thing it does is scan the keys for duplicates (`step_json.go:15`, `step_json.go:58-90`), lowercasing them because `encoding/json` matches keys case-insensitively (`step_json.go:77-78`). Without that scan, `"config": {...}, "config": {...}` would let a second config overwrite the first and hide an unknown field (`step_json.go:56-57`).

**L8–L8** — `"id": "health"` fills `Step.ID` (`workflow.go:30`, read through the wire struct at `step_json.go:19`). The same identifier regex applies and the ID must be unique within this workflow only (`workflow.go:78-83`); another file may reuse `health`, and `workflows/scheduled-check.json:12` does. The run copies it into each `StepRun.ID` (`runner.go:134`) and the store into `run_steps.step_id` (`runs.go:48-51`), which is how a saved step result is matched back to its definition.

**L9–L9** — `"name": "Check Patchbay"` fills `Step.Name` (`workflow.go:31`). It must not be blank (`workflow.go:84-86`). The step list shows it (`web/src/components/StepList.tsx:23`), the run copies it (`runner.go:134`), and a progress-save failure quotes it in the run error (`runner.go:238`).

**L10–L10** — `"type": "http.check"` fills `Step.Type` (`workflow.go:32`). This one string drives three switches: the decoder picks the HTTP config shape (`step_json.go:28-29`), `Validate` picks the HTTP rules (`workflow.go:87-88`), and `checkExecutor` picks `httpNode.Execute` (`cmd/server/main.go:153-155`). The roadmap names exactly these three switches as today's substitute for a node registry (`docs/roadmap.md:99`). Any other value fails at decode time with `type must be http.check or tcp.check` (`step_json.go:49-50`).

**L11–L11** — `"config": {` opens the object that is first captured as raw bytes in `json.RawMessage` (`step_json.go:22`) and only then decoded strictly into the HTTP-only struct at `step_json.go:30-37`. The result is stored as a `CheckConfig` with the TCP fields left zero (`step_json.go:38`). The comment at `workflow.go:36-38` explains that one struct holds both check types as plain values; the roadmap plans to split it when SSH arrives (`docs/roadmap.md:101`).

**L12–L12** — `"url": "http://127.0.0.1:8080/api/health"` fills `CheckConfig.URL` (`workflow.go:40`). `Validate` parses it and requires an `http` or `https` scheme, a hostname, no user info, and no fragment (`workflow.go:92-95`). The executor builds a GET request from it unchanged (`internal/nodes/httpcheck/http.go:42`) and records it in the result (`http.go:39`). The loader never expands variables (`README.md:302`). This exact URL works because the default listen address is `127.0.0.1:8080` (`main.go:29`), `/api/health` answers 200 (`api.go:34-36`), and inside Docker the server binds `0.0.0.0:8080` so loopback still reaches it (`Dockerfile:24`); the Compose health check uses the same URL (`compose.yaml:14`). If you change the native `-addr` port you must change this line too (`README.md:324-327`).

**L13–L13** — `"expectedStatus": 200` fills `CheckConfig.ExpectedStatus` (`workflow.go:41`). It must be between 100 and 599 (`workflow.go:96-98`). The executor compares the response code to it to decide `Healthy` (`http.go:67`) and writes the received code, and on a mismatch both numbers, into the reason text (`http.go:68-72`). A mismatch is health data, not an execution error (`README.md:236-239`).

**L14–L14** — `"timeoutMs": 3000` fills `CheckConfig.TimeoutMS` (`workflow.go:44`, the only field without `omitempty` because both check types require it). It must be between 100 and 30000 (`workflow.go:112-114`). The executor derives a per-step context deadline from it (`http.go:40`) and names it in the timeout reason (`http.go:58`). The UI prints it in the step list (`StepList.tsx:41`). Without a bound, a huge value would let one step hold an active-run slot for as long as the target stalls.

**L15–L15** — `}` closes `config`. `decodeStrict` then requires that the raw bytes held exactly one JSON value (`step_json.go:98-100`), and `DisallowUnknownFields` at `step_json.go:94` has already rejected any key that is not `url`, `expectedStatus` or `timeoutMs`, including an empty `"host": ""` (`tcp_test.go:109`).

**L16–L16** — `}` closes the step. Only now does `UnmarshalJSON` assign `*s = step` (`step_json.go:52`), so a failed decode leaves the previous value untouched (`tcp_test.go:123-125`).

**L17–L17** — `]` closes `steps`.

**L18–L18** — `}` closes the definition. `Load` then confirms nothing follows (`workflow.go:178-180`), runs `Validate` (`workflow.go:181-183`), checks the ID against earlier files (`workflow.go:184-187`), and appends the definition (`workflow.go:188`).

### workflows/http-check.json

A copy of `examples/http-check.json`, added to the loaded directory in commit 7bc8df5. It points at a private LAN address, so on most machines its check ends unhealthy with a connection failure or a 3000 ms timeout. That is by design: the run still completes, and the unhealthy card shows what a real outage looks like (`README.md:236-239`).

**L1–L1** — `{` opens the document; same single-document rule as above (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `Definition.SchemaVersion` (`workflow.go:21`), checked at `workflow.go:64-66`.

**L3–L3** — `"id": "my-service"`; `Definition.ID` (`workflow.go:22`), regex at `workflow.go:61`. Because this file is loaded at every startup, the ID `my-service` is already taken. Copying `examples/http-check.json` into `workflows/` under a new file name without changing this line fails startup with `duplicate workflow id "my-service"` (`workflow.go:184-187`). The README's copy instructions do not mention the ID (`README.md:225-229`), so change it yourself when you copy.

**L4–L4** — `"name": "My service"`; `Definition.Name` (`workflow.go:23`), non-blank at `workflow.go:67-69`.

**L5–L5** — `"description": "Set the URL to an HTTP health endpoint you control before running."`; `Definition.Description` (`workflow.go:24`), displayed at `WorkflowsPage.tsx:165`. The sentence is the template's instruction to its user, placed in the only field the format allows for prose.

**L6–L6** — `"steps": [`; `Definition.Steps` (`workflow.go:26`), 1 to 20 entries (`workflow.go:75-77`).

**L7–L7** — `{` opens the step; duplicate-key scan at `step_json.go:15`.

**L8–L8** — `"id": "health"`; `Step.ID` (`workflow.go:30`), unique within this file (`workflow.go:80-82`).

**L9–L9** — `"name": "Check health"`; `Step.Name` (`workflow.go:31`), non-blank at `workflow.go:84-86`.

**L10–L10** — `"type": "http.check"`; `Step.Type` (`workflow.go:32`), selects the HTTP decoder (`step_json.go:29`), HTTP validation (`workflow.go:88`), and the HTTP executor (`main.go:154-155`).

**L11–L11** — `"config": {`; captured raw at `step_json.go:22`, decoded at `step_json.go:30-38`.

**L12–L12** — `"url": "http://192.168.1.50:8080/health"`; `CheckConfig.URL` (`workflow.go:40`), validated at `workflow.go:92-95`. `192.168.1.50` is RFC 1918 private space: it is a placeholder for a host on your own LAN. Unless such a host exists, `http.go:48` returns an error, `http.go:60` records `Request failed: ...` (bounded by `resulttext.LimitReason`), and `http.go:62` returns `nil` so the run continues. Inside Docker, a `localhost` URL would mean the container itself, so a LAN address is the right kind of example (`README.md:298-300`).

**L13–L13** — `"expectedStatus": 200`; `CheckConfig.ExpectedStatus` (`workflow.go:41`), range at `workflow.go:96-98`, compared at `http.go:67`.

**L14–L14** — `"timeoutMs": 3000`; `CheckConfig.TimeoutMS` (`workflow.go:44`), range at `workflow.go:112-114`, deadline at `http.go:40`. With an unreachable LAN address this value decides how long the step waits before reporting `No response within 3000 ms` (`http.go:58`).

**L15–L15** — `}` closes `config`; strict single-value check at `step_json.go:98-100`.

**L16–L16** — `}` closes the step; assignment at `step_json.go:52`.

**L17–L17** — `]` closes `steps`.

**L18–L18** — `}` closes the definition; `Validate` and the duplicate-ID check follow (`workflow.go:181-187`).

### workflows/scheduled-check.json

The only loaded file with a `schedule`. It makes the default install start a run of this workflow every five minutes, automatically, for as long as the server is up. Commit 7bc8df5 added it already pointing at Patchbay itself, but with a typo (`172.0.0.1`), while the template created in the same commit pointed at the LAN placeholder; commit 97cd953 corrected the URL to `127.0.0.1`. The loaded copy has therefore differed from its template on line 16 since both were created.

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `Definition.SchemaVersion` (`workflow.go:21`), checked at `workflow.go:64-66`.

**L3–L3** — `"id": "scheduled-health"`; `Definition.ID` (`workflow.go:22`). The scheduler stores the next occurrence under this key (`scheduler.go:88`, `scheduler.go:141`), the API reads it back through `NextRun(definition.ID)` (`api.go:42`), and the runner uses it to skip an occurrence while the previous run of this same workflow is still queued or running (`runner.go:119`, `scheduler.go:129-130`).

**L4–L4** — `"name": "Scheduled health check"`; `Definition.Name` (`workflow.go:23`). The sidebar appends `Scheduled` after the step count because `schedule` is present (`Sidebar.tsx:47`, `web/src/format.ts:23-25`).

**L5–L5** — `"description": "Check an HTTP health endpoint every five minutes. Set the URL and timezone before use."`; `Definition.Description` (`workflow.go:24`). The heading shows it, followed by the cron summary on its own line (`WorkflowsPage.tsx:163-170`, `format.ts:29-35`).

**L6–L6** — `"schedule": {` fills `Definition.Schedule`, a `*Schedule` with `omitempty` (`workflow.go:25`). Because the key is present the pointer is non-nil, so `Validate` runs `NewTimetable` on it (`workflow.go:70-74`) and `schedule.New` keeps this definition as a scheduler entry (`scheduler.go:59-66`). An empty `{}` here is rejected, not treated as "manual": `schedule_test.go:114-122` pins that down. The roadmap asked for exactly this: a trigger stored separately from executable steps, with an explicit time zone (`docs/roadmap.md:97`, `docs/roadmap.md:126`).

**L7–L7** — `"cron": "*/5 * * * *"` fills `Schedule.Cron` (`schedule.go:14`). `NewTimetable` rejects a blank expression (`schedule.go:30-32`), rejects `TZ=` inside it so the zone cannot be smuggled past the `timezone` field (`schedule.go:33-35`), parses it with `cron.ParseStandard` (`schedule.go:36-39`), requires at least one occurrence in the next five years (`schedule.go:48-51`), and requires consecutive occurrences at least one minute apart (`schedule.go:26`, `schedule.go:52-54`). Five fields mean minute, hour, day of month, month, day of week (`README.md:366-367`); `*/5` in the minute field yields :00, :05, :10 and so on. This is the same interval the roadmap's service-monitoring workflow specifies (`docs/roadmap.md:24`). At runtime the scheduler sleeps until each occurrence and then calls `runner.Start` (`scheduler.go:119`, `scheduler.go:125`); it never executes a step itself (`AGENTS.md:100-102`).

**L8–L8** — `"timezone": "UTC"` fills `Schedule.Timezone` (`schedule.go:15`). It must be present and must not be `Local` (`schedule.go:40-42`), and `time.LoadLocation` must know it (`schedule.go:43-46`). The rule exists so the file means the same thing on every host (`README.md:369-372`); `Local` would make the schedule depend on whichever machine runs it. `UTC` needs no zone database, but names like `Europe/Amsterdam` do, which is why `main.go:17` imports `time/tzdata` and `Dockerfile:16` installs `tzdata`. Occurrences are computed in this zone (`schedule.go:60-62`) and `nextRunAt` is reported in it (`README.md:388-389`).

**L9–L9** — `},` closes `schedule`. Nothing else is allowed inside it: `Schedule` has only two fields and the loader's `DisallowUnknownFields` (`workflow.go:174`) applies to nested objects too.

**L10–L10** — `"steps": [`; `Definition.Steps` (`workflow.go:26`), 1 to 20 (`workflow.go:75-77`). A schedule does not change how steps run; it only decides when `Start` is called.

**L11–L11** — `{` opens the step; `step_json.go:14-15`.

**L12–L12** — `"id": "health"`; `Step.ID` (`workflow.go:30`). The same step ID as `workflows/01-patchbay.json:8`, which is fine because uniqueness is per workflow (`workflow.go:78-83`).

**L13–L13** — `"name": "Check health"`; `Step.Name` (`workflow.go:31`, `workflow.go:84-86`).

**L14–L14** — `"type": "http.check"`; `Step.Type` (`workflow.go:32`); decoder `step_json.go:29`, validation `workflow.go:88`, executor `main.go:154-155`.

**L15–L15** — `"config": {`; raw capture `step_json.go:22`, HTTP decode `step_json.go:30-38`.

**L16–L16** — `"url": "http://127.0.0.1:8080/api/health"`; `CheckConfig.URL` (`workflow.go:40`), validated at `workflow.go:92-95`. This is the line that differs from `examples/scheduled-check.json:16`. Pointing the loaded copy at Patchbay itself means the five-minute schedule produces healthy runs out of the box, and it is also why your run history fills with `scheduled-health` entries while you are developing. The history list shows the newest 100 runs across workflows (`api.go:92`), so this schedule can push older manual runs out of the list.

**L17–L17** — `"expectedStatus": 200`; `CheckConfig.ExpectedStatus` (`workflow.go:41`, `workflow.go:96-98`, `http.go:67`).

**L18–L18** — `"timeoutMs": 3000`; `CheckConfig.TimeoutMS` (`workflow.go:44`, `workflow.go:112-114`, `http.go:40`).

**L19–L19** — `}` closes `config` (`step_json.go:98-100`).

**L20–L20** — `}` closes the step (`step_json.go:52`).

**L21–L21** — `]` closes `steps`.

**L22–L22** — `}` closes the definition. After `Validate`, `schedule.New` builds a `Timetable` for it a second time (`scheduler.go:62`), and `Start` logs `schedule active` with the cron, zone and first occurrence (`scheduler.go:85-92`).

### workflows/sequence.json

Two HTTP steps against two LAN placeholders. The file exists to show that `steps` is an ordered list and that an unhealthy first result does not stop the second step. It is a copy of `examples/sequence.json`, added to the loaded directory in commit 7bc8df5.

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`.

**L3–L3** — `"id": "two-services"`; `workflow.go:22`, regex `workflow.go:61`, unique across files `workflow.go:184-187`.

**L4–L4** — `"name": "Two services, in sequence"`; `workflow.go:23`. The comma and spaces are fine here; only `id` is restricted.

**L5–L5** — `"description": "Set both URLs to your own services. Checks execute in order, even if a result is unhealthy."`; `workflow.go:24`. The second sentence states the engine rule this file demonstrates: a check that observes a failure returns `nil` error (`http.go:62`), so the loop records the output as `succeeded` with `healthy: false` and moves on (`runner.go:256-258`). Only an executor error (`runner.go:250-255`), a failed progress save (`runner.go:233-243`, `runner.go:269-280`), or cancellation stops later steps.

**L6–L6** — `"steps": [`; `workflow.go:26`. Two entries, within the 1 to 20 bound (`workflow.go:75-77`).

**L7–L7** — `{` opens the first step (`step_json.go:14`).

**L8–L8** — `"id": "first-service"`; `Step.ID` (`workflow.go:30`). Hyphens are allowed by the regex (`workflow.go:61`). Both step IDs in this file are recorded in the `seen` map at `workflow.go:78-83`.

**L9–L9** — `"name": "Check first service"`; `workflow.go:31`, `workflow.go:84-86`. Shown with its position number in the step list (`StepList.tsx:19`, `StepList.tsx:23`).

**L10–L10** — `"type": "http.check"`; `workflow.go:32`; `step_json.go:29`, `workflow.go:88`, `main.go:154-155`.

**L11–L11** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L12–L12** — `"url": "http://192.168.1.50:8080/health"`; `workflow.go:40`, `workflow.go:92-95`, `http.go:42`. Same placeholder as `workflows/http-check.json:12`.

**L13–L13** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`, `http.go:67`.

**L14–L14** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`, `http.go:40`. Because steps run one after another, the worst case for this file is 6000 ms of waiting before the run finishes.

**L15–L15** — `}` closes the first `config` (`step_json.go:98-100`).

**L16–L16** — `},` closes the first step (`step_json.go:52`). The comma separates array elements; a trailing comma after the last element would be a JSON syntax error and fail at `workflow.go:175-177`.

**L17–L17** — `{` opens the second step. `UnmarshalJSON` runs again from scratch for this object (`step_json.go:14`).

**L18–L18** — `"id": "second-service"`; `workflow.go:30`. Must differ from `first-service` (`workflow.go:80-82`); `workflow_test.go:65` covers the duplicate case.

**L19–L19** — `"name": "Check second service"`; `workflow.go:31`, `workflow.go:84-86`.

**L20–L20** — `"type": "http.check"`; `workflow.go:32`. Steps may mix types; `tcp_test.go:73-89` loads an HTTP step followed by a TCP step.

**L21–L21** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L22–L22** — `"url": "http://192.168.1.51:8080/health"`; `workflow.go:40`, `workflow.go:92-95`. A second placeholder address, one higher, so the two steps are visibly different targets.

**L23–L23** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`.

**L24–L24** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`.

**L25–L25** — `}` closes the second `config` (`step_json.go:98-100`).

**L26–L26** — `}` closes the second step (`step_json.go:52`).

**L27–L27** — `]` closes `steps`. `Runner.Start` later allocates `len(definition.Steps)` step records in this order (`runner.go:128`, `runner.go:133-135`), and the store writes them with a `position` column (`runs.go:38`, `runs.go:48-51`).

**L28–L28** — `}` closes the definition (`workflow.go:178-187`).

### workflows/tcp-check.json

The only loaded TCP workflow. It is a copy of `examples/tcp-check.json` (created in commit 14ba1b0 with TCP support; copied into `workflows/` in 7bc8df5). Its host is a documentation-only address, so the check cannot succeed: it ends with `No connection within 3000 ms` when the host forwards the packets and gets no answer, or `Connection failed: ...` when the host has no route to that block.

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`. TCP support did not bump this number; the README explains that database schema version 3 records TCP support instead, without rewriting saved JSON (`README.md:588-591`, `README.md:600`).

**L3–L3** — `"id": "tcp-service-check"`; `workflow.go:22`, `workflow.go:61`, `workflow.go:184-187`.

**L4–L4** — `"name": "TCP service check"`; `workflow.go:23`, `workflow.go:67-69`.

**L5–L5** — `"description": "Confirm that a configured port accepts TCP connections."`; `workflow.go:24`. The wording matches what a TCP check proves and nothing more: `README.md:251-255` says `Connected` means the port accepts connections, not that the application behind it is healthy.

**L6–L6** — `"steps": [`; `workflow.go:26`, `workflow.go:75-77`.

**L7–L7** — `{` opens the step (`step_json.go:14-15`).

**L8–L8** — `"id": "port"`; `Step.ID` (`workflow.go:30`), regex `workflow.go:61`.

**L9–L9** — `"name": "SSH port"`; `Step.Name` (`workflow.go:31`). The name says what the author expects on port 22; the check itself never speaks SSH.

**L10–L10** — `"type": "tcp.check"`; `Step.Type` (`workflow.go:32`). This selects the TCP decoder branch (`step_json.go:39-48`), the TCP validation branch (`workflow.go:99-108`), and `tcpNode.Execute` (`main.go:156-157`). The TCP decoder's struct has only `host`, `port`, `timeoutMs` (`step_json.go:40-44`), so a `url` key here is rejected at decode time (`tcp_test.go:111`).

**L11–L11** — `"config": {`; raw capture at `step_json.go:22`, TCP decode at `step_json.go:45-48`, stored as `CheckConfig{Host, Port, TimeoutMS}` with the HTTP fields zero.

**L12–L12** — `"host": "192.0.2.10"` fills `CheckConfig.Host` (`workflow.go:42`). `Validate` first confirms the HTTP fields are empty (`workflow.go:100-102`), then runs `validTCPHost` (`workflow.go:103-105`, body at `workflow.go:119-144`): no URL characters, brackets, `@`, whitespace or control characters (`workflow.go:120-124`); an IP address passes at once, including unbracketed IPv6 with a zone (`workflow.go:125-127`); otherwise it must be a DNS name of valid labels (`workflow.go:128-143`). The port must not be inside the host (`README.md:316-319`). `192.0.2.10` is in RFC 5737's TEST-NET-1 block, reserved for documentation, so the dial at `internal/nodes/tcpcheck/tcp.go:36` cannot succeed; the step reports `No connection within 3000 ms` when the deadline expires (`tcp.go:45-46`) or `Connection failed: ...` when the dial fails at once (`tcp.go:47-48`). The executor joins host and port with `net.JoinHostPort`, which adds IPv6 brackets when needed (`tcp.go:32-34`), and the UI does the same for display (`format.ts:37-40`).

**L13–L13** — `"port": 22` fills `CheckConfig.Port` (`workflow.go:43`). It must be 1 to 65535 (`workflow.go:106-108`). The decoder requires a JSON number: `"22"` as a string or `22.5` are rejected (`tcp_test.go:114-115`). The result records it (`tcp.go:29`), and the step list shows `192.0.2.10:22` with `Expected TCP connection` (`format.ts:49-55`, `StepList.tsx:29`, `StepList.tsx:35`).

**L14–L14** — `"timeoutMs": 3000`; `CheckConfig.TimeoutMS` (`workflow.go:44`), range at `workflow.go:112-114` (the check runs after the type switch, so it applies to both types). The TCP executor's deadline covers DNS lookup and connection setup together (`tcp.go:30-34`).

**L15–L15** — `}` closes `config` (`step_json.go:98-100`). An `expectedStatus` key, even `0`, would have failed here (`tcp_test.go:112`).

**L16–L16** — `}` closes the step (`step_json.go:52`).

**L17–L17** — `]` closes `steps`.

**L18–L18** — `}` closes the definition (`workflow.go:178-187`). When this workflow runs, the saved result carries `"type": "tcp.check"` (`tcp.go:29`, `workflow.go:50`) so the UI labels old HTTP history and new TCP history correctly (`web/src/api.ts:27-40`).

### examples/http-check.json

Byte-for-byte identical to `workflows/http-check.json`. Created in commit 0d3a085, which removed the earlier demo fixtures and introduced the "templates for services you control" idea (`README.md:223-230`, `README.md:410-411`). Nothing loads this file: `Dockerfile:20` copies only `workflows/`, `compose.yaml:8` mounts only `./workflows`, `web/playwright.config.ts:12` points at `tests/fixtures/workflows`, and `grep examples/` across the repository finds only documentation. `AGENTS.md:131` still requires keeping these examples in step with validation changes, because a template that no longer loads would mislead the person who copies it.

**L1–L1** — `{` opens the document. If copied into `workflows/`, the single-document rule applies (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`.

**L3–L3** — `"id": "my-service"`. Change this line when you copy the file: the loaded `workflows/http-check.json:3` already owns `my-service`, and two files with one ID fail startup (`workflow.go:184-187`).

**L4–L4** — `"name": "My service"`; `workflow.go:23`, `workflow.go:67-69`.

**L5–L5** — `"description": "Set the URL to an HTTP health endpoint you control before running."`; `workflow.go:24`. The instruction to the copier.

**L6–L6** — `"steps": [`; `workflow.go:26`, `workflow.go:75-77`.

**L7–L7** — `{` opens the step (`step_json.go:14`).

**L8–L8** — `"id": "health"`; `workflow.go:30`, `workflow.go:80-82`.

**L9–L9** — `"name": "Check health"`; `workflow.go:31`, `workflow.go:84-86`.

**L10–L10** — `"type": "http.check"`; `workflow.go:32`; `step_json.go:29`, `workflow.go:88`, `main.go:154-155`.

**L11–L11** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L12–L12** — `"url": "http://192.168.1.50:8080/health"`; `workflow.go:40`, `workflow.go:92-95`. The line the README tells you to replace (`README.md:295`). Keep it absolute with `http` or `https`; a relative path or `file://` URL is rejected (`workflow_test.go:67-68`).

**L13–L13** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`. Set it to the code your endpoint really returns; `docs/m0-walkthrough.md:123-124` suggests changing it to 503 as an exercise.

**L14–L14** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`.

**L15–L15** — `}` closes `config` (`step_json.go:98-100`).

**L16–L16** — `}` closes the step (`step_json.go:52`).

**L17–L17** — `]` closes `steps`.

**L18–L18** — `}` closes the definition.

### examples/scheduled-check.json

The template for a cron-driven workflow (`README.md:343-372`). Created in commit 7bc8df5 together with its loaded copy. It differs from `workflows/scheduled-check.json` on one line only: line 16 points at the LAN placeholder instead of Patchbay itself.

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`.

**L3–L3** — `"id": "scheduled-health"`. Already owned by `workflows/scheduled-check.json:3`; change it when copying (`workflow.go:184-187`).

**L4–L4** — `"name": "Scheduled health check"`; `workflow.go:23`, `workflow.go:67-69`.

**L5–L5** — `"description": "Check an HTTP health endpoint every five minutes. Set the URL and timezone before use."`; `workflow.go:24`. "Set the timezone" is a real instruction: the `UTC` placeholder on line 8 is valid, but a schedule like `0 9 * * *` only means 9 in the morning for you if the zone is yours.

**L6–L6** — `"schedule": {`; `Definition.Schedule` (`workflow.go:25`), validated at `workflow.go:70-74`, consumed at `scheduler.go:59-66`.

**L7–L7** — `"cron": "*/5 * * * *"`; `Schedule.Cron` (`schedule.go:14`), checks at `schedule.go:30-39` and `schedule.go:48-54`. `schedule_test.go:23-31` lists what is rejected: words instead of fields, six fields, `CRON_TZ=` inside the expression, `@every 10s`, and a date that never exists.

**L8–L8** — `"timezone": "UTC"`; `Schedule.Timezone` (`schedule.go:15`), checks at `schedule.go:40-46`. Replace with an IANA name such as `America/New_York`; `Local` and a missing value are rejected (`schedule_test.go:27-29`).

**L9–L9** — `},` closes `schedule`.

**L10–L10** — `"steps": [`; `workflow.go:26`, `workflow.go:75-77`.

**L11–L11** — `{` opens the step (`step_json.go:14`).

**L12–L12** — `"id": "health"`; `workflow.go:30`, `workflow.go:80-82`.

**L13–L13** — `"name": "Check health"`; `workflow.go:31`, `workflow.go:84-86`.

**L14–L14** — `"type": "http.check"`; `workflow.go:32`; `step_json.go:29`, `workflow.go:88`, `main.go:154-155`.

**L15–L15** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L16–L16** — `"url": "http://192.168.1.50:8080/health"`; `workflow.go:40`, `workflow.go:92-95`. The one line that differs from the loaded copy (`workflows/scheduled-check.json:16`). As a template it points at your LAN, not at Patchbay, because a scheduled check of Patchbay by Patchbay cannot alert when Patchbay is down (`README.md:25`).

**L17–L17** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`.

**L18–L18** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`.

**L19–L19** — `}` closes `config` (`step_json.go:98-100`).

**L20–L20** — `}` closes the step (`step_json.go:52`).

**L21–L21** — `]` closes `steps`.

**L22–L22** — `}` closes the definition.

### examples/sequence.json

Byte-for-byte identical to `workflows/sequence.json`. Created in commit 0d3a085. `README.md:225-226` describes it as "checks two services in order".

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`.

**L3–L3** — `"id": "two-services"`. Already owned by `workflows/sequence.json:3`; change it when copying (`workflow.go:184-187`).

**L4–L4** — `"name": "Two services, in sequence"`; `workflow.go:23`, `workflow.go:67-69`.

**L5–L5** — `"description": "Set both URLs to your own services. Checks execute in order, even if a result is unhealthy."`; `workflow.go:24`. States the sequential rule (`runner.go:225`, `http.go:62`).

**L6–L6** — `"steps": [`; `workflow.go:26`, `workflow.go:75-77`.

**L7–L7** — `{` opens the first step (`step_json.go:14`).

**L8–L8** — `"id": "first-service"`; `workflow.go:30`, `workflow.go:80-82`.

**L9–L9** — `"name": "Check first service"`; `workflow.go:31`, `workflow.go:84-86`.

**L10–L10** — `"type": "http.check"`; `workflow.go:32`; `step_json.go:29`, `workflow.go:88`, `main.go:154-155`.

**L11–L11** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L12–L12** — `"url": "http://192.168.1.50:8080/health"`; `workflow.go:40`, `workflow.go:92-95`. First address to replace (`README.md:228`).

**L13–L13** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`.

**L14–L14** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`.

**L15–L15** — `}` closes the first `config` (`step_json.go:98-100`).

**L16–L16** — `},` closes the first step (`step_json.go:52`).

**L17–L17** — `{` opens the second step (`step_json.go:14`).

**L18–L18** — `"id": "second-service"`; `workflow.go:30`, unique within the file (`workflow.go:80-82`).

**L19–L19** — `"name": "Check second service"`; `workflow.go:31`, `workflow.go:84-86`.

**L20–L20** — `"type": "http.check"`; `workflow.go:32`.

**L21–L21** — `"config": {`; `step_json.go:22`, `step_json.go:30-38`.

**L22–L22** — `"url": "http://192.168.1.51:8080/health"`; `workflow.go:40`, `workflow.go:92-95`. Second address to replace.

**L23–L23** — `"expectedStatus": 200`; `workflow.go:41`, `workflow.go:96-98`.

**L24–L24** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`.

**L25–L25** — `}` closes the second `config` (`step_json.go:98-100`).

**L26–L26** — `}` closes the second step (`step_json.go:52`).

**L27–L27** — `]` closes `steps`.

**L28–L28** — `}` closes the definition.

### examples/tcp-check.json

Byte-for-byte identical to `workflows/tcp-check.json`. Created in commit 14ba1b0, the commit that added TCP checks. `README.md:304-322` is the matching documentation, including the host rules.

**L1–L1** — `{` opens the document (`workflow.go:175-180`).

**L2–L2** — `"schemaVersion": 1`; `workflow.go:21`, `workflow.go:64-66`.

**L3–L3** — `"id": "tcp-service-check"`. Already owned by `workflows/tcp-check.json:3`; change it when copying (`workflow.go:184-187`).

**L4–L4** — `"name": "TCP service check"`; `workflow.go:23`, `workflow.go:67-69`.

**L5–L5** — `"description": "Confirm that a configured port accepts TCP connections."`; `workflow.go:24`.

**L6–L6** — `"steps": [`; `workflow.go:26`, `workflow.go:75-77`.

**L7–L7** — `{` opens the step (`step_json.go:14`).

**L8–L8** — `"id": "port"`; `workflow.go:30`, `workflow.go:80-82`. The README's inline example uses `ssh-port` instead (`README.md:309`); both match the regex.

**L9–L9** — `"name": "SSH port"`; `workflow.go:31`, `workflow.go:84-86`.

**L10–L10** — `"type": "tcp.check"`; `workflow.go:32`; TCP decoder `step_json.go:39-48`, TCP validation `workflow.go:99-108`, TCP executor `main.go:156-157`.

**L11–L11** — `"config": {`; `step_json.go:22`, `step_json.go:40-48`.

**L12–L12** — `"host": "192.0.2.10"`; `CheckConfig.Host` (`workflow.go:42`), `validTCPHost` at `workflow.go:119-144`. Replace with a DNS name, IPv4 or unbracketed IPv6 address of a service you own (`README.md:316-320`). `tcp_test.go:18` lists accepted forms and `tcp_test.go:30-41` lists rejected ones such as `nas.home:22`, `[::1]`, and `user@nas.home`.

**L13–L13** — `"port": 22`; `CheckConfig.Port` (`workflow.go:43`), 1 to 65535 (`workflow.go:106-108`).

**L14–L14** — `"timeoutMs": 3000`; `workflow.go:44`, `workflow.go:112-114`, deadline at `tcp.go:30`.

**L15–L15** — `}` closes `config` (`step_json.go:98-100`).

**L16–L16** — `}` closes the step (`step_json.go:52`).

**L17–L17** — `]` closes `steps`.

**L18–L18** — `}` closes the definition.

## How it is used

### Who reads the directories

| Reader | What it does | Where |
| --- | --- | --- |
| `-workflows` flag | Names the directory; default `workflows` relative to the working directory | `cmd/server/main.go:30` |
| `workflow.Load` | Reads every `.json` entry in name order, rejects startup on the first bad file or an empty directory | `internal/workflow/workflow.go:146-194`, called at `main.go:58-61` |
| `make api` | `go run ./cmd/server` with no flags, so it loads `workflows/` | `Makefile:26-27`, `README.md:60` |
| `make build` then `./bin/patchbay` | Same default directory | `Makefile:10-13`, `README.md:71-72` |
| Dockerfile | `COPY workflows/ /app/workflows/` bakes the directory into the image; `WORKDIR /app` makes the default flag resolve to `/app/workflows` | `Dockerfile:17`, `Dockerfile:20`, `Dockerfile:24` |
| Compose | Bind-mounts your local `./workflows` read-only over `/app/workflows`, so `docker compose restart app` reloads edits without a rebuild | `compose.yaml:8`, `README.md:33` |
| Playwright | Starts the binary with `-workflows tests/fixtures/workflows`, so browser tests never load these files | `web/playwright.config.ts:12`, `web/tests/fixtures/workflows/checks.json` |
| Go tests | Write their own files into `t.TempDir()` and call `workflow.Load` on that, so no test depends on `workflows/` | `internal/workflow/workflow_test.go:87-97`, `cmd/server/checks_test.go:38-50` |
| `examples/` | Read by nothing; copied by hand into `workflows/` | `README.md:223-230`, `docs/verification.md:59-60` |

What changes when you edit a file: nothing until restart. `Load` runs once (`docs/m0-walkthrough.md:41-43`, `README.md:65-66`). After a restart the sidebar, the `GET /api/workflows` response, the scheduler's entries, and future runs use the new values. Past runs keep the `definition_json` snapshot written at admission (`internal/store/runs.go:18`, `internal/store/schema.go:21`), so history is independent of the current file (`AGENTS.md:126-127`: "Keep historical results independent of the currently loaded workflow"). A scheduled file also changes startup logging: `scheduler.go:89-90` prints the cron, zone and first occurrence.

### Field map

Every key, the Go field it fills, the rule that checks it, and who uses it afterwards.

| JSON key | Go field | Validation | Consumers |
| --- | --- | --- | --- |
| `schemaVersion` | `Definition.SchemaVersion` `workflow.go:21` | must be `1` `workflow.go:64-66` | echoed by the API `web/src/api.ts:17` |
| `id` | `Definition.ID` `workflow.go:22` | regex `workflow.go:61` at `workflow.go:67-69`; unique per directory `workflow.go:184-187` | route lookup `api.go:62`, overlap key `runner.go:119`, scheduler key `scheduler.go:88`, column `runs.go:30-32` |
| `name` | `Definition.Name` `workflow.go:23` | non-blank `workflow.go:67-69` | `Sidebar.tsx:43`, `WorkflowsPage.tsx:161`, `runner.go:127` |
| `description` | `Definition.Description` `workflow.go:24` | none | `WorkflowsPage.tsx:165` |
| `schedule` | `Definition.Schedule` `workflow.go:25` | `NewTimetable` when present `workflow.go:70-74` | `scheduler.go:59-66`, `format.ts:24`, `WorkflowsPage.tsx:156-158` |
| `schedule.cron` | `Schedule.Cron` `schedule.go:14` | non-blank, no `TZ=`, parses, has an occurrence, at least one minute apart `schedule.go:30-39`, `schedule.go:48-54` | `scheduler.go:87`, `scheduler.go:139`, `format.ts:31` |
| `schedule.timezone` | `Schedule.Timezone` `schedule.go:15` | present, not `Local`, known to `time.LoadLocation` `schedule.go:40-46` | `schedule.go:61`, `format.ts:31` |
| `steps` | `Definition.Steps` `workflow.go:26` | 1 to 20 entries `workflow.go:75-77` | `runner.go:133-135`, `runner.go:225`, `StepList.tsx:14`, `canvasDraft.ts:49` |
| `steps[].id` | `Step.ID` `workflow.go:30` | regex, unique within workflow `workflow.go:80-82` | `runner.go:134`, `runs.go:48-51` |
| `steps[].name` | `Step.Name` `workflow.go:31` | non-blank `workflow.go:84-86` | `runner.go:134`, `runner.go:238`, `StepList.tsx:23` |
| `steps[].type` | `Step.Type` `workflow.go:32` | `http.check` or `tcp.check` `step_json.go:28-50`, `workflow.go:87-110` | `main.go:153-157`, `format.ts:49`, `canvasDraft.ts:49` |
| `steps[].config` | `Step.Config` `workflow.go:33` | decoded per type `step_json.go:29-48`; wrong-type fields rejected `workflow.go:89-91`, `workflow.go:100-102` | `http.go:38`, `tcp.go:28` |
| `config.url` (HTTP) | `CheckConfig.URL` `workflow.go:40` | absolute `http`/`https`, host, no user, no fragment `workflow.go:92-95` | `http.go:39`, `http.go:42`, `format.ts:58` |
| `config.expectedStatus` (HTTP) | `CheckConfig.ExpectedStatus` `workflow.go:41` | 100 to 599 `workflow.go:96-98` | `http.go:67`, `format.ts:59` |
| `config.host` (TCP) | `CheckConfig.Host` `workflow.go:42` | `validTCPHost` `workflow.go:103-105`, `workflow.go:119-144` | `tcp.go:29`, `tcp.go:34`, `format.ts:52` |
| `config.port` (TCP) | `CheckConfig.Port` `workflow.go:43` | 1 to 65535 `workflow.go:106-108` | `tcp.go:29`, `tcp.go:34`, `format.ts:52` |
| `config.timeoutMs` (both) | `CheckConfig.TimeoutMS` `workflow.go:44` | 100 to 30000 `workflow.go:112-114` | `http.go:40`, `tcp.go:30`, `StepList.tsx:41` |

Two rules apply to the whole file rather than a field: at most 64 KiB (`workflow.go:18`, `workflow.go:164-171`, `README.md:335-337`) and no unknown keys anywhere (`workflow.go:174`, `step_json.go:94`). Validation also runs a second time inside `Runner.Start` (`runner.go:106`), because the runner must not trust its callers; both the API and the scheduler go through that one entry point (`AGENTS.md:100`).

## Check yourself

1. You copy `examples/http-check.json` to `workflows/my-nas.json`, change only the URL, and start the server. What happens, and which line decides it?

<details><summary>Answer</summary>
Startup fails with <code>invalid workflow configuration: workflows/my-nas.json: duplicate workflow id "my-service"</code>. The file name does not matter; <code>workflow.go:184-187</code> compares <code>d.ID</code> against every ID already loaded, and <code>workflows/http-check.json:3</code> loaded first because <code>h</code> sorts before <code>m</code>. <code>main.go:58-61</code> turns that error into a non-zero exit before the database is opened. Change line 3 of your copy.
</details>

2. Why does `Patchbay health` appear first in the sidebar and open by default, and what would change if you renamed `01-patchbay.json` to `patchbay.json`?

<details><summary>Answer</summary>
<code>os.ReadDir</code> at <code>workflow.go:148</code> returns entries sorted by name, and <code>Load</code> appends in that order (<code>workflow.go:188</code>). The API preserves slice order (<code>api.go:39</code>), the sidebar numbers entries by index (<code>Sidebar.tsx:39</code>), and the page selects index 0 when no workflow is chosen (<code>WorkflowsPage.tsx:79</code>). Renamed to <code>patchbay.json</code>, it would sort after <code>http-check.json</code>, so <code>My service</code> would become workflow 01 and the default selection. No validation or execution behavior would change.
</details>

3. In `workflows/sequence.json`, the first service is down. Does the second step run? What is the run's status afterwards?

<details><summary>Answer</summary>
Yes. The HTTP executor treats an unreachable target as health data and returns a result with <code>healthy: false</code> and a nil error (<code>http.go:56-62</code>). The loop records that step as <code>succeeded</code> with an unhealthy output (<code>runner.go:256-258</code>) and continues to the second step (<code>runner.go:225</code>). The run finishes as <code>succeeded</code>, which the UI labels <strong>Completed</strong>, while the step card shows <strong>Unhealthy</strong>. Only an executor error, a failed progress save, or cancellation stops later steps (<code>runner.go:233-243</code>, <code>runner.go:250-255</code>).
</details>

4. You delete the `timezone` line from `workflows/scheduled-check.json`. Where does the failure surface, and why does the project refuse to fall back to the host's local zone?

<details><summary>Answer</summary>
<code>NewTimetable</code> returns <code>schedule needs an explicit timezone name such as UTC or Europe/Amsterdam</code> at <code>schedule.go:40-42</code>; <code>Validate</code> passes that error up at <code>workflow.go:70-74</code>; <code>Load</code> prefixes the file path at <code>workflow.go:181-183</code>; the server exits. The rule exists so a file means the same thing on every host (<code>README.md:369-372</code>): a laptop in one zone and a Docker container in UTC would otherwise run the same file at different moments.
</details>

5. The scheduler fires for `scheduled-health` while the previous run of that workflow is still executing. Who decides what happens, and does the scheduler retry?

<details><summary>Answer</summary>
The runner decides. The scheduler only calls <code>s.start</code>, which is <code>runner.Start</code> (<code>scheduler.go:125</code>, wired at <code>main.go:116</code>). <code>Start</code> finds the ID in <code>busyWorkflows</code> and returns <code>ErrWorkflowBusy</code> (<code>runner.go:119-121</code>). The scheduler logs a skip and computes the next occurrence (<code>scheduler.go:129-130</code>, <code>scheduler.go:135-139</code>). It never retries or replays (<code>AGENTS.md:100-102</code>, <code>README.md:374-378</code>).
</details>

6. While the server is running, you change `expectedStatus` in `workflows/01-patchbay.json` to 204 and save. What do the next manual run and the run history show?

<details><summary>Answer</summary>
Nothing changes until restart. The definitions slice was built once at <code>main.go:58</code> and there is no watcher (<code>docs/m0-walkthrough.md:41-43</code>). The next run still expects 200. After a restart the new value applies to new runs, but every earlier run keeps its own <code>definition_json</code> snapshot written at admission (<code>runs.go:18-33</code>), so history still shows the 200 expectation for those runs.
</details>

## Experiments

1. **Trip the timeout bound.** In `workflows/http-check.json` line 14, change `3000` to `50`. Run `go run ./cmd/server`. Expected: the process exits immediately with `invalid workflow configuration: workflows/http-check.json: step "health" timeoutMs must be between 100 and 30000` and never opens the database. Restore the file. The unit test that guards the same rule is `go test ./internal/workflow -run 'TestValidate$'`, whose `unbounded timeout` case (`workflow_test.go:70`) must keep passing.

2. **Add a comment the loader cannot accept.** Add `"comment": "mine",` after line 2 of `workflows/01-patchbay.json`. Run `go run ./cmd/server`. Expected: `invalid workflow configuration: workflows/01-patchbay.json: json: unknown field "comment"` from the strict decoder at `workflow.go:174`. Remove the line. Then run `go test ./internal/workflow -run TestLoadJSONAndRejectBadFiles` and confirm it passes; its first bad fixture (`workflow_test.go:105`) is exactly this mistake.

3. **Weaken the cron floor and watch a named test fail.** In `internal/workflow/schedule.go` line 26, change `time.Minute` to `time.Second`. Run `go test ./internal/workflow -run TestValidateSchedule`. Expected: the subtest `TestValidateSchedule/sub-minute_interval` fails, because `@every 10s` (`schedule_test.go:30`) is now accepted and the test wanted an error mentioning `once a minute`. Revert the constant and rerun; all subtests pass.

4. **Make the TCP template succeed.** Copy `examples/tcp-check.json` to `workflows/local-port.json`. Change line 3 to `"id": "local-port"`, line 12 to `"host": "127.0.0.1"`, and line 13 to `"port": 8080`. Start the server with `go run ./cmd/server`, then run `curl -X POST -H 'Content-Type: application/json' http://127.0.0.1:8080/api/workflows/local-port/runs` and fetch the returned ID with `curl http://127.0.0.1:8080/api/runs/<id>`. Expected: the step output has `"type": "tcp.check"`, `"healthy": true`, and `"reason": "TCP connection established"` (`tcp.go:52-53`). Delete the file afterwards so your directory matches the repository.

## Next

Chapter 3, "Server startup, wiring, shutdown, and the database lock" ([03-server-main-and-database-lock.md](03-server-main-and-database-lock.md))
