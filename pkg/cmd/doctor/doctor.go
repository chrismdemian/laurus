// Package doctor implements the doctor command for diagnosing issues.
package doctor

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/update"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

type checkStatus string

const (
	statusPass checkStatus = "PASS"
	statusWarn checkStatus = "WARN"
	statusFail checkStatus = "FAIL"
)

type checkResult struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
}

// NewCmdDoctor returns the doctor command.
func NewCmdDoctor(f *cmdutil.Factory) *cobra.Command {
	var benchmark bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common issues",
		Long: `Run diagnostic checks on your Laurus configuration, authentication, cache, and Canvas connectivity.

Use --benchmark to compare GraphQL vs REST API performance on your Canvas instance.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if benchmark {
				return benchmarkRun(f)
			}
			return doctorRun(f)
		},
	}

	cmd.Flags().BoolVar(&benchmark, "benchmark", false, "Benchmark GraphQL vs REST performance")

	return cmd
}

func doctorRun(f *cmdutil.Factory) error {
	ios := f.IOStreams()
	var results []checkResult

	// Styles for colored output
	passStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	nameStyle := lipgloss.NewStyle().Bold(true).Width(14)

	formatStatus := func(s checkStatus) string {
		if !ios.ColorEnabled {
			return fmt.Sprintf("[%s]", s)
		}
		switch s {
		case statusPass:
			return passStyle.Render(fmt.Sprintf("[%s]", s))
		case statusWarn:
			return warnStyle.Render(fmt.Sprintf("[%s]", s))
		case statusFail:
			return failStyle.Render(fmt.Sprintf("[%s]", s))
		}
		return fmt.Sprintf("[%s]", s)
	}

	printResult := func(r checkResult) {
		if !ios.IsJSON {
			_, _ = fmt.Fprintf(ios.Out, "  %s %s  %s\n", formatStatus(r.Status), nameStyle.Render(r.Name), r.Detail)
		}
	}

	// Header
	if !ios.IsJSON {
		_, _ = fmt.Fprintf(ios.Out, "Laurus %s (%s) built %s\n", f.Version, runtime.GOOS+"/"+runtime.GOARCH, runtime.Version())
		_, _ = fmt.Fprintln(ios.Out)
	}

	// 1. Config
	cfg, cfgErr := f.Config()
	if cfgErr != nil {
		r := checkResult{"Config", statusFail, fmt.Sprintf("Error: %s", cfgErr)}
		results = append(results, r)
		printResult(r)
	} else if cfg.CanvasURL == "" {
		configPath, _ := config.DefaultPath()
		r := checkResult{"Config", statusWarn, fmt.Sprintf("No Canvas URL configured (%s)", configPath)}
		results = append(results, r)
		printResult(r)
	} else {
		configPath, _ := config.DefaultPath()
		r := checkResult{"Config", statusPass, fmt.Sprintf("%s → %s", configPath, cfg.CanvasURL)}
		results = append(results, r)
		printResult(r)
	}

	// 2. Auth (skip if not configured)
	var token string
	if cfgErr == nil && cfg.CanvasURL != "" {
		td, authErr := f.Auth(cfg.CanvasURL)
		if authErr != nil {
			r := checkResult{"Auth", statusFail, fmt.Sprintf("Error: %s", authErr)}
			results = append(results, r)
			printResult(r)
		} else {
			token = td.Token
			if auth.IsExpiringSoon(td) {
				days := auth.DaysRemaining(td)
				r := checkResult{"Auth", statusWarn, fmt.Sprintf("Token expires in %d days", days)}
				results = append(results, r)
				printResult(r)
			} else {
				days := auth.DaysRemaining(td)
				detail := "Token valid"
				if days > 0 {
					detail = fmt.Sprintf("Token valid (%d days remaining)", days)
				} else if days == -1 {
					detail = "Token valid (expiry unknown)"
				}
				r := checkResult{"Auth", statusPass, detail}
				results = append(results, r)
				printResult(r)
			}
		}

		// 3. Connectivity + Rate limit (skip if auth failed)
		if token == "" {
			r := checkResult{"Connectivity", statusFail, "Skipped (auth required)"}
			results = append(results, r)
			printResult(r)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			client := canvas.NewClient(cfg.CanvasURL, token, f.Version)
			user, headers, connErr := canvas.GetWithHeaders[canvas.User](ctx, client, "/api/v1/users/self/profile", nil)
			if connErr != nil {
				r := checkResult{"Connectivity", statusFail, fmt.Sprintf("Error: %s", connErr)}
				results = append(results, r)
				printResult(r)
			} else {
				tz := ""
				if user.TimeZone != "" {
					tz = " (" + user.TimeZone + ")"
				}
				r := checkResult{"Connectivity", statusPass, fmt.Sprintf("%s @ %s%s", user.Name, cfg.CanvasURL, tz)}
				results = append(results, r)
				printResult(r)

				// Rate limit from response headers
				if rlStr := headers.Get("X-Rate-Limit-Remaining"); rlStr != "" {
					remaining, _ := strconv.ParseFloat(rlStr, 64)
					switch {
					case remaining < 10:
						r := checkResult{"Rate limit", statusFail, fmt.Sprintf("%.0f remaining (critically low)", remaining)}
						results = append(results, r)
						printResult(r)
					case remaining < 100:
						r := checkResult{"Rate limit", statusWarn, fmt.Sprintf("%.0f remaining", remaining)}
						results = append(results, r)
						printResult(r)
					default:
						r := checkResult{"Rate limit", statusPass, fmt.Sprintf("%.0f remaining", remaining)}
						results = append(results, r)
						printResult(r)
					}
				}
			}
		}
	}

	// 4. Cache
	db, cacheErr := f.Cache()
	if cacheErr != nil {
		r := checkResult{"Cache", statusWarn, fmt.Sprintf("Error: %s", cacheErr)}
		results = append(results, r)
		printResult(r)
	} else {
		counts, fileSize, statsErr := db.Stats()
		if statsErr != nil {
			r := checkResult{"Cache", statusWarn, fmt.Sprintf("Error reading stats: %s", statsErr)}
			results = append(results, r)
			printResult(r)
		} else {
			totalItems := 0
			for _, c := range counts {
				totalItems += c
			}
			if totalItems == 0 {
				r := checkResult{"Cache", statusWarn, "Empty — run 'laurus sync' to populate"}
				results = append(results, r)
				printResult(r)
			} else {
				r := checkResult{"Cache", statusPass, fmt.Sprintf("%d items, %s", totalItems, formatBytes(fileSize))}
				results = append(results, r)
				printResult(r)
			}
		}
	}

	// 5. Update check
	if f.Version != "dev" {
		cached := update.LoadCachedCheck()
		if cached != nil && cached.LatestVersion != "" {
			if cached.LatestVersion == f.Version || cached.CurrentVersion != f.Version {
				r := checkResult{"Version", statusPass, fmt.Sprintf("%s (latest)", f.Version)}
				results = append(results, r)
				printResult(r)
			} else {
				r := checkResult{"Version", statusWarn, fmt.Sprintf("%s (update available: %s)", f.Version, cached.LatestVersion)}
				results = append(results, r)
				printResult(r)
			}
		} else {
			r := checkResult{"Version", statusPass, fmt.Sprintf("%s (run 'laurus update' to check)", f.Version)}
			results = append(results, r)
			printResult(r)
		}
	}

	// JSON output — return early, skip human summary
	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, results)
	}

	_, _ = fmt.Fprintln(ios.Out)

	// Summary
	var warns, fails int
	for _, r := range results {
		switch r.Status {
		case statusWarn:
			warns++
		case statusFail:
			fails++
		}
	}
	if fails > 0 {
		_, _ = fmt.Fprintf(ios.ErrOut, "%d issue(s) found. See above for details.\n", fails+warns)
	} else if warns > 0 {
		_, _ = fmt.Fprintf(ios.ErrOut, "%d warning(s), but everything should work.\n", warns)
	} else {
		_, _ = fmt.Fprintln(ios.ErrOut, "All checks passed.")
	}

	return nil
}

func benchmarkRun(f *cmdutil.Factory) error {
	ios := f.IOStreams()

	cfg, err := f.Config()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.CanvasURL == "" {
		return fmt.Errorf("not configured; run 'laurus setup' first")
	}
	td, err := f.Auth(cfg.CanvasURL)
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Benchmarking GraphQL vs REST on %s...\n\n", cfg.CanvasURL)

	// Single client — GraphQL is always available; REST functions and GraphQL
	// functions are called independently for benchmarking.
	client := canvas.NewClient(cfg.CanvasURL, td.Token, f.Version)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type benchResult struct {
		Name     string
		REST     time.Duration
		GraphQL  time.Duration
		RESTErr  error
		GQLErr   error
		RESTRows int
		GQLRows  int
	}

	var results []benchResult

	// Benchmark 1: List courses
	{
		var r benchResult
		r.Name = "List courses + enrollments"

		start := time.Now()
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				r.RESTErr = err
				break
			}
			_ = c
			r.RESTRows++
		}
		r.REST = time.Since(start)

		start = time.Now()
		courses, gqlErr := canvas.QueryCourseSummariesGraphQL(ctx, client, canvas.GraphQLCourseListOptions{})
		r.GraphQL = time.Since(start)
		r.GQLErr = gqlErr
		r.GQLRows = len(courses)
		results = append(results, r)
	}

	// Benchmark 2: Dashboard assignments (all courses)
	{
		var r benchResult
		r.Name = "Dashboard assignments"

		start := time.Now()
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				r.RESTErr = err
				break
			}
			for a, err := range canvas.ListAssignments(ctx, client, c.ID, canvas.ListAssignmentsOptions{
				Bucket: "upcoming",
			}) {
				if err != nil {
					break
				}
				_ = a
				r.RESTRows++
			}
		}
		r.REST = time.Since(start)

		start = time.Now()
		dashCourses, gqlErr := canvas.QueryDashboardAssignmentsGraphQL(ctx, client)
		r.GraphQL = time.Since(start)
		r.GQLErr = gqlErr
		for _, dc := range dashCourses {
			r.GQLRows += len(dc.Assignments)
		}
		results = append(results, r)
	}

	// Benchmark 3: Grade breakdown for first course
	{
		var r benchResult
		r.Name = "Grade breakdown (1 course)"

		var firstCourseID int64
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				break
			}
			firstCourseID = c.ID
			break
		}

		if firstCourseID > 0 {
			start := time.Now()
			for ag, err := range canvas.ListAssignmentGroups(ctx, client, firstCourseID, []string{"assignments", "submission"}) {
				if err != nil {
					r.RESTErr = err
					break
				}
				r.RESTRows += len(ag.Assignments)
			}
			r.REST = time.Since(start)

			start = time.Now()
			groups, gqlErr := canvas.QueryCourseGradesGraphQL(ctx, client, firstCourseID)
			r.GraphQL = time.Since(start)
			r.GQLErr = gqlErr
			for _, g := range groups {
				r.GQLRows += len(g.Assignments)
			}
		} else {
			r.RESTErr = fmt.Errorf("no active courses")
			r.GQLErr = fmt.Errorf("no active courses")
		}
		results = append(results, r)
	}

	// Print results
	_, _ = fmt.Fprintf(ios.Out, "%-35s %12s %12s %8s %8s\n", "Query", "REST", "GraphQL", "REST #", "GQL #")
	_, _ = fmt.Fprintf(ios.Out, "%-35s %12s %12s %8s %8s\n", "-----", "----", "-------", "------", "-----")
	for _, r := range results {
		restStr := r.REST.Round(time.Millisecond).String()
		gqlStr := r.GraphQL.Round(time.Millisecond).String()
		if r.RESTErr != nil {
			restStr = "error"
		}
		if r.GQLErr != nil {
			gqlStr = "error"
		}
		_, _ = fmt.Fprintf(ios.Out, "%-35s %12s %12s %8d %8d\n", r.Name, restStr, gqlStr, r.RESTRows, r.GQLRows)
	}

	_, _ = fmt.Fprintln(ios.Out)

	// Check for errors
	for _, r := range results {
		if r.GQLErr != nil {
			_, _ = fmt.Fprintf(ios.ErrOut, "GraphQL error in %q: %v\n", r.Name, r.GQLErr)
		}
	}

	_, _ = fmt.Fprintln(ios.Out, "GraphQL is used automatically for grade queries (fastest path).")
	_, _ = fmt.Fprintln(ios.Out, "To disable GraphQL entirely: export LAURUS_DISABLE_GRAPHQL=1")

	return nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
