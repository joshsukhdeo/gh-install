package ui

import (
	"regexp"
	"strings"
)

var (
	emojiRx      = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2300}-\x{23FF}\x{2B50}\x{FE0E}\x{FE0F}]`)
	multiSpaceRx = regexp.MustCompile(`[ ]{2,}`)
)

// StripEmojis removes emojis and symbols from text and collapses excess whitespace.
func StripEmojis(s string) string {
	res := emojiRx.ReplaceAllString(s, "")
	res = multiSpaceRx.ReplaceAllString(res, " ")
	return strings.TrimSpace(res)
}
