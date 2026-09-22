package browse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Bori513/lifelog/internal/questions"
)

const PageSize = 50

var ErrInvalidFilter = errors.New("browse: invalid filter")

type Operator string

const (
	OperatorEqual    Operator = "eq"
	OperatorGreater  Operator = "gt"
	OperatorAtLeast  Operator = "gte"
	OperatorLess     Operator = "lt"
	OperatorAtMost   Operator = "lte"
	OperatorBefore   Operator = "before"
	OperatorAfter    Operator = "after"
	OperatorContains Operator = "contains"
	OperatorFilled   Operator = "filled"
	OperatorEmpty    Operator = "empty"
)

type Filter struct {
	From, To   string
	QuestionID int64
	Operator   Operator
	Value      string
	OptionID   int64
	Page       int
}

type Question struct {
	ID      int64
	Label   string
	Type    questions.QuestionType
	Active  bool
	Options []Option
}

type Option struct {
	ID     int64
	Label  string
	Active bool
}

type Day struct {
	ID                                    int64
	EntryDate, GeneralNote, SpecialMoment string
	Location                              string
	HasPhotos                             bool
}

type Result struct {
	Days                 []Day
	Total, Page          int
	HasPrevious, HasNext bool
}

type MonthDay struct {
	EntryDate        string
	MatchesFilter    bool
	HasPhotos        bool
	HasSpecialMoment bool
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ListQuestions includes active questions and inactive questions with historical
// answers. Select options follow the same rule, preserving access to history.
func (s *Store) ListQuestions(ctx context.Context, journalID int64) ([]Question, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id, q.label, q.type, q.is_active
		FROM questions q
		WHERE q.journal_id = ? AND (q.is_active = 1 OR EXISTS (
			SELECT 1 FROM answers a JOIN days d ON d.id = a.day_id
			WHERE a.question_id = q.id AND d.journal_id = q.journal_id))
		ORDER BY q.position, q.id`, journalID)
	if err != nil {
		return nil, fmt.Errorf("list browse questions: %w", err)
	}
	defer rows.Close()
	var result []Question
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.Label, &q.Type, &q.Active); err != nil {
			return nil, fmt.Errorf("scan browse question: %w", err)
		}
		result = append(result, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list browse questions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close browse questions: %w", err)
	}
	for i := range result {
		if result[i].Type != questions.QuestionTypeSelect && result[i].Type != questions.QuestionTypeMultiSelect {
			continue
		}
		opts, err := s.listOptions(ctx, journalID, result[i].ID)
		if err != nil {
			return nil, err
		}
		result[i].Options = opts
	}
	return result, nil
}

func (s *Store) listOptions(ctx context.Context, journalID, questionID int64) ([]Option, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, o.label, o.is_active
		FROM question_options o JOIN questions q ON q.id = o.question_id
		WHERE q.journal_id = ? AND q.id = ? AND (o.is_active = 1 OR EXISTS (
			SELECT 1 FROM answer_options ao JOIN answers a ON a.id = ao.answer_id JOIN days d ON d.id = a.day_id
			WHERE ao.option_id = o.id AND d.journal_id = q.journal_id))
		ORDER BY o.position, o.id`, journalID, questionID)
	if err != nil {
		return nil, fmt.Errorf("list browse options: %w", err)
	}
	defer rows.Close()
	var result []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Label, &o.Active); err != nil {
			return nil, fmt.Errorf("scan browse option: %w", err)
		}
		result = append(result, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list browse options: %w", err)
	}
	return result, nil
}

