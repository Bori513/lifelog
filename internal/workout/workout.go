// Package workout parses LifeLog's compact, raw-text-first workout notation.
package workout

import (
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
	Name string
	Sets []Set
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

// Parse always retains raw exactly. Unrecognized lines and sets become issues;
// valid sets on a partially malformed line are still returned.
func Parse(raw string) Workout {
	result := Workout{Raw: raw}
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, setText, ok := splitLine(line)
		if !ok {
			result.Issues = append(result.Issues, Issue{Line: line, Token: line})
			continue
		}
		exercise := Exercise{Name: name}
		for _, rawSet := range strings.Split(setText, ",") {
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
		}
	}
	return "", "", false
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
