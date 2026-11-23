package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/guardian"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// Defaults for the event stream.
const (
	// defaultKeepAlive is how often an open stream gets a comment line, so
	// proxies do not close an idle connection (Requirement 8.4).
	defaultKeepAlive = 15 * time.Second
	// defaultLambdaStreamLimit is how long a stream stays open on Lambda
	// (Requirement 8.6). The response is buffered there, so the client gets the
	// first event when the stream closes and then polls.
	defaultLambdaStreamLimit = 25 * time.Second
)

// Deps holds the parts of the service that a test replaces. A zero field takes
// its production default.
type Deps struct {
	// Store holds the vaults. The default is a MemoryStore.
	Store store.VaultStore
	// Clock tells the time to the vault service and the guardian service. The
	// default is time.Now. A fake clock must be the one the Store reads too.
	Clock vault.Clock
	// Logger receives the one line per request. The default writes to stderr.
	Logger *log.Logger
	// KeepAlive is the interval of the stream's keep-alive comments.
	KeepAlive time.Duration
	// LambdaStreamLimit is how long a stream stays open when OnLambda is set.
	LambdaStreamLimit time.Duration
}

// server holds what the handlers share.
type server struct {
	cfg       Config
	vault     *vault.Service
	guardian  *guardian.Service
	hub       *guardian.Hub
	keepAlive time.Duration
	lambdaMax time.Duration
}

// Build creates the store the configuration names, wires the services and
// returns the handler that both entrypoints serve (Requirements 10.2, 10.3,
// 10.5). A "memory" driver builds a MemoryStore. Any other value goes to
// store.NewFromEnv, which reads STORAGE_DRIVER, DYNAMODB_TABLE_NAME and
// DYNAMODB_ENDPOINT, and rejects a driver it does not know.
func Build(ctx context.Context, cfg Config) (http.Handler, error) {
	var vs store.VaultStore
	if cfg.StorageDriver == "memory" {
		vs = store.NewMemoryStore()
	} else {
		var err error
		if vs, err = store.NewFromEnv(ctx); err != nil {
			return nil, err
		}
	}
	return New(cfg, Deps{Store: vs}), nil
}

// New wires the services over deps and returns the handler: the router wrapped in
// the body limit, CORS and logging, outermost last.
func New(cfg Config, deps Deps) http.Handler {
	h, _ := assemble(cfg, deps)
	return h
}

// assemble is New, and it also returns the server so that a test can look at the
// hub.
func assemble(cfg Config, deps Deps) (http.Handler, *server) {
	if deps.Store == nil {
		deps.Store = store.NewMemoryStore()
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	if deps.KeepAlive <= 0 {
		deps.KeepAlive = defaultKeepAlive
	}
	if deps.LambdaStreamLimit <= 0 {
		deps.LambdaStreamLimit = defaultLambdaStreamLimit
	}

	signer := guardian.NewSigner(cfg.HMACKey)
	hub := guardian.NewHub()
	vs := vault.New(deps.Store, deps.Clock, cfg.DemoMode, signer, hub)
	s := &server{
		cfg:       cfg,
		vault:     vs,
		guardian:  guardian.NewService(vs, signer, deps.Clock),
		hub:       hub,
		keepAlive: deps.KeepAlive,
		lambdaMax: deps.LambdaStreamLimit,
	}

	var d dispatcher
	d.handle(http.MethodPost, "/api/v1/transactions/evaluate", s.evaluate)
	d.handle(http.MethodPost, "/api/v1/vault/{id}/reality-check", s.realityCheck)
	d.handle(http.MethodPost, "/api/v1/vault/{id}/guardian-action", s.guardianAction)
	d.handle(http.MethodPost, "/api/v1/vault/{id}/demo-fast-forward", s.fastForward)
	d.handle(http.MethodGet, "/api/v1/vault/{id}", s.getVault)
	d.handle(http.MethodGet, "/api/v1/events/stream", s.stream)

	var h http.Handler = &d
	h = bodyLimit(h)
	h = cors(h, cfg.AllowedOrigin)
	return logging(h, deps.Logger), s
}

// decodeJSON reads the whole request body and decodes one JSON value from it into
// dst. Fields that dst does not declare are dropped, so a request cannot carry an
// OTP, PIN or password field into the service (Requirement 11.3). It returns an
// *http.MaxBytesError for a body over the limit, and errBadJSON for an empty
// body, a syntax error, a wrong type or anything after the value.
func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return err
		}
		return errBadJSON
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(dst); err != nil {
		return errBadJSON
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errBadJSON
	}
	return nil
}

