# 17. Small presentational components and formatting helpers

Files: [web/src/components/AppHeader.tsx](../../web/src/components/AppHeader.tsx), [web/src/components/Breadcrumb.tsx](../../web/src/components/Breadcrumb.tsx), [web/src/components/PageHeading.tsx](../../web/src/components/PageHeading.tsx), [web/src/components/Badge.tsx](../../web/src/components/Badge.tsx), [web/src/components/StatusBadge.tsx](../../web/src/components/StatusBadge.tsx), [web/src/components/ErrorBanner.tsx](../../web/src/components/ErrorBanner.tsx), [web/src/components/JsonDetails.tsx](../../web/src/components/JsonDetails.tsx), [web/src/components/ScrollColumn.tsx](../../web/src/components/ScrollColumn.tsx), [web/src/components/classes.ts](../../web/src/components/classes.ts), [web/src/format.ts](../../web/src/format.ts)

Revision: commit 1c4786e

## Role in the system

These ten files are the leaves of the frontend tree. Eight of them are React components that take plain props and return elements; one is a list of shared Tailwind class strings; one is a module of pure functions that turn API values into words. None of them imports `request` from `web/src/api.ts`, calls `useQuery`, or reads the workspace context. `AGENTS.md:59` calls the components "Presentational pieces" and `AGENTS.md:60` calls `format.ts` "Pure label, timing, summary, and check-result presentation helpers". That is the whole contract: data comes in as props or arguments, elements or strings go out, and nothing here starts a request or changes state that another file owns.

Data arrives from two directions. The pages (`WorkflowsPage.tsx`, `CanvasPage.tsx`, `ErrorPage.tsx`, `NotFoundPage.tsx`, chapters 13 and 15) hand `AppHeader`, `Breadcrumb`, `PageHeading`, `ErrorBanner`, and `ScrollColumn` their props. The run-view components (`RunDetails.tsx`, `StepList.tsx`, `ExecutionHistory.tsx`, `Sidebar.tsx`, chapter 16) hand `Badge`, `StatusBadge`, `JsonDetails`, and the `format.ts` functions the `Run`, `StepRun`, `CheckOutput`, and `Workflow` objects that TanStack Query fetched. The types those objects carry are declared in `web/src/api.ts:3-63` and mirror the Go structs in `internal/engine/runner.go:24-46` and `internal/workflow/workflow.go:20-59`, so every string that `format.ts` builds is a sentence about a field the Go server wrote.

On the path from a browser click to a saved run, these files sit at both ends and never in the middle. Before the click, `AppHeader` and `PageHeading` frame the Run button that `WorkflowsPage.tsx:176-185` draws. After `Runner.Start` has saved the run and the one-second poll has brought it back, `format.ts` turns `status`, `startedAt`, `finishedAt`, and each step's `output` into "Completed", "42 ms total", "Healthy", and "Time to headers", and `Badge`, `StatusBadge`, `ErrorBanner`, and `JsonDetails` draw those words. The Playwright tests in `web/tests/` find elements by role and by these exact strings, so a change to a label here is a change to the test contract, not a cosmetic edit.

## Concepts you need

