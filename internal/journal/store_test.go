package journal

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/questions"
)

func TestGetDayDoesNotCreateMissingDay(t *testing.T) {
	store, db := newTestStore(t)
	day, err := store.GetDay(context.Background(), 1, "2026-08-29")
	if err != nil || day.Exists {
		t.Fatalf("GetDay() = %+v, %v", day, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM days`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("day count = %d, %v", count, err)
	}
	if _, err := store.GetDay(context.Background(), 1, "2026-02-30"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid date error = %v", err)
	}
	if _, err := store.GetDay(context.Background(), 999, "2026-08-29"); !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("missing journal error = %v", err)
	}
}

func TestSaveDayCreatesThenUpdatesOneDayAndReadsValues(t *testing.T) {
	store, db := newTestStore(t)
	q := addQuestion(t, db, 1, "Reflection", questions.QuestionTypeLongText, true)
	text := "line one\n  line two  "
	day := save(t, store, 1, "2026-08-29", SaveDayInput{GeneralNote: "note", SpecialMoment: "moment", Location: "home", Answers: []AnswerInput{{QuestionID: q, TextValue: &text}}})
	if !day.Exists || day.GeneralNote != "note" || day.SpecialMoment != "moment" || day.Location != "home" || len(day.Answers) != 1 || *day.Answers[0].TextValue != text {
		t.Fatalf("saved day = %+v", day)
	}
	save(t, store, 1, "2026-08-29", SaveDayInput{GeneralNote: "changed"})
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM days`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("day count = %d, %v", count, err)
	}
	day, _ = store.GetDay(context.Background(), 2, "2026-08-29")
	if day.Exists {
		t.Fatal("day leaked across journals")
	}
}

func TestTextBooleanAndClear(t *testing.T) {
	store, db := newTestStore(t)
	short := addQuestion(t, db, 1, "Short", questions.QuestionTypeShortText, true)
	boolean := addQuestion(t, db, 1, "Bool", questions.QuestionTypeBoolean, true)
	text := " value "
	yes := true
	no := false
	save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: short, TextValue: &text}, {QuestionID: boolean, BoolValue: &yes}}})
	day := save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: boolean, BoolValue: &no}, {QuestionID: short, Clear: true}}})
	if len(day.Answers) != 1 || day.Answers[0].BoolValue == nil || *day.Answers[0].BoolValue {
		t.Fatalf("answers after false and clear = %+v", day.Answers)
	}
	empty := ""
	day = save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: boolean, Clear: true}, {QuestionID: short, TextValue: &empty}}})
	if len(day.Answers) != 0 {
		t.Fatalf("answers after clear = %+v", day.Answers)
	}
}

