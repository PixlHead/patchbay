# 1. Project layout, build, and packaging

Files: [go.mod](../../go.mod), [Makefile](../../Makefile), [Dockerfile](../../Dockerfile), [compose.yaml](../../compose.yaml), [.gitignore](../../.gitignore), [.dockerignore](../../.dockerignore), [web/package.json](../../web/package.json), [web/vite.config.ts](../../web/vite.config.ts), [web/tsconfig.json](../../web/tsconfig.json), [web/.prettierrc.json](../../web/.prettierrc.json)

Revision: commit 1c4786e

## Role in the system

None of these ten files runs a workflow. They decide how the program is built, started, packaged, and kept clean. `go.mod` names the Go module and the two libraries the backend may import (a cron parser and a pure-Go SQLite driver). The `Makefile` is the developer's command list: install, build, test, format, run, and the Docker shortcuts. The `Dockerfile` turns the repository into one small image with the Go binary, the built frontend, and the starter workflows. `compose.yaml` runs that image with a loopback-only port, a read-only filesystem, a named volume for the SQLite file, and a health check. The two ignore files keep caches, databases, built assets, and secrets out of Git and out of the image. The four `web/` files configure the frontend toolchain: npm scripts and pinned packages, the Vite dev server and bundler, the TypeScript checker, and Prettier.

Data flows through them indirectly. A browser click starts in files that Vite compiled from `web/src/` into `web/dist/` (`web/package.json:8`, `web/vite.config.ts:6`). The Go server serves that directory from the `-web` flag whose default is `web/dist` (`cmd/server/main.go:31`, `internal/httpapi/api.go:122`). The click sends `POST /api/workflows/{id}/runs` through the relative path in `web/src/api.ts:66`. During development the Vite proxy forwards it to Go on port 8080 (`web/vite.config.ts:11`); in Docker the browser talks to the published port `127.0.0.1:8080` (`compose.yaml:6`). Go saves the run into the SQLite file from the `-db` flag, default `data/patchbay.db` (`cmd/server/main.go:32`). On the host that file is ignored by Git (`.gitignore:3`); in Docker it lives in the `data` volume mounted at `/app/data` (`compose.yaml:9`, `Dockerfile:21`).

The roadmap sets the frame for why these files look the way they do. `docs/roadmap.md` chose "Docker first; initially one application instance with persistent local storage", "Pin dependency versions when creating the skeleton", and "Build the React assets into the application image, include required certificates and timezone data, and keep application state on local persistent storage. Run with the minimum required privileges." Milestone 0 (the skeleton) is complete, and the README states that M1 already has SQLite history, bounded concurrent runs, TCP checks, and cron schedules, while SSH, scripts, Discord, and administrator login remain. Every hardening line below (non-root user, read-only filesystem, loopback port) exists because the README says the prototype has no login and must stay local until M1 authentication arrives.

## Concepts you need

- **Go modules (`go.mod` and `go.sum`).** A module is a tree of Go packages with one `go.mod` file at its root. The `module` line sets the import-path prefix for every package inside it, `go` sets the minimum Go version, and `require` lists dependencies with exact versions. `go.sum` stores cryptographic hashes so a later download of the same version must match. Docs: <https://go.dev/ref/mod>
- **Indirect dependencies.** A `// indirect` comment marks a module your code never imports directly but a dependency needs. Go records them so the build is reproducible without reading every dependency's own `go.mod` again. Docs: <https://go.dev/ref/mod#go-mod-file-require>
- **Build constraints (build tags).** A `//go:build` line at the top of a Go file tells the compiler to include that file only for matching operating systems or tags. Patchbay's database lock file compiles only on Linux and macOS, which the Docker build (Linux) satisfies. Docs: <https://pkg.go.dev/go/build#hdr-Build_Constraints>
- **Blank imports.** `import _ "pkg"` runs a package's `init` functions for their side effects without using any name from it. Patchbay uses two: one registers the SQLite driver, one embeds a time-zone database. Docs: <https://go.dev/doc/effective_go#blank_import>
- **cgo and `CGO_ENABLED=0`.** cgo lets Go call C code but then the binary depends on a C library at build and run time. With `CGO_ENABLED=0` the compiler produces a pure Go, statically linked binary that runs on a bare Alpine image. This only works because every dependency is pure Go. Docs: <https://pkg.go.dev/cmd/cgo>
- **Go's race detector (`go test -race`).** The `-race` flag instruments the test binary to detect two goroutines touching the same memory without synchronization. The roadmap requires it for admission and shutdown tests. Docs: <https://go.dev/doc/articles/race_detector>
- **GNU Make: targets, prerequisites, and `.PHONY`.** A rule `name: prerequisites` followed by tab-indented commands is a target. Make assumes a target is a file and skips it when a file of that name is newer than its prerequisites. `.PHONY` declares targets that are commands, not files, so they always run. Docs: <https://www.gnu.org/software/make/manual/html_node/Phony-Targets.html>
- **Make variable flavors and `export`.** `:=` assigns once, immediately. `?=` assigns only if the variable is not already set, and variables from the shell environment count as set. `export` passes the variable into the environment of each command Make runs. Docs: <https://www.gnu.org/software/make/manual/html_node/Flavors.html> and <https://www.gnu.org/software/make/manual/html_node/Variables_002fRecursion.html>
- **Docker multi-stage builds and layer caching.** Each `FROM` starts a new stage; `COPY --from=stage` copies files from an earlier stage into a later one, so build tools never ship in the final image. Each instruction produces a cached layer, and a change invalidates that layer and everything after it, which is why dependency manifests are copied before source code. Docs: <https://docs.docker.com/build/building/multi-stage/> and <https://docs.docker.com/reference/dockerfile/>
- **Build context and `.dockerignore`.** `docker build .` first uploads the directory (the context) to the builder. `.dockerignore` removes paths from that upload, so `COPY` never sees them and the upload stays small. Docs: <https://docs.docker.com/build/concepts/context/#dockerignore-files>
- **Docker Compose services, ports, volumes, and health checks.** A Compose file describes containers to run together. `ports` publishes a container port on the host, `volumes` attaches host directories or named volumes, and `healthcheck` runs a command inside the container until it succeeds. `docker compose up --wait` blocks until every service is healthy. Docs: <https://docs.docker.com/reference/compose-file/services/>
- **gitignore patterns.** A trailing `/` matches only directories, a leading `/` anchors the pattern to the repository root, and a pattern without `/` matches at any depth. Docs: <https://git-scm.com/docs/gitignore>
- **npm `package.json`, lockfile, and `npm ci`.** `package.json` lists scripts and the version of each dependency; `package-lock.json` records the exact resolved tree. `npm ci` installs exactly what the lockfile says, deletes any existing `node_modules` first, and fails if the lockfile and `package.json` disagree. Docs: <https://docs.npmjs.com/cli/v10/configuring-npm/package-json> and <https://docs.npmjs.com/cli/v10/commands/npm-ci>
- **ES modules in Node (`"type": "module"`).** This field makes Node treat `.js` files, and tools treat `.ts` config files, as ES modules that use `import` and `export` instead of `require`. Docs: <https://nodejs.org/api/packages.html#type>
- **Vite.** Vite is the frontend dev server and bundler. In development it serves source files on demand with hot module replacement; for production it bundles them into `dist/`. Its config can add plugins and proxy API paths to another server. Docs: <https://vite.dev/config/>
- **TypeScript compiler options.** `tsconfig.json` controls how `tsc` type-checks: the output language level (`target`), the ambient type libraries (`lib`), module resolution rules, JSX handling, and strictness. With `noEmit`, `tsc` only checks and Vite does the compiling. Docs: <https://www.typescriptlang.org/tsconfig/>
- **Tailwind CSS v4 (CSS-first configuration).** Tailwind generates utility classes from the class names it finds in your source. Version 4 is configured in CSS with `@import 'tailwindcss'` and `@theme`, not in a JavaScript config file; a Vite plugin runs it. Docs: <https://tailwindcss.com/docs/installation/using-vite> and <https://tailwindcss.com/docs/theme>
- **Prettier and the Tailwind class sorter.** Prettier rewrites code into one consistent style. The Tailwind plugin additionally sorts `className` strings into Tailwind's canonical order so diffs stay small. Docs: <https://prettier.io/docs/options> and <https://github.com/tailwindlabs/prettier-plugin-tailwindcss>
- **Playwright `webServer`.** Playwright can start your application before the browser tests and wait for a URL to respond. Patchbay's config starts the Go binary on port 18080, which is why the `e2e` Make target depends on `build`. Docs: <https://playwright.dev/docs/test-webserver>

