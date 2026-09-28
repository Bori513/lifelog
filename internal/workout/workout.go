// Package workout parses LifeLog's compact, raw-text-first workout notation.
package workout

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type LoadType string

const (
	Bodyweight LoadType = "bodyweight"
	External   LoadType = "external"
	Added      LoadType = "added"
	Assisted   LoadType = "assisted"
)

type Set struct {
	Reps     int
	LoadType LoadType
	Weight   float64
	Raw      string
}

type Exercise struct {
	Name     string
	Sets     []Set
	Activity *Activity
}

type Activity struct {
	Durations  []int
	DistanceKM *float64
	Incline    *float64
	Raw        string
}

type Issue struct {
	Line  string
	Token string
}

type Workout struct {
	Raw       string
	Exercises []Exercise
	Issues    []Issue
}

var setPattern = regexp.MustCompile(`^(\d+)\s*(?:([xX+\-])\s*(\d+(?:\.\d+)?))?$`)
var (
	durationPattern = regexp.MustCompile(`(?i)^(?:(\d+)h(?:\s*(\d+)m)?(?:\s*(\d+)s)?|(\d+)m(?:\s*(\d+)s)?|(\d+)s)`)
	distancePattern = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)km\b`)
	inclinePattern  = regexp.MustCompile(`(?i)^i(\d+(?:\.\d+)?)\b`)
)

// Parse always retains raw exactly. Unrecognized lines and sets become issues;
// valid sets on a partially malformed line are still returned.
func Parse(raw string) Workout {
	result := Workout{Raw: raw}
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, valueText, ok := splitLine(line)
		if !ok {
			result.Issues = append(result.Issues, Issue{Line: line, Token: line})
			continue
		}
		exercise := Exercise{Name: name}
		if activity, valid := parseActivity(valueText); valid {
			exercise.Activity = &activity
			result.Exercises = append(result.Exercises, exercise)
			continue
		}
		if looksLikeActivity(valueText) {
			if activity, ok := parsePartialActivity(valueText); ok {
				exercise.Activity = &activity
				result.Exercises = append(result.Exercises, exercise)
			}
			result.Issues = append(result.Issues, Issue{Line: line, Token: strings.TrimSpace(valueText)})
			continue
		}
		for _, rawSet := range strings.Split(valueText, ",") {
			token := strings.TrimSpace(rawSet)
			set, valid := parseSet(token)
			if !valid {
				result.Issues = append(result.Issues, Issue{Line: line, Token: token})
				continue
			}
			exercise.Sets = append(exercise.Sets, set)
		}
		if len(exercise.Sets) > 0 {
			result.Exercises = append(result.Exercises, exercise)
		}
	}
	return result
}

func splitLine(line string) (string, string, bool) {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[:i]), ":"))
		rest := strings.TrimSpace(line[i:])
		first, _, _ := strings.Cut(rest, ",")
		if name != "" {
			if _, ok := parseSet(strings.TrimSpace(first)); ok {
				return name, rest, true
			}
			if _, ok := parseActivity(rest); ok {
				return name, rest, true
			}
			if looksLikeActivity(rest) {
				return name, rest, true
			}
		}
	}
	return "", "", false
}

func looksLikeActivity(text string) bool {
	text = strings.TrimSpace(text)
	return durationPattern.MatchString(text) || distancePattern.MatchString(text) || inclinePattern.MatchString(text)
}

// parsePartialActivity keeps only metrics that remain unambiguous after a
// malformed activity line. In particular, duplicate metric types are discarded.
func parsePartialActivity(text string) (Activity, bool) {
	activity := Activity{Raw: strings.TrimSpace(text)}
	if strings.Contains(text, ",") {
		for _, raw := range strings.Split(text, ",") {
			token := strings.TrimSpace(raw)
			if seconds, consumed, ok := parseDuration(token); ok && consumed == len(token) {
				activity.Durations = append(activity.Durations, seconds)
			}
		}
		return activity, len(activity.Durations) > 0
	}
	durationSeen, distanceSeen, inclineSeen := false, false, false
	for _, token := range strings.Fields(text) {
		if seconds, consumed, ok := parseDuration(token); ok && consumed == len(token) {
			if durationSeen {
				activity.Durations = nil
			} else {
				activity.Durations = []int{seconds}
			}
			durationSeen = true
			continue
		}
		if match := distancePattern.FindStringSubmatch(token); match != nil && len(match[0]) == len(token) {
			if distanceSeen {
				activity.DistanceKM = nil
			} else if value, err := strconv.ParseFloat(match[1], 64); err == nil && value > 0 {
				activity.DistanceKM = &value
			}
			distanceSeen = true
			continue
		}
		if match := inclinePattern.FindStringSubmatch(token); match != nil && len(match[0]) == len(token) {
			if inclineSeen {
				activity.Incline = nil
			} else if value, err := strconv.ParseFloat(match[1], 64); err == nil {
				activity.Incline = &value
			}
			inclineSeen = true
		}
	}
	return activity, len(activity.Durations) > 0 || activity.DistanceKM != nil || activity.Incline != nil
}

func parseActivity(text string) (Activity, bool) {
	activity := Activity{Raw: strings.TrimSpace(text)}
	if strings.Contains(text, ",") {
		for _, raw := range strings.Split(text, ",") {
			seconds, consumed, ok := parseDuration(strings.TrimSpace(raw))
			if !ok || consumed != len(strings.TrimSpace(raw)) {
				return Activity{}, false
			}
			activity.Durations = append(activity.Durations, seconds)
		}
		return activity, len(activity.Durations) > 0
	}
	remaining := strings.TrimSpace(text)
	for remaining != "" {
		if seconds, consumed, ok := parseDuration(remaining); ok {
			if len(activity.Durations) > 0 {
				return Activity{}, false
			}
			activity.Durations = []int{seconds}
			remaining = strings.TrimSpace(remaining[consumed:])
			continue
		}
		if match := distancePattern.FindStringSubmatch(remaining); match != nil {
			if activity.DistanceKM != nil {
				return Activity{}, false
			}
			value, _ := strconv.ParseFloat(match[1], 64)
			if value <= 0 {
				return Activity{}, false
			}
			activity.DistanceKM = &value
			remaining = strings.TrimSpace(remaining[len(match[0]):])
			continue
		}
		if match := inclinePattern.FindStringSubmatch(remaining); match != nil {
			if activity.Incline != nil {
				return Activity{}, false
			}
			value, _ := strconv.ParseFloat(match[1], 64)
			activity.Incline = &value
			remaining = strings.TrimSpace(remaining[len(match[0]):])
			continue
		}
		return Activity{}, false
	}
	return activity, len(activity.Durations) > 0 || activity.DistanceKM != nil || activity.Incline != nil
}

func parseDuration(text string) (int, int, bool) {
	match := durationPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, 0, false
	}
	value := func(index int) int { n, _ := strconv.Atoi(match[index]); return n }
	hours, minutes, seconds := 0, 0, 0
	if match[1] != "" {
		hours, minutes, seconds = value(1), value(2), value(3)
		if minutes >= 60 || seconds >= 60 {
			return 0, 0, false
		}
	} else if match[4] != "" {
		minutes, seconds = value(4), value(5)
		if seconds >= 60 {
			return 0, 0, false
		}
	} else {
		seconds = value(6)
	}
	total := hours*3600 + minutes*60 + seconds
	return total, len(match[0]), total > 0
}

func FormatDuration(seconds int) string {
	parts := make([]string, 0, 3)
	if hours := seconds / 3600; hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes := seconds % 3600 / 60; minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if remainder := seconds % 60; remainder > 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainder))
	}
	return strings.Join(parts, " ")
}

func FormatKilometers(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) + " km" }
func FormatIncline(value float64) string    { return "incline " + strconv.FormatFloat(value, 'f', -1, 64) }

func FormatActivity(activity Activity) string {
	parts := make([]string, 0, 3)
	if len(activity.Durations) > 1 {
		values := make([]string, len(activity.Durations))
		for i, seconds := range activity.Durations {
			values[i] = strings.ReplaceAll(FormatDuration(seconds), " ", "")
		}
		parts = append(parts, strings.Join(values, ","))
	} else if len(activity.Durations) == 1 {
		parts = append(parts, strings.ReplaceAll(FormatDuration(activity.Durations[0]), " ", ""))
	}
	if activity.DistanceKM != nil {
		parts = append(parts, strings.ReplaceAll(FormatKilometers(*activity.DistanceKM), " ", ""))
	}
	if activity.Incline != nil {
		parts = append(parts, FormatIncline(*activity.Incline))
	}
	return strings.Join(parts, " · ")
}

func parseSet(token string) (Set, bool) {
	match := setPattern.FindStringSubmatch(token)
	if match == nil {
		return Set{}, false
	}
	reps, err := strconv.Atoi(match[1])
	if err != nil {
		return Set{}, false
	}
	set := Set{Reps: reps, LoadType: Bodyweight, Raw: token}
	if match[2] == "" {
		return set, true
	}
	weight, err := strconv.ParseFloat(match[3], 64)
	if err != nil {
		return Set{}, false
	}
	set.Weight = weight
	switch strings.ToLower(match[2]) {
	case "x":
		set.LoadType = External
	case "+":
		set.LoadType = Added
	case "-":
		set.LoadType = Assisted
	}
	return set, true
}
