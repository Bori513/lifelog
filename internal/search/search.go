package search

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const DefaultLimit = 50

type Result struct {
	DayID     int64
	EntryDate string
	Snippet   string
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func RebuildDay(ctx context.Context, db executor, dayID int64) error {
	var journalID int64
	var entryDate, generalNote, specialMoment, location string
	err := db.QueryRowContext(ctx, `SELECT journal_id, entry_date, general_note, special_moment, location FROM days WHERE id = ?`, dayID).
		Scan(&journalID, &entryDate, &generalNote, &specialMoment, &location)
	if err != nil {
		return fmt.Errorf("read day for search: %w", err)
	}
	parts := []string{"General note:\n" + generalNote, "Remember this:\n" + specialMoment, "Location:\n" + location}
	rows, err := db.QueryContext(ctx, `SELECT id, question_label_snapshot, text_value, number_value, bool_value, time_value FROM answers WHERE day_id = ? ORDER BY question_id, id`, dayID)
	if err != nil {
		return fmt.Errorf("read answers for search: %w", err)
	}
	type answer struct {
		id      int64
		label   string
		text    sql.NullString
		number  sql.NullFloat64
		boolean sql.NullBool
		time    sql.NullString
	}
	var answers []answer
	for rows.Next() {
		var a answer
		if err := rows.Scan(&a.id, &a.label, &a.text, &a.number, &a.boolean, &a.time); err != nil {
			rows.Close()
			return fmt.Errorf("scan answer for search: %w", err)
		}
		answers = append(answers, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read answers for search: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close answers for search: %w", err)
	}
	for _, a := range answers {
		value := ""
		switch {
		case a.text.Valid:
			value = a.text.String
		case a.number.Valid:
			value = strconv.FormatFloat(a.number.Float64, 'f', -1, 64)
		case a.boolean.Valid && a.boolean.Bool:
			value = "Yes"
		case a.boolean.Valid:
			value = "No"
		case a.time.Valid:
			value = a.time.String
		default:
			optionRows, err := db.QueryContext(ctx, `SELECT option_label_snapshot FROM answer_options WHERE answer_id = ? ORDER BY option_id`, a.id)
			if err != nil {
				return fmt.Errorf("read answer options for search: %w", err)
			}
			var options []string
			for optionRows.Next() {
				var option string
				if err := optionRows.Scan(&option); err != nil {
					optionRows.Close()
					return fmt.Errorf("scan answer option for search: %w", err)
				}
				options = append(options, option)
			}
			if err := optionRows.Err(); err != nil {
				optionRows.Close()
				return fmt.Errorf("read answer options for search: %w", err)
			}
			if err := optionRows.Close(); err != nil {
				return fmt.Errorf("close answer options for search: %w", err)
			}
			value = strings.Join(options, ", ")
		}
		parts = append(parts, a.label+":\n"+value)
	}
	body := strings.Join(parts, "\n\n")
	_, err = db.ExecContext(ctx, `INSERT INTO search_documents(day_id, journal_id, entry_date, body) VALUES (?, ?, ?, ?) ON CONFLICT(day_id) DO UPDATE SET journal_id = excluded.journal_id, entry_date = excluded.entry_date, body = excluded.body`, dayID, journalID, entryDate, body)
	if err != nil {
		return fmt.Errorf("write search document: %w", err)
	}
	return nil
}

func (s *Store) RebuildAll(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin search rebuild: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_documents`); err != nil {
		return fmt.Errorf("clear search documents: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM days ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list days for search rebuild: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan day for search rebuild: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list days for search rebuild: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close days for search rebuild: %w", err)
	}
	for _, id := range ids {
		if err := RebuildDay(ctx, tx, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit search rebuild: %w", err)
	}
	return nil
}

func (s *Store) Initialize(ctx context.Context) error {
	var days, documents int
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM days), (SELECT count(*) FROM search_documents)`).Scan(&days, &documents); err != nil {
		return fmt.Errorf("inspect search index: %w", err)
	}
	if days > 0 && documents == 0 {
		return s.RebuildAll(ctx)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, journalID int64, query string, limit int) ([]Result, error) {
	match := plainTextQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 || limit > DefaultLimit {
		limit = DefaultLimit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.day_id, d.entry_date, snippet(search_fts, 0, '', '', ' … ', 24) FROM search_fts JOIN search_documents d ON d.day_id = search_fts.rowid WHERE search_fts MATCH ? AND d.journal_id = ? ORDER BY d.entry_date DESC, d.day_id DESC LIMIT ?`, match, journalID, limit)
	if err != nil {
		return nil, fmt.Errorf("search journal: %w", err)
	}
	defer rows.Close()
	var results []Result
	for rows.Next() {
		var result Result
		if err := rows.Scan(&result.DayID, &result.EntryDate, &result.Snippet); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search journal: %w", err)
	}
	return results, nil
}

func plainTextQuery(value string) string {
	var terms []string
	for _, term := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		terms = append(terms, `"`+term+`"`)
	}
	return strings.Join(terms, " AND ")
}
