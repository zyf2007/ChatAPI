package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	httpapp "github.com/zyf2007/ChatAPI/internal/testutil/httpapp"
)

func TestProtocolListModels(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_models",
		Username: "models",
		Email:    "models@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	modelKeyService := modelkey.NewService(st, "test-master-key")
	_, rawKey, err := modelKeyService.CreateKey(context.Background(), "user_models", "model-a", "gpt-debug-a")
	if err != nil {
		t.Fatalf("create model key a: %v", err)
	}
	if _, _, err := modelKeyService.CreateKey(context.Background(), "user_models", "model-b", "gpt-debug-b"); err != nil {
		t.Fatalf("create model key b: %v", err)
	}
	if _, _, err := modelKeyService.CreateKey(context.Background(), "user_models", "model-a-dup", "gpt-debug-a"); err != nil {
		t.Fatalf("create duplicate model key: %v", err)
	}

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		ModelAPIKeys:   modelKeyService,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	getJSONWithHeaders(t, server.URL+"/v1/models", nil, http.StatusUnauthorized)
	for _, path := range []string{"/api/user/virtual-models", "/api/user/config", "/api/admin/settings/media", "/api/admin/settings/access"} {
		if status, body := getTextWithHeaders(t, server.URL+path, nil); status != http.StatusUnauthorized {
			t.Fatalf("Serve auth lost for %s: %d %s", path, status, body)
		}
	}
	resp := getJSONWithHeaders(t, server.URL+"/v1/models", map[string]string{
		"Authorization": "Bearer " + rawKey,
	}, http.StatusOK)
	if resp["object"] != "list" {
		t.Fatalf("unexpected models response: %#v", resp)
	}
	items := resp["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("unexpected models list length: %#v", resp)
	}
}
