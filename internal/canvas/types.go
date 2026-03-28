package canvas

import "time"

// =============================================================================
// Core Types (Phase 2)
// =============================================================================

// Course represents a Canvas course.
type Course struct {
	ID                          int64        `json:"id"`
	Name                        string       `json:"name"`
	CourseCode                  string       `json:"course_code"`
	EnrollmentTermID            int64        `json:"enrollment_term_id"`
	StartAt                     *time.Time   `json:"start_at"`
	EndAt                       *time.Time   `json:"end_at"`
	TimeZone                    string       `json:"time_zone"`
	SyllabusBody                *string      `json:"syllabus_body"`
	Teachers                    []User       `json:"teachers"`
	WorkflowState               string       `json:"workflow_state"`
	TotalStudents               *int         `json:"total_students"`
	UUID                        string       `json:"uuid"`
	Enrollments                 []Enrollment `json:"enrollments"`
	HTMLURL                     string       `json:"html_url"`
	ApplyAssignmentGroupWeights bool         `json:"apply_assignment_group_weights"`
	GradingStandardID           *int64       `json:"grading_standard_id"`
}

// User represents a Canvas user profile.
type User struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	ShortName string  `json:"short_name"`
	Email     *string `json:"email"`
	AvatarURL string  `json:"avatar_url"`
	TimeZone  string  `json:"time_zone"`
	Pronouns  *string `json:"pronouns"`
}

// Enrollment represents a user's enrollment in a course.
type Enrollment struct {
	ID              int64             `json:"id"`
	UserID          int64             `json:"user_id"`
	CourseID        int64             `json:"course_id"`
	Type            string            `json:"type"`
	EnrollmentState string            `json:"enrollment_state"`
	Grades          *EnrollmentGrades `json:"grades"`

	// Computed fields — returned by GET /courses with include[]=total_scores.
	// These are top-level on the enrollment, NOT inside the grades sub-object.
	ComputedCurrentScore *float64 `json:"computed_current_score"`
	ComputedFinalScore   *float64 `json:"computed_final_score"`
	ComputedCurrentGrade *string  `json:"computed_current_grade"`
	ComputedFinalGrade   *string  `json:"computed_final_grade"`
}

// EnrollmentGrades holds computed grade data for an enrollment.
type EnrollmentGrades struct {
	CurrentScore *float64 `json:"current_score"`
	FinalScore   *float64 `json:"final_score"`
	CurrentGrade *string  `json:"current_grade"`
	FinalGrade   *string  `json:"final_grade"`
}

// Assignment represents a Canvas assignment.
type Assignment struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	Description        *string           `json:"description"`
	DueAt              *time.Time        `json:"due_at"`
	LockAt             *time.Time        `json:"lock_at"`
	UnlockAt           *time.Time        `json:"unlock_at"`
	PointsPossible     *float64          `json:"points_possible"`
	GradingType        string            `json:"grading_type"`
	SubmissionTypes    []string          `json:"submission_types"`
	AssignmentGroupID  int64             `json:"assignment_group_id"`
	Published          bool              `json:"published"`
	Rubric             []RubricCriterion `json:"rubric"`
	OmitFromFinalGrade bool              `json:"omit_from_final_grade"`
	CourseID           int64             `json:"course_id"`
	HTMLURL            string            `json:"html_url"`
	Position           int               `json:"position"`
	Submission         *Submission       `json:"submission"` // populated with include[]=submission
	Missing            bool              `json:"missing"`
	ScoreStatistics    *ScoreStatistics  `json:"score_statistics"` // populated with include[]=score_statistics
}

// ScoreStatistics holds class-wide score distribution for an assignment.
type ScoreStatistics struct {
	Mean   *float64 `json:"mean"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	Median *float64 `json:"median"`
	LowerQ *float64 `json:"lower_q"`
	UpperQ *float64 `json:"upper_q"`
}

// RubricCriterion represents one criterion in an assignment rubric.
type RubricCriterion struct {
	ID              string         `json:"id"`
	Description     string         `json:"description"`
	LongDescription string         `json:"long_description"`
	Points          float64        `json:"points"`
	Ratings         []RubricRating `json:"ratings"`
}

// RubricRating represents one possible rating within a rubric criterion.
type RubricRating struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Points      float64 `json:"points"`
}

// Submission represents a student's submission for an assignment.
type Submission struct {
	ID                 int64                            `json:"id"`
	AssignmentID       int64                            `json:"assignment_id"`
	UserID             int64                            `json:"user_id"`
	Score              *float64                         `json:"score"`
	Grade              *string                          `json:"grade"`
	SubmittedAt        *time.Time                       `json:"submitted_at"`
	WorkflowState      string                           `json:"workflow_state"`
	Late               bool                             `json:"late"`
	Missing            bool                             `json:"missing"`
	Excused            bool                             `json:"excused"`
	SubmissionComments []SubmissionComment              `json:"submission_comments"`
	RubricAssessment   map[string]RubricAssessmentEntry `json:"rubric_assessment"`
	PostedAt           *time.Time                       `json:"posted_at"`
	Attempt            *int                             `json:"attempt"`
}

// SubmissionComment represents a comment on a submission.
type SubmissionComment struct {
	ID        int64     `json:"id"`
	AuthorID  int64     `json:"author_id"`
	Author    string    `json:"author_name"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// RubricAssessmentEntry represents a grader's assessment for one rubric criterion.
type RubricAssessmentEntry struct {
	Points   *float64 `json:"points"`
	Comments *string  `json:"comments"`
	RatingID *string  `json:"rating_id"`
}

// AssignmentGroup represents a group of assignments with weighting and drop rules.
type AssignmentGroup struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	GroupWeight float64              `json:"group_weight"`
	Rules       AssignmentGroupRules `json:"rules"`
	Assignments []Assignment         `json:"assignments"` // populated with include[]=assignments
}

