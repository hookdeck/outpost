package e2e_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// runMethodsInParallel runs each Test method of a suite as a parallel subtest
// on its own suite instance, from SetupSuite to TearDownSuite, so each method
// gets its own Outpost.
//
// The subtests keep the names suite.Run gives them, so -run filters still
// apply. Not supported: suite.Run's -testify.m filter and the
// BeforeTest/AfterTest and stats hooks.
func runMethodsInParallel[S suite.TestingSuite](t *testing.T, newSuite func() S) {
	t.Helper()
	typ := reflect.TypeOf(newSuite())
	for i := range typ.NumMethod() {
		method := typ.Method(i)
		if !strings.HasPrefix(method.Name, "Test") {
			continue
		}
		t.Run(method.Name, func(t *testing.T) {
			t.Parallel()
			s := newSuite()
			s.SetT(t)
			s.SetS(s)
			if tearDown, ok := any(s).(suite.TearDownAllSuite); ok {
				defer tearDown.TearDownSuite()
			}
			if setup, ok := any(s).(suite.SetupAllSuite); ok {
				setup.SetupSuite()
			}
			if tearDown, ok := any(s).(suite.TearDownTestSuite); ok {
				defer tearDown.TearDownTest()
			}
			if setup, ok := any(s).(suite.SetupTestSuite); ok {
				setup.SetupTest()
			}
			method.Func.Call([]reflect.Value{reflect.ValueOf(s)})
		})
	}
}
