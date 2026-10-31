package handler

import (
	"net/http"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
)

// sessionOrLabOwnerID resolves owners for interactive workspace operations.
// The router authenticates Serve sessions or applies the global Lab access gate.
func sessionOrLabOwnerID(r *http.Request) (string, bool) {
	if principal, ok := session.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID), true
	}
	return labOwnerID(r)
}

func labOwnerID(r *http.Request) (string, bool) {
	if value, ok := actor.FromContext(r.Context()); ok && value.Source == "lab" && value.UserID == lab.DefaultOwnerID {
		return value.UserID, true
	}
	return "", false
}
