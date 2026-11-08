package utils

// FilePanelMax defines the number of FOOTER columns (NOT max file panels!)
// This is used to divide the footer into 3 equal sections: processBar, metadata, clipboard
// Note: This is DIFFERENT from common.FilePanelMax (10) which defines max actual file panels
const FilePanelMax = 3

// BorderPaddingForFooter is the border padding for footer calculations
const BorderPaddingForFooter = 2

// FooterWidth calculates the width of each footer component
// The footer is divided into FilePanelMax (3) equal sections across fullWidth
// Formula: (fullWidth / 3) - borderPadding
// We have three panels, so 6 characters for border
// <---><---><--->
// Hence we have (fullWidth - 6) / 3 = fullWidth/3 - 2
func FooterWidth(fullWidth int) int {
	return fullWidth/FilePanelMax - BorderPaddingForFooter
}

// Including borders
func FullFooterHeight(footerHeight int, toggleFooter bool) int {
	if toggleFooter {
		return footerHeight + BorderPaddingForFooter
	}
	return 0
}
