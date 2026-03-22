package models

import "time"

// LoginRequest represents the login request from frontend
type LoginRequest struct {
	Account  string `json:"account"`
	Email    string `json:"email"`
	Password string `json:"password"`
	CDigest  string `json:"cdigest,omitempty"`
	Captcha  string `json:"captcha,omitempty"`
}

// LoginResponse represents the login response to frontend
type LoginResponse struct {
	Success  bool      `json:"success"`
	UserId   string    `json:"X-User-Id,omitempty"`
	UserInfo *UserInfo `json:"userInfo,omitempty"`
	Tokens   string    `json:"tokens,omitempty"`
	Captcha  string    `json:"captcha,omitempty"` // base64 encoded captcha image
	CDigest  string    `json:"cdigest,omitempty"` // captcha digest for retry
	Error    string    `json:"error,omitempty"`
}

// DataRequest represents the shared payload for data-fetch endpoints
type DataRequest struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Password string `json:"password"`
	UserType string `json:"userType"`
}

// ActionResponse represents the standardized response for data endpoints
type ActionResponse struct {
	Success bool   `json:"success"`
	Reason  string `json:"reason,omitempty"`
}

// UserInfo represents user profile data
type UserInfo struct {
	Name           string `json:"name"`
	Mobile         string `json:"mobile"`
	Program        string `json:"program"`
	Semester       int    `json:"semester"`
	RegNumber      string `json:"regNumber"`
	Batch          string `json:"batch"`
	Year           int    `json:"year"`
	Department     string `json:"department"`
	Section        string `json:"section"`
	Specialization string `json:"specialization,omitempty"`
}

// AttendanceEntry represents a single attendance record extracted from HTML
type AttendanceEntry struct {
	CourseCode           string  `json:"courseCode"`
	CourseTitle          string  `json:"courseTitle"`
	Category             string  `json:"category"`
	Faculty              string  `json:"faculty"`
	Slot                 string  `json:"slot"`
	HoursConducted       float64 `json:"hoursConducted"`
	HoursAbsent          float64 `json:"hoursAbsent"`
	AttendancePercentage float64 `json:"attendancePercentage"`
}

// MarksAssessment represents an assessment entry within a course
type MarksAssessment struct {
	Name  string   `json:"name"`
	Score *float64 `json:"score"`
	Max   *float64 `json:"max"`
}

// MarksEntry represents the marks data for a single course
type MarksEntry struct {
	CourseCode  string            `json:"courseCode"`
	CourseTitle string            `json:"courseTitle"`
	Assessments []MarksAssessment `json:"assessments"`
	Total       *float64          `json:"total"`
}

// Course represents a single course
type Course struct {
	Code           string `json:"code"`
	Title          string `json:"title"`
	Credit         string `json:"credit"`
	Category       string `json:"category"`
	CourseCategory string `json:"courseCategory"`
	Type           string `json:"type"`
	SlotType       string `json:"slotType"`
	Faculty        string `json:"faculty"`
	Slot           string `json:"slot"`
	Room           string `json:"room"`
	AcademicYear   string `json:"academicYear"`
}

// CoursesData represents the complete courses data for caching
type CoursesData struct {
	RegNumber string   `json:"regNumber"`
	Courses   []Course `json:"courses"`
	Status    int      `json:"status"`
	Error     *string  `json:"error"`
}

// TimetableSlot represents a single slot in the timetable
type TimetableSlot struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	SlotType   string `json:"slotType"`
	Slot       string `json:"slot"`
	Online     bool   `json:"online"`
	RoomNo     string `json:"roomNo"`
	CourseType string `json:"courseType"`
	IsOptional bool   `json:"isOptional"`
}

// TimetableDay represents one day in the timetable
type TimetableDay struct {
	Day   int              `json:"day"`
	Table []*TimetableSlot `json:"table"`
}

