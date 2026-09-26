# Patchbay verification

This record covers the current single-application setup. Previous setup commands
have been replaced by the commands below.

## Verified September 13, 2026

- `make test`: all Go tests with race detection, plus TypeScript checking.
- `make build`: production React assets and the native Patchbay binary.
- Compose configuration validation: only the `app` service is defined.
- Actual Docker image build and startup in an isolated temporary Compose project.
  The default **Patchbay health** workflow completes with HTTP 200, and the app
  serves its built frontend.
- The browser workflow fixture was also executed through the real API in a
  temporary app container. Its two steps run in order and produce the expected
  healthy and unhealthy results using the app's own health endpoint.
- The current image contains only the app executable; its CLI help lists only
  the address, workflow-directory, and frontend-directory flags.
- Formatting checks and `git diff --check`.

`make e2e` starts the single native test app successfully, but Chromium cannot
launch inside this session's macOS sandbox (`MachPortRendezvousServer: Permission
denied`). Browser assertions and screenshots remain unverified. The API check
above verifies the fixture and execution path, not browser rendering.

Temporary test containers, networks, and image tags were removed. The existing
development instance was not restarted. The obsolete auxiliary native binary
was removed from `bin/`.

## Verified September 24, 2026

The frontend rebuild (TanStack Router, TanStack Query, and Tailwind CSS) landed
in six increments. After each increment the owner ran these checks on the
development machine, and all passed:

- `make test`: Go race tests and `tsc --noEmit` under TypeScript 7.0.2.
- `make build`: the production frontend through Vite 8 with the Tailwind plugin,
  and the native binary.
- `make e2e`: the 11 Playwright tests in Chromium, installed once into
  `.cache/playwright`. The specs did not change during the rebuild.
- `docker compose build` after the Tailwind wiring, which confirmed that the
  lockfile carries the Alpine (musl) binary for Tailwind's native scanner.

Static reviews compared each increment with the previous stylesheets, the
Playwright contract, and the installed library sources; they are not test runs.
The New workflow page and the not-found page have no Playwright coverage; the
owner checked them by hand after the final increment.

## Checks to run

```sh
make test
make build
docker compose config --quiet
docker compose up --build --wait
```

Open `http://localhost:8080` and run **Patchbay health**. It should complete with
an HTTP 200 result. Your own workflow files belong in `workflows/`; the files
in `examples/` are templates whose URLs need to be configured.

For browser checks on their separate test port:

```sh
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web exec -- playwright install chromium
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" make e2e
```

Playwright starts a single Patchbay process on port 18080 and checks that
instance's health endpoint. The two different expected status codes in its
workflow fixture produce healthy and unhealthy results without another service.
Go's HTTP tests own their temporary servers for HTTP 503, redirects, timeouts,
connection failures, and cancellation. No external services are contacted by
these tests.
