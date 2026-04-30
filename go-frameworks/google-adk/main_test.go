package googleadk_test

import (
	"os"
	"testing"

	"github.com/grafana/sigil-sdk/go/sigil/sigiltest"
)

func TestMain(m *testing.M) {
	sigiltest.ClearAmbientEnv()
	os.Exit(m.Run())
}