## Walkthrough

### go.mod

**L1–L2** — `module patchbay` sets the import-path prefix for every package in this repository. That is why `cmd/server/main.go:19` imports `"patchbay/internal/engine"` and `internal/httpapi/api.go:15` imports `"patchbay/internal/engine"`. Renaming this line would break every internal import. There is no domain name in the path because the module is never published; a local name is enough for `go build` and `go test`.

**L3–L4** — `go 1.26.0` is the minimum Go version for this module. The compiler enables language features up to this version and refuses older toolchains. README says "Prerequisites: Go 1.26+". The Docker build uses `golang:1.26.4-alpine` (`Dockerfile:8`), which satisfies it. If this line named a version newer than the installed toolchain, the `go` command would try to download a newer toolchain, which fails in an offline image build.

**L5–L8** — The first `require` block lists the two libraries Patchbay imports directly. `github.com/robfig/cron/v3` is imported at `internal/workflow/schedule.go:8` and used at `internal/workflow/schedule.go:36` (`cron.ParseStandard`) to parse the `schedule.cron` field; the roadmap chose it for "parsing and time calculations" while "the application owns admission, overlap, and restart policy". `modernc.org/sqlite` is the pure-Go SQLite driver registered by the blank import at `internal/store/sqlite.go:13`; it is what makes `CGO_ENABLED=0` at `Dockerfile:13` possible (README: "supports the existing build with CGO disabled"). The versions are exact, following the roadmap rule to pin dependencies. Removing either line makes `go build` fail with a missing module error.

**L9–L20** — The second `require` block is maintained by `go mod tidy`, not by hand. Every entry is marked `// indirect`: nobody in `cmd/` or `internal/` imports them, but `modernc.org/sqlite` and its own dependencies need `modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory`, `github.com/google/uuid`, and the rest, while the cron library needs nothing extra. Go lists them so the build is reproducible without re-reading each dependency's `go.mod`. `go.sum` (copied at `Dockerfile:10`) holds the hash of every module here plus modules needed only to load the dependency graph, which is why `go.sum` has more lines than `go.mod`. If an entry here were deleted, `go build` would report an inconsistent module graph and ask you to run `go mod tidy`.

### Makefile

**L1–L1** — `export GOCACHE := $(CURDIR)/.cache/go-build` points the Go build cache at `.cache/go-build` inside the repository. `$(CURDIR)` is Make's variable for the directory Make was started in, so the path is absolute. `:=` assigns immediately and unconditionally, so a `GOCACHE` from the shell is overridden. `export` puts it in the environment of every command below, where the `go` tool reads it. README: "The Makefile keeps Go and npm caches in `.cache/`. This is local tooling state, excluded from Git and the Docker build" (`.gitignore:2`, `.dockerignore:2`). Without this line, builds would use the user's global cache, which works but mixes project state into the home directory.

**L2–L2** — `export npm_config_cache := $(CURDIR)/.cache/npm` does the same for npm. npm reads any environment variable named `npm_config_<option>` as the option `<option>`, so this sets npm's download cache to `.cache/npm`. Every `npm --prefix web ...` recipe below inherits it.

**L3–L3** — `export PLAYWRIGHT_BROWSERS_PATH ?= $(CURDIR)/.cache/playwright` tells Playwright where its downloaded browsers live. The `?=` is deliberate: README says "The Makefile defaults `PLAYWRIGHT_BROWSERS_PATH` to `.cache/playwright`; an explicit environment override is respected." The README install command sets the same variable by hand before `playwright install chromium`, and this line makes `make e2e` find that installation. If this used `:=`, a user with browsers elsewhere could not point Make at them.

**L4–L4** — Blank line.

**L5–L5** — `.PHONY: install build test e2e fmt api web up down` declares all nine targets as commands rather than files. This matters most for `web`: a directory named `web/` exists, so without this line `make web` would print "'web' is up to date" and never start Vite. It also means a stray file named `build` or `test` could never silence those targets.

**L6–L6** — Blank line.

**L7–L8** — `install` runs `npm --prefix web ci`. `--prefix web` makes npm act inside `web/` without a `cd`, and `ci` installs exactly `web/package-lock.json` (README: "Dependencies are pinned in `web/package-lock.json`"). It fails if the lockfile and `web/package.json` disagree, which protects the pinned versions. There is no Go install step because `go build` downloads modules itself.

**L9–L9** — Blank line.

**L10–L11** — `build` first runs `npm --prefix web run build`, the `build` script at `web/package.json:8` (`tsc --noEmit && vite build`). That type-checks the frontend and writes the bundle to `web/dist/`, the directory the Go server serves by default (`cmd/server/main.go:31`, `internal/httpapi/api.go:122`). Make stops here if the type check fails, because each recipe line must exit zero.

**L12–L13** — `mkdir -p bin` creates the output directory (ignored by Git at `.gitignore:4`), then `go build -o bin/patchbay ./cmd/server` compiles the server. README shows `make build` followed by `./bin/patchbay`. The Playwright config executes exactly this binary at `web/playwright.config.ts:12` (`exec ../bin/patchbay ...`). The frontend is built first so that a fresh checkout ends with both halves in place.

**L14–L14** — Blank line.

**L15–L17** — `test` runs `go test -race ./...` and then `npm --prefix web run typecheck`. AGENTS.md: "`make test`: Go race tests and TypeScript checking." `./...` means every package under the module, and `-race` is the race detector the roadmap requires for "concurrent admission, history reads, and shutdown". The second line runs `tsc --noEmit` (`web/package.json:9`) over everything in `web/tsconfig.json:14`, which includes the Playwright tests and both config files. Browser tests are not part of `test`; they are `e2e`. If the Go tests fail, the type check never runs.

**L18–L18** — Blank line.

