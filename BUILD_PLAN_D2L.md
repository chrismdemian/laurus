# Laurus — D2L Brightspace Build Plan

> D2L Brightspace as a second LMS backend. Feasibility: **YES**, ~3 weeks of work.
> Primary access path: session cookie capture → official Valence REST API.
> Research docs: `research/d2l-01` through `research/d2l-07`.

## Feasibility Summary

D2L has no student-self-service API tokens (unlike Canvas). Every documented OAuth path
requires an institutional admin to register the app via Manage Extensibility — dead end
for a distributable consumer tool. **But**: session cookies (`d2lSessionVal` +
`d2lSecureSessionVal`) from a normal browser login grant full access to the official
Valence REST API (`/d2l/api/le/...`, `/d2l/api/lp/...`). Confirmed by three independent
production projects (RohanMuppa MCP, singularity, patrick). This is not scraping — it's
the real API with alternate auth presentation.

**Primary vector: manual cookie capture.** User pastes two cookie values from DevTools.
Session lasts ~3 hours default timeout. Tool re-prompts on 401.

**Fallback vector to investigate first: Brightspace Pulse APK OAuth client_id extraction.**
If the mobile app ships a globally-registered client_id using PKCE, we get durable refresh
tokens and no session expiry. 2–4 hour investigation gates the auth architecture.

## Key Decisions (locked)

| Decision | Choice | Rationale |
|---|---|---|
| Architecture | **Option A: Parallel packages** (`internal/d2l/` beside `internal/canvas/`) | Zero regression risk on Canvas code. Honest: D2L grades/content/quizzes are categorically different. |
| Config | **Multi-profile TOML** (`[[profile]]` array) | Supports UofT Canvas + another school's D2L simultaneously. Backward-compatible migration from `canvas_url`. |
| Cache | **Separate SQLite DB per profile** | Zero migration risk; no ID-collision edge cases; every query works unchanged. |
| Factory | **Interface refactor** (`cmdutil.Factory.Client` returns `lms.Client` interface) | Eliminates the "most dangerous latent assumption." Pre-req for all D2L work. |
| CLI UX | **Profile-scoped default** (`laurus next` uses active profile; `--profile` flag for override; `--all` flag for merged view) | Matches `gh auth switch` / `kubectl config use-context` mental model. Preserves sub-100ms cache reads. |
| Grade calculator | **Display D2L's server-computed grades** (no local Kane-Kane) | D2L grade model is categorically different; re-implementing it is a separate 2-week project. What-if simulation deferred. |
| Write operations | **Read-only for v1** | XSRF token handling adds complexity + TOS exposure. Submissions come in a later phase. |
| Distribution | **Personal first, then public on Twitter** | Accept DMCA takedown risk. No lawsuit precedent for student LMS tools. |

## Dependency Graph

```
Phase 0: Pulse APK Investigation  ─── (go/no-go gate for auth model)
         │
Phase 1: Factory Interface Refactor ──── (MANDATORY prerequisite)
         │
Phase 2: Config Multi-Profile ─────────── (parallel with 1)
         │
Phase 3: D2L Auth (cookie or OAuth)
         │
Phase 4: D2L Client Foundation ─ version discovery, pagination, rate limit, errors
         │
         ├── Phase 5: Courses + Enrollments + Whoami
         │        │
         │        ├── Phase 6: Grades
         │        ├── Phase 7: Assignments (Dropbox) + Submissions
         │        ├── Phase 8: Announcements (News) + Calendar
         │        ├── Phase 9: Discussions + Quizzes
         │        └── Phase 10: Content + File Downloads
         │
Phase 11: Cache Schema per Profile
         │
Phase 12: Command Routing (laurus next / grades / assignments work across LMSes)
         │
Phase 13: MCP + TUI Wiring
         │
Phase 14: Auth UX Polish (laurus auth add, refresh, status)
         │
Phase 15: Distribution + Monitoring
         │
Phase 16 (deferred): Write Operations (XSRF + submit assignment + post reply)
```

---

## Go Dependencies (new)

