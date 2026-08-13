package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"alow-copilot-go/internal/invoice"
)

type fakeExtractor struct{ text string }

func (f fakeExtractor) Extract(context.Context, io.Reader) (invoice.ReadSeekCloser, error) {
	return memoryText{Reader: bytes.NewReader([]byte(f.text))}, nil
}

type memoryText struct{ *bytes.Reader }

func (memoryText) Close() error { return nil }

func TestDeployTestReturnsOnlyOKStatus(t *testing.T) {
	server := New("secret", 1<<20, 1, 1, fakeExtractor{}, invoice.NewRegistry(), slog.Default())
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/deploy-test", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status: %d", response.Code)
	}
	if response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("resposta inesperada: %s", response.Body.String())
	}
}

func TestExtractRequiresAuthenticationAndStreamsNDJSON(t *testing.T) {
	server := New("secret", 1<<20, 1, 1, fakeExtractor{text: "VIVO\nContrato: 123\nCNPJ 12.345.678/0001-90\nCompetência: 07/2026\n(65) 99999-2222 Plano R$ 10,00"}, invoice.NewRegistry(invoice.NewTelecomParser("VIVO", "VIVO")), slog.Default())
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/telecom/invoices/extract", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", unauthorized.Code)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "invoice.pdf")
	_, _ = part.Write([]byte("%PDF"))
	_ = writer.WriteField("import_id", "import-1")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/v1/telecom/invoices/extract", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if lines := strings.Count(strings.TrimSpace(response.Body.String()), "\n") + 1; lines != 3 {
		t.Fatalf("esperava metadata, items e completed; recebeu %d linhas: %s", lines, response.Body.String())
	}
}

type blockingExtractor struct {
	started chan struct{}
	release chan struct{}
}

func (extractor blockingExtractor) Extract(context.Context, io.Reader) (invoice.ReadSeekCloser, error) {
	close(extractor.started)
	<-extractor.release
	return memoryText{Reader: bytes.NewReader([]byte("VIVO\nContrato: 123"))}, nil
}

func TestExtractRejectsRequestsAboveConcurrencyLimit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := New(
		"secret",
		1<<20,
		10,
		1,
		blockingExtractor{started: started, release: release},
		invoice.NewRegistry(invoice.NewTelecomParser("VIVO", "VIVO")),
		slog.Default(),
	)

	firstDone := make(chan struct{})
	firstRequest := extractionRequest(t)
	go func() {
		defer close(firstDone)
		server.Handler().ServeHTTP(httptest.NewRecorder(), firstRequest)
	}()
	<-started

	second := httptest.NewRecorder()
	server.Handler().ServeHTTP(second, extractionRequest(t))
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("esperava 429 com Retry-After; status=%d headers=%v", second.Code, second.Header())
	}

	close(release)
	<-firstDone
}

func TestExtractRemovesMultipartTemporaryFiles(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Setenv("TMPDIR", temporaryDirectory)
	server := New(
		"secret",
		16<<20,
		10,
		1,
		fakeExtractor{text: "VIVO\nContrato: 123\nCNPJ 12.345.678/0001-90\nCompetência: 07/2026"},
		invoice.NewRegistry(invoice.NewTelecomParser("VIVO", "VIVO")),
		slog.Default(),
	)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "large-invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), 9<<20)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/telecom/invoices/extract", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("arquivos multipart temporários não foram removidos: %v", entries)
	}
}

func extractionRequest(t *testing.T) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("%PDF"))
	_ = writer.WriteField("import_id", "import-concurrency")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/v1/telecom/invoices/extract", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer secret")
	return request
}
