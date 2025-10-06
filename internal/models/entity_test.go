package models_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// Run full entity store test suite
func TestEntityStore(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{})
}
