package browse

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/journal"
	"github.com/Bori513/lifelog/internal/profiles"
	"github.com/Bori513/lifelog/internal/questions"
)

type fixture struct {
	t         *testing.T
	browse    *Store
	journal   *journal.Store
	questions *questions.Store
	journalID int64
}

func newFixture(t *testing.T) *fixture {
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ps := profiles.NewStore(db)
	p, err := ps.CreateProfile(t.Context(), profiles.CreateProfileInput{Name: "Browser", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	js, err := ps.ListJournals(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, browse: NewStore(db), journal: journal.NewStore(db), questions: questions.NewStore(db), journalID: js[0].ID}
}

func (f *fixture) question(label string, kind questions.QuestionType) questions.Question {
	q, err := f.questions.CreateQuestion(f.t.Context(), f.journalID, questions.CreateQuestionInput{Label: label, Type: kind})
	if err != nil {
		f.t.Fatal(err)
	}
	return q
}

func (f *fixture) option(questionID int64, label string) questions.QuestionOption {
	o, err := f.questions.CreateOption(f.t.Context(), f.journalID, questionID, questions.CreateOptionInput{Label: label})
	if err != nil {
		f.t.Fatal(err)
	}
	return o
}

func (f *fixture) save(date string, answers ...journal.AnswerInput) {
	_, err := f.journal.SaveDay(f.t.Context(), f.journalID, date, journal.SaveDayInput{GeneralNote: "note " + date, Answers: answers})
	if err != nil {
		f.t.Fatal(err)
	}
}

func dates(result Result) string {
	value := ""
	for _, day := range result.Days {
		value += day.EntryDate + ","
	}
	return value
}

func text(value string) *string     { return &value }
func number(value float64) *float64 { return &value }
func boolean(value bool) *bool      { return &value }

func TestDateRangeOrderingAndValidation(t *testing.T) {
	f := newFixture(t)
	for _, date := range []string{"2026-01-01", "2026-01-02", "2026-01-03"} {
		f.save(date)
	}
	tests := []struct {
		name   string
		filter Filter
		want   string
	}{
		{"all", Filter{}, "2026-01-03,2026-01-02,2026-01-01,"},
		{"from", Filter{From: "2026-01-02"}, "2026-01-03,2026-01-02,"},
		{"to", Filter{To: "2026-01-02"}, "2026-01-02,2026-01-01,"},
		{"inclusive", Filter{From: "2026-01-02", To: "2026-01-02"}, "2026-01-02,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.browse.ListDays(t.Context(), f.journalID, tt.filter)
			if err != nil || dates(got) != tt.want {
				t.Fatalf("dates=%q err=%v, want %q", dates(got), err, tt.want)
			}
		})
	}
	for _, filter := range []Filter{{From: "01-02-2026"}, {From: "2026-02-02", To: "2026-01-01"}} {
		if _, err := f.browse.ListDays(t.Context(), f.journalID, filter); !errors.Is(err, ErrInvalidFilter) {
			t.Fatalf("invalid range err=%v", err)
		}
	}
}

func TestBooleanKeepsUnansweredDistinctFromNo(t *testing.T) {
	f := newFixture(t)
	q := f.question("Good?", questions.QuestionTypeBoolean)
	f.save("2026-01-01", journal.AnswerInput{QuestionID: q.ID, BoolValue: boolean(true)})
	f.save("2026-01-02", journal.AnswerInput{QuestionID: q.ID, BoolValue: boolean(false)})
	f.save("2026-01-03")
	for value, want := range map[string]string{"true": "2026-01-01,", "false": "2026-01-02,"} {
		got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: q.ID, Operator: OperatorEqual, Value: value})
		if err != nil || dates(got) != want {
			t.Fatalf("value=%s dates=%q err=%v", value, dates(got), err)
		}
	}
}

func TestSelectAndMultiSelectUseHistoricalOptionIdentity(t *testing.T) {
	f := newFixture(t)
	selectQ := f.question("Mood", questions.QuestionTypeSelect)
	happy, sad := f.option(selectQ.ID, "Happy"), f.option(selectQ.ID, "Sad")
	multiQ := f.question("Activities", questions.QuestionTypeMultiSelect)
	run, read := f.option(multiQ.ID, "Run"), f.option(multiQ.ID, "Read")
	f.save("2026-02-01", journal.AnswerInput{QuestionID: selectQ.ID, OptionIDs: []int64{happy.ID}}, journal.AnswerInput{QuestionID: multiQ.ID, OptionIDs: []int64{run.ID, read.ID}})
	f.save("2026-02-02", journal.AnswerInput{QuestionID: selectQ.ID, OptionIDs: []int64{sad.ID}}, journal.AnswerInput{QuestionID: multiQ.ID, OptionIDs: []int64{read.ID}})
	if err := f.questions.RenameOption(t.Context(), f.journalID, selectQ.ID, happy.ID, questions.RenameOptionInput{Label: "Great"}); err != nil {
		t.Fatal(err)
	}
	if err := f.questions.DeactivateOption(t.Context(), f.journalID, selectQ.ID, happy.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.questions.DeactivateQuestion(t.Context(), f.journalID, selectQ.ID); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		q, option int64
		want      string
	}{{selectQ.ID, happy.ID, "2026-02-01,"}, {selectQ.ID, sad.ID, "2026-02-02,"}, {multiQ.ID, run.ID, "2026-02-01,"}} {
		got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: tt.q, OptionID: tt.option})
		if err != nil || dates(got) != tt.want {
			t.Fatalf("filter=%+v dates=%q err=%v", tt, dates(got), err)
		}
	}
	available, err := f.browse.ListQuestions(t.Context(), f.journalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 2 || available[0].Label != "Mood" || available[0].Active || available[0].Options[0].Label != "Great" || available[0].Options[0].Active {
		t.Fatalf("historical controls=%+v", available)
	}
}

func TestNumericAndScaleOperatorsUseNumericComparison(t *testing.T) {
	f := newFixture(t)
	q := f.question("Amount", questions.QuestionTypeNumber)
	scale := f.question("Score", questions.QuestionTypeScale5)
	for i, value := range []float64{2, 10, 20} {
		date := fmt.Sprintf("2026-03-0%d", i+1)
		answers := []journal.AnswerInput{{QuestionID: q.ID, NumberValue: number(value)}}
		if i < 2 {
			answers = append(answers, journal.AnswerInput{QuestionID: scale.ID, NumberValue: number(float64(i + 2))})
		}
		f.save(date, answers...)
	}
	tests := []struct {
		op    Operator
		value string
		want  string
	}{
		{OperatorEqual, "10", "2026-03-02,"}, {OperatorGreater, "10", "2026-03-03,"},
		{OperatorAtLeast, "10", "2026-03-03,2026-03-02,"}, {OperatorLess, "10", "2026-03-01,"},
		{OperatorAtMost, "10", "2026-03-02,2026-03-01,"},
	}
	for _, tt := range tests {
		got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: q.ID, Operator: tt.op, Value: tt.value})
		if err != nil || dates(got) != tt.want {
			t.Fatalf("op=%s dates=%q err=%v", tt.op, dates(got), err)
		}
	}
	got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: scale.ID, Operator: OperatorGreater, Value: "2"})
	if err != nil || dates(got) != "2026-03-02," {
		t.Fatalf("scale dates=%q err=%v", dates(got), err)
	}
}

func TestTimeAndTextFilters(t *testing.T) {
	f := newFixture(t)
	timeQ := f.question("Wake", questions.QuestionTypeTime)
	textQ := f.question("Thoughts", questions.QuestionTypeShortText)
	f.save("2026-04-01", journal.AnswerInput{QuestionID: timeQ.ID, TimeValue: text("07:30")}, journal.AnswerInput{QuestionID: textQ.ID, TextValue: text("Hello WORLD")})
	f.save("2026-04-02", journal.AnswerInput{QuestionID: timeQ.ID, TimeValue: text("09:00")}, journal.AnswerInput{QuestionID: textQ.ID, TextValue: text("Filled")})
	f.save("2026-04-03")
	for _, tt := range []struct {
		op          Operator
		value, want string
	}{{OperatorEqual, "07:30", "2026-04-01,"}, {OperatorBefore, "08:00", "2026-04-01,"}, {OperatorAfter, "08:00", "2026-04-02,"}} {
		got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: timeQ.ID, Operator: tt.op, Value: tt.value})
		if err != nil || dates(got) != tt.want {
			t.Fatalf("time op=%s dates=%q err=%v", tt.op, dates(got), err)
		}
	}
	for _, tt := range []struct {
		op          Operator
		value, want string
	}{{OperatorContains, "world", "2026-04-01,"}, {OperatorFilled, "", "2026-04-02,2026-04-01,"}, {OperatorEmpty, "", "2026-04-03,"}} {
		got, err := f.browse.ListDays(t.Context(), f.journalID, Filter{QuestionID: textQ.ID, Operator: tt.op, Value: tt.value})
		if err != nil || dates(got) != tt.want {
			t.Fatalf("text op=%s dates=%q err=%v", tt.op, dates(got), err)
		}
	}
}

func TestIsolationAndPagination(t *testing.T) {
	f := newFixture(t)
	q := f.question("Private", questions.QuestionTypeBoolean)
	for i := 1; i <= 51; i++ {
		f.save(fmt.Sprintf("2025-%02d-%02d", (i-1)/28+1, (i-1)%28+1), journal.AnswerInput{QuestionID: q.ID, BoolValue: boolean(true)})
	}
	first, err := f.browse.ListDays(t.Context(), f.journalID, Filter{})
	if err != nil || len(first.Days) != PageSize || !first.HasNext || first.HasPrevious || first.Total != 51 {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	second, err := f.browse.ListDays(t.Context(), f.journalID, Filter{Page: 2})
	if err != nil || len(second.Days) != 1 || second.HasNext || !second.HasPrevious {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	other := newProfileJournal(t, f)
	if got, err := f.browse.ListDays(t.Context(), other, Filter{QuestionID: q.ID, Operator: OperatorEqual, Value: "true"}); !errors.Is(err, ErrInvalidFilter) || len(got.Days) != 0 {
		t.Fatalf("cross-journal result=%+v err=%v", got, err)
	}
}

func newProfileJournal(t *testing.T, f *fixture) int64 {
	ps := profiles.NewStore(f.browse.db)
	p, err := ps.CreateProfile(t.Context(), profiles.CreateProfileInput{Name: "Other", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	js, err := ps.ListJournals(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return js[0].ID
}
