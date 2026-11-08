package utils

import (
	"context"
	"time"

	"github.com/reinhrst/fzf-lib"
)

// Returning a string slice causes inefficiency in current usage
func FzfSearch(query string, source []string) []fzf.MatchResult {
	fzfSearcher := fzf.New(source, fzf.DefaultOptions())
	fzfSearcher.Search(query)
	// TODO : This is a blocking call, which will cause the UI to freeze if the query is slow.
	// Need to put a timeout on this
	fzfResults := <-fzfSearcher.GetResultChannel()
	fzfSearcher.End()
	return fzfResults.Matches
}

// FzfSearchWithTimeout searches with a timeout and returns partial results if exceeded
func FzfSearchWithTimeout(
	ctx context.Context,
	query string,
	source []string,
	timeout time.Duration,
) ([]fzf.MatchResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fzfSearcher := fzf.New(source, fzf.DefaultOptions())
	fzfSearcher.Search(query)

	select {
	case fzfResults := <-fzfSearcher.GetResultChannel():
		fzfSearcher.End()
		return fzfResults.Matches, false // false = no timeout
	case <-ctx.Done():
		fzfSearcher.End()
		// Return empty results on timeout
		return nil, true // true = timeout occurred
	}
}
