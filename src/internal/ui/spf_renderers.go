package ui

import (
	"log/slog"

	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"
)

func FilePanelRenderer(totalHeight int, totalWidth int, filePanelFocussed bool) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)

	cfg.ContentFGColor = common.FilePanelFGColor
	cfg.ContentBGColor = common.FilePanelBGColor

	cfg.BorderRequired = true
	cfg.BorderBGColor = common.FilePanelBGColor
	cfg.BorderFGColor = common.FilePanelBorderColor
	// The title reads in the text color, or the active border's when focused;
	// what follows it, such as "9/15", is dimmer.
	cfg.TitleFGColor = common.FilePanelFGColor
	cfg.TitleSuffixFGColor = common.FilePanelBorderColor
	if filePanelFocussed {
		cfg.BorderFGColor = common.FilePanelBorderActiveColor
		cfg.TitleFGColor = common.FilePanelBorderActiveColor
	}
	cfg.Border = DefaultLipglossBorder()

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	return r
}

// PinnedPanelRenderer draws the preview pinned to a session (V): its frame
// and what follows the title ("pinned") in Claude's orange, so it can't be
// taken for the preview that follows the keyboard.
func PinnedPanelRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)
	cfg.ContentFGColor = common.FilePanelFGColor
	cfg.ContentBGColor = common.FilePanelBGColor
	cfg.BorderRequired = true
	cfg.BorderBGColor = common.FilePanelBGColor
	cfg.BorderFGColor = lipgloss.Color(common.ClaudeAgentColor)
	cfg.TitleFGColor = common.FilePanelFGColor
	cfg.TitleSuffixFGColor = lipgloss.Color(common.ClaudeAgentColor)
	cfg.Border = DefaultLipglossBorder()
	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	return r
}

func FilePreviewPanelRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	if totalHeight < 0 {
		totalHeight = 0
	}
	if totalWidth < 0 {
		totalWidth = 0
	}
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)
	cfg.ContentFGColor = common.FilePanelFGColor
	cfg.ContentBGColor = common.FilePanelBGColor
	cfg.BorderRequired = false

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to safe renderer", "error", err)
		fallbackCfg := rendering.DefaultRendererConfig(max(0, totalHeight), max(0, totalWidth))
		fallbackCfg.ContentFGColor = common.FilePanelFGColor
		fallbackCfg.ContentBGColor = common.FilePanelBGColor
		fallbackCfg.BorderRequired = false
		r, err = rendering.NewRenderer(fallbackCfg)
		if err != nil {
			slog.Error("Error in creating fallback renderer. Using zero-size renderer", "error", err)
			r, _ = rendering.NewRenderer(rendering.DefaultRendererConfig(0, 0))
		}
	}
	return r
}

// FilePreviewFrameRenderer draws the box around the preview pane, titled
// "Preview". It takes the keyboard's colors when the preview has focus.
func FilePreviewFrameRenderer(totalHeight int, totalWidth int, focused bool) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(max(0, totalHeight), max(0, totalWidth))
	cfg.ContentFGColor = common.FilePanelFGColor
	cfg.ContentBGColor = common.FilePanelBGColor
	cfg.BorderRequired = true
	cfg.BorderBGColor = common.FilePanelBGColor
	cfg.BorderFGColor = common.FilePanelBorderColor
	cfg.TitleFGColor = common.FilePanelFGColor
	if focused {
		cfg.BorderFGColor = common.FilePanelBorderActiveColor
		cfg.TitleFGColor = common.FilePanelBorderActiveColor
	}
	cfg.Border = DefaultLipglossBorder()

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	r.SetBorderTitle("Preview")
	if keys := common.Hotkeys.PinViewer; len(keys) > 0 && keys[0] != "" {
		r.SetBorderInfoItems(keys[0] + " pin") // as the dock's boxes give their key
	}
	return r
}

func PromptRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)
	cfg.TruncateHeight = true
	cfg.ContentFGColor = common.ModalFGColor
	cfg.ContentBGColor = common.ModalBGColor

	cfg.BorderRequired = true
	cfg.BorderBGColor = common.ModalBGColor
	cfg.BorderFGColor = common.ModalBorderActiveColor

	cfg.Border = DefaultLipglossBorder()

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	return r
}

// AskBarRenderer draws the Ask bar's frame.
func AskBarRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	return HelpMenuRenderer(totalHeight, totalWidth)
}

// SessionsSheetRenderer draws the Sessions sheet's frame.
func SessionsSheetRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	return HelpMenuRenderer(totalHeight, totalWidth)
}

func HelpMenuRenderer(totalHeight int, totalWidth int) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)
	cfg.ContentFGColor = common.ModalFGColor
	cfg.ContentBGColor = common.ModalBGColor

	cfg.BorderRequired = true
	cfg.BorderBGColor = common.ModalBGColor
	cfg.BorderFGColor = common.ModalBorderActiveColor

	cfg.Border = DefaultLipglossBorder()

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	return r
}

func DefaultFooterRenderer(totalHeight int, totalWidth int, focussed bool) *rendering.Renderer {
	cfg := rendering.DefaultRendererConfig(totalHeight, totalWidth)

	cfg.ContentFGColor = common.FooterFGColor
	cfg.ContentBGColor = common.FooterBGColor

	cfg.BorderRequired = true
	cfg.BorderBGColor = common.FooterBGColor
	cfg.BorderFGColor = common.FooterBorderColor
	cfg.TitleFGColor = common.FooterFGColor
	if focussed {
		cfg.BorderFGColor = common.FooterBorderActiveColor
		cfg.TitleFGColor = common.FooterBorderActiveColor
	}
	cfg.Border = DefaultLipglossBorder()

	r, err := rendering.NewRenderer(cfg)
	if err != nil {
		slog.Error("Error in creating renderer. Falling back to default renderer", "error", err)
		r = &rendering.Renderer{}
	}
	return r
}

func ProcessBarRenderer(totalHeight int, totalWidth int, processBarFocussed bool) *rendering.Renderer {
	r := DefaultFooterRenderer(totalHeight, totalWidth, processBarFocussed)
	r.SetBorderTitle("Processes")
	return r
}

func MetadataRenderer(totalHeight int, totalWidth int, metadataFocussed bool) *rendering.Renderer {
	r := DefaultFooterRenderer(totalHeight, totalWidth, metadataFocussed)
	r.SetBorderTitle("Metadata")
	return r
}

func DefaultLipglossBorder() lipgloss.Border {
	return lipgloss.Border{
		Top:         common.Config.BorderTop,
		Bottom:      common.Config.BorderBottom,
		Left:        common.Config.BorderLeft,
		Right:       common.Config.BorderRight,
		TopLeft:     common.Config.BorderTopLeft,
		TopRight:    common.Config.BorderTopRight,
		BottomLeft:  common.Config.BorderBottomLeft,
		BottomRight: common.Config.BorderBottomRight,
		MiddleLeft:  common.Config.BorderMiddleLeft,
		MiddleRight: common.Config.BorderMiddleRight,
	}
}