// AssignmentGroupRules controls drop behavior for a group.
type AssignmentGroupRules struct {
	DropLowest  int     `json:"drop_lowest"`
	DropHighest int     `json:"drop_highest"`
	NeverDrop   []int64 `json:"never_drop"`
}

// =============================================================================
// Phase 3+ Types (forward-declared for completeness)
// =============================================================================

// Module represents a Canvas course module.
type Module struct {
	ID                        int64      `json:"id"`
	Name                      string     `json:"name"`
	Position                  int        `json:"position"`
	UnlockAt                  *time.Time `json:"unlock_at"`
	RequireSequentialProgress bool       `json:"require_sequential_progress"`
	ItemsCount                int        `json:"items_count"`
	State                     *string    `json:"state"`
	CompletedAt               *time.Time `json:"completed_at"`
	PublishFinalGrade         bool       `json:"publish_final_grade"`
	Published                 bool       `json:"published"`
	ItemsURL                  string     `json:"items_url"`
}

// ModuleItem represents an item within a module.
type ModuleItem struct {
	ID                    int64                  `json:"id"`
	ModuleID              int64                  `json:"module_id"`
	Position              int                    `json:"position"`
	Title                 string                 `json:"title"`
	Type                  string                 `json:"type"`
	ContentID             int64                  `json:"content_id"`
	HTMLURL               string                 `json:"html_url"`
	URL                   *string                `json:"url"`
	PageURL               *string                `json:"page_url"`
	ExternalURL           *string                `json:"external_url"`
	CompletionRequirement *CompletionRequirement `json:"completion_requirement"`
}

// CompletionRequirement describes what must be done to complete a module item.
type CompletionRequirement struct {
	Type      string   `json:"type"`
	Completed bool     `json:"completed"`
	MinScore  *float64 `json:"min_score"`
}

// Page represents a Canvas wiki page.
type Page struct {
	PageID    int64     `json:"page_id"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Body      *string   `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Published bool      `json:"published"`
}

// File represents a Canvas file.
type File struct {
	ID          int64     `json:"id"`
	UUID        string    `json:"uuid"`
	FolderID    int64     `json:"folder_id"`
	DisplayName string    `json:"display_name"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content-type"`
	URL         string    `json:"url"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ModifiedAt  time.Time `json:"modified_at"`
}

// Folder represents a Canvas folder.
type Folder struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	FullName       string    `json:"full_name"`
	ParentFolderID *int64    `json:"parent_folder_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	FilesCount     int       `json:"files_count"`
	FoldersCount   int       `json:"folders_count"`
	FilesURL       string    `json:"files_url"`
	FoldersURL     string    `json:"folders_url"`
}

// Announcement represents a Canvas announcement (a special discussion topic).
type Announcement struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	PostedAt    time.Time `json:"posted_at"`
	Author      User      `json:"author"`
	ContextCode string    `json:"context_code"`
	ReadState   string    `json:"read_state"`
	HTMLURL     string    `json:"html_url"`
}

// DiscussionTopic represents a Canvas discussion topic.
type DiscussionTopic struct {
	ID                      int64      `json:"id"`
	Title                   string     `json:"title"`
	Message                 *string    `json:"message"`
	PostedAt                *time.Time `json:"posted_at"`
	LastReplyAt             *time.Time `json:"last_reply_at"`
	Author                  User       `json:"author"`
	DiscussionSubentryCount int        `json:"discussion_subentry_count"`
	ReadState               string     `json:"read_state"`
	UnreadCount             int        `json:"unread_count"`
	Pinned                  bool       `json:"pinned"`
	Published               bool       `json:"published"`
	HTMLURL                 string     `json:"html_url"`
	Locked                  bool       `json:"locked"`
}

