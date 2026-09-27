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