| Package | Purpose | Why this one |
|---|---|---|
| `golang.org/x/oauth2` | OAuth 2.0 + PKCE | **Only if Phase 0 succeeds.** Standard library-adjacent, battle-tested. |
| `github.com/go-rod/rod` | Embedded browser | **Only for future Vector B fallback.** Auto-downloads Chromium. NOT needed for v1. |

Everything else already in `go.mod` (retryablehttp, rate limiter, keyring, TOML, decimal,
glamour) works for D2L unchanged.

---

## Phase 0: Pulse APK Investigation

**Goal**: Determine whether Brightspace Pulse ships a globally-registered OAuth `client_id`
that works across all institutions. If yes, rebuild auth around OAuth + PKCE. If no,
proceed with cookie capture.

**Effort**: 2–4 hours.

- [ ] Download latest `com.d2l.brightspace.student.android` APK from APKMirror
- [ ] `apktool d brightspace-pulse.apk && jadx -d pulse-src brightspace-pulse.apk`
- [ ] Grep for OAuth config:
  - `grep -r "client_id\|oauth_client_id\|auth.brightspace.com" res/ assets/ smali/`
  - Check `AndroidManifest.xml` for custom URL schemes / deep links
  - Check `strings.xml` and `assets/*.json`
- [ ] If `client_id` found, test against `https://auth.brightspace.com/oauth2/auth` with
      PKCE challenge from a known institution (use `curl` + browser manually)
- [ ] Document finding: globally-registered vs per-institution, PKCE-only vs secret-required
- [ ] **Decision**:
  - **If OAuth works**: jump to Phase 3 auth path "OAuth + PKCE"
  - **If OAuth fails or client_id is per-institution**: Phase 3 auth path "cookie capture"

**Deliverable**: `research/d2l-08-pulse-apk-findings.md` with the extracted config (if any)
and a one-line decision.

---

## Phase 1: Factory Interface Refactor

**Goal**: Replace `cmdutil.Factory.Client func() (*canvas.Client, error)` with an interface
so commands can target either LMS. **No behavioral change** — same Canvas logic, same
outputs, just interface-ified.

> This MUST ship before any D2L code. It's the "most dangerous latent assumption" from the
> architecture research — ignoring it means duplicating every command file.

- [ ] Create `internal/lms/` package with a minimal interface:
  ```go
  type Client interface {
      BaseURL() string
      Kind() Kind // "canvas" | "d2l"
      WhoAmI(ctx context.Context) (User, error)
  }
  ```
  Keep the interface TINY. Every other operation stays type-asserted per-LMS for now.
  Resist the urge to build a full abstraction — it'll leak.
- [ ] `*canvas.Client` implements `lms.Client`. Add `Kind() Kind { return KindCanvas }`.
- [ ] Change `cmdutil.Factory.Client func() (lms.Client, error)`. Commands that need
      Canvas-specific methods type-assert: `c := f.Client().(*canvas.Client)`. Ugly but
      honest — will stay Canvas-only until D2L implementations exist.
- [ ] Update all call sites in `pkg/cmd/*/` (mechanical change)
- [ ] Update `pkg/mcp/server.go` same way
- [ ] **Verify**: all existing tests pass, `laurus next`/`laurus grades`/etc. behave identically
- [ ] Commit: "Refactor Factory.Client to return lms.Client interface"

**Risk**: Touches every command file. Blast radius is wide but change per-file is small.

---

## Phase 2: Config Multi-Profile

**Goal**: Support N profiles in `config.toml`, each with its own LMS type + URL. Migrate
existing single-URL configs transparently.

- [ ] New TOML schema:
  ```toml
  default = "utoronto"
  sync_dir = "~/School"
  theme = "auto"

  [[profile]]
  name = "utoronto"
  lms  = "canvas"
  url  = "https://q.utoronto.ca"

  [[profile]]
  name = "waterloo"
  lms  = "d2l"
  url  = "https://learn.uwaterloo.ca"
  ```
