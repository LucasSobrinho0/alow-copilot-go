package diagnostic

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"alow-copilot-go/internal/invoice"
)

const outputDirectoryName = "pdftotext"

type Exporter struct {
	MediaDirectory string
	Extractor      invoice.TextExtractor
}

type Report struct {
	Processed       int
	OutputDirectory string
}

func ParseEnabled(value string) (bool, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("PDF_TO_TEXT deve ser true ou false: %w", err)
	}
	return enabled, nil
}

func (e Exporter) Run(ctx context.Context) (Report, error) {
	if strings.TrimSpace(e.MediaDirectory) == "" {
		return Report{}, fmt.Errorf("diretório de mídia não informado")
	}
	if e.Extractor == nil {
		return Report{}, fmt.Errorf("extrator é obrigatório")
	}

	entries, err := os.ReadDir(e.MediaDirectory)
	if err != nil {
		return Report{}, fmt.Errorf("listar %s: %w", e.MediaDirectory, err)
	}

	inputs := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			continue
		}
		inputs = append(inputs, entry.Name())
	}
	sort.Strings(inputs)
	if len(inputs) == 0 {
		return Report{}, fmt.Errorf("nenhum PDF encontrado em %s", e.MediaDirectory)
	}

	outputDirectory := filepath.Join(e.MediaDirectory, outputDirectoryName)
	if err := os.MkdirAll(outputDirectory, 0o750); err != nil {
		return Report{}, fmt.Errorf("criar %s: %w", outputDirectory, err)
	}

	for _, inputName := range inputs {
		if err := e.exportFile(ctx, inputName, outputDirectory); err != nil {
			return Report{}, err
		}
	}

	return Report{Processed: len(inputs), OutputDirectory: outputDirectory}, nil
}

func (e Exporter) exportFile(ctx context.Context, inputName, outputDirectory string) error {
	inputPath := filepath.Join(e.MediaDirectory, inputName)
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", inputName, err)
	}
	defer input.Close()

	text, err := e.Extractor.Extract(ctx, input)
	if err != nil {
		return fmt.Errorf("%s: %w", inputName, err)
	}
	defer text.Close()

	outputName := strings.TrimSuffix(inputName, filepath.Ext(inputName)) + ".txt"
	outputPath := filepath.Join(outputDirectory, outputName)
	temporary, err := os.CreateTemp(outputDirectory, "."+outputName+".tmp-")
	if err != nil {
		return fmt.Errorf("criar saída temporária de %s: %w", inputName, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := io.Copy(temporary, text); err != nil {
		temporary.Close()
		return fmt.Errorf("gravar texto extraído de %s: %w", inputName, err)
	}
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return fmt.Errorf("proteger saída de %s: %w", inputName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("fechar saída de %s: %w", inputName, err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publicar saída de %s: %w", inputName, err)
	}
	return nil
}
