package metadata

import "github.com/junjieliang672/marmota/src/config/icon"

// Spacing between Key and Value while rendering
const keyValueSpacing = " "
const keyValueSpacingLen = 1

const fileStatErrorMsg = "Cannot load file stats"
const linkFileBrokenMsg = "Link file is broken!"

const borderSize = 2

// Metadata keys using icons. They are variables, as the icons are, but
// never change.
var (
	keyName         = icon.FileTag    //nolint:gochecknoglobals // file name
	keySize         = icon.Size       //nolint:gochecknoglobals // file size
	keyDataModified = icon.Calendar   //nolint:gochecknoglobals // date modified
	keyPermissions  = icon.Lock       //nolint:gochecknoglobals // permissions
	keyResolution   = icon.Resolution //nolint:gochecknoglobals // image resolution
	keyPage         = icon.Page       //nolint:gochecknoglobals // PDF page
)

const keyPath = "Path"

var sortPriority = map[string]int{ //nolint: gochecknoglobals // This is effectively const.
	// Metadata field priority indices for display ordering
	// Only these fields are displayed (Resolution for images, Page for PDFs)
	keyName:         0,
	keySize:         1,
	keyDataModified: 2, //nolint:mnd // display order index
	keyPermissions:  3, //nolint:mnd // display order index
	keyResolution:   4, //nolint:mnd // display order index
	keyPage:         5, //nolint:mnd // display order index
}