func TestNumericScaleAndTimeValidation(t *testing.T) {
	store, db := newTestStore(t)
	types := []questions.QuestionType{questions.QuestionTypeNumber, questions.QuestionTypeScale5, questions.QuestionTypeScale10, questions.QuestionTypeTime}
	ids := make([]int64, len(types))
	for i, kind := range types {
		ids[i] = addQuestion(t, db, 1, string(kind), kind, true)
	}
	for _, tc := range []struct {
		name  string
		input AnswerInput
		ok    bool
	}{
		{"number", AnswerInput{QuestionID: ids[0], NumberValue: fp(12.5)}, true}, {"nan", AnswerInput{QuestionID: ids[0], NumberValue: fp(math.NaN())}, false},
		{"scale5-min", AnswerInput{QuestionID: ids[1], NumberValue: fp(1)}, true}, {"scale5-max", AnswerInput{QuestionID: ids[1], NumberValue: fp(5)}, true}, {"scale5-zero", AnswerInput{QuestionID: ids[1], NumberValue: fp(0)}, false}, {"scale5-six", AnswerInput{QuestionID: ids[1], NumberValue: fp(6)}, false}, {"scale5-fraction", AnswerInput{QuestionID: ids[1], NumberValue: fp(2.5)}, false},
		{"scale10-min", AnswerInput{QuestionID: ids[2], NumberValue: fp(1)}, true}, {"scale10-max", AnswerInput{QuestionID: ids[2], NumberValue: fp(10)}, true}, {"scale10-low", AnswerInput{QuestionID: ids[2], NumberValue: fp(-1)}, false}, {"scale10-high", AnswerInput{QuestionID: ids[2], NumberValue: fp(11)}, false}, {"scale10-fraction", AnswerInput{QuestionID: ids[2], NumberValue: fp(9.1)}, false},
		{"time", AnswerInput{QuestionID: ids[3], TimeValue: sp("07:05")}, true}, {"bad-time", AnswerInput{QuestionID: ids[3], TimeValue: sp("7:05")}, false}, {"impossible-time", AnswerInput{QuestionID: ids[3], TimeValue: sp("24:00")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.SaveDay(context.Background(), 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{tc.input}})
			if tc.ok && err != nil {
				t.Fatal(err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidAnswer) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSelectMultiSelectSnapshotsAndValidation(t *testing.T) {
	store, db := newTestStore(t)
	selectQ := addQuestion(t, db, 1, "Color", questions.QuestionTypeSelect, true)
	multiQ := addQuestion(t, db, 1, "Tags", questions.QuestionTypeMultiSelect, true)
	otherQ := addQuestion(t, db, 1, "Other", questions.QuestionTypeSelect, true)
	foreignQ := addQuestion(t, db, 2, "Foreign", questions.QuestionTypeSelect, true)
	red := addOption(t, db, selectQ, "Red", true)
	blue := addOption(t, db, selectQ, "Blue", true)
	one := addOption(t, db, multiQ, "One", true)
	two := addOption(t, db, multiQ, "Two", true)
	three := addOption(t, db, multiQ, "Three", true)
	wrong := addOption(t, db, otherQ, "Wrong", true)
	foreign := addOption(t, db, foreignQ, "Foreign", true)
	day := save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: selectQ, OptionIDs: []int64{red}}, {QuestionID: multiQ, OptionIDs: []int64{one, two}}}})
	if day.Answers[0].SelectedOptions[0].OptionLabelSnapshot != "Red" {
		t.Fatalf("select snapshot = %+v", day.Answers[0])
	}
	mustExec(t, db, `UPDATE question_options SET label = 'One renamed' WHERE id = ?`, one)
	day = save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: multiQ, OptionIDs: []int64{one, three}}}})
	if got := optionSnapshots(day, multiQ); got[one] != "One" || got[three] != "Three" || len(got) != 2 {
		t.Fatalf("snapshots = %v", got)
	}
	for _, input := range []AnswerInput{{QuestionID: selectQ, OptionIDs: []int64{red, blue}}, {QuestionID: selectQ, OptionIDs: []int64{wrong}}, {QuestionID: selectQ, OptionIDs: []int64{foreign}}, {QuestionID: multiQ, OptionIDs: []int64{one, one}}} {
		if _, err := store.SaveDay(context.Background(), 1, "2026-08-30", SaveDayInput{Answers: []AnswerInput{input}}); err == nil {
			t.Fatalf("invalid options accepted: %+v", input)
		}
	}
}

func TestHistoricalQuestionAndOptionEditing(t *testing.T) {
	store, db := newTestStore(t)
	textQ := addQuestion(t, db, 1, "Old label", questions.QuestionTypeShortText, true)
	multiQ := addQuestion(t, db, 1, "Choices", questions.QuestionTypeMultiSelect, true)
	old := addOption(t, db, multiQ, "Old option", true)
	active := addOption(t, db, multiQ, "Active", true)
	freshInactive := addOption(t, db, multiQ, "Never selected", false)
	v := "first"
	save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: textQ, TextValue: &v}, {QuestionID: multiQ, OptionIDs: []int64{old, active}}}})
	mustExec(t, db, `UPDATE questions SET label = 'New label', is_active = 0 WHERE id = ?`, textQ)
	mustExec(t, db, `UPDATE question_options SET label = 'Renamed option', is_active = 0 WHERE id = ?`, old)
	v = "updated"
	day := save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: textQ, TextValue: &v}, {QuestionID: multiQ, OptionIDs: []int64{old}}}})
	if answerFor(day, textQ).QuestionLabelSnapshot != "Old label" || optionSnapshots(day, multiQ)[old] != "Old option" {
		t.Fatalf("historical snapshots changed: %+v", day.Answers)
	}
	day = save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: multiQ, OptionIDs: []int64{active}}}})
	if _, exists := optionSnapshots(day, multiQ)[old]; exists {
		t.Fatalf("inactive historical option was not deselected: %+v", day.Answers)
	}
	day = save(t, store, 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: textQ, Clear: true}, {QuestionID: multiQ, OptionIDs: []int64{}}}})
	if len(day.Answers) != 0 {
		t.Fatalf("historical clear failed: %+v", day.Answers)
	}
	if _, err := store.SaveDay(context.Background(), 1, "2026-08-30", SaveDayInput{Answers: []AnswerInput{{QuestionID: textQ, TextValue: sp("new")}}}); !errors.Is(err, ErrInactiveQuestion) {
		t.Fatalf("inactive new question error = %v", err)
	}
	if _, err := store.SaveDay(context.Background(), 1, "2026-08-30", SaveDayInput{Answers: []AnswerInput{{QuestionID: multiQ, OptionIDs: []int64{freshInactive}}}}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("inactive new option error = %v", err)
	}
}

