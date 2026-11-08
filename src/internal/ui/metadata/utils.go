package metadata

import (
	"fmt"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func getMaxKeyLength(meta [][2]string) int {
	maxLen := 0
	for _, pair := range meta {
		if len(pair[0]) > maxLen {
			maxLen = len(pair[0])
		}
	}
	return maxLen
}

func computeMetadataWidths(viewWidth, maxKeyLen int) (int, int) {
	keyLen := maxKeyLen
	valueLen := viewWidth - keyLen
	if valueLen < viewWidth/2 {
		//nolint:mnd // standard halving for center split
		valueLen = viewWidth / 2
		keyLen = viewWidth - valueLen
	}

	return keyLen, valueLen
}

// TODO : Simplify these mystic calculations, or add explanation comments.
// TODO : unit test and fix this mess
func formatMetadataLines(meta [][2]string, startIdx, height, keyLen, valueLen int) []string {
	lines := []string{}
	endIdx := min(startIdx+height, len(meta))
	for i := startIdx; i < endIdx; i++ {
		key := meta[i][0]
		value := meta[i][1]

		// Special handling for Name field (full file path) - split across multiple lines
		if key == keyName && len(value) > valueLen {
			keyTrunc := common.TruncateMiddleText(key, keyLen, "...")
			// First line with key
			firstChunk := value
			if len(value) > valueLen {
				firstChunk = value[:valueLen]
			}
			line := fmt.Sprintf("%-*s%s%s", keyLen, keyTrunc, keyValueSpacing, firstChunk)
			lines = append(lines, line)

			// Continue with remaining path chunks on subsequent lines (without key)
			remaining := value[len(firstChunk):]
			for len(remaining) > 0 {
				if len(lines) >= height {
					break // Don't exceed height limit
				}
				chunk := remaining
				if len(chunk) > valueLen {
					chunk = remaining[:valueLen]
				}
				// Indent continuation lines to align with value column
				line := fmt.Sprintf("%-*s%s%s", keyLen, "", keyValueSpacing, chunk)
				lines = append(lines, line)
				remaining = remaining[len(chunk):]
			}
		} else {
			// Normal handling for other fields - truncate if needed
			valueTrunc := common.TruncateMiddleText(value, valueLen, "...")
			keyTrunc := common.TruncateMiddleText(key, keyLen, "...")
			line := fmt.Sprintf("%-*s%s%s", keyLen, keyTrunc, keyValueSpacing, valueTrunc)
			lines = append(lines, line)
		}
	}
	return lines
}

func computeRenderDimensions(metadata [][2]string, viewWidth int) (int, int) {
	// Compute dimension based values
	maxKeyLen := getMaxKeyLength(metadata)
	return computeMetadataWidths(viewWidth, maxKeyLen)
}
