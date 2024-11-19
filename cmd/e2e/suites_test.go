package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	port   int
	apiKey string
}

func (c *testClient) GET(path string) (*http.Response, error) {
	request, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d%s", c.port, path), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	return http.DefaultClient.Do(request)
}

func (c *testClient) POST(path string, body map[string]interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}
	request, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d%s", c.port, path), bodyReader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(request)
}

func (c *testClient) PUT(path string, body map[string]interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}
	request, err := http.NewRequest("PUT", fmt.Sprintf("http://localhost:%d%s", c.port, path), bodyReader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(request)
}

func (c *testClient) PATCH(path string, body map[string]interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}
	request, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d%s", c.port, path), bodyReader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(request)
}

func (c *testClient) DELETE(path string) (*http.Response, error) {
	request, err := http.NewRequest("DELETE", fmt.Sprintf("http://localhost:%d%s", c.port, path), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	return http.DefaultClient.Do(request)
}

func (c *testClient) ParseBody(resp *http.Response) (map[string]interface{}, error) {
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

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
	client *testClient
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
	suite.client = &testClient{port: config.Port, apiKey: config.APIKey}
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
