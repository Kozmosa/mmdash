package repo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModuleRejectsMalformedCommitChanges(t *testing.T) {
	const head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requestPrefix := `{"workspace":"code","expected_head_sha":"` + head +
		`","message":"test","idempotency_key":"request-1","changes":[`
	testCases := []string{
		`{"operation":"put","path":"safe.txt","content_base64":"%%%"}`,
		`{"operation":"put","path":"safe.txt","content_base64":"c2FmZQ==","extra":true}`,
		`{"operation":"put","path":"safe.txt"}`,
		`{"operation":"delete","path":"safe.txt","content_base64":""}`,
		`{"operation":"chmod","path":"safe.txt"}`,
	}
	handler := (Module{Service: Service{
		Access: &serviceAccess{},
	}}).ProjectHandler()
	for index, change := range testCases {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/projects/project-1/repository/commits",
			strings.NewReader(requestPrefix+change+`]}`),
		)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf(
				"case %d returned %d: %s",
				index, response.Code, response.Body.String(),
			)
		}
	}
}

func TestCheckoutJSONDoesNotExposeManagedPathOrActor(t *testing.T) {
	contents, err := json.Marshal(Checkout{
		CheckoutID:      "checkout-1",
		CheckoutRelpath: "checkouts/private",
		CreatedBy:       "user-private",
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(contents)
	if strings.Contains(serialized, "private") {
		t.Fatalf("checkout response exposed private fields: %s", serialized)
	}
}
