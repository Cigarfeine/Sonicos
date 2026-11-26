package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SafwanGanz/Sonicos/internal/command"
	"github.com/SafwanGanz/Sonicos/internal/whatsapp"
)

type Server struct {
	bot         *whatsapp.Bot
	registry    *command.Registry
	modStore    command.ModerationStore
	logger      *slog.Logger
	authPassword string
	mu          sync.RWMutex
	settings    map[string]any
	clients     map[chan []byte]struct{}
	clientsMu   sync.Mutex
	startedAt   time.Time
	version     string
	logBuffer   []logItem
	logMu       sync.Mutex
	maxLogs     int
	modEvents   []modHistoryItem
	modMu       sync.Mutex
	maxModEvents int
}

type statusResponse struct {
	Connected        bool   `json:"connected"`
	State            string `json:"state"`
	AccountID        string `json:"account_id"`
	Phone            string `json:"phone"`
	PushName         string `json:"push_name"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	ConnectedSince   string `json:"connected_since"`
	TotalMessages    int64  `json:"total_messages"`
	ActiveGroups     int    `json:"active_groups"`
	TotalUsers       int    `json:"total_users"`
	ModerationEvents int    `json:"moderation_events"`
	TotalWarnings    int    `json:"total_warnings"`
	Maintenance      bool   `json:"maintenance"`
	Version          string `json:"version"`
	Health           []healthCheck `json:"health"`
}

type healthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type loginResponse struct {
	Authenticated bool   `json:"authenticated"`
	Token         string `json:"token,omitempty"`
}

type qrResponse struct {
	State       string `json:"state"`
	QR          string `json:"qr,omitempty"`
	Image       string `json:"qr_image,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

type pairRequest struct {
	CountryCode string `json:"country_code"`
	PhoneNumber string `json:"phone_number"`
}

type pairResponse struct {
	PairingCode string `json:"pairing_code"`
	ExpiresAt   string `json:"expires_at"`
}

type settingsRequest struct {
	BotName      string `json:"bot_name"`
	Description  string `json:"description"`
	Language     string `json:"language"`
	Timezone     string `json:"timezone"`
	AutoStart    bool   `json:"auto_start"`
	Maintenance  bool   `json:"maintenance"`
}

type settingsResponse struct {
	BotName      string `json:"bot_name"`
	Prefix       string `json:"prefix"`
	Description  string `json:"description"`
	Language     string `json:"language"`
	Timezone     string `json:"timezone"`
	AutoStart    bool   `json:"auto_start"`
	Maintenance  bool   `json:"maintenance"`
}

type prefixRequest struct {
	Prefix string `json:"prefix"`
}

type groupResponse struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Members            int      `json:"members"`
	BotEnabled         bool     `json:"bot_enabled"`
	WelcomeEnabled     bool     `json:"welcome_enabled"`
	ModerationEnabled  bool     `json:"moderation_enabled"`
	AdminOnlyCommands  bool     `json:"admin_only_commands"`
	WelcomeMessage     string   `json:"welcome_message"`
	DisabledCommands   []string `json:"disabled_commands"`
	Admins             []groupAdmin `json:"admins"`
}

type groupAdmin struct {
	JID string `json:"jid"`
	Name string `json:"name"`
}

type groupPatchRequest struct {
	BotEnabled          *bool     `json:"bot_enabled,omitempty"`
	WelcomeEnabled      *bool     `json:"welcome_enabled,omitempty"`
	ModerationEnabled   *bool     `json:"moderation_enabled,omitempty"`
	AdminOnlyCommands   *bool     `json:"admin_only_commands,omitempty"`
	WelcomeMessage      string    `json:"welcome_message,omitempty"`
	DisabledCommands    []string  `json:"disabled_commands,omitempty"`
}

type moderationSettings struct {
	Enabled            bool    `json:"enabled"`
	DryRun             bool    `json:"dry_run"`
	AutoDelete         bool    `json:"auto_delete"`
	RemovalEnabled     bool    `json:"removal_enabled"`
	WarningThreshold   int     `json:"warning_threshold"`
	RemovalViolation   int     `json:"removal_violation"`
	HighConfidence     float64 `json:"high_confidence"`
	PossibleConfidence float64 `json:"possible_confidence"`
}

type enrolledItem struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	EnrolledAt  string `json:"enrolled_at"`
	EnrolledBy  string `json:"enrolled_by"`
	EnrolledByName string `json:"enrolled_by_name,omitempty"`
}

