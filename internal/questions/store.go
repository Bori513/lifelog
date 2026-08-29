package questions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateQuestion(ctx context.Context, journalID int64, input CreateQuestionInput) (Question, error) {
	label, err := validLabel(input.Label)
	if err != nil {
		return Question{}, err
	}
	if !input.Type.valid() {
		return Question{}, fmt.Errorf("%w: %q", ErrInvalidQuestionType, input.Type)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Question{}, fmt.Errorf("begin question creation: %w", err)
	}
	defer tx.Rollback()

	var position int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM questions WHERE journal_id = ?`, journalID).Scan(&position)
	if err != nil {
		return Question{}, fmt.Errorf("find question position: %w", err)
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO questions (journal_id, label, type, position, is_active, created_at, updated_at)
		SELECT id, ?, ?, ?, 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		FROM journals WHERE id = ?
		RETURNING id, journal_id, label, type, position, is_active, created_at, updated_at`,
		label, input.Type, position, journalID)
	question, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("create question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Question{}, fmt.Errorf("commit question creation: %w", err)
	}
	return question, nil
}

func (s *Store) ListQuestions(ctx context.Context, journalID int64, includeInactive bool) ([]Question, error) {
	query := `SELECT id, journal_id, label, type, position, is_active, created_at, updated_at FROM questions WHERE journal_id = ?`
	if !includeInactive {
		query += ` AND is_active = 1`
	}
	query += ` ORDER BY position ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, journalID)
	if err != nil {
		return nil, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()

	var result []Question
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}
		result = append(result, question)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list questions: %w", err)
	}
	return result, nil
}

func (s *Store) RenameQuestion(ctx context.Context, journalID, questionID int64, input RenameQuestionInput) error {
	label, err := validLabel(input.Label)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE questions SET label = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND journal_id = ?`, label, questionID, journalID)
	if err != nil {
		return fmt.Errorf("rename question: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) DeactivateQuestion(ctx context.Context, journalID, questionID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE questions SET is_active = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND journal_id = ?`, questionID, journalID)
	if err != nil {
		return fmt.Errorf("deactivate question: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) ReactivateQuestion(ctx context.Context, journalID, questionID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin question reactivation: %w", err)
	}
	defer tx.Rollback()

	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT is_active FROM questions WHERE id = ? AND journal_id = ?`, questionID, journalID).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("find question for reactivation: %w", err)
	}
	if !active {
		var position int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM questions WHERE journal_id = ? AND is_active = 1`, journalID).Scan(&position); err != nil {
			return fmt.Errorf("find reactivated question position: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE questions SET is_active = 1, position = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND journal_id = ?`, position, questionID, journalID); err != nil {
			return fmt.Errorf("reactivate question: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit question reactivation: %w", err)
	}
	return nil
}

func (s *Store) ReorderQuestions(ctx context.Context, journalID int64, input ReorderQuestionsInput) error {
	return s.reorder(ctx, `SELECT id FROM questions WHERE journal_id = ? AND is_active = 1`, `UPDATE questions SET position = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND journal_id = ? AND is_active = 1`, journalID, input.IDs, "questions")
}

func (s *Store) CreateOption(ctx context.Context, journalID, questionID int64, input CreateOptionInput) (QuestionOption, error) {
	label, err := validLabel(input.Label)
	if err != nil {
		return QuestionOption{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return QuestionOption{}, fmt.Errorf("begin option creation: %w", err)
	}
	defer tx.Rollback()

	var questionType QuestionType
	if err := tx.QueryRowContext(ctx, `SELECT type FROM questions WHERE id = ? AND journal_id = ?`, questionID, journalID).Scan(&questionType); errors.Is(err, sql.ErrNoRows) {
		return QuestionOption{}, ErrNotFound
	} else if err != nil {
		return QuestionOption{}, fmt.Errorf("find option question: %w", err)
	}
	if !questionType.allowsOptions() {
		return QuestionOption{}, ErrOptionsNotAllowed
	}
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM question_options WHERE question_id = ?`, questionID).Scan(&position); err != nil {
		return QuestionOption{}, fmt.Errorf("find option position: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO question_options (question_id, label, position, is_active) VALUES (?, ?, ?, 1)`, questionID, label, position)
	if err != nil {
		return QuestionOption{}, fmt.Errorf("create option: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return QuestionOption{}, fmt.Errorf("read created option ID: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return QuestionOption{}, fmt.Errorf("commit option creation: %w", err)
	}
	return QuestionOption{ID: id, QuestionID: questionID, Label: label, Position: position, IsActive: true}, nil
}

func (s *Store) ListOptions(ctx context.Context, journalID, questionID int64, includeInactive bool) ([]QuestionOption, error) {
	query := `SELECT o.id, o.question_id, o.label, o.position, o.is_active FROM question_options o JOIN questions q ON q.id = o.question_id WHERE q.journal_id = ? AND q.id = ?`
	if !includeInactive {
		query += ` AND o.is_active = 1`
	}
	query += ` ORDER BY o.position ASC, o.id ASC`
	rows, err := s.db.QueryContext(ctx, query, journalID, questionID)
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	defer rows.Close()
	var result []QuestionOption
	for rows.Next() {
		var option QuestionOption
		if err := rows.Scan(&option.ID, &option.QuestionID, &option.Label, &option.Position, &option.IsActive); err != nil {
			return nil, fmt.Errorf("scan option: %w", err)
		}
		result = append(result, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	return result, nil
}

func (s *Store) RenameOption(ctx context.Context, journalID, questionID, optionID int64, input RenameOptionInput) error {
	label, err := validLabel(input.Label)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE question_options SET label = ? WHERE id = ? AND question_id = ? AND EXISTS (SELECT 1 FROM questions WHERE id = ? AND journal_id = ?)`, label, optionID, questionID, questionID, journalID)
	if err != nil {
		return fmt.Errorf("rename option: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) DeactivateOption(ctx context.Context, journalID, questionID, optionID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE question_options SET is_active = 0 WHERE id = ? AND question_id = ? AND EXISTS (SELECT 1 FROM questions WHERE id = ? AND journal_id = ?)`, optionID, questionID, questionID, journalID)
	if err != nil {
		return fmt.Errorf("deactivate option: %w", err)
	}
	return requireChanged(result)
}

func (s *Store) ReactivateOption(ctx context.Context, journalID, questionID, optionID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin option reactivation: %w", err)
	}
	defer tx.Rollback()
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT o.is_active FROM question_options o JOIN questions q ON q.id = o.question_id WHERE o.id = ? AND o.question_id = ? AND q.journal_id = ?`, optionID, questionID, journalID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find option for reactivation: %w", err)
	}
	if !active {
		var position int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM question_options WHERE question_id = ? AND is_active = 1`, questionID).Scan(&position); err != nil {
			return fmt.Errorf("find reactivated option position: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE question_options SET is_active = 1, position = ? WHERE id = ? AND question_id = ?`, position, optionID, questionID); err != nil {
			return fmt.Errorf("reactivate option: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit option reactivation: %w", err)
	}
	return nil
}

func (s *Store) ReorderOptions(ctx context.Context, journalID, questionID int64, input ReorderOptionsInput) error {
	return s.reorder(ctx, `SELECT o.id FROM question_options o JOIN questions q ON q.id = o.question_id WHERE q.journal_id = ? AND o.question_id = ? AND o.is_active = 1`, `UPDATE question_options SET position = ? WHERE id = ? AND question_id = ? AND is_active = 1`, []int64{journalID, questionID}, input.IDs, "options")
}

func (s *Store) reorder(ctx context.Context, selectSQL, updateSQL string, parent any, ids []int64, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s reorder: %w", name, err)
	}
	defer tx.Rollback()

	args := []any{parent}
	if values, ok := parent.([]int64); ok {
		args = make([]any, len(values))
		for i := range values {
			args[i] = values[i]
		}
	}
	rows, err := tx.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return fmt.Errorf("read active %s: %w", name, err)
	}
	active := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan active %s: %w", name, err)
		}
		active[id] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close active %s rows: %w", name, err)
	}
	if len(ids) != len(active) {
		return ErrInvalidReorder
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return ErrInvalidReorder
		}
		if _, exists := active[id]; !exists {
			return ErrInvalidReorder
		}
		seen[id] = struct{}{}
	}
	for position, id := range ids {
		updateArgs := []any{position, id, parent}
		if values, ok := parent.([]int64); ok {
			updateArgs = []any{position, id, values[len(values)-1]}
		}
		if _, err := tx.ExecContext(ctx, updateSQL, updateArgs...); err != nil {
			return fmt.Errorf("reorder %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s reorder: %w", name, err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanQuestion(row scanner) (Question, error) {
	var question Question
	err := row.Scan(&question.ID, &question.JournalID, &question.Label, &question.Type, &question.Position, &question.IsActive, &question.CreatedAt, &question.UpdatedAt)
	return question, err
}

func validLabel(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrInvalidLabel
	}
	return value, nil
}

func requireChanged(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected row count: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