- [ ] `internal/config/migrate.go`: on load, if `canvas_url` exists and no `[[profile]]`,
      synthesize a `utoronto`-named Canvas profile and rewrite the file. One-time.
- [ ] `config.ActiveProfile()`, `config.AllProfiles()`, `config.ProfileByName(name)`
- [ ] CLI override: global `--profile <name>` flag on root cobra command
- [ ] `laurus profiles` subcommand: list all profiles with active indicator
- [ ] `laurus use <name>` subcommand: switch default profile
- [ ] **Verify**: existing Canvas users see no behavioral change post-migration

---

## Phase 3: D2L Auth

**Goal**: Store D2L credentials in the keychain. Two implementations depending on Phase 0 outcome.

### 3A. Cookie Capture Auth (if OAuth path failed)

- [ ] `internal/auth/d2l_cookie.go`: `StoreCookies(url, session, secure string)`,
      `LoadCookies(url)` using existing 99designs/keyring
- [ ] Cookies stored as a single JSON blob per keychain entry: `{"session": "...", "secure": "...", "capturedAt": "..."}`
- [ ] Capture timestamp used for proactive "session will expire soon" warning at ~4h
- [ ] On 401 from any D2L API call, surface `AuthExpiredError` that the command layer
      prompts to re-paste

### 3B. OAuth + PKCE Auth (if Phase 0 succeeded)

- [ ] `internal/auth/d2l_oauth.go`: use `golang.org/x/oauth2` + PKCE code verifier
- [ ] Spin up localhost callback server on random port (`127.0.0.1:0`) during `laurus auth add`
- [ ] Open browser to `https://auth.brightspace.com/oauth2/auth?client_id={pulse_client_id}&...`
- [ ] Store `access_token` + `refresh_token` + `expires_at` in keyring
- [ ] Background refresh when `expires_at` within 5 minutes; single-use refresh tokens
      rotate on every exchange, so **persist the new refresh token immediately** after every
      refresh call (mutex-protected to prevent concurrent refresh races)

Whichever path ships: same `lms.Client` interface downstream. Commands don't care.

---

## Phase 4: D2L Client Foundation

**Goal**: `internal/d2l/` package with HTTP plumbing that every endpoint uses.

- [ ] `internal/d2l/client.go`:
  ```go
  type Client struct {
      baseURL  string
      auth     authMethod // cookie or oauth
      http     *http.Client
      limiter  *rate.Limiter // 3 req/sec sustained, 10 burst
      lpVer    string        // dynamically discovered
      leVer    string
  }
  ```
- [ ] Layered RoundTripper stack (same pattern as Canvas client):
  - `hashicorp/go-retryablehttp` for 429 retry with exponential backoff + jitter
  - `rate.Limiter` token bucket
  - Auth injector (adds `Cookie: d2lSessionVal=...; d2lSecureSessionVal=...` OR
    `Authorization: Bearer <oauth_access_token>`)
  - User-Agent: real Chrome UA string (reduces bot-detection signal; some institutions
    fingerprint non-browser UAs)
- [ ] **Dynamic version discovery**: on first use, GET `/d2l/api/versions/` (public, no
      auth). Parse `[{"ProductCode": "lp", "LatestVersion": "1.56"}, ...]`. Cache per-instance
      in-memory + disk (24h TTL). Use discovered versions in all subsequent paths.
- [ ] Pagination: `Paginate[T]` helper that handles both `PagedResultSet` (bookmark +
      `HasMoreItems` flag) and `ObjectListPage` (full `Next` URL). Must re-sign `Next` URLs
      with current auth. Do NOT reconstruct URLs.
- [ ] Error types:
  - `AuthExpiredError` — 401 + no `WWW-Authenticate` header reset needed
  - `FeatureDisabledError` — 403 with plain-text `Tool disabled for this org unit` body
  - `NotFoundError` — 404 (remember: D2L returns 404 for malformed JSON fields, not just
    missing resources — differentiate if possible)
  - Wrap everything else as `UnexpectedError` with status + body
