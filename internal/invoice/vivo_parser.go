package invoice

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	vivoOperatorPattern  = regexp.MustCompile(`(?i)telef[oô]nica\s+brasil\s+s\.?\s*a\.?`)
	vivoAccountPattern   = regexp.MustCompile(`(?i)n(?:[úu]mero|[º°o])\s+da\s+conta\s*:\s*([0-9]+)`)
	vivoReferencePattern = regexp.MustCompile(
		`(?i)m[eê]s\s+de\s+refer[eê]ncia\s*:\s*(0[1-9]|1[0-2])/(\d{4})`,
	)
	vivoPeriodPattern = regexp.MustCompile(
		`(?i)per[ií]odo\s*:\s*(\d{2}/\d{2}/\d{4})\s+a\s+(\d{2}/\d{2}/\d{4})`,
	)
	vivoCustomerPattern = regexp.MustCompile(
		`(?i)^\s*(.*?)\s{2,}CPF/CNPJ\s*:\s*(\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2})`,
	)
	vivoCustomerDocumentPattern = regexp.MustCompile(
		`(?i)CPF/CNPJ\s*:\s*(\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2})`,
	)
	vivoDueLabelPattern   = regexp.MustCompile(`(?i)^\s*vencimento\s*:?[\s]*$`)
	vivoTotalLabelPattern = regexp.MustCompile(`(?i)total\s+a\s+pagar(?:\s*-\s*R\$|\s*:)?`)
	vivoSummaryStart      = regexp.MustCompile(`(?i)VEJA\s+OS\s+N[ÚU]MEROS\s+VIVO\s+E\s+PLANOS`)
	vivoSummaryEnd        = regexp.MustCompile(`(?i)^\s*Total\s+N[úu]meros\s+Vivo\s*:\s*(\d+)`)
	vivoPhonePattern      = regexp.MustCompile(`\b\d{2}-\d{4,5}-\d{4}\b`)
	vivoMoneyToken        = regexp.MustCompile(`-?\d{1,3}(?:\.\d{3})*,\d{2}|-?\d+,\d{2}`)
	vivoBarcodePattern    = regexp.MustCompile(`\b(\d{12})\s+(\d{12})\s+(\d{12})\s+(\d{12})\b`)
	vivoColumns           = regexp.MustCompile(`\s{2,}`)
)

type VivoParser struct{}

func NewVivoParser() VivoParser { return VivoParser{} }

func (VivoParser) Code() string { return "VIVO" }

func (VivoParser) Score(text string) int {
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	sample := strings.Join(lines, "\n")
	if vivoOperatorPattern.MatchString(sample) {
		return 100
	}
	return 0
}

func (VivoParser) Parse(text string) (Document, error) {
	document := Document{}
	stats, err := NewVivoParser().ParseStream(strings.NewReader(text), 250, func(chunk StreamChunk) error {
		if chunk.Metadata != nil {
			document = *chunk.Metadata
		}
		document.Items = append(document.Items, chunk.Items...)
		document.UsageRecords = append(document.UsageRecords, chunk.Usage...)
		return nil
	})
	if err != nil {
		return Document{}, err
	}
	document.LineCount = stats.LineCount
	finalizeVivoMetadata(&document, true)
	return document, nil
}

