package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
)

// Freshness tiers. A cache-first tool names the tier its data belongs to;
// the cache is refreshed through the sync layer when the last complete sync
// of that (resource, course) is older than the tier.
const (
	tierGrade    = 5 * time.Minute  // anything carrying scores or grades
	tierActivity = 30 * time.Minute // announcements, discussion topics
	tierFiles    = 1 * time.Hour    // file metadata
	tierStable   = 4 * time.Hour    // modules, pages
	tierIdentity = 24 * time.Hour   // course identity for name resolution
)

// backoffAfterFailure is how long a failed sync of a (resource, course)
// suppresses automatic retries so many callers (and many processes: the
// MCP server, a cron sync) cannot pile onto a broken endpoint. It is read
// from sync_meta.last_attempt_at, which the sync layer writes on failure,
// so it holds across processes. fresh=true bypasses it.
const backoffAfterFailure = 2 * time.Minute

// refreshTimeout bounds one detached refresh. The Canvas client has no
// overall HTTP timeout of its own, and a refresh outlives the caller that
// started it (see refresh), so it needs its own ceiling.
const refreshTimeout = 5 * time.Minute

// envelope wraps every read so the model can never mistake cached data for
// live data: as_of is when the data was fetched from Canvas, stale says the
// data is older than its tier or the last sync did not complete, source is
// "cache" or "live".
type envelope struct {
	AsOf       time.Time `json:"as_of"`
	Stale      bool      `json:"stale"`
	Source     string    `json:"source"`
	SyncStatus string    `json:"sync_status,omitempty"`
	SyncError  string    `json:"sync_error,omitempty"`
	Note       string    `json:"note,omitempty"`
	Data       any       `json:"data"`
}

const (
	sourceCache = "cache"
	sourceLive  = "live"
)

// liveResult wraps data fetched directly from Canvas.
func liveResult(data any) (*mcplib.CallToolResult, error) {
	return jsonResult(envelope{AsOf: time.Now().UTC(), Source: sourceLive, Data: data})
}

// liveEmpty is the envelope for a live read that found nothing: an empty
// list plus a note, so the shape never changes on the caller.
func liveEmpty(note string) (*mcplib.CallToolResult, error) {
	return jsonResult(envelope{AsOf: time.Now().UTC(), Source: sourceLive, Note: note, Data: []any{}})
}

// readSpec names what a cache-first read wants.
type readSpec struct {
	rt       cache.ResourceType
	courseID int64
	ttl      time.Duration
	fresh    bool // bypass the cache and sync now
}

// read serves spec from the cache, refreshing it through the sync layer
// first when it is missing, older than ttl, or fresh is set. dest receives
// the rows (a pointer to a slice of the canvas type). The returned envelope
// carries the freshness stamp. If a refresh fails and cached rows exist,
// they are served with stale=true and sync_error set; a refresh failure
// with nothing cached is an error. With no cache available at all (no
// factory Cache), live is called and its rows are returned as source=live.
func (s *Server) read(ctx context.Context, spec readSpec, dest any, live func() error) (envelope, error) {
	db, err := s.getCache()
	if err != nil {
		if live == nil {
			return envelope{}, err
		}
		if err := live(); err != nil {
			return envelope{}, err
		}
		return envelope{AsOf: time.Now().UTC(), Source: sourceLive, Note: "cache unavailable: " + err.Error()}, nil
	}

	meta, err := db.GetSyncMeta(spec.rt, spec.courseID)
	if err != nil {
		return envelope{}, fmt.Errorf("reading sync state: %w", err)
	}
	var syncErr error
	if s.needsRefresh(meta, spec) {
		// A failed refresh or a truncated one (rows stored, previous complete
		// set still served) both surface as sync_error on the envelope.
		syncErr = s.refresh(ctx, spec.rt, spec.courseID).Err
		meta, err = db.GetSyncMeta(spec.rt, spec.courseID)
		if err != nil {
			return envelope{}, fmt.Errorf("reading sync state: %w", err)
		}
	}

	if _, err := db.ListFresh(spec.rt, spec.courseID, dest); err != nil {
		return envelope{}, fmt.Errorf("reading cache: %w", err)
	}

	env := envelope{AsOf: meta.LastSyncAt.UTC(), Source: sourceCache, SyncStatus: meta.Status}
	if meta.LastSyncAt.IsZero() {
		// Nothing has ever completed for this (resource, course).
		if syncErr != nil {
			return envelope{}, syncErr
		}
		return envelope{}, errors.New("no cached data and the refresh produced none")
	}
	if meta.Status == cache.StatusSkipped {
		// Canvas said forbidden/not found for this resource of this course:
		// an empty set is the truthful answer, re-checked once per tier.
		env.Note = "this course does not expose this resource (Canvas returned forbidden/not found)"
		env.Stale = time.Since(meta.LastSyncAt) > spec.ttl
		return env, nil
	}
	if syncErr != nil {
		env.Stale = true
		env.SyncError = syncErr.Error()
	}
	if time.Since(meta.LastSyncAt) > spec.ttl || meta.Status != cache.StatusSuccess {
		env.Stale = true
	}
	return env, nil
}