- [ ] Rate limit: respect `X-Rate-Limit-Remaining`. Token bucket at 3 req/s (matches
      RohanMuppa's production settings; D2L's 50000/min limit is not a practical concern
      for a single-user CLI).

---

## Phase 5: Courses + Enrollments + Whoami

- [ ] `d2l.WhoAmI(ctx)` → `GET /d2l/api/lp/{ver}/users/whoami` → `Identifier`, `UniqueName`, `FirstName`, `LastName`
- [ ] Cache `Identifier` on the client (it has no `self` alias like Canvas)
- [ ] `d2l.ListCourses(ctx)` → `GET /d2l/api/lp/{ver}/enrollments/myenrollments/?orgUnitTypeId=3`
      (3 = Course Offering; confirm per instance, since OrgUnit type IDs are institution-specific)
- [ ] `d2l.GetOrgUnitTypes(ctx)` → `GET /d2l/api/lp/{ver}/outypes/` — call on first profile
      setup, cache the type map per-instance, use it to find the "Course Offering" type ID
      dynamically
- [ ] `Course` struct with fields mapped from D2L response: `OrgUnitID`, `Name`, `Code`,
      `StartDate`, `EndDate`, `IsActive`
- [ ] Tests: mock HTTP responses from RohanMuppa's fixtures if available, otherwise fabricate

---

## Phase 6: Grades

- [ ] `d2l.ListGrades(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/grades/values/myGradeValues/`
- [ ] Returns empty array for unreleased grades — can't distinguish "ungraded" from
      "graded but hidden." Document this in the CLI output.
- [ ] `GradeValue` struct: `GradeObjectIdentifier`, `PointsNumerator`, `PointsDenominator`,
      `WeightedDenominator`, `WeightedNumerator`, `DisplayedGrade`
- [ ] `d2l.GetFinalGrade(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/grades/final/values/{userID}`
- [ ] **No local grade calculator**. Display what D2L returns. Note in docs that grade
      what-if simulation is Canvas-only.

---

## Phase 7: Assignments + Submissions

- [ ] `d2l.ListDropboxFolders(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/dropbox/folders/`
- [ ] `d2l.GetMySubmissions(ctx, orgUnitID, folderID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/dropbox/folders/{folderID}/submissions/mysubmissions/`
- [ ] `d2l.GetMyFeedback(ctx, orgUnitID, folderID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/dropbox/folders/{folderID}/feedback/myFeedback/`
- [ ] `Assignment` struct with `Name`, `DueDate`, `StartDate`, `EndDate`, `GroupSubmission`,
      `AllowableFiles`, `Instructions` (HTML)
- [ ] Map to unified display: the `laurus next` / `laurus assignments` commands need a
      shared display type — define it in `internal/lms/display.go` as a minimal set of
      fields both LMSes can populate (title, due, course, status, URL)

---

## Phase 8: Announcements + Calendar

- [ ] `d2l.ListNews(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/news/`
- [ ] `d2l.ListMyCalendarEvents(ctx, startDate, endDate, orgUnitIDsCSV)` →
      `GET /d2l/api/le/{ver}/calendar/events/myEvents/?startDateTime=...&endDateTime=...&orgUnitIdsCSV=...`
      — max 100 course IDs per call; chunk if user has >100 courses (unlikely but handle it)
- [ ] Merge calendar events with Canvas's upcoming events in the `laurus next` output

---

## Phase 9: Discussions + Quizzes

- [ ] `d2l.ListDiscussionForums(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/discussions/forums/`
- [ ] `d2l.ListDiscussionTopics(ctx, orgUnitID, forumID)` → `.../forums/{forumID}/topics/`
- [ ] `d2l.ListDiscussionPosts(ctx, orgUnitID, forumID, topicID)` → `.../topics/{topicID}/posts/`
- [ ] `d2l.ListQuizzes(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/quizzes/`
- [ ] `d2l.ListQuizAttempts(ctx, orgUnitID, quizID)` → `.../quizzes/{quizID}/attempts/`
- [ ] Note: quiz question-level data is NOT returned by the API. `laurus quiz view` can
      show attempt scores but not questions.

