package analysis

import "math"

// computeRestrictionScore derives a 0-100 score where higher = more restricted.
// Positive changes in limit events and negative changes in usage both increase the score.
func computeRestrictionScore(changes map[string]float64) float64 {
	// Each metric gets a weight. Usage drops are restriction signals; limit increases are too.
	type weighted struct {
		key    string
		weight float64
		invert bool // if true, negative change = more restricted
	}

	factors := []weighted{
		{"messages_per_active_day", 0.25, true},
		{"tool_calls_per_active_day", 0.20, true},
		{"tokens_per_active_day", 0.20, true},
		{"active_mins_per_day", 0.15, true},
		{"sessions_per_active_day", 0.10, true},
		{"limit_events_per_active_day", 0.10, false},
	}

	score := 0.0
	for _, f := range factors {
		change := changes[f.key]
		var contribution float64
		if f.invert {
			// usage drop (negative change) → positive restriction contribution
			contribution = -change * f.weight
		} else {
			// limit event increase (positive change) → positive restriction contribution
			contribution = change * f.weight
		}
		score += contribution
	}

	// clamp to [-100, 100] and normalise to [0, 100]
	score = math.Max(-100, math.Min(100, score))
	return score
}