**L19–L20** — `e2e: build` is the only target with a prerequisite. It must be: `web/playwright.config.ts:9-14` starts `../bin/patchbay -addr 127.0.0.1:18080 -workflows tests/fixtures/workflows -web dist`, so both `bin/patchbay` and `web/dist/` must exist. The recipe runs the `test:e2e` script (`web/package.json:11`). AGENTS.md: "`make e2e`: builds and runs Playwright; it starts an isolated local app/database." Dropping the prerequisite would make a fresh checkout fail with "no such file" when Playwright tries to start the server.

**L21–L21** — Blank line.

**L22–L24** — `fmt` runs `gofmt -w cmd internal` (rewrite every Go file in place) and `npm --prefix web run format` (`web/package.json:12`). AGENTS.md: "Format only touched files with `gofmt` and the project's Prettier configuration. `make fmt` formats broadly, so avoid it for a narrow change." So this target exists for whole-repository passes, not for everyday edits.

**L25–L25** — Blank line.

**L26–L27** — `api` runs `go run ./cmd/server` with every flag at its default: listen on `127.0.0.1:8080`, load `workflows/`, serve `web/dist`, open `data/patchbay.db` (`cmd/server/main.go:29-32`). README: "`make api` Terminal 1: Go API on 127.0.0.1:8080; loads workflows/". `go run` compiles to a temporary location, so `bin/` is untouched.

**L28–L28** — Blank line.

**L29–L30** — `web` runs the `dev` script (`web/package.json:7`), which starts Vite on `127.0.0.1:5173` with the `/api` proxy from `web/vite.config.ts:7-12`. README: "`make web` Terminal 2: React/Vite on http://127.0.0.1:5173". This is the target that `.PHONY` at L5 rescues from the `web/` directory.

**L31–L31** — Blank line.

**L32–L34** — `up` runs `docker compose up --build --wait` and then `docker ps`. `--build` rebuilds the image from `Dockerfile` every time, so edited Go or frontend code is included. `--wait` returns only when the `healthcheck` at `compose.yaml:13-17` reports healthy, so the following `docker ps` shows a running, healthy `app`. README's Docker section documents the same command.

**L35–L35** — Blank line.

**L36–L38** — `down` runs `docker compose down --remove-orphans` and `docker ps`. README: "To clean up containers left by an older setup, run `make down` ... It also removes orphaned containers belonging to this Compose project. Stopping or recreating the app preserves saved run history." The named volume survives because `down` without `-v` never deletes volumes.

**L39–L39** — Blank line.

**L40–L46** — A commented-out `cleanup` recipe listing what a full reset would delete: `.cache`, `bin`, `web/dist`, `web/node_modules`, `web/test-results`, and the `patchbay_data` volume. It is commented out, so it is not a target and is not listed in `.PHONY`; its last line would destroy run history (README: "Deleting the volume deletes that history"). The volume name shows how Compose names the `data` volume from `compose.yaml:19-20`: project name (the directory, `patchbay`) plus underscore plus volume key.

### Dockerfile

**L1–L1** — `FROM node:24-alpine AS frontend` starts the first stage on a small Alpine image with Node 24. Node 24 satisfies the `engines` range at `web/package.json:34` (`>=22.12.0`); README calls Node 24 LTS "a suitable choice". The stage is named `frontend` so L19 can copy out of it.

**L2–L2** — `WORKDIR /src/web` creates and enters the directory for the frontend build. All later relative paths in this stage, and the `dist/` output, live under it (`/src/web/dist` is referenced at L19).

**L3–L3** — `COPY web/package*.json ./` copies only `package.json` and `package-lock.json`. This is the layer-caching trick: dependency installation at L4 is cached until one of these two files changes, so editing a component does not reinstall packages.

**L4–L4** — `RUN npm ci` installs the exact lockfile tree, including `devDependencies` (`web/package.json:21-32`), because `typescript`, `vite`, and `tailwindcss` are needed to build. This is the same command as `make install` (`Makefile:8`). It fails the image build if the lockfile is out of sync with `package.json`.

**L5–L5** — `COPY web/ ./` copies the frontend source, `index.html`, `tsconfig.json`, `vite.config.ts`, `playwright.config.ts`, and `tests/`. It does not copy `web/node_modules` or `web/dist`, because `.dockerignore:5-6` excludes them. That exclusion is what keeps the fresh Linux `node_modules` from L4 from being overwritten by a host copy built for another operating system.

**L6–L6** — `RUN npm run build` runs `tsc --noEmit && vite build` (`web/package.json:8`). `tsc` checks `src`, `tests`, and both config files (`web/tsconfig.json:14`), which is why `@playwright/test` had to be installed at L4. `vite build` writes `/src/web/dist/`. A type error anywhere fails the image build, the same way it fails `make build`.

**L7–L7** — Blank line.

**L8–L8** — `FROM golang:1.26.4-alpine AS backend` starts the Go build stage. The toolchain version is at least `go 1.26.0` from `go.mod:3`, so no toolchain download is needed. Alpine's Go image ships no C compiler, which is fine because L13 disables cgo.

**L9–L9** — `WORKDIR /src` sets the module root inside the image. The `./cmd/server` path at L13 is relative to it.

**L10–L10** — `COPY go.mod go.sum ./` copies the module manifest and the checksum file. `go build` at L13 reads `go.mod` to know what to download and verifies each download against `go.sum`. A `go.sum` entry missing for a required module fails the build with "missing go.sum entry". There is no separate `go mod download` step, so modules are fetched inside L13; README notes "The initial image build downloads dependencies".

**L11–L12** — `COPY cmd/server/ ./cmd/server/` and `COPY internal/ ./internal/` copy exactly the two Go source trees. `web/`, `docs/`, `examples/`, and `workflows/` are not needed to compile. The copy includes `cmd/server/database_lock.go`, whose `//go:build darwin || linux` tag at `cmd/server/database_lock.go:1` matches the Linux build. If a future Go package were placed outside these two directories, the native build would work but this image build would fail with an unknown import.

**L13–L13** — `RUN CGO_ENABLED=0 go build -trimpath -o /out/patchbay ./cmd/server` compiles the server. `CGO_ENABLED=0` produces a statically linked binary with no libc dependency, so it runs on plain Alpine at L15; this is possible only because `modernc.org/sqlite` is pure Go (`internal/store/sqlite.go:13`, `go.mod:7`). `-trimpath` strips the build machine's paths (such as `/src`) from the binary so builds are reproducible and leak no layout. The output goes to `/out/patchbay`, which L18 copies.

**L14–L14** — Blank line.

**L15–L15** — `FROM alpine:3.23` starts the final runtime stage from a minimal base. Neither Node nor the Go toolchain ships in the image; only what the next lines copy in. This is the stage `compose.yaml:4` tags as `patchbay:m0`.

**L16–L16** — `RUN apk add --no-cache ca-certificates tzdata` installs two runtime data packages. `ca-certificates` gives Go's TLS client the root certificates it needs: the HTTP executor clones Go's default transport and keeps "Go's connection, TLS, and HTTP/2 defaults" (`internal/nodes/httpcheck/http.go:23-25`), and README says "normal HTTPS certificate validation remains enabled", so without this package every `https://` check would report a certificate error. `tzdata` supplies IANA time zones for the `schedule.timezone` field; README: "The Docker image installs `tzdata`, and the Go binary embeds a fallback zone database for hosts without one" (`cmd/server/main.go:16-17`). The roadmap asked for exactly this: "include required certificates and timezone data". `--no-cache` leaves no package index behind.