// TimetableData represents the complete timetable data for caching
type TimetableData struct {
	RegNumber string         `json:"regNumber"`
	Batch     string         `json:"batch"`
	Schedule  []TimetableDay `json:"schedule"`
}

// CalendarDay represents a single day in the calendar
type CalendarDay struct {
	Date     int     `json:"date"`
	Day      string  `json:"day"`
	Event    *string `json:"event"`
	DayOrder string  `json:"day_order"`
}

// CalendarMonth represents a month in the calendar
type CalendarMonth struct {
	Month string        `json:"month"`
	Dates []CalendarDay `json:"dates"`
}

// CalendarData represents the complete calendar data (legacy nested format)
type CalendarData struct {
	Error    bool            `json:"error"`
	Message  *string         `json:"message"`
	Status   int             `json:"status"`
	Today    *CalendarDay    `json:"today"`
	Tomorrow *CalendarDay    `json:"tomorrow"`
	Index    int             `json:"index"`
	Calendar []CalendarMonth `json:"calendar"`
}

// NormalizedCalendarEntry represents a single normalized calendar entry
type NormalizedCalendarEntry struct {
	Date     string  `json:"date"`      // DD/MM/YYYY format
	DayName  string  `json:"day_name"`  // Mon, Tue, etc.
	Event    *string `json:"event"`     // Holiday name or null
	DayOrder string  `json:"day_order"` // Day 1, Day 2, etc.
	Month    string  `json:"month"`     // Jan, Feb, etc.
	Year     int     `json:"year"`      // 2025, 2026, etc.
}

// NormalizedCalendarData represents the complete normalized calendar data
type NormalizedCalendarData struct {
	Calendar []NormalizedCalendarEntry `json:"calendar"`
	Today    *NormalizedCalendarEntry  `json:"today,omitempty"`
	Tomorrow *NormalizedCalendarEntry  `json:"tomorrow,omitempty"`
}

// TokenData represents session tokens stored in Supabase
type TokenData struct {
	UserID          string    `json:"user_id"`
	Tokens          string    `json:"tokens"` // All cookies stored as string
	ExpiryTimestamp time.Time `json:"expiry_timestamp"`
	Email           string    `json:"email"`
}

// SuccessResponse is a generic success response
type SuccessResponse struct {
	Success bool `json:"success"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

// LookupResponse represents user lookup API response
type LookupResponse struct {
	Identifier string `json:"identifier"`
	Digest     string `json:"digest"`
}

// CaptchaResponse represents CAPTCHA API response
type CaptchaResponse struct {
	Captcha struct {
		ImageBytes string `json:"image_bytes"`
	} `json:"captcha"`
}

// Job represents a background job in the system
type Job struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	JobType       string     `json:"job_type"`  // 'login' or 'fetch'
	DataType      string     `json:"data_type"` // 'auth' for login, 'courses'/'timetable'/'calendar'/'user' for fetch
	Status        string     `json:"status"`    // 'pending', 'running', 'done', 'failed'
	Priority      int        `json:"priority"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	RetryCount    int        `json:"retry_count"`
	FailureReason *string    `json:"failure_reason,omitempty"`
	// Credentials for login jobs (not stored in DB, passed in memory)
	Email    string `json:"-"` // JSON omit
	Password string `json:"-"` // JSON omit
}

// JobCreateRequest represents the data needed to create a job
type JobCreateRequest struct {
	UserID             string
	JobType            string
	DataType           string
	Priority           int
	Email              string   // For login jobs
	Password           string   // For login jobs
	RequestedDataTypes []string // For login jobs - data types to fetch after login
}

// WorkerLoginRequest represents a login job with credentials for the worker
type WorkerLoginRequest struct {
	UserID             string
	Email              string
	Password           string
	Priority           int
	RequestedDataTypes []string
}

const (
	JobPriorityLowest = 1
)

const (
	AttendanceDataType = "attendance"
)

