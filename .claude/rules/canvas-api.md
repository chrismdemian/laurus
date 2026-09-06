---
paths:
  - "internal/canvas/**"
  - "pkg/mcp/**"
---

# Canvas API Rules

## Authentication
- Always use `Authorization: Bearer <token>` header, never `access_token` query param
- Query param auth causes tokens to be stripped from pagination Link headers

## Pagination
- Two styles exist: offset-based and bookmark-based — treat Link URLs as opaque
- Parse `Link` header case-insensitively
- Default page size is 10; request `per_page=100` for efficiency
- `rel="next"` absent = last page; `rel="last"` may be omitted on expensive endpoints
- A `rel="next"` with an EMPTY URL is a confirmed intermittent Canvas bug: `Paginate` yields the items it has, then `ErrPaginationTruncated`. Never treat that partial set as complete (the cache marks it `suspect` and does not prune)

## Announcements
- `GET /announcements` needs `context_codes[]`; `end_date` defaults to 28 days AFTER `start_date`, so an old `start_date` alone is an EMPTY window (0 rows for courses holding dozens). `canvas.ListAnnouncements` sends a far-future `end_date` whenever `StartDate` is set without `EndDate`

## Rate Limiting
- Watch `X-Rate-Limit-Remaining` header on every response
- On 429: exponential backoff with jitter
- Throttling is ALSO answered as HTTP 403 with body "Rate Limit Exceeded" and `X-Rate-Limit-Remaining` <= 0 (not 429). `isThrottled403` maps it to `ErrRateLimited` and the client retries it; a plain 403 stays `ErrForbidden` and is never retried
- Sequential requests virtually never throttle; avoid parallel fan-out
- Each token has its own independent quota bucket

## Dates
- All timestamps are UTC ISO 8601 (`2024-09-01T23:59:59Z`)
- Convert to user's timezone (from profile `time_zone` field) for display
- Assignment `due_at` can be null (no due date)

## Errors
- 401 with `WWW-Authenticate` header = invalid/expired token → re-auth
- 401 without `WWW-Authenticate` = permission denied → surface error
- Error format is inconsistent: check `errors` array, `error` string, `message` string
- Permission denied messages are localized — never parse error text

## File Uploads (3-step)
1. POST preflight to get `upload_url` + `upload_params`
2. POST to `upload_url` as multipart — echo all `upload_params`, `file` MUST be last field
3. GET the redirect Location with auth header to confirm
- Token in upload_params expires in 5-30 min depending on backend (InstFS vs S3)
- Submitting ≠ uploading — step 4 is a separate POST to create the submission with file IDs

## GraphQL
- Endpoint: `POST /api/graphql` with same Bearer token
- Use `_id` field for REST-compatible numeric IDs (not `id` which is base64 global)
- `createSubmission` mutation is marked unstable — use REST for submissions
- No file upload support in GraphQL — always use REST for files

## IDs
- Always use numeric Canvas IDs, never SIS IDs (students typically lack SIS read permission)
- Use `self` in place of user_id for the authenticated user
