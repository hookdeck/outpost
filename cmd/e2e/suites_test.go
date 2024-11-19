package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type e2eSuite struct {
	ctx     context.Context
	config  *config.Config
	cleanup func()
	client  httpclient.Client
}

func (suite *e2eSuite) SetupSuite() {
	suite.client = httpclient.New(fmt.Sprintf("http://localhost:%d/api/v1", suite.config.Port), suite.config.APIKey)
	go func() {
		application := app.New(suite.config)
		application.Run(suite.ctx)
	}()
}

func (s *e2eSuite) TearDownSuite() {
	s.cleanup()
}

func (s *e2eSuite) AuthRequest(req httpclient.Request) httpclient.Request {
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", s.config.APIKey)
	return req
}

func (suite *e2eSuite) RunAPITests(t *testing.T, tests []APITest) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, suite.client)
		})
	}
}

type APITest struct {
	Name     string
	Request  httpclient.Request
	Expected httpclient.Response
}

func (test *APITest) Run(t *testing.T, client httpclient.Client) {
	resp, err := client.Do(test.Request)
	require.NoError(t, err)
	assert.Equal(t, test.Expected.StatusCode, resp.StatusCode)
	if test.Expected.Body != nil {
		assert.True(t, resp.MatchBody(test.Expected.Body), "expected body %s, got %s", test.Expected.Body, resp.Body)
	}
}

type basicSuite struct {
	suite.Suite
	e2eSuite
}

func (suite *basicSuite) SetupSuite() {
	config, cleanup, err := configs.Basic(suite.T())
	require.NoError(suite.T(), err)
	suite.e2eSuite = e2eSuite{
		ctx:     context.Background(),
		config:  config,
		cleanup: cleanup,
	}
	suite.e2eSuite.SetupSuite()

	// wait for outpost services to start
	time.Sleep(2 * time.Second)
}

func (s *basicSuite) TearDownSuite() {
	s.e2eSuite.TearDownSuite()
}

func TestBasicSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, new(basicSuite))
}
