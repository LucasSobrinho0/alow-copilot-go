package invoice

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type TextExtractor interface {
	Extract(context.Context, io.Reader) (ReadSeekCloser, error)
}

type PDFToTextExtractor struct{}

func (PDFToTextExtractor) Extract(ctx context.Context, input io.Reader) (ReadSeekCloser, error) {
	output, err := os.CreateTemp("", "alow-pdftotext-*.txt")
	if err != nil {
		return nil, fmt.Errorf("preparar saída do pdftotext: %w", err)
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		os.Remove(outputPath)
		return nil, fmt.Errorf("fechar saída temporária: %w", err)
	}

	command := exec.CommandContext(ctx, "pdftotext", "-layout", "-", outputPath)
	command.Stdin = input
	if err := command.Run(); err != nil {
		os.Remove(outputPath)
		return nil, fmt.Errorf("extrair texto do PDF: %w", err)
	}

	extracted, err := os.Open(outputPath)
	if err != nil {
		os.Remove(outputPath)
		return nil, fmt.Errorf("abrir texto extraído: %w", err)
	}
	stat, err := extracted.Stat()
	if err != nil {
		extracted.Close()
		os.Remove(outputPath)
		return nil, fmt.Errorf("inspecionar texto extraído: %w", err)
	}
	if stat.Size() == 0 {
		extracted.Close()
		os.Remove(outputPath)
		return nil, fmt.Errorf("PDF sem texto pesquisável")
	}

	return &temporaryTextFile{File: extracted, path: outputPath}, nil
}

type temporaryTextFile struct {
	*os.File
	path string
}

func (file *temporaryTextFile) Close() error {
	closeError := file.File.Close()
	removeError := os.Remove(file.path)
	if closeError != nil {
		return closeError
	}
	return removeError
}