**L17–L17** — `WORKDIR /app` is the directory the server runs in. It matters because the three path flags default to relative paths: `workflows` (`cmd/server/main.go:30`), `web/dist` (`cmd/server/main.go:31`), and `data/patchbay.db` (`cmd/server/main.go:32`) all resolve under `/app`. L18 to L21 lay the files out to match.

**L18–L18** — `COPY --from=backend /out/ /app/` copies the compiled binary from the Go stage, so it lands at `/app/patchbay`, the path L24 executes.

**L19–L19** — `COPY --from=frontend /src/web/dist/ /app/web/dist/` copies the Vite bundle to `/app/web/dist`, which is exactly the `-web` default. The Go file server at `internal/httpapi/api.go:122` serves it, and `internal/httpapi/api.go:129-131` returns 503 "Frontend is not built" if `index.html` is missing. README: "The image contains its frontend assets and needs no CDN at runtime."

**L20–L20** — `COPY workflows/ /app/workflows/` bakes the repository's workflow files into the image, matching the `-workflows` default. `workflow.Load` reads this directory at startup (`cmd/server/main.go:58`) and refuses to start if it is empty or invalid. In Compose this directory is covered by a read-only bind mount (`compose.yaml:8`), so the image copy is the fallback for running the image without Compose.

**L21–L21** — `RUN mkdir -m 0700 /app/data && chown 10001:10001 /app/data` creates the database directory before the user switch. Mode `0700` matches `os.MkdirAll(filepath.Dir(dbPath), 0o700)` at `cmd/server/main.go:62` and the AGENTS.md rule to keep database files private. The `chown` gives ownership to the user set at L22, so that user can create `patchbay.db` with `os.OpenFile(..., 0o600)` at `internal/store/sqlite.go:34`. When Compose first creates the `data` volume for this mount point, Docker initializes the volume from this directory, including its ownership; without this line the volume would be root-owned and the non-root server would fail with a permission error while creating the database file.

**L22–L22** — `USER 10001:10001` switches every later instruction and the running container to a numeric non-root user and group. README: "The app uses a non-root container user." The number exists in no `/etc/passwd`, which is fine for a static binary. Together with `read_only` and `cap_drop` in `compose.yaml:11-12`, this upholds the roadmap rule "Run with the minimum required privileges."

**L23–L23** — `EXPOSE 8080` documents the port the server listens on. It publishes nothing by itself; `compose.yaml:5-6` does the actual publishing. Tools and readers use it to know which port to map.

**L24–L24** — `CMD ["/app/patchbay", "-addr", "0.0.0.0:8080"]` starts the server in exec form (no shell, so signals reach the process and `SIGTERM` triggers the `signal.NotifyContext` at `cmd/server/main.go:38`). `-addr 0.0.0.0:8080` overrides the native default `127.0.0.1:8080` (`cmd/server/main.go:29`) because inside a container the loopback address is reachable only from that container; the published port and the health check at `compose.yaml:14` both need the server on all container interfaces. README: "`-addr` controls where the server listens; it does not add allowed hosts, and binding to `0.0.0.0` or `::` does not permit every hostname." The host guard still only accepts `localhost`, `127.0.0.1`, and `::1` (`internal/httpapi/host_guard.go:22`). README shows how to replace this command in Compose to pass `-max-active-runs`.

### compose.yaml

**L1–L2** — `services:` opens the list of containers; `app:` names the only one. That name is what README's `docker compose logs -f app` and `docker compose restart app` refer to, and it is the prefix in the container name.

**L3–L3** — `build: .` tells Compose to build the image from the `Dockerfile` in the repository root with the root as build context. That is why `.dockerignore` applies, and why `make up` passes `--build` (`Makefile:33`).

**L4–L4** — `image: patchbay:m0` names and tags the built image. Without it Compose would invent a name from the project and service. The `m0` tag is a milestone label, not a version the code reads.

**L5–L6** — `ports: - "127.0.0.1:8080:8080"` publishes container port 8080 on the host's loopback address only. README: "The published port binds to loopback. This M0 application has no login; keep it local until administrator authentication is added in M1." Writing `8080:8080` instead would expose the unauthenticated prototype to the LAN. The string is quoted because YAML could otherwise misread a value with colons. The browser then sends `Host: localhost:8080`, which the host guard accepts (`internal/httpapi/host_guard_test.go:22`).

**L7–L7** — `volumes:` opens the mounts for this service.

**L8–L8** — `./workflows:/app/workflows:ro` bind-mounts the host's `workflows/` over the copy baked in at `Dockerfile:20`, read-only. This is what makes README's "Reload edited workflow files" work: edit on the host, `docker compose restart app`, and `workflow.Load` (`cmd/server/main.go:58`) reads the new files at startup. `:ro` keeps the container from ever changing definitions, in line with the README statement that the backend has no workflow authoring yet.

**L9–L9** — `data:/app/data` mounts the named volume declared at L19-20 on the database directory from `Dockerfile:21`. README: "Compose mounts a writable named volume at `/app/data` for SQLite. ... `make down` preserves this volume and its saved history." This is the only writable path once L11 makes the rest read-only. Native development uses `data/patchbay.db` on the host instead, so the two installations have separate histories (README).

**L10–L10** — `stop_grace_period: 20s` gives the process 20 seconds between `SIGTERM` and `SIGKILL`, double Compose's 10-second default. README: "Compose allows 20 seconds for graceful shutdown, including database cleanup and HTTP shutdown." The budget covers the shutdown sequence at `cmd/server/main.go:137-145`: the runner allows five seconds per active run's final save (`internal/engine/runner.go:322`) and one shared five-second budget for queued cleanup (`internal/engine/runner.go:203`), then HTTP shutdown gets five more seconds (`cmd/server/main.go:141`). The runner's budgets overlap (each active run saves in its own goroutine while queued cleanup runs on the closing goroutine), so `runner.Close` takes up to about five seconds and `server.Shutdown` up to five more, before `db.Close` runs. With the default 10 seconds, `SIGKILL` could land before SQLite closed, and a save still in flight would leave records for startup to mark interrupted.

**L11–L11** — `read_only: true` mounts the container's root filesystem read-only. The binary, the frontend, and the workflow copy cannot be modified at runtime; only the volume at L9 accepts writes. README: "The rest of the container filesystem remains read-only." If the database directory had not been created in the image at `Dockerfile:21`, `os.MkdirAll` at `cmd/server/main.go:62` would fail here.

**L12–L12** — `cap_drop: [ALL]` removes every Linux capability from the container process. The server needs none: it binds port 8080 (above 1024), runs as user 10001, and only reads and writes its own files. This is the Compose half of "minimum required privileges".

**L13–L14** — `healthcheck:` with `test: [CMD, wget, -q, -O, "-", "http://127.0.0.1:8080/api/health"]` runs Alpine's BusyBox `wget` inside the container against the readiness route registered at `internal/httpapi/api.go:34-36`. `-q` silences output and `-O -` writes the body to stdout; a non-2xx response makes `wget` exit non-zero, which Docker counts as a failed probe. Inside the container `127.0.0.1` is the container's own loopback, reachable because L24 of the Dockerfile binds `0.0.0.0`. The request carries `Host: 127.0.0.1:8080`, which the host guard allows (`internal/httpapi/host_guard_test.go:23` is named "Docker health check"). README stresses that `/api/health` reports "App readiness, not monitored-service health".

