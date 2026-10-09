# 13. Frontend entry, routing, and shared workspace state

Files: [web/index.html](../../web/index.html), [web/src/main.tsx](../../web/src/main.tsx), [web/src/router.tsx](../../web/src/router.tsx), [web/src/App.tsx](../../web/src/App.tsx), [web/src/workspace.ts](../../web/src/workspace.ts), [web/src/pages/NotFoundPage.tsx](../../web/src/pages/NotFoundPage.tsx), [web/src/pages/ErrorPage.tsx](../../web/src/pages/ErrorPage.tsx)

Revision: commit 1c4786e

## Role in the system

These seven files are the frame that every page hangs in. `web/index.html` is the one HTML document the browser ever loads. `main.tsx` mounts React into it and wraps the whole tree in two providers: the TanStack Query client (chapter 14) and the TanStack Router. `router.tsx` declares three addresses, `/`, `/workflows`, and `/canvas`, and says which component draws each one. `App.tsx` is the component at the root of that route tree. It renders once and stays mounted while the pages below it come and go, so it is the place for state that must outlive a page switch: the selected workflow, the local drafts, the per-workflow canvas layouts, and the unfinished New workflow form. `workspace.ts` is the typed channel through which the pages read that state. `NotFoundPage.tsx` and `ErrorPage.tsx` are the two shell-level failure pages: one for an address that matches nothing, one for a page that throws while rendering. README.md:213-218 describes the App, router, not-found, and error-page responsibilities, and AGENTS.md:55-58 lists all of these files in the code map.

Two kinds of data come in. The URL comes from the browser: the hash fragment (`#/workflows?view=details`) names the page and the open view, and `router.tsx` turns it into a matched route and a validated `view` value. User events come from the pages: `createDraft` (App.tsx:16) is the only action the shell owns, and it only changes memory. Nothing in these files calls `fetch`. The one place the frontend talks to Go is `request` in `web/src/api.ts:65`, and the only callers are in `WorkflowsPage.tsx:44`, `:49`, and `:56`. What goes out is a rendered page inside `<Outlet />` (App.tsx:48) and a `Workspace` object (workspace.ts:5-14) that `WorkflowsPage.tsx:33-34` and `CanvasPage.tsx:18` read with `useWorkspace`.

On the path from a browser click to a saved run, these files sit before the click. The browser requests `/`, the Go file server at `internal/httpapi/api.go:122-133` returns `index.html`, `main.tsx` boots, the router reads `#/workflows` and renders `WorkflowsPage` inside `App`. Only then can the Run button at `WorkflowsPage.tsx:176-185` call `startRun`, whose `startMutation.mutate` (`WorkflowsPage.tsx:93`) posts to `api.go:50`, which calls the runner (chapter 6). The run id never appears in the route today; `docs/roadmap.md:83` says to put the workflow id and then the run id in the route "when the first link consumer arrives". Until then the selected workflow lives in React state (App.tsx:11), not in the URL, and a reload forgets it. The open view does live in the URL (router.tsx:26-32), and the rule that decides which belongs where is the question this chapter keeps returning to.

## Concepts you need

