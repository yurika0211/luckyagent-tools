package memory

import "strings"

// ContextText is the note section to inject. A missing locator keeps the
// whole note and says the named section was not found, instead of inventing one.
func (e ActivationExcerpt) ContextText() string {
	context := strings.TrimSpace(e.Context)
	if !e.Missing {
		return context
	}
	where := excerptLocator(e.Heading, e.Block)
	if where == "" {
		return context
	}
	if context == "" {
		return "(section not found: " + where + ")"
	}
	return context + "\n(section not found: " + where + ")"
}

func excerptLocator(heading, block string) string {
	if block != "" {
		return "^" + block
	}
	return strings.TrimSpace(heading)
}

// SelectContextText returns the located section when a wikilink named one.
// Notes without a heading or block locator keep their whole body. The stored
// note is unchanged either way.
func SelectContextText(content string, excerpts []ActivationExcerpt) string {
	content = strings.TrimSpace(content)
	for _, excerpt := range excerpts {
		if excerpt.Heading == "" && excerpt.Block == "" {
			continue
		}
		if selected := excerpt.ContextText(); selected != "" {
			return selected
		}
	}
	return content
}
