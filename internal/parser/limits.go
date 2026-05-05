package parser

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type LimitPatternFile struct {
	Patterns         []TextPattern    `yaml:"patterns"`
	SystemErrorCodes []SystemErrCode  `yaml:"system_error_codes"`
}

type TextPattern struct {
	Pattern        string  `yaml:"pattern"`
	Classification string  `yaml:"classification"`
	Confidence     float64 `yaml:"confidence"`
}

type SystemErrCode struct {
	Status         int     `yaml:"status"`
	ErrorType      string  `yaml:"error_type"`
	Classification string  `yaml:"classification"`
	Confidence     float64 `yaml:"confidence"`
}

func LoadLimitPatterns(path string) (*LimitPatternFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f LimitPatternFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// DetectInText checks text (case-insensitive) against text patterns.
// Returns the first match with confidence >= threshold.
func (f *LimitPatternFile) DetectInText(text string, threshold float64) (classification, pattern string, confidence float64, ok bool) {
	lower := strings.ToLower(text)
	for _, p := range f.Patterns {
		if p.Confidence < threshold {
			continue
		}
		if strings.Contains(lower, strings.ToLower(p.Pattern)) {
			return p.Classification, p.Pattern, p.Confidence, true
		}
	}
	return "", "", 0, false
}

// DetectInSystemError checks a system error entry against code patterns.
func (f *LimitPatternFile) DetectInSystemError(status int, errType string, threshold float64) (classification, pattern string, confidence float64, ok bool) {
	for _, c := range f.SystemErrorCodes {
		if c.Confidence < threshold {
			continue
		}
		if c.Status != 0 && c.Status == status {
			return c.Classification, "HTTP " + statusStr(c.Status), c.Confidence, true
		}
		if c.ErrorType != "" && strings.EqualFold(c.ErrorType, errType) {
			return c.Classification, c.ErrorType, c.Confidence, true
		}
	}
	return "", "", 0, false
}

func statusStr(s int) string {
	switch s {
	case 429:
		return "429"
	default:
		return ""
	}
}
