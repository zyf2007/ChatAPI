# Playwright browser tests

Run from the repository root:

```bash
npm --prefix frontend run test:e2e
```

Type-check test code and shared fixtures with `npm --prefix frontend run test:e2e:types`.

The shared endpoint fixture is in `fixtures/apiEndpoint.ts`. It reads:

- `CHATAPI_TEST_API_BASE_URL` (default: `http://127.0.0.1:5000`)
- `CHATAPI_TEST_API_KEY` (optional; never logged)
- `CHATAPI_TEST_MODEL` (optional)

For the supplied remote endpoint, set these variables in the shell before running tests. The key is intentionally not stored in this repository. The browser tests default to the local Vite server and do not call a remote endpoint unless a test explicitly uses the fixture configuration.

To run the Lab smoke tests, start the local backend with `go run ./cmd/server lab` from `backend/`, then run:

```bash
CHATAPI_TEST_LAB=1 npm --prefix frontend run test:e2e -- lab-smoke.spec.ts --workers=1
```

Use a local Lab without an access password for these tests. The API endpoint fixture must point to that same backend; the default is port 5000. `fixtures/labWorkspace.ts` owns creating waiting requests, observing their completion, cleaning up only those requests, opening the mobile sidebar, and auditing page/network failures. Conversation selectors use durable conversation IDs rather than changing previews.

The suite covers model CRUD and `/v1/models` discovery across reloads, workspace notification/media settings, all four supported instance pages, hidden account features, homepage statistics, and an actual API request answered and deleted through the UI. `lab-settings.spec.ts` keeps these configuration journeys separate; `openLabSettings()` shares desktop/mobile navigation. Shared configuration changes restore their original values and require `CHATAPI_TEST_LAB_MUTATIONS=1`. Go integration tests also exercise image uploads/reads and owner isolation with a real image processor.

The pruning journey deletes older conversations, so run it only against a separate temporary database configured with `CHATAPI_DB_DSN` and a separate `CHATAPI_MEDIA_DERIVED_DIR`:

```bash
CHATAPI_TEST_LAB=1 CHATAPI_TEST_LAB_MUTATIONS=1 npm --prefix frontend run test:e2e -- --workers=1
```

`CHATAPI_HEADED=1` requests a visible Chromium window; in headless CI or containers without a display, leave it unset. Set `CHATAPI_CHROMIUM_PATH` to a local Chromium executable when `/usr/bin/chromium` does not exist.

The HTML report is `test-results/report/index.html`; failed runs retain screenshots, traces, and video under `test-results/artifacts/`. Credentials should remain in environment variables, never in committed tests or reports.

PR review findings and exact validation scope are in `pr-review-2026-10-31.md`. The optional `pr48-regression.spec.ts` runs only with `CHATAPI_TEST_PR48=1` against a separately started PR frontend, and marks the confirmed regression as an expected failure. It is not part of the passing Lab smoke count.
