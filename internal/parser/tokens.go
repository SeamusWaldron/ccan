package parser

// EstimateTokens estimates token count from character count.
func EstimateTokens(chars int, charsPerTok int) int {
	if charsPerTok <= 0 {
		charsPerTok = 4
	}
	return chars / charsPerTok
}
