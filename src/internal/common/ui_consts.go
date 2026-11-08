package common

import "time"

// ClaudeAgentColor marks anything a Claude agent is doing (the ✳ glyph). It is
// fixed rather than themed so agent activity reads the same in every theme.
const ClaudeAgentColor = "#f0a07a"

// CodexAgentColor marks a Codex session, as ClaudeAgentColor marks Claude
// Code's: its cc or co tag, and its name in the s popup.
const CodexAgentColor = "#7aa2f7"

// ClaudeNeedsYouColor marks a session that needs you (the ⚑ glyph); git's
// letter for a changed file uses it too.
const ClaudeNeedsYouColor = "#f9e2af"

// GitUntrackedColor is git's "?" for an untracked file.
const GitUntrackedColor = "#6c7086"

// Shared UI/layout constants to replace magic numbers flagged by mnd.
const (
	HelpKeyColumnWidth       = 55              // width of help key column in CLI help
	DefaultCLIContextTimeout = 5 * time.Second // default CLI context timeout for CLI ops

	PanelPadding    = 3 // rows reserved around file list (borders/header/footer)
	BorderPadding   = 2 // rows/cols for outer border frame
	InnerPadding    = 4 // cols for inner content padding (truncate widths)
	FooterGroupCols = 3 // columns per group in footer layout math

	DefaultFilePanelWidth    = 10 // default width for file panels
	FilePanelMax             = 10 // max number of file panels supported
	MinWidthForRename        = 18 // minimal width for rename input to render
	SuperfileInitialWidthPct = 50 // percentage of total tab width reserved for superfile after the first agent split
	SuperfileManagedMinCols  = 62 // minimum width reserved for superfile when opening the first managed agent pane
	ManagedAgentPaneCols     = 1  // fixed number of columns in the managed agent area
	ManagedAgentPaneRows     = 2  // fixed number of rows in the managed agent area
	ManagedAgentPaneCapacity = ManagedAgentPaneCols * ManagedAgentPaneRows
	ResponsiveWidthThreshold = 95 // width breakpoint for layout behavior

	HeightBreakA = 30 // responsive height tiers
	HeightBreakB = 35
	HeightBreakC = 40
	HeightBreakD = 45

	ReRenderChunkDivisor = 100 // divisor for re-render throttling

	FilePanelWidthUnit    = 20                     // width unit used to calculate max file panels
	DefaultPreviewTimeout = 500 * time.Millisecond // preview operation timeout

	// File permissions

	// UI positioning
	CenterDivisor = 2 // divisor for centering UI elements
)
