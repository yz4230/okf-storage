package okf

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

// Frontmatter holds the top-level fields of a document's YAML frontmatter.
type Frontmatter map[string]any

// parseFrontmatter decodes data into a non-nil Frontmatter, so that a
// document with empty frontmatter is told apart from one without any.
func parseFrontmatter(data string) (Frontmatter, error) {
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(data), &fm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFrontmatter, err)
	}
	if fm == nil {
		fm = Frontmatter{}
	}
	return fm, nil
}