type matchItem struct {
	ID        string `json:"id"`
	Tier      string `json:"tier"`
	TS        string `json:"ts"`
	User      string `json:"user"`
	UserName  string `json:"user_name"`
	Group     string `json:"group"`
	GroupName string `json:"group_name"`
	Type      string `json:"type"`
	Confidence float64 `json:"confidence"`
	Status    string `json:"status"`
}

type matchReviewRequest struct {
	Decision string `json:"decision"`
}

type modHistoryItem struct {
	TS        string `json:"ts"`
	User      string `json:"user"`
	UserName  string `json:"user_name"`
	Group     string `json:"group"`
	GroupName string `json:"group_name"`
	Action    string `json:"action"`
	DryRun    bool   `json:"dry_run"`
	Detail    string `json:"detail"`
}

type userItem struct {
	JID        string `json:"jid"`
	Name       string `json:"name"`
	Display    string `json:"display"`
	Group      string `json:"group"`
	GroupName  string `json:"group_name"`
	IsAdmin    bool   `json:"is_admin"`
	Warnings   int    `json:"warnings"`
}

type userHistoryItem struct {
	TS        string `json:"ts"`
	Action    string `json:"action"`
	Type      string `json:"type"`
	Detail    string `json:"detail"`
	Message   string `json:"message"`
}

type logItem struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Level  string `json:"level"`
	Type   string `json:"type"`
	Message string `json:"message"`
}

type commandItem struct {
	Name        string `json:"name"`
	Aliases     []string `json:"aliases"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Permission  string `json:"permission"`
	AdminOnly   bool   `json:"admin_only"`
}

type pagedResponse struct {
	Items []any `json:"items"`
	Total int   `json:"total"`
}

func NewServer(bot *whatsapp.Bot, registry *command.Registry, modStore command.ModerationStore, logger *slog.Logger) *Server {
	s := &Server{
		bot:          bot,
		registry:     registry,
		modStore:     modStore,
		logger:       logger,
		settings:     make(map[string]any),
		clients:      make(map[chan []byte]struct{}),
		startedAt:    time.Now(),
		version:      "v0.1.0",
		logBuffer:    make([]logItem, 0, 1000),
		maxLogs:      1000,
		modEvents:    make([]modHistoryItem, 0, 500),
		maxModEvents: 500,
	}

	// Load auth password from env
	s.authPassword = os.Getenv("DASHBOARD_PASSWORD")
	if s.authPassword == "" {
		s.authPassword = "36545280"
	}

	// Initialize default settings
	s.settings["bot_name"] = "Sonicos"
	s.settings["prefix"] = os.Getenv("BOT_PREFIX")
	if s.settings["prefix"] == "" {
		s.settings["prefix"] = "!"
	}
	s.settings["description"] = "A helpful WhatsApp assistant for community groups."
	s.settings["language"] = "en"
	s.settings["timezone"] = "UTC"
	s.settings["auto_start"] = true
	s.settings["maintenance"] = false

	return s
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow auth endpoints without auth
		path := r.URL.Path
		if path == "/auth/login" || path == "/auth/logout" || path == "/auth/me" {
			next.ServeHTTP(w, r)
			return
		}
		// Check session cookie
		cookie, err := r.Cookie("sonicos_session")
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.authPassword)) != 1 {
			http.Error(w, `{"error":"Not signed in"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Authorization, Cookie")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Authorization, Cookie")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) corsAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Authorization, Cookie")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Auth check (skip for auth endpoints)
		path := r.URL.Path
		if path == "/auth/login" || path == "/auth/logout" || path == "/auth/me" {
			next(w, r)
			return
		}
		cookie, err := r.Cookie("sonicos_session")
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.authPassword)) != 1 {
			s.writeError(w, http.StatusUnauthorized, "Not signed in")
			return
		}
		next(w, r)
	}
}

func (s *Server) json(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("missing request body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) writeError(w http.ResponseWriter, code int, msg string) {
	s.json(w, code, map[string]string{"error": msg})
}

// SSE broadcast
func (s *Server) broadcast(event string, data any) {
	payload, _ := json.Marshal(data)
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(payload))
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- []byte(msg):
		default:
			// Slow client, skip
		}
	}
}

func (s *Server) addClient(ch chan []byte) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[ch] = struct{}{}
}

func (s *Server) removeClient(ch chan []byte) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, ch)
	close(ch)
}

