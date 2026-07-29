package diagnostic

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"alow-copilot-go/internal/invoice"
)

type fakeExtractor struct{}

func (fakeExtractor) Extract(context.Context, io.Reader) (invoice.ReadSeekCloser, error) {
	return memoryText{Reader: bytes.NewReader([]byte("VIVO CONTA\n  Linha preservada    R$ 109,90\n"))}, nil
}

type memoryText struct{ *bytes.Reader }

func (memoryText) Close() error { return nil }

func TestParseEnabled(t *testing.T) {
	enabled, err := ParseEnabled("true")
	if err != nil || !enabled {
		t.Fatalf("esperava true, recebeu %v, %v", enabled, err)
	}
	if _, err := ParseEnabled("talvez"); err == nil {
		t.Fatal("valor inválido deveria falhar")
	}
}

func TestExporterCreatesRawPDFToTextOutput(t *testing.T) {
	media := t.TempDir()
	if err := os.WriteFile(filepath.Join(media, "vivo.PDF"), []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	exporter := Exporter{MediaDirectory: media, Extractor: fakeExtractor{}}

	report, err := exporter.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Processed != 1 {
		t.Fatalf("esperava 1 PDF, recebeu %d", report.Processed)
	}

	output, err := os.ReadFile(filepath.Join(media, outputDirectoryName, "vivo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "VIVO CONTA\n  Linha preservada    R$ 109,90\n"
	if string(output) != expected {
		t.Fatalf("texto extraído foi alterado:\n%s", output)
	}
}
