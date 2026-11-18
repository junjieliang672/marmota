// Package words finds the words of a text the way the system does, so that
// moving and deleting by word (option+← →, option+backspace) stop where they
// do in any other text field of the computer. A program's own rule, "what
// lies between two spaces" or "a run of letters", takes a whole Chinese
// sentence for one word, since it has no spaces and its characters are
// letters: the system's tokenizer knows 我认为应该这样 as 我, 认为, 应该, 这样.
//
// Moving and deleting share one rule: what option+backspace deletes is what
// option+← moves over.
//
// The tokenizer is the Mac's (words_darwin.go). Elsewhere Supported is
// false, and callers keep the rule they had.
package words

// Bounds are the words of text, in order: each is where it starts and where
// it ends, counted in characters. Spaces and punctuation are in no word.
func Bounds(text string) [][2]int {
	return bounds(text)
}

// Supported reports whether this build can ask the system for words.
func Supported() bool {
	return supported
}

// Left is where moving left by a word from pos goes in text: the start of
// the nearest word that starts before pos, the text's start with none. What
// lies between, spaces and punctuation, is moved over with the word.
func Left(text []rune, pos int) int {
	return leftOf(Bounds(string(text)), pos)
}

// Right is where moving right by a word from pos goes in text: the end of
// the nearest word that ends after pos, the text's end with none.
func Right(text []rune, pos int) int {
	return rightOf(Bounds(string(text)), pos, len(text))
}

func leftOf(words [][2]int, pos int) int {
	to := 0
	for _, w := range words {
		if w[0] >= pos {
			break
		}
		to = w[0]
	}
	return to
}

func rightOf(words [][2]int, pos, end int) int {
	for _, w := range words {
		if w[1] > pos {
			return w[1]
		}
	}
	return end
}