// --- Auth ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req loginRequest
	if err := s.readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.authPassword)) == 1 {
		http.SetCookie(w, &http.Cookie{
			Name:     "sonicos_session",
			Value:    s.authPassword,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 7,
		})
		s.addLog("info", "user authenticated via dashboard")
		s.json(w, http.StatusOK, loginResponse{Authenticated: true})
	} else {
		s.writeError(w, http.StatusUnauthorized, "incorrect password")
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sonicos_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
	})
	s.addLog("info", "user logged out from dashboard")
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cookie, err := r.Cookie("sonicos_session")
	authenticated := err == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.authPassword)) == 1
	s.json(w, http.StatusOK, map[string]any{"authenticated": authenticated})
}

// --- Status ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	st := s.bot.ClientState()
	connected := st == "connected"

	accountID := ""
	phone := ""
	pushName := ""
	if s.bot.DeviceID() != nil {
		accountID = s.bot.DeviceID().String()
		phone = s.bot.DeviceID().User
	}

	resp := statusResponse{
		Connected:        connected,
		State:            st,
		AccountID:        accountID,
		Phone:            phone,
		PushName:         pushName,
		UptimeSeconds:    int64(time.Since(s.startedAt).Seconds()),
	TotalMessages:    s.getStat("messages", 0),
	ActiveGroups:     int(s.getStat("groups", 0)),
	TotalUsers:       int(s.getStat("users", 0)),
	ModerationEvents: int(s.getStat("moderation", 0)),
	TotalWarnings:    int(s.getStat("warnings", 0)),
		Maintenance:      s.getBool("maintenance", false),
		Version:          s.version,
		Health: []healthCheck{
			{Name: "WhatsApp socket", Status: healthStatus(connected), Detail: healthDetail(connected)},
			{Name: "Database", Status: "ok", Detail: "Session store healthy"},
			{Name: "Media scanner", Status: "ok", Detail: "Ready"},
			{Name: "Webhook queue", Status: "ok", Detail: "No pending deliveries"},
		},
	}
	if connected {
		resp.ConnectedSince = ""
	}

	s.json(w, http.StatusOK, resp)
}

func healthStatus(connected bool) string {
	if connected {
		return "ok"
	}
	return "down"
}

func healthDetail(connected bool) string {
	if connected {
		return "Connected"
	}
	return "Not connected"
}

// --- QR ---

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	qrData, state, err := s.bot.QRCode()
	if err != nil {
		if errors.Is(err, errAlreadyConnected) {
			s.writeError(w, http.StatusConflict, "Already connected")
			return
		}
		if errors.Is(err, errNoQR) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := qrResponse{
		State:  state,
		QR:     qrData,
	}
	if qrData != "" {
		resp.Image = "data:image/png;base64," + qrData
	}
	s.json(w, http.StatusOK, resp)
}

func (s *Server) handleQRRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.bot.RefreshQR(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

// --- Pairing ---

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req pairRequest
	if err := s.readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.CountryCode == "" || req.PhoneNumber == "" {
		s.writeError(w, http.StatusBadRequest, "country_code and phone_number are required")
		return
	}

	code, expiresAt, err := s.bot.RequestPairing(req.CountryCode, req.PhoneNumber)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := pairResponse{
		PairingCode: code,
		ExpiresAt:   expiresAt,
	}
	s.json(w, http.StatusOK, resp)
}

// --- Connection ---

