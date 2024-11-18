package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/suite"
)

type testClient struct {
	port int
}

func (c *testClient) Get(path string) (*http.Response, error) {
	request, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d%s", c.port, path), nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(request)
}

type e2eSuite struct {
	ctx     context.Context
	config  *config.Config
	cleanup func()
}

func (suite *e2eSuite) SetupTest() {
	go func() {
		application := app.New(suite.config)
		application.Run(suite.ctx)
	}()
}

func (s *e2eSuite) TearDownTest() {
	s.cleanup()
}

type basicSuite struct {
	suite.Suite
	e2eSuite
	client *testClient
}

func (suite *basicSuite) SetupTest() {
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
	suite.client = &testClient{port: config.Port}
	suite.e2eSuite.SetupTest()

	// wait for outpost services to start
	time.Sleep(2 * time.Second)
}

func (s *basicSuite) TearDownTest() {
	s.e2eSuite.TearDownTest()
}

func TestBasicSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, new(basicSuite))
}
