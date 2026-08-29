package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Bori513/lifelog/internal/questions"
	searchindex "github.com/Bori513/lifelog/internal/search"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func (s *Store) GetDay(ctx context.Context, journalID int64, entryDate string) (Day, error) {
	if !validDate(entryDate) {
		return Day{}, ErrInvalidDate
	}
	var journalExists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM journals WHERE id = ?)`, journalID).Scan(&journalExists); err != nil {
		return Day{}, fmt.Errorf("check journal: %w", err)
	}
	if !journalExists {
		return Day{}, ErrJournalNotFound
	}
	day := Day{JournalID: journalID, EntryDate: entryDate}
	err := s.db.QueryRowContext(ctx, `SELECT id, general_note, special_moment, location, created_at, updated_at FROM days WHERE journal_id = ? AND entry_date = ?`, journalID, entryDate).
		Scan(&day.ID, &day.GeneralNote, &day.SpecialMoment, &day.Location, &day.CreatedAt, &day.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return day, nil
	}
	if err != nil {
		return Day{}, fmt.Errorf("read day: %w", err)
	}
	day.Exists = true
	rows, err := s.db.QueryContext(ctx, `SELECT id, question_id, text_value, number_value, bool_value, time_value, question_label_snapshot FROM answers WHERE day_id = ? ORDER BY question_id`, day.ID)
	if err != nil {
		return Day{}, fmt.Errorf("read answers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a Answer
		var textValue, timeValue sql.NullString
		var numberValue sql.NullFloat64
		var boolValue sql.NullBool
		if err := rows.Scan(&a.ID, &a.QuestionID, &textValue, &numberValue, &boolValue, &timeValue, &a.QuestionLabelSnapshot); err != nil {
			return Day{}, fmt.Errorf("scan answer: %w", err)
		}
		if textValue.Valid {
			a.TextValue = &textValue.String
		}
		if numberValue.Valid {
			a.NumberValue = &numberValue.Float64
		}
		if boolValue.Valid {
			a.BoolValue = &boolValue.Bool
		}
		if timeValue.Valid {
			a.TimeValue = &timeValue.String
		}
		day.Answers = append(day.Answers, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Day{}, fmt.Errorf("read answers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Day{}, fmt.Errorf("close answers: %w", err)
	}
	for i := range day.Answers {
		opts, err := s.readOptions(ctx, day.Answers[i].ID)
		if err != nil {
			return Day{}, err
		}
		day.Answers[i].SelectedOptions = opts
	}
	return day, nil
}

func (s *Store) readOptions(ctx context.Context, answerID int64) ([]SelectedOption, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT option_id, option_label_snapshot FROM answer_options WHERE answer_id = ? ORDER BY option_id`, answerID)
	if err != nil {
		return nil, fmt.Errorf("read selected options: %w", err)
	}
	defer rows.Close()
	var result []SelectedOption
	for rows.Next() {
		var o SelectedOption
		if err := rows.Scan(&o.OptionID, &o.OptionLabelSnapshot); err != nil {
			return nil, fmt.Errorf("scan selected option: %w", err)
		}
		result = append(result, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read selected options: %w", err)
	}
	return result, nil
}

type questionState struct {
	id              int64
	label           string
	kind            questions.QuestionType
	active          bool
	answerID        int64
	hasAnswer       bool
	existingOptions map[int64]struct{}
}
type optionState struct {
	id     int64
	label  string
	active bool
}

