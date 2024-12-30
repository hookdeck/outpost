package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type e2eSuite struct {
	ctx               context.Context
	config            *config.Config
	mockServerBaseURL string
	mockServerInfra   *testinfra.MockServerInfra
	cleanup           func()
	client            httpclient.Client
}

func (suite *e2eSuite) SetupSuite() {
	suite.client = httpclient.New(fmt.Sprintf("http://localhost:%d/api/v1", suite.config.Port), suite.config.APIKey)
	go func() {
		application := app.New(suite.config)
		if err := application.Run(suite.ctx); err != nil {
			log.Println("Application failed to run", err)
		}
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

func (s *e2eSuite) AuthJWTRequest(req httpclient.Request, token string) httpclient.Request {
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	return req
}

func (suite *e2eSuite) RunAPITests(t *testing.T, tests []APITest) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, suite.client)
		})
	}
}

type APITest struct {
	Name     string
	Delay    time.Duration
	Request  httpclient.Request
	Expected APITestExpectation
}

type APITestExpectation struct {
	Match    *httpclient.Response
	Validate map[string]interface{}
}

func (test *APITest) Run(t *testing.T, client httpclient.Client) {
	t.Helper()

	if test.Delay > 0 {
		time.Sleep(test.Delay)
	}

	resp, err := client.Do(test.Request)
	require.NoError(t, err)

	if test.Expected.Match != nil {
		assert.Equal(t, test.Expected.Match.StatusCode, resp.StatusCode)
		if test.Expected.Match.Body != nil {
			assert.True(t, resp.MatchBody(test.Expected.Match.Body), "expected body %s, got %s", test.Expected.Match.Body, resp.Body)
		}
	}

	if test.Expected.Validate != nil {
		c := jsonschema.NewCompiler()
		require.NoError(t, c.AddResource("schema.json", test.Expected.Validate))
		schema, err := c.Compile("schema.json")
		require.NoError(t, err, "failed to compile schema: %v", err)
		respStr, _ := json.Marshal(resp)
		var respJSON map[string]interface{}
		require.NoError(t, json.Unmarshal(respStr, &respJSON), "failed to parse response: %v", err)
		validationErr := schema.Validate(respJSON)
		if assert.NoError(t, validationErr, "response validation failed: %v: %s", validationErr, respJSON) == false {
			log.Println(resp)
		}
	}
}

type basicSuite struct {
	suite.Suite
	e2eSuite
}

func (suite *basicSuite) SetupSuite() {
	t := suite.T()
	t.Cleanup(testinfra.Start(t))
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)
	suite.e2eSuite = e2eSuite{
		ctx:               context.Background(),
		config:            configs.Basic(t),
		mockServerBaseURL: mockServerBaseURL,
		mockServerInfra:   testinfra.NewMockServerInfra(mockServerBaseURL),
		cleanup:           func() {},
	}
	suite.e2eSuite.SetupSuite()

	// wait for outpost services to start
	// TODO: replace with a health check
	time.Sleep(2 * time.Second)
}

func (s *basicSuite) TearDownSuite() {
	s.e2eSuite.TearDownSuite()
}

func TestBasicSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, new(basicSuite))
}
