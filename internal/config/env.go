package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnv carrega as variaveis do arquivo .env sem substituir o ambiente atual.
func LoadEnv() error {
	file, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("abrir .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("linha %d do .env nao possui o formato CHAVE=VALOR", lineNumber)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("linha %d do .env possui uma chave vazia", lineNumber)
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if err := os.Setenv(key, strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("definir variavel %s: %w", key, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ler .env: %w", err)
	}

	return nil
}

func Value(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// Required retorna uma variavel obrigatoria ou informa que ela nao foi definida.
func Required(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("variavel de ambiente obrigatoria nao definida: %s", key)
	}

	return value, nil
}
