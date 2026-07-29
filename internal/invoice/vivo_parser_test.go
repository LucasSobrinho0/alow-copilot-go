package invoice

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVivoParserExtractsHeaderLinesAndAccountAdjustments(t *testing.T) {
	text := `Nº da Conta: 0421466285
Mês de referência: 05/2026
Período: 06/04/2026 a 05/05/2026
Telefonica Brasil S.A.
CNPJ Matriz :02.558.157/0001-62
AGUAS DO RIO 1 SPE SA                                                         CPF/CNPJ: 42.310.775/0001-03
Vencimento
28/05/2026
Total a Pagar - R$
130,00
Outros Lançamentos
Parcelamento (Ex.: Conta; Aparelho e Outros)                                  100,00
846800006047 413000480017 104214662850 052622605286
DETALHAMENTO TOTAL DA CONTA
VEJA OS NÚMEROS VIVO E PLANOS QUE COMPÕEM A SUA CONTA
Número Vivo Plano Valor Total R$
21-99677-6236 SMART EMPRESAS 6GB TE 15,00 21-99771-0761 PLANO BASE INTERNET PJ 10,00
Total Números Vivo: 2`

	document, err := NewVivoParser().Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if document.OperatorCode.Value != "VIVO" || document.ContractNumber.Value != "0421466285" {
		t.Fatalf("identificação incorreta: %#v", document)
	}
	if document.CustomerName.Value != "AGUAS DO RIO 1 SPE SA" || document.CustomerDocument.Value != "42310775000103" {
		t.Fatalf("cliente incorreto: %#v", document)
	}
	if document.ReferenceMonth.Value != "2026-05-01" || document.ReferenceStart.Value != "2026-04-06" ||
		document.ReferenceEnd.Value != "2026-05-05" || document.DueDate.Value != "2026-05-28" {
		t.Fatalf("datas incorretas: %#v", document)
	}
	if document.TotalAmountCents.Value != 13000 || document.LineCount != 2 || len(document.Barcode.Value) != 48 {
		t.Fatalf("totais incorretos: %#v", document)
	}
	if sumVivoItems(document.Items) != document.TotalAmountCents.Value {
		t.Fatalf("itens não conciliam com total: %#v", document.Items)
	}
	if document.Items[0].PhoneNumber != "5521996776236" || document.Items[1].PhoneNumber != "5521997710761" {
		t.Fatalf("linhas incorretas: %#v", document.Items)
	}
}

func TestVivoParserRealFixturesWhenAvailable(t *testing.T) {
	fixtures := []struct {
		textName         string
		pdfName          string
		expectedAccount  string
		expectedName     string
		expectedDocument string
		expectedLines    int
		expectedTotal    int64
		expectedDue      string
	}{
		{"vivo.txt", "vivo.PDF", "0384607283", "UNIMED VALE DO SEPOTUBA - COOPERATI", "02597394000132", 46, 186806, "2026-07-25"},
		{"vivo_dois.txt", "vivo_dois.PDF", "0454492190", "COMPANHIA RIOGRANDENSE DE SANEAMENT", "92802784000190", 600, 913577, "2026-05-28"},
		{"vivo_tres.txt", "vivo_tres.PDF", "0421466285", "AGUAS DO RIO 1 SPE SA", "42310775000103", 915, 6044130, "2026-05-28"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.pdfName, func(t *testing.T) {
			text := vivoFixtureText(t, fixture.textName, fixture.pdfName)
			document, err := NewVivoParser().Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			if document.ContractNumber.Value != fixture.expectedAccount || document.CustomerName.Value != fixture.expectedName ||
				document.CustomerDocument.Value != fixture.expectedDocument || document.LineCount != fixture.expectedLines ||
				document.TotalAmountCents.Value != fixture.expectedTotal || document.DueDate.Value != fixture.expectedDue {
				t.Fatalf("fixture inesperada: account=%q name=%q document=%q lines=%d total=%d due=%q missing=%#v",
					document.ContractNumber.Value, document.CustomerName.Value, document.CustomerDocument.Value,
					document.LineCount, document.TotalAmountCents.Value, document.DueDate.Value, document.MissingFields)
			}
			if document.ReferenceMonth.Value == "" || document.ReferenceStart.Value == "" || document.ReferenceEnd.Value == "" {
				t.Fatalf("referência incompleta: %#v", document)
			}
			if sumVivoItems(document.Items) != document.TotalAmountCents.Value {
				t.Fatalf("soma dos itens (%d) difere do total (%d)", sumVivoItems(document.Items), document.TotalAmountCents.Value)
			}

			maxChunk := 0
			stats, err := NewVivoParser().ParseStream(strings.NewReader(text), 250, func(chunk StreamChunk) error {
				maxChunk = max(maxChunk, len(chunk.Items))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if stats.LineCount != fixture.expectedLines || stats.ItemCount != len(document.Items) || maxChunk > 250 {
				t.Fatalf("stream inconsistente: stats=%#v items=%d max=%d", stats, len(document.Items), maxChunk)
			}
		})
	}
}

func vivoFixtureText(t *testing.T, textName, pdfName string) string {
	t.Helper()
	media := filepath.Join("..", "..", "media")
	textPath := filepath.Join(media, "pdftotext", textName)
	if contents, err := os.ReadFile(textPath); err == nil {
		return string(contents)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext e fixture textual não estão disponíveis")
	}
	pdfPath := filepath.Join(media, pdfName)
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skip("fixture PDF não está disponível")
	}
	outputPath := filepath.Join(t.TempDir(), textName)
	if output, err := exec.Command("pdftotext", "-layout", pdfPath, outputPath).CombinedOutput(); err != nil {
		t.Fatalf("extrair fixture Vivo: %v: %s", err, output)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func sumVivoItems(items []Item) int64 {
	total := int64(0)
	for _, item := range items {
		total += item.AmountCents
	}
	return total
}