// DiscussionEntry represents a reply in a discussion topic.
type DiscussionEntry struct {
	ID        int64             `json:"id"`
	UserID    int64             `json:"user_id"`
	UserName  string            `json:"user_name"`
	Message   string            `json:"message"`
	CreatedAt time.Time         `json:"created_at"`
	Replies   []DiscussionEntry `json:"replies"`
}

// DiscussionTopicView is the envelope returned by GET /courses/:id/discussion_topics/:id/view.
type DiscussionTopicView struct {
	Participants  []DiscussionParticipant `json:"participants"`
	UnreadEntries []int64                 `json:"unread_entries"`
	View          []DiscussionEntry       `json:"view"`
}

// DiscussionParticipant identifies a user in a discussion view.
type DiscussionParticipant struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

// Conversation represents a Canvas inbox conversation.
type Conversation struct {
	ID            int64                     `json:"id"`
	Subject       string                    `json:"subject"`
	WorkflowState string                    `json:"workflow_state"`
	LastMessage   string                    `json:"last_message"`
	LastMessageAt time.Time                 `json:"last_message_at"`
	MessageCount  int                       `json:"message_count"`
	Participants  []ConversationParticipant `json:"participants"`
	Starred       bool                      `json:"starred"`
	Messages      []ConversationMessage     `json:"messages,omitempty"`
}

// ConversationParticipant is a user in a conversation.
type ConversationParticipant struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ConversationMessage represents a single message in a conversation.
type ConversationMessage struct {
	ID        int64     `json:"id"`
	AuthorID  int64     `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// CalendarEvent represents a Canvas calendar event from /calendar_events.
type CalendarEvent struct {
	ID            int64      `json:"id"`
	Title         string     `json:"title"`
	StartAt       *time.Time `json:"start_at"`
	EndAt         *time.Time `json:"end_at"`
	Description   *string    `json:"description"`
	ContextCode   string     `json:"context_code"`
	WorkflowState string     `json:"workflow_state"`
	AllDay        bool       `json:"all_day"`
	HTMLURL       string     `json:"html_url"`
}

// UpcomingEvent represents an item from /users/self/upcoming_events.
// Canvas returns mixed types here: assignment events have string IDs
// like "assignment_500", so ID is omitted to avoid unmarshal errors.
type UpcomingEvent struct {
	Title      string      `json:"title"`
	Type       string      `json:"type"` // "event" or "assignment"
	StartAt    *time.Time  `json:"start_at"`
	EndAt      *time.Time  `json:"end_at"`
	HTMLURL    string      `json:"html_url"`
	Assignment *Assignment `json:"assignment"` // present for assignment-type events
}

// PlannerItem represents an item on the Canvas planner.
type PlannerItem struct {
	PlannerableID   int64      `json:"plannable_id"`
	PlannerableType string     `json:"plannable_type"`
	PlannerableDate *time.Time `json:"plannable_date"`
	ContextType     string     `json:"context_type"`
	ContextName     string     `json:"context_name"`
	CourseID        *int64     `json:"course_id"`
	HTMLURL         string     `json:"html_url"`
}

// PlannerNote represents a student-created planner note.
type PlannerNote struct {
	ID       int64      `json:"id"`
	Title    string     `json:"title"`
	Details  *string    `json:"details"`
	TodoDate *time.Time `json:"todo_date"`
	CourseID *int64     `json:"course_id"`
}

// TodoItem represents an item from the user's Canvas todo list.
type TodoItem struct {
	Type        string      `json:"type"` // "submitting" or "grading"
	Assignment  *Assignment `json:"assignment"`
	ContextType string      `json:"context_type"`
	ContextName string      `json:"context_name"`
	CourseID    *int64      `json:"course_id"`
	HTMLURL     string      `json:"html_url"`
}

// GradingStandard represents a grading scheme (e.g., A/B/C letter grades).
type GradingStandard struct {
	ID            int64                `json:"id"`
	Title         string               `json:"title"`
	GradingScheme []GradingSchemeEntry `json:"grading_scheme"`
}

// GradingSchemeEntry maps a letter grade to a minimum percentage cutoff.
type GradingSchemeEntry struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// GradingPeriod represents a time-bounded grading period within a term.
type GradingPeriod struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	StartDate time.Time  `json:"start_date"`
	EndDate   time.Time  `json:"end_date"`
	CloseDate *time.Time `json:"close_date"`
	Weight    *float64   `json:"weight"`
	IsClosed  bool       `json:"is_closed"`
}

// Rubric represents a standalone rubric definition.
type Rubric struct {
	ID             int64             `json:"id"`
	Title          string            `json:"title"`
	PointsPossible float64           `json:"points_possible"`
	Criteria       []RubricCriterion `json:"data"`
}
