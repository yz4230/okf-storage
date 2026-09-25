package okf

import (
	"fmt"
	"strings"
)

const frontmatterDelim = "---"

type Document struct {
	Frontmatter *Frontmatter
	Body        string
}

// ParseDocument splits doc into YAML frontmatter and a markdown body.
//
// A document that does not start with a `---` line has no frontmatter and is
// returned with a nil Frontmatter and doc as its Body. This is the case for
// reserved files such as index.md and log.md (§8, §9). Use
// [Frontmatter.Validate] to check that a concept document is conformant.
func ParseDocument(doc string) (*Document, error) {
	first := doc
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		first = doc[:i+1]
	}
	if !isDelim(first) {
		return &Document{Frontmatter: nil, Body: doc}, nil
	}

	rest := doc[len(first):]
	offset := 0
	for line := range strings.Lines(rest) {
		if isDelim(line) {
			fm, err := parseFrontmatter(rest[:offset])
			if err != nil {
				return nil, err
			}
			return &Document{Frontmatter: fm, Body: rest[offset+len(line):]}, nil
		}
		offset += len(line)
	}
	return nil, fmt.Errorf("%w: unterminated frontmatter", ErrInvalidDocument)
}

func isDelim(line string) bool {
	return strings.TrimRight(line, " \t\r\n") == frontmatterDelim
}