const (
	DeltaTypeNoChange   = "NO_CHANGE"
	DeltaTypeAddition   = "ADDITION"
	DeltaTypeCorrection = "CORRECTION"
	DeltaTypeMixed      = "MIXED"
)

const (
	TentativeStatusPending   = "PENDING"
	TentativeStatusReverted  = "REVERTED"
	TentativeStatusConfirmed = "CONFIRMED"
)

// AttendanceSnapshot represents a single ingestion of attendance counters for a course
type AttendanceSnapshot struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"userId"`
	Slot                 string    `json:"slot"`
	Faculty              string    `json:"faculty"`
	Category             string    `json:"category"`
	CourseCode           string    `json:"courseCode"`
	CourseTitle          string    `json:"courseTitle"`
	HoursAbsent          int       `json:"hoursAbsent"`
	HoursConducted       int       `json:"hoursConducted"`
	AttendancePercentage int       `json:"attendancePercentage"`
	FetchedAt            time.Time `json:"fetchedAt"`
	CreatedAt            time.Time `json:"createdAt"`
}

// AttendanceDelta captures the delta computed between snapshot pairs
type AttendanceDelta struct {
	ID                  string    `json:"id"`
	UserID              string    `json:"userId"`
	CourseCode          string    `json:"courseCode"`
	Slot                string    `json:"slot,omitempty"` // set in worker for logs / timetable match; not stored in attendance_deltas unless column exists
	PrevHoursAbsent     int       `json:"prevHoursAbsent"`
	PrevHoursConducted  int       `json:"prevHoursConducted"`
	CurrHoursAbsent     int       `json:"currHoursAbsent"`
	CurrHoursConducted  int       `json:"currHoursConducted"`
	DeltaHoursAbsent    int       `json:"deltaHoursAbsent"`
	DeltaHoursConducted int       `json:"deltaHoursConducted"`
	DeltaHoursPresent   int       `json:"deltaHoursPresent"`
	DeltaType           string    `json:"deltaType"`
	ComputedAt          time.Time `json:"computedAt"`
}

// TentativeAttendanceEvent represents an inference that can later be confirmed or reverted
type TentativeAttendanceEvent struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"userId"`
	CourseCode           string    `json:"courseCode"`
	InferredHoursAbsent  int       `json:"inferredHoursAbsent"`
	InferredHoursPresent int       `json:"inferredHoursPresent"`
	CandidateStartDate   time.Time `json:"candidateStartDate"`
	CandidateEndDate     time.Time `json:"candidateEndDate"`
	Status               string    `json:"status"` // PENDING | REVERTED | CONFIRMED
	InferredAt           time.Time `json:"inferredAt"`
	ExpiresAt            time.Time `json:"expiresAt"`
}

// FinalAttendanceEvent represents a confirmed attendance assignment
type FinalAttendanceEvent struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	CourseCode   string    `json:"courseCode"`
	CourseTitle  string    `json:"courseTitle"`
	Category     string    `json:"category"`
	ClassDate    time.Time `json:"classDate"`
	HoursAbsent  int       `json:"hoursAbsent"`
	HoursPresent int       `json:"hoursPresent"`
	FinalizedAt  time.Time `json:"finalizedAt"`
}

// CourseScheduleEntry represents a single scheduled slot for a course
type CourseScheduleEntry struct {
	CourseCode string `json:"courseCode"`
	Slot       string `json:"slot"`
	Weekday    int    `json:"weekday"`   // 1 = Monday, 7 = Sunday
	StartTime  string `json:"startTime"` // Stored as HH:MM:SS
	EndTime    string `json:"endTime"`
}

// AcademicCalendarEntry represents a holiday/non-holiday entry
type AcademicCalendarEntry struct {
	Date        time.Time `json:"date"`
	IsHoliday   bool      `json:"isHoliday"`
	Description *string   `json:"description"`
}

// UserRecord is a lightweight representation of users stored in Supabase
type UserRecord struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}
