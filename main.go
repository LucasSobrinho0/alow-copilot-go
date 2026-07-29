package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"alow-copilot-go/internal/config"
	"alow-copilot-go/internal/diagnostic"
	"alow-copilot-go/internal/httpapi"
	"alow-copilot-go/internal/invoice"
)

func main() {
	if err := config.LoadEnv(); err != nil {
		log.Fatal(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "pdftotext" {
		runPDFToTextDiagnostic()
		return
	}

	if _, err := config.Required("API_KEY"); err != nil {
		log.Fatal(err)
	}

	address := config.Value("HTTP_ADDRESS", ":8080")
	server := httpapi.New(
		os.Getenv("API_KEY"),
		httpapi.ParseBytes(config.Value("MAX_UPLOAD_BYTES", ""), 100<<20),
		httpapi.ParseInt(config.Value("ITEM_CHUNK_SIZE", ""), 250),
		httpapi.ParseInt(config.Value("MAX_CONCURRENT_EXTRACTIONS", ""), 2),
		invoice.PDFToTextExtractor{},
		invoice.NewRegistry(
			invoice.NewVivoParser(),
			invoice.NewClaroParser(),
		),
		slog.Default(),
	)
	log.Printf("servico de extracao iniciado em %s", address)
	log.Fatal(http.ListenAndServe(address, server.Handler()))
}

func runPDFToTextDiagnostic() {
	enabled, err := diagnostic.ParseEnabled(config.Value("PDF_TO_TEXT", "false"))
	if err != nil {
		log.Fatal(err)
	}
	if !enabled {
		log.Print("PDF_TO_TEXT=false: diagnóstico desabilitado; nenhum arquivo foi criado")
		return
	}

	exporter := diagnostic.Exporter{
		MediaDirectory: "/media",
		Extractor:      invoice.PDFToTextExtractor{},
	}
	report, err := exporter.Run(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d PDF(s) processado(s); arquivos disponíveis em %s", report.Processed, report.OutputDirectory)
}
