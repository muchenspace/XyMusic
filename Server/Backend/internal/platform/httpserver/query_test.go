package httpserver

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLastQueryValueUsesFinalRepeatedValue(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?value=first&unknown=true&value=last", nil)
	value, present := LastQueryValue(context, "value")
	if !present || value != "last" {
		t.Fatalf("value/present = %q/%v", value, present)
	}
	if _, present := LastQueryValue(context, "missing"); present {
		t.Fatal("missing query value was reported as present")
	}
}
