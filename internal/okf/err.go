package okf

import "errors"

var (
	ErrInvalidDocument    = errors.New("invalid document")
	ErrInvalidFrontmatter = errors.New("invalid frontmatter")
)
