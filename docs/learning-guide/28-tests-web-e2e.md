# 28. Tests: Playwright end-to-end checks

Files: [web/tests/workflows.spec.ts](../../web/tests/workflows.spec.ts), [web/tests/tcp.spec.ts](../../web/tests/tcp.spec.ts), [web/playwright.config.ts](../../web/playwright.config.ts), [web/tests/fixtures/workflows/checks.json](../../web/tests/fixtures/workflows/checks.json)

Revision: commit 1c4786e

## Role in the system

Every other test chapter stays inside one Go package. These four files test the whole path at once: a real Chromium browser loads the built frontend from a real Patchbay binary, which runs real `http.check` steps and saves them in a real SQLite file. `web/playwright.config.ts` is the glue. It tells Playwright to start `bin/patchbay` on port 18080 with the fixture directory `web/tests/fixtures/workflows/` and a fresh database under `.cache/e2e/`, and to wait for `GET /api/health` before any test runs. `checks.json` is the one workflow that server loads. Its two steps call the test server's own health endpoint, one expecting 200 and one expecting 204, so a healthy result and an unhealthy result appear without any outside service (`README.md:480-481`). `make e2e` builds the frontend and binary first (`Makefile:19-20`) and then runs `playwright test` (`web/package.json:11`).

The path from a click to a saved run is the one Chapter 11 and Chapter 15 describe. The **Run workflow** button at `web/src/pages/WorkflowsPage.tsx:176-185` fires the mutation at `WorkflowsPage.tsx:54-59`, which sends `POST /api/workflows/browser-checks/runs` with `Content-Type: application/json` and no body. The host guard checks `127.0.0.1` (`internal/httpapi/host_guard.go:22`), the route at `internal/httpapi/api.go:50-88` calls `runner.Start`, which saves the run before admitting it (`internal/engine/runner.go:136-145`) and answers `202` (`api.go:84`). The runner executes both steps in order with `httpcheck.Execute` (`internal/nodes/httpcheck/http.go:37-74`), saving before and after each (`runner.go:233`, `runner.go:269`), then finalizes (`runner.go:286`). Meanwhile the page polls `GET /api/runs` every second (`WorkflowsPage.tsx:47-51`), which reads SQLite (`api.go:92`), and `RunDetails` renders what comes back (`web/src/components/RunDetails.tsx`). The first test in `workflows.spec.ts` drives exactly this path and then reloads the page to prove the result came from the database and not from React state.

The other eleven tests never start a real run. Nine of them use `page.route` to replace `/api/runs` (and in `tcp.spec.ts` also `/api/workflows`) with JSON the test writes itself; the mobile test instead aborts every `/api/` request, and the view-switching test reads the real server without touching the network. Faked responses are how the browser tests inject failure and control time without touching Go: a queued run, a canceled run, an interrupted run, a progress-save failure, a retained unsaved final result, and TCP outputs all come from fake responses shaped by the `Run` and `Workflow` types in `web/src/api.ts`. The Go tests in Chapters 21 to 26 prove the server produces those shapes; these tests prove the frontend renders every one of them readably. This split follows `AGENTS.md:134-137`: fakes, temporary databases, and local servers, with no live homelab service.

## Concepts you need

