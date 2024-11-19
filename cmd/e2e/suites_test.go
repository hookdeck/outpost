package e2e_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/suite"
)

type e2eSuite struct {
	ctx     context.Context
	config  *config.Config
	cleanup func()
}

func (suite *e2eSuite) SetupSuite() {
	go func() {
		application := app.New(suite.config)
		application.Run(suite.ctx)
	}()
}

func (s *e2eSuite) TearDownSuite() {
	s.cleanup()
}

type basicSuite struct {
	suite.Suite
	e2eSuite
	client httpclient.Client
}

func (suite *basicSuite) SetupSuite() {
	config, cleanup, err := configs.Basic(suite.T())
	log.Println(config.Port)
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.e2eSuite = e2eSuite{
		ctx:     context.Background(),
		config:  config,
		cleanup: cleanup,
	}
	suite.client = httpclient.New(fmt.Sprintf("http://localhost:%d/api/v1", config.Port), config.APIKey)
	suite.e2eSuite.SetupSuite()

	// wait for outpost services to start
	time.Sleep(2 * time.Second)
}

func (s *basicSuite) TearDownSuite() {
	s.e2eSuite.TearDownSuite()
}

func (s *basicSuite) AuthRequest(req httpclient.Request) httpclient.Request {
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", s.config.APIKey)
	return req
}

func TestBasicSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, new(basicSuite))
}
