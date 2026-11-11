package api_test

import (
	"strconv"
	"testing"

	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func testManager(t *testing.T, adapter bool) (*testsupport.Backend, *testsupport.FakeCollector) {
	b := testsupport.NewBackend(t, adapter)

	return b, b.Fake
}

func apiRequest(t *testing.T, b *testsupport.Backend, method, path string, status int) map[string]any {
	return testsupport.Request(t, b.Handler(), method, path, status)
}

func writeFixture(t *testing.T, path, text string) {
	testsupport.Write(t, path, text)
}

func strconvTest(n int64) string {
	return strconv.FormatInt(n, 10)
}
