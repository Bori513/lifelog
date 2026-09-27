package questions

import "errors"

var (
	ErrNotFound              = errors.New("questions: not found")
	ErrInvalidLabel          = errors.New("questions: label must not be blank")
	ErrInvalidQuestionType   = errors.New("questions: invalid question type")
	ErrInvalidCalendarMarker = errors.New("questions: calendar marker is too long")
	ErrOptionsNotAllowed     = errors.New("questions: options are only allowed for select and multi-select questions")
	ErrInvalidReorder        = errors.New("questions: reorder must contain every active item exactly once")
)

const MaxCalendarMarkerRunes = 16

type QuestionType string

const (
	QuestionTypeShortText   QuestionType = "short_text"
	QuestionTypeLongText    QuestionType = "long_text"
	QuestionTypeWorkout     QuestionType = "workout"
	QuestionTypeBoolean     QuestionType = "boolean"
	QuestionTypeNumber      QuestionType = "number"
	QuestionTypeScale5      QuestionType = "scale_5"
	QuestionTypeScale10     QuestionType = "scale_10"
	QuestionTypeTime        QuestionType = "time"
	QuestionTypeSelect      QuestionType = "select"
	QuestionTypeMultiSelect QuestionType = "multi_select"
)

func (t QuestionType) valid() bool {
	switch t {
	case QuestionTypeShortText, QuestionTypeLongText, QuestionTypeWorkout, QuestionTypeBoolean,
		QuestionTypeNumber, QuestionTypeScale5, QuestionTypeScale10,
		QuestionTypeTime, QuestionTypeSelect, QuestionTypeMultiSelect:
		return true
	default:
		return false
	}
}

func (t QuestionType) allowsOptions() bool {
	return t == QuestionTypeSelect || t == QuestionTypeMultiSelect
}

type Question struct {
	ID             int64
	JournalID      int64
	Label          string
	CalendarMarker string
	Type           QuestionType
	Position       int
	IsActive       bool
	CreatedAt      string
	UpdatedAt      string
}

type QuestionOption struct {
	ID         int64
	QuestionID int64
	Label      string
	Position   int
	IsActive   bool
}

type CreateQuestionInput struct {
	Label          string
	Type           QuestionType
	CalendarMarker string
}

type RenameQuestionInput struct {
	Label          string
	CalendarMarker string
}

type ReorderQuestionsInput struct {
	IDs []int64
}

type CreateOptionInput struct {
	Label string
}

type RenameOptionInput struct {
	Label string
}

type ReorderOptionsInput struct {
	IDs []int64
}
