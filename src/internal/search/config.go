package search

import "time"

// ConfigBuilder provides a fluent interface for building SearchConfig
type ConfigBuilder struct {
	config SearchConfig
}

// NewConfigBuilder creates a new config builder with default values
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: DefaultConfig(),
	}
}

// WithRecursive enables or disables recursive search
func (b *ConfigBuilder) WithRecursive(enabled bool) *ConfigBuilder {
	b.config.EnableRecursive = enabled
	return b
}

// WithStacking enables or disables search layer stacking
func (b *ConfigBuilder) WithStacking(enabled bool) *ConfigBuilder {
	b.config.EnableStacking = enabled
	return b
}

// WithTwoStageESC enables or disables two-stage ESC behavior
func (b *ConfigBuilder) WithTwoStageESC(enabled bool) *ConfigBuilder {
	b.config.TwoStageESC = enabled
	return b
}

// WithMaxDepth sets the maximum recursion depth
func (b *ConfigBuilder) WithMaxDepth(depth int) *ConfigBuilder {
	b.config.MaxDepth = depth
	return b
}

// WithTimeout sets the search timeout
func (b *ConfigBuilder) WithTimeout(timeout time.Duration) *ConfigBuilder {
	b.config.Timeout = timeout
	return b
}

// WithFileFilter enables or disables f: prefix support
func (b *ConfigBuilder) WithFileFilter(enabled bool) *ConfigBuilder {
	b.config.AllowFileFilter = enabled
	return b
}

// WithDirFilter enables or disables d: prefix support
func (b *ConfigBuilder) WithDirFilter(enabled bool) *ConfigBuilder {
	b.config.AllowDirFilter = enabled
	return b
}

// WithCustomFilter sets a custom filter function
func (b *ConfigBuilder) WithCustomFilter(filter func(Searchable, string) bool) *ConfigBuilder {
	b.config.CustomFilter = filter
	return b
}

// Build returns the constructed SearchConfig
func (b *ConfigBuilder) Build() SearchConfig {
	return b.config
}

// Preset configurations for common use cases

// FilePanelConfig returns configuration for file panel
func FilePanelConfig() SearchConfig {
	return NewConfigBuilder().
		WithRecursive(true).
		WithStacking(true).
		WithTwoStageESC(true).
		WithMaxDepth(DefaultMaxDepth).
		WithTimeout(DefaultTimeout).
		WithFileFilter(true).
		WithDirFilter(true).
		Build()
}

// SidebarConfig returns configuration for sidebar
func SidebarConfig() SearchConfig {
	return NewConfigBuilder().
		WithRecursive(true).
		WithStacking(false).
		WithTwoStageESC(true).
		WithMaxDepth(DefaultMaxDepth).
		WithTimeout(DefaultTimeout).
		Build()
}

// HelpMenuConfig returns configuration for help menu
func HelpMenuConfig() SearchConfig {
	return NewConfigBuilder().
		WithRecursive(false).
		WithStacking(false).
		WithTwoStageESC(true).
		Build()
}