- **Function components, props, and default values.** A component is a function that takes one `props` object and returns elements. Writing `function AppHeader({ page, loading = false }: AppHeaderProps)` destructures the object in the parameter list and gives `loading` a default when the caller leaves it out. A prop marked `?` in the type is optional for callers; the default decides what the component sees. [React: Passing props to a component](https://react.dev/learn/passing-props-to-a-component)
- **`children` and `ReactNode`.** Whatever a caller writes between `<Badge>` and `</Badge>` arrives as the `children` prop. `ReactNode` is the TypeScript type for anything React can draw: a string, a number, an element, a fragment, an array, `null`, or `undefined`. `PageHeading` types `description` and `action` as `ReactNode` so a caller can pass a sentence, a fragment with a link, or nothing. [React: Passing JSX as children](https://react.dev/learn/passing-props-to-a-component#passing-jsx-as-children)
- **Default and named exports.** `export default function Badge` is the module's one default export; `export function badgeTone` is a named export. A file imports the default under any name (`import Badge from './Badge'`) and named exports by their exact names in braces. `StatusBadge.tsx:2` does both on one line. [MDN: export](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Statements/export)
- **`useRef` and DOM refs.** `useRef(null)` returns an object with one `current` field that survives re-renders. Passing it as `ref={column}` on an element makes React store that DOM node in `current` after the element is on the page. Changing `current` never triggers a re-render, which is the point: it is a handle, not state. [React: Manipulating the DOM with refs](https://react.dev/learn/manipulating-the-dom-with-refs)
- **`useEffect` with an empty dependency array.** An effect runs after React has put the component's elements into the DOM. With `[]` as its second argument it runs once, after the first render, and never again for that instance. Under `StrictMode` (`main.tsx:10`) development builds run it twice to flush out effects that are unsafe to repeat; focusing an element twice is harmless. [React: useEffect](https://react.dev/reference/react/useEffect)
- **Conditional rendering.** `{cond && <X />}` draws `X` only when `cond` is truthy; `{a ? <X /> : <Y />}` picks a branch; `undefined`, `null`, `false`, and `''` draw nothing. `PageHeading.tsx:18` and `AppHeader.tsx:39-46` use both forms. [React: Conditional rendering](https://react.dev/learn/conditional-rendering)
- **TanStack Router `Link` and `aria-current`.** `Link` renders an `<a>` whose `href` the router computes from `to`, intercepts the click so the page does not reload, and sets `aria-current="page"` on the link whose route is active. Because `router.tsx:59-63` registered the router's type, `to` accepts only `/`, `/workflows`, or `/canvas`. [TanStack Router: Link](https://tanstack.com/router/latest/docs/framework/react/api/router/linkComponent), [MDN: aria-current](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Attributes/aria-current)
- **Tailwind v4: utilities, `@theme` tokens, arbitrary values, and dynamic spacing.** Each class does one thing. `bg-accent` exists because `index.css:42` declares `--color-accent` inside `@theme`; `text-2xs` exists because of `--text-2xs` at `index.css:9`. Square brackets give a one-off value: `rounded-[10px]`, `tracking-[1.8px]`. Spacing classes accept any multiple of 0.25rem, so `px-1.75` is 7px and `mb-5.5` is 22px without any configuration. [Tailwind: Theme variables](https://tailwindcss.com/docs/theme), [Tailwind: Using arbitrary values](https://tailwindcss.com/docs/adding-custom-styles#using-arbitrary-values)
- **Tailwind variants.** A prefix applies a class under a condition: `hover:bg-surface-hover`, `md:px-[34px]` from the medium breakpoint up, `focus-visible:outline-3` only for keyboard focus, and `aria-[current=page]:bg-surface-selected` when the element carries that ARIA attribute. Variants let the DOM state drive the look without a JavaScript `if`. [Tailwind: Hover, focus, and other states](https://tailwindcss.com/docs/hover-focus-and-other-states)
- **Tailwind class detection.** Tailwind generates CSS only for class names it finds as complete strings in the source files. A built name like `` `bg-${tone}` `` produces no CSS. This rule is why `Badge.tsx:6-19`, `ErrorBanner.tsx:15`, and every entry in `classes.ts` are full literals. The Prettier plugin named in `web/.prettierrc.json` sorts the classes inside `className` attributes and reads `src/index.css` to recognise the project's tokens. [Tailwind: Detecting classes in source files](https://tailwindcss.com/docs/detecting-classes-in-source-files)
- **ARIA roles and attributes.** `role="alert"` marks a live region that assistive technology announces at once. `aria-hidden="true"` hides decoration from screen readers. `aria-label` names a landmark such as a `<nav>`. The Playwright tests locate elements by these roles, never by CSS classes. [MDN: ARIA alert role](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Roles/alert_role), [MDN: aria-hidden](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Attributes/aria-hidden)
- **Focus management: `tabIndex`, `focus()`, and `preventScroll`.** A `<main>` cannot receive focus by default. `tabIndex={-1}` makes it focusable from code while keeping it out of the Tab key order. `element.focus({ preventScroll: true })` moves keyboard focus without scrolling the element into view. Keyboard scrolling (arrows, PageDown, Space) goes to the focused scrollable element, so focusing the scroll container is what makes those keys work. [MDN: HTMLElement.focus()](https://developer.mozilla.org/en-US/docs/Web/API/HTMLElement/focus), [MDN: tabindex](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Global_attributes/tabindex)
- **`<details>` and `<summary>`, and `JSON.stringify(value, null, 2)`.** `<details>` is a native disclosure: the browser toggles it open and closed on a click of its `<summary>`, with no JavaScript. `JSON.stringify` with `null` as the replacer and `2` as the indent produces pretty-printed JSON and omits fields whose value is `undefined`. [MDN: details](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/details), [MDN: JSON.stringify](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/JSON/stringify)
- **TypeScript: `Record`, indexed access types, and literal unions.** `Record<K, V>` is an object type with exactly the keys in `K`, each of type `V`; if `K` is a union of string literals the compiler refuses an object that misses one. `Run['status']` is an indexed access type: it reads the type of one field, here the union `'queued' | 'running' | ...` from `api.ts:56`. [TypeScript: Record](https://www.typescriptlang.org/docs/handbook/utility-types.html#recordkeys-type), [TypeScript: Indexed access types](https://www.typescriptlang.org/docs/handbook/2/indexed-access-types.html)
- **The runtime `in` operator and `as` assertions.** `key in object` is a JavaScript check that returns `true` when the object or its prototype chain has that property. `value as T` tells the compiler to treat `value` as `T` with no runtime check. `Badge.tsx:22` pairs them: the `in` check proves membership, then `as` records that proof for the type checker. [MDN: in operator](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/in), [TypeScript: Type assertions](https://www.typescriptlang.org/docs/handbook/2/everyday-types.html#type-assertions)
- **Discriminated unions, `unknown`, and `instanceof` narrowing.** `WorkflowStep` and `CheckOutput` (`api.ts:3-12`, `:27-40`) are unions whose `type` field says which shape the value has; testing `step.type === 'tcp.check'` tells the compiler which `config` fields exist. `unknown` is the safe "anything" type: you must narrow it before use, and `error instanceof Error` is one way. [TypeScript: Discriminated unions](https://www.typescriptlang.org/docs/handbook/2/narrowing.html#discriminated-unions), [TypeScript: unknown](https://www.typescriptlang.org/docs/handbook/2/functions.html#unknown)
- **`Date`, `getTime`, and `toLocaleString`.** `new Date(isoString)` parses an RFC 3339 timestamp from the API; `getTime()` gives milliseconds since the epoch, so subtracting two gives a duration. `toLocaleString(undefined, options)` formats in the browser's own locale and zone; `{ timeZoneName: 'short' }` appends the zone's abbreviation. An unparseable string gives `Invalid Date` and `NaN` from `getTime()`, which the tests at `workflows.spec.ts:204-205` check never reaches the screen. [MDN: Date.prototype.toLocaleString](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date/toLocaleString)
- **`??`, `?.`, template literals, `some`, and `filter`.** `a ?? b` is `b` only when `a` is `null` or `undefined`; `a?.b` stops at `undefined` instead of throwing. Backtick strings embed expressions with `${}`. `array.some(fn)` is true if any element passes; `array.filter(fn).length` counts the ones that do. [MDN: Nullish coalescing](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/Nullish_coalescing), [MDN: Array.prototype.some](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/some)
- **`import type` under `verbatimModuleSyntax`.** `web/tsconfig.json:11` requires type-only imports to say `import type`, so the bundler can delete them without analysing the module. Five of these files carry such a line; four open with it, and `ScrollColumn.tsx:2` puts it after a value import. [TypeScript: verbatimModuleSyntax](https://www.typescriptlang.org/tsconfig/#verbatimModuleSyntax)
- **Playwright locator strictness.** A Playwright locator that resolves to more than one element throws when a single-element action such as `textContent()` or `toContainText(string)` is called on it. Two comments in this chapter (`JsonDetails.tsx:1`, `ErrorBanner.tsx:9`) exist because of that rule. [Playwright: Strictness](https://playwright.dev/docs/locators#strictness)

## Walkthrough

### web/src/components/AppHeader.tsx

**L1–L1** — `import { Link } from '@tanstack/react-router'`. `Link` draws the two page links at L28-33. The router renders an `<a>` with a hash `href`, handles the click without a reload, and sets `aria-current="page"` on the active one. Because `router.tsx:59-63` registered the router type, a misspelled `to` fails `npm --prefix web run typecheck`.

**L2–L2** — `focusRingClass` from `classes.ts:7-8`. The base stylesheet gives buttons and `<summary>` a focus outline (`index.css:126-130`) but says nothing about links, so the header appends this class to each link at L10. Without it, Tab would land on "Workflows" with no visible sign.

**L3–L3** — Blank.

**L4–L8** — `AppHeaderProps`. `page` is the literal union `'workflows' | 'canvas'` and chooses the right-hand status text at L39. `loading` and `disconnected` are optional and only `WorkflowsPage.tsx:102` passes them; `CanvasPage.tsx:28`, `ErrorPage.tsx:13`, and `NotFoundPage.tsx:9` pass `page` alone. Making them optional is what lets a page with no polling render the header at all.

**L9–L9** — Blank.

**L10–L10** — `navLinkClass`, a module-scope constant built once when the file loads. The important parts: `no-underline` cancels the base `a { text-decoration: underline }` rule at `index.css:86-89` (utility classes win over the base layer); `aria-[current=page]:bg-surface-selected` and `aria-[current=page]:text-ink-selected` style the active link from the attribute that `Link` sets, so the component needs no `active` prop and cannot disagree with the router; `${focusRingClass}` adds the keyboard ring. The string is one complete literal so Tailwind's scanner finds every class (see the class-detection concept).

**L11–L11** — Blank.

**L12–L12** — The component signature with defaults `loading = false` and `disconnected = false`. The defaults mean a caller that omits both gets the green "Local workspace" state at L43-44, which is what the error and not-found pages show even though they fetch nothing.

**L13–L13** — `return (` opens the JSX.

**L14–L14** — The `<header>` element. `shrink-0` matters most: the page shell is `flex h-dvh flex-col` (`classes.ts:15`), and without `shrink-0` a tall column below would squeeze the header shorter than `min-h-16`. `flex-wrap` and `justify-between` let the three children (brand, nav, status) wrap onto two rows on a phone; `md:` variants restore one row and wider padding from the medium breakpoint.

**L15–L15** — The brand group: a flex row holding the logo mark and the word "Patchbay", with `tracking-[-0.6px]` tightening the letters.

**L16–L21** — The logo mark. A 34px rounded square (`size-[34px]`, `bg-accent-deep` from `index.css:44`) holding a "p" and an absolutely positioned "b" in `text-accent-glow` (`index.css:45`). `aria-hidden="true"` at L18 hides both letters from screen readers, because they are decoration; the accessible name of the brand is the plain text at L22. Without `relative` on the outer span the `absolute` "b" would position itself against the nearest positioned ancestor, or the page itself, since the header sets no position.

**L22–L22** — The text "Patchbay", the only part of the brand that assistive technology reads.

**L23–L23** — Closes the brand group.

**L24–L27** — The `<nav>` with `aria-label="Pages"`. The label gives the landmark a name, which matters because `Sidebar.tsx:27-30` has a second `<nav>` named "Workflows" on the same page. `order-3 w-full` sends the nav to its own full-width row on phones (flex `order` beats source order); `md:order-none md:w-auto md:mr-auto` puts it back inline and pushes everything after it to the right from the medium breakpoint.

**L28–L33** — The two links. `to="/workflows"` and `to="/canvas"` are the routes at `router.tsx:34-39` and `:41-46`; each of those routes runs `validateView` (`router.tsx:28-32`), so the page that opens always has a defined `view` even though the link passes no search params. Both share `navLinkClass`.

**L34–L34** — Closes the nav.

**L35–L35** — The status group. `ml-auto` pushes it to the right edge on phones, where it shares the first row with the brand; `md:ml-0` hands that job to the nav's `md:mr-auto`.

**L36–L38** — The "M0" chip, `hidden md:inline-block`, so phones do not spend the space. "M0" is the milestone name from `README.md:1` and the roadmap's first milestone (`docs/roadmap.md:174`).

**L39–L39** — `page === 'canvas' ?` chooses between the two status displays.

**L40–L40** — On the New workflow page the text is "Local draft". That page never polls the server (`CanvasPage.tsx` imports no query hook), and drafts exist only in the browser (`AGENTS.md:70`, `README.md:198-199`), so a connection dot would claim something the page does not know.

**L41–L45** — On the workflows page the status is a dot and a word. The `<i>` at L43 is a 6px circle (`size-1.5 rounded-full`) coloured `bg-offline` (`index.css:49`) when `disconnected`, else `bg-online` (`index.css:48`). L44 is a nested ternary: "Disconnected" wins over "Connecting", which wins over "Local workspace". The order matters when the first poll fails before the other has answered: `WorkflowsPage.tsx:72-74` can then have both `loading` and `disconnected` true, and the user should see red.

**L46–L47** — Close the ternary and the status group.

**L48–L50** — Close the header, the `return`, and the function.

### web/src/components/Breadcrumb.tsx

**L1–L1** — The whole prop type is inline: `{ page: string }`. The two callers pass fixed strings, "Workflows" at `WorkflowsPage.tsx:152` and "New workflow" at `CanvasPage.tsx:41`. `WorkflowsPage` draws it only inside the Details view (`WorkflowsPage.tsx:146-152`); the Canvas view has its own title bar (`WorkflowCanvas.tsx:68-78`).

**L2–L6** — The markup: "Workspace", a slash in `text-text-separator` (`index.css:40`), then `page`. It is a plain `<div>` rather than a `<nav>` because neither crumb is a link; it is a label of where the reader is, not navigation. `mb-5.5` and `md:mb-7.5` (22px and 30px) match the bottom margins of `PageHeading.tsx:12`, so the two stack with consistent rhythm.

**L7–L7** — Closes the function.

### web/src/components/PageHeading.tsx

**L1–L2** — `import type { ReactNode }`, type-only because of `verbatimModuleSyntax`, then a blank line.

**L3–L8** — `PageHeadingProps`. `eyebrow` and `title` are plain strings. `description` and `action` are optional `ReactNode` so callers can pass richer content: `WorkflowsPage.tsx:162-173` passes a fragment with the description and a monospace schedule line; `ErrorPage.tsx:18-26` passes a sentence with a "Try again" button and a `Link`; `NotFoundPage.tsx:14-18` passes a sentence with a `Link`; `WorkflowsPage.tsx:174-187` passes the Run button as `action`. Typing these as `string` would force every caller to flatten its content.

**L9–L10** — Blank line, then the signature. Four callers: `WorkflowsPage.tsx:153`, `CanvasPage.tsx:42`, `ErrorPage.tsx:15`, `NotFoundPage.tsx:11`.

**L11–L12** — `return (` and the wrapper. `flex-col items-start` stacks the text block above the action on narrow screens; `lg:flex-row lg:items-center lg:justify-between` puts the action to the right of the text from the large breakpoint. `mb-5.5 md:mb-7.5` is the same rhythm as `Breadcrumb.tsx:3`.

**L13–L13** — Opens the text block that holds the eyebrow, title, and description together, so the action at L22 is the wrapper's second flex child.

**L14–L16** — The eyebrow line. The component does not uppercase it; the callers pass uppercase strings ("SCHEDULED WORKFLOW", "MANUAL WORKFLOW", "LOCAL DRAFT" at `WorkflowsPage.tsx:155-159`, "VISUAL WORKFLOWS" at `CanvasPage.tsx:43`, "ERROR" at `ErrorPage.tsx:16`, "NOT FOUND" at `NotFoundPage.tsx:12`). `tracking-[1.8px]` is the letter spacing that makes small capitals readable.

**L17–L17** — The `<h1>`. The base rule at `index.css:96-100` sets a 1.875rem size; `text-2xl lg:text-3xl` overrides it per breakpoint because utilities beat the base layer. This heading is the handle the browser tests use to know the page is ready: `workflows.spec.ts:10` waits for `getByRole('heading', { name: 'Browser checks', exact: true })`.

**L18–L20** — `{description && <p>...</p>}` draws the paragraph only when there is content. `WorkflowsPage.tsx:163` passes `(selected.description || scheduleLine) && (...)`, which is `''` when a workflow has neither, and an empty string draws nothing. `max-w-[540px]` keeps the sentence at a readable measure.

**L21–L21** — Closes the text block.

**L22–L22** — `{action}`. When the caller passes `undefined` or `false` (a local draft has no Run button: `WorkflowsPage.tsx:175` passes `savedWorkflow && (...)`), React draws nothing and the wrapper has one child.

**L23–L25** — Close the wrapper, the `return`, and the function.

### web/src/components/Badge.tsx

**L1–L3** — `ReactNode` for `children`, then `Run` and `StepRun` from `api.ts:52-63` and `:42-50`. The two run types are imported only to read their `status` fields at L4. A blank line follows.

**L4–L4** — `BadgeTone`. The union adds three names of its own, `neutral`, `healthy`, and `unhealthy`, to `Run['status']` and `StepRun['status']`, the indexed access types that read the status unions at `api.ts:56` and `:45`. Flattened, that is eleven literals. Deriving them from `api.ts` instead of retyping them means that adding a status to the API type makes L7 fail to compile until a colour is chosen, which is the "update ... relevant UI/API types together" rule at `AGENTS.md:131-132` enforced by the compiler.

**L5–L6** — Blank line, then the comment "Every class string is a complete literal so Tailwind can find it." The alternative, `` `bg-${tone}-bg` ``, would type-check and produce an unstyled badge, because Tailwind never sees the assembled name.

**L7–L7** — `const toneClass: Record<BadgeTone, string>`. The `Record` type is the exhaustiveness check: leave one tone out and the file does not compile (Experiment 2).

**L8–L10** — `neutral`, `pending`, and `skipped` share the grey pair `bg-badge-bg text-badge` (`index.css:61-62`). A step that is waiting or was never reached is not news.

**L11–L11** — `healthy` is green (`index.css:59-60`). It is a statement about the monitored service, not about the run.

**L12–L13** — `unhealthy` and `queued` share amber (`index.css:53-54`). An unhealthy observation is not a failure of execution; `README.md:236-239` separates "Completed" (execution) from "Healthy" (the service), and the amber colour, not red, keeps that distinction visible.

**L14–L14** — `running` is blue (`index.css:55-56`).

**L15–L15** — `succeeded` is green (`index.css:51-52`).

**L16–L18** — `failed`, `canceled`, and `interrupted` share red (`index.css:57-58`). All three mean the run did not complete its steps: an executor error (`runner.go:251`) or a failed progress save (`:234`, `:275`), shutdown (`runner.go:227`, `:253`), or a restart that found the run unfinished (`run_interruptions.go:32`).

**L19–L19** — Closes the map.

**L20–L23** — `badgeTone(status: string): BadgeTone`. It takes any string, because `StatusBadge` accepts any string, and returns `'neutral'` for one it does not know, so a status from a newer server still gets a badge. `status in toneClass` is the runtime check and `status as BadgeTone` is the assertion that records it for the compiler. One nuance of `in`: it also sees inherited names such as `toString`. No real status collides, because statuses come from the fixed strings in `runner.go:128`, `:134`, `:224-257`, `:305` and `run_interruptions.go:25`, `:32`. Its only caller is `StatusBadge.tsx:5`.

**L24–L25** — Blank line, then the `Badge` signature. `tone` is typed `BadgeTone`, not `string`, so a direct caller is checked at compile time: `RunDetails.tsx:52` passes `step.output.healthy ? 'healthy' : 'unhealthy'` and `WorkflowCanvas.tsx:73` passes `"neutral"`.

**L26–L32** — The `<span>`. `inline-block` lets it have padding inside a line of text; `whitespace-nowrap` keeps two-word badges such as "Not established" on one line; `text-2xs` is the 10px size from `index.css:9`; `${toneClass[tone]}` appends the colour pair. The tests read the badge by its text, for example `getByText('Completed', { exact: true })` at `workflows.spec.ts:35`.

**L33–L33** — Closes the function.

### web/src/components/StatusBadge.tsx

**L1–L1** — `statusLabel` from `format.ts:15-17` turns a status into its display word.

**L2–L2** — The default export `Badge` and the named export `badgeTone` from the same file, in one import.

**L3–L3** — Blank.

**L4–L6** — The component. It takes `status: string`, not `Run['status']`, because the same badge draws run statuses (`ExecutionHistory.tsx:55`, `RunDetails.tsx:16`) and step statuses (`RunDetails.tsx:56`), which are different unions, and because an unknown value must still render (`format.ts:14`). It composes the two helpers: `badgeTone` picks the colour, `statusLabel` picks the word. The test at `workflows.spec.ts:163` expects "Interrupted" twice on one screen, once from the run badge and once from the interrupted step's badge, which is this component called from two places.

### web/src/components/ErrorBanner.tsx

**L1–L2** — `import type { ReactNode }` and a blank line.

**L3–L7** — `ErrorBannerProps`: `children` for the message and an optional `inset`. The comment at L5 explains `inset`: "Inside a panel the banner keeps the panel's horizontal inset." `RunDetails.tsx:22` and `:24` pass it because they sit inside the rounded panel; `WorkflowsPage.tsx:117` and `:119` leave it off because they sit in the open feedback area above the view panel.

**L8–L9** — Blank line, then the contract comment: "The only component that renders `role="alert"`; the browser tests count alerts." The counts are real: `workflows.spec.ts:74` expects zero alerts on the page after the connection recovers, `:263` expects exactly one or two inside "Run results" depending on `finalSaveFailed`, and `:273`, `:320` expect zero after switching to a run with no error. `:68` and `:311` call `toContainText` on a single-element locator. Any other component that rendered `role="alert"` inside "Run results", or anywhere on the page during the connection tests, would shift those counts or trip Playwright's strictness rule.

**L10–L10** — The signature, with `inset = false` as the default.

**L11–L17** — The `<div role="alert">`. The role makes the message an assertive live region that assistive technology announces when it appears, which fits an error that arrived without the user asking. The colours are the danger tokens at `index.css:63-65`. The template literal at L14-16 chooses between two complete literals: `mx-4 mb-4 md:mx-5.5` when inset, `mb-5` otherwise. `mx-4` is 16px and `md:mx-5.5` is 22px, which match the panel's own `px-4 md:px-[22px]` at `RunDetails.tsx:18`, so the banner lines up with the text above it. Both branches are whole strings so Tailwind finds every class.

**L18–L18** — `{children}`: the message. Callers pass a string (`WorkflowsPage.tsx:117`, `:119`, `RunDetails.tsx:22`) or two sentences of JSX text (`RunDetails.tsx:25-26`).

**L19–L21** — Close the div, the `return`, and the function.

### web/src/components/JsonDetails.tsx

**L1–L1** — The contract comment: "The browser tests read one `<pre>` per panel, so each panel renders exactly one JsonDetails." `workflows.spec.ts:31` does `results.locator('pre').textContent()` inside the "Run results" region and parses the result as JSON. `textContent()` on a locator that matches two elements throws, so a second `<pre>` in the panel would break the test before any assertion ran. `StepList.tsx:50` and `RunDetails.tsx:39` each render one, in two different regions.

**L2–L2** — The signature: `summary` is the clickable label, `value` is `unknown`. `unknown` lets the component accept a `Workflow` (`StepList.tsx:50`) or a `Run` (`RunDetails.tsx:39`) without knowing either shape; it only ever hands the value to `JSON.stringify`.

**L3–L4** — `return (` and the `<details>` element. The browser owns its open state; React never sets the `open` attribute, so a re-render from the next poll neither opens nor closes it. The grey `bg-surface-muted` and top border mark the panel's footer.

**L5–L5** — The `<summary>`, the only clickable part. `cursor-pointer` because `index.css:115-117` gives that cursor to buttons only. The base rules at `index.css:122-130` give `<summary>` a focus outline and remove the mobile tap highlight.

**L6–L8** — The `<pre>` holding `JSON.stringify(value, null, 2)`. Two spaces of indent make it readable; `undefined` fields are omitted, so a test fixture with `finalSaveFailed: undefined` (`workflows.spec.ts:251`) prints the same JSON the server would send. `max-h-82.5 overflow-auto` (330px) caps the height so a long run does not stretch the page; `whitespace-pre-wrap wrap-anywhere` lets long URLs wrap, which keeps the mobile layout from scrolling sideways (`workflows.spec.ts:61-63` asserts the document is no wider than the viewport). Nothing else goes inside the `<pre>`, because the test parses its text as JSON.

**L9–L11** — Close `<details>`, the `return`, and the function.

### web/src/components/ScrollColumn.tsx

**L1–L3** — `useEffect` and `useRef` on one line, `ReactNode` as a type-only import on the next, then a blank line. The split is required by `verbatimModuleSyntax`.

**L4–L6** — The comment states both reasons the component exists. First, the page shell is viewport-height (`classes.ts:14-15`), so this column, not the document, is the thing that scrolls. Second, keyboard scrolling goes to the focused scrollable element; without the focus at L11, "the arrow and page keys scroll nothing until a click lands inside it."

**L7–L7** — The signature. Two callers wrap their whole page body in it: `WorkflowsPage.tsx:112-208` and `CanvasPage.tsx:29-86`. `ErrorPage.tsx:14` and `NotFoundPage.tsx:10` use a plain `<main>` with `wideColumnClass` and no shell, so on those pages the document scrolls normally and nothing depends on a page that may have thrown (`ErrorPage.tsx:8-9`).

**L8–L8** — `const column = useRef<HTMLElement>(null)`. The ref starts `null`; React fills `column.current` with the `<main>` node after the first commit. The type parameter is `HTMLElement` because that is what a `<main>` is.

**L9–L12** — A blank line, then the effect. `column.current?.focus({ preventScroll: true })` runs once after mount (`[]`). `?.` guards the `null` that would exist if the ref were never attached. `preventScroll: true` stops the browser from scrolling the focused element into view, which would otherwise jump the page on load. The focus has to live in an effect because the DOM node does not exist while the function body runs. In development `StrictMode` runs it twice; focusing twice changes nothing.

**L13–L14** — Blank line and `return (`.

**L15–L19** — The `<main>`. `ref={column}` attaches the ref. `tabIndex={-1}` makes the element focusable from code without adding a Tab stop. `flex-1` fills the shell below the header; `min-h-0` lets a flex child shrink below its content height, which is the condition for `overflow-y-auto` to produce a scrollbar instead of growing; `min-w-0` does the same for width so a long JSON line cannot widen the page; `outline-none` hides the focus ring, since this focus is plumbing, not a target the user chose. The test comment at `workflows.spec.ts:50` relies on this: "Only the main column scrolls, so a tall viewport captures the whole Details view."

**L20–L20** — `{children}`: the tabs, feedback, and view panel from the calling page.

**L21–L23** — Close `<main>`, the `return`, and the function.

### web/src/components/classes.ts

**L1–L1** — The comment: "Class strings shared by several components. Tailwind reads them from this file." Sharing a string instead of copying it keeps two panels identical, and keeping each string a complete literal keeps Tailwind's scanner working.

**L2–L2** — `panelClass`: a white, bordered, 10px-rounded box with `overflow-hidden`. The overflow rule clips children to the rounded corners, which matters because `JsonDetails` paints a grey footer at the bottom of each panel (`JsonDetails.tsx:4`); without clipping its corners would poke out square. Used by `StepList.tsx:8`, `RunDetails.tsx:13`, `ExecutionHistory.tsx:27` and `:44`.

**L3–L5** — Blank line, then `panelHeadingClass`: a flex row with the title at the left and a badge or note at the right, a bottom border, and padding that grows at `md:`. Used by `StepList.tsx:9` and `RunDetails.tsx:14`.

**L6–L8** — Blank line, then `focusRingClass`: a 3px outline in `--color-focus` (`index.css:47`) with a 3px offset, only under `focus-visible:`, so a mouse click shows nothing and keyboard focus shows a ring. Links (`AppHeader.tsx:10`) and inputs (L12) need it because the base layer at `index.css:126-130` covers only buttons and summaries.

**L9–L10** — Blank line, then `fieldLabelClass`: a `<label>` laid out as a one-column `grid` with a 7px `gap-1.75`, so the label text sits above the input that the label wraps. Used by `WorkflowCanvas.tsx:80` and `CanvasPage.tsx:55`.

**L11–L12** — Blank line, then `inputClass`, a template literal that appends `focusRingClass`. `min-w-0` lets the input shrink inside a flex row instead of forcing the row wider. Used by the `<select>` at `WorkflowCanvas.tsx:83` and the `<input>` at `CanvasPage.tsx:59`.

**L13–L15** — Blank line, the comment "A page is a fixed-height shell. The header stays in place and ScrollColumn scrolls below it.", then `pageShellClass` = `flex h-dvh flex-col`. `h-dvh` is `100dvh`, the dynamic viewport height that follows a phone browser's collapsing toolbar. Used by `WorkflowsPage.tsx:101` and `CanvasPage.tsx:27`. Without a fixed height the document would grow with its content, `ScrollColumn`'s `overflow-y-auto` would never engage, and the header would scroll away.

**L16–L19** — Blank line, the comment "The New workflow, not-found, and error pages use a wider column than the runner.", then `wideColumnClass`. Its cap is `max-w-[1600px]` against the runner's `max-w-[1320px]` at `WorkflowsPage.tsx:25-26`, and its paddings (`px-4.5` 18px, `lg:px-10.5` 42px) mirror the runner's `px-[18px]` and `lg:px-[42px]`, so the two columns differ in width and nothing else. Used by `CanvasPage.tsx:40`, `ErrorPage.tsx:14`, and `NotFoundPage.tsx:10`.

### web/src/format.ts

**L1–L2** — Type-only imports of `CheckOutput` (`api.ts:27-40`), `Run` (`api.ts:52-63`), `Workflow` (`api.ts:16-25`), and `WorkflowStep` (`api.ts:3-12`), then a blank line. This module imports nothing from `components/`; the dependency runs one way, components import `format`, which is what lets these functions stay pure.

**L3–L12** — `statusLabels`, a `Record<string, string>` from API status to display word. Two mappings are deliberate choices, not translations: `succeeded` reads "Completed" because `README.md:236` says "Completed describes execution, while Healthy describes the service", and `pending` reads "Waiting". The key type is `string` rather than `BadgeTone` so `format.ts` does not depend on `Badge.tsx`.

**L13–L17** — Blank line, the comment "An unknown status renders as its raw value, so a newer server never hides a state.", then `statusLabel`. `statusLabels[status] ?? status` returns the word when the key exists and the raw status otherwise. `web/tsconfig.json` does not enable `noUncheckedIndexedAccess`, so the compiler types the lookup as `string`; the `??` is there for the runtime, where a missing key gives `undefined`. Caller: `StatusBadge.tsx:5`. The tests read its output: "Completed" (`workflows.spec.ts:35`), "Interrupted" (`:163`), "Skipped" (`:167`), "Queued" (`:207`), "Failed" (`:259`).

**L18–L21** — Blank line, then `errorMessage(error: unknown)`. Thrown values can be anything, so the parameter is `unknown` and `instanceof Error` narrows it. `request` always throws an `Error` whose message is the server's `error` sentence, a `Request failed (N)` line, or a sentence naming the Go server (`api.ts:71`, `:73`, `:76`), so the fallback "Something went wrong. Please try again." appears only for a non-`Error` throw such as a string. Callers: `WorkflowsPage.tsx:74` and `:76` for the two query errors and the mutation error, `ErrorPage.tsx:20` for a render-time throw.

**L22–L25** — Blank line, then `triggerLabel`: "Scheduled" when `workflow.schedule` is present, else "Manual trigger". `schedule` is optional at `api.ts:21` because the Go field is `omitempty` (`workflow.go:25`). Caller: `Sidebar.tsx:47`. `README.md:386` states the behaviour: "The sidebar labels a scheduled workflow Scheduled".

**L26–L28** — Blank line, then the comment: the next run shows in the browser's zone, and that zone is named so it "cannot be confused with the schedule's own zone." A schedule in `Europe/Amsterdam` read by a browser in New York would otherwise show a time with no hint of which clock it belongs to.

**L29–L30** — `scheduleSummary(workflow)` returns `''` at once when there is no schedule, so the caller at `WorkflowsPage.tsx:83` and the `scheduleLine &&` test at `:166` can treat the empty string as "nothing to show".

**L31–L31** — The first part: `Cron ${cron} (${timezone})`, the two fields of `WorkflowSchedule` (`api.ts:14`, Go `schedule.go:14-15`). This part is in the schedule's own zone.

**L32–L32** — Returns that summary alone when `nextRunAt` is missing. `api.ts:23-24` says the field is "Present only while the server has a next occurrence", and `api.go:27` omits it when empty, for example while the scheduler is stopped.

**L33–L35** — `new Date(workflow.nextRunAt).toLocaleString(undefined, { timeZoneName: 'short' })` formats the timestamp in the browser's locale and zone and appends the zone abbreviation; `undefined` means "use the browser default locale". L34 joins the two halves with a middle dot. `README.md:386-389` describes exactly this: the heading shows "its cron expression, time zone, and next run in the browser's time zone". The page draws the result in a monospace line at `WorkflowsPage.tsx:166-170`.

**L36–L40** — Blank line, then `tcpEndpoint(host, port)`. The comment at L38 explains: configuration stores IPv6 without brackets, so brackets are added for display. The Go side enforces the unbracketed form (`workflow.go:103-104`, `:125-126`, `README.md:316-320`) and the executor adds brackets the same way with `net.JoinHostPort` (`tcp.go:32-34`). `host.includes(':')` is the test, because a DNS name or IPv4 address never contains a colon. Callers: L52 and L76. The test at `tcp.spec.ts:110` expects `[::1]:8080` and `:133` expects `closed.invalid:22`.

**L41–L46** — Blank line, then the `StepDefinition` type: the three strings `StepList` prints for a configured step, a type label, an endpoint, and what the step expects.

**L47–L49** — Blank line, then `stepDefinition(step)` and its first branch test, `step.type === 'tcp.check'`. `WorkflowStep` is a discriminated union (`api.ts:3-12`); inside the branch the compiler knows `step.config` has `host` and `port`, and after it, `url` and `expectedStatus`.

**L50–L55** — The TCP shape: "TCP CHECK", the bracketed `host:port`, and "TCP connection" as the expectation. `tcp.spec.ts:109-113` checks each and also checks that no "GET" or "HTTP" text leaks into a TCP step.

**L56–L61** — The HTTP shape: "HTTP CHECK", `GET ${url}` because the executor sends a GET (`http.go:42`), and `HTTP ${expectedStatus}`. `tcp.spec.ts:114-116` reads the first endpoint. Caller: `StepList.tsx:15`, which draws the three fields at `:25`, `:29`, and `:35`.

**L62–L69** — Blank line, then the `OutputPresentation` type: the five strings `RunDetails` prints for a saved result. `endpoint` is optional; only the TCP branch sets it, and `RunDetails.tsx:70` draws the endpoint line only when it exists.

**L70–L73** — Blank line, the comment "A result saved before TCP support has no type field and is an HTTP result.", then `outputPresentation(output)` and its branch test on `output.type === 'tcp.check'`. The rule is the one at `AGENTS.md:125-127`, "a missing result type means legacy HTTP", and `api.ts:33-34` encodes it by making `type` optional on the HTTP member. Because the function reads the saved output's own type, a result stays readable even if the loaded workflow has since changed.

**L74–L81** — The TCP presentation: badge "Connected" or "Unreachable", the bracketed endpoint, metric label "Connection" with value "Accepted" or "Not established", and time label "Time to connect". The TCP executor measures the dial alone (`tcp.go:35-37`), so "Time to connect" is the honest name. A TCP reason never names its target ("TCP connection established", `tcp.go:53`), which is why this branch supplies the endpoint for `RunDetails.tsx:70-73` to print. `tcp.spec.ts:125-133` checks the badges, the endpoints, the connection values, and the time label; only the "Connection" label goes unasserted.

**L82–L88** — The HTTP presentation: badge "Healthy" or "Unhealthy" (the service words from `README.md:236`), no endpoint, metric label "Response" with `HTTP ${statusCode}` or "No response", and time label "Time to headers". `statusCode` is optional (`api.ts:37`) and the Go field is `omitempty` (`workflow.go:54`), so a timeout leaves it absent and the `?` test falls to "No response". The HTTP executor stops its clock when `client.Do` returns, which is when the headers have arrived (`http.go:47-49`, `README.md:241`). Callers: `RunDetails.tsx:53` reads `.badge`, `RunDetails.tsx:66` reads the rest. `tcp.spec.ts:143-146` checks that a legacy result shows "HTTP 204", "Time to headers", and no "Time to connect".

**L89–L92** — Blank line, then `hasTcpStep(run)`: true when any step's saved output has `type === 'tcp.check'`. `step.output?.type` returns `undefined` for a step that never produced output. Reading outputs rather than the currently loaded workflow's step types upholds "Keep historical results independent of the currently loaded workflow" (`AGENTS.md:126-127`). Callers: L107, L124, and `RunDetails.tsx:33`, which adds the "does not verify application health" note.

**L93–L95** — Blank line, then `runTiming(run)` and its first rule: a `queued` run reads "Waiting for an execution slot" (`README.md:168`, "The frontend shows Queued while waiting"). This check comes before the `startedAt` check because a queued run has no start time either, and the waiting message is the useful one.

**L96–L96** — No `startedAt` means "Never started". The Go field is `omitzero` (`runner.go:41`), so a run that was canceled or interrupted while still waiting arrives without it (`README.md:177`, `:575`). The test at `workflows.spec.ts:199-203` expects this text for canceled and interrupted runs with no start time.

**L97–L100** — With a `finishedAt`, the elapsed time is `finishedAt - startedAt` in milliseconds, printed as "N ms total". `Math.max(0, elapsed)` keeps a clock adjustment between the two timestamps from printing a negative number. The subtraction works because both strings parse to valid dates; `workflows.spec.ts:205` guards against "NaN ms" ever appearing.

**L101–L101** — An `interrupted` run with a start and no finish reads "Finish time unknown". `README.md:573-575` explains: startup cleanup marks the run interrupted but "Missing finish times stay unknown", and the frontend must not invent a duration. `workflows.spec.ts:164` expects this text.

**L102–L103** — Anything else, in practice a `running` run, reads "No completion recorded yet". Caller for the whole function: `RunDetails.tsx:19`.

**L104–L107** — Blank line, then `runSummary(run)` and its two inputs: `unhealthy`, the count of steps whose saved output says `healthy: false`, and `tcp`, whether any step was a TCP check. The count reads outputs, not statuses, because a step that correctly observed an unhealthy service still has status `succeeded` (`runner.go:257`); health lives in the output.

**L108–L112** — When something was unhealthy, the wording depends on `tcp`: "N check(s) did not pass" for a run with TCP steps, "N unhealthy service(s)" for an HTTP-only run, with singular and plural handled. `tcp.spec.ts:123` expects "1 check did not pass".

**L113–L115** — A `succeeded` run with nothing unhealthy reads "All checks passed" or "All services healthy", again by `tcp`; any other status with nothing unhealthy returns `''`, so a failed or running run shows no summary. `tcp.spec.ts:149-150` checks the TCP wording appears and the HTTP wording does not. Caller: `RunDetails.tsx:20`.

**L116–L120** — Blank line, then `runHelp(run)` and the `queued` sentence: "This run will start automatically when an execution slot is available." It explains the bounded queue from `README.md:164-168` in the user's terms. `workflows.spec.ts:208-210` expects it.

**L121–L123** — The `interrupted` sentence: Patchbay restarted before completion was recorded, saved results are preserved, steps were not resumed. It restates `AGENTS.md:98-99`, "Startup marks leftover queued/running records interrupted; it does not resume actions", and `README.md:569-575`. `workflows.spec.ts:168-170` expects the second half of it.

**L124–L127** — Every other status gets the "Completed means the checks finished" sentence, with "check" or "service" wording chosen by `hasTcpStep`. This is the UI's statement of the Completed-versus-Healthy rule (`README.md:236-239`). Caller: `RunDetails.tsx:32`.

## State and rendering

None of these files owns server state, and none calls TanStack Query or TanStack Router hooks except `AppHeader`'s `Link`. What they hold is at module scope and never changes: `navLinkClass` (`AppHeader.tsx:10`), `toneClass` (`Badge.tsx:7-19`), `statusLabels` (`format.ts:3-12`), and the seven strings in `classes.ts`. The one hook-held value is the ref in `ScrollColumn.tsx:8`, which is a handle to a DOM node, not state; writing to it causes no render. The `<details>` element in `JsonDetails.tsx:4` has state too, but the browser owns it: React never sets `open`, so the disclosure stays where the user left it across polls, and the JSON inside updates in place.

These components re-render whenever their parent does, because nothing is memoised and they are cheap pure functions of their props. The parents re-render when a poll brings different data into the query cache (`WorkflowsPage.tsx:42-51`, chapter 15), when the user selects a workflow or a run, or when the URL's `view` changes. A poll that returns identical data leaves the cache reference alone, so no re-render reaches these leaves. `ScrollColumn`'s effect runs only on mount, so a re-render never steals focus back from a button the user has tabbed to.

The data path to the screen is short. `GET /api/runs` becomes a `Run`; `RunDetails` passes it to `runTiming`, `runSummary`, and `runHelp` for three sentences (`RunDetails.tsx:19`, `:20`, `:32`), passes `run.status` to `StatusBadge` (`:16`), and for each step passes `step.output` to `outputPresentation` (`:53`, `:66`) or `step.status` to `StatusBadge` (`:56`); the same `Run` object goes to `JsonDetails` (`:39`), so the JSON is the exact response. `GET /api/workflows` becomes a `Workflow`; `scheduleSummary` turns it into the heading's monospace line (`WorkflowsPage.tsx:83`, `:166-170`), `triggerLabel` into the sidebar's "Scheduled" (`Sidebar.tsx:47`), and `stepDefinition` into each row of the step list (`StepList.tsx:15`).

Errors reach the user through `errorMessage` and `ErrorBanner`. A failed poll or a refused start becomes a string at `WorkflowsPage.tsx:74` and `:76`, drawn as a banner at `:117` and `:119`; only the poll failure also drives the red dot and "Disconnected" in the header (`WorkflowsPage.tsx:102`, `AppHeader.tsx:43-44`). A run-level `error` from the Go runner (`runner.go:238`, `:276`) is drawn by `RunDetails.tsx:22`. A final-save failure is not an error status: `finalSaveFailed` is "a memory-only warning, not a new execution status" (`AGENTS.md:92`), so `Badge` has no tone for it and `RunDetails.tsx:23-28` draws it as a second inset banner while the status badge still says "Completed". A throw during rendering unwinds to the router's boundary, which draws `ErrorPage` with `PageHeading` and `errorMessage` (`ErrorPage.tsx:15-27`) and deliberately avoids `ScrollColumn` and the page shell.

## Check yourself

1. The server starts sending a run with `status: "paused"`, a value no file in `web/src` knows. Trace what `StatusBadge` draws and say whether anything compiles differently if `api.ts:56` is also updated.

<details><summary>Answer</summary>At runtime <code>badgeTone('paused')</code> (<code>Badge.tsx:21-23</code>) finds no such key in <code>toneClass</code> and returns <code>'neutral'</code>, and <code>statusLabel('paused')</code> (<code>format.ts:15-17</code>) finds no label and returns the raw string, so the user sees a grey badge reading "paused". Nothing is hidden, which is the point of the comment at <code>format.ts:14</code>. If someone adds <code>'paused'</code> to the union at <code>api.ts:56</code>, <code>BadgeTone</code> (<code>Badge.tsx:4</code>) gains the literal and <code>Record&lt;BadgeTone, string&gt;</code> at <code>Badge.tsx:7</code> stops compiling until a class pair is added; <code>statusLabels</code> is keyed by <code>string</code> and does not complain, so the label would still be the raw word until someone adds it.</details>

2. A run has `status: "succeeded"` and one step whose output has `healthy: false`. What do the run badge, that step's badge, and the summary line say, and why do they not agree?

<details><summary>Answer</summary>The run badge says "Completed" in green (<code>format.ts:6</code>, <code>Badge.tsx:15</code>). The step badge says "Unhealthy" in amber, because <code>RunDetails.tsx:51-54</code> prefers the output over the status and <code>outputPresentation</code> picks the badge from <code>healthy</code> (<code>format.ts:83</code>). The summary says "1 unhealthy service" (<code>format.ts:106-111</code>). They disagree on purpose: <code>README.md:236-239</code> says "Completed describes execution, while Healthy describes the service"; the runner set the step to <code>succeeded</code> because the executor returned a result (<code>runner.go:257</code>), and the result records what it saw. <code>runSummary</code> counts outputs, not statuses, for the same reason.</details>

3. Swap the first two rules of `runTiming` so the `startedAt` check at L96 runs before the `queued` check at L95. Which named Playwright test fails, at which line, and why does only that case change?

<details><summary>Answer</summary>A queued run has no <code>startedAt</code> (the Go field is <code>omitzero</code>, <code>runner.go:41</code>), so the swapped function returns "Never started" for it. The test <code>a queued run without a start time has readable history</code> (<code>workflows.spec.ts:174-213</code>) fails at <code>:199-203</code>, which expects "Waiting for an execution slot" for the queued case. The canceled and interrupted cases of the same loop still pass, because for them "Never started" was already the expected text. Order matters only where two rules match the same input.</details>

4. Delete the `useEffect` in `ScrollColumn.tsx:10-12`. What stops working for a user, why does `make e2e` still pass, and what would go wrong if you kept the effect but removed `preventScroll: true`?

<details><summary>Answer</summary>After a load, pressing PageDown or the arrow keys scrolls nothing until the user clicks inside the column, because keyboard scrolling goes to the focused element and nothing scrollable has focus (<code>ScrollColumn.tsx:4-6</code>). The browser tests never scroll with the keyboard; they resize the viewport tall and take screenshots (<code>workflows.spec.ts:50-52</code>), so no assertion notices. Without <code>preventScroll: true</code>, <code>focus()</code> may scroll the newly focused <code>&lt;main&gt;</code> into view on load; since it sits directly under the header this is usually invisible, but on a page restored mid-scroll it would jump.</details>

5. `RunDetails` renders `JsonDetails` once and, when `finalSaveFailed` is set, two `ErrorBanner`s. Explain what happens to `workflows.spec.ts:31` if a teammate adds a second `JsonDetails` for the workflow snapshot inside "Run results", and what happens to `:263` if they turn the "Final result not saved" note in `ExecutionHistory.tsx:66-70` into an `ErrorBanner`.

<details><summary>Answer</summary>Line 31 calls <code>results.locator('pre').textContent()</code>. With two <code>&lt;pre&gt;</code> elements in the region the locator is no longer unique and Playwright throws a strictness error before <code>JSON.parse</code> runs, failing <code>run sequential checks and retain healthy and unhealthy results on reload</code>; that is what the comment at <code>JsonDetails.tsx:1</code> warns about. Line 263 counts alerts inside the "Run results" region only, and the history list is outside that region, so the count there would not change. Nor would anything else in the current suite: <code>:273</code>, <code>:320</code>, and the <code>toContainText</code> calls at <code>:311-314</code> are scoped to that same region, <code>:315</code> matches by text, which the change leaves intact, and the page-wide alert locators at <code>:68</code>, <code>:72</code>, <code>:74</code>, and <code>:125</code> run in tests where no run on screen has <code>finalSaveFailed</code>. The change would pass silently, which is why the comment at <code>ErrorBanner.tsx:9</code> is a convention the author has to keep rather than a rule the tests fully enforce; the first page-wide alert count written with an unsaved run on screen would then be off by one.</details>

6. `hasTcpStep` reads `step.output?.type`, not the loaded workflow's `steps[i].type`. Describe a sequence of edits and runs where the two would disagree, and say which rule decides.

<details><summary>Answer</summary>Run a workflow whose second step is a TCP check, then edit the JSON file to replace that step with an HTTP check and restart the server. The old run's saved output still carries <code>type: "tcp.check"</code> (<code>workflow.go:47-50</code>, <code>AGENTS.md:125-126</code>), so <code>hasTcpStep</code> returns true and the summary and help text use "check" wording and the TCP note at <code>RunDetails.tsx:33-37</code> appears, even though the current definition has no TCP step. Reading the definition instead would relabel history every time a file changed. <code>AGENTS.md:126-127</code> decides: "Keep historical results independent of the currently loaded workflow."</details>

## Experiments

1. **Rename a status label and watch the tests name it.** Change `succeeded: 'Completed'` at `format.ts:6` to `succeeded: 'Succeeded'` and run `make e2e` (it builds the frontend and binary, then runs Playwright against an isolated database; `Makefile:19-20`, `AGENTS.md:143`). Expected: three tests fail on `getByText('Completed', { exact: true })`: `run sequential checks and retain healthy and unhealthy results on reload` at `workflows.spec.ts:35`, `an unsaved final result keeps its execution status and shows a warning` at `:308`, and `TCP results and legacy HTTP history render from saved output after reload` at `tcp.spec.ts:122`. The Go tests are untouched, because the label never leaves the browser. Restore the line and rerun; all pass.

2. **Remove a tone and let the compiler find it.** Delete L18 of `Badge.tsx` (the `interrupted` entry) and run `npm --prefix web run typecheck`. Expected: one error at `web/src/components/Badge.tsx:7` saying property `interrupted` is missing in the object type but required in `Record<BadgeTone, string>`. No runtime test is needed to catch it. Restore the line; the check passes. Then try the opposite: add a made-up entry `paused: 'bg-badge-bg text-badge'` and see the error move to that line, because `Record` rejects extra keys as well.

3. **Drop the IPv6 brackets.** Change L39 of `format.ts` to `` return `${host}:${port}`; `` and run `make e2e`. Expected: `TCP results and legacy HTTP history render from saved output after reload` fails at `tcp.spec.ts:110`, expecting `[::1]:8080` and receiving `::1:8080`; `:126` would fail the same way. The Go executor still dials correctly, because `net.JoinHostPort` (`tcp.go:34`) does its own bracketing; only the display was wrong. Restore the line.

4. **See the scroll column take focus, and the details stay open across polls.** Run `make api` in one terminal and `make web` in another (`README.md:60-61`), open `http://127.0.0.1:5173/#/workflows?view=details`, open the browser's developer tools, and type `document.activeElement` in the console. Expected: the `<main>` element. Press PageDown without clicking: the column scrolls. Now comment out L11 of `ScrollColumn.tsx`, let Vite reload, and repeat: `document.activeElement` is `<body>` and PageDown scrolls nothing until you click in the column. Restore the line. Finally click "View execution JSON" under a run and click Run workflow: the JSON text changes with each one-second poll while the disclosure stays open, because the browser, not React, owns the `open` state (`JsonDetails.tsx:4`).

## Next

Chapter 18, "The canvas: drafts, React Flow, and the New workflow page" ([18-web-canvas.md](18-web-canvas.md))
