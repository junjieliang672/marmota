//go:build darwin && cgo

package words

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

// tokens writes the words of the n bytes of UTF-8 at s into out, each as
// where it starts and how long it is, in UTF-16 units, and returns how many
// there are: at most max. It returns -1 for bytes that are no text.
static long tokens(const char *s, long n, long *out, long max) {
	CFStringRef text = CFStringCreateWithBytes(NULL, (const UInt8 *)s, n, kCFStringEncodingUTF8, false);
	if (text == NULL) {
		return -1;
	}
	CFStringTokenizerRef t = CFStringTokenizerCreate(NULL, text, CFRangeMake(0, CFStringGetLength(text)),
		kCFStringTokenizerUnitWord, NULL);
	long count = 0;
	if (t != NULL) {
		while (count < max && CFStringTokenizerAdvanceToNextToken(t) != kCFStringTokenizerTokenNone) {
			CFRange r = CFStringTokenizerGetCurrentTokenRange(t);
			out[2 * count] = r.location;
			out[2 * count + 1] = r.length;
			count++;
		}
		CFRelease(t);
	}
	CFRelease(text);
	return count;
}
*/
import "C"

import (
	"unicode/utf16"
	"unsafe"
)

const supported = true

// bounds asks the Mac's tokenizer (CFStringTokenizer, by word) for the
// words of text. It counts in UTF-16 units, as its strings do: they are
// counted again in characters.
func bounds(text string) [][2]int {
	if text == "" {
		return nil
	}
	// A text has no more words than characters.
	limit := len(text)
	out := make([]C.long, 2*limit)
	bytes := []byte(text)
	n := C.tokens((*C.char)(unsafe.Pointer(&bytes[0])), C.long(len(bytes)), &out[0], C.long(limit))
	if n <= 0 {
		return nil
	}

	// at[u] is the character that starts at UTF-16 unit u.
	at := make([]int, 0, len(text)+1)
	chars := 0
	for _, r := range text {
		at = append(at, chars)
		if utf16.RuneLen(r) == 2 { //nolint:mnd // a character outside the first 65536 takes two units
			at = append(at, chars)
		}
		chars++
	}
	at = append(at, chars)

	words := make([][2]int, 0, int(n))
	for i := range int(n) {
		start, length := int(out[2*i]), int(out[2*i+1])
		if start < 0 || start+length >= len(at) || length <= 0 {
			continue
		}
		words = append(words, [2]int{at[start], at[start+length]})
	}
	return words
}