func (s *Store) SaveDay(ctx context.Context, journalID int64, entryDate string, input SaveDayInput) (Day, error) {
	if !validDate(entryDate) {
		return Day{}, ErrInvalidDate
	}
	seen := make(map[int64]struct{}, len(input.Answers))
	for _, answer := range input.Answers {
		if _, ok := seen[answer.QuestionID]; ok {
			return Day{}, ErrDuplicateQuestion
		}
		seen[answer.QuestionID] = struct{}{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Day{}, fmt.Errorf("begin day save: %w", err)
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM journals WHERE id = ?)`, journalID).Scan(&exists); err != nil {
		return Day{}, fmt.Errorf("check journal: %w", err)
	}
	if !exists {
		return Day{}, ErrJournalNotFound
	}
	var dayID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM days WHERE journal_id = ? AND entry_date = ?`, journalID, entryDate).Scan(&dayID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Day{}, fmt.Errorf("find day: %w", err)
	}
	for _, answer := range input.Answers {
		state, err := loadQuestionState(ctx, tx, journalID, dayID, answer.QuestionID)
		if err != nil {
			return Day{}, err
		}
		if err := validateAnswer(ctx, tx, state, answer); err != nil {
			return Day{}, err
		}
	}
	if dayID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO days (journal_id, entry_date, general_note, special_moment, location, created_at, updated_at) VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) RETURNING id`, journalID, entryDate, input.GeneralNote, input.SpecialMoment, input.Location).Scan(&dayID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE days SET general_note = ?, special_moment = ?, location = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, input.GeneralNote, input.SpecialMoment, input.Location, dayID)
	}
	if err != nil {
		return Day{}, fmt.Errorf("write day: %w", err)
	}
	for _, answer := range input.Answers {
		state, err := loadQuestionState(ctx, tx, journalID, dayID, answer.QuestionID)
		if err != nil {
			return Day{}, err
		}
		if isClear(state.kind, answer) {
			if state.hasAnswer {
				if _, err := tx.ExecContext(ctx, `DELETE FROM answers WHERE id = ?`, state.answerID); err != nil {
					return Day{}, fmt.Errorf("clear answer: %w", err)
				}
			}
			continue
		}
		if !state.hasAnswer {
			err = tx.QueryRowContext(ctx, `INSERT INTO answers (day_id, question_id, text_value, number_value, bool_value, time_value, question_label_snapshot, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) RETURNING id`, dayID, answer.QuestionID, answer.TextValue, answer.NumberValue, answer.BoolValue, answer.TimeValue, state.label).Scan(&state.answerID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE answers SET text_value = ?, number_value = ?, bool_value = ?, time_value = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, answer.TextValue, answer.NumberValue, answer.BoolValue, answer.TimeValue, state.answerID)
		}
		if err != nil {
			return Day{}, fmt.Errorf("write answer: %w", err)
		}
		if state.kind == questions.QuestionTypeSelect || state.kind == questions.QuestionTypeMultiSelect {
			if _, err := tx.ExecContext(ctx, `DELETE FROM answer_options WHERE answer_id = ? AND option_id NOT IN (`+placeholders(len(answer.OptionIDs))+`)`, append([]any{state.answerID}, int64Args(answer.OptionIDs)...)...); err != nil {
				return Day{}, fmt.Errorf("remove selected options: %w", err)
			}
			for _, id := range answer.OptionIDs {
				if _, old := state.existingOptions[id]; old {
					continue
				}
				var label string
				if err := tx.QueryRowContext(ctx, `SELECT label FROM question_options WHERE id = ?`, id).Scan(&label); err != nil {
					return Day{}, fmt.Errorf("read option label: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO answer_options (answer_id, option_id, option_label_snapshot) VALUES (?, ?, ?)`, state.answerID, id, label); err != nil {
					return Day{}, fmt.Errorf("select option: %w", err)
				}
			}
		}
	}
	if err := searchindex.RebuildDay(ctx, tx, dayID); err != nil {
		return Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return Day{}, fmt.Errorf("commit day save: %w", err)
	}
	return s.GetDay(ctx, journalID, entryDate)
}

func loadQuestionState(ctx context.Context, tx *sql.Tx, journalID, dayID, questionID int64) (questionState, error) {
	var q questionState
	err := tx.QueryRowContext(ctx, `SELECT id, label, type, is_active FROM questions WHERE id = ? AND journal_id = ?`, questionID, journalID).Scan(&q.id, &q.label, &q.kind, &q.active)
	if errors.Is(err, sql.ErrNoRows) {
		return q, ErrQuestionNotFound
	}
	if err != nil {
		return q, fmt.Errorf("read question: %w", err)
	}
	q.existingOptions = map[int64]struct{}{}
	if dayID != 0 {
		err = tx.QueryRowContext(ctx, `SELECT id FROM answers WHERE day_id = ? AND question_id = ?`, dayID, questionID).Scan(&q.answerID)
		q.hasAnswer = err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return q, fmt.Errorf("read existing answer: %w", err)
		}
		if q.hasAnswer {
			rows, err := tx.QueryContext(ctx, `SELECT option_id FROM answer_options WHERE answer_id = ?`, q.answerID)
			if err != nil {
				return q, fmt.Errorf("read existing options: %w", err)
			}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return q, err
				}
				q.existingOptions[id] = struct{}{}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return q, fmt.Errorf("read existing options: %w", err)
			}
			if err := rows.Close(); err != nil {
				return q, err
			}
		}
	}
	return q, nil
}