- **JSX and the automatic runtime.** JSX is the `<div>` syntax inside `.tsx` files. The TypeScript compiler turns `<AppHeader page="workflows" />` into a function call that builds a React element, a plain object describing what to draw. Because `web/tsconfig.json:7` sets `"jsx": "react-jsx"`, no file needs to import `React` for JSX to work; `main.tsx:1` imports only the names it uses. Text between tags is trimmed at line breaks, which is why `ErrorPage.tsx:20` and `:23` write `{' '}` for a space. [React: Writing markup with JSX](https://react.dev/learn/writing-markup-with-jsx)
- **Components, props, and one-way data flow.** A React component is a function that takes one `props` object and returns elements. Data flows down: `App` passes values into the context, the pages read them, and the pages ask for changes by calling functions that `App` handed down (`selectWorkflow`, `createDraft`). A child never reaches up and writes a parent's variable. [React: Passing props to a component](https://react.dev/learn/passing-props-to-a-component)
- **`useState` and functional updates.** `const [value, setValue] = useState(initial)` gives a component one piece of state that survives re-renders. Calling `setValue` schedules a re-render; the variable in the current function body does not change until that re-render runs. `setValue((current) => next)` passes an updater function, which React calls with the latest state, so several updates in one event cannot overwrite each other. App.tsx:20 and :29 use this form because each new value depends on the old one. [React: useState](https://react.dev/reference/react/useState)
- **Context: `createContext`, `useContext`, and the React 19 provider syntax.** Context passes a value to every component below a provider without threading it through props. `createContext(default)` makes the context object (workspace.ts:16); rendering `<WorkspaceContext value={...}>` provides a value (App.tsx:36-49); `useContext(WorkspaceContext)` reads the nearest provided value (workspace.ts:19). Before React 19 the provider was written `<WorkspaceContext.Provider>`; this project is on React 19.2.8 (`web/package.json`), where the context object itself is the provider. [React: createContext](https://react.dev/reference/react/createContext), [React: useContext](https://react.dev/reference/react/useContext)
- **Custom hooks.** A function whose name starts with `use` and that calls other hooks is a custom hook. `useWorkspace` (workspace.ts:18-22) wraps `useContext` and adds a check. Hooks follow the same rules as built-in ones: call them at the top level of a component, in the same order every render. [React: Reusing logic with custom hooks](https://react.dev/learn/reusing-logic-with-custom-hooks)
- **`createRoot` and `StrictMode`.** `createRoot(domNode).render(element)` is how React 18 and later attach a tree to the page (main.tsx:9). `StrictMode` (main.tsx:10) adds development-only checks: it renders each component twice and runs effects twice to expose code that is not safe to repeat. A production build removes every one of its effects. [React: createRoot](https://react.dev/reference/react-dom/client/createRoot), [React: StrictMode](https://react.dev/reference/react/StrictMode)
- **Error boundaries.** When a component throws while rendering, React unwinds to the nearest error boundary, which can draw a fallback instead of a blank page. A boundary catches rendering errors only, not errors thrown in event handlers or in `fetch` callbacks. TanStack Router puts a boundary around every route and renders `ErrorPage` in it (router.tsx:55). [React: Catching rendering errors with an error boundary](https://react.dev/reference/react/Component#catching-rendering-errors-with-an-error-boundary)
- **TanStack Router: route tree and `Outlet`.** Routes are built in code: a root route, child routes with `path`, and `addChildren` to connect them (router.tsx:16-48). The router matches the URL to one chain of routes and renders their components nested: the root's component draws `<Outlet />` where the matched child should appear (App.tsx:48). [TanStack Router: Route trees](https://tanstack.com/router/latest/docs/framework/react/guide/route-trees), [TanStack Router: Outlets](https://tanstack.com/router/latest/docs/framework/react/guide/outlets)
- **TanStack Router: hash history.** A history object tells the router how to read and write the address bar. Browser history uses real paths (`/workflows`); hash history keeps everything after `#` (`/#/workflows`), which the browser never sends to the server. router.tsx:50-54 picks hash history because the Go file server has no fallback to `index.html`. [TanStack Router: History types](https://tanstack.com/router/latest/docs/framework/react/guide/history-types)
- **TanStack Router: search param validation and `SearchSchemaInput`.** `validateSearch` turns the raw `?key=value` object into a typed value before the component renders; `useSearch` returns that value and `navigate({ search })` writes it. `SearchSchemaInput` marks the function's parameter as the input shape (what links may pass) while the return type is the output shape (what pages read), so `view` can be optional in a link and always present on the page (router.tsx:26-32). [TanStack Router: Search params](https://tanstack.com/router/latest/docs/framework/react/guide/search-params)
- **TanStack Router: `beforeLoad`, `redirect`, not-found and error components.** `beforeLoad` runs before a route's component; throwing a `redirect` from it sends the router elsewhere (router.tsx:21-23). `notFoundComponent` draws when no route matches (router.tsx:16); `defaultErrorComponent` draws when a route throws (router.tsx:55). [TanStack Router: Navigation](https://tanstack.com/router/latest/docs/framework/react/guide/navigation), [TanStack Router: Not found errors](https://tanstack.com/router/latest/docs/framework/react/guide/not-found-errors)
- **TanStack Router: type registration.** `declare module '@tanstack/react-router' { interface Register { router: typeof router } }` (router.tsx:59-63) is TypeScript module augmentation: it adds a member to a library interface. The router reads its own type from `Register`, so `<Link to="...">`, `useNavigate`, `useSearch({ from })`, and `redirect` accept only paths that exist. [TanStack Router: Type safety](https://tanstack.com/router/latest/docs/framework/react/guide/type-safety), [TypeScript: Module augmentation](https://www.typescriptlang.org/docs/handbook/declaration-merging.html#module-augmentation)
- **TanStack Query: `QueryClient` and `QueryClientProvider`.** A `QueryClient` holds a cache of server data keyed by arrays like `['workflows']`. `QueryClientProvider` puts one client into context so `useQuery` anywhere below it shares that cache (main.tsx:11). The client itself is configured in `queryClient.ts:3-17`, which chapter 14 covers. [TanStack Query: QueryClientProvider](https://tanstack.com/query/latest/docs/framework/react/reference/QueryClientProvider)
- **React Flow node objects.** `CanvasDraft.nodes` (canvasDraft.ts:4-9) is an array of React Flow `Node` objects: `id`, `type`, `position`, `data`. The shell stores these objects as they are; it never inspects them. `docs/roadmap.md:178` plans to replace this with the definition's own shape in M2. [React Flow: Nodes](https://reactflow.dev/api-reference/types/node)
- **TypeScript: `import type` and `verbatimModuleSyntax`.** `import type { X }` imports a name that exists only for the type checker; the bundler drops the line. `web/tsconfig.json:11` turns on `verbatimModuleSyntax`, which requires the `type` keyword on every type-only import so Vite can delete imports without analysing them. That is why router.tsx:8 is a separate statement from the value imports at router.tsx:1-7, and why router.tsx:10 and App.tsx:3 carry the keyword. [TypeScript: verbatimModuleSyntax](https://www.typescriptlang.org/tsconfig/#verbatimModuleSyntax)
- **TypeScript: the non-null assertion `!`.** Under `"strict": true` (`web/tsconfig.json:8`), `document.getElementById` returns `HTMLElement | null`. Writing `!` after it tells the compiler "this is not null" without a runtime check. It is a promise the author makes, and main.tsx:9 can make it because index.html:10 always contains the element. [TypeScript: Non-null assertion operator](https://www.typescriptlang.org/docs/handbook/release-notes/typescript-2-0.html#non-null-assertion-operator)
- **TypeScript: `Dispatch<SetStateAction<T>>`, `Record`, and the `in` narrowing.** `Dispatch<SetStateAction<T>>` is the exact type of a `useState` setter: a function that accepts either a `T` or an updater `(prev: T) => T`. `Record<string, CanvasDraft>` is an object used as a map from id to draft. `'steps' in workflow` narrows a union: a `Workflow` has `steps`, a `LocalWorkflow` does not, so after the check TypeScript knows which one it holds (WorkflowsPage.tsx:80, Sidebar.tsx:46). [TypeScript: Narrowing](https://www.typescriptlang.org/docs/handbook/2/narrowing.html)
- **The `void` operator on a promise.** `navigate(...)` returns a promise. `void navigate(...)` evaluates it and discards the result on purpose, which tells readers and lint rules that nobody is waiting for it (App.tsx:32). [MDN: void operator](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Operators/void)
- **`crypto.randomUUID()` and secure contexts.** The Web Crypto API makes a random version-4 UUID string. Browsers expose it only in a secure context: `https://` pages, or `http://` on `localhost` and `127.0.0.1`. App.tsx:19 relies on it. [MDN: Crypto.randomUUID()](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID)
- **Vite's `index.html` entry.** Vite treats `index.html` as the build entry. In development it serves the file and transforms `/src/main.tsx` on the fly; in a build it rewrites the script tag to a hashed bundle and injects the CSS link. [Vite: index.html and project root](https://vite.dev/guide/#index-html-and-project-root)
- **Tailwind utility classes.** `className="text-accent underline"` (ErrorPage.tsx:21) applies two small single-purpose classes instead of a named style. Tailwind scans the source for class names and generates only those; `web/src/index.css:42` defines the project's own `accent` color inside the `@theme` block that starts at `index.css:5`. [Tailwind: Styling with utility classes](https://tailwindcss.com/docs/styling-with-utility-classes)

## Walkthrough

### web/index.html

**L1–L1** — `<!doctype html>`. Puts the browser in standards mode. Without it the page renders in quirks mode, where box sizes and `h-dvh` layouts (`classes.ts:15`) behave differently.

**L2–L2** — `<html lang="en">`. Declares the document language for screen readers, spell checkers, and hyphenation. Every text string in the app is English.

**L3–L3** — Opens `<head>`.

**L4–L4** — `<meta charset="UTF-8" />`. The app renders characters such as `▶` (WorkflowsPage.tsx:182), `◎` (Sidebar.tsx:56), and `…` (WorkflowsPage.tsx:122). Without an explicit charset the browser guesses, and those glyphs can become garbage.

**L5–L5** — The viewport meta. Tells phones to use their real width and not pretend to be a 980px desktop. `README.md:483-484` says browser tests cover a mobile layout, and the `md:` and `lg:` breakpoints throughout the components (for example `AppHeader.tsx:14`) only make sense when the viewport is honest.

**L6–L6** — `<meta name="theme-color" content="#f4f6f5" />`. Mobile browsers color their own toolbar with this value. It is the same hex as `--color-canvas` in `web/src/index.css:18`, which `index.css:81-82` uses for the body background, so the browser frame blends into the page. If a designer changes the canvas color in `index.css` and forgets this line, the toolbar no longer matches.

**L7–L7** — `<title>Patchbay</title>`. The tab title. Nothing in the app changes it later.

**L8–L8** — Closes `<head>`.

**L9–L9** — Opens `<body>`.

**L10–L10** — `<div id="root"></div>`. The only element in the body, and the one React takes over. `main.tsx:9` finds it by this id. If the id changed here and not there, `createRoot` would throw at startup and the page would stay blank.

**L11–L11** — `<script type="module" src="/src/main.tsx">`. The single entry point. `type="module"` makes the browser load it as an ES module, which is what Vite serves in development (`README.md:61`, `vite.config.ts:7-12`). At build time Vite replaces this tag with a reference to the hashed bundle in `web/dist`, and the Go server serves that directory (`cmd/server/main.go:31`, `internal/httpapi/api.go:122-133`). The leading `/` is a root-relative path; it works under both servers because both serve the frontend at `/`.

**L12–L13** — Close `<body>` and `<html>`.

### web/src/main.tsx

**L1–L4** — Library imports. `StrictMode` from `react` for L10; `createRoot` from `react-dom/client` for L9, which is the React 18+ API (the older `ReactDOM.render` is gone); `QueryClientProvider` for L11; `RouterProvider` for L12. Each name is a value, so none needs `import type`.

**L5–L6** — Project imports. `queryClient` is the one `QueryClient` instance built at `queryClient.ts:3` (chapter 14). `router` is the one router built at `router.tsx:52`. Both are module-scope singletons: they are created when the module first loads and exist for the life of the tab, independent of any component.

**L7–L7** — `import './index.css'`. A side-effect import: nothing is named, the module just has to run. Vite sees it and bundles the stylesheet. `index.css` is the only stylesheet (AGENTS.md:61); it pulls in Tailwind and the React Flow styles (`index.css:1-3`). Without this line every `className` in the app would refer to a class that does not exist and the page would render as unstyled text.

**L8–L8** — Blank line.

**L9–L9** — `createRoot(document.getElementById('root')!).render(`. Finds the `<div id="root">` from `index.html:10`, creates a React root on it, and renders the tree below. The `!` tells TypeScript the element is not `null`; the author can promise that because `index.html` is part of the same repository. If the element were missing at runtime, React would throw "Target container is not a DOM element" before drawing anything.

**L10–L10** — `<StrictMode>`. Wraps everything so development double-renders apply to the whole tree. It has no behavior in a production build, so it costs nothing to keep.

**L11–L11** — `<QueryClientProvider client={queryClient}>`. Puts the query client into React context. It sits outside the router on purpose: every page the router renders can then call `useQuery` (`WorkflowsPage.tsx:42`, `:47`), `useMutation` (`WorkflowsPage.tsx:54`), and `useQueryClient` (`WorkflowsPage.tsx:35`). If the two providers were swapped, those hooks would throw "No QueryClient set" the first time a page rendered. Because the client lives above the router, the cache survives route changes, which is what lets the workflows page show its last lists immediately when you come back from the New workflow page (`README.md:219-221`).

**L12–L12** — `<RouterProvider router={router} />`. Starts the router: it reads the current hash, matches it against the tree from `router.tsx:48`, and renders the matched components. From here on, `App` (`router.tsx:16`) is the top of the visible tree.

**L13–L15** — Close the providers, `StrictMode`, and the `render(` call.

### web/src/router.tsx

**L1–L7** — Five value imports from the router. `createHashHistory` for L54, `createRootRoute` for L16, `createRoute` for the three routes, `createRouter` for L52, and `redirect` for L22. The multi-line form is Prettier's formatting once the list no longer fits on one line.

**L8–L8** — `import type { SearchSchemaInput }`. Type-only, so it needs the `type` keyword under `verbatimModuleSyntax` (`web/tsconfig.json:11`). It is used at L28 to mark the validator's parameter as the input shape.

**L9–L9** — `App`, the root route component (L16). The import direction is router → App → workspace; `App.tsx` imports `Outlet` and `useNavigate` from the `@tanstack/react-router` library (App.tsx:2), never from `./router`, so there is no cycle.

**L10–L10** — `import type { WorkflowView }`. The union `'canvas' | 'details'` is defined once at `ViewTabs.tsx:3`, beside the tab bar that draws it, and the router reuses it so the URL, the tabs, and the pages agree on the two allowed values. The other importers are `WorkflowsPage.tsx:17` and `CanvasPage.tsx:13`.

**L11–L14** — The four page components. `ErrorPage` and `NotFoundPage` are wired as fallbacks (L16, L55); `WorkflowsPage` and `CanvasPage` are the two real pages (L38, L45). This file is the only importer of all four; nothing else needs to know how pages are chosen.

**L15–L15** — Blank line.

**L16–L16** — `createRootRoute({ component: App, notFoundComponent: NotFoundPage })`. The root matches every URL. Its `component` renders first and must draw `<Outlet />` for the children (`App.tsx:48`). `notFoundComponent` is what the router draws, inside that same outlet, when the hash matches no child route, for example `#/nothing`. Because it renders below `App`, the `WorkspaceContext` exists on the not-found page too; `NotFoundPage` simply does not use it. `README.md:216-217` promises "an unknown address shows a not-found page with a link back to the workflows", and this one option is what keeps that promise.

**L17–L17** — Blank line.

**L18–L20** — `indexRoute` begins: its parent is the root, its path is `/`. `getParentRoute` is a function returning the parent rather than the parent itself so TypeScript can infer the tree without circular type references. A fresh tab opened at `http://127.0.0.1:8080/` has an empty hash, which the hash history reads as `/`, so this is the route every first visit hits.

**L21–L23** — `beforeLoad` throws `redirect({ to: '/workflows', replace: true })`. `beforeLoad` runs before the route renders anything; throwing a redirect makes the router navigate instead. The index route has no `component`, so without this redirect a first visit would render `App` with an empty outlet: header-less, blank, with no way forward. `replace: true` overwrites the history entry for `/` rather than adding one, so pressing Back after the redirect leaves the app instead of bouncing back to `/` and redirecting again. The Playwright test at `web/tests/workflows.spec.ts:78` opens `/` and expects the canvas for the first workflow at `:82`; it passes only because this redirect lands on `/workflows` and L31 defaults the view to `canvas`.

**L24–L24** — Closes `indexRoute`.

**L25–L25** — Blank line.

**L26–L27** — The comment states both decisions in this block: the view lives in the URL "so a reload keeps it", and `SearchSchemaInput` "makes `view` optional for links while the validated output is always set". `README.md:188-189` makes the same promise to users: "The open view is part of the URL (`#/workflows?view=details`), so a reload keeps it."

**L28–L30** — `validateView` signature. The parameter type `{ view?: WorkflowView } & SearchSchemaInput` is what callers may pass: `view` optional. The return type `{ view: WorkflowView }` is what pages receive: `view` always present. This split is why `<Link to="/workflows">` with no search compiles (`NotFoundPage.tsx:16`, `AppHeader.tsx:28`) and why `const { view } = useSearch({ from: '/workflows' })` at `WorkflowsPage.tsx:37` needs no fallback.

**L31–L31** — `return { view: search.view === 'details' ? 'details' : 'canvas' }`. The whole validator. Any value other than the exact string `details`, including a missing value, a typo, or `?view=garbage`, becomes `canvas`. It never throws. That matters: a validator that threw on bad input would send the user to `ErrorPage` for a mistyped URL. Here a bad URL just shows the Canvas view, the same default that the test at `web/tests/workflows.spec.ts:78-82` relies on after the index redirect.

**L32–L32** — Closes `validateView`.

**L33–L33** — Blank line.

**L34–L39** — `workflowsRoute`: path `/workflows`, `validateSearch: validateView`, component `WorkflowsPage`. The validator runs on every navigation to this path, before the page renders, so `WorkflowsPage.tsx:37` always reads a clean value. `WorkflowsPage.tsx:96-98` writes the other direction with `navigate({ to: '/workflows', search: { view: next }, replace: true })`; because `view` is in the search schema, the compiler checks that `next` is a `WorkflowView`.

**L40–L40** — Blank line.

**L41–L46** — `canvasRoute`: path `/canvas`, the same validator, component `CanvasPage`. Sharing `validateView` means both pages own the same `view` contract and can share `ViewTabs`. `CanvasPage.tsx:19` reads it with `useSearch({ from: '/canvas' })` and `CanvasPage.tsx:22-24` writes it. `README.md:195` names this page's address: `http://localhost:8080/#/canvas`.

**L47–L47** — Blank line.

**L48–L48** — `rootRoute.addChildren([indexRoute, workflowsRoute, canvasRoute])`. Attaches the three children and produces the typed tree the router needs. Array order does not decide matching; the router ranks routes by their paths. A route created with `createRoute` but left out of this array would never match.

**L49–L49** — Blank line.

**L50–L51** — The comment explains the history choice in terms of the Go server: "Hash history keeps every page load at "/", which the Go file server serves without a fallback to index.html. Browser history needs that fallback in internal/httpapi first." Check it against `api.go:122-133`: the catch-all `/` handler calls `http.FileServer`, which looks for a real file. A request for `/workflows` would get Go's 404 page, because no file by that name exists in `web/dist`. With hash history the browser only ever requests `/`, and `#/workflows` stays in the address bar.

**L52–L56** — `createRouter({ routeTree, history: createHashHistory(), defaultErrorComponent: ErrorPage })`, exported as `router`. `history` is the hash history from the comment above. `defaultErrorComponent` is the fallback for any route that does not declare its own `errorComponent`; none here does, so every rendering error in any page ends at `ErrorPage`. The export is consumed by `main.tsx:6` and by the type registration at L61.

**L57–L57** — Blank line.

**L58–L58** — The comment: registering the router "gives Link, useNavigate, and redirect the route paths as types".

**L59–L63** — `declare module '@tanstack/react-router' { interface Register { router: typeof router } }`. Module augmentation: this adds a `router` member to the library's empty `Register` interface. The library's `Link`, `useNavigate`, `useSearch`, and `redirect` read their path and search types from `Register`. With it, `to: '/workflowz'` is a compile error and `useSearch({ from: '/workflows' })` knows that the result has a `view`. The block produces no JavaScript; it exists only for `tsc --noEmit` (`web/package.json`, `typecheck` script), which `make test` runs (`Makefile:17`).

### web/src/App.tsx

**L1–L2** — `useState` from React for the four state hooks at L11-L14. `Outlet` and `useNavigate` from the router for L48 and L10.

**L3–L3** — `import type { CanvasDraft, CanvasDrafts, LocalWorkflow }` from `canvasDraft.ts`. `CanvasDraft` (`canvasDraft.ts:6-9`) is a name plus React Flow nodes; `CanvasDrafts` (`:18`) is a map from workflow id to draft; `LocalWorkflow` (`:11-16`) is a sidebar entry for a draft that has no saved steps. The types live beside the canvas code because the canvas is the only thing that interprets them; `App` only stores them.

**L4–L4** — `WorkspaceContext`, the context object from `workspace.ts:16`. `App` provides it; the pages consume it through `useWorkspace`.

**L5–L5** — Blank line.

**L6–L8** — The comment is the design of the file in three sentences. "The router renders App once at the root and swaps pages inside Outlet, so the state kept here survives navigation and API polling." That is why the four `useState` calls are here and not in `WorkflowsPage`: a page unmounts when you leave it and its state is gone. "Unmounting the runner stops polling while the New workflow page is open" says the opposite is also intended: the `useQuery` calls with `refetchInterval: 1000` are in `WorkflowsPage.tsx:42-51`, so leaving that page stops the once-a-second requests. `README.md:190-192` states the user-facing version: layouts "remain in memory while you switch workflows or visit the New workflow page; normal API polling does not reset them."

**L9–L9** — `export default function App()`. No props: the router renders it with none. It is the `component` at `router.tsx:16`.

**L10–L10** — `const navigate = useNavigate()`. Returns a function that changes the URL. It is called once, at L32, after a draft is created.

**L11–L11** — `workflowId`, initially `''`. The id of the sidebar selection, shared so that it survives a trip to the New workflow page. `''` means "nothing chosen yet"; `WorkflowsPage.tsx:79` turns that into "the first available workflow" with `?? availableWorkflows[0]`. The same fallback covers an id that disappears, for example after the server restarts with different files. The setter is handed down as `selectWorkflow` (L39) and called from `WorkflowsPage.tsx:86`.

**L12–L12** — `localWorkflows`, initially an empty array. Every draft the user created with Create draft, in creation order. `WorkflowsPage.tsx:78` appends these after the server's list so they appear at the bottom of the sidebar. They are `LocalWorkflow` values, which have no `steps`, and that absence is how the rest of the UI knows not to offer a Run button (`WorkflowsPage.tsx:80`, `:175`). AGENTS.md:70 states the limit this state enforces: "Canvas nodes and new workflow drafts are frontend-only and are lost on reload."

**L13–L13** — `canvasDrafts`, initially `{}`. One `CanvasDraft` per workflow id, for saved and local workflows alike, holding the node positions the user dragged. Absent keys mean "never touched"; `WorkflowsPage.tsx:136` then builds a fresh draft from the saved steps with `draftFromWorkflow` (`canvasDraft.ts:44-52`). `Sidebar.tsx:48` reads the node count for local drafts from this map. Keeping it keyed by id is what makes layouts "belong to that workflow only" (`README.md:189-190`).

**L14–L14** — `newWorkflow`, initially `{ name: '', nodes: [] }`. The one draft being built on the New workflow page. `CanvasPage.tsx:18` reads it as `draft` and writes it through `setNewWorkflow`. Holding it here, not in `CanvasPage`, means you can start a draft, look at the Workflows page, and come back to find it intact.

**L15–L15** — Blank line.

**L16–L16** — `function createDraft()`. The only action the shell owns. It is called from the form submit at `CanvasPage.tsx:51` and reaches there through the context value at L45.

**L17–L18** — Trim the name and return if it is empty. `CanvasPage.tsx:74` already disables the button and `:51` already checks `trim()`, so this is the third guard. It is here because the function is the one place that creates a draft, and a page that forgot its own check must still not create a nameless workflow.

**L19–L19** — ``const id = `local-${crypto.randomUUID()}` ``. A new id for the draft. The `local-` prefix makes the id recognisable in DevTools; the code never tests for it, because `'steps' in workflow` (`WorkflowsPage.tsx:80`, `Sidebar.tsx:46`) is the real discriminator. The UUID makes collisions with saved ids and with earlier drafts practically impossible. `crypto.randomUUID` is only defined in a secure context; `localhost` and `127.0.0.1` qualify; `README.md:60-72` names the `127.0.0.1` addresses and `README.md:195` the `localhost` one. The other caller in the frontend is `WorkflowCanvas.tsx:33`, for node ids.

**L20–L28** — `setLocalWorkflows((current) => [...current, { local: true, id, name, description: ... }])`. The updater form reads the latest array and returns a new one with the draft appended; the old array is never mutated, which is how React detects a change. `local: true` is the literal type from `canvasDraft.ts:12`; `description` is the fixed sentence the Details view shows under the heading (`WorkflowsPage.tsx:165`), and it tells the user the truth stated at `README.md:198-199`: a local draft runs nothing.

**L29–L29** — `setCanvasDrafts((current) => ({ ...current, [id]: { ...newWorkflow, name } }))`. Copies the map and adds one entry under the new id. The entry copies the New workflow draft and replaces its `name` with the trimmed one, so what the user arranged on the blank canvas becomes that workflow's layout. `[id]` is a computed property key. After this line the draft and `newWorkflow` share the same `nodes` array, which is safe only because L31 throws `newWorkflow` away.

**L30–L30** — `setWorkflowId(id)`. Selects the new draft, so the Workflows page opens on it (`WorkflowsPage.tsx:79`). This is "opens its own page" in `README.md:197-198`.

**L31–L31** — `setNewWorkflow({ name: '', nodes: [] })`. Resets the form and the blank canvas so the next visit to New workflow starts empty. Without it, the next Create draft would copy the same nodes again.

**L32–L32** — `void navigate({ to: '/workflows' })`. Moves to the Workflows page. No `search` is passed, so the router starts from an empty search object and `validateView` (`router.tsx:31`) chooses `canvas`: the user lands on the new draft's canvas. React batches the four state updates above with this navigation, so `WorkflowsPage` mounts and reads a workspace in which the draft already exists. `void` discards the returned promise on purpose.

**L33–L33** — Closes `createDraft`.

**L34–L34** — Blank line.

**L35–L35** — `return (`. Everything below is what `App` draws.

**L36–L47** — `<WorkspaceContext value={{ ... }}>`. React 19's provider syntax. The object literal lists every member of the `Workspace` type (`workspace.ts:5-14`); TypeScript rejects a missing or misspelled key. `selectWorkflow: setWorkflowId` exposes the setter under the narrower type `(id: string) => void`, so pages can pass an id but not an updater function; the two canvas setters are exposed as full `Dispatch<SetStateAction<...>>` because `WorkflowsPage.tsx:138` and `CanvasPage.tsx:64` need the updater form. A new object is built on every render of `App`, which makes every consumer re-render whenever `App`'s state changes. With only two consumers, both of which show that state, that is the intended effect.

**L48–L48** — `<Outlet />`. The slot where the router draws the matched child: `WorkflowsPage`, `CanvasPage`, or, for an unknown hash, `NotFoundPage` (`router.tsx:16`). `App` draws nothing else: no header, no layout. Each page owns its own `AppHeader`, which is why the not-found and error pages render one themselves (`NotFoundPage.tsx:9`, `ErrorPage.tsx:13`).

**L49–L51** — Close the provider, the `return`, and the function.

### web/src/workspace.ts

**L1–L3** — `createContext` and `useContext` are values; `Dispatch` and `SetStateAction` are types, so L2 uses `import type`. L3 imports the three draft types used in the `Workspace` fields. The file is `.ts`, not `.tsx`, because it contains no JSX.

**L4–L4** — Blank line.

**L5–L7** — `Workspace` begins with `workflowId: string` and `selectWorkflow: (id: string) => void`. The selection and the only way to change it. The function type is deliberately smaller than the setter it wraps (App.tsx:39): pages choose an id, they do not compute one from the previous value.

**L8–L10** — `localWorkflows: LocalWorkflow[]`, read-only from the pages' point of view because no setter is exposed; the only way to add one is `createDraft`. `canvasDrafts` and `setCanvasDrafts: Dispatch<SetStateAction<CanvasDrafts>>`, the full setter because `WorkflowsPage.tsx:138-141` must merge one workflow's update into the map with an updater function.

**L11–L13** — `newWorkflow: CanvasDraft` and `setNewWorkflow: Dispatch<SetStateAction<CanvasDraft>>`. `CanvasPage.tsx:38` passes `setNewWorkflow` straight to `WorkflowCanvas` as its `onChange`, whose type is `UpdateCanvasDraft` (`canvasDraft.ts:19`): a function taking an updater. A `useState` setter accepts updaters, so the assignment type-checks without an adapter. `createDraft: () => void` is the action from App.tsx:16.

**L14–L14** — Closes the type.

**L15–L15** — Blank line.

**L16–L16** — `export const WorkspaceContext = createContext<Workspace | null>(null)`. The default is `null`, not a fake workspace, so that reading the context outside `App` is detectable. The context and the hook live in this file rather than in `App.tsx`; AGENTS.md:57 lists the two files as one unit. The split also leaves `App.tsx` exporting only a component, which is what Vite's React plugin needs to hot-swap a module cleanly; a file that mixed a component with other exports would force a full reload on every edit.

**L17–L17** — Blank line.

**L18–L18** — `export function useWorkspace(): Workspace`. The return type drops `null`, so callers get a usable object without checking. Callers: `WorkflowsPage.tsx:33-34` and `CanvasPage.tsx:18`.

**L19–L19** — `const workspace = useContext(WorkspaceContext)`. Reads the nearest provided value, which is the object from App.tsx:37-46 whenever the caller is under `App`.

**L20–L20** — `if (!workspace) throw new Error('useWorkspace needs the App route component above it.')`. Fails fast with a sentence that names the fix. Without this line a page rendered outside `App` would crash later on `workspace.workflowId` with "Cannot read properties of null", far from the cause. Because the throw happens during render, the router's error boundary catches it and `ErrorPage` shows this exact message (`ErrorPage.tsx:20`, `format.ts:19-21`).

**L21–L22** — Return the workspace and close the function.

### web/src/pages/NotFoundPage.tsx

**L1–L4** — `Link` from the router for L16; `AppHeader` and `PageHeading` from the shared components; `wideColumnClass` from `classes.ts:17-19`, whose comment says "The New workflow, not-found, and error pages use a wider column than the runner." No `useWorkspace`, no `useSearch`: this page must render for any hash, including one that matches no route with a search schema.

**L5–L5** — Blank line.

**L6–L8** — `export default function NotFoundPage()` returns a plain `<div>`. No props: the router renders it with none (`router.tsx:16`). It does not use `pageShellClass` (`classes.ts:15`) because it has nothing to scroll; a static heading in a normal document flow is enough.

**L9–L9** — `<AppHeader page="workflows" />`. Draws the same header as the real pages, so the user still sees the logo and the two navigation links (`AppHeader.tsx:28-33`). `page="workflows"` picks the status-dot variant at `AppHeader.tsx:39-46`; with `loading` and `disconnected` left at their `false` defaults (`AppHeader.tsx:12`) it reads "Local workspace". Neither nav link shows as current, because the current hash matches neither path.

**L10–L10** — `<main className={wideColumnClass}>`. The centered column with the page's padding.

**L11–L13** — `PageHeading` with `eyebrow="NOT FOUND"` and `title="Page not found"`. The eyebrow is the small uppercase label `PageHeading.tsx:14-16` draws above the `<h1>`.

**L14–L18** — The `description` prop as a fragment: one sentence and `<Link to="/workflows">Go to workflows</Link>`. `PageHeading.tsx:6` types `description` as `ReactNode`, so elements are allowed, and `PageHeading.tsx:18-20` draws it only when present. The `Link` is typed by the registration at `router.tsx:59-63`, so a wrong path is a compile error. Under hash history it renders as `<a href="#/workflows">`, and because no `search` is given, the destination opens on the `canvas` view (`router.tsx:31`). This is the "link back to the workflows" from `README.md:217`.

**L19–L23** — Close `PageHeading`, `<main>`, the `<div>`, the `return`, and the function.

### web/src/pages/ErrorPage.tsx

**L1–L2** — `Link` for L24 and the type `ErrorComponentProps` for L10. The type-only import gets its own line because of `verbatimModuleSyntax`.

**L3–L6** — The same header, column class, and heading as the not-found page, plus `errorMessage` from `format.ts:19-21`, which returns `error.message` for an `Error` and a generic sentence for anything else. The same helper turns query and mutation failures into banner text at `WorkflowsPage.tsx:74` and `:76`, so a thrown error and a failed request read the same way to the user.

**L7–L7** — Blank line.

**L8–L9** — The comment: "The router renders this in place of a page that threw while rendering, so it must not depend on that page's state." The page that threw may have thrown because its state was bad. If this page called `useSearch({ from: '/workflows' })` or `useWorkspace`, it could throw too, and then nothing would render at all. So it uses only props the router hands it and components that take plain values.

**L10–L10** — `export default function ErrorPage({ error, reset }: ErrorComponentProps)`. The router passes the thrown `error` and a `reset` function that clears the boundary and renders the route again. This component is wired at `router.tsx:55` as `defaultErrorComponent`, so it covers every route.

**L11–L13** — `return (`, the outer `<div>`, and `<AppHeader page="workflows" />`. The header is drawn here, not by `App`, because `App` renders only an `Outlet` (App.tsx:48). The navigation links in the header are a second way out when the error repeats.

**L14–L14** — `<main className={wideColumnClass}>`, the same column as the not-found and New workflow pages (`classes.ts:17-19`).

**L15–L17** — `PageHeading` with `eyebrow="ERROR"` and `title="Something went wrong"`.

**L18–L20** — The description opens with `{errorMessage(error)}` followed by `{' '}`. The message is the developer's text, for example the sentence from `workspace.ts:20`. JSX drops whitespace at line breaks, so the explicit `{' '}` keeps a space before the button.

**L21–L23** — `<button type="button" className="text-accent underline" onClick={reset}>Try again</button>`. `type="button"` stops a browser from treating it as a submit button. It is a button and not a link because it performs an action in place rather than navigating; the two Tailwind utilities make it look like the links beside it. `reset` re-renders the failed route; if the cause is still there, the page throws again and this component comes back at once.

**L24–L24** — `or <Link to="/workflows">go to workflows</Link>.` The escape route when retrying does not help. Navigating to a different route gives the boundary a fresh start, and `WorkflowsPage` renders from the beginning. This is the "retry link" from `README.md:218`.

**L25–L31** — Close the fragment, `PageHeading`, `<main>`, the `<div>`, the `return`, and the function.

## State and rendering

Four homes hold state in this part of the app, and each has a different lifetime.

**Module scope.** `router` (`router.tsx:52`) and `queryClient` (`queryClient.ts:3`) are created once when their modules load and live until the tab closes. They are not React state; changing them never re-renders anything by itself.

**TanStack Query cache.** The `['workflows']` and `['runs']` entries exist inside `queryClient`. They are written by the polling queries in `WorkflowsPage.tsx:42-51` and by the optimistic update at `WorkflowsPage.tsx:62-65`. Because `QueryClientProvider` sits above `RouterProvider` (`main.tsx:11-12`), the cache survives when `WorkflowsPage` unmounts; what stops is the once-a-second refetch, since no mounted component asks for the data any more. Returning to the page shows the cached lists immediately and refetches in the background. Chapter 14 covers the defaults that govern retries and background polling.

**App component state.** `workflowId`, `localWorkflows`, `canvasDrafts`, and `newWorkflow` (`App.tsx:11-14`) live as long as `App` is mounted, which is as long as the tab is open and not reloaded. A route change swaps what `<Outlet />` draws but does not touch `App`, so these values persist across pages. A reload rebuilds the whole tree from `index.html`, and the four values start over at their initial literals. AGENTS.md:70 and README.md:202-203 state that limit; nothing in the code tries to hide it.

**URL search params.** `view` lives in the hash, after `?`. It is written by `navigate({ search: { view } })` (`WorkflowsPage.tsx:97`, `CanvasPage.tsx:23`), validated by `validateView` (`router.tsx:28-32`) on every navigation, and read by `useSearch` (`WorkflowsPage.tsx:37`, `CanvasPage.tsx:19`). It survives reloads and can be bookmarked, which is why the Playwright specs open `/#/workflows?view=details` directly (`web/tests/workflows.spec.ts:9`, `:58`, `:120`). It is per page: moving from `/workflows?view=details` to `/canvas` with a plain link starts the canvas page at its own default.

**Page component state.** `runId` (`WorkflowsPage.tsx:36`) is local to the page and resets when the page unmounts. That is deliberate: which run you were inspecting is not something the New workflow page needs.

What triggers a re-render. A call to any `App` setter re-renders `App`, builds a new context value (`App.tsx:37-46`), and re-renders whichever consumer is mounted; only one page is in the `Outlet` at a time. A navigation re-renders the `Outlet` with a different child; the old page's hooks and local state are discarded. A poll result changes the query cache and re-renders only the components subscribed to that query, which are inside `WorkflowsPage`; `App` does not subscribe, which is what the comment at `App.tsx:6-7` means by "survives API polling".

How data reaches the screen. `GET /api/workflows` becomes `workflowsQuery.data`, is merged with `localWorkflows` at `WorkflowsPage.tsx:78`, and the merged list feeds the `Sidebar` (`WorkflowsPage.tsx:105-110`). `workflowId` from the context picks `selected` at `WorkflowsPage.tsx:79`. `canvasDrafts[selected.id]`, or a draft built from the saved steps, feeds `WorkflowCanvas` at `WorkflowsPage.tsx:136`, and edits flow back through `setCanvasDrafts` at `:138-141`. `view` decides whether the canvas or the details column is drawn at `WorkflowsPage.tsx:133`.

How errors reach the user. There are three separate paths, and they do not cross. A request that fails lands in `workflowsQuery.error`, `runsQuery.error`, or `startMutation.error` and is drawn as an `ErrorBanner` by `WorkflowsPage.tsx:114-119`; the page keeps rendering. A component that throws while rendering, including `useWorkspace` called in the wrong place (`workspace.ts:20`), is caught by the route's boundary and replaced by `ErrorPage` (`router.tsx:55`), which offers `reset` and a link out. A hash that matches no route is drawn as `NotFoundPage` (`router.tsx:16`). An error thrown inside an event handler, such as `createDraft` on a page served from a non-secure origin where `crypto.randomUUID` is undefined, goes to none of these: React does not route handler exceptions to boundaries, so it reaches only the browser console and the click appears to do nothing.

## Check yourself

1. The user is on `#/workflows`, has dragged two nodes on the canvas for `browser-checks`, clicks New workflow, then clicks Workflows again. Which pieces of state still hold their values, which were reset, and which lines decide each?

<details><summary>Answer</summary>The canvas layout survives: it is in <code>canvasDrafts</code> at <code>App.tsx:13</code>, and <code>App</code> never unmounted because the router only swapped the child inside <code>Outlet</code> (<code>App.tsx:48</code>). The selection survives for the same reason (<code>App.tsx:11</code>). The <code>view</code> search param was reset: the header link at <code>AppHeader.tsx:28</code> passes no <code>search</code>, so <code>validateView</code> at <code>router.tsx:31</code> returned <code>canvas</code>. <code>runId</code> at <code>WorkflowsPage.tsx:36</code> was reset to <code>''</code> because the page unmounted. The query cache kept its last lists (<code>main.tsx:11</code> sits above the router), but polling stopped while the New workflow page was open and resumed on return.</details>

2. Suppose `main.tsx:11-12` were swapped so `RouterProvider` wrapped `QueryClientProvider`. What breaks, and at which line does the failure first show?

<details><summary>Answer</summary>Nothing changes until a page that uses queries renders. <code>WorkflowsPage</code> calls <code>useQueryClient()</code> at <code>WorkflowsPage.tsx:35</code> and <code>useQuery</code> at <code>:42</code>, and both look up the client in context. With the provider inside the router, those components are rendered by the router outside any provider, so the hook throws "No QueryClient set, use QueryClientProvider to set one". That throw happens during render, so the route boundary catches it and <code>ErrorPage</code> appears with that message at <code>ErrorPage.tsx:20</code>. The New workflow page would still work, because it runs no queries.</details>

3. Trace `createDraft` (App.tsx:16-33) and explain why the user lands on the new draft's canvas rather than on the first saved workflow's details view. Name the lines that make each decision.

<details><summary>Answer</summary><code>App.tsx:30</code> sets <code>workflowId</code> to the new id, so when <code>WorkflowsPage</code> mounts, <code>WorkflowsPage.tsx:79</code> finds the draft in <code>availableWorkflows</code> (merged at <code>:78</code>) and selects it instead of falling back to the first saved workflow. <code>App.tsx:32</code> navigates to <code>/workflows</code> with no <code>search</code>, so <code>router.tsx:31</code> validates an empty search as <code>view: 'canvas'</code>, and <code>WorkflowsPage.tsx:133</code> draws the canvas. The canvas reads <code>canvasDrafts[selected.id]</code> at <code>:136</code>, which <code>App.tsx:29</code> filled with the nodes from the New workflow page. React batches the state updates with the navigation inside the same event, so the page never renders an intermediate state.</details>

4. `validateView` (router.tsx:28-32) never throws. What would be different for the user if it threw on an unknown value, and which option at `router.tsx:52-56` would decide what they see?

<details><summary>Answer</summary>A URL such as <code>#/workflows?view=detail</code> (missing the final <code>s</code>) would make the route fail before rendering. TanStack Router treats a search validation error as a route error, so the <code>defaultErrorComponent</code> at <code>router.tsx:55</code> would render <code>ErrorPage</code> with the validator's message, and the user would have to click "go to workflows" to recover. As written, L31 maps every unknown value to <code>canvas</code>, so the same URL simply opens the canvas. The trade-off is that a typo is silently corrected rather than reported; for a view toggle, silent correction is the better default.</details>

5. `WorkspaceContext` is created with `null` (workspace.ts:16) and `useWorkspace` throws when it reads `null` (workspace.ts:20). Why not create the context with a harmless default object so the throw is never needed?

<details><summary>Answer</summary>A default object would hide the bug. A page rendered outside <code>App</code> would read an empty workflow id, an empty draft list, and setters that update nothing; it would render and seem to work until a click did nothing. The <code>null</code> default plus the throw turns that misuse into an immediate, named error during render, which the boundary at <code>router.tsx:55</code> shows on screen with the sentence from L20. Today every consumer (<code>WorkflowsPage.tsx:33</code>, <code>CanvasPage.tsx:18</code>) is a child route of <code>App</code>, so the throw never fires in production; it exists to protect the next route someone adds.</details>

6. `NotFoundPage` and `ErrorPage` both draw their own `AppHeader`. What does that tell you about the root route's component, and what would you have to change if `App` drew the header instead?

<details><summary>Answer</summary>It tells you <code>App</code> renders only <code>&lt;Outlet /&gt;</code> (<code>App.tsx:48</code>) and owns no layout, so every page, including the two fallbacks, must draw its own header. If <code>App</code> drew the header, <code>NotFoundPage.tsx:9</code> and <code>ErrorPage.tsx:13</code> would have to lose theirs, and so would <code>WorkflowsPage.tsx:102</code> and <code>CanvasPage.tsx:28</code>. But the header takes <code>loading</code> and <code>disconnected</code> from the queries in <code>WorkflowsPage</code> (<code>WorkflowsPage.tsx:72-74</code>), and those queries would then have to move up into <code>App</code>, which would keep polling on every page, breaking the "unmounting the runner stops polling" rule at <code>App.tsx:7-8</code>. The per-page header is the cost of that rule.</details>

## Experiments

1. **Flip the default view and watch the route test fail.** Change `router.tsx:31` to return `'details'` when `search.view` is anything other than `'canvas'` (swap the two literals). Run `make e2e`, which builds the frontend and the binary and then runs Playwright (`Makefile:19-20`). Expected: the test `the Canvas and Details views switch and survive a reload` fails at `web/tests/workflows.spec.ts:82`, because after `page.goto('/')` the index redirect (router.tsx:21-23) lands on `/workflows` with an empty search, which now validates to `details`, and the region "Canvas for Browser checks" is never visible. The specs that open `/#/workflows?view=details` directly still pass. Restore L31 and rerun; all tests pass.

2. **Remove the non-null assertion.** Delete the `!` at `main.tsx:9` and run `npm --prefix web run typecheck`. Expected: one error, TS2345, at `web/src/main.tsx:9`, saying that `HTMLElement | null` is not assignable to the parameter type of `createRoot`, because `getElementById` may return `null` under `strict` (`web/tsconfig.json:8`). Put the `!` back; the check passes. Then decide for yourself whether a runtime `if` with a clear error would be better than the assertion, given that `index.html:10` is in the same repository.

3. **See what the type registration buys.** Change `to="/workflows"` at `NotFoundPage.tsx:16` to `to="/workflowz"` and run `npm --prefix web run typecheck`. Expected: an error at `web/src/pages/NotFoundPage.tsx:16` rejecting the string, because `Register` (router.tsx:59-63) tells `Link` which paths exist. Now comment out router.tsx:59-63 and run the check again. Expected: the typo is no longer reported, because without a registered router `to` accepts any string. Restore both edits.

4. **Watch a state update not apply until the next render.** Insert `console.log('drafts before this click:', localWorkflows.length);` as the first line inside `createDraft`, after `App.tsx:16`. Start the backend with `make api` in one terminal and `npm --prefix web run dev` in another, open `http://127.0.0.1:5173/#/canvas`, switch to Details, type a name, and click Create draft. Expected: the console prints `0`, the sidebar shows one local draft, and the URL becomes `#/workflows`. Open New workflow again, create a second draft, and the console prints `1`. The log shows the value of `localWorkflows` from the render in which the handler was created; the updater at `App.tsx:20` is what sees the current array. Remove the line when done.

## Next

Chapter 14, "Talking to the API: types, request helper, and query defaults" ([14-web-api-and-query-client.md](14-web-api-and-query-client.md))
