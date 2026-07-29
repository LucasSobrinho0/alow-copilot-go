package invoice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaroParserExtractsHeaderAndOptionalLineSections(t *testing.T) {
	text := `                                                                                         Período de uso
                                                                                         de 21/05/2026 a 20/06/2026
                                                                                                                                                   Vencimento
                            EMPRESA DE TESTE
                                                                                         Nº da conta: 132544238                                  17/07/2026
                                                                                         Nº do cliente: 125824417
                            AV CENTRAL 100
                            SALA 1                                                       CPF/CNPJ 08.861.901/0001-80
                                                                                         Razão Social: Claro S/A
Total a pagar R$ 58,99
Cliente        Mês Referência
EMPRESA TESTE  Junho 2026
84850000001-3 55970162202-6 60717132544-9 23807316188-8

DETALHAMENTO DE LIGAÇÕES E SERVIÇOS DO CELULAR (65) 99604 4554
Mensalidades e Pacotes Promocionais
Descrição                                                                       Total (R$)
Oferta Conjunta Claro MIX                                                       48,49
   Claro Monitor lite                                                           -
Bônus de Internet Turbo - 4GB                                                   0,00
TOTAL                                                                        R$ 48,49
Ligações Locais
Ligações Tarifa Zero Local
Data Hora         Origem(UF)-Destino                                            Número        Dur. Efetiva   Duração Tarifa (R$) Valor Total (R$) Valor Cobrado (R$)
24/05 17:28:04 Mato Grosso Mato Grosso (65)                                     65999122142   00:00:11       00:00:30 0,00       0,00             0,00
Internet (MB)
Serviço                                                                         Mbytes Utilizados   Tarifa (R$) Valor Total (R$) Valor Cobrado (R$)
Internet                                                                        4.290,927           0,00        0,00             0,00
Detalhes da Internet móvel
Data                                                                            Mbytes Utilizados   Tarifa (R$) Valor Cobrado (R$)
19/05                                                                           80,061              0,00        0,00
Torpedos
Descrição                                                                       Quantidade Tarifa (R$) Valor Total (R$) Valor Cobrado (R$)
Torpedo - Outras Operadoras                                                     3,000      0,39        1,17             0,00
Envio de SMS’s realizado pelo seu celular
Data Hora        Origem-Destino                                                 Número             Tarifa (R$) Valor Cobrado (R$)
02/06 09:14:17 Mato Grosso Mato Grosso (65)                                     65-99920-3078      0,39        0,00

DETALHAMENTO DE LIGAÇÕES E SERVIÇOS DO CELULAR (65) 99912 2142
Mensalidades e Pacotes Promocionais
Descrição                                                                       Total (R$)
Plano somente mensalidade                                                       10,50
TOTAL                                                                        R$ 10,50`

	document, err := NewClaroParser().Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if document.OperatorCode.Value != "CLARO" || document.ContractNumber.Value != "132544238" {
		t.Fatalf("identificação incorreta: %#v", document)
	}
	if document.CustomerName.Value != "EMPRESA DE TESTE" || document.CustomerDocument.Value != "08861901000180" {
		t.Fatalf("cliente incorreto: %#v", document)
	}
	if document.ReferenceStart.Value != "2026-05-21" || document.ReferenceEnd.Value != "2026-06-20" ||
		document.ReferenceMonth.Value != "2026-06-01" || document.DueDate.Value != "2026-07-17" {
		t.Fatalf("datas incorretas: %#v", document)
	}
	if document.TotalAmountCents.Value != 5899 || len(document.Barcode.Value) != 48 {
		t.Fatalf("pagamento incorreto: %#v", document)
	}
	if document.LineCount != 2 || len(document.Items) != 4 {
		t.Fatalf("esperava dois blocos e quatro serviços, recebeu lines=%d items=%#v", document.LineCount, document.Items)
	}
	if !document.Items[1].Included || document.Items[1].ParentServiceName != "Oferta Conjunta Claro MIX" {
		t.Fatalf("serviço incluído não preservado: %#v", document.Items[1])
	}
	if len(document.UsageRecords) != 5 {
		t.Fatalf("esperava ligação, resumo/detalhe de dados e resumo/detalhe de SMS; recebeu %#v", document.UsageRecords)
	}
	if document.UsageRecords[0].EffectiveDurationSeconds != 11 || document.UsageRecords[0].BilledDurationSeconds != 30 {
		t.Fatalf("duração da ligação incorreta: %#v", document.UsageRecords[0])
	}
}