func TestDuplicateIsolationAndAtomicRollback(t *testing.T) {
	store, db := newTestStore(t)
	valid := addQuestion(t, db, 1, "Valid", questions.QuestionTypeShortText, true)
	foreign := addQuestion(t, db, 2, "Foreign", questions.QuestionTypeShortText, true)
	old := "old"
	save(t, store, 1, "2026-08-29", SaveDayInput{GeneralNote: "old note", Answers: []AnswerInput{{QuestionID: valid, TextValue: &old}}})
	if _, err := store.SaveDay(context.Background(), 1, "2026-08-29", SaveDayInput{Answers: []AnswerInput{{QuestionID: valid, TextValue: &old}, {QuestionID: valid, Clear: true}}}); !errors.Is(err, ErrDuplicateQuestion) {
		t.Fatalf("duplicate error = %v", err)
	}
	changed := "changed"
	if _, err := store.SaveDay(context.Background(), 1, "2026-08-29", SaveDayInput{GeneralNote: "changed note", Answers: []AnswerInput{{QuestionID: valid, TextValue: &changed}, {QuestionID: foreign, TextValue: &changed}}}); !errors.Is(err, ErrQuestionNotFound) {
		t.Fatalf("foreign question error = %v", err)
	}
	day, _ := store.GetDay(context.Background(), 1, "2026-08-29")
	if day.GeneralNote != "old note" || *answerFor(day, valid).TextValue != "old" {
		t.Fatalf("partial save persisted: %+v", day)
	}
}

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO users (id,name,created_at,updated_at) VALUES (1,'User','x','x')`)
	mustExec(t, db, `INSERT INTO journals (id,user_id,name,created_at,updated_at) VALUES (1,1,'One','x','x'),(2,1,'Two','x','x')`)
	return NewStore(db), db
}
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func addQuestion(t *testing.T, db *sql.DB, journalID int64, label string, kind questions.QuestionType, active bool) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO questions (journal_id,label,type,position,is_active,created_at,updated_at) VALUES (?,?,?,?,?,'x','x')`, journalID, label, kind, 0, active)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}
func addOption(t *testing.T, db *sql.DB, questionID int64, label string, active bool) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO question_options (question_id,label,position,is_active) VALUES (?,?,0,?)`, questionID, label, active)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}
func save(t *testing.T, store *Store, journalID int64, date string, input SaveDayInput) Day {
	t.Helper()
	day, err := store.SaveDay(context.Background(), journalID, date, input)
	if err != nil {
		t.Fatal(err)
	}
	return day
}
func fp(v float64) *float64 { return &v }
func sp(v string) *string   { return &v }
func answerFor(day Day, questionID int64) Answer {
	for _, a := range day.Answers {
		if a.QuestionID == questionID {
			return a
		}
	}
	return Answer{}
}
func optionSnapshots(day Day, questionID int64) map[int64]string {
	result := map[int64]string{}
	for _, o := range answerFor(day, questionID).SelectedOptions {
		result[o.OptionID] = o.OptionLabelSnapshot
	}
	return result
}
