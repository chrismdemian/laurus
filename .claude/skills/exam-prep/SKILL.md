---
name: exam-prep
description: Analyze past exams, problem sets, and course materials from Canvas to create a ranked study guide for any upcoming exam. Downloads all course content, identifies repeating question patterns, ranks question types by probability, maps practice problems, and provides generalized solving algorithms. Only works in the laurus project directory.
argument-hint: "<course name or ID> [optional: paths to supplementary exam PDFs separated by spaces]"
effort: high
---

# Exam Prep — Automated Study Guide Generator

Analyze a Canvas course's past exams, problem sets, term tests, and course materials to produce a prioritized study guide with question type rankings, practice problem mappings, and generalized solving algorithms.

**This skill requires the laurus CLI.** It will only work in the laurus project directory.

---

## Step 0: Validate Environment

1. Confirm we're in the laurus project directory (check for `main.go` or `go.mod` with module `laurus`).
2. Build laurus if no binary exists: `export PATH="$PATH:/c/Program Files/Go/bin" && go build -o laurus.exe .`
3. Verify auth: `./laurus.exe auth status` — if not logged in, tell the user to run `./laurus.exe setup`.

---

## Step 1: Parse Arguments

Parse `$ARGUMENTS` for:
- **Course identifier**: a course name (e.g., "ECE259"), course code, or numeric Canvas course ID
- **Supplementary files**: any file paths to additional exam PDFs not on Canvas (the user may paste absolute paths)

If no course is specified, list available courses with `./laurus.exe courses` and ask the user to pick one.

---

## Step 2: Find the Course

Run `./laurus.exe courses` and match the course identifier from arguments. Extract the **course ID** (numeric).

If the course name is ambiguous (e.g., both LEC and TUT sections exist), prefer the LEC section — it has the main course materials.

---

## Step 3: Download All Course Materials

Run:
```bash
./laurus.exe download-all <course_id> -o /tmp/exam_prep_<course_id>
```

This downloads all files from all modules via module item URLs (bypasses Quercus Files API 403 restriction). It organizes files into module-name folders.

Also fetch announcements:
```bash
./laurus.exe announcements <course_id>
```