func TestClaroParserRealFixturesWhenAvailable(t *testing.T) {
	fixtures := []struct {
		name            string
		expectedLines   int
		expectedAccount string
		expectedName    string
		expectedStart   string
		expectedEnd     string
		expectedDue     string
		expectedTotal   int64
	}{
		{
			name: "alow_claro.txt", expectedLines: 3, expectedAccount: "132544238",
			expectedName: "JORGE LUIZ CAMPOS", expectedStart: "2026-05-21",
			expectedEnd: "2026-06-20", expectedDue: "2026-07-17", expectedTotal: 15597,
		},
		{
			name: "claro_test.txt", expectedLines: 15, expectedAccount: "182066895",
			expectedName: "COMPANHIA RIOGRANDENSE DE SANEAMENTO CORSAN", expectedStart: "2026-04-02",
			expectedEnd: "2026-05-01", expectedDue: "2026-05-28", expectedTotal: 270000,
		},
		{
			name: "faturagrande.txt", expectedLines: 3661, expectedAccount: "163636445",
			expectedName: "COMPANHIA RIOGRANDENSE DE SANEAMENTO CORSAN", expectedStart: "2026-04-02",
			expectedEnd: "2026-05-01", expectedDue: "2026-05-28", expectedTotal: 4917647,
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "media", "pdftotext", fixture.name)
			text, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				t.Skip("fixture real não está disponível neste ambiente")
			}
			if err != nil {
				t.Fatal(err)
			}
			document, err := NewClaroParser().Parse(string(text))
			if err != nil {
				t.Fatal(err)
			}
			if document.ContractNumber.Value != fixture.expectedAccount {
				t.Fatalf("conta esperada %s, recebeu %s", fixture.expectedAccount, document.ContractNumber.Value)
			}
			if document.CustomerName.Value != fixture.expectedName ||
				document.ReferenceStart.Value != fixture.expectedStart ||
				document.ReferenceEnd.Value != fixture.expectedEnd ||
				document.DueDate.Value != fixture.expectedDue ||
				document.TotalAmountCents.Value != fixture.expectedTotal {
				t.Fatalf(
					"cabeçalho inesperado: name=%q start=%q end=%q due=%q total=%d",
					document.CustomerName.Value, document.ReferenceStart.Value, document.ReferenceEnd.Value,
					document.DueDate.Value, document.TotalAmountCents.Value,
				)
			}
			if document.LineCount != fixture.expectedLines {
				t.Fatalf("esperava %d linhas, recebeu %d", fixture.expectedLines, document.LineCount)
			}
			if len(document.Items) < fixture.expectedLines {
				t.Fatalf("esperava ao menos um serviço por linha, recebeu %d serviços", len(document.Items))
			}
			if document.CustomerDocument.Value == "" || document.ReferenceMonth.Value == "" || document.DueDate.Value == "" {
				t.Fatalf("metadados obrigatórios ausentes: %#v", document.MissingFields)
			}
			if len(document.Barcode.Value) != 48 || len(document.UsageRecords) == 0 {
				t.Fatalf("pagamento ou consumo ausente: barcode=%q usage=%d", document.Barcode.Value, len(document.UsageRecords))
			}
			maxItems, maxUsage := 0, 0
			streamStats, err := NewClaroParser().ParseStream(strings.NewReader(string(text)), 250, func(chunk StreamChunk) error {
				maxItems = max(maxItems, len(chunk.Items))
				maxUsage = max(maxUsage, len(chunk.Usage))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if streamStats.LineCount != document.LineCount ||
				streamStats.ItemCount != len(document.Items) ||
				streamStats.UsageCount != len(document.UsageRecords) ||
				maxItems > 250 || maxUsage > 250 {
				t.Fatalf(
					"stream divergente: stats=%#v document=(%d,%d,%d) max=(%d,%d)",
					streamStats, document.LineCount, len(document.Items), len(document.UsageRecords), maxItems, maxUsage,
				)
			}
			t.Logf("linhas=%d serviços=%d consumos=%d", document.LineCount, len(document.Items), len(document.UsageRecords))
		})
	}
}