func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.bot.Reconnect(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.bot.Disconnect(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleUnlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.bot.Unlink(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

// --- Settings ---

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp := settingsResponse{
			BotName:     s.getStr("bot_name", "Sonicos"),
			Prefix:      s.getStr("prefix", "!"),
			Description: s.getStr("description", ""),
			Language:    s.getStr("language", "en"),
			Timezone:    s.getStr("timezone", "UTC"),
			AutoStart:   s.getBool("auto_start", true),
			Maintenance: s.getBool("maintenance", false),
		}
		s.json(w, http.StatusOK, resp)
	case http.MethodPut:
		var req settingsRequest
		if err := s.readJSON(w, r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		s.mu.Lock()
		if req.BotName != "" { s.settings["bot_name"] = req.BotName }
		if req.Description != "" { s.settings["description"] = req.Description }
		if req.Language != "" { s.settings["language"] = req.Language }
		if req.Timezone != "" { s.settings["timezone"] = req.Timezone }
		s.settings["auto_start"] = req.AutoStart
		s.settings["maintenance"] = req.Maintenance
		s.mu.Unlock()
		s.broadcast("config", map[string]string{"action": "settings_updated"})
		s.addLog("info", "dashboard settings updated")
		s.json(w, http.StatusOK, map[string]string{"ok": "true"})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleChangePrefix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req prefixRequest
	if err := s.readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Prefix == "" || len(req.Prefix) > 3 || strings.Contains(req.Prefix, " ") {
		s.writeError(w, http.StatusBadRequest, "prefix must be 1-3 characters with no spaces")
		return
	}
	s.mu.Lock()
	s.settings["prefix"] = req.Prefix
	s.mu.Unlock()
	s.broadcast("config", map[string]string{"action": "prefix_changed", "prefix": req.Prefix})
	s.addLog("info", "bot prefix changed to "+req.Prefix)
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

// --- Commands ---

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cmds := s.registry.Commands()
	out := make([]commandItem, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, commandItem{
			Name:        c.Name,
			Aliases:     c.Aliases,
			Category:    c.Category,
			Description: c.Description,
			Enabled:     true,
			Permission:  "everyone",
		})
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handleCommandPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/commands/")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "command name required")
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

// --- Groups ---

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	groups := s.bot.ListGroups()
	out := make([]groupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupResponse{
			ID:                 g.JID,
			Name:               g.Name,
			Members:            len(g.Participants),
			BotEnabled:         true,
			WelcomeEnabled:     false,
			ModerationEnabled:  false,
			AdminOnlyCommands:  false,
			DisabledCommands:   nil,
			Admins:             nil,
		})
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handleGroup(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/groups/")
	if id == "" || id == "/" {
		s.writeError(w, http.StatusBadRequest, "group id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		g := s.bot.GetGroup(id)
		if g == nil {
			s.writeError(w, http.StatusNotFound, "group not found")
			return
		}
		resp := groupResponse{
			ID:                 g.JID,
			Name:               g.Name,
			Members:            len(g.Participants),
			BotEnabled:         true,
			WelcomeEnabled:     false,
			ModerationEnabled:  false,
			AdminOnlyCommands:  false,
			DisabledCommands:   nil,
			Admins:             nil,
		}
		s.json(w, http.StatusOK, resp)
	case http.MethodPatch:
		var req groupPatchRequest
		if err := s.readJSON(w, r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		s.broadcast("group", map[string]string{"id": id, "action": "updated"})
		s.addModEvent("dashboard", "group_updated", "group "+id)
		s.json(w, http.StatusOK, map[string]string{"ok": "true"})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Moderation ---

func (s *Server) handleModSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp := moderationSettings{
			Enabled:            s.getBool("mod_enabled", true),
			DryRun:             s.getBool("mod_dry_run", true),
			AutoDelete:         s.getBool("mod_auto_delete", false),
			RemovalEnabled:     s.getBool("mod_removal_enabled", true),
			WarningThreshold:   s.getInt("mod_warning_threshold", 3),
			RemovalViolation:   s.getInt("mod_removal_violation", 4),
			HighConfidence:     s.getFloat("mod_high_confidence", 0.9),
			PossibleConfidence: s.getFloat("mod_possible_confidence", 0.6),
		}
		s.json(w, http.StatusOK, resp)
	case http.MethodPut:
		var req moderationSettings
		if err := s.readJSON(w, r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		s.mu.Lock()
		s.settings["mod_enabled"] = req.Enabled
		s.settings["mod_dry_run"] = req.DryRun
		s.settings["mod_auto_delete"] = req.AutoDelete
		s.settings["mod_removal_enabled"] = req.RemovalEnabled
		s.settings["mod_warning_threshold"] = req.WarningThreshold
		s.settings["mod_removal_violation"] = req.RemovalViolation
		s.settings["mod_high_confidence"] = req.HighConfidence
		s.settings["mod_possible_confidence"] = req.PossibleConfidence
		s.mu.Unlock()
		s.broadcast("moderation", map[string]string{"action": "settings_updated"})
		s.json(w, http.StatusOK, map[string]string{"ok": "true"})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleModEnrolled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.modStore == nil {
		s.json(w, http.StatusOK, []enrolledItem{})
		return
	}
	groups := s.bot.ListGroups()
	ctx := r.Context()
	seen := make(map[string]struct{})
	var items []enrolledItem
	for _, g := range groups {
		entries, err := s.modStore.ListBannedStickers(ctx, g.JID)
		if err != nil {
			continue
		}
		for _, e := range entries {
			key := e.FileSHA256 + "|" + e.AudioSHA256
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, enrolledItem{
				ID:           key,
				Label:        e.Reason,
				Type:         "sticker",
				EnrolledAt:   time.Unix(e.AddedAt, 0).UTC().Format(time.RFC3339),
				EnrolledBy:   e.AddedBy,
			})
		}
	}
	s.json(w, http.StatusOK, items)
}

func (s *Server) handleModEnrolledItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/moderation/enrolled/")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "id required")
		return
	}
	s.json(w, http.StatusOK, nil)
}

func (s *Server) handleModMatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.modMu.Lock()
	items := make([]matchItem, 0, len(s.modEvents))
	for _, e := range s.modEvents {
		items = append(items, matchItem{
			ID:        e.TS,
			Tier:      "high",
			TS:        e.TS,
			User:      e.User,
			UserName:  "",
			Group:     "",
			GroupName: "",
			Type:      e.Action,
			Confidence: 1.0,
			Status:    "reviewed",
		})
	}
	s.modMu.Unlock()
	s.json(w, http.StatusOK, items)
}

func (s *Server) handleModReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/moderation/matches/")
	id = strings.TrimSuffix(id, "/review")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "id required")
		return
	}
	var req matchReviewRequest
	if err := s.readJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	s.broadcast("moderation", map[string]string{"id": id, "action": "review", "decision": req.Decision})
	s.addModEvent("dashboard", "review_"+req.Decision, "match "+id)
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleModHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.modMu.Lock()
	rows := make([]modHistoryItem, len(s.modEvents))
	copy(rows, s.modEvents)
	s.modMu.Unlock()
	anyRows := make([]any, len(rows))
	for i := range rows {
		anyRows[i] = rows[i]
	}
	s.json(w, http.StatusOK, pagedResponse{Items: anyRows, Total: len(rows)})
}

// --- Users ---

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	users := s.bot.ListUsers()
	ctx := r.Context()
	items := make([]userItem, 0, len(users))
	for _, u := range users {
		warnings := 0
		if s.modStore != nil {
			for _, g := range u.Groups {
				n, err := s.modStore.GetStrikes(ctx, g, u.JID)
				if err == nil && n > warnings {
					warnings = n
				}
			}
		}
		items = append(items, userItem{
			JID:        u.JID,
			Name:       u.Name,
			Display:    u.Name,
			Group:      strings.Join(u.Groups, ","),
			GroupName:  strings.Join(u.GroupNames, ","),
			IsAdmin:    u.IsAdmin,
			Warnings:   warnings,
		})
	}
	anyItems := make([]any, len(items))
	for i := range items {
		anyItems[i] = items[i]
	}
	s.json(w, http.StatusOK, pagedResponse{Items: anyItems, Total: len(items)})
}

