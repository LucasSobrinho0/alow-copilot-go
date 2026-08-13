package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"alow-copilot-go/internal/invoice"
)

type Server struct {
	apiKey    string
	maxBytes  int64
	chunkSize int
	slots     chan struct{}
	extractor invoice.TextExtractor
	registry  invoice.Registry
	logger    *slog.Logger
}

func New(apiKey string, maxBytes int64, chunkSize, maxConcurrent int, extractor invoice.TextExtractor, registry invoice.Registry, logger *slog.Logger) *Server {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Server{
		apiKey: apiKey, maxBytes: maxBytes, chunkSize: chunkSize,
		slots:     make(chan struct{}, maxConcurrent),
		extractor: extractor, registry: registry, logger: logger,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /deploy-test", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/telecom/invoices/extract", s.authorize(s.extract))
	return mux
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(token) != len(s.apiKey) || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "não autorizado"})
			return
		}
		next(w, r)
	}
}

func (s *Server) extract(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "15")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"message": "extrator ocupado; tente novamente em instantes",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "arquivo inválido ou acima do limite"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "o campo file é obrigatório"})
		return
	}
	defer file.Close()
	importID := r.FormValue("import_id")
	if importID == "" {
		importID = "unknown"
	}

	text, err := s.extractor.Extract(r.Context(), io.Reader(file))
	if err != nil {
		s.logger.Warn("invoice extraction failed", "import_id", importID, "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "não foi possível extrair o texto do PDF"})
		return
	}
	defer text.Close()

	encoder := json.NewEncoder(w)
	sequence := 0
	started := false
	emit := func(chunk invoice.StreamChunk) error {
		if !started {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			started = true
		}
		sequence++
		event := invoice.Event{ImportID: importID, Sequence: sequence}
		switch {
		case chunk.Metadata != nil:
			event.Type = "metadata"
			event.Metadata = chunk.Metadata
		case len(chunk.Items) > 0:
			event.Type = "items"
			event.Items = chunk.Items
		case len(chunk.Usage) > 0:
			event.Type = "usage"
			event.Usage = chunk.Usage
		default:
			return nil
		}
		if err := encoder.Encode(event); err != nil {
			return err
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return nil
	}
	stats, err := s.registry.ParseStream(text, s.chunkSize, emit)
	if err != nil {
		if !started {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		sequence++
		_ = encoder.Encode(invoice.Event{
			Type: "error", ImportID: importID, Sequence: sequence,
			ErrorCode: "PARSING_FAILED", Message: err.Error(),
		})
		return
	}
	sequence++
	_ = encoder.Encode(invoice.Event{
		Type: "completed", ImportID: importID, Sequence: sequence,
		ItemCount: stats.ItemCount, UsageCount: stats.UsageCount, LineCount: stats.LineCount,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ParseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func ParseBytes(value string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