---

## Phase 10: Content + File Downloads

- [ ] `d2l.GetContentRoot(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/content/root/`
- [ ] `d2l.GetModuleStructure(ctx, orgUnitID, moduleID)` → `.../content/modules/{moduleID}/structure/`
- [ ] `d2l.DownloadTopicFile(ctx, orgUnitID, topicID, dest io.Writer)` →
      `GET /d2l/api/le/{ver}/{orgUnitID}/content/topics/{topicID}/file` — streams binary
- [ ] `d2l.GetCourseOverview(ctx, orgUnitID)` → `GET /d2l/api/le/{ver}/{orgUnitID}/overview`
- [ ] `d2l.GetSyllabusAttachment(ctx, orgUnitID, dest io.Writer)` →
      `.../overview/attachment`
- [ ] `laurus files` and `laurus download` route to the right backend based on profile LMS

---

## Phase 11: Cache Schema per Profile

**Goal**: `internal/cache/` opens a profile-specific SQLite DB instead of the single
Canvas-shared one.

- [ ] `cache.Open(profileName string)` — resolves to `~/.local/share/laurus/<profile>.db`
- [ ] Existing schema stays; each profile gets its own fresh DB on first use
- [ ] For D2L profiles: use D2L's field names where schema makes sense; reuse Canvas tables
      where concepts overlap (courses, assignments — store D2L responses in compatible shapes)
- [ ] Don't add D2L-specific tables in v1 unless needed. Discussions/quizzes/content can be
      fetched live and cached in generic `blob_cache` table keyed by URL+ETag

---

## Phase 12: Command Routing

**Goal**: Every existing `laurus` command works transparently on D2L profiles.

- [ ] `laurus next` — reads active profile, routes to `*canvas.Client` or `*d2l.Client`
- [ ] `laurus grades` — same
- [ ] `laurus assignments` — same, merged display type
- [ ] `laurus announcements` — same
- [ ] `laurus calendar` — same
- [ ] `laurus discussions` — same
- [ ] `laurus files` — same
- [ ] `laurus download` — same
- [ ] `laurus quiz list` / `view` — routes correctly
- [ ] New global flag: `--all-profiles` — merges output from every configured profile,
      labels rows by profile name (deferred if complexity bites)
- [ ] Commands that don't map to D2L (e.g., `laurus calc` local grade simulator) show
      "not supported for D2L profiles" and exit 1

---

## Phase 13: MCP + TUI Wiring

- [ ] MCP server registers tools scoped to the active profile. Tool descriptions mention
      which LMS to avoid AI-assistant confusion (`list_courses` works for both; description
      says "from the active LMS profile").
- [ ] Alternative (future): per-LMS tool sets registered when multi-profile mode. Skip in v1.
- [ ] TUI (Phase 11 of main BUILD_PLAN — if not yet built, D2L support slots in here
      naturally). Dashboard shows profile switcher in the top bar. Swap the backing client
      on profile change.

---

## Phase 14: Auth UX Polish

- [ ] `laurus auth add` — interactive:
  - Prompts for school URL
  - Detects LMS type: probe `/d2l/api/versions/` (D2L) vs `/api/v1/users/self` (Canvas)
  - Routes to appropriate auth flow (cookie paste, OAuth redirect, or Canvas token paste)
  - Validates by calling `WhoAmI` and displaying the detected name
  - Saves profile + credentials
- [ ] `laurus auth status` — shows each profile with last-auth timestamp, session health,
      "needs refresh?" warning
- [ ] `laurus auth refresh <profile>` — re-runs the cookie-paste or OAuth flow for a single
      profile