// evaluate scores a payment and creates its vault (Requirement 1).
func (s *server) evaluate(w http.ResponseWriter, r *http.Request) {
	var req vault.EvaluateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	view, err := s.vault.Evaluate(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	infoFrom(r).setVaultID(view.ID)
	writeJSON(w, http.StatusOK, view)
}

// realityCheckRequest is the body of the reality-check route. Both fields are
// pointers, so a field that is absent can be told from one that is present.
type realityCheckRequest struct {
	Answers   *vault.Answers `json:"answers"`
	Reconfirm *bool          `json:"reconfirm"`
}

// realityCheck takes either the three answers or a reconfirmation. A body with
// both, with neither, or with reconfirm false is a 400 (Requirements 3.1, 3.7,
// 4.5).
func (s *server) realityCheck(w http.ResponseWriter, r *http.Request) {
	var req realityCheckRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}

	var (
		view vault.View
		err  error
	)
	switch {
	case req.Answers != nil && req.Reconfirm == nil:
		view, err = s.vault.RealityCheck(r.Context(), idFrom(r), *req.Answers)
	case req.Answers == nil && req.Reconfirm != nil && *req.Reconfirm:
		view, err = s.vault.Reconfirm(r.Context(), idFrom(r))
	default:
		err = errBadJSON
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// guardianAction carries out an approve or a veto (Requirement 6). The service
// checks the token before it reads the action.
func (s *server) guardianAction(w http.ResponseWriter, r *http.Request) {
	var action guardian.Action
	if err := decodeJSON(r, &action); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.guardian.Act(r.Context(), idFrom(r), action)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// getVault returns the current View of a vault (Requirement 7).
func (s *server) getVault(w http.ResponseWriter, r *http.Request) {
	view, err := s.vault.Get(r.Context(), idFrom(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// fastForward shortens the lock for the demo. With Demo_Mode off it answers 404
// before it reads the vault (Requirement 9.2).
func (s *server) fastForward(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DemoMode {
		writeCode(w, http.StatusNotFound, codeNotFound, msgNotFound)
		return
	}
	view, err := s.vault.FastForward(r.Context(), idFrom(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// stream opens a Server-Sent Events stream for one vault (Requirement 8).
//
// A missing vault_id is a 400 and an unknown vault a 404, and neither opens a
// stream. Otherwise the current View is the first event, then every change the
// hub publishes, with a keep-alive comment between them. On Lambda the stream
// closes after a fixed time, because the response is buffered there and the
// client falls back to polling. Events carry no guardian token: the panel holds
// the token from its read of the vault.
func (s *server) stream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("vault_id")
	if id == "" {
		writeCode(w, http.StatusBadRequest, codeInvalidRequest, msgMissingVaultID)
		return
	}
	if _, err := s.vault.Get(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	infoFrom(r).setVaultID(id)

	// Subscribe before the first read, so a change between the two is not lost.
	events, cancel := s.hub.Subscribe(id)
	defer cancel()
	current, err := s.vault.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	current.GuardianToken = ""
	first, err := json.Marshal(current)
	if err != nil {
		return
	}
	if !writeEvent(w, rc, "data: ", first) {
		return
	}

	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	var limit <-chan time.Time
	if s.cfg.OnLambda {
		timer := time.NewTimer(s.lambdaMax)
		defer timer.Stop()
		limit = timer.C
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-limit:
			return
		case event, ok := <-events:
			if !ok || !writeEvent(w, rc, "data: ", event) {
				return
			}
		case <-keepAlive.C:
			if !writeEvent(w, rc, ": keep-alive", nil) {
				return
			}
		}
	}
}

// writeEvent writes one event or comment line, ends it with a blank line, and
// flushes. It reports false when the connection is gone. A writer that cannot
// flush still gets the bytes, which is what happens behind the Lambda adapter.
func writeEvent(w http.ResponseWriter, rc *http.ResponseController, prefix string, data []byte) bool {
	if _, err := fmt.Fprintf(w, "%s%s\n\n", prefix, data); err != nil {
		return false
	}
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	return true
}
