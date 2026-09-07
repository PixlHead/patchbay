# Patchbay M0 handoff verification

Recorded September 6, 2026.

## Passed

- `make test`: Go tests with race detection, plus TypeScript checking.
- `go vet ./...`, with the project-local Go cache.
- `make build`: production React assets and the server/demo native binaries.
- Linux cross-compilation of the server for amd64 and arm64, with CGO disabled.
- `docker compose config --quiet`: Compose configuration validation.
- gofmt and Prettier formatting checks.
- Dependency installation audit: no reported vulnerabilities at installation.

An additional temporary integration harness started the built server and demo
processes and exercised the actual HTTP executor through the real API. It verified
healthy, HTTP 503, sequential, timeout, and connection-refused results; serving the
built frontend; bounded-session behavior at restart; and invalid configuration
rejection before startup. Both processes were cleaned up afterward.

A temporary Happy DOM harness rendered the real React component against the built
Go API. It clicked through all four workflows, checked the distinction between a
completed run and an unhealthy service, remounted the app to verify history,
and checked connection-loss feedback and recovery. This harness was kept outside
the project so M0 did not acquire another test framework dependency.

## Not verified in the current environment

The two included Playwright tests could start the app/demo servers, but macOS
sandbox restrictions prevented Chromium from launching (`MachPortRendezvousServer:
Permission denied`). Their browser assertions and screenshots did not execute.
The DOM check does not replace browser rendering or mobile layout verification.

Docker's engine was unavailable at its local socket. Compose configuration and
Linux compilation were checked, but the container image build and container runtime
have not been verified. Start Docker Desktop, then run the commands below.

## Finish those checks locally

From the project directory:

```sh
docker compose up --build -d --wait
```

Open `http://localhost:8080` and run the examples. Then, to run the browser checks
on their separate test ports:

```sh
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" npm --prefix web exec -- playwright install chromium
PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/playwright" make e2e
```

These commands are not marked passed until they actually run successfully.
