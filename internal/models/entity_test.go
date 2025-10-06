package models_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

func TestEntityStore_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{deploymentID: ""})
}

func TestEntityStore_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{deploymentID: "dp_test_001"})
}