After download, run `ls` on the output directory to inventory what's available. Categorize the downloaded files into:
- **Past final exams** (questions + solutions)
- **Term tests / midterms** (questions + solutions)
- **Problem sets** (problem statements + solutions + answer keys)
- **Syllabus / timetable**
- **Study guide / aid sheet**
- **Lecture notes** (note their existence but don't read them all)

Combine with any **supplementary files** the user provided in arguments.

---

## Step 4: Launch Parallel Analysis Agents

**CRITICAL: Context management.** Do NOT read large PDFs in the main conversation. Delegate ALL heavy reading to subagents. The main conversation should only receive structured summaries from agents.

### Agent Group A: Exam Analysis (one agent per final exam)

For each **final exam** (NOT term tests — those are for practice mapping later), launch a **separate Opus subagent** in the background:

```
Prompt template for each exam agent:
"You are analyzing a [COURSE] final exam from [YEAR]. Read the exam PDF (and solution PDF if available).

IMPORTANT: If reading scanned PDFs, use the Read tool with the `pages` parameter. Read at most 4-5 pages at a time (pages: "1-4", then "5-8", etc.) to avoid image dimension errors. Never read an entire large PDF at once.

For EVERY question on the exam, extract:
1. Question number and point value
2. Primary topic/concept tested
3. Sub-topic or specific skill
4. Brief description of what the problem asks (2-3 sentences)
5. Whether it involves: conceptual understanding, calculation, derivation, or proof
6. Specific geometry used (sphere, cylinder, parallel plate, coaxial, toroid, etc.)
7. Any notable techniques required (integration, boundary conditions, circuit analogy, etc.)

Also note:
- The exam format (total points, number of questions, time limit)
- The professor name if visible
- Whether a formula sheet / aid sheet was provided

Organize output as a structured summary table at the end.

File(s) to read: [PATHS]"
```

### Agent Group B: Syllabus & Study Materials (one agent)

Launch one **Opus subagent** to read the syllabus, study guide, and aid sheet:

```
"Read these course documents and extract:
1. SYLLABUS: topics covered in order, exam format/weight, professor name, any hints about final exam content
2. STUDY GUIDE: recommended study strategies, topic emphasis, any weighting hints
3. AID SHEET: what formulas/concepts are PROVIDED (this reveals what the exam tests — application, not memorization)
4. TIMETABLE: what topics are covered after the last midterm (these are ONLY tested on the final — expect heavy representation)

File(s): [PATHS]"
```

### Agent Group C: Problem Set Catalog (split into 2 agents if >6 sets)

**First check if an answer key / problem statement PDF exists** (text-based, not scanned). If it does, read it directly with bash `cat` — it's far more reliable than reading scanned solution PDFs.

If only scanned solution PDFs exist, launch Opus subagents with explicit page-chunking instructions:

```
"Catalog problem set solutions for [COURSE]. For EACH problem, extract:
1. Problem set number and problem number (include Conceptual, Workbook, and Problem types)
2. Topic/concept
3. Sub-topic
4. Brief description
5. Geometry involved
6. Difficulty level

CRITICAL: Read PDFs using the pages parameter — at most 3-4 pages at a time. Never read an entire PDF at once.

Files: [PATHS]"
```

Split problem sets roughly in half between two agents (e.g., PS 1-6 and PS 7-12) to parallelize.

### Agent Group D: Term Test Analysis (one agent for all term tests)

Launch one **Opus subagent** to analyze all term tests/midterms:

```
"Analyze these term tests for [COURSE]. For each question, extract the same fields as the exam analysis (topic, sub-topic, description, geometry, type, points). These will be used as additional practice problems.

Note which professor wrote each test and the year.

Files: [PATHS]"
```

**Launch all agents simultaneously** using parallel tool calls. Wait for all to complete.

---

## Step 5: Synthesize — Cross-Exam Pattern Analysis

Once all agents report back, **the main conversation synthesizes** the findings. This is the core analysis step.

### 5a: Build the Question Type Frequency Table

For each final exam, list every question's primary topic. Then count how many exams (out of N total) include each topic. Sort by frequency.

**Weighting:** More recent exams and exams by the same professor as the current year should be given slightly higher weight. Note if the professor is consistent across years — if so, the format is likely very stable.

### 5b: Rank Question Types

Create tiers:
- **TIER 1 (Near-certain)**: Appeared on 80%+ of exams (e.g., 4/5 or 5/5)
- **TIER 2 (Likely)**: Appeared on 50-79% of exams (e.g., 3/5)
- **TIER 3 (Possible)**: Appeared on 20-49% of exams (e.g., 1-2/5)

### 5c: Cross-reference with Syllabus

Identify topics that are **only tested on the final** (covered after the last midterm). These topics are guaranteed to appear since the final is the only chance to examine them. Flag them prominently.

---

## Step 6: Generate the Study Guide

Output a comprehensive study guide with this structure:

### Header
- Course name, exam date (if known), exam format, what's provided (aid sheet, calculator, etc.)

### For EACH question type (most probable to least probable):

```markdown
### TYPE N: [Topic Name] (appeared X/Y exams, ~Z points each)

**Probability tier:** [1/2/3]

**Exam appearances:**
| Year | Question | Geometry | Points | Professor |
|------|----------|----------|--------|-----------|

**Variations you'll encounter:**
- [Bullet list of the different ways this question has been asked]

**General Solving Algorithm:**
1. [Step 1 — generalizable to any variation]
2. [Step 2]
3. ...
[Include which equations to use for each variation, decision points, common pitfalls]

**Practice — Problem Sets:**
- [PS X, Problem Y: brief description — why it matches]
- [PS X, Workbook W: brief description]

**Practice — Term Tests:**
- [Year Term Test N, Q#: brief description — why it matches]

**Practice — Past Finals:**
- [Year Final Q#: good to do under timed conditions]
```

### Priority Study Plan

At the end, generate a **day-by-day study plan** based on how many days until the exam (ask if not known, default to 2 days):
- Morning/afternoon/evening blocks
- Highest-probability topics first
- End with a timed practice exam recommendation

### Key Strategic Insight

Always include a section highlighting which topics are **only tested on the final** and therefore guaranteed to appear with significant weight.

---

## Step 7: Handle Edge Cases

- **No past final exams on Canvas:** Use term tests as the primary pattern source. Note that predictions are less reliable. Still produce the analysis.
- **No problem sets:** Skip the problem set mapping. Map practice to term test questions and past exam questions only.
- **No syllabus/study guide:** Skip that section. Infer topic coverage from lecture note titles and exam content.
- **Only 1-2 past exams:** Note low sample size. Still analyze patterns but caveat that predictions are less confident. Lean more heavily on syllabus topic weighting.
- **Agent fails (image dimension error, timeout):** Re-launch with smaller page chunks (2-3 pages at a time) or try extracting text first with bash before reading as PDF.
- **download-all fails entirely:** Fall back to `./laurus.exe modules <course_id> --json` to get file URLs, then attempt direct download with curl using the Canvas token.

---

## Important Notes

- **Never read large PDFs in the main conversation.** Always delegate to subagents. The main conversation should only orchestrate and synthesize.
- **Use Opus model for all subagents** — the analysis requires judgment about question classification and pattern recognition.
- **Launch agents in parallel** whenever possible. Don't wait for one exam to finish before starting the next.
- **Text extraction is preferred over PDF reading** for scanned documents. If `download-all` produces `.txt` sidecar files, use those instead of the PDFs.
- **The answer key PDF** (if it exists) is often text-based even when solutions are scanned. Always check for it first — it catalogs every problem by topic.
- **Don't read lecture notes** unless specifically asked. They're too voluminous and the exam/problem set analysis is sufficient for pattern identification.
- **This skill produces study guidance, not academic dishonesty.** It analyzes publicly available past exams and course materials to help the student study efficiently.
