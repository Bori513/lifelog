package workout

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("workout: history not found")

type AnswerRecord struct{ Date, Raw string }
type Template struct {
	Name     string
	Position int
}
type HistoryEntry struct {
	Date, Sets string
	ParsedSets []Set
	Activities []Activity
}
type ExerciseHistory struct {
	Name, Key string
	Entries   []HistoryEntry
}
type Best struct {
	Type       LoadType
	Set        Set
	Metric     string
	Duration   int
	DistanceKM float64
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// LoadQuestionAnswers verifies journal ownership and workout type, then loads the
// selected question's answers newest first in one bounded query.
func (s *Store) LoadQuestionAnswers(ctx context.Context, journalID, questionID int64) (string, []AnswerRecord, error) {
	var label string
	if err := s.db.QueryRowContext(ctx, `SELECT label FROM questions WHERE id = ? AND journal_id = ? AND type = 'workout'`, questionID, journalID).Scan(&label); errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	} else if err != nil {
		return "", nil, fmt.Errorf("find workout question: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.entry_date, a.text_value FROM answers a JOIN days d ON d.id = a.day_id WHERE a.question_id = ? AND d.journal_id = ? AND a.text_value IS NOT NULL AND trim(a.text_value) <> '' ORDER BY d.entry_date DESC`, questionID, journalID)
	if err != nil {
		return "", nil, fmt.Errorf("load workout answers: %w", err)
	}
	defer rows.Close()
	var records []AnswerRecord
	for rows.Next() {
		var item AnswerRecord
		if err := rows.Scan(&item.Date, &item.Raw); err != nil {
			return "", nil, fmt.Errorf("scan workout answer: %w", err)
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("read workout answers: %w", err)
	}
	return label, records, nil
}

func NormalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// BuildHistory groups only by trimmed, case-insensitive exact names. Template
// spelling and order win; historical-only exercises follow first appearance in
// the newest-first records.
func BuildHistory(records []AnswerRecord, templates []Template) []ExerciseHistory {
	byKey := map[string]*ExerciseHistory{}
	var ordered []*ExerciseHistory
	for _, item := range templates {
		key := NormalizeName(item.Name)
		if key == "" {
			continue
		}
		if _, exists := byKey[key]; exists {
			continue
		}
		h := &ExerciseHistory{Name: strings.TrimSpace(item.Name), Key: key}
		byKey[key] = h
		ordered = append(ordered, h)
	}
	for _, record := range records {
		perDay := map[string][]Set{}
		activitiesPerDay := map[string][]Activity{}
		rawPerDay := map[string][]string{}
		names := map[string]string{}
		var dayOrder []string
		for _, exercise := range Parse(record.Raw).Exercises {
			key := NormalizeName(exercise.Name)
			if key == "" {
				continue
			}
			if _, ok := perDay[key]; !ok {
				dayOrder = append(dayOrder, key)
				names[key] = strings.TrimSpace(exercise.Name)
			}
			perDay[key] = append(perDay[key], exercise.Sets...)
			if exercise.Activity != nil {
				activitiesPerDay[key] = append(activitiesPerDay[key], *exercise.Activity)
				rawPerDay[key] = append(rawPerDay[key], FormatActivity(*exercise.Activity))
			} else {
				for _, set := range exercise.Sets {
					rawPerDay[key] = append(rawPerDay[key], set.Raw)
				}
			}
		}
		for _, key := range dayOrder {
			h := byKey[key]
			if h == nil {
				h = &ExerciseHistory{Name: names[key], Key: key}
				byKey[key] = h
				ordered = append(ordered, h)
			}
			h.Entries = append(h.Entries, HistoryEntry{Date: record.Date, Sets: strings.Join(rawPerDay[key], ","), ParsedSets: perDay[key], Activities: activitiesPerDay[key]})
		}
	}
	result := make([]ExerciseHistory, 0, len(ordered))
	for _, item := range ordered {
		if len(item.Entries) > 0 {
			result = append(result, *item)
		}
	}
	return result
}

func Bests(entries []HistoryEntry) []Best {
	byType := map[LoadType]Set{}
	seen := map[LoadType]bool{}
	longestDuration, longestDistance := 0, float64(0)
	for _, entry := range entries {
		for _, set := range entry.ParsedSets {
			current := byType[set.LoadType]
			if !seen[set.LoadType] || better(set, current) {
				byType[set.LoadType], seen[set.LoadType] = set, true
			}
		}
		for _, activity := range entry.Activities {
			for _, duration := range activity.Durations {
				if duration > longestDuration {
					longestDuration = duration
				}
			}
			if activity.DistanceKM != nil && *activity.DistanceKM > longestDistance {
				longestDistance = *activity.DistanceKM
			}
		}
	}
	order := []LoadType{External, Added, Assisted, Bodyweight}
	result := make([]Best, 0, len(seen))
	for _, kind := range order {
		if seen[kind] {
			result = append(result, Best{Type: kind, Set: byType[kind]})
		}
	}
	if longestDuration > 0 {
		result = append(result, Best{Metric: "duration", Duration: longestDuration})
	}
	if longestDistance > 0 {
		result = append(result, Best{Metric: "distance", DistanceKM: longestDistance})
	}
	return result
}

func better(candidate, current Set) bool {
	switch candidate.LoadType {
	case Assisted:
		return candidate.Weight < current.Weight || candidate.Weight == current.Weight && candidate.Reps > current.Reps
	case Bodyweight:
		return candidate.Reps > current.Reps
	default:
		return candidate.Weight > current.Weight || candidate.Weight == current.Weight && candidate.Reps > current.Reps
	}
}

func EntriesInRange(entries []HistoryEntry, from, through string) []HistoryEntry {
	result := make([]HistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Date >= from && entry.Date <= through {
			result = append(result, entry)
		}
	}
	return result
}

func SortTemplates(items []Template) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
}
