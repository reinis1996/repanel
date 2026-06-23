package api

import (
	"net/http"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestFunctionURL(t *testing.T) {
	got := functionURL(models.Function{Hostname: "x.function-url.example.com"})
	if got != "https://x.function-url.example.com" {
		t.Errorf("functionURL = %q", got)
	}
}

// Routes must register without panicking — in particular the literal
// /api/functions/meta and the /api/functions/{id} wildcard must not be an
// ambiguous pair for Go's ServeMux (the literal takes precedence).
func TestRoutesRegister(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked: %v", r)
		}
	}()
	(&Server{}).Routes(http.NewServeMux())
}
