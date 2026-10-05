---
name: course-brief
description: Maintain one rolling brief.md per course from Canvas via the Laurus MCP so the agent reads a short file instead of re-reading syllabi, schedules, and announcements every session. Use when the user asks about a course, a deadline, a grade weight, or says to refresh/sync school info.
---

# Course brief

A brief is the agent's memory of a course. It replaces re-reading Canvas. The rule: **read the brief first, hit Canvas only for what the brief cannot answer, and write anything new back into the brief.**

## Layout

```
<school repo>/
  <term>/README.md            term overview: course table, weekly rhythm, term-wide dates
  <term>/<COURSE>/brief.md    one per course
  canvas/<COURSE>/            raw downloads (gitignored)
```

## What counts as a deadline
Anything with a date, wherever it appears: syllabus PDF, front page, a wiki page, an announcement, a module item, a lab handout, or the Canvas assignment feed. The assignment feed is just one source and is often empty; a quiz listed only in the syllabus is exactly as real. When asked what is due, list everything dated from the brief, graded first, and never describe syllabus-only items as informal or unofficial.

## Answering a question about a course

1. Read `<term>/<COURSE>/brief.md`. If it answers the question, stop. Do not call Canvas tools.
2. If the brief is silent, or its `Last refreshed` line is older than 3 days and the question is time-sensitive (due dates, announcements, grades), run a refresh (below) and answer from the updated brief.
3. Never re-read a syllabus PDF or a long Canvas page for a fact the brief already holds. If you had to read one, its content belongs in the brief.
4. Exception, study planning. If the question is what to read, which problems to do, or what a test covers, open the schedule source named in the brief's lecture-schedule heading once. Check the brief's table against it before building the plan, and fix the brief if the two differ. A plan built on a lossy summary inherits its errors.

## Refreshing a brief

Use the Laurus MCP tools (`canvas` server). Pull only what can have changed:

| Source | Tool | Goes into |
|---|---|---|
| Announcements | `list_announcements` (course_id, then `get_announcement` for bodies) | "Announcements log", and any dates/policy changes they carry into the tables |
| Assignments | `list_assignments` (course_id) | "Key dates" and marking tables |
| Modules / files | `list_modules` (every refresh, not only when an announcement mentions a file) | "Files" section. If any module file is not already listed in the brief's Files section, of any type (homework, lab handout, slides, not just syllabus-like), run `laurus download-all <id> -o canvas/<COURSE>`, then read the new file and move its dated items and requirements into the brief. An announcement summarising a handout is not a substitute for the handout: Homework 1 in ECE360 had 8 problems while its announcement listed 7 |
| Pages | `list_pages` / `get_page` | only pages named schedule, syllabus, evaluation, marks, office hours, project |
| Syllabus body | `get_course` (also returns `default_view` and `front_page`) | "Marking scheme", "Office hours", "Policies" |
| Front page | `get_front_page` (course_id). Courses with `default_view: wiki` keep the syllabus link, staff, dates, and weekly topics here even when Pages is disabled | everything above, plus "Weekly log" |
| Files linked from pages | `get_file` with the bare file id from the link (`/files/<id>`); works even when the course Files tab is hidden. CLI: `laurus download <course> <id> -o <path>` | "Files" section; download syllabus PDFs into `canvas/<COURSE>/` |
| Grades | `get_grades` | only when asked; do not store grades in the brief |

Then edit the brief in place:

- Update the `Last refreshed: YYYY-MM-DD (sources)` line.
- Add new announcements to "Announcements log" as `- YYYY-MM-DD summary`, newest last. Keep entries to one line.
- Move any date, weight, room, or policy change into the table it belongs in. Mark instructor-flagged tentative dates as **tentative**.
- Do not append raw text. Summarise. A brief should stay under ~150 lines.
- Schedules keep the source's granularity. A per-lecture schedule stays per lecture: lecture number, topic, and textbook sections exactly as the source gives them. Never merge lectures into weeks or widen section ranges. Name the source file in the table's heading so the table can be checked in one step.
- When the repo keeps lecture notes, add an "Actual coverage" line under the schedule. Say what the lectures really covered, which topics were not on the schedule, and which lecture days have no notes (a notes timeline flags those).
- Lab handouts and assignments: record the textbook sections and pages they cite. Translate them to the edition the course uses, because older handouts often cite older section numbers.
- If the term README's "Term-wide dates" is affected (a midterm moved), update it too.

## Brief structure

Every brief has these sections in this order. Omit a section only if the course truly has nothing for it, and say so ("not on Canvas yet").

1. Title line: `# CODE — Title`
2. `Last refreshed:` line, Canvas URL and course id, instructor + email, office hours, forum links
3. Schedule table (session, time, room)
4. Marking scheme table (component, weight, notes). Pass conditions in bold.
5. Key dates table, chronological
6. Policies (late, missed work, remark, email)
7. Textbook / materials / tools
8. Topics / lecture schedule (per lecture, with textbook sections and an "Actual coverage" line; see Refreshing)
9. Files (paths under `canvas/<COURSE>/`)
10. Announcements log
11. Notes (what is hidden on Canvas, what is TODO)

## Conventions

- Dates in America/Toronto local time. Canvas returns UTC; a `22:10Z` due time in September is 6:10 PM ET.
- Course ids: the number in the Canvas URL. Keep it in the brief so tools can be called without a lookup.
- Files tab may return 403 while module downloads and file-by-id downloads still work. Use the file id from the page link. Some ids are refused even then; note them in the brief and move on.
- Always check `default_view`. If it is `wiki`, the front page is the primary source and must be read on every refresh.
- Never write a Canvas token, cookie, or personal grade into a brief.
- Starting a new term: create `<term>/`, run a full pull for each course (syllabus, assignments, modules, pages, announcements, files), write briefs from scratch, then the term README.
