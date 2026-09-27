package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Bori513/lifelog/internal/workout"
)

func (s *Server) getWorkoutHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireProfile(w, r)
	if !ok {
		return
	}
	j, ok := s.defaultJournal(w, r, p)
	if !ok {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	label, records, err := workout.NewStore(s.db).LoadQuestionAnswers(r.Context(), j.ID, id)
	if errors.Is(err, workout.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internal(w, "load workout history", err)
		return
	}
	configured, err := s.questions.ListExerciseTemplates(r.Context(), j.ID, id)
	if err != nil {
		s.internal(w, "load workout history templates", err)
		return
	}
	templates := make([]workout.Template, len(configured))
	for i, item := range configured {
		templates[i] = workout.Template{Name: item.Name, Position: item.Position}
	}
	histories := workout.BuildHistory(records, templates)
	view := WorkoutHistoryView{QuestionID: id, QuestionLabel: label, View: r.URL.Query().Get("view")}
	if view.View == "" {
		view.View = "history"
	}
	if view.View != "history" && view.View != "month" && view.View != "year" {
		http.Error(w, "Invalid history view.", http.StatusBadRequest)
		return
	}
	selectedKey := workout.NormalizeName(r.URL.Query().Get("exercise"))
	if selectedKey == "" && len(histories) > 0 {
		selectedKey = histories[0].Key
	}
	var selected *workout.ExerciseHistory
	for i := range histories {
		item := &histories[i]
		active := item.Key == selectedKey
		view.Exercises = append(view.Exercises, WorkoutExerciseView{Name: item.Name, Key: item.Key, Selected: active})
		if active {
			selected = item
			view.SelectedExercise = item.Name
		}
	}
	base := fmt.Sprintf("/workout/questions/%d", id)
	view.HistoryURL = workoutURL(base, selectedKey, "history", "", "")
	view.MonthURL = workoutURL(base, selectedKey, "month", "", "")
	view.YearURL = workoutURL(base, selectedKey, "year", "", "")
	if selected != nil {
		entries := selected.Entries
		loc, e := time.LoadLocation(p.Timezone)
		if e != nil {
			s.internal(w, "load profile timezone", e)
			return
		}
		now := s.now().In(loc)
		switch view.View {
		case "month":
			month, e := parseHistoryMonth(r.URL.Query().Get("month"), now)
			if e != nil {
				http.Error(w, "Invalid month.", http.StatusBadRequest)
				return
			}
			from, through := month.Format("2006-01-02"), month.AddDate(0, 1, -1).Format("2006-01-02")
			entries = workout.EntriesInRange(entries, from, through)
			view.PeriodLabel = month.Format("January 2006")
			previous := month.AddDate(0, -1, 0)
			prevEntries := workout.EntriesInRange(selected.Entries, previous.Format("2006-01-02"), previous.AddDate(0, 1, -1).Format("2006-01-02"))
			view.PreviousBests = bestViews(workout.Bests(prevEntries))
			view.PreviousURL = workoutURL(base, selectedKey, "month", month.AddDate(0, -1, 0).Format("2006-01"), "")
			view.NextURL = workoutURL(base, selectedKey, "month", month.AddDate(0, 1, 0).Format("2006-01"), "")
		case "year":
			year, e := parseHistoryYear(r.URL.Query().Get("year"), now.Year())
			if e != nil {
				http.Error(w, "Invalid year.", http.StatusBadRequest)
				return
			}
			from, through := fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year)
			entries = workout.EntriesInRange(entries, from, through)
			view.PeriodLabel = strconv.Itoa(year)
			prevEntries := workout.EntriesInRange(selected.Entries, fmt.Sprintf("%04d-01-01", year-1), fmt.Sprintf("%04d-12-31", year-1))
			view.PreviousBests = bestViews(workout.Bests(prevEntries))
			view.PreviousURL = workoutURL(base, selectedKey, "year", "", strconv.Itoa(year-1))
			view.NextURL = workoutURL(base, selectedKey, "year", "", strconv.Itoa(year+1))
		}
		view.Sessions = len(entries)
		view.Bests = bestViews(workout.Bests(entries))
		if view.View == "history" {
			for _, entry := range entries {
				parsed, e := time.Parse("2006-01-02", entry.Date)
				if e != nil {
					continue
				}
				view.Entries = append(view.Entries, WorkoutEntryView{Date: entry.Date, DateLabel: parsed.Format("2 January 2006"), Sets: entry.Sets})
			}
		}
	}
	d := PageData{Title: label + " history", ProfileName: p.Name, CSRF: s.csrfToken(w, r), NavSection: "today", WorkoutHistory: view}
	s.render(w, "workout-history.html", d)
}

func bestViews(items []workout.Best) []WorkoutBestView {
	result := make([]WorkoutBestView, 0, len(items))
	for _, item := range items {
		label, value := "", ""
		switch item.Type {
		case workout.External:
			label, value = "External load best", fmt.Sprintf("%d × %s kg", item.Set.Reps, formatWorkoutWeight(item.Set.Weight))
		case workout.Added:
			label, value = "Added weight best", fmt.Sprintf("%d × +%s kg", item.Set.Reps, formatWorkoutWeight(item.Set.Weight))
		case workout.Assisted:
			label, value = "Lowest assistance best", fmt.Sprintf("%d × -%s kg", item.Set.Reps, formatWorkoutWeight(item.Set.Weight))
		case workout.Bodyweight:
			label, value = "Bodyweight best", fmt.Sprintf("%d reps", item.Set.Reps)
		}
		result = append(result, WorkoutBestView{Label: label, Value: value})
	}
	return result
}
func formatWorkoutWeight(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func parseHistoryMonth(value string, now time.Time) (time.Time, error) {
	if value == "" {
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), nil
	}
	parsed, err := time.ParseInLocation("2006-01", value, now.Location())
	if err != nil || parsed.Format("2006-01") != value {
		return time.Time{}, errors.New("invalid month")
	}
	return parsed, nil
}
func parseHistoryYear(value string, current int) (int, error) {
	if value == "" {
		return current, nil
	}
	year, err := strconv.Atoi(value)
	if err != nil || year < 1 || year > 9999 || strconv.Itoa(year) != value {
		return 0, errors.New("invalid year")
	}
	return year, nil
}
func workoutURL(base, exercise, view, month, year string) string {
	q := url.Values{}
	if exercise != "" {
		q.Set("exercise", exercise)
	}
	if view != "history" {
		q.Set("view", view)
	}
	if month != "" {
		q.Set("month", month)
	}
	if year != "" {
		q.Set("year", year)
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}
