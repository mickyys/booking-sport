package auth0mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAuth0 struct {
	tokenCalls  atomic.Int32
	findCalls   atomic.Int32
	createCalls atomic.Int32
	ticketCalls atomic.Int32
	roleCalls   atomic.Int32

	usersByEmail   func(w http.ResponseWriter, r *http.Request)
	createOverride func(w http.ResponseWriter, r *http.Request)
	ticketOverride func(w http.ResponseWriter, r *http.Request)
	assignRole     func(w http.ResponseWriter, r *http.Request)

	mu      sync.Mutex
	creates []map[string]interface{}
	tickets []map[string]interface{}
	roles   []map[string]interface{}
}

func newTestClient(t *testing.T, f *fakeAuth0) *ManagementClient {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			f.tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "test-token", "expires_in": 3600})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users-by-email":
			f.findCalls.Add(1)
			if f.usersByEmail != nil {
				f.usersByEmail(w, r)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/users":
			f.createCalls.Add(1)
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.creates = append(f.creates, body)
			f.mu.Unlock()
			if f.createOverride != nil {
				f.createOverride(w, r)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"user_id":        "auth0|new",
				"email":          body["email"],
				"email_verified": true,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/tickets/password-change":
			f.ticketCalls.Add(1)
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.tickets = append(f.tickets, body)
			f.mu.Unlock()
			if f.ticketOverride != nil {
				f.ticketOverride(w, r)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"ticket": "https://test.auth0.local/changepw"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/roles"):
			f.roleCalls.Add(1)
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.roles = append(f.roles, body)
			f.mu.Unlock()
			if f.assignRole != nil {
				f.assignRole(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("ruta inesperada: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return &ManagementClient{
		domain:       "test.auth0.local",
		clientID:     "cid",
		clientSecret: "csecret",
		connection:   DefaultConnection,
		http:         ts.Client(),
		baseURL:      ts.URL,
	}
}

func TestFindByEmailFound(t *testing.T) {
	f := &fakeAuth0{}
	c := newTestClient(t, f)
	f.usersByEmail = func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("email"); got != "a@b.cl" {
			t.Errorf("email en query = %q, want a@b.cl", got)
		}
		_, _ = w.Write([]byte(`[{"user_id":"auth0|123","email":"a@b.cl","email_verified":true}]`))
	}

	u, err := c.FindByEmail(context.Background(), "a@b.cl")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if u == nil || u.ID != "auth0|123" || !u.EmailVerified || u.Email != "a@b.cl" {
		t.Fatalf("usuario inesperado: %+v", u)
	}
	if f.findCalls.Load() != 1 || f.tokenCalls.Load() != 1 {
		t.Fatalf("find=%d token=%d", f.findCalls.Load(), f.tokenCalls.Load())
	}
}

func TestFindByEmailNotFound(t *testing.T) {
	c := newTestClient(t, &fakeAuth0{})
	u, err := c.FindByEmail(context.Background(), "nadie@club.cl")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if u != nil {
		t.Fatalf("want nil, got %+v", u)
	}
}

func TestCreateAccountEnviaCredencialesCorrectas(t *testing.T) {
	f := &fakeAuth0{}
	c := newTestClient(t, f)

	u, err := c.CreateAccount(context.Background(), "nuevo@club.cl", "ClaveSegura123")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if f.createCalls.Load() != 1 {
		t.Fatalf("create HTTP calls = %d, want 1", f.createCalls.Load())
	}
	if f.ticketCalls.Load() != 0 {
		t.Errorf("ticket HTTP calls = %d, want 0 (no debe enviar correos de Auth0)", f.ticketCalls.Load())
	}
	if u == nil || u.ID != "auth0|new" {
		t.Fatalf("usuario retornado = %+v, want ID auth0|new", u)
	}

	created := f.payloadAt(f.creates, 0)
	if created["email"] != "nuevo@club.cl" {
		t.Errorf("email = %v, want nuevo@club.cl", created["email"])
	}
	if created["connection"] != DefaultConnection {
		t.Errorf("connection = %v, want %q", created["connection"], DefaultConnection)
	}
	if created["password"] != "ClaveSegura123" {
		t.Errorf("password = %v, want la clave entregada por el usuario", created["password"])
	}
	if created["email_verified"] != true {
		t.Errorf("email_verified = %v, want true", created["email_verified"])
	}
}

func TestCreateAccountYaExisteRetornaUsuarioExistente(t *testing.T) {
	f := &fakeAuth0{}
	f.createOverride = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"statusCode":409,"error":"FBP0002","message":"The user already exists."}`))
	}
	f.usersByEmail = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"user_id":"auth0|existente","email":"x@y.cl","email_verified":true}]`))
	}
	c := newTestClient(t, f)

	u, err := c.CreateAccount(context.Background(), "x@y.cl", "ClaveSegura123")
	if err != nil {
		t.Fatalf("CreateAccount con usuario existente: %v", err)
	}
	if u == nil || u.ID != "auth0|existente" {
		t.Errorf("usuario = %+v, want auth0|existente (localizado vía FindByEmail)", u)
	}
	if f.findCalls.Load() < 1 {
		t.Errorf("find HTTP calls = %d, want >=1 (localizar usuario existente)", f.findCalls.Load())
	}
}

