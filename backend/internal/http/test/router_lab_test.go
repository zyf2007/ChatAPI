package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	httpapp "github.com/zyf2007/ChatAPI/internal/testutil/httpapp"
)

func TestRouterLabModeAccessAndEndpoints(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:   st,
			Pending: pending,
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Events:             noopEvents,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st, "test-master-key")

	cfg := config.Default(config.ModeLab, "/tmp/chatapi-test")
	cfg.MetricsEnabled = true
	cfg.MediaDerivedDir = t.TempDir()
	cfg.LabPassword = "secret"
	labService := labauth.NewService(cfg)

	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		Turn:           turnService,
		Query:          queryService,
		ModelAPIKeys:   modelKeyService,
		AppAPIKeys:     appKeyService,
		Lab:            labService,
		AuthSettings:   authsettings.NewService(st, cfg),
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace", nil)
	if err != nil {
		t.Fatalf("new lab workspace request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("lab workspace request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected lab unauthorized status: %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected html password page, got %q", contentType)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace?password=secret", nil)
	if err != nil {
		t.Fatalf("new lab bootstrap request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("lab bootstrap request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected lab bootstrap status: %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected lab access cookie")
	}

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/lab/workspace", nil)
	if err != nil {
		t.Fatalf("new lab workspace request: %v", err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("lab workspace with cookie: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected lab workspace status: %d", resp.StatusCode)
	}

	cookieHeader := cookies[0].Name + "=" + cookies[0].Value
	session := getJSONWithHeaders(t, server.URL+"/api/auth/session", map[string]string{"Cookie": cookieHeader}, http.StatusOK)
	if authenticated, ok := session["authenticated"].(bool); !ok || !authenticated {
		t.Fatalf("expected authenticated Lab session, got %#v", session)
	}
	if session["mode"] != "lab" {
		t.Fatalf("expected explicit Lab mode, got %#v", session)
	}

	// Every interactive endpoint remains protected by the same Lab gate.
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/session"},
		{http.MethodGet, "/v1/models"},
		{http.MethodGet, "/api/user/virtual-models"},
		{http.MethodPost, "/api/user/virtual-models"},
		{http.MethodDelete, "/api/user/virtual-models/unknown"},
		{http.MethodGet, "/api/user/config"},
		{http.MethodPost, "/api/user/config"},
		{http.MethodGet, "/api/admin/settings/media"},
		{http.MethodPatch, "/api/admin/settings/media"},
		{http.MethodGet, "/api/admin/settings/access"},
		{http.MethodPatch, "/api/admin/settings/access"},
		{http.MethodGet, "/api/admin/settings/overview"},
		{http.MethodGet, "/api/admin/settings/runtime"},
		{http.MethodGet, "/api/admin/settings/catalog"},
		{http.MethodGet, "/api/admin/monitor/stream"},
		{http.MethodGet, "/api/admin/audit/logs"},
		{http.MethodDelete, "/api/conversations/unknown"},
		{http.MethodPost, "/api/conversations/prune"},
		{http.MethodPost, "/api/conversations/unknown/output-images"},
		{http.MethodGet, "/api/media/assets/unknown"},
	} {
		req, _ := http.NewRequest(endpoint.method, server.URL+endpoint.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unguarded endpoint %s %s: %d", endpoint.method, endpoint.path, resp.StatusCode)
		}
	}

	headers := map[string]string{"Cookie": cookieHeader}

	assertStatus := func(method, path, body string, expected int) {
		t.Helper()
		request, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Cookie", cookieHeader)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != expected {
			t.Fatalf("%s %s: expected %d, got %d", method, path, expected, response.StatusCode)
		}
	}
	// Workspace models use Lab ownership without manufacturing a model API key.
	listed := getJSONWithHeaders(t, server.URL+"/v1/models", headers, http.StatusOK)
	if len(listed["data"].([]any)) != 0 {
		t.Fatalf("unexpected initial models: %#v", listed)
	}
	created := postJSONWithHeaders(t, server.URL+"/api/user/virtual-models", map[string]any{"name": "lab-browser-model"}, headers, http.StatusCreated)
	modelID := created["item"].(map[string]any)["id"].(string)
	persistedModels, err := st.ListVirtualModelsByUser(context.Background(), labauth.DefaultOwnerID)
	if err != nil || len(persistedModels) != 1 || persistedModels[0].ID != modelID {
		t.Fatalf("Lab model not persisted: %#v %v", persistedModels, err)
	}
	if _, err := st.CreateVirtualModel(context.Background(), common.CreateVirtualModelInput{ID: "foreign-model", UserID: "other-owner", Name: "private-model"}); err != nil {
		t.Fatal(err)
	}
	listed = getJSONWithHeaders(t, server.URL+"/v1/models", headers, http.StatusOK)
	if len(listed["data"].([]any)) != 1 || listed["data"].([]any)[0].(map[string]any)["id"] != "lab-browser-model" {
		t.Fatalf("catalog owner isolation failed: %#v", listed)
	}
	listed = getJSONWithHeaders(t, server.URL+"/api/user/virtual-models", headers, http.StatusOK)
	if len(listed["items"].([]any)) != 1 {
		t.Fatalf("model CRUD owner isolation failed: %#v", listed)
	}
	assertStatus(http.MethodDelete, "/api/user/virtual-models/foreign-model", "", http.StatusNotFound)
	deleteJSONWithCookie(t, server.URL+"/api/user/virtual-models/"+modelID, cookies[0], http.StatusOK)
	listed = getJSONWithHeaders(t, server.URL+"/v1/models", headers, http.StatusOK)
	if len(listed["data"].([]any)) != 0 {
		t.Fatalf("deleted model still discovered: %#v", listed)
	}

	// Notification settings belong to the workspace, while account configuration stays closed.
	postJSONWithHeaders(t, server.URL+"/api/user/config", map[string]any{"ntfy_url_enabled": false, "ntfy_url": ""}, headers, http.StatusOK)
	savedConfig, err := st.GetUserConfig(context.Background(), labauth.DefaultOwnerID, "settings")
	if err != nil || savedConfig.Value["ntfy_url_enabled"] != false {
		t.Fatalf("Lab config not persisted: %#v %v", savedConfig, err)
	}
	if _, err := st.SetUserConfig(context.Background(), common.SetUserConfigInput{UserID: "other-owner", Key: "settings", Value: map[string]any{"ntfy_url": "private-topic"}}); err != nil {
		t.Fatal(err)
	}
	listed = getJSONWithHeaders(t, server.URL+"/api/user/config", headers, http.StatusOK)
	if listed["ntfy_url"] != "" {
		t.Fatalf("foreign config leaked: %#v", listed)
	}
	assertStatus(http.MethodPost, "/api/user/config", `{"ntfy_url_enabled":true,"ntfy_url":"http://127.0.0.1/private"}`, http.StatusBadRequest)

	// The same media and access settings services drive the Lab UI and runtime consumers.
	media := patchJSONWithCookie(t, server.URL+"/api/admin/settings/media", map[string]any{"values": map[string]any{"max_bytes": 4096}}, cookies[0], http.StatusOK)
	if media["result"].(map[string]any)["document"].(map[string]any)["values"].(map[string]any)["max_bytes"] != float64(4096) {
		t.Fatalf("media setting not applied: %#v", media)
	}
	patchJSONWithCookie(t, server.URL+"/api/admin/settings/media", map[string]any{"values": map[string]any{"max_bytes": -1}}, cookies[0], http.StatusBadRequest)
	patchJSONWithCookie(t, server.URL+"/api/admin/settings/access", map[string]any{"values": map[string]any{"pending_turn_ttl": "1h"}}, cookies[0], http.StatusOK)
	patchJSONWithCookie(t, server.URL+"/api/admin/settings/access", map[string]any{"values": map[string]any{"max_connections_per_user_per_instance": 7, "user_conversation_limit": 5}}, cookies[0], http.StatusOK)
	session = getJSONWithHeaders(t, server.URL+"/api/auth/session", headers, http.StatusOK)
	if session["realtime_max_connections_per_user"] != float64(7) || session["user_conversation_limit"] != float64(5) {
		t.Fatalf("Lab session uses stale policy limits: %#v", session)
	}
	getJSONWithHeaders(t, server.URL+"/api/admin/settings/overview", headers, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/admin/settings/runtime", headers, http.StatusOK)
	catalog := getJSONWithHeaders(t, server.URL+"/api/admin/settings/catalog", headers, http.StatusOK)
	if len(catalog["catalog"].(map[string]any)["groups"].([]any)) != 2 {
		t.Fatalf("Lab settings exposed unsupported domains: %#v", catalog)
	}
	for _, path := range []string{"/api/user/model-keys", "/api/user/api-keys", "/api/admin/users", "/api/admin/settings/auth", "/api/admin/settings/automation", "/api/automation/rules"} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("Cookie", cookieHeader)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("unexpected account API %s: %d", path, response.StatusCode)
		}
	}
	ctx := context.Background()
	turn, conversation, _, err := turnService.Submitter.Submit(ctx, turnsvc.SubmitInput{
		OwnerID: labauth.DefaultOwnerID,
		Actor:   labService.Actor(),
		Request: protocol.TurnRequest{
			Protocol: protocol.ProtocolResponses, Model: "lab-test", LastUserContent: "draw",
			BuiltinTools: []protocol.BuiltinTool{{Kind: "image_generation", Type: "image_generation"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session = getJSONWithHeaders(t, server.URL+"/api/auth/session", headers, http.StatusOK)
	if session["current_conversation_count"] != float64(1) {
		t.Fatalf("Lab session conversation count is stale: %#v", session)
	}
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(tinyPNGBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	// A saved media limit must affect the next upload without a process restart.
	patchJSONWithCookie(t, server.URL+"/api/admin/settings/media", map[string]any{"values": map[string]any{"max_bytes": 8}}, cookies[0], http.StatusOK)
	limitedRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/conversations/"+conversation.ID+"/output-images", bytes.NewReader(upload.Bytes()))
	limitedRequest.Header.Set("Cookie", cookieHeader)
	limitedRequest.Header.Set("Content-Type", form.FormDataContentType())
	limitedResponse, err := client.Do(limitedRequest)
	if err != nil {
		t.Fatal(err)
	}
	limitedResponse.Body.Close()
	if limitedResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("saved media limit did not reject upload: %d", limitedResponse.StatusCode)
	}
	patchJSONWithCookie(t, server.URL+"/api/admin/settings/media", map[string]any{"values": map[string]any{"max_bytes": 4096}}, cookies[0], http.StatusOK)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/conversations/"+conversation.ID+"/output-images", &upload)
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		AssetID string `json:"asset_id"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		t.Fatalf("decode upload (status %d): %v", resp.StatusCode, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || asset.AssetID == "" {
		t.Fatalf("Lab upload failed: status=%d asset=%#v", resp.StatusCode, asset)
	}
	storedAsset, err := st.GetMediaAssetByID(ctx, asset.AssetID)
	if err != nil || storedAsset.OwnerID != labauth.DefaultOwnerID {
		t.Fatalf("incorrect uploaded asset owner: %#v, %v", storedAsset, err)
	}
	if _, err := os.Stat(storedAsset.Path); err != nil {
		t.Fatalf("uploaded file was not persisted: %v", err)
	}
	status, _ := getTextWithHeaders(t, server.URL+"/api/media/assets/"+storedAsset.FileID, headers)
	if status != http.StatusOK {
		t.Fatalf("Lab cannot read its image: %d", status)
	}

	// Lab must never expose or mutate another owner's resources.
	foreign, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "foreign", RequestID: "foreign_req", ResponseID: "foreign_resp",
		OwnerID: "another_owner", RequestFormat: "responses", Model: "lab-test", UserContent: "private",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateMediaAsset(ctx, common.CreateMediaAssetInput{
		ID: "foreign_asset", FileID: "foreign_file", OwnerID: "another_owner", Path: filepath.Join(t.TempDir(), "private.avif"),
		MediaType: storedAsset.MediaType, Bytes: storedAsset.Bytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _ = getTextWithHeaders(t, server.URL+"/api/media/assets/foreign_file", headers)
	if status != http.StatusNotFound {
		t.Fatalf("Lab exposed foreign image: %d", status)
	}
	for _, item := range []struct {
		id   string
		want int
	}{
		{conversation.ID, http.StatusConflict}, {foreign.ID, http.StatusNotFound},
	} {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/conversations/"+item.id, nil)
		req.Header.Set("Cookie", cookieHeader)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != item.want {
			t.Fatalf("delete %s: got %d, want %d", item.id, resp.StatusCode, item.want)
		}
	}
	postJSONWithHeaders(t, server.URL+"/lab/requests/"+turn.RequestID+"/complete", map[string]any{"text": "done", "mode": "assistant_message"}, headers, http.StatusOK)
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/conversations/"+conversation.ID, nil)
	req.Header.Set("Cookie", cookieHeader)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete closed Lab conversation: %d", resp.StatusCode)
	}
	if _, err := st.GetConversation(ctx, conversation.ID); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("conversation deletion was not persisted: %v", err)
	}
	for _, id := range []string{"lab_closed", "lab_waiting"} {
		_, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
			ConversationID: id, RequestID: id + "_req", ResponseID: id + "_resp",
			OwnerID: labauth.DefaultOwnerID, RequestFormat: "responses", Model: "lab-test", UserContent: id,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CompletePendingTurn(ctx, common.CompletePendingInput{
		ConversationID: "lab_closed", ResponseID: "lab_closed_resp", OutputText: "done", Mode: "assistant_message",
	}); err != nil {
		t.Fatal(err)
	}
	pruned := postJSONWithHeaders(t, server.URL+"/api/conversations/prune", map[string]any{"keep_count": 0}, headers, http.StatusOK)
	if pruned["deleted_count"] != float64(1) || pruned["skipped_count"] != float64(1) {
		t.Fatalf("unexpected Lab prune result: %#v", pruned)
	}
	if _, err := st.GetConversation(ctx, "lab_closed"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("prune was not persisted: %v", err)
	}
	for _, id := range []string{"lab_waiting", foreign.ID} {
		if _, err := st.GetConversation(ctx, id); err != nil {
			t.Fatalf("prune deleted protected conversation %s: %v", id, err)
		}
	}

	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/ready", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/setup/status", nil, http.StatusOK)
	status, body := getTextWithHeaders(t, server.URL+"/metrics", nil)
	if status != http.StatusOK || !strings.Contains(body, "chatapi_http_requests_total") {
		t.Fatalf("unexpected metrics response: status=%d body=%q", status, body)
	}
}

func TestRouterGlobalAccessRateLimit(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.MetricsEnabled = true
	cfg.AccessRateLimitRequests = 2
	cfg.AccessRateLimitWindow = time.Minute

	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)
	getJSONWithHeaders(t, server.URL+"/api/health", nil, http.StatusOK)

	getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	status, body := getTextWithHeaders(t, server.URL+"/api/auth/session", nil)
	if status != http.StatusTooManyRequests || !strings.Contains(body, "request rate limited") {
		t.Fatalf("unexpected rate limit response: status=%d body=%q", status, body)
	}
}

func TestRouterPrincipalAccessRateLimitForAppKey(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_a",
		Username: "alice",
		Email:    "alice@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	appKeyService := appkey.NewService(st, "test-master-key")
	_, rawAppKey, err := appKeyService.CreateKey(context.Background(), "user_a", "user-a-app", []string{"requests:read"}, nil, nil)
	if err != nil {
		t.Fatalf("create app key: %v", err)
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	accessSettings := authaccess.NewSettingsService(st, authaccess.Settings{})
	accessDomain := accessSettings.AdminDomain()
	if _, _, err := accessDomain.Patch(context.Background(), map[string]any{
		"app_key_rate_limit_requests": 1,
		"app_key_rate_limit_window":   "1m",
	}); err != nil {
		t.Fatalf("set access settings: %v", err)
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
		Query:          queryService,
		AppAPIKeys:     appKeyService,
		Policy:         policy.NewService(),
		AccessSettings: accessSettings,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	getJSONWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": rawAppKey,
	}, http.StatusOK)

	status, body := getTextWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": rawAppKey,
	})
	if status != http.StatusTooManyRequests || !strings.Contains(body, "principal rate limited") || !strings.Contains(body, "principal_rate_limited") {
		t.Fatalf("unexpected principal rate limit response: status=%d body=%q", status, body)
	}
}