func (VivoParser) ParseStream(reader io.Reader, chunkSize int, emit func(StreamChunk) error) (ParseStats, error) {
	if chunkSize <= 0 {
		chunkSize = 250
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	preamble := make([]string, 0, 512)
	stats := ParseStats{}
	lineItems := make([]Item, 0, chunkSize)
	lineItemsTotal := int64(0)
	nextRow := 1
	metadataEmitted := false
	inLineSummary := false
	var header Document

	emitMetadata := func() error {
		if metadataEmitted {
			return nil
		}
		header = parseVivoHeader(preamble)
		finalizeVivoMetadata(&header, false)
		metadataEmitted = true
		return emit(StreamChunk{Metadata: &header})
	}
	emitItems := func(items []Item) error {
		for start := 0; start < len(items); start += chunkSize {
			end := min(start+chunkSize, len(items))
			if err := emit(StreamChunk{Items: items[start:end]}); err != nil {
				return err
			}
		}
		stats.ItemCount += len(items)
		return nil
	}
	flushLineItems := func() error {
		if len(lineItems) == 0 {
			return nil
		}
		if err := emitItems(lineItems); err != nil {
			return err
		}
		lineItems = lineItems[:0]
		return nil
	}

	for scanner.Scan() {
		rawLine := scanner.Text()
		line := normalizeVivoLine(rawLine)
		if !inLineSummary {
			if vivoSummaryStart.MatchString(line) {
				if err := emitMetadata(); err != nil {
					return stats, err
				}
				inLineSummary = true
				continue
			}
			if !metadataEmitted {
				preamble = append(preamble, rawLine)
			}
			continue
		}

		if match := vivoSummaryEnd.FindStringSubmatch(line); len(match) == 2 {
			if err := flushLineItems(); err != nil {
				return stats, err
			}
			inLineSummary = false
			continue
		}

		for _, item := range parseVivoSummaryLine(rawLine) {
			item.Row = nextRow
			nextRow++
			lineItemsTotal += item.AmountCents
			lineItems = append(lineItems, item)
			stats.LineCount++
			if len(lineItems) >= chunkSize {
				if err := flushLineItems(); err != nil {
					return stats, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("ler fatura Vivo: %w", err)
	}
	if err := emitMetadata(); err != nil {
		return stats, err
	}
	if err := flushLineItems(); err != nil {
		return stats, err
	}
	if stats.LineCount == 0 {
		return stats, fmt.Errorf("nenhum número Vivo foi identificado no detalhamento da conta")
	}

	accountItems := parseVivoAccountItems(preamble)
	accountTotal := int64(0)
	for index := range accountItems {
		accountItems[index].Row = nextRow
		nextRow++
		accountTotal += accountItems[index].AmountCents
	}
	if difference := header.TotalAmountCents.Value - lineItemsTotal - accountTotal; difference != 0 {
		accountItems = append(accountItems, Item{
			Row: nextRow, ServiceName: "Ajuste da conta não detalhado por linha", Quantity: 1,
			Unit: "UN", AmountCents: difference, Confidence: .65,
			AdditionalInformation: "Diferença entre o total da conta e os valores detalhados por número Vivo.",
		})
	}
	if err := emitItems(accountItems); err != nil {
		return stats, err
	}
	return stats, nil
}

func parseVivoHeader(lines []string) Document {
	document := Document{
		SchemaVersion: "telecom-invoice.v1",
		OperatorCode:  Field[string]{Value: "VIVO", Confidence: .99},
	}
	joined := strings.Join(lines, "\n")
	if match := vivoAccountPattern.FindStringSubmatch(joined); len(match) == 2 {
		document.ContractNumber = Field[string]{Value: match[1], Confidence: .99}
		document.CustomerCode = Field[string]{Value: match[1], Confidence: .96}
	}
	if match := vivoReferencePattern.FindStringSubmatch(joined); len(match) == 3 {
		document.ReferenceMonth = Field[string]{Value: match[2] + "-" + match[1] + "-01", Confidence: .99}
	}
	if match := vivoPeriodPattern.FindStringSubmatch(joined); len(match) == 3 {
		document.ReferenceStart = Field[string]{Value: isoDate(match[1]), Confidence: .99}
		document.ReferenceEnd = Field[string]{Value: isoDate(match[2]), Confidence: .99}
	}
	document.CustomerName, document.CustomerDocument = findVivoCustomer(lines)
	document.DueDate = Field[string]{Value: findVivoLabeledDate(lines, vivoDueLabelPattern), Confidence: .98}
	document.TotalAmountCents = Field[int64]{Value: findVivoTotal(lines), Confidence: .99}
	if match := vivoBarcodePattern.FindStringSubmatch(joined); len(match) == 5 {
		document.Barcode = Field[string]{Value: strings.Join(match[1:], ""), Confidence: .99}
	}
	return document
}

func findVivoCustomer(lines []string) (Field[string], Field[string]) {
	for _, rawLine := range lines {
		line := strings.ReplaceAll(rawLine, "\f", "")
		if match := vivoCustomerPattern.FindStringSubmatch(line); len(match) == 3 {
			name := strings.TrimSpace(match[1])
			if name != "" {
				return Field[string]{Value: name, Confidence: .99}, Field[string]{Value: digits(match[2]), Confidence: .99}
			}
		}
	}
	// Algumas versões quebram o nome e o CPF/CNPJ em linhas diferentes.
	for index, rawLine := range lines {
		if match := vivoCustomerDocumentPattern.FindStringSubmatch(rawLine); len(match) == 2 {
			for previous := index - 1; previous >= 0 && previous >= index-3; previous-- {
				name := strings.TrimSpace(strings.ReplaceAll(lines[previous], "\f", ""))
				if name != "" && !strings.Contains(strings.ToUpper(name), "TELEFÔNICA BRASIL") {
					return Field[string]{Value: name, Confidence: .82}, Field[string]{Value: digits(match[1]), Confidence: .99}
				}
			}
		}
	}
	return Field[string]{}, Field[string]{}
}

func findVivoLabeledDate(lines []string, label *regexp.Regexp) string {
	for index, rawLine := range lines {
		line := normalizeVivoLine(rawLine)
		if !label.MatchString(line) {
			continue
		}
		for offset := 0; offset <= 2 && index+offset < len(lines); offset++ {
			if value := datePattern.FindString(lines[index+offset]); value != "" {
				return isoDate(value)
			}
		}
	}
	return ""
}

func findVivoTotal(lines []string) int64 {
	for index, rawLine := range lines {
		if !vivoTotalLabelPattern.MatchString(normalizeVivoLine(rawLine)) {
			continue
		}
		for offset := 0; offset <= 2 && index+offset < len(lines); offset++ {
			amounts := vivoMoneyToken.FindAllString(lines[index+offset], -1)
			if len(amounts) > 0 {
				return moneyCents(amounts[len(amounts)-1])
			}
		}
	}
	return 0
}

func parseVivoSummaryLine(rawLine string) []Item {
	phoneIndexes := vivoPhonePattern.FindAllStringIndex(rawLine, -1)
	items := make([]Item, 0, len(phoneIndexes))
	for index, phoneIndex := range phoneIndexes {
		segmentEnd := len(rawLine)
		if index+1 < len(phoneIndexes) {
			segmentEnd = phoneIndexes[index+1][0]
		}
		segment := rawLine[phoneIndex[1]:segmentEnd]
		amountIndexes := vivoMoneyToken.FindAllStringIndex(segment, -1)
		if len(amountIndexes) == 0 {
			continue
		}
		amountIndex := amountIndexes[len(amountIndexes)-1]
		serviceName := strings.TrimSpace(segment[:amountIndex[0]])
		if serviceName == "" {
			continue
		}
		rawAmount := segment[amountIndex[0]:amountIndex[1]]
		items = append(items, Item{
			PhoneNumber: normalizePhone(rawLine[phoneIndex[0]:phoneIndex[1]]),
			ServiceName: serviceName, Quantity: 1, Unit: "UN",
			AmountCents: moneyCents(rawAmount), RawAmount: rawAmount, Confidence: .97,
		})
	}
	return items
}

func parseVivoAccountItems(lines []string) []Item {
	const (
		sectionIgnored = iota
		sectionAdditional
	)
	section := sectionIgnored
	items := make([]Item, 0, 8)
	for _, rawLine := range lines {
		line := normalizeVivoLine(rawLine)
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "SERVIÇOS CONTRATADOS"), strings.HasPrefix(upper, "UTILIZAÇÃO DENTRO"):
			section = sectionIgnored
			continue
		case strings.HasPrefix(upper, "UTILIZAÇÃO ACIMA DO CONTRATADO"),
			strings.HasPrefix(upper, "SERVIÇOS UTILIZADOS EM PERÍODOS ANTERIORES"),
			strings.HasPrefix(upper, "SERVIÇOS TELEFÔNICA BRASIL"),
			strings.HasPrefix(upper, "OUTROS LANÇAMENTOS"):
			section = sectionAdditional
			continue
		case strings.HasPrefix(upper, "MENSAGEM IMPORTANTE"), vivoSummaryStart.MatchString(line):
			section = sectionIgnored
		}
		if section != sectionAdditional || strings.HasPrefix(upper, "SUBTOTAL") || strings.HasPrefix(upper, "TOTAL") {
			continue
		}
		amountIndexes := vivoMoneyToken.FindAllStringIndex(rawLine, -1)
		if len(amountIndexes) == 0 {
			continue
		}
		amountIndex := amountIndexes[len(amountIndexes)-1]
		amount := moneyCents(rawLine[amountIndex[0]:amountIndex[1]])
		if amount == 0 {
			continue
		}
		nameColumn := strings.TrimSpace(rawLine[:amountIndex[0]])
		columns := vivoColumns.Split(nameColumn, -1)
		name := strings.TrimSpace(columns[0])
		if name == "" {
			continue
		}
		items = append(items, Item{
			ServiceName: name, Quantity: 1, Unit: "UN", AmountCents: amount,
			RawAmount: rawLine[amountIndex[0]:amountIndex[1]], Confidence: .88,
		})
	}
	return items
}

func finalizeVivoMetadata(document *Document, validateLines bool) {
	document.MissingFields = nil
	required := []struct {
		name  string
		value string
	}{
		{"contract_number", document.ContractNumber.Value},
		{"customer_name", document.CustomerName.Value},
		{"customer_document", document.CustomerDocument.Value},
		{"reference_month", document.ReferenceMonth.Value},
		{"reference_start", document.ReferenceStart.Value},
		{"reference_end", document.ReferenceEnd.Value},
		{"due_date", document.DueDate.Value},
	}
	for _, field := range required {
		if field.value == "" {
			document.MissingFields = append(document.MissingFields, field.name)
		}
	}
	if document.TotalAmountCents.Value == 0 {
		document.MissingFields = append(document.MissingFields, "total_amount")
	}
	if validateLines && document.LineCount == 0 {
		document.MissingFields = append(document.MissingFields, "lines")
	}
	document.OverallConfidence = .96
	if len(document.MissingFields) > 0 {
		document.OverallConfidence = .65
	}
}

func normalizeVivoLine(line string) string {
	return strings.TrimSpace(strings.ReplaceAll(line, "\f", ""))
}