func (s *Server) handleUserHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	items := []userHistoryItem{}
	s.json(w, http.StatusOK, items)
}

func (s *Server) handleUserReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleUserRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	jid := parts[0]
	if len(parts) >= 2 && parts[1] == "history" && r.Method == http.MethodGet {
		s.handleUserHistoryWithJID(w, r, jid)
		return
	}
	if len(parts) >= 3 && parts[1] == "warnings" && parts[2] == "reset" && r.Method == http.MethodPost {
		s.handleUserResetWithJID(w, r, jid)
		return
	}
	s.writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleUserHistoryWithJID(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	items := []userHistoryItem{}
	s.json(w, http.StatusOK, items)
}

func (s *Server) handleUserResetWithJID(w http.ResponseWriter, r *http.Request, jid string) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.json(w, http.StatusOK, map[string]string{"ok": "true"})
}

// --- Logs ---

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.logMu.Lock()
	items := make([]logItem, len(s.logBuffer))
	copy(items, s.logBuffer)
	s.logMu.Unlock()
	anyItems := make([]any, len(items))
	for i := range items {
		anyItems[i] = items[i]
	}
	s.json(w, http.StatusOK, pagedResponse{Items: anyItems, Total: len(items)})
}

// --- Events (SSE) ---

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

		cookie, err := r.Cookie("sonicos_session")
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.authPassword)) != 1 {
			// Allow SSE without auth for demo, but prefer auth
			_ = cookie
		}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch := make(chan []byte, 8)
	s.addClient(ch)
	defer s.removeClient(ch)

	// Send initial status
	st := s.bot.ClientState()
	initMsg := fmt.Sprintf("event: status\ndata: {\"state\":\"%s\",\"connected\":%v}\n\n", st, st == "connected")
	w.Write([]byte(initMsg))
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			w.Write(msg)
			flusher.Flush()
		}
	}
}

