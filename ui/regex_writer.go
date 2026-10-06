package ui

import (
	"fmt"
	"io"
	"regexp"
)

var (
	regexConsolePattern = regexp.MustCompile(`(?i)([^=:\s]*?(?:reg(?:exp|ex)|matcher))(=|:\s*)(?:"([^"]{200})[^"]*"|([^\s]{200})[^\s]*)`)
	regexJSONPattern    = regexp.MustCompile(`(?i)("(?:[^"]*?(?:reg(?:exp|ex)|matcher))"):\s*"([^"]{200})[^"]*"`)
)

// RegexTruncatingWriter wraps an io.Writer to truncate any regex fields exceeding MaxLen characters (default 200).
type RegexTruncatingWriter struct {
	Writer io.Writer
	MaxLen int
}

func (w *RegexTruncatingWriter) Write(p []byte) (int, error) {
	if w.Writer == nil {
		return len(p), nil
	}

	limit := w.MaxLen
	if limit <= 0 {
		limit = 200
	}

	out := p
	if limit == 200 {
		out = regexConsolePattern.ReplaceAll(out, []byte(`${1}${2}"${3}${4}..."`))
		out = regexJSONPattern.ReplaceAll(out, []byte(`${1}: "${2}..."`))
	} else {
		patternConsole := regexp.MustCompile(fmt.Sprintf(`(?i)([^=:\s]*?(?:reg(?:exp|ex)|matcher))(=|:\s*)(?:"([^"]{%d})[^"]*"|([^\s]{%d})[^\s]*)`, limit, limit))
		patternJSON := regexp.MustCompile(fmt.Sprintf(`(?i)("(?:[^"]*?(?:reg(?:exp|ex)|matcher))"):\s*"([^"]{%d})[^"]*"`, limit))
		out = patternConsole.ReplaceAll(out, []byte(`${1}${2}"${3}${4}..."`))
		out = patternJSON.ReplaceAll(out, []byte(`${1}: "${2}..."`))
	}

	_, err := w.Writer.Write(out)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