func TestClaroParserStreamsLargeFixtureInBoundedChunks(t *testing.T) {
	path := filepath.Join("..", "..", "media", "pdftotext", "faturagrande.txt")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		t.Skip("fixture grande não está disponível neste ambiente")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	metadataEvents := 0
	itemCount := 0
	usageCount := 0
	stats, err := NewClaroParser().ParseStream(file, 250, func(chunk StreamChunk) error {
		if chunk.Metadata != nil {
			metadataEvents++
		}
		if len(chunk.Items) > 250 || len(chunk.Usage) > 250 {
			t.Fatalf("lote excedeu 250 registros: items=%d usage=%d", len(chunk.Items), len(chunk.Usage))
		}
		itemCount += len(chunk.Items)
		usageCount += len(chunk.Usage)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if metadataEvents != 1 || stats.LineCount != 3661 ||
		stats.ItemCount != itemCount || stats.UsageCount != usageCount {
		t.Fatalf(
			"stream grande inconsistente: metadata=%d items=%d usage=%d stats=%#v",
			metadataEvents, itemCount, usageCount, stats,
		)
	}
}

func TestClaroParserStreamsBoundedChunksAndReconcilesCallSubtotal(t *testing.T) {
	text := `EMPRESA TESTE
Nº da conta: 123
CPF/CNPJ 08.861.901/0001-80
Razão Social: Claro S/A
Período de uso de 01/06/2026 a 30/06/2026
Vencimento 10/07/2026
Total a pagar R$ 10,00
DETALHAMENTO DE LIGAÇÕES E SERVIÇOS DO CELULAR (65) 99999 1111
Mensalidades e Pacotes Promocionais
Plano A 10,00
TOTAL R$ 10,00
Ligações Locais
Ligações Tarifa Zero Local
Data Hora Origem(UF)-Destino Número Dur. Efetiva Duração Tarifa (R$) Valor Total (R$) Valor Cobrado (R$)
05/06 10:00:00 Mato Grosso Mato Grosso (65) 65999992222 00:01:00 00:01:00 1,00 1,00 1,00
Total 00:01:00 00:01:00 1,00 1,20 1,20
DETALHAMENTO DE LIGAÇÕES E SERVIÇOS DO CELULAR (65) 99999 2222
Mensalidades e Pacotes Promocionais
Plano B 0,00
TOTAL R$ 0,00`

	chunks := make([]StreamChunk, 0)
	stats, err := NewClaroParser().ParseStream(strings.NewReader(text), 1, func(chunk StreamChunk) error {
		if len(chunk.Items) > 1 || len(chunk.Usage) > 1 {
			t.Fatalf("lote excedeu o limite: %#v", chunk)
		}
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.LineCount != 2 || stats.ItemCount != 2 || len(chunks) < 4 || chunks[0].Metadata == nil {
		t.Fatalf("stream inesperado: stats=%#v chunks=%#v", stats, chunks)
	}

	var summary *UsageRecord
	for _, chunk := range chunks {
		if len(chunk.Usage) == 1 && chunk.Usage[0].Type == "CALL_SUMMARY" {
			record := chunk.Usage[0]
			summary = &record
		}
	}
	if summary == nil || summary.ReconciliationStatus != "DIVERGENT" ||
		summary.DetailChargedCents != 100 || summary.DifferenceCents != -20 ||
		summary.DetailCount != 1 {
		t.Fatalf("reconciliação inesperada: %#v", summary)
	}
}
