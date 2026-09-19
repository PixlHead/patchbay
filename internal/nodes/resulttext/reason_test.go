package resulttext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLimitReason(t *testing.T) {
	const suffix = "... [truncated]"
	for _, test := range []struct {
		name, input, want string
	}{
		{"short", "Request failed: connection refused", "Request failed: connection refused"},
		{"exact byte limit", strings.Repeat("a", MaxReasonBytes), strings.Repeat("a", MaxReasonBytes)},
		{"over byte limit", strings.Repeat("a", MaxReasonBytes+1), strings.Repeat("a", MaxReasonBytes-len(suffix)) + suffix},
		{"multibyte boundary", strings.Repeat("界", MaxReasonBytes), strings.Repeat("界", (MaxReasonBytes-len(suffix))/len("界")) + suffix},
		{"invalid UTF-8", "before\xff\xfeafter", "before\uFFFDafter"},
		// Replacing invalid bytes can grow text that originally fit the byte limit.
		{"UTF-8 replacement grows text", strings.Repeat("\xffa", MaxReasonBytes/2), strings.Repeat("\uFFFDa", (MaxReasonBytes-len(suffix))/4) + suffix},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := LimitReason(test.input)
			if got != test.want || len(got) > MaxReasonBytes || !utf8.ValidString(got) {
				t.Fatalf("unexpected bounded reason: length=%d validUTF8=%v", len(got), utf8.ValidString(got))
			}
		})
	}
}