- **Playwright test runner and `defineConfig`.** `@playwright/test` exports `test` and `expect`. Each `test(title, async ({ page }) => ...)` gets a fresh browser context and a fresh `page`, so routes, viewport changes, and state from one test never leak into the next. `defineConfig` in `playwright.config.ts` types the configuration object: where the specs live, how many worker processes run, and what every test's `page` defaults to. [playwright.dev/docs/test-configuration](https://playwright.dev/docs/test-configuration)
- **Playwright `webServer`.** The config can name a shell command that starts the application and a URL to poll. Playwright runs the command before the first test, waits until the URL answers, runs the tests, and then kills the process. `reuseExistingServer: false` means an already-running server on that port is an error instead of a silent reuse. [playwright.dev/docs/test-webserver](https://playwright.dev/docs/test-webserver)
- **Locators and ARIA roles.** `page.getByRole('button', { name: 'Run workflow' })` finds elements by the role and accessible name a screen reader would see, which is more stable than CSS selectors. A `<section aria-label="...">` has the `region` role, `<article>` is `article`, `<h1>` to `<h6>` are `heading`, and `role="alert"` is `alert`. Locators are lazy: they describe a query and re-run it at every assertion. `filter({ hasText })`, `.first()`, and `locator('.endpoint')` narrow a locator. [playwright.dev/docs/locators](https://playwright.dev/docs/locators) and [MDN: ARIA roles](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Roles)
- **Auto-retrying assertions and `expect.poll`.** `await expect(locator).toBeVisible()` re-checks the page until it passes or the expect timeout (5 seconds by default) ends. `toHaveCount(0)` asserts absence. `expect.poll(fn)` re-runs a function, not a locator, until its return value matches; this chapter uses it to parse JSON out of the page. A plain `expect(value)` without `await` checks once. [playwright.dev/docs/test-assertions](https://playwright.dev/docs/test-assertions)
- **Network interception with `page.route`.** `page.route(glob, handler)` intercepts requests the page makes. `route.fulfill({ json })` answers with that JSON and a JSON content type; `route.fulfill({ status: 409, json })` fakes an error; `route.abort()` makes the browser's `fetch` reject as if the network were down. Routes persist across `page.reload()` until `page.unroute`. [playwright.dev/docs/mock](https://playwright.dev/docs/mock)
- **`page.request` and `page.waitForResponse`.** `page.request.get(path)` sends an HTTP request from the test, not from the page, resolving `path` against `baseURL`. `page.waitForResponse(predicate)` returns a promise that resolves when the page receives a matching response; you create it before the action that triggers the request so the response cannot be missed. [playwright.dev/docs/api-testing](https://playwright.dev/docs/api-testing) and [page.waitForResponse](https://playwright.dev/docs/api/class-page#page-wait-for-response)
- **Viewport, screenshots, and `pageerror`.** `page.setViewportSize` resizes the virtual window, which is how a phone layout is tested on a desktop. `page.screenshot({ path })` captures the viewport to a file. `page.on('pageerror', ...)` collects uncaught JavaScript exceptions, which would otherwise pass silently if the page still looked right. [playwright.dev/docs/emulation#viewport](https://playwright.dev/docs/emulation#viewport), [playwright.dev/docs/screenshots](https://playwright.dev/docs/screenshots), [page 'pageerror' event](https://playwright.dev/docs/api/class-page#page-event-page-error)
- **Parameterized tests with a `for` loop.** Calling `test(...)` inside a loop registers one test per iteration. Titles must be unique, so each embeds the loop variable. The test body uses the variable in conditionals to adjust both the fake data and the assertions. [playwright.dev/docs/test-parameterize](https://playwright.dev/docs/test-parameterize)
- **`async`/`await` and promises.** Every Playwright call that talks to the browser returns a promise. `await` pauses the test function until it settles. Forgetting `await` on an assertion makes it a no-op, which is why every `expect(locator)` in these files is awaited. [MDN: async function](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Statements/async_function)
- **TypeScript `import type`, spread, and `!`.** `import type { Run }` imports only a type and is erased at compile time; `verbatimModuleSyntax` (`web/tsconfig.json:11`) requires it. `{ ...failed, id: 'x', error: undefined }` copies an object and overrides fields. `failed.error!` tells the compiler a value is not `undefined`. Because `tsconfig.json:14` includes `tests`, `npm --prefix web run typecheck` type-checks every fake run against `api.ts`. [TS: type-only imports](https://www.typescriptlang.org/docs/handbook/modules/reference.html#type-only-imports-and-exports), [MDN: spread syntax](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/Spread_syntax), [TS: non-null assertion](https://www.typescriptlang.org/docs/handbook/2/everyday-types.html#non-null-assertion-operator-postfix-)
- **`JSON.stringify` drops `undefined`.** When `route.fulfill({ json })` serializes an object, properties whose value is `undefined` are omitted from the JSON. That is how a fake run "removes" `error` or `finalSaveFailed`, matching the Go side where `omitempty` leaves those fields out (`internal/engine/runner.go:39`, `runner.go:45`). [MDN: JSON.stringify](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/JSON/stringify)
- **TanStack Query polling and retries.** `useQuery` with `refetchInterval: 1000` refetches every second and keeps the last good `data` while an error is set. `retry: false` surfaces the first failure immediately instead of after three delayed retries. These two settings decide how fast the disconnection banner appears and disappears. [TanStack Query: useQuery](https://tanstack.com/query/latest/docs/framework/react/reference/useQuery) and [query retries](https://tanstack.com/query/latest/docs/framework/react/guides/query-retries)
- **TanStack Router hash history and search params.** The app's routes live after `#` (`/#/workflows?view=details`), so the Go file server only ever serves `/`. `validateSearch` turns `?view=` into typed state, and `navigate({ search })` writes it back, which is why a reload keeps the open view. [TanStack Router: history types](https://tanstack.com/router/latest/docs/framework/react/guide/history-types)
- **Shell `&&`, `mktemp -d`, and `exec`.** `a && b` runs `b` only if `a` succeeded. `mktemp -d dir/run.XXXXXX` creates a uniquely named directory. `exec cmd` replaces the shell process with `cmd`, so the process Playwright started is the server itself and the signal Playwright sends at the end reaches it directly. [Bash builtins: exec](https://www.gnu.org/software/bash/manual/html_node/Bourne-Shell-Builtins.html#index-exec) and [coreutils: mktemp](https://www.gnu.org/software/coreutils/manual/html_node/mktemp-invocation.html)
- **Go `flag` package.** `flag.String("addr", "127.0.0.1:8080", ...)` defines a command-line flag with a default. The config passes `-addr`, `-workflows`, `-web`, and `-db` to override the defaults at `cmd/server/main.go:29-32`. [pkg.go.dev/flag](https://pkg.go.dev/flag)
- **Go struct tags, `omitempty`, and `omitzero`.** The JSON the fake runs imitate is produced by `encoding/json` from `engine.Run` (`runner.go:34-46`). `omitempty` drops empty strings, `nil` pointers, and `false`; `omitzero` drops a zero `time.Time`. That is why a queued run has no `startedAt` and a saved run has no `finalSaveFailed` key. [pkg.go.dev/encoding/json](https://pkg.go.dev/encoding/json)

## Walkthrough

### web/tests/workflows.spec.ts

**L1–L3** — Imports `expect` and `test` from Playwright, and the `Run` type from the app's own `web/src/api.ts:52-63`. Using the app's type means every fake run in this file is checked by `tsc` against the same shape the page expects; a typo like `finishedAt: 42` would fail `npm --prefix web run typecheck` before a browser opened. `import type` is required by `web/tsconfig.json:11`.

**L4–L6** — The first test is the only one that starts a real run. Its title states the claim: two checks run in order, the healthy and the unhealthy result both render, and both survive a reload. `{ page }` is Playwright's per-test page fixture; the destructuring is split across lines by Prettier.

**L7–L8** — `errors` collects every uncaught exception the page throws. A rendering bug that throws inside React could leave the page looking fine while an error boundary swallowed it; L53 fails the test if anything landed here.

**L9–L10** — Opens the Details view directly through the hash URL. `router.tsx:34-39` matches `/workflows`, `validateView` at `web/src/router.tsx:28-32` turns `view=details` into the `'details'` state, and `WorkflowsPage.tsx:133-145` renders the details column instead of the canvas. Waiting for the `Browser checks` heading (`web/src/components/PageHeading.tsx:17`, title from `checks.json:4`) proves the first `GET /api/workflows` poll (`WorkflowsPage.tsx:42-46`, served by `api.go:37-49`) succeeded before anything is clicked.

**L11–L13** — Fetches the current history straight from the API with `page.request`, bypassing the page. The route at `api.go:89-102` reads up to 100 runs from SQLite. `previousRuns` is kept so L25 can prove the run created later is new. The database is fresh per server start (`playwright.config.ts:8-13`), so this list is normally empty; the check still holds if someone runs the specs against a reused database.

**L14–L18** — Registers a listener for the start request before clicking. The predicate matches method `POST` and pathname `/api/workflows/browser-checks/runs`, the URL the mutation at `WorkflowsPage.tsx:56` builds with `encodeURIComponent`. Creating the promise first and awaiting it after the click is the ordering that cannot miss a fast response.

**L19–L21** — Clicks **Run workflow** (`WorkflowsPage.tsx:176-185`, handler `startRun` at `WorkflowsPage.tsx:91-94`) and awaits the response. `202` is what `api.go:84` writes after `runner.Start` admits the run; `README.md:164` documents it. A `409`, `429`, `415`, or `500` here would mean the request failed one of the checks at `api.go:53-79`.

**L22–L25** — Decodes the accepted run and checks its ID is a non-empty string that was not in the earlier list. The ID comes from `rand.Text()` at `runner.go:127`. Without L25 a stale database could make the later "completed" assertion pass on an old run.

**L26–L27** — `results` is the `<section aria-label="Run results">` at `RunDetails.tsx:13`; `steps` is every `<article>` inside it, one per step (`RunDetails.tsx:48`). Scoping to the region matters: the same words appear in the history list and the step list, and the assertions must hit the details panel only.

**L28–L34** — The helper reads the `<pre>` that `JsonDetails` renders at `web/src/components/JsonDetails.tsx:6` with the whole run as JSON (`RunDetails.tsx:39`). The comment at `JsonDetails.tsx:1` promises exactly one `<pre>` per panel for this reason. `expect.poll` parses it repeatedly until the object has the new ID, `workflowId: 'browser-checks'`, and `status: 'succeeded'` together. The comment at L28 says why all three are checked at once: an older succeeded run could satisfy a status check alone. The page shows the new run immediately because `onSuccess` at `WorkflowsPage.tsx:60-67` puts it at the top of the cached list and selects it; the status flips to `succeeded` when a later poll returns the row that `finishRun` saved (`runner.go:286`, `runner.go:300-314`). L34 calls the helper for the first time.

**L35–L39** — Four texts from the rendered run. `Completed` is the label for `succeeded` (`web/src/format.ts:6`) in the panel's `StatusBadge` (`RunDetails.tsx:16`). The two reasons are the exact strings the HTTP executor writes at `http.go:69` and `http.go:71`, shown at `RunDetails.tsx:69`. `Healthy` and `Unhealthy` come from `format.ts:83` through the badge at `RunDetails.tsx:52-54`. Together they prove the distinction `AGENTS.md:91` demands: the run succeeded as an execution even though one check reported an unhealthy service, because `http.go:67-73` records a wrong status as health data and returns a `nil` error.

**L40–L43** — The two `<h4>` step names (`RunDetails.tsx:50`) must appear in this exact order. The array form of `toHaveText` also fixes the count at two. The order is the definition order from `checks.json:9` and `checks.json:19`, preserved by `runner.go:133-135` when the run is created and by `ORDER BY position` when steps are read back (`internal/store/run_reads.go:90-91`).

**L44–L46** — Reloads the page. React state is gone, so `runId` is `''` again and `inspectedRun` falls back to `history[0]` (`WorkflowsPage.tsx:82`), the newest run from `ListRuns` (`run_reads.go:39-40`). Calling the helper again proves the newest saved run is this one and it is still `succeeded`. Only `finalSaveFailed` results come from memory (`api.go:144-149`); for a normal run this is SQLite's row.

**L47–L49** — Both reasons render again from saved `output_json` (`run_reads.go:106-109`), and the button is enabled. `WorkflowsPage.tsx:178` disables it only while a start is pending or the connection is down, so this asserts the mutation settled and polling is healthy.

**L50–L52** — Takes a desktop screenshot after making the viewport 2200 pixels tall. The comment explains why: the page shell keeps the document at viewport height and only the main column scrolls (`web/src/components/ScrollColumn.tsx:4-6`, `ScrollColumn.tsx:18`), so a viewport-sized screenshot would cut the history off. The file goes to `web/test-results/`, which `.gitignore:7` excludes; `README.md:486` names the directory.

**L53–L54** — No page errors during the whole flow, including the reload. The closing brace ends the test.

**L55–L56** — The second test covers two things that need no real run: a phone-width layout and the feedback when the backend disappears.

**L57–L60** — Sets a 390 by 844 viewport before navigating, so the first render is already the narrow layout, then waits for the heading and the button. Below Tailwind's `md` breakpoint the sidebar becomes a horizontal strip (`web/src/components/Sidebar.tsx:15`, `Sidebar.tsx:28`) and the button is full width (`WorkflowsPage.tsx:177`).

**L61–L63** — Runs a snippet in the browser: the document's scroll width must not exceed the window width. This is the "no horizontal page scroll" rule for narrow screens; classes such as `min-w-0` at `ScrollColumn.tsx:18` and `wrap-anywhere` at `RunDetails.tsx:44` are what keep long URLs from widening the page. A plain `expect` is enough because the value is already computed.

**L64–L66** — A tall viewport for the mobile screenshot, then back to the phone size so the disconnection checks run at the realistic height.

**L67–L69** — `route.abort()` on every `/api/` request makes the browser's `fetch` at `api.ts:66` reject. The next poll (within one second, `WorkflowsPage.tsx:45` and `:50`) fails, and because `retry: false` at `web/src/queryClient.ts:6-7` the error is set at once. `WorkflowsPage.tsx:73-74` turns it into `connectionError`, and the banner at `WorkflowsPage.tsx:117` renders the message followed by `Retrying automatically.` inside the only `role="alert"` element (`web/src/components/ErrorBanner.tsx:9-13`). L69 checks `disabled={starting || !!connectionError}` (`WorkflowsPage.tsx:178`): with the backend unreachable, a click would only fail.

**L70–L72** — Switches to the Canvas tab (`web/src/components/ViewTabs.tsx:46-59`, handler `showView` at `WorkflowsPage.tsx:96-98`) and expects the same alert. The feedback block sits above the tab panel (`WorkflowsPage.tsx:28-30`, `WorkflowsPage.tsx:114-126`), outside the `view === 'canvas'` branch, which is what `README.md:189` promises.

**L73–L75** — Removes the route. The polling never stopped, so the next refetch succeeds, TanStack Query clears `error`, and the alert count drops to zero. This proves recovery is automatic, with no reload and no click.

**L76–L77** — The third test checks the two views and that the URL remembers the choice.

**L78–L81** — `page.goto('/')` lands on the index route, whose `beforeLoad` at `router.tsx:18-24` redirects to `/workflows`. With no `?view=`, `validateView` (`router.tsx:31`) picks `canvas`. Three locators: the canvas section named `Canvas for Browser checks` (`web/src/components/WorkflowCanvas.tsx:64-67`, name from the draft seeded by `WorkflowsPage.tsx:136`), the step list section named by its heading (`web/src/components/StepList.tsx:8-10`), and the tab list named `Workflow views` (`ViewTabs.tsx:40-44`).

**L82–L84** — The canvas is visible, the heading is visible (here the canvas's own `<h1>` at `WorkflowCanvas.tsx:70-72`), and the step list does not exist. `WorkflowsPage.tsx:133-145` renders either the canvas or the details column, never both, so `toHaveCount(0)` is the right assertion.

**L85–L88** — Clicking **Details** calls `navigate({ search: { view: 'details' }, replace: true })` (`WorkflowsPage.tsx:97`). Now the steps exist and the canvas does not.

**L89–L91** — A reload keeps the step list, because the view is in the URL hash (`router.tsx:26-27`, `README.md:188-189`). If the view lived in React state, the reload would show the canvas and L91 would fail.

**L92–L95** — Back to **Canvas**: canvas present, steps absent. The closing brace ends the test.

**L96–L97** — The fourth test is about ownership of the admission decision: a `running` row in history must not disable the button, because only the server knows whether a run is really active.

**L98–L112** — Replaces `/api/runs` with one run that looks active: `status: 'running'`, a start time, no finish time, one running step. `route.fulfill({ json })` sets `Content-Type: application/json`, which `api.ts:67` requires. Only this URL is faked; `/api/workflows` still comes from the real server, so the run's `workflowId` must match the fixture's `browser-checks` for the filter at `WorkflowsPage.tsx:81` to show it.

**L113–L119** — Fakes the start route with `409` and the exact `ErrWorkflowBusy` text from `runner.go:18`. The comment says what is being tested: the UI's display of a refusal, not the server's decision (that is `runner.go:119-121` and `api.go:69-70`, covered in Chapter 26). The glob `**/api/workflows/browser-checks/runs` does not match `/api/runs`, so the two routes do not collide.

**L120–L121** — Opens the Details view. `No completion recorded yet` is the branch at `format.ts:102` for a started run with no finish time whose status is not `interrupted`; the fake row has `startedAt` and no `finishedAt`.

**L122–L124** — The button is enabled despite the running row, then clicked. `WorkflowsPage.tsx:178` never looks at history; the comment at `WorkflowsPage.tsx:53` and `README.md:581-582` explain that saved running statuses can outlive a crashed process, so the server's admission check is the only truth.

**L125–L127** — The faked `409` body is thrown by `request` as `Error(data.error)` (`api.ts:68-72`), becomes `actionError` (`WorkflowsPage.tsx:76`), and renders in the second banner at `WorkflowsPage.tsx:119`. The button is enabled again because `startMutation.isPending` is false once the mutation settled. The brace ends the test.

**L128–L129** — The fifth test renders what startup cleanup leaves behind: an interrupted run.

**L130–L139** — The fake run has `status: 'interrupted'`, a creation and start time, and no `finishedAt`. That is the row `store.MarkUnfinishedRunsInterrupted` produces at `internal/store/run_interruptions.go:31-32` and `README.md:571-575` describes: the finish time stays unknown because the process died before writing it.

**L140–L153** — The first step is `succeeded` with a full HTTP output. Cleanup leaves terminal steps alone (`run_interruptions.go:22-27`), so a completed result must still render after a restart. The `.invalid` hostname is reserved and never resolves; no test will ever contact it.

**L154–L160** — The step that was running became `interrupted`, and the one that never started became `skipped` (`run_interruptions.go:24-27`). The closing lines end the fulfilled JSON and the route call.

**L161–L162** — Opens the Details view and scopes to the results region.

**L163–L164** — `Interrupted` appears exactly twice inside the panel: the run badge at `RunDetails.tsx:16` and the badge for the step with no output at `RunDetails.tsx:56` (label from `format.ts:9`). The history list also shows a badge (`web/src/components/ExecutionHistory.tsx:55`) but that is outside `results`. `Finish time unknown` is the `interrupted` branch at `format.ts:101`, reached only when there is a start time and no finish time.

**L165–L167** — The completed step's reason, its `Healthy` badge (`format.ts:83`), and the `Skipped` label (`format.ts:11`) all render. This is the frontend half of `AGENTS.md:98-99`: startup marks records interrupted but preserves results.

**L168–L172** — The help text at `format.ts:121-123` explains to the administrator that steps were not resumed, and the button is enabled: an interrupted run is terminal, so a new run may start. The brace ends the test.

**L173–L175** — A loop registers three tests, one per status that can exist without a start time: `queued`, `canceled`, and `interrupted`. The title embeds the status so each test name is unique.

**L176–L185** — The fake run has `createdAt` but no `startedAt`. Only the `canceled` case gets a `finishedAt`: a queued run canceled at shutdown passes through `finishRun`, which stamps a finish time (`runner.go:200-214`, `runner.go:302`), so it has a finish time but no start time (`README.md:176-177`). The conditional spread adds the key only for that case.

**L186–L196** — One step, `pending` while queued and `skipped` otherwise, matching `finishRun` at `runner.go:303-307` and cleanup at `run_interruptions.go:25`. The closing lines end the route.

**L197–L198** — Opens the Details view and scopes to the results region.

**L199–L203** — `runTiming` checks `queued` first (`format.ts:95`, `Waiting for an execution slot`) and then the missing start time (`format.ts:96`, `Never started`). The order of those two lines is the production rule under test: the `canceled` run has a `finishedAt`, and if the subtraction at `format.ts:98` ran first it would compute a duration from `undefined`.

**L204–L205** — No `Invalid Date` anywhere on the page and no `NaN ms` in the panel. `new Date(undefined)` prints `Invalid Date`, and `undefined - number` is `NaN`. The history list formats `createdAt` (`ExecutionHistory.tsx:56-61`), which `engine.Run` always sets (`runner.go:40`, `runner.go:128`), and `format.ts:96` guards the subtraction. `{ exact: false }` is a substring match.

**L206–L211** — For the queued case only: the `Queued` label (`format.ts:4`) and the help sentence at `format.ts:118-120`, which `README.md:168` promises.

**L212–L213** — Close the test and the loop.

**L214–L218** — A second loop registers two tests for a run stopped by a progress-save failure, once with `finalSaveFailed` absent and once with it `true`. The two differ only in whether a second warning appears.

**L219–L229** — The fake run is `failed` with the exact message `runner.go:276` formats when a step's result cannot be saved, naming the step `Expected response`. `finalSaveFailed` takes the loop value. All three timestamps are present because the final save of a failed run normally succeeds (`README.md:534-540`).

**L230–L246** — The first step completed with output; the second is `skipped`. This is `AGENTS.md:86-88` in data form: the runner keeps completed outputs (`runner.go:278-279`) and `finishRun` marks the never-run step skipped (`runner.go:303-307`). The closing lines end the object.

**L247–L253** — A second run built by spreading `failed` and overriding: a new ID, `error: undefined`, `finalSaveFailed: undefined`, and a single step that failed with `Executor failed.`. `JSON.stringify` drops the `undefined` keys, so the page receives a run with no run-level error and no flag, which is what a step failure looks like from `runner.go:250-255`. The type allows `undefined` because both fields are optional (`api.ts:57-58`).

**L254–L256** — Fulfills `/api/runs` with both runs, `failed` first so it is inspected by default (`WorkflowsPage.tsx:82`), opens the Details view, and scopes to the results panel.

**L257–L258** — Filters the panel's alerts to the one containing `Could not save the result` and asserts its full text equals the run's `error`. That is the banner at `RunDetails.tsx:22`; `README.md:538-539` says the reason is displayed above the step results.

**L259–L262** — `Failed` (`format.ts:7`), `Healthy`, the reason, and `Skipped`: the failure reason and the preserved output render together.

**L263–L268** — The panel has two alerts when `finalSaveFailed` is true and one otherwise, because `RunDetails.tsx:23-28` adds the second banner only for the flag. The filtered check reads `This result is temporary` from `RunDetails.tsx:25`. Counting inside `results` keeps page-level banners (`WorkflowsPage.tsx:116-119`) out of the count.

**L269–L271** — After a reload the route still answers, and the reason is still shown. The reason is part of the saved run (`README.md:539-540`), so it must not depend on anything the page remembered.

**L272–L274** — Clicks the history button whose text starts `Run step-fai`, the first eight characters of `step-failure` as `ExecutionHistory.tsx:64` renders them. `onInspect` sets `runId` (`ExecutionHistory.tsx:52`, `WorkflowsPage.tsx:196`). Now there are no alerts, and the step's own `error` renders at `RunDetails.tsx:60`. A run-level reason and a step error are different fields (`runner.go:39`), and the page must not show a stale reason from the previously inspected run.

**L275–L276** — Close the test and the loop.

**L277–L278** — The last test in this file renders the memory-only warning for a result whose final save failed.

**L279–L287** — `status: 'succeeded'` together with `finalSaveFailed: true`. `AGENTS.md:92` says the flag is a warning, not a status, and `runner.go:310` sets it without changing `run.Status`. All timestamps are present because execution finished normally.

**L288–L303** — One succeeded step with output. The result exists; only its last write failed. The closing lines end the object.

**L304–L305** — A copy without the flag, listed second, so the test can switch between a flagged and an unflagged run on the same page.

**L306–L307** — Opens the Details view and scopes to the panel.

**L308–L310** — `Completed`, `Healthy`, and the reason: the execution outcome and the service health render unchanged. `README.md:545-546` promises a successful run still appears as Completed.

**L311–L314** — The alert contains both halves of the sentence at `RunDetails.tsx:25-26`: the result could not be saved, and it may be lost on restart or eviction. This is the wording `AGENTS.md:94` asks for: do not promise the result survives.

**L315–L316** — `Final result not saved` appears exactly once on the whole page, from `ExecutionHistory.tsx:66-70`, which renders it only for the flagged run (`README.md:546-547`). The button is enabled: the warning does not block new runs.

**L317–L320** — Inspects `saved-result` through its history button. Zero alerts proves `RunDetails` derives the warning from the `run` prop at render time and keeps no sticky state from the previous run.

**L321–L323** — The saved run shows `Completed`, and switching back to the flagged run brings the alert back.

**L324–L326** — Reloads and expects the alert again. The comment and `README.md:553` state the rule: a browser reload does not clear the server's memory. Here the fake route stands in for that memory; the Go test in Chapter 26 proves the server really retains it. The brace ends the test.

### web/tests/tcp.spec.ts

**L1–L3** — Imports, now including the `Workflow` type (`api.ts:16-25`) because this test fakes the workflow list as well as the history.

**L4–L6** — One test: TCP results and legacy HTTP history render from saved output, and still do after a reload. No real TCP connection is made anywhere; the TCP executor's own behavior is covered by its Go tests (`internal/nodes/tcpcheck/`, Chapter 23).

**L7–L8** — Page errors are collected as in the first file. They matter more here: a TCP output has no `url` or `expectedStatus`, so a rendering path that assumed HTTP fields could throw.

**L9–L14** — A fake definition `mixed-checks` with `schemaVersion: 1`, the only version `workflow.go:64-66` accepts. Faking the list means the sidebar shows this workflow instead of the fixture and it is selected as the first entry (`WorkflowsPage.tsx:79`).

**L15–L20** — The HTTP step. Its config shape is the `http.check` member of `WorkflowStep` at `api.ts:4-7`.

**L21–L28** — The TCP step with host `::1` and port 8080. The host is stored without brackets; `format.ts:38` notes that configuration stores IPv6 bare and the display adds the brackets, and `workflow.go:103-104` rejects brackets in a definition. The closing lines end the definition.

**L29–L37** — The `mixed` run: `succeeded`, with all three timestamps, for the faked workflow.

**L38–L51** — The HTTP step's output carries an explicit `type: 'http.check'`. The Go side never writes that value: `http.go:39` sets no `Type`, and `workflow.go:50` omits the empty field, so real HTTP results have no `type` key. `api.ts:34` still allows the explicit spelling, and this step proves the page accepts it. The reason is the executor's healthy string (`http.go:69`).

**L52–L64** — A healthy TCP output: `type: 'tcp.check'`, `host`, `port: 9090`, and the reason from `internal/nodes/tcpcheck/tcp.go:53`. The port differs from the definition's 8080 on purpose: the panel must render the saved output, not the current configuration.

**L65–L79** — An unhealthy TCP output for `closed.invalid:22` with `healthy: false`. The TCP executor returns refusals as health data with a `nil` error (`tcp.go:44-50`), so the step is `succeeded` while the service is unreachable. The closing lines end the run.

**L80–L83** — `legacyHTTP` spreads `mixed` and replaces the ID and the steps.

**L84–L99** — Its single step reuses the ID `tcp`, which the faked definition now assigns to a TCP step, but its output has no `type` and carries HTTP fields. The comment at L85 states the rule: saved output still describes HTTP. `AGENTS.md:126-127` says a missing result type means legacy HTTP and historical results stay independent of the loaded workflow; `format.ts:71-73` implements it by branching on `output.type` alone.

**L100** — `acceptedTCP` is `mixed` with only the healthy TCP step, for the summary wording at L149.

**L101–L105** — Fakes both lists. `**/api/workflows` matches only the list route, not the start route or `/api/runs`. `mixed` is first in the history, so it is inspected by default. Then the Details view is opened.

**L106–L108** — The step list region (`StepList.tsx:8-10`) and, inside it, the `<li>` (`StepList.tsx:17`) whose text includes `TCP port`.

**L109–L113** — The TCP definition shows the `TCP CHECK` label (`format.ts:51`, rendered at `StepList.tsx:24-26`, visible because Playwright's default 1280-pixel viewport is above the `md` breakpoint), the bracketed endpoint `[::1]:8080` in the `.endpoint` code element (`StepList.tsx:28`, built by `tcpEndpoint` at `format.ts:37-40`), and `TCP connection` as the expectation (`format.ts:53`, `StepList.tsx:35`). It must not contain `GET` or `HTTP`, which only the HTTP branch at `format.ts:56-60` produces.

**L114–L117** — The first `.endpoint` in the list belongs to the HTTP step and reads `GET http://service.invalid/health` (`format.ts:58`), so the two branches of `stepDefinition` are both exercised on one page.

**L118–L120** — The results region and two articles picked by their step names (`RunDetails.tsx:50`).

**L121–L123** — The helper starts with the run badge `Completed` and the summary `1 check did not pass`. `runSummary` at `format.ts:105-115` counts unhealthy outputs and, because `hasTcpStep` is true (`format.ts:90-92`), uses the "check" wording instead of "service".

**L124–L130** — `HTTP 200` is the HTTP step's metric value (`format.ts:85`, rendered at `RunDetails.tsx:79`). In the connected article: the `Connected` badge (`format.ts:75`), the endpoint `[::1]:9090` in a `.endpoint` element that `RunDetails.tsx:70-73` renders only when the presentation sets one (TCP does at `format.ts:76`; HTTP never does), `Accepted` (`format.ts:78`), and `Time to connect` followed by `4 ms` (`format.ts:79`, `RunDetails.tsx:83-86`). The article must not mention `HTTP` or `Time to headers`.

**L131–L137** — In the unreachable article: `Unreachable` (`format.ts:75`), `Not established` (`format.ts:78`), and `closed.invalid:22` without brackets (`format.ts:39`). The panel also shows the TCP help sentence from `RunDetails.tsx:33-38`, which `hasTcpStep` gates. The brace ends the helper.

**L138–L140** — Runs the checks, reloads, runs them again. The route survives the reload, so what this proves is that a fresh page builds the same rendering from saved output alone.

**L141–L146** — Inspects `legacy-http`. Its output has no `type`, so `outputPresentation` takes the HTTP branch (`format.ts:82-87`): `HTTP 204` as the metric, `Time to headers` with `12 ms`, and a `Healthy` badge. `Time to connect` must be absent. If the page chose the renderer from the current definition's step type, this run would wrongly render as TCP.

**L147–L152** — Inspects `accepted-tcp`: with no unhealthy outputs and a TCP step, the summary is `All checks passed` (`format.ts:113`) and the HTTP wording `All services healthy` is absent. Finally, no page errors were thrown while rendering outputs that lack HTTP fields. The brace ends the test.

### web/playwright.config.ts

**L1** — `defineConfig` gives the exported object Playwright's configuration type, so a misspelled key fails `tsc` (the file is included by `web/tsconfig.json:14`).

**L2–L4** — The exported configuration begins. `testDir: './tests'` is relative to this file, so both `.spec.ts` files in `web/tests/` are collected.

**L5** — One worker process. All tests share one server and one database, the first test starts a real run of the only workflow, and the same-workflow overlap rule at `runner.go:119-121` would answer a second concurrent start with `409`. The screenshots also write to fixed paths. Serial execution removes every one of those collisions.

**L6** — `baseURL` makes `page.goto('/')` and `page.request.get('/api/runs')` resolve against the test server. `127.0.0.1` is on the host guard's default allowlist (`host_guard.go:22`); `README.md:112-113` says the port does not matter. `headless: true` runs Chromium without a window.

**L7–L8** — The `webServer` block, with its comment: make a fresh database only when starting the test server, and keep it for debugging. `README.md:475-478` repeats both halves.

**L9–L11** — The first parts of the shell command. Playwright runs it with the config file's directory as the working directory, so `../.cache/e2e` is the repository's `.cache/e2e/`, which `.gitignore:2` excludes. `mktemp -d` creates a unique `run.XXXXXX` directory per server start, so a previous run's history can never satisfy an assertion (`README.md:476-477`). The `&&` chain stops at the first failure.

**L12–L13** — `exec` replaces the shell with `../bin/patchbay`, so the process Playwright owns and later signals is the server itself. The flags override the defaults at `cmd/server/main.go:29-32`: listen on `127.0.0.1:18080`, load `tests/fixtures/workflows` through `workflow.Load` (`internal/workflow/workflow.go:147-157`, `.json` files only), serve the built `dist` directory, and put SQLite in the fresh directory. `make e2e` depends on `build` (`Makefile:19-20`), which produces both `dist` and `bin/patchbay` (`Makefile:10-13`). `main.go:62` creates the database directory with mode `0700` and `store.Open` creates the file.

**L14** — Joins the four fragments with spaces into one command string.

**L15** — Playwright polls this URL until it answers before running any test. `GET /api/health` at `api.go:34-36` returns `{"status":"ok"}` once the server is listening, which happens only after migrations, the write check, and interruption cleanup succeeded (`main.go:75-130`). `README.md:432` calls it app readiness, not monitored-service health, and that is exactly what the gate needs.

**L16–L18** — `reuseExistingServer: false`: if something already listens on 18080, Playwright reports an error instead of running the specs against a development server with its own history. The closing lines end the config.

### web/tests/fixtures/workflows/checks.json

**L1–L5** — A workflow file in the format Chapter 2 describes. `schemaVersion` must be 1 (`workflow.go:64-66`). The `id` `browser-checks` matches the identifier rule at `workflow.go:61` and `workflow.go:67` and is the path segment the first test waits for (`workflows.spec.ts:17`). The `name` is the heading every test waits for, and the description renders under it (`WorkflowsPage.tsx:162-173`).

**L6** — The steps array; `workflow.go:75-77` requires 1 to 20 entries.

**L7–L16** — Step `healthy`: an `http.check` against the test server's own `/api/health` on port 18080, the address from `playwright.config.ts:12`. The decoder at `internal/workflow/step_json.go:30-38` selects `url`, `expectedStatus`, and `timeoutMs` for this type. `200` is what the health route returns (`api.go:35`), so `http.go:67-69` marks the result healthy with `Received expected HTTP 200`, the text asserted at `workflows.spec.ts:36`. `3000` milliseconds is within the 100 to 30000 range at `workflow.go:112-114`, and far above what a loopback request needs.

**L17–L26** — Step `unhealthy`: the same URL but `expectedStatus: 204`. The health route still returns 200, so `http.go:70-71` writes `Expected HTTP 204; received HTTP 200` and `healthy` is false, while the step itself succeeds because `http.go:73` returns a `nil` error for a wrong status, just as `http.go:62` does for an unreachable service. This is the "deliberately expects HTTP 204" trick `README.md:480-481` describes: an unhealthy result with no outside service.

**L27–L28** — Close the array and the object. The loader rejects unknown fields (`workflow.go:174`), so the file contains only the keys the types define.

## What these tests prove

| test function | production rule it proves (production file:line) | how it injects failure or controls time | test file:line |
| --- | --- | --- | --- |
| `run sequential checks and retain healthy and unhealthy results on reload` | A click sends the JSON `POST` and gets `202` (`web/src/pages/WorkflowsPage.tsx:54-59`, `internal/httpapi/api.go:53-60`, `api.go:84`); steps run in definition order and both outputs are saved and served after reload (`internal/engine/runner.go:133-135`, `runner.go:225-285`, `internal/store/run_reads.go:39-40`, `run_reads.go:90-91`); an unhealthy check is still a succeeded step (`internal/nodes/httpcheck/http.go:62-73`, `AGENTS.md:91`); the main column scrolls, not the document (`web/src/components/ScrollColumn.tsx:4-6`) | Real server, fresh SQLite file, real executor against the server's own `/api/health`; `page.request` for a baseline; `waitForResponse` registered before the click; `expect.poll` on the panel's JSON; `pageerror` capture | `web/tests/workflows.spec.ts:4` |
| `expectNewRunCompleted` (helper) | One `<pre>` per panel holds the inspected run's JSON (`web/src/components/JsonDetails.tsx:1-7`, `web/src/components/RunDetails.tsx:39`); the new run is selected on success and replaced by server data on the next poll (`WorkflowsPage.tsx:60-67`, `WorkflowsPage.tsx:82`) | `expect.poll` with `JSON.parse`, matching ID, workflow, and status together | `web/tests/workflows.spec.ts:29` |
| `mobile layout and backend-disconnection feedback` | No horizontal page scroll at phone width; the first failed poll shows the banner and disables Run (`web/src/queryClient.ts:6-7`, `WorkflowsPage.tsx:73-74`, `WorkflowsPage.tsx:117`, `WorkflowsPage.tsx:178`, `web/src/components/ErrorBanner.tsx:9-13`); the banner shows in both views (`WorkflowsPage.tsx:28-30`, `WorkflowsPage.tsx:114-126`, `README.md:189`); polling recovers by itself (`WorkflowsPage.tsx:45`, `WorkflowsPage.tsx:50`) | `setViewportSize` 390 by 844; `page.route('**/api/**', abort)` then `unroute`; `page.evaluate` for scroll width | `web/tests/workflows.spec.ts:56` |
| `the Canvas and Details views switch and survive a reload` | `/` redirects to `/workflows` with the canvas view (`web/src/router.tsx:18-24`, `router.tsx:31`); exactly one view renders (`WorkflowsPage.tsx:133-145`); the view lives in `?view=` and survives reload (`WorkflowsPage.tsx:97`, `router.tsx:26-32`, `README.md:188-189`) | Real server; `page.reload()`; `toHaveCount(0)` for the absent view | `web/tests/workflows.spec.ts:77` |
| `an old unfinished run does not disable starting another workflow` | Run is enabled regardless of saved statuses; the server's admission check is authoritative and its refusal is displayed (`WorkflowsPage.tsx:53`, `WorkflowsPage.tsx:178`, `WorkflowsPage.tsx:119`, `web/src/api.ts:68-72`, `README.md:581-582`); `No completion recorded yet` for a running run (`web/src/format.ts:102`) | `route.fulfill` of `/api/runs` with a `running` row and of the start route with `409` and the `ErrWorkflowBusy` text (`runner.go:18`) | `web/tests/workflows.spec.ts:97` |
| `interrupted history preserves results and shows an unknown finish time` | Startup cleanup output renders: interrupted run and step, skipped step, preserved output, unknown finish time, help text, Run enabled (`internal/store/run_interruptions.go:24-32`, `format.ts:101`, `format.ts:121-123`, `RunDetails.tsx:16`, `RunDetails.tsx:56`, `AGENTS.md:98-99`, `README.md:571-575`) | `route.fulfill` with an `interrupted` run that has no `finishedAt` | `web/tests/workflows.spec.ts:129` |
| `a ${status} run without a start time has readable history` (three tests) | `runTiming` checks `queued` and then the missing start before subtracting times (`format.ts:95-99`); no `Invalid Date` or `NaN ms`; queued label and help (`format.ts:4`, `format.ts:118-120`); a canceled queued run has a finish time but no start (`runner.go:200-214`, `runner.go:302`, `README.md:176-177`) | Loop over three statuses; conditional `finishedAt` and step status in the fake row | `web/tests/workflows.spec.ts:175` |
| `progress-save failure explains the stopped run` (two tests) | The run-level reason from `runner.go:276` renders above preserved outputs and skipped steps (`RunDetails.tsx:22`, `runner.go:278-279`, `runner.go:303-307`, `AGENTS.md:86-88`, `README.md:534-540`); the final-save warning is a second alert only when flagged (`RunDetails.tsx:23-28`); a step error is a different field (`runner.go:39`, `RunDetails.tsx:60`); switching runs clears the reason | Loop over `finalSaveFailed`; spread with `undefined` to drop keys; `page.reload()`; history button click | `web/tests/workflows.spec.ts:216` |
| `an unsaved final result keeps its execution status and shows a warning` | `finalSaveFailed` is a warning, not a status (`runner.go:45`, `runner.go:310`, `AGENTS.md:92`, `README.md:544-547`); the warning renders in the panel and once in the list (`RunDetails.tsx:23-28`, `web/src/components/ExecutionHistory.tsx:66-70`); it belongs to the run, not to component state; a reload does not clear it (`README.md:553`) | `route.fulfill` with a flagged and an unflagged copy; button clicks; `page.reload()` | `web/tests/workflows.spec.ts:278` |
| `TCP results and legacy HTTP history render from saved output after reload` | Step definitions and results branch on their own type (`format.ts:48-61`, `format.ts:71-88`); IPv6 hosts are bracketed for display (`format.ts:37-40`); a result without `type` is HTTP even when the current step ID is TCP (`api.ts:33-34`, `AGENTS.md:126-127`); TCP wording in summary and help (`format.ts:105-115`, `format.ts:124-127`, `RunDetails.tsx:33-38`); TCP-only endpoint line (`RunDetails.tsx:70-73`) | `route.fulfill` of `/api/workflows` and `/api/runs`; saved port differs from configured port; `page.reload()`; `pageerror` capture | `web/tests/tcp.spec.ts:4` |
| `expectMixedResults` (helper) | Mixed HTTP and TCP outputs render side by side with no cross-contamination of labels | Re-run before and after reload | `web/tests/tcp.spec.ts:121` |
| `playwright.config.ts` `webServer` | The built binary starts with fixture workflows and a fresh database, and is ready only when `/api/health` answers (`cmd/server/main.go:29-32`, `main.go:62`, `main.go:75-130`, `api.go:34-36`, `README.md:474-478`) | `mktemp -d` per start; `exec`; `reuseExistingServer: false`; one worker | `web/playwright.config.ts:7` |
| `checks.json` (fixture) | A valid definition (`internal/workflow/workflow.go:63-117`) whose two steps produce one healthy and one unhealthy HTTP result from the server's own health route (`http.go:67-71`, `README.md:480-481`) | Different `expectedStatus` values against the same loopback URL; no outside service | `web/tests/fixtures/workflows/checks.json:1` |

## Check yourself

1. In the first test, `startResponsePromise` is created at L14–L18 and awaited at L20, with the click in between. What would go wrong if the click came first, and why does the test read the run ID from the response rather than from the page?

<details><summary>Answer</summary>`page.waitForResponse` only sees responses that arrive after it is registered. On a loopback server the `202` can come back within a few milliseconds of the click, so registering afterwards could miss it and the test would hang until its timeout. Reading the ID from the response is the only way to know which run the server created: the page shows `Run ` plus eight characters of the ID (`ExecutionHistory.tsx:64`), which is not enough to compare against `previousRuns` at L25, and reading it from the `<pre>` would make the "new run" check depend on the very rendering the test is trying to verify. The response is the server's own statement (`api.go:84`), so the later `expect.poll` compares the page against ground truth.</details>

2. The mobile test aborts every `/api/` request at L67 and expects the alert within Playwright's default five-second assertion timeout. Which two settings make that fast, and what would change if `retry` in `queryClient.ts:7` were set to `3`?

<details><summary>Answer</summary>`refetchInterval: 1000` at `WorkflowsPage.tsx:45` and `:50` means the next poll happens within a second of the abort, and `retry: false` at `queryClient.ts:7` means that one failed fetch is reported immediately as the query's `error`. The comment at `queryClient.ts:6` states this intent. With `retry: 3`, TanStack Query would retry with delays that double from one second (1, 2, 4 seconds) before setting `error`, so the banner would appear after roughly seven seconds and L68 would time out. The same setting would also delay the disabled-button check at L69. The recovery at L73–L74 works for the mirror reason: polling never stops while in error, so the first successful poll after `unroute` clears the error.</details>

3. The interrupted, queued, canceled, and unsaved-result tests all fake `/api/runs`. Why can none of those states be produced by driving the real test server from a browser test, and where is each state's production behavior actually proven?

<details><summary>Answer</summary>They belong to the server's lifecycle, not to any HTTP request. `interrupted` is written only by `MarkUnfinishedRunsInterrupted` at startup (`run_interruptions.go:12-13`), which would require killing the server mid-run and restarting it under Playwright's control. `canceled` without a start time comes from `Close` draining the queue (`runner.go:176-191`). `finalSaveFailed` needs three failed `UpdateRun` attempts (`runner.go:325-343`), which the Go tests cause with a SQLite trigger. `queued` needs both active slots full. The Go chapters prove the server produces these rows: Chapter 22 for cancel and queue, Chapter 25 for interruption cleanup (`internal/store/run_interruptions_test.go`), Chapters 21 and 26 for final-save failure and the overlay. The browser tests then take those rows as given and prove the page renders them without inventing times or hiding results. The two halves meet at the `Run` type in `api.ts:52-63`, which `tsc` checks on both the fakes and the components.</details>

4. In the "old unfinished run" test, the history contains a `running` run and the button is still enabled. Who owns the decision that a workflow may start, and what bug would a frontend-side "disable while a run is running" rule introduce?

<details><summary>Answer</summary>The runner owns it, under its mutex: `Start` checks `busyWorkflows` at `runner.go:119-121` and capacity at `runner.go:122-125`, and the API maps the refusal to `409` or `429` (`api.go:69-72`). `AGENTS.md:78-79` requires those checks to live there. The page only forwards the request and shows the answer (`WorkflowsPage.tsx:119`). If the page disabled the button whenever history showed `running`, a saved `running` row left by a crash would block the workflow until startup cleanup rewrote it, and a stale poll could block a workflow that had already finished. The comment at `WorkflowsPage.tsx:53` and `README.md:581-582` state this rule, and L123 and L126 pin it: enabled before the refusal, enabled after it.</details>

5. In the progress-save test, clicking `Run step-fai` at L272 must leave zero alerts. Which production lines make that true, and what would a stale alert indicate about where the warning state lives?

<details><summary>Answer</summary>`RunDetails` renders `run.error` and `run.finalSaveFailed` straight from its prop at `RunDetails.tsx:22-28`; it keeps no `useState`. The inspected run is selected by `runId` in `WorkflowsPage.tsx:82` and changes when `onInspect` calls `setRunId` (`ExecutionHistory.tsx:52`, `WorkflowsPage.tsx:196`). The second run has no `error` and no flag because `JSON.stringify` dropped the `undefined` keys at L250–L251, so nothing renders. A stale alert would mean some component had copied the warning into its own state when the first run was shown, which would also make the unsaved-result test fail at L320. Keeping the warning in the run data matches the server, where the flag is set once on the retained snapshot (`runner.go:310`) and never stored separately.</details>

6. `legacyHTTP` in `tcp.spec.ts` gives its step the ID `tcp`, the same ID the faked definition uses for a TCP step. Why must the panel render it as HTTP, and which line decides?

<details><summary>Answer</summary>History must stay readable after a definition changes. `AGENTS.md:126-127` says a missing result type means legacy HTTP and historical results are independent of the loaded workflow; `api.ts:33-34` encodes that as an optional `type`. `outputPresentation` at `format.ts:72-73` branches only on `output.type === 'tcp.check'`, so an output with no type takes the HTTP branch at `format.ts:82-87` regardless of what the current definition says about step `tcp`. If the page looked up the step's type in the workflow instead, L143–L146 would fail: the metric would read `Not established` or similar and `Time to connect` would appear. The server side of the same rule is schema version 3 in Chapter 9, which records TCP support without rewriting saved JSON.</details>

## Experiments

1. Change `"expectedStatus": 204` at `web/tests/fixtures/workflows/checks.json:23` to `200`, then run `PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" make e2e` (install Chromium first with the command at `README.md:469` if you have not). Expect `run sequential checks and retain healthy and unhealthy results on reload` to fail at `workflows.spec.ts:37` after the five-second assertion timeout, because both steps now report `Received expected HTTP 200` and the `Unhealthy` badge at L39 never appears either. Every other test passes: they fake history and never depend on the fixture's second step. Restore the file.

2. Change `retry: false` at `web/src/queryClient.ts:7` to `retry: 3`, run `make build`, then `PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web run test:e2e -- --grep "mobile layout"`. Predict a failure at `workflows.spec.ts:68`: the query retries with growing delays before it reports an error, so the alert does not appear within five seconds. `npm --prefix web run typecheck` still passes, because the change is a value, not a type. Restore the line and rebuild.

3. Change `'Finish time unknown'` at `web/src/format.ts:101` to `'Finish time not recorded'`, run `make build`, then `PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web run test:e2e -- --grep "interrupted"`. Two tests match the pattern. Predict that `interrupted history preserves results and shows an unknown finish time` fails at `workflows.spec.ts:164`, while `a interrupted run without a start time has readable history` still passes: its fake run has no `startedAt`, so `runTiming` returns `Never started` at `format.ts:96` before reaching the changed line. Restore the string and rebuild.

4. Change `"id": "browser-checks"` at `checks.json:3` to `"id": "Browser-checks"` and run `make e2e`. No test runs: `workflow.Load` rejects the uppercase identifier at `workflow.go:67-68`, `runServer` returns the error at `cmd/server/main.go:59-60`, and the process exits before `/api/health` ever answers, so Playwright reports that the `webServer` process could not start and prints the Go error. This shows the health URL at `playwright.config.ts:15` is a startup gate: a server that fails validation never gets a chance to fail a test for a confusing reason. Restore the file.

## Next

Guide index: [README.md](README.md). What comes next in the project: [docs/roadmap.md](../roadmap.md).