// needsRefresh decides whether to hit Canvas before serving.
func (s *Server) needsRefresh(meta cache.SyncMeta, spec readSpec) bool {
	if spec.fresh {
		return true
	}
	if (meta.Status == cache.StatusFailed || meta.Status == cache.StatusSuspect) && time.Since(meta.LastAttemptAt) < backoffAfterFailure {
		// Backing off after a recorded failure, or after a truncated fetch
		// (a persistently truncating endpoint must not cost a request per
		// read; the previous complete set is served meanwhile).
		return false
	}
	if meta.LastSyncAt.IsZero() {
		return true // never synced
	}
	switch meta.Status {
	case cache.StatusSuccess, cache.StatusSkipped:
		// Skipped (403/404) is re-checked once per tier, not on every read,
		// so a course with a disabled tab does not cost a request per call.
		return time.Since(meta.LastSyncAt) > spec.ttl
	default:
		return true // suspect or failed: try to complete the set
	}
}

// refresh runs the sync job for (rt, courseID) exactly once per concurrent
// burst (singleflight); the sync layer records failures for backoff.
//
// The sync runs on a context detached from the caller's: the flight is
// shared by every caller in the burst, so the first caller giving up must
// not cancel it and stamp a context-canceled failure (and a 2-minute
// backoff) onto everyone else. A caller whose own context ends returns at
// once with that error while the refresh completes in the background.
func (s *Server) refresh(ctx context.Context, rt cache.ResourceType, courseID int64) syncer.Result {
	key := string(rt) + ":" + strconv.FormatInt(courseID, 10)
	ch := s.flight.DoChan(key, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		client, err := s.getClient()
		if err != nil {
			return syncer.Result{Status: cache.StatusFailed, Err: err}, nil
		}
		db, err := s.getCache()
		if err != nil {
			return syncer.Result{Status: cache.StatusFailed, Err: err}, nil
		}
		return syncer.SyncResource(fctx, client, db, rt, courseID), nil
	})
	select {
	case r := <-ch:
		res, _ := r.Val.(syncer.Result)
		return res
	case <-ctx.Done():
		return syncer.Result{Status: cache.StatusFailed, Err: ctx.Err()}
	}
}

// cachedCourses returns the course list from the cache (identity tier),
// syncing first if it has never been synced or is older than the tier.
func (s *Server) cachedCourses(ctx context.Context, ttl time.Duration, fresh bool) ([]canvas.Course, envelope, error) {
	var courses []canvas.Course
	env, err := s.read(ctx, readSpec{rt: cache.ResourceCourses, courseID: 0, ttl: ttl, fresh: fresh}, &courses, func() error {
		client, err := s.getClient()
		if err != nil {
			return err
		}
		all, err := collectIter(canvas.ListCourses(ctx, client, canvas.CourseListOptions{Include: []string{"enrollments", "total_scores"}}))
		courses = all
		return err
	})
	return courses, env, err
}

// findCourse resolves a course query like canvas.FindCourse but from the
// cached course list first, so nearly every tool call stops costing a live
// course listing. Same precedence: numeric ID, exact code, code substring,
// name substring. A miss falls back to the live resolver.
func (s *Server) findCourse(ctx context.Context, query string) (canvas.Course, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return canvas.Course{}, fmt.Errorf("course is required: %w", canvas.ErrNotFound)
	}
	if courses, _, err := s.cachedCourses(ctx, tierIdentity, false); err == nil {
		if c, ok := matchCourse(courses, query); ok {
			return c, nil
		}
	}
	client, err := s.getClient()
	if err != nil {
		return canvas.Course{}, err
	}
	return canvas.FindCourse(ctx, client, query)
}

func matchCourse(courses []canvas.Course, query string) (canvas.Course, bool) {
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		for _, c := range courses {
			if c.ID == id {
				return c, true
			}
		}
	}
	q := strings.ToLower(query)
	for _, c := range courses {
		if strings.EqualFold(c.CourseCode, query) {
			return c, true
		}
	}
	for _, c := range courses {
		if strings.Contains(strings.ToLower(c.CourseCode), q) {
			return c, true
		}
	}
	for _, c := range courses {
		if strings.Contains(strings.ToLower(c.Name), q) {
			return c, true
		}
	}
	return canvas.Course{}, false
}

// freshArg is the shared tool argument that bypasses the cache.
func freshArg() mcplib.ToolOption {
	return mcplib.WithBoolean("fresh",
		mcplib.Description("Bypass the cache and fetch from Canvas now (default false: serve from the local cache, refreshing it when older than this tool's freshness tier)"),
	)
}