func TestCreateAccountFailsOnNon2xx(t *testing.T) {
	f := &fakeAuth0{}
	f.createOverride = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_body"}`))
	}
	c := newTestClient(t, f)

	u, err := c.CreateAccount(context.Background(), "x@y.cl", "ClaveSegura123")
	if err == nil {
		t.Fatal("esperaba error para respuesta no-2xx de creación")
	} else if !strings.Contains(err.Error(), "/users") {
		t.Fatalf("error sin contexto del endpoint: %v", err)
	}
	if u != nil {
		t.Errorf("usuario = %+v, want nil cuando falla la creación", u)
	}
}

func TestCreatePasswordSetupTicket(t *testing.T) {
	t.Setenv("URL_FRONTEND", "https://app.reservaloya.cl")
	f := &fakeAuth0{}
	c := newTestClient(t, f)

	url, err := c.CreatePasswordSetupTicket(context.Background(), "auth0|nuevo")
	if err != nil {
		t.Fatalf("CreatePasswordSetupTicket: %v", err)
	}
	if url != "https://test.auth0.local/changepw" {
		t.Errorf("ticket URL = %q, want https://test.auth0.local/changepw", url)
	}

	ticket := f.payloadAt(f.tickets, 0)
	if ticket == nil {
		t.Fatal("no se registró payload del ticket")
	}
	if ticket["user_id"] != "auth0|nuevo" {
		t.Errorf("user_id = %v, want auth0|nuevo", ticket["user_id"])
	}
	if ticket["send_email"] != false {
		t.Errorf("send_email = %v, want false (Auth0 no debe enviar correo)", ticket["send_email"])
	}
	if ticket["result_url"] != "https://app.reservaloya.cl/admin" {
		t.Errorf("result_url = %v, want https://app.reservaloya.cl/admin", ticket["result_url"])
	}
}

func TestAssignRole(t *testing.T) {
	t.Run("rol vacío es no-op sin HTTP", func(t *testing.T) {
		f := &fakeAuth0{}
		c := newTestClient(t, f)
		if err := c.AssignRole(context.Background(), "auth0|1", ""); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
		if f.roleCalls.Load() != 0 {
			t.Fatalf("llamadas HTTP inesperadas: %d", f.roleCalls.Load())
		}
	})

	t.Run("payload usa atributo roles", func(t *testing.T) {
		f := &fakeAuth0{}
		c := newTestClient(t, f)
		if err := c.AssignRole(context.Background(), "auth0|1", "rol_1"); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
		body := f.payloadAt(f.roles, 0)
		roles, ok := body["roles"].([]interface{})
		if !ok || len(roles) != 1 || roles[0] != "rol_1" {
			t.Errorf("payload = %v, want {\"roles\": [\"rol_1\"]}", body)
		}
		if _, exists := body["role_ids"]; exists {
			t.Errorf("payload no debe usar role_ids (Auth0 lo rechaza): %v", body)
		}
	})

	t.Run("204 ok y ya-asignado son no-op", func(t *testing.T) {
		f := &fakeAuth0{}
		c := newTestClient(t, f)
		if err := c.AssignRole(context.Background(), "auth0|1", "rol_1"); err != nil {
			t.Fatalf("AssignRole 204: %v", err)
		}
		f.assignRole = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"description":"The user already has the role specified","error":"invalid_body"}`))
		}
		if err := c.AssignRole(context.Background(), "auth0|1", "rol_1"); err != nil {
			t.Fatalf("AssignRole ya-asignado: %v", err)
		}
	})

	t.Run("400 por otra razón sí es error", func(t *testing.T) {
		f := &fakeAuth0{}
		f.assignRole = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"description":"role not found"}`))
		}
		c := newTestClient(t, f)
		if err := c.AssignRole(context.Background(), "auth0|1", "mal"); err == nil {
			t.Fatal("esperaba error para 400 no relacionado con rol duplicado")
		}
	})
}

func TestTokenEndpointError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer srv.Close()

	c := &ManagementClient{
		domain:       "test.auth0.local",
		clientID:     "cid",
		clientSecret: "csecret",
		connection:   DefaultConnection,
		http:         srv.Client(),
		baseURL:      srv.URL,
	}

	if _, err := c.FindByEmail(context.Background(), "a@b.cl"); err == nil || !strings.Contains(err.Error(), "auth0:") {
		t.Fatalf("esperaba error auth0:, got %v", err)
	}
}

func TestTokenCachedBetweenRequests(t *testing.T) {
	f := &fakeAuth0{}
	c := newTestClient(t, f)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := c.FindByEmail(ctx, "a@b.cl"); err != nil {
			t.Fatalf("FindByEmail %d: %v", i, err)
		}
	}
	if got := f.tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d, want 1 (cache)", got)
	}

	c.mu.Lock()
	c.tokenExpiry = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if _, err := c.FindByEmail(ctx, "a@b.cl"); err != nil {
		t.Fatalf("FindByEmail tras expirar: %v", err)
	}
	if got := f.tokenCalls.Load(); got != 2 {
		t.Fatalf("token calls tras expiración = %d, want 2", got)
	}
}

func TestConcurrentRenewalFetchesOnce(t *testing.T) {
	f := &fakeAuth0{}
	c := newTestClient(t, f)
	ctx := context.Background()

	c.mu.Lock()
	c.accessToken = ""
	c.tokenExpiry = time.Time{}
	c.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.FindByEmail(ctx, "a@b.cl"); err != nil {
				t.Errorf("FindByEmail concurrente: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := f.tokenCalls.Load(); got != 1 {
		t.Fatalf("renovaciones concurrentes = %d, want 1", got)
	}
}

func (f *fakeAuth0) payloadAt(list []map[string]interface{}, i int) map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(list) {
		return nil
	}
	return list[i]
}