**L15–L15** — `interval: 5s` runs the probe every five seconds.

**L16–L16** — `timeout: 2s` counts a probe that takes longer than two seconds as failed. The health handler does no I/O, so two seconds is generous.

**L17–L17** — `retries: 5` marks the container unhealthy only after five consecutive failures. `docker compose up --wait` (`Makefile:33`) returns once the first probe succeeds; the retry count matters for the unhealthy transition, not the healthy one.

**L18–L18** — Blank line.

**L19–L20** — The top-level `volumes: data:` declares the named volume used at L9. Compose names it `patchbay_data` (project name plus key), which is the name in the commented cleanup recipe at `Makefile:46`. Declaring it here, instead of using a bind mount, keeps the SQLite file on Docker's local storage, which README requires: "Keep the database on a local filesystem (including Docker's local named volume)".

### .gitignore

**L1–L1** — `.DS_Store` ignores the metadata files macOS Finder drops into every browsed directory. The roadmap lists "Development: Linux and macOS", so these appear on half the development machines.

**L2–L2** — `.cache/` ignores the directory where `Makefile:1-3` puts the Go, npm, and Playwright caches and where `web/playwright.config.ts:10-11` creates `.cache/e2e/run.XXXXXX/` database directories for each browser-test run. AGENTS.md: "Do not commit caches".

**L3–L3** — `/data/` ignores the default database directory from `cmd/server/main.go:32`. The leading `/` anchors it to the repository root, so a `data` directory elsewhere would still be tracked; the trailing `/` matches only a directory. AGENTS.md forbids committing databases, and the file would also contain whatever URLs and results a user has run.

**L4–L4** — `bin/` ignores the compiled server from `Makefile:12-13`.