func (s *Store) ListDays(ctx context.Context, journalID int64, f Filter) (Result, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if err := validateDateRange(f.From, f.To); err != nil {
		return Result{}, err
	}
	where := []string{"d.journal_id = ?"}
	args := []any{journalID}
	if f.From != "" {
		where = append(where, "d.entry_date >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "d.entry_date <= ?")
		args = append(args, f.To)
	}
	clause, clauseArgs, err := s.filterClause(ctx, journalID, f)
	if err != nil {
		return Result{}, err
	}
	if clause != "" {
		where = append(where, clause)
		args = append(args, clauseArgs...)
	}
	predicate := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM days d WHERE `+predicate, args...).Scan(&total); err != nil {
		return Result{}, fmt.Errorf("count browse days: %w", err)
	}
	queryArgs := append(append([]any(nil), args...), PageSize, (f.Page-1)*PageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.entry_date, d.general_note, d.special_moment, d.location,
		EXISTS(SELECT 1 FROM photos p WHERE p.day_id = d.id)
		FROM days d WHERE `+predicate+` ORDER BY d.entry_date DESC, d.id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return Result{}, fmt.Errorf("list browse days: %w", err)
	}
	defer rows.Close()
	result := Result{Total: total, Page: f.Page, HasPrevious: f.Page > 1}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.ID, &d.EntryDate, &d.GeneralNote, &d.SpecialMoment, &d.Location, &d.HasPhotos); err != nil {
			return Result{}, fmt.Errorf("scan browse day: %w", err)
		}
		result.Days = append(result.Days, d)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("list browse days: %w", err)
	}
	result.HasNext = f.Page*PageSize < total
	return result, nil
}

// ListMonthDays returns a compact projection for all saved days in one inclusive
// month range. A question condition marks matching days without hiding other
// saved days.
func (s *Store) ListMonthDays(ctx context.Context, journalID int64, from, to string, f Filter) ([]MonthDay, bool, error) {
	if err := validateDateRange(from, to); err != nil || from == "" || to == "" {
		if err != nil {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: month range", ErrInvalidFilter)
	}
	clause, args, err := s.filterClause(ctx, journalID, f)
	if err != nil {
		return nil, false, err
	}
	active := clause != ""
	matchSQL := "1"
	if active {
		matchSQL = clause
	}
	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, journalID, from, to)
	rows, err := s.db.QueryContext(ctx, `SELECT d.entry_date, (`+matchSQL+`),
		EXISTS(SELECT 1 FROM photos p WHERE p.day_id = d.id), trim(d.special_moment) <> ''
		FROM days d WHERE d.journal_id = ? AND d.entry_date >= ? AND d.entry_date <= ?
		ORDER BY d.entry_date`, queryArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("list calendar month days: %w", err)
	}
	defer rows.Close()
	var result []MonthDay
	for rows.Next() {
		var day MonthDay
		if err := rows.Scan(&day.EntryDate, &day.MatchesFilter, &day.HasPhotos, &day.HasSpecialMoment); err != nil {
			return nil, false, fmt.Errorf("scan calendar month day: %w", err)
		}
		result = append(result, day)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("list calendar month days: %w", err)
	}
	return result, active, nil
}

func (s *Store) filterClause(ctx context.Context, journalID int64, f Filter) (string, []any, error) {
	if f.QuestionID == 0 {
		if f.Operator != "" || f.Value != "" || f.OptionID != 0 {
			return "", nil, fmt.Errorf("%w: question required", ErrInvalidFilter)
		}
		return "", nil, nil
	}
	var kind questions.QuestionType
	err := s.db.QueryRowContext(ctx, `SELECT type FROM questions WHERE id = ? AND journal_id = ? AND (is_active = 1 OR EXISTS (SELECT 1 FROM answers a JOIN days ad ON ad.id = a.day_id WHERE a.question_id = questions.id AND ad.journal_id = questions.journal_id))`, f.QuestionID, journalID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf("%w: question", ErrInvalidFilter)
	}
	if err != nil {
		return "", nil, fmt.Errorf("read browse question: %w", err)
	}
	return s.questionClause(ctx, journalID, kind, f)
}

func validateDateRange(from, to string) error {
	for _, value := range []string{from, to} {
		if value == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil || parsed.Format("2006-01-02") != value {
			return fmt.Errorf("%w: date", ErrInvalidFilter)
		}
	}
	if from != "" && to != "" && from > to {
		return fmt.Errorf("%w: date range", ErrInvalidFilter)
	}
	return nil
}

func (s *Store) questionClause(ctx context.Context, journalID int64, kind questions.QuestionType, f Filter) (string, []any, error) {
	answer := `EXISTS (SELECT 1 FROM answers a WHERE a.day_id = d.id AND a.question_id = ? AND `
	switch kind {
	case questions.QuestionTypeBoolean:
		if f.Operator == "" || f.Value == "" {
			return "", nil, nil
		}
		if f.Operator != OperatorEqual || (f.Value != "true" && f.Value != "false") {
			return "", nil, fmt.Errorf("%w: boolean", ErrInvalidFilter)
		}
		return answer + `a.bool_value = ?)`, []any{f.QuestionID, f.Value == "true"}, nil
	case questions.QuestionTypeSelect, questions.QuestionTypeMultiSelect:
		if f.OptionID == 0 {
			return "", nil, nil
		}
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM question_options o JOIN questions q ON q.id=o.question_id WHERE o.id=? AND q.id=? AND q.journal_id=? AND (o.is_active=1 OR EXISTS(SELECT 1 FROM answer_options ao WHERE ao.option_id=o.id)))`, f.OptionID, f.QuestionID, journalID).Scan(&exists); err != nil {
			return "", nil, fmt.Errorf("check browse option: %w", err)
		}
		if !exists {
			return "", nil, fmt.Errorf("%w: option", ErrInvalidFilter)
		}
		return `EXISTS (SELECT 1 FROM answers a JOIN answer_options ao ON ao.answer_id = a.id WHERE a.day_id = d.id AND a.question_id = ? AND ao.option_id = ?)`, []any{f.QuestionID, f.OptionID}, nil
	case questions.QuestionTypeNumber, questions.QuestionTypeScale5, questions.QuestionTypeScale10:
		if f.Operator == "" && f.Value == "" {
			return "", nil, nil
		}
		op := map[Operator]string{OperatorEqual: "=", OperatorGreater: ">", OperatorAtLeast: ">=", OperatorLess: "<", OperatorAtMost: "<="}[f.Operator]
		value, err := strconv.ParseFloat(f.Value, 64)
		if op == "" || err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return "", nil, fmt.Errorf("%w: number", ErrInvalidFilter)
		}
		return answer + `a.number_value ` + op + ` ?)`, []any{f.QuestionID, value}, nil
	case questions.QuestionTypeTime:
		if f.Operator == "" && f.Value == "" {
			return "", nil, nil
		}
		op := map[Operator]string{OperatorEqual: "=", OperatorBefore: "<", OperatorAfter: ">"}[f.Operator]
		parsed, err := time.Parse("15:04", f.Value)
		if op == "" || err != nil || parsed.Format("15:04") != f.Value {
			return "", nil, fmt.Errorf("%w: time", ErrInvalidFilter)
		}
		return answer + `a.time_value ` + op + ` ?)`, []any{f.QuestionID, f.Value}, nil
	case questions.QuestionTypeShortText, questions.QuestionTypeLongText:
		switch f.Operator {
		case "":
			return "", nil, nil
		case OperatorContains:
			if strings.TrimSpace(f.Value) == "" {
				return "", nil, fmt.Errorf("%w: text", ErrInvalidFilter)
			}
			return answer + `instr(lower(a.text_value), lower(?)) > 0)`, []any{f.QuestionID, f.Value}, nil
		case OperatorFilled:
			return answer + `a.text_value IS NOT NULL AND trim(a.text_value) <> '')`, []any{f.QuestionID}, nil
		case OperatorEmpty:
			// Empty means no meaningful value: no row, NULL, or blank text.
			return `NOT EXISTS (SELECT 1 FROM answers a WHERE a.day_id = d.id AND a.question_id = ? AND a.text_value IS NOT NULL AND trim(a.text_value) <> '')`, []any{f.QuestionID}, nil
		default:
			return "", nil, fmt.Errorf("%w: text operator", ErrInvalidFilter)
		}
	default:
		return "", nil, fmt.Errorf("%w: question type", ErrInvalidFilter)
	}
}