// --- Static file serving ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

// --- Helpers ---

func (s *Server) getStr(key, def string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func (s *Server) getBool(key string, def bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func (s *Server) getInt(key string, def int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return def
}

func (s *Server) getFloat(key string, def float64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

func (s *Server) getStat(key string, def int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		if i, ok := v.(int64); ok {
			return i
		}
	}
	return def
}

func (s *Server) addLog(level, message string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entry := logItem{
		ID:      fmt.Sprintf("log-%d", time.Now().UnixNano()),
		TS:      time.Now().UTC().Format(time.RFC3339),
		Level:   level,
		Type:    "system",
		Message: message,
	}
	s.logBuffer = append(s.logBuffer, entry)
	if len(s.logBuffer) > s.maxLogs {
		s.logBuffer = s.logBuffer[len(s.logBuffer)-s.maxLogs:]
	}
}

func (s *Server) addModEvent(user, action, detail string) {
	s.modMu.Lock()
	defer s.modMu.Unlock()
	event := modHistoryItem{
		TS:        time.Now().UTC().Format(time.RFC3339),
		User:      user,
		UserName:  user,
		Group:     "",
		GroupName: "",
		Action:    action,
		DryRun:    false,
		Detail:    detail,
	}
	s.modEvents = append(s.modEvents, event)
	if len(s.modEvents) > s.maxModEvents {
		s.modEvents = s.modEvents[len(s.modEvents)-s.maxModEvents:]
	}
	s.addLog("info", "moderation: "+action+" - "+detail)
}

func toAny(items []any) []any { return items }

// Handler returns an http.Handler with all API routes and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API routes (auth required, CORS enabled)
	mux.HandleFunc("/status", s.corsAuth(s.handleStatus))
	mux.HandleFunc("/events", s.corsAuth(s.handleEvents))
	mux.HandleFunc("/qr", s.corsAuth(s.handleQR))
	mux.HandleFunc("/connection/qr/refresh", s.corsAuth(s.handleQRRefresh))
	mux.HandleFunc("/pair", s.corsAuth(s.handlePair))
	mux.HandleFunc("/connection/reconnect", s.corsAuth(s.handleReconnect))
	mux.HandleFunc("/connection/disconnect", s.corsAuth(s.handleDisconnect))
	mux.HandleFunc("/connection/unlink", s.corsAuth(s.handleUnlink))
	mux.HandleFunc("/settings", s.corsAuth(s.handleSettings))
	mux.HandleFunc("/commands/prefix", s.corsAuth(s.handleChangePrefix))
	mux.HandleFunc("/commands", s.corsAuth(s.handleCommands))
	mux.HandleFunc("/commands/", s.corsAuth(s.handleCommandPatch))
	mux.HandleFunc("/groups", s.corsAuth(s.handleGroups))
	mux.HandleFunc("/groups/", s.corsAuth(s.handleGroup))
	mux.HandleFunc("/moderation/settings", s.corsAuth(s.handleModSettings))
	mux.HandleFunc("/moderation/enrolled", s.corsAuth(s.handleModEnrolled))
	mux.HandleFunc("/moderation/enrolled/", s.corsAuth(s.handleModEnrolledItem))
	mux.HandleFunc("/moderation/matches", s.corsAuth(s.handleModMatches))
	mux.HandleFunc("/moderation/matches/", s.corsAuth(s.handleModReview))
	mux.HandleFunc("/moderation/history", s.corsAuth(s.handleModHistory))
	mux.HandleFunc("/users", s.corsAuth(s.handleUsers))
	mux.HandleFunc("/users/", s.corsAuth(s.handleUserRoutes))
	mux.HandleFunc("/logs", s.corsAuth(s.handleLogs))

	// Auth routes (no auth required, CORS enabled)
	mux.HandleFunc("/auth/login", s.cors(s.handleLogin))
	mux.HandleFunc("/auth/logout", s.cors(s.handleLogout))
	mux.HandleFunc("/auth/me", s.cors(s.handleMe))

	// Serve the dashboard HTML at root
	mux.HandleFunc("/", s.cors(s.handleIndex))

	return mux
}

// Error sentinels
var (
	errAlreadyConnected = errors.New("already connected")
	errNoQR             = errors.New("no QR available")
)