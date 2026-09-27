package workout

import "testing"

func TestParseSetForms(t *testing.T) {
	tests := []struct {
		input string
		kind  LoadType
		reps  int
		load  float64
	}{
		{"Bench 10x60", External, 10, 60},
		{"Bench 8x72.5", External, 8, 72.5},
		{"Pull ups 10+5", Added, 10, 5},
		{"Pull ups 8+12.5", Added, 8, 12.5},
		{"Dips 10-20", Assisted, 10, 20},
		{"Dips 8-12.5", Assisted, 8, 12.5},
		{"Push ups 20", Bodyweight, 20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Parse(tt.input)
			if got.Raw != tt.input || len(got.Exercises) != 1 || len(got.Exercises[0].Sets) != 1 {
				t.Fatalf("Parse() = %+v", got)
			}
			set := got.Exercises[0].Sets[0]
			if set.LoadType != tt.kind || set.Reps != tt.reps || set.Weight != tt.load {
				t.Fatalf("set = %+v", set)
			}
		})
	}
}

func TestParseExercisesWhitespaceAndOptionalColon(t *testing.T) {
	raw := "Bench: 10 x 60, 8 x 70\nPull ups 10 + 5, 8 + 10\nPush ups 20,18,15"
	got := Parse(raw)
	if len(got.Exercises) != 3 || got.Exercises[0].Name != "Bench" || len(got.Exercises[0].Sets) != 2 || len(got.Issues) != 0 {
		t.Fatalf("Parse() = %+v", got)
	}
}

func TestParsePreservesRawAndReportsPartialAndUnrecognizedInput(t *testing.T) {
	raw := "Bench 10x60, hello, 6x80\nwhatever"
	got := Parse(raw)
	if got.Raw != raw || len(got.Exercises) != 1 || len(got.Exercises[0].Sets) != 2 || len(got.Issues) != 2 {
		t.Fatalf("Parse() = %+v", got)
	}
	if got.Issues[0].Token != "hello" || got.Issues[1].Line != "whatever" {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestCanonicalParserDoesNotTreatBareOrIncompleteDraftNamesAsHistory(t *testing.T) {
	for _, raw := range []string{"Pullup", "Pullup   ", "pullup 10x", "Bench nope"} {
		got := Parse(raw)
		if len(got.Exercises) != 0 {
			t.Fatalf("Parse(%q) exercises = %+v, want none", raw, got.Exercises)
		}
	}
	valid := Parse("Pullup 8+10")
	if len(valid.Exercises) != 1 || valid.Exercises[0].Name != "Pullup" {
		t.Fatalf("valid canonical parse = %+v", valid)
	}
}

func TestHistoryGroupingOrderingAndBests(t *testing.T) {
	records := []AnswerRecord{
		{Date: "2026-09-27", Raw: "bench press 3x95,broken,8x95\nPull ups 10,5+25,10-5"},
		{Date: "2026-09-20", Raw: "BENCH PRESS 6x90\nBench 20\nPull ups 8-10,6+20"},
	}
	history := BuildHistory(records, []Template{{Name: "Bench Press", Position: 0}, {Name: "Pull ups", Position: 1}})
	if len(history) != 3 || history[0].Name != "Bench Press" || history[1].Name != "Pull ups" || history[2].Name != "Bench" {
		t.Fatalf("history=%+v", history)
	}
	if len(history[0].Entries) != 2 || history[0].Entries[0].Date != "2026-09-27" || history[0].Entries[0].Sets != "3x95,8x95" {
		t.Fatalf("bench entries=%+v", history[0].Entries)
	}
	bests := Bests(history[0].Entries)
	if len(bests) != 1 || bests[0].Type != External || bests[0].Set.Weight != 95 || bests[0].Set.Reps != 8 {
		t.Fatalf("external bests=%+v", bests)
	}
	mixed := Bests(history[1].Entries)
	if len(mixed) != 3 || mixed[0].Type != Added || mixed[0].Set.Weight != 25 || mixed[1].Type != Assisted || mixed[1].Set.Weight != 5 || mixed[2].Type != Bodyweight || mixed[2].Set.Reps != 10 {
		t.Fatalf("mixed bests=%+v", mixed)
	}
	if NormalizeName(" BENCH PRESS ") != NormalizeName("Bench Press") || NormalizeName("Bench") == NormalizeName("Bench press") {
		t.Fatal("name normalization is not trimmed case-insensitive exact matching")
	}
}

func TestBestRulesAndCalendarRange(t *testing.T) {
	entries := []HistoryEntry{{Date: "2025-12-31", ParsedSets: Parse("X 6x90,8x90,3x95,6+20,8+20,5+25,8-10,6-5,10-5,15,20").Exercises[0].Sets}, {Date: "2026-01-01", ParsedSets: Parse("X 6x95").Exercises[0].Sets}}
	bests := Bests(entries)
	want := map[LoadType]Set{External: {Reps: 6, Weight: 95}, Added: {Reps: 5, Weight: 25}, Assisted: {Reps: 10, Weight: 5}, Bodyweight: {Reps: 20}}
	for _, best := range bests {
		expected := want[best.Type]
		if best.Set.Reps != expected.Reps || best.Set.Weight != expected.Weight {
			t.Fatalf("best %s=%+v want=%+v", best.Type, best.Set, expected)
		}
	}
	filtered := EntriesInRange(entries, "2026-01-01", "2026-01-31")
	if len(filtered) != 1 || filtered[0].Date != "2026-01-01" {
		t.Fatalf("filtered=%+v", filtered)
	}
}
