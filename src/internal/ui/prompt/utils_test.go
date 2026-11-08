package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func TestModel_getPromptAction(t *testing.T) {
	// Notes of Things we tested
	// About Tokenization failure. Don't test all failures,
	// it will be in tokenize_test.go

	testdata := []struct {
		name           string
		text           string
		shellMode      bool
		expectecAction common.ModelAction
		expectedErr    bool
		expectedErrMsg string
	}{
		{
			name:           "No Action",
			text:           "",
			shellMode:      true,
			expectecAction: common.NoAction{},
			expectedErr:    false,
			expectedErrMsg: "",
		},
		{
			name:      "Shell command",
			text:      "abc xyz /def",
			shellMode: true,
			expectecAction: common.ShellCommandAction{
				Command: "abc xyz /def",
			},
			expectedErr:    false,
			expectedErrMsg: "",
		},
		{
			name:           "Tokenization failure",
			text:           "cd ${sdfdsf", // Missing "}"
			shellMode:      false,
			expectecAction: common.NoAction{},
			expectedErr:    true,
			expectedErrMsg: tokenizationError + " : " + curlyBracketMatchError().Error(),
		},
		{
			name:           "Invalid command",
			text:           "abcd",
			shellMode:      false,
			expectecAction: common.NoAction{},
			expectedErr:    true,
			expectedErrMsg: "Invalid spf command : abcd (use claude or codex)",
		},
		{
			name:           "Correct codex command",
			text:           CodexCommand,
			shellMode:      false,
			expectecAction: common.CodexAction{},
			expectedErr:    false,
			expectedErrMsg: "",
		},
		{
			name:           "codex with arguments",
			text:           CodexCommand + " xyz",
			shellMode:      false,
			expectecAction: common.NoAction{},
			expectedErr:    true,
			expectedErrMsg: "codex command accepts no arguments; usage: codex",
		},
		{
			name:           "Removed commands are gone",
			text:           "cd /abc",
			shellMode:      false,
			expectecAction: common.NoAction{},
			expectedErr:    true,
			expectedErrMsg: "Invalid spf command : cd (use claude or codex)",
		},
	}

	for _, tt := range testdata {
		t.Run(tt.name, func(t *testing.T) {
			action, err := getPromptAction(tt.shellMode, tt.text, "/")
			if err != nil {
				assert.True(t, tt.expectedErr)
				//nolint: errorlint // We don't expect a wrapped error here, so using type assertion
				cmdErr, ok := err.(invalidCmdError)
				assert.True(t, ok)
				if tt.expectedErrMsg != "" {
					assert.Equal(t, tt.expectedErrMsg, cmdErr.uiMessage())
				}
			}

			assert.Equal(t, tt.expectecAction, action)
		})
	}
}

func Test_getFirstToken(t *testing.T) {
	t.Run("Basic test", func(t *testing.T) {
		assert.Equal(t, "abc", getFirstToken("abc"))
		assert.Equal(t, "abc", getFirstToken("abc a b c"))
		assert.Equal(t, "abc", getFirstToken("abc "))
		assert.Equal(t, "abc", getFirstToken("  abc "))
		assert.Equal(t, "abc\n\ta", getFirstToken("abc\n\ta"))
	})
}
