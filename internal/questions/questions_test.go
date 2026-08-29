package questions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Bori513/lifelog/internal/database"
)

func TestQuestionCreationValidationAndIsolation(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	first, err := store.CreateQuestion(ctx, 1, CreateQuestionInput{Label: "  Mood  ", Type: QuestionTypeScale5})
	if err != nil {
		t.Fatalf("CreateQuestion() error = %v", err)
	}
	if first.Label != "Mood" || first.Position != 0 || !first.IsActive {
		t.Fatalf("first question = %+v", first)
	}
	second, err := store.CreateQuestion(ctx, 1, CreateQuestionInput{Label: "Notes", Type: QuestionTypeLongText})
	if err != nil {
		t.Fatalf("CreateQuestion() second error = %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("second position = %d, want 1", second.Position)
	}
	foreign, err := store.CreateQuestion(ctx, 2, CreateQuestionInput{Label: "Other journal", Type: QuestionTypeBoolean})
	if err != nil {
		t.Fatalf("CreateQuestion() foreign journal error = %v", err)
	}
	if foreign.Position != 0 {
		t.Fatalf("foreign position = %d, want 0", foreign.Position)
	}

	for _, test := range []struct {
		name  string
		input CreateQuestionInput
		want  error
	}{
		{name: "blank", input: CreateQuestionInput{Label: " \t\n", Type: QuestionTypeShortText}, want: ErrInvalidLabel},
		{name: "invalid type", input: CreateQuestionInput{Label: "Valid", Type: "rating"}, want: ErrInvalidQuestionType},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.CreateQuestion(ctx, 1, test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("CreateQuestion() error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := store.CreateQuestion(ctx, 999, CreateQuestionInput{Label: "Missing", Type: QuestionTypeShortText}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateQuestion() missing journal error = %v, want ErrNotFound", err)
	}

	questions, err := store.ListQuestions(ctx, 1, false)
	if err != nil {
		t.Fatalf("ListQuestions() error = %v", err)
	}
	if got := questionIDs(questions); !reflect.DeepEqual(got, []int64{first.ID, second.ID}) {
		t.Fatalf("journal 1 IDs = %v", got)
	}
}

func TestQuestionRenameActivationAndListing(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	first := mustCreateQuestion(t, store, 1, "First", QuestionTypeShortText)
	second := mustCreateQuestion(t, store, 1, "Second", QuestionTypeShortText)
	third := mustCreateQuestion(t, store, 1, "Third", QuestionTypeShortText)

	if err := store.RenameQuestion(ctx, 1, first.ID, RenameQuestionInput{Label: "  Renamed  "}); err != nil {
		t.Fatalf("RenameQuestion() error = %v", err)
	}
	if err := store.RenameQuestion(ctx, 1, first.ID, RenameQuestionInput{Label: "  "}); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("blank RenameQuestion() error = %v", err)
	}
	if err := store.RenameQuestion(ctx, 2, first.ID, RenameQuestionInput{Label: "Wrong"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RenameQuestion() error = %v", err)
	}

	if err := store.DeactivateQuestion(ctx, 1, second.ID); err != nil {
		t.Fatalf("DeactivateQuestion() error = %v", err)
	}
	if err := store.DeactivateQuestion(ctx, 1, second.ID); err != nil {
		t.Fatalf("idempotent DeactivateQuestion() error = %v", err)
	}
	active, _ := store.ListQuestions(ctx, 1, false)
	if got := questionIDs(active); !reflect.DeepEqual(got, []int64{first.ID, third.ID}) {
		t.Fatalf("active IDs = %v", got)
	}
	all, _ := store.ListQuestions(ctx, 1, true)
	if len(all) != 3 {
		t.Fatalf("all question count = %d, want 3", len(all))
	}
	var rowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM questions WHERE id = ?`, second.ID).Scan(&rowCount); err != nil || rowCount != 1 {
		t.Fatalf("deactivated row count = %d, error = %v", rowCount, err)
	}

	if err := store.ReactivateQuestion(ctx, 1, second.ID); err != nil {
		t.Fatalf("ReactivateQuestion() error = %v", err)
	}
	if err := store.ReactivateQuestion(ctx, 1, second.ID); err != nil {
		t.Fatalf("idempotent ReactivateQuestion() error = %v", err)
	}
	active, _ = store.ListQuestions(ctx, 1, false)
	if got := questionIDs(active); !reflect.DeepEqual(got, []int64{first.ID, third.ID, second.ID}) {
		t.Fatalf("reactivated IDs = %v", got)
	}
	if active[0].Label != "Renamed" {
		t.Fatalf("renamed label = %q", active[0].Label)
	}
}

func TestQuestionReorderValidationAndAtomicity(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	first := mustCreateQuestion(t, store, 1, "First", QuestionTypeShortText)
	second := mustCreateQuestion(t, store, 1, "Second", QuestionTypeShortText)
	third := mustCreateQuestion(t, store, 1, "Third", QuestionTypeShortText)
	foreign := mustCreateQuestion(t, store, 2, "Foreign", QuestionTypeShortText)
	if err := store.DeactivateQuestion(ctx, 1, third.ID); err != nil {
		t.Fatal(err)
	}

	if err := store.ReorderQuestions(ctx, 1, ReorderQuestionsInput{IDs: []int64{second.ID, first.ID}}); err != nil {
		t.Fatalf("ReorderQuestions() error = %v", err)
	}
	want := []int64{second.ID, first.ID}
	assertQuestionOrder(t, store, 1, want)

	invalid := [][]int64{
		{second.ID, second.ID},
		{second.ID},
		{second.ID, foreign.ID},
		{second.ID, 9999},
	}
	for _, ids := range invalid {
		if err := store.ReorderQuestions(ctx, 1, ReorderQuestionsInput{IDs: ids}); !errors.Is(err, ErrInvalidReorder) {
			t.Fatalf("ReorderQuestions(%v) error = %v", ids, err)
		}
		assertQuestionOrder(t, store, 1, want)
	}
}

func TestOptionCreationValidationAndIsolation(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	selectQuestion := mustCreateQuestion(t, store, 1, "Color", QuestionTypeSelect)
	multiQuestion := mustCreateQuestion(t, store, 1, "Tags", QuestionTypeMultiSelect)
	textQuestion := mustCreateQuestion(t, store, 1, "Text", QuestionTypeShortText)

	first := mustCreateOption(t, store, 1, selectQuestion.ID, "  Red  ")
	second := mustCreateOption(t, store, 1, selectQuestion.ID, "Blue")
	if first.Label != "Red" || first.Position != 0 || second.Position != 1 {
		t.Fatalf("created options = %+v, %+v", first, second)
	}
	if _, err := store.CreateOption(ctx, 1, selectQuestion.ID, CreateOptionInput{Label: " \t"}); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("blank CreateOption() error = %v", err)
	}
	if _, err := store.CreateOption(ctx, 1, textQuestion.ID, CreateOptionInput{Label: "No"}); !errors.Is(err, ErrOptionsNotAllowed) {
		t.Fatalf("non-select CreateOption() error = %v", err)
	}
	if _, err := store.CreateOption(ctx, 2, selectQuestion.ID, CreateOptionInput{Label: "Wrong"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign CreateOption() error = %v", err)
	}
	multi := mustCreateOption(t, store, 1, multiQuestion.ID, "One")
	if multi.Position != 0 {
		t.Fatalf("multi option position = %d", multi.Position)
	}
}

func TestOptionRenameActivationListingAndReorder(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	question := mustCreateQuestion(t, store, 1, "Color", QuestionTypeSelect)
	otherQuestion := mustCreateQuestion(t, store, 1, "Other", QuestionTypeSelect)
	first := mustCreateOption(t, store, 1, question.ID, "First")
	second := mustCreateOption(t, store, 1, question.ID, "Second")
	third := mustCreateOption(t, store, 1, question.ID, "Third")
	foreign := mustCreateOption(t, store, 1, otherQuestion.ID, "Foreign")

	if err := store.RenameOption(ctx, 1, question.ID, first.ID, RenameOptionInput{Label: "  Renamed  "}); err != nil {
		t.Fatalf("RenameOption() error = %v", err)
	}
	if err := store.RenameOption(ctx, 1, question.ID, first.ID, RenameOptionInput{Label: " "}); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("blank RenameOption() error = %v", err)
	}
	if err := store.RenameOption(ctx, 1, otherQuestion.ID, first.ID, RenameOptionInput{Label: "Wrong"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RenameOption() error = %v", err)
	}
	if err := store.DeactivateOption(ctx, 1, question.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeactivateOption(ctx, 1, question.ID, second.ID); err != nil {
		t.Fatalf("idempotent DeactivateOption() error = %v", err)
	}
	active, _ := store.ListOptions(ctx, 1, question.ID, false)
	if got := optionIDs(active); !reflect.DeepEqual(got, []int64{first.ID, third.ID}) {
		t.Fatalf("active options = %v", got)
	}
	all, _ := store.ListOptions(ctx, 1, question.ID, true)
	if len(all) != 3 {
		t.Fatalf("all option count = %d", len(all))
	}
	var rowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM question_options WHERE id = ?`, second.ID).Scan(&rowCount); err != nil || rowCount != 1 {
		t.Fatalf("deactivated option row count = %d, error = %v", rowCount, err)
	}

	if err := store.ReorderOptions(ctx, 1, question.ID, ReorderOptionsInput{IDs: []int64{third.ID, first.ID}}); err != nil {
		t.Fatalf("ReorderOptions() error = %v", err)
	}
	assertOptionOrder(t, store, 1, question.ID, []int64{third.ID, first.ID})
	for _, ids := range [][]int64{{third.ID, third.ID}, {third.ID}, {third.ID, foreign.ID}, {third.ID, 9999}} {
		if err := store.ReorderOptions(ctx, 1, question.ID, ReorderOptionsInput{IDs: ids}); !errors.Is(err, ErrInvalidReorder) {
			t.Fatalf("ReorderOptions(%v) error = %v", ids, err)
		}
		assertOptionOrder(t, store, 1, question.ID, []int64{third.ID, first.ID})
	}

	if err := store.ReactivateOption(ctx, 1, question.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ReactivateOption(ctx, 1, question.ID, second.ID); err != nil {
		t.Fatalf("idempotent ReactivateOption() error = %v", err)
	}
	assertOptionOrder(t, store, 1, question.ID, []int64{third.ID, first.ID, second.ID})
	active, _ = store.ListOptions(ctx, 1, question.ID, false)
	if active[1].Label != "Renamed" {
		t.Fatalf("renamed option label = %q", active[1].Label)
	}
}

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO users (id, name, created_at, updated_at) VALUES (1, 'User', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO journals (id, user_id, name, created_at, updated_at) VALUES (1, 1, 'First', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z'), (2, 1, 'Second', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	return NewStore(db), db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
}

func mustCreateQuestion(t *testing.T, store *Store, journalID int64, label string, questionType QuestionType) Question {
	t.Helper()
	question, err := store.CreateQuestion(context.Background(), journalID, CreateQuestionInput{Label: label, Type: questionType})
	if err != nil {
		t.Fatalf("CreateQuestion() error = %v", err)
	}
	return question
}

func mustCreateOption(t *testing.T, store *Store, journalID, questionID int64, label string) QuestionOption {
	t.Helper()
	option, err := store.CreateOption(context.Background(), journalID, questionID, CreateOptionInput{Label: label})
	if err != nil {
		t.Fatalf("CreateOption() error = %v", err)
	}
	return option
}

func questionIDs(items []Question) []int64 {
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	return ids
}

func optionIDs(items []QuestionOption) []int64 {
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	return ids
}

func assertQuestionOrder(t *testing.T, store *Store, journalID int64, want []int64) {
	t.Helper()
	items, err := store.ListQuestions(context.Background(), journalID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := questionIDs(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("question order = %v, want %v", got, want)
	}
}

func assertOptionOrder(t *testing.T, store *Store, journalID, questionID int64, want []int64) {
	t.Helper()
	items, err := store.ListOptions(context.Background(), journalID, questionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := optionIDs(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("option order = %v, want %v", got, want)
	}
}
