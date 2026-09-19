// Package resulttext bounds the reason text shared by check executors.
package resulttext

import (
	"strings"
	"unicode/utf8"
)

// MaxReasonBytes is the maximum stored size of a check's reason, in bytes.
const MaxReasonBytes = 4 * 1024

// LimitReason bounds a reason, including its prefix and truncation marker,
// before it reaches history. Transport errors can include untrusted text.
func LimitReason(reason string) string {
	reason = strings.ToValidUTF8(reason, "\uFFFD")
	if len(reason) <= MaxReasonBytes {
		return reason
	}
	const suffix = "... [truncated]"
	end := MaxReasonBytes - len(suffix)
	// Back up if the byte limit would split a multibyte character.
	for !utf8.RuneStart(reason[end]) {
		end--
	}
	return reason[:end] + suffix
}