**L5–L8** — Four frontend outputs: `web/node_modules/` (installed by `npm ci`), `web/dist/` (written by `vite build`), `web/test-results/` (Playwright screenshots; README: "Screenshots go to `web/test-results/`"), and `web/playwright-report/` (Playwright's HTML report directory). AGENTS.md: "Do not commit ... built assets, test output".

**L9–L9** — `*.log` ignores log files anywhere in the tree. The server logs to stderr, so this is a guard against redirected output.

**L10–L10** — `.env` ignores a local environment file. AGENTS.md: "Do not commit ... credentials, or local environment files." Nothing in the code reads such a file today; the rule is preventive.

### .dockerignore

**L1–L1** — `.git` keeps the repository history out of the build context. No `COPY` instruction references it, but excluding it makes the context upload much smaller and ensures no history can leak into a layer.

**L2–L2** — `.cache` excludes the Make caches and the Playwright e2e databases, mirroring `.gitignore:2`. README: "local tooling state, excluded from Git and the Docker build."

**L3–L3** — `data/` excludes the native development database. `.dockerignore` patterns are matched from the context root, so no leading slash is needed. This guarantees a developer's `data/patchbay.db` can never end up inside an image.

**L4–L4** — `bin` excludes the native binary. The image builds its own at `Dockerfile:13`; copying a host binary built for another OS would be useless.

**L5–L8** — `web/node_modules`, `web/dist`, `web/test-results`, and `web/playwright-report` are excluded so that `COPY web/ ./` at `Dockerfile:5` brings only source. This is the load-bearing part of the file: `node_modules` from the host may contain binaries for macOS, and a stale `dist` could shadow the fresh build. The `.dockerignore` rules make `Dockerfile:4` and `Dockerfile:6` the only sources of those directories.

**L9–L10** — `*.log` and `.env` mirror `.gitignore:9-10` so logs and local secrets never enter the context.

### web/package.json

**L1–L1** — Opening brace of the JSON object.

**L2–L4** — `"name": "patchbay-web"` is the package name (also recorded in `package-lock.json`). `"private": true` makes npm refuse to publish this package, which is correct for an application. `"version": "0.0.1"` is informational; nothing in the app reads it.

**L5–L5** — `"type": "module"` makes Node and the tools treat the package's files as ES modules. This is why `web/vite.config.ts:1-5` and `web/playwright.config.ts:1-3` can use `import` and `export default` directly. Removing it would make Vite and Playwright fall back to CommonJS handling of their config files and could break their loading.

**L6–L6** — `"scripts"` opens the command aliases that `npm run <name>` executes with `node_modules/.bin` on the `PATH`.

**L7–L7** — `"dev": "vite"` starts the dev server configured at `web/vite.config.ts:7-12`. `Makefile:30` calls it.

**L8–L8** — `"build": "tsc --noEmit && vite build"` type-checks first and bundles only if the check passes; the `&&` is what makes a type error stop the build. `Makefile:11` and `Dockerfile:6` both call it. Output goes to `web/dist/` because `vite build` writes to `dist` under the project root by default.

**L9–L9** — `"typecheck": "tsc --noEmit"` is the first half of `build` on its own. `Makefile:17` calls it as the frontend part of `make test`.

**L10–L10** — `"preview": "vite preview"` serves the built `dist/` with Vite for a local look at the production bundle. No Make target uses it; README documents running the built frontend through Go instead.

**L11–L11** — `"test:e2e": "playwright test"` runs the browser tests using `web/playwright.config.ts`. `Makefile:20` calls it after `build`.

**L12–L12** — `"format": "prettier --write src tests index.html *.json *.ts"` rewrites the frontend sources with the options in `web/.prettierrc.json`. npm runs the script through a shell, so the shell expands `*.json` to `package.json`, `package-lock.json`, and `tsconfig.json`, and `*.ts` to `playwright.config.ts` and `vite.config.ts`; the dotfile `.prettierrc.json` is not matched. `Makefile:24` calls it.

**L13–L13** — Closing brace of `scripts`.

**L14–L14** — `"dependencies"` opens the packages shipped in the bundle. Every version is exact, with no `^` or `~`, following the roadmap rule "Pin dependency versions when creating the skeleton" and README's "Dependencies are pinned".

**L15–L15** — `@tanstack/react-query` provides the polling queries. `web/src/main.tsx:3` imports `QueryClientProvider`, `web/src/main.tsx:11` wraps the app in it, and `web/src/queryClient.ts` sets the defaults (README: "refreshes the workflow and run lists once per second").

**L16–L16** — `@tanstack/react-router` provides the hash-based routes. `web/src/main.tsx:4` imports `RouterProvider`, `web/src/main.tsx:12` renders it, and `web/src/router.tsx:54` calls `createHashHistory()`, which is why the Go file server needs no fallback route for deep links.

**L17–L17** — `@xyflow/react` is React Flow, the canvas library. `web/src/components/CanvasNode.tsx:2` imports its node types, and `web/src/index.css:3` imports its stylesheet into the `base` cascade layer.

**L18–L19** — `react` and `react-dom` at matching versions. `web/src/main.tsx:1-2` imports `StrictMode` and `createRoot` from them.

**L20–L20** — Closing brace of `dependencies`.

**L21–L21** — `"devDependencies"` opens the build-time packages. `npm ci` installs them too, which `Dockerfile:4` relies on.

**L22–L22** — `@playwright/test` is the browser test runner imported at `web/playwright.config.ts:1` and `web/tests/workflows.spec.ts:1`. It is a devDependency because tests never ship in the bundle, but `tsc` still needs its types because `web/tsconfig.json:14` includes `tests`.

**L23–L23** — `@tailwindcss/vite` is the Vite plugin registered at `web/vite.config.ts:3,6` that compiles `@import 'tailwindcss'` in `web/src/index.css:1`.

**L24–L25** — `@types/react` and `@types/react-dom` give TypeScript the React typings. Without them every JSX element and hook would be an error under `strict`.

**L26–L26** — `@vitejs/plugin-react` is registered at `web/vite.config.ts:2,6`. It applies the automatic JSX runtime and Fast Refresh (README: "Frontend edits update automatically").

**L27–L27** — `prettier` is the formatter run by the `format` script at L12.

**L28–L28** — `prettier-plugin-tailwindcss` is loaded by `web/.prettierrc.json:4` to sort Tailwind classes.

**L29–L29** — `tailwindcss` is the Tailwind core that the Vite plugin at L23 drives. It is pinned to the same version as the plugin.

**L30–L30** — `typescript` provides the `tsc` command used by the `build` and `typecheck` scripts at L8-9.

**L31–L31** — `vite` is the dev server and bundler used by the `dev`, `build`, and `preview` scripts.

**L32–L32** — Closing brace of `devDependencies`.

**L33–L35** — `"engines": { "node": "^20.19.0 || >=22.12.0" }` records the supported Node versions. README: "Node 20.19+ or 22.12+". The range is Vite's own requirement, so an older Node would fail inside Vite anyway; npm only warns about `engines` unless `engine-strict` is set. The Docker build uses Node 24 (`Dockerfile:1`), inside the range.

**L36–L36** — Closing brace of the file.

### web/vite.config.ts

**L1–L1** — `import { defineConfig } from 'vite';` imports a helper that adds types and editor completion to the config object. It changes nothing at runtime.

**L2–L2** — `import react from '@vitejs/plugin-react';` imports the React plugin from the devDependency at `web/package.json:26`.

**L3–L3** — `import tailwindcss from '@tailwindcss/vite';` imports the Tailwind plugin from `web/package.json:23`.

**L4–L4** — Blank line.

**L5–L5** — `export default defineConfig({` exports the config as the module default. This is an ES module export, which works because of `"type": "module"` at `web/package.json:5`.

**L6–L6** — `plugins: [react(), tailwindcss()],` activates both plugins. `react()` compiles JSX using the automatic runtime chosen by `web/tsconfig.json:7` and enables Fast Refresh. `tailwindcss()` scans the source for class names and generates CSS for `@import 'tailwindcss'` at `web/src/index.css:1`. Without the Tailwind plugin the import would fail to resolve and the page would render unstyled.

**L7–L7** — `server: {` opens the dev-server options used by `npm run dev` (`Makefile:30`).

**L8–L8** — `host: '127.0.0.1',` binds the dev server to loopback only. This mirrors the Go default at `cmd/server/main.go:29` and the AGENTS.md rule to keep the unauthenticated prototype local. The proxy at L11 also keeps the browser's Host header, `127.0.0.1:5173`, which the Go host guard accepts (`internal/httpapi/host_guard_test.go:24`).

**L9–L9** — `port: 5173,` is the port README names: "Visit http://127.0.0.1:5173 during frontend development."

**L10–L10** — `strictPort: true,` makes Vite exit if 5173 is taken instead of silently picking 5174. The documented URL and the host-guard test case stay correct, and a developer never ends up with two Vite instances.

**L11–L11** — `proxy: { '/api': 'http://127.0.0.1:8080' },` forwards every request whose path starts with `/api` to the Go server from `make api`. `web/src/api.ts:66` always fetches `/api${path}` with a relative URL, so in development the browser asks Vite and Vite asks Go. README: "Vite proxies `/api` requests to Go." Without this line, Vite would answer `/api/...` with `index.html`, and `web/src/api.ts:75-76` would throw "The backend is unavailable". The proxy does not rewrite the Host header, which is why the guard allows the Vite port.

**L12–L12** — Closing brace of `server`.

**L13–L13** — `build: { chunkSizeWarningLimit: 1024 },` raises the bundle-size warning threshold from Vite's 500 kB default to 1024 kB. React Flow, the router, and the query library together exceed 500 kB, so the default would print a warning on every `vite build`. This changes only the warning, not the output.

**L14–L14** — Closing of `defineConfig({ ... })` and the statement.

### web/tsconfig.json

**L1–L2** — Opening brace and `"compilerOptions"`, the block `tsc` reads for every file in `include`.

**L3–L3** — `"target": "ES2022"` sets the language level `tsc` assumes for output and for checking features such as class fields and top-level `await`. Vite does its own transform for the bundle, but `tsc` still needs a target to type-check. README's browser floor for Tailwind v4 (Safari 16.4, Chrome 111, Firefox 128) supports ES2022.

**L4–L4** — `"lib": ["ES2022", "DOM", "DOM.Iterable"]` lists the ambient type libraries: the ES2022 built-ins, the browser DOM (`document.getElementById` at `web/src/main.tsx:9`, `fetch` at `web/src/api.ts:66`), and iterable DOM collections. Removing `DOM` would make every browser API an unknown name.

**L5–L5** — `"module": "ESNext"` keeps `import`/`export` syntax in the checked output model instead of converting to CommonJS. Vite expects ES modules.

**L6–L6** — `"moduleResolution": "Bundler"` resolves imports the way Vite does: package `exports` maps are honored and relative imports need no file extension (`'./queryClient'` at `web/src/main.tsx:5`). Any other setting would report missing modules for exactly the imports Vite resolves fine.

**L7–L7** — `"jsx": "react-jsx"` selects React's automatic JSX runtime, so no file needs `import React`. No file under `web/src` has such an import; `web/src/main.tsx` imports only `StrictMode`. This must agree with the plugin at `web/vite.config.ts:6`, which uses the same runtime.

**L8–L8** — `"strict": true` turns on all strict checks (strict null checks, no implicit `any`, and more). It is why `web/src/api.ts:57-58` and `web/src/api.ts:60-61` mark optional fields with `?` and why the step and run types are explicit, as AGENTS.md asks for "Explicit TypeScript API types".

**L9–L9** — `"skipLibCheck": true` skips type-checking `.d.ts` files inside `node_modules`. It speeds up `tsc` and avoids failures caused by conflicts between library typings that the project cannot fix.

**L10–L10** — `"noEmit": true` makes `tsc` a checker only; Vite emits the JavaScript. The scripts at `web/package.json:8-9` also pass `--noEmit`, so even an editor running `tsc` with this config never writes stray `.js` files next to the sources.

**L11–L11** — `"verbatimModuleSyntax": true` requires type-only imports to be written as `import type`, for example `web/src/components/ExecutionHistory.tsx:1` and `web/tests/workflows.spec.ts:2`. Vite compiles each file in isolation and cannot know whether an imported name is a type, so a plain import of a type would survive into the bundle and fail at runtime. This option makes `tsc` report that mistake.

**L12–L12** — `"types": ["vite/client"]` loads Vite's ambient declarations (`import.meta.env`, `import.meta.hot`, and module declarations for `*.css` and image imports) and limits automatic `@types` inclusion to this one package. No file under `web/src` uses `import.meta` today, so this is the standard Vite setting kept ready rather than a line something depends on now.

**L13–L13** — Closing brace of `compilerOptions`.

**L14–L14** — `"include": ["src", "vite.config.ts", "playwright.config.ts", "tests"]` lists what `tsc` checks. README: "TypeScript checking (app, browser tests, configs)". Including `tests` is why `@playwright/test` must be installed before `tsc` runs (`Dockerfile:4`), and including both config files catches typos in them before Vite or Playwright would.

**L15–L15** — Closing brace of the file.

### web/.prettierrc.json

**L1–L1** — Opening brace. Prettier finds this file automatically when run from `web/` by the `format` script at `web/package.json:12`.

**L2–L2** — `"singleQuote": true` makes Prettier use single quotes for strings. Every import in the frontend follows it, for example `web/src/main.tsx:1-7` and `web/vite.config.ts:1-3`.

**L3–L3** — `"printWidth": 100` wraps lines at 100 characters instead of Prettier's 80. The long `className` strings and the API types fit on one line more often.

**L4–L4** — `"plugins": ["prettier-plugin-tailwindcss"]` loads the class sorter from `web/package.json:28`. It reorders Tailwind classes inside `className` strings into Tailwind's recommended order, so two developers writing the same classes produce the same text. AGENTS.md notes that `web/src/components/classes.ts` holds the shared utility strings this plugin keeps tidy.

**L5–L5** — `"tailwindStylesheet": "./src/index.css"` tells the plugin where the Tailwind v4 CSS entry is. Tailwind v4 has no `tailwind.config.js`; the project palette and sizes are declared with `@theme` at `web/src/index.css:5-71`. The plugin reads that file so it recognizes project-specific classes such as `bg-canvas` or `text-2xs` as Tailwind classes and sorts them correctly. The path is relative to this config file.

**L6–L6** — Closing brace of the file.

## How it is used

| File or field | Read by | What changes when you edit it |
| --- | --- | --- |
| `go.mod:1` `module patchbay` | Every `go build`, `go test`, and `go run` | Every `patchbay/...` import path (`cmd/server/main.go:19-25`) must change with it |
| `go.mod:3` `go 1.26.0` | The Go toolchain on the host and in `Dockerfile:8` | A newer version than the installed toolchain triggers a toolchain download or a build failure |
| `go.mod:6-7` direct requires | `internal/workflow/schedule.go:8`, `internal/store/sqlite.go:13` | Changing a version changes which library code is compiled; `go.sum` must be updated with `go mod tidy` |
| `Makefile:1-3` exports | `go`, `npm`, and Playwright as environment variables | Moves caches and browsers; `?=` on line 3 lets the shell override |
| `Makefile:10-13` `build` | Developer handoff, `make e2e` via the prerequisite at line 19 | Produces `web/dist/` and `bin/patchbay`, the paths `web/playwright.config.ts:12` runs |
| `Makefile:15-17` `test` | Developer handoff (AGENTS.md verification commands) | Which Go packages and TypeScript files are checked |
| `Makefile:26-30` `api`, `web` | Native development | Native server flags (none are passed, so `cmd/server/main.go:29-35` defaults apply) |
| `Makefile:32-38` `up`, `down` | Docker development | Whether the image is rebuilt and whether orphans are removed |
| `Dockerfile:1-6` frontend stage | `docker compose up --build` via `compose.yaml:3` | Node version and how `web/dist` is produced inside the image |
| `Dockerfile:8-13` backend stage | Same | Go version, cgo setting, binary flags; which Go source directories are compiled |
| `Dockerfile:15-24` runtime stage | Same, and `compose.yaml:4` for the tag | Runtime packages, file layout under `/app`, user, and the `-addr` the server binds |
| `Dockerfile:24` `CMD` | The container at start | Server flags; README shows overriding it with `command:` in Compose for `-max-active-runs` |
| `compose.yaml:6` ports | Docker's port publisher | Which host address and port reach the server; `127.0.0.1` keeps it loopback-only |
| `compose.yaml:8` workflows mount | Container filesystem; read by `workflow.Load` at `cmd/server/main.go:58` on each start | Which workflow files the container loads; `:ro` keeps it read-only |
| `compose.yaml:9,19-20` data volume | `store.Open` at `cmd/server/main.go:75` through `/app/data/patchbay.db` | Where history lives; deleting the volume deletes history |
| `compose.yaml:10` `stop_grace_period` | Docker on `docker compose down` or `stop` | How long the shutdown at `cmd/server/main.go:137-145` may take before `SIGKILL` |
| `compose.yaml:11-12` `read_only`, `cap_drop` | The container runtime | Which paths and privileges the process has |
| `compose.yaml:13-17` healthcheck | Docker, and `make up` through `--wait` | Probe route (`internal/httpapi/api.go:34`), cadence, and the unhealthy threshold |
| `.gitignore` | `git status`, `git add` | Which generated files Git offers to commit |
| `.dockerignore` | `docker build` context upload | What `COPY web/ ./` and `COPY workflows/` can see |
| `web/package.json:7-12` scripts | `Makefile:11,17,20,24,30` and `Dockerfile:6` | The exact commands behind each Make target |
| `web/package.json:15-19` dependencies | `web/src/main.tsx:1-4`, `web/src/router.tsx`, `web/src/components/*.tsx`, `web/src/index.css:3` | Library versions in the shipped bundle; `package-lock.json` must be regenerated |
| `web/package.json:22-31` devDependencies | `web/vite.config.ts:2-3`, `web/playwright.config.ts:1`, `web/.prettierrc.json:4`, `tsc` | Toolchain versions; `npm ci` fails until the lockfile matches |
| `web/package.json:34` engines | npm (warning only) | The documented Node range; Vite enforces the real floor |
| `web/vite.config.ts:6` plugins | `vite` in `dev` and `build` | JSX compilation, Fast Refresh, and Tailwind generation |
| `web/vite.config.ts:8-11` server | `vite` in `dev` only | Dev URL, port behavior, and where `/api` is forwarded (`web/src/api.ts:66`) |
| `web/vite.config.ts:13` build | `vite build` only | The size at which a chunk warning prints |
| `web/tsconfig.json:3-12` compilerOptions | `tsc --noEmit` in `build` and `typecheck`; editors | Strictness, resolution, JSX runtime; must agree with `web/vite.config.ts:6` |
| `web/tsconfig.json:14` include | `tsc` | Which directories are type-checked; dropping `tests` would hide test type errors |
| `web/.prettierrc.json` | `prettier` in the `format` script | Quote style, line width, class sorting, and which stylesheet defines the theme |

This chapter covers no workflow JSON files; their field-to-struct mapping is in Chapter 2.

## Check yourself

1. `make e2e` lists `build` as a prerequisite, but `make test` does not. Why is that the right split?

<details><summary>Answer</summary>
`web/playwright.config.ts:12` executes <code>../bin/patchbay -addr 127.0.0.1:18080 -workflows tests/fixtures/workflows -web dist</code>. Both <code>bin/patchbay</code> and <code>web/dist</code> are outputs of <code>make build</code> (<code>Makefile:11-13</code>), so without the prerequisite a fresh checkout would fail to start the test server. <code>make test</code> runs <code>go test -race ./...</code> and <code>tsc --noEmit</code>, which compile from source and need neither output, so building first would only add time.
</details>

2. Delete the `web/node_modules` line from `.dockerignore` and run `docker compose up --build`. What goes wrong, and at which Dockerfile line?

<details><summary>Answer</summary>
<code>Dockerfile:5</code> (<code>COPY web/ ./</code>) would now copy the host's <code>node_modules</code> over the directory that <code>npm ci</code> created at <code>Dockerfile:4</code>. If the host is macOS, native helpers inside it are built for the wrong platform, and <code>npm run build</code> at <code>Dockerfile:6</code> can fail or run the wrong binaries. The context upload also grows by hundreds of megabytes and the cached layer at line 5 is invalidated far more often. The Go stage is unaffected.
</details>

3. The native server defaults to `-addr 127.0.0.1:8080`, yet `Dockerfile:24` passes `0.0.0.0:8080`. Does this expose the prototype to the network, and what keeps a foreign hostname out?

<details><summary>Answer</summary>
Inside a container the loopback interface belongs to that container, so a server bound to <code>127.0.0.1</code> would be unreachable from the published port and from the health check. Binding <code>0.0.0.0</code> only exposes the container's own interfaces; <code>compose.yaml:6</code> publishes that port on the host's <code>127.0.0.1</code> only, so other machines cannot reach it. Separately, the host guard built at <code>cmd/server/main.go:54</code> allows only <code>localhost</code>, <code>127.0.0.1</code>, and <code>::1</code> (<code>internal/httpapi/host_guard.go:22</code>), and README states that <code>-addr</code> "does not add allowed hosts".
</details>

4. Docker sends `SIGTERM` to the container. List the order of shutdown steps and explain why `stop_grace_period` is 20 seconds instead of Compose's 10-second default.

<details><summary>Answer</summary>
<code>SIGTERM</code> cancels the context from <code>signal.NotifyContext</code> at <code>cmd/server/main.go:38</code>. The <code>case &lt;-ctx.Done()</code> branch of the <code>select</code> at <code>cmd/server/main.go:137</code> then runs: <code>scheduler.Close()</code>, <code>runner.Close()</code>, and <code>server.Shutdown</code> with a five-second context (<code>cmd/server/main.go:139-143</code>). Inside <code>runner.Close</code> (<code>internal/engine/runner.go:176</code>) each active run's final save gets five seconds (<code>internal/engine/runner.go:322</code>) and queued-run cleanup shares another five seconds (<code>internal/engine/runner.go:203</code>). After <code>runServer</code> returns, the deferred calls run in reverse: <code>server.Close</code>, <code>scheduler.Close</code>, <code>runner.Close</code> again (already closed), <code>db.Close</code>, and the lock release. The active runs' final saves run in their own goroutines and the queued cleanup runs on the closing goroutine, so those budgets overlap and <code>runner.Close</code> takes up to about five seconds; <code>server.Shutdown</code> can take five more before <code>db.Close</code> and the lock release even start. That already reaches a 10-second grace period, so <code>SIGKILL</code> could cut shutdown off before SQLite closed cleanly; 20 seconds covers the documented budgets.
</details>

5. A fresh clone, `make up`, then `docker compose logs app` shows a permission error creating `/app/data/patchbay.db`. Which Dockerfile line was probably removed, and why does it matter even though Compose mounts a volume there?

<details><summary>Answer</summary>
<code>Dockerfile:21</code> (<code>mkdir -m 0700 /app/data && chown 10001:10001 /app/data</code>). The server runs as user 10001 (<code>Dockerfile:22</code>) and creates the database with <code>os.OpenFile(..., O_CREATE|O_EXCL, 0o600)</code> at <code>internal/store/sqlite.go:34</code>. When Compose creates the <code>data</code> volume for the first time, Docker initializes it from the image's directory at that path, including ownership. Without the <code>chown</code>, the volume is root-owned and the non-root process cannot create the file. The <code>mkdir</code> also matters because <code>compose.yaml:11</code> makes the root filesystem read-only, so <code>os.MkdirAll</code> at <code>cmd/server/main.go:62</code> could not create the directory at runtime.
</details>

6. Why does `web/tsconfig.json` set both `"noEmit": true` and `"verbatimModuleSyntax": true` when Vite, not `tsc`, produces the bundle?

<details><summary>Answer</summary>
<code>noEmit</code> keeps <code>tsc</code> from writing <code>.js</code> files beside the sources; Vite compiles from the <code>.ts</code> and <code>.tsx</code> files directly, and stray outputs would confuse module resolution. <code>verbatimModuleSyntax</code> exists because Vite compiles each file alone and cannot tell a type import from a value import. A type imported without <code>import type</code> would remain as a runtime import of something that does not exist in the compiled module. The option makes <code>tsc --noEmit</code> (<code>web/package.json:8-9</code>) catch that before <code>vite build</code> runs, which is why files such as <code>web/src/components/ExecutionHistory.tsx:1</code> write <code>import type</code>.
</details>

## Experiments

1. **See a build tag in action.** Run `GOOS=windows go build -o /dev/null ./cmd/server` from the repository root. Expected: a compile error such as `undefined: lockDatabase` pointing at `cmd/server/main.go:66`, because `cmd/server/database_lock.go:1` is `//go:build darwin || linux` and the Windows build excludes that file. Then run `go build -o /dev/null ./cmd/server` without `GOOS` and expect it to succeed.

2. **Trace an indirect dependency.** Run `go mod why modernc.org/libc`. Expected output is an import chain starting at `patchbay/internal/store`, through `modernc.org/sqlite`, to `modernc.org/libc`, which explains the `// indirect` line at `go.mod:17`. Then run `go list -m all | wc -l` and compare with the 11 modules named in `go.mod`; the longer list is the full module graph that `go.sum` must cover.

3. **Send the same request the Compose health check sends, and one the host guard rejects.** In one terminal run `make api`. In another run `curl -i http://127.0.0.1:8080/api/health` and expect `HTTP/1.1 200 OK` with the body `{"status":"ok"}` from `internal/httpapi/api.go:34-36`. Now run `curl -i -H 'Host: attacker.example' http://127.0.0.1:8080/api/health` and expect `403`, because that hostname is not in the allowed set at `internal/httpapi/host_guard.go:22`. This is why `compose.yaml:14` uses `127.0.0.1`.

4. **Watch the chunk-size setting change only a warning.** Edit `web/vite.config.ts:13` to `chunkSizeWarningLimit: 100`, then run `npm --prefix web run build`. Expected: the build still succeeds and `web/dist/` is written, but Vite prints a warning that some chunks are larger than 100 kB after minification. Restore the value to `1024` and build again; the warning disappears. The server behaves the same either way because it only serves whatever is in `web/dist` (`internal/httpapi/api.go:122`).

## Next

Chapter 2, "Workflow JSON files and templates" ([02-workflow-files.md](02-workflow-files.md))
