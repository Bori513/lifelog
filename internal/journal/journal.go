package journal

import "errors"

var (
	ErrInvalidDate       = errors.New("journal: invalid entry date")
	ErrJournalNotFound   = errors.New("journal: journal not found")
	ErrQuestionNotFound  = errors.New("journal: question not found or belongs to another journal")
	ErrInactiveQuestion  = errors.New("journal: inactive question cannot receive a new answer")
	ErrWrongAnswerType   = errors.New("journal: answer data does not match question type")
	ErrInvalidAnswer     = errors.New("journal: invalid answer")
	ErrInvalidOption     = errors.New("journal: invalid or inactive option")
	ErrDuplicateQuestion = errors.New("journal: duplicate question")
	ErrDuplicateOption   = errors.New("journal: duplicate option")
)

type Day struct {
	Exists        bool
	ID            int64
	JournalID     int64
	EntryDate     string
	GeneralNote   string
	SpecialMoment string
	Location      string
	CreatedAt     string
	UpdatedAt     string
	Answers       []Answer
}

type Answer struct {
	ID                    int64
	QuestionID            int64
	QuestionLabelSnapshot string
	TextValue             *string
	NumberValue           *float64
	BoolValue             *bool
	TimeValue             *string
	SelectedOptions       []SelectedOption
}

type SelectedOption struct {
	OptionID            int64
	OptionLabelSnapshot string
}

type SaveDayInput struct {
	GeneralNote   string
	SpecialMoment string
	Location      string
	Answers       []AnswerInput
}

// AnswerInput is present only for questions modified by this save. Clear removes
// an existing answer. Empty text and empty option selections also mean clear.
type AnswerInput struct {
	QuestionID  int64
	Clear       bool
	TextValue   *string
	NumberValue *float64
	BoolValue   *bool
	TimeValue   *string
	OptionIDs   []int64
}