func validateAnswer(ctx context.Context, tx *sql.Tx, q questionState, a AnswerInput) error {
	if a.Clear {
		if a.TextValue != nil || a.NumberValue != nil || a.BoolValue != nil || a.TimeValue != nil || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
		return nil
	}
	if isClear(q.kind, a) {
		return nil
	}
	if !q.active && !q.hasAnswer {
		return ErrInactiveQuestion
	}
	scalarCount := 0
	if a.TextValue != nil {
		scalarCount++
	}
	if a.NumberValue != nil {
		scalarCount++
	}
	if a.BoolValue != nil {
		scalarCount++
	}
	if a.TimeValue != nil {
		scalarCount++
	}
	switch q.kind {
	case questions.QuestionTypeShortText, questions.QuestionTypeLongText:
		if a.TextValue == nil || scalarCount != 1 || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
	case questions.QuestionTypeBoolean:
		if a.BoolValue == nil || scalarCount != 1 || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
	case questions.QuestionTypeNumber:
		if a.NumberValue == nil || scalarCount != 1 || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
		if math.IsNaN(*a.NumberValue) || math.IsInf(*a.NumberValue, 0) {
			return ErrInvalidAnswer
		}
	case questions.QuestionTypeScale5, questions.QuestionTypeScale10:
		if a.NumberValue == nil || scalarCount != 1 || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
		max := float64(5)
		if q.kind == questions.QuestionTypeScale10 {
			max = 10
		}
		if *a.NumberValue < 1 || *a.NumberValue > max || math.Trunc(*a.NumberValue) != *a.NumberValue {
			return ErrInvalidAnswer
		}
	case questions.QuestionTypeTime:
		if a.TimeValue == nil || scalarCount != 1 || len(a.OptionIDs) != 0 {
			return ErrWrongAnswerType
		}
		parsed, err := time.Parse("15:04", *a.TimeValue)
		if err != nil || parsed.Format("15:04") != *a.TimeValue {
			return ErrInvalidAnswer
		}
	case questions.QuestionTypeSelect, questions.QuestionTypeMultiSelect:
		if scalarCount != 0 {
			return ErrWrongAnswerType
		}
		if q.kind == questions.QuestionTypeSelect && len(a.OptionIDs) > 1 {
			return ErrInvalidAnswer
		}
		seen := map[int64]struct{}{}
		for _, id := range a.OptionIDs {
			if _, ok := seen[id]; ok {
				return ErrDuplicateOption
			}
			seen[id] = struct{}{}
			var o optionState
			err := tx.QueryRowContext(ctx, `SELECT id, label, is_active FROM question_options WHERE id = ? AND question_id = ?`, id, q.id).Scan(&o.id, &o.label, &o.active)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidOption
			}
			if err != nil {
				return fmt.Errorf("read option: %w", err)
			}
			_, historical := q.existingOptions[id]
			if !o.active && !historical {
				return ErrInvalidOption
			}
		}
	default:
		return ErrWrongAnswerType
	}
	return nil
}

func isClear(kind questions.QuestionType, a AnswerInput) bool {
	if a.Clear {
		return true
	}
	if (kind == questions.QuestionTypeShortText || kind == questions.QuestionTypeLongText) && a.TextValue != nil && *a.TextValue == "" {
		return true
	}
	return (kind == questions.QuestionTypeSelect || kind == questions.QuestionTypeMultiSelect) && len(a.OptionIDs) == 0
}
func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	result := "?"
	for i := 1; i < n; i++ {
		result += ",?"
	}
	return result
}
func int64Args(ids []int64) []any {
	result := make([]any, len(ids))
	for i, id := range ids {
		result[i] = id
	}
	return result
}
