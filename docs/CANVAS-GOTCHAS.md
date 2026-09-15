# Canvas API gotchas

Moved verbatim from the machine-local CLAUDE.md on 2026-09-14 (config cut), where it lived as
"Known Gotchas". Each entry is a real Canvas behaviour that broke Laurus once. Grep this file
when a Canvas call behaves oddly, and append an entry in the same commit as the fix.

## Calendar event IDs can be strings

Both `/users/self/upcoming_events` and `/calendar_events?type=assignment` return IDs like
`"assignment_500"` (strings), not integers. `UpcomingEvent` omits the ID field entirely.
`CalendarEvent` has a custom `UnmarshalJSON` that handles both int64 and string IDs, extracting
the numeric suffix from strings like `"assignment_500"` → `500`.

## Canvas quizzes don't set SubmittedAt

In-browser quizzes may have `workflow_state: "graded"` and a `grade` but `submitted_at: null`.
Always check `grade != nil || workflow_state == "graded"` in addition to `submitted_at != nil`
when determining if something was submitted.

## Announcements start_date defaults to 14 days ago

`GET /api/v1/announcements` defaults `start_date` to 14 days ago if omitted. Always pass an
explicit `start_date` (e.g. `2000-01-01`) to get all announcements. The `end_date` defaults to
28 days after `start_date`.

## Discussion /view endpoint edge cases

`GET /courses/:id/discussion_topics/:id/view` can return **503** if Canvas hasn't built the
thread cache yet (retry after a short delay). It also returns **403 with plain text body
`require_initial_post`** (not JSON) if the topic requires an initial post and the student hasn't
posted yet. Don't parse that 403 body as JSON.

## Conversations unread_count returns a string

`GET /api/v1/conversations/unread_count` returns `{"unread_count": "5"}` — a string, not an
integer. Parse with `strconv.Atoi`.

## Pages tab disabled but pages exist

Quercus (and other Canvas instances) can disable the Pages navigation tab while individual pages
still exist and are accessible via slug URL. `GET /courses/:id/pages` returns 404 "That page has
been disabled for this course" but `GET /courses/:id/pages/:slug` works fine. When `FindPage`
gets a 404 from `ListPages`, it gracefully skips fuzzy search rather than failing.

## Files tab 403 on most Quercus courses

UofT's Quercus restricts `GET /courses/:id/files` (403) on most courses even though files are
embedded in modules. Files are accessible through module items but not the Files API. Handle 403
gracefully in `files` and `download` commands.

## Grade calculator parity

Laurus must match Canvas's exact algorithm, taken from their TypeScript/Ruby source:

- `current_score` counts only graded items; `final_score` treats ungraded work as 0
- Weighted groups: compute each group's percentage, then the weighted sum, scaling if the
  weights total under 100%
- Drop-lowest uses the Kane & Kane bisection algorithm, not brute force
- Excused items are fully removed before any calculation
- Use `math/big` or `shopspring/decimal` for arbitrary precision, to match Canvas's Big.js

Twelve edge cases are written up in `BUILD_PLAN.md`, which is gitignored and local-only — it is
present on the Legion checkout, not on the Mac.
