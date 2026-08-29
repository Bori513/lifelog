package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/journal"
	"github.com/Bori513/lifelog/internal/profiles"
	"github.com/Bori513/lifelog/internal/questions"
	"github.com/Bori513/lifelog/internal/search"
)

type fixture struct {
	t        *testing.T
	journal  *journal.Store
	search   *search.Store
	q        *questions.Store
	profiles *profiles.Store
	jid      int64
}

func setup(t *testing.T) (*fixture, func(string, ...any)) {
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := profiles.NewStore(db).CreateProfile(t.Context(), profiles.CreateProfileInput{Name: "Test", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	js, err := profiles.NewStore(db).ListJournals(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, journal: journal.NewStore(db), search: search.NewStore(db), q: questions.NewStore(db), profiles: profiles.NewStore(db), jid: js[0].ID}
	exec := func(query string, args ...any) {
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	return f, exec
}

func (f *fixture) find(term string) []search.Result {
	f.t.Helper()
	r, err := f.search.Search(context.Background(), f.jid, term, 50)
	if err != nil {
		f.t.Fatalf("Search(%q): %v", term, err)
	}
	return r
}

func TestIndexesAllAnswerKindsAndSynchronizesSave(t *testing.T) {
	f, _ := setup(t)
	makeQ := func(label string, kind questions.QuestionType) questions.Question {
		q, err := f.q.CreateQuestion(t.Context(), f.jid, questions.CreateQuestionInput{Label: label, Type: kind})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	short := makeQ("Companion", questions.QuestionTypeShortText)
	long := makeQ("Story", questions.QuestionTypeLongText)
	boolean := makeQ("Did you drink alcohol?", questions.QuestionTypeBoolean)
	number := makeQ("Distance", questions.QuestionTypeNumber)
	scale := makeQ("Energy", questions.QuestionTypeScale5)
	timeQ := makeQ("Bedtime", questions.QuestionTypeTime)
	selectQ := makeQ("Meal type", questions.QuestionTypeSelect)
	multiQ := makeQ("Activities", questions.QuestionTypeMultiSelect)
	dinner, _ := f.q.CreateOption(t.Context(), f.jid, selectQ.ID, questions.CreateOptionInput{Label: "Dinner"})
	walking, _ := f.q.CreateOption(t.Context(), f.jid, multiQ.ID, questions.CreateOptionInput{Label: "Walking"})
	swimming, _ := f.q.CreateOption(t.Context(), f.jid, multiQ.ID, questions.CreateOptionInput{Label: "Swimming"})
	text, story, bedtime := "Boris", "A long vacation story", "23:45"
	yes, distance, energy := true, 12.5, 4.0
	_, err := f.journal.SaveDay(t.Context(), f.jid, "2026-08-28", journal.SaveDayInput{GeneralNote: "Pizza at McDonald's", SpecialMoment: "Sunset memory", Location: "Rovinj, Croatia", Answers: []journal.AnswerInput{
		{QuestionID: short.ID, TextValue: &text}, {QuestionID: long.ID, TextValue: &story}, {QuestionID: boolean.ID, BoolValue: &yes}, {QuestionID: number.ID, NumberValue: &distance}, {QuestionID: scale.ID, NumberValue: &energy}, {QuestionID: timeQ.ID, TimeValue: &bedtime}, {QuestionID: selectQ.ID, OptionIDs: []int64{dinner.ID}}, {QuestionID: multiQ.ID, OptionIDs: []int64{walking.ID, swimming.ID}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"pizza", "McDonald's", "Sunset", "Rovinj", "Boris", "vacation", "alcohol", "Yes", "12.5", "4", "23:45", "Dinner", "Walking", "Swimming"} {
		if len(f.find(term)) != 1 {
			t.Errorf("%q did not match", term)
		}
	}
	changed := "Ana"
	_, err = f.journal.SaveDay(t.Context(), f.jid, "2026-08-28", journal.SaveDayInput{GeneralNote: "Pizza", Answers: []journal.AnswerInput{{QuestionID: short.ID, TextValue: &changed}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.find("Boris")) != 0 || len(f.find("Ana")) != 1 {
		t.Fatal("edited answer did not synchronize")
	}
	_, err = f.journal.SaveDay(t.Context(), f.jid, "2026-08-28", journal.SaveDayInput{Answers: []journal.AnswerInput{{QuestionID: short.ID, Clear: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.find("Ana")) != 0 || len(f.find("pizza")) != 0 {
		t.Fatal("cleared content remained searchable")
	}
}

func TestSnapshotsRebuildOrderingAndQuerySafety(t *testing.T) {
	f, exec := setup(t)
	q, _ := f.q.CreateQuestion(t.Context(), f.jid, questions.CreateQuestionInput{Label: "Old wording", Type: questions.QuestionTypeSelect})
	o, _ := f.q.CreateOption(t.Context(), f.jid, q.ID, questions.CreateOptionInput{Label: "Old option"})
	_, err := f.journal.SaveDay(t.Context(), f.jid, "2026-01-01", journal.SaveDayInput{Answers: []journal.AnswerInput{{QuestionID: q.ID, OptionIDs: []int64{o.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.q.RenameQuestion(t.Context(), f.jid, q.ID, questions.RenameQuestionInput{Label: "New wording"})
	_ = f.q.RenameOption(t.Context(), f.jid, q.ID, o.ID, questions.RenameOptionInput{Label: "New option"})
	_, err = f.journal.SaveDay(t.Context(), f.jid, "2026-01-01", journal.SaveDayInput{Answers: []journal.AnswerInput{{QuestionID: q.ID, OptionIDs: []int64{o.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.journal.SaveDay(t.Context(), f.jid, "2026-12-31", journal.SaveDayInput{GeneralNote: "Old wording elsewhere"})
	results := f.find("Old wording")
	if len(results) != 2 || results[0].EntryDate != "2026-12-31" {
		t.Fatalf("ordering/results=%v", results)
	}
	if len(f.find("Old option")) != 1 || len(f.find("New option")) != 0 {
		t.Fatal("option snapshot was not preserved")
	}
	exec(`DELETE FROM search_documents`)
	if len(f.find("Old option")) != 0 {
		t.Fatal("corruption setup failed")
	}
	if err := f.search.RebuildAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.search.RebuildAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.find("Old option")) != 1 {
		t.Fatal("rebuild did not restore index")
	}
	for _, query := range []string{"McDonald's", `a"b`, "foo-bar", "Rovinj, Croatia", "C++", `AND OR * () : ^`} {
		if _, err := f.search.Search(t.Context(), f.jid, query, 50); err != nil {
			t.Errorf("unsafe query %q: %v", query, err)
		}
	}
}

func TestJournalIsolationAndAtomicIndexFailure(t *testing.T) {
	f, exec := setup(t)
	_, _ = f.journal.SaveDay(t.Context(), f.jid, "2026-08-01", journal.SaveDayInput{GeneralNote: "private term"})
	other, err := f.profiles.CreateProfile(t.Context(), profiles.CreateProfileInput{Name: "Other", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	otherJournals, err := f.profiles.ListJournals(t.Context(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.journal.SaveDay(t.Context(), otherJournals[0].ID, "2026-08-02", journal.SaveDayInput{GeneralNote: "private other-only"})
	if err != nil {
		t.Fatal(err)
	}
	results, err := f.search.Search(t.Context(), otherJournals[0].ID, "term", 50)
	if err != nil || len(results) != 0 {
		t.Fatalf("isolation results=%v err=%v", results, err)
	}
	results, err = f.search.Search(t.Context(), f.jid, "other-only", 50)
	if err != nil || len(results) != 0 {
		t.Fatalf("reverse isolation results=%v err=%v", results, err)
	}
	exec(`DROP TRIGGER search_documents_update`)
	exec(`CREATE TRIGGER search_documents_update AFTER UPDATE ON search_documents BEGIN SELECT RAISE(ABORT, 'index failed'); END`)
	_, err = f.journal.SaveDay(t.Context(), f.jid, "2026-08-01", journal.SaveDayInput{GeneralNote: "changed term"})
	if err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	day, err := f.journal.GetDay(t.Context(), f.jid, "2026-08-01")
	if err != nil || day.GeneralNote != "private term" {
		t.Fatalf("journal partially committed: %#v err=%v", day, err)
	}
}

func TestOpenInitializesExistingUnindexedDays(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	db, err := database.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profiles.NewStore(db).CreateProfile(t.Context(), profiles.CreateProfileInput{Name: "Upgrade", Timezone: "UTC"})
	js, _ := profiles.NewStore(db).ListJournals(t.Context(), p.ID)
	if _, err := journal.NewStore(db).SaveDay(t.Context(), js[0].ID, "2025-04-03", journal.SaveDayInput{GeneralNote: "historical upgrade term"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM search_documents`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	results, err := search.NewStore(db).Search(t.Context(), js[0].ID, "upgrade", 50)
	if err != nil || len(results) != 1 {
		t.Fatalf("upgrade initialization results=%v err=%v", results, err)
	}
}
