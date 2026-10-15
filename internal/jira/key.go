package jira

import "regexp"

var (
	browseRe   = regexp.MustCompile(`(?i)/browse/([A-Z][A-Z0-9_]*-\d+)`)
	selectedRe = regexp.MustCompile(`(?i)selectedIssue=([A-Z][A-Z0-9_]*-\d+)`)
	anyKeyRe   = regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9_]*-\d+)\b`)
)

// ParseKey pulls an issue key out of pasted text: a browse URL, a board URL
// with ?selectedIssue=, or any text containing a key. Other text is returned
// unchanged (trimmed), like the original StopWatch.
func ParseKey(text string) string {
	for _, re := range []*regexp.Regexp{browseRe, selectedRe, anyKeyRe} {
		if m := re.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	}
	return text
}
