package bundle_test

import (
	"testing"

	"github.com/yz4230/okf-storage/internal/bundle"
	"github.com/yz4230/okf-storage/internal/bundle/catalogtest"
)

func TestMemCatalog(t *testing.T) {
	catalogtest.Run(t, func(*testing.T) bundle.Catalog { return bundle.NewMemCatalog() })
}