- [ ] `laurus auth rm <profile>` — delete credentials + remove profile entry
- [ ] Proactive expiry warning: on any command, if stored D2L cookies are >4 hours old,
      print a one-line warning ("Your Waterloo session will likely expire soon — run
      `laurus auth refresh waterloo`")

---

## Phase 15: Distribution + Monitoring

- [ ] Update `.goreleaser.yml` — no new binary variants needed (same build, new code)
- [ ] README: add "Supported LMSes" section. Honest docs about cookie-capture UX.
- [ ] GitHub Action: weekly `curl /d2l/api/versions/` against 2–3 real institutions; alert
      on version bumps so we can test-run the regression suite
- [ ] Add a CLI banner on first-run explaining the DevTools cookie step (link to a 30-second
      screencast or animated GIF in the README)
- [ ] DMCA-response plan: if D2L sends a takedown letter, comply. Keep a fork-friendly
      structure (all D2L code in `internal/d2l/` makes it easy to strip if needed).

---

## Phase 16 (deferred): Write Operations

Split off to a later build plan. Requires:
- XSRF token extraction during cookie capture (grab `D2L.LP.Web.Authentication.Xsrf`
  JavaScript global or `<meta name="csrf-token">` during the browser session)
- `POST /d2l/api/le/{ver}/{orgUnitID}/dropbox/folders/{folderID}/submissions/` with
  `multipart/mixed` body (NOT `multipart/form-data` — JSON first part, file second,
  mandatory `Content-Disposition` on each)
- Discussion post submission
- Significantly higher TOS exposure — keep this behind a separate opt-in flag

---

## Testing Strategy

- [ ] No D2L sandbox exists for public testing. Options:
  - RohanMuppa has published response fixtures — use for unit tests
  - Test against a real instance the user has access to (UofT uses Canvas; need a friend
    at a D2L school or request institutional permission)
  - Record real responses locally during development, scrub PII, commit as testdata
- [ ] Integration tests behind a `-tags d2l_integration` build tag — only run manually
- [ ] Golden-file tests for response parsing

---

## Known Gotchas (add to CLAUDE.md once confirmed)

From research — these are real and will bite:

1. **OrgUnit type IDs are institution-specific integers.** Call `/outypes/` per instance;
   don't hardcode "Course Offering = 3."
2. **Pagination has two shapes.** `PagedResultSet` vs `ObjectListPage`. Single loop will
   silently drop results after page 1 on half the endpoints.
3. **Unreleased grades return empty.** Can't distinguish "ungraded" from "hidden."
4. **OAuth refresh tokens rotate on every use.** Mutex around refresh or lose session.
5. **Dropbox submissions use `multipart/mixed` (RFC 2046), not `multipart/form-data`.**
   Error on missing `Content-Disposition` is misleading ("submitted comments are too large").
6. **Date formats vary by endpoint.** Some ISO 8601, some Unix epoch. Per-endpoint handling.
7. **404 can mean "invalid JSON field format," not "resource not found."** Don't always
   trust 404 = doesn't exist.
8. **No `self` alias.** Always fetch `/users/whoami` at startup and cache the user ID.
9. **No unified "assignments" concept.** Coursework is fragmented across dropbox, quizzes,
   discussions, and content topics. `laurus assignments` must aggregate from multiple
   endpoints per course.
10. **Session cookies expire at ~180min inactivity.** No programmatic refresh. Must re-prompt.

---

## Timeline Estimate

Assuming solo dev, comfortable with Go, with Canvas architecture already shipped:

| Phase | Effort |
|---|---|
| Phase 0 (Pulse investigation) | 0.5 day |
| Phase 1 (Factory refactor) | 0.5 day |
| Phase 2 (Multi-profile config) | 0.5 day |
| Phase 3 (D2L auth) | 1 day (cookie) or 2 days (OAuth) |
| Phase 4 (Client foundation) | 1.5 days |
| Phase 5–10 (API coverage) | 5–7 days |
| Phase 11 (Cache per profile) | 0.5 day |
| Phase 12 (Command routing) | 1 day |
| Phase 13 (MCP/TUI wiring) | 1 day |
| Phase 14 (Auth UX polish) | 1 day |
| Phase 15 (Release prep) | 0.5 day |
| **Total** | **~13–16 days of focused work** |

Matches the research assessment of ≤3 weeks. Write operations (Phase 16) add another 3–5 days.
