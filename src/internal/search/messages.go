package search

// AsyncSearchCompleteMsg contains async search results
type AsyncSearchCompleteMsg struct {
	Query        string
	Results      []Searchable
	DepthLimited bool
	TimedOut     bool
	// Truncated is true when the final result set was capped at
	// DefaultMaxResults. See spec-006 AC-MEM-1.
	Truncated bool
	Error     error
}

// AsyncSearchPartialMsg is emitted mid-search when the coordinator has scored
// a first batch of results and wants to give the UI something to render before
// the full walk completes. See spec-006 contracts.new_message_types and
// decision 006-004 (first-batch only).
type AsyncSearchPartialMsg struct {
	Query     string
	Results   []Searchable
	Truncated bool
}

// SearchLayerPoppedMsg indicates a layer was removed
type SearchLayerPoppedMsg struct {
	RemainingLayers int
}

// SearchStartedMsg indicates a search has started
type SearchStartedMsg struct {
	Query string
	Mode  SearchMode
}
