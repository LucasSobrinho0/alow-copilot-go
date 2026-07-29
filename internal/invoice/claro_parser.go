package invoice

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	claroOperatorPattern  = regexp.MustCompile(`(?i)raz[aã]o\s+social:\s*claro\s+s\s*/?\s*a`)
	claroAccountPattern   = regexp.MustCompile(`(?i)n[º°o]\s*da\s*conta:\s*([0-9]+)`)
	claroCustomerPattern  = regexp.MustCompile(`(?i)n[º°o]\s*do\s*cliente:\s*([0-9]+)`)
	claroDocumentPattern  = regexp.MustCompile(`(?i)CPF/CNPJ\s*:?\s*(\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2})`)
	claroPeriodPattern    = regexp.MustCompile(`(?is)per[ií]odo\s+de\s+uso\s+de\s+(\d{2}/\d{2}/\d{4})\s+a\s+(\d{2}/\d{2}/\d{4})`)
	claroLinePattern      = regexp.MustCompile(`(?i)DETALHAMENTO\s+DE\s+LIGA[CÇ][OÕ]ES\s+E\s+SERVI[CÇ]OS\s+DO\s+CELULAR\s*\((\d{2})\)\s*(\d{4,5})\s*(\d{4})`)
	claroBarcodePattern   = regexp.MustCompile(`\b(\d{11}-\d)\s+(\d{11}-\d)\s+(\d{11}-\d)\s+(\d{11}-\d)\b`)
	claroServicePattern   = regexp.MustCompile(`^(.*?)\s+(?:R\$\s*)?(-|-?\d{1,3}(?:\.\d{3})*,\d{2}|-?\d+,\d{2})\s*$`)
	claroCallPattern      = regexp.MustCompile(`^(\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(.+?)\s+(\d[\d .()-]{7,18})\s+(\d{2}:\d{2}:\d{2})\s+(\d{2}:\d{2}:\d{2})(.*)$`)
	claroCallTotalPattern = regexp.MustCompile(`(?i)^TOTAL\s+(\d{2}:\d{2}:\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(-?\d+(?:\.\d{3})*,\d{2})\s+(-?\d+(?:\.\d{3})*,\d{2})\s+(-?\d+(?:\.\d{3})*,\d{2})\s*$`)
	claroSMSPattern       = regexp.MustCompile(`^(\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(.+?)\s+(\d[\d .()-]{7,18})\s+(-?\d+(?:\.\d{3})*,\d{2})\s+(-?\d+(?:\.\d{3})*,\d{2})\s*$`)
	claroMoneyToken       = regexp.MustCompile(`-?\d{1,3}(?:\.\d{3})*,\d{2}|-?\d+,\d{2}`)
	claroMonthYear        = regexp.MustCompile(`(?i)\b(janeiro|fevereiro|mar[cç]o|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro)\s+(\d{4})\b`)
	claroNumericMonth     = regexp.MustCompile(`\b(0[1-9]|1[0-2])/(\d{4})\b`)
	claroDate             = regexp.MustCompile(`\b\d{2}/\d{2}/\d{4}\b`)
	claroPeriodLine       = regexp.MustCompile(`(?i)^DE\s+\d{2}/\d{2}/\d{4}\s+A\s+\d{2}/\d{2}/\d{4}$`)
	claroShortDate        = regexp.MustCompile(`^\d{2}/\d{2}$`)
	claroPostalCode       = regexp.MustCompile(`^\d{5}-?\d{3}\b`)
	claroColumns          = regexp.MustCompile(`\s{2,}`)
	claroAddressStart     = regexp.MustCompile(`(?i)^(?:R|RUA|AV|AVENIDA|ROD|RODOVIA|ESTRADA|TRAVESSA)\s+`)
)

type claroLineBlock struct {
	phoneNumber string
	lines       []string
}

func (ClaroParser) Code() string { return "CLARO" }

func (ClaroParser) Score(text string) int {
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	for _, line := range lines {
		if claroOperatorPattern.MatchString(normalizeClaroLine(line)) {
			return 100
		}
	}
	return 0
}

func (ClaroParser) Parse(text string) (Document, error) {
	lines := strings.Split(text, "\n")
	preambleEnd := len(lines)
	for index, line := range lines {
		if claroLinePattern.MatchString(normalizeClaroLine(line)) {
			preambleEnd = index
			break
		}
	}
	preamble := lines[:preambleEnd]
	document := parseClaroHeader(preamble)
	blocks := splitClaroLineBlocks(lines)
	document.LineCount = len(blocks)

	for _, block := range blocks {
		services := parseClaroServices(block)
		for index := range services {
			services[index].Row = len(document.Items) + 1
			document.Items = append(document.Items, services[index])
		}
		usage := parseClaroUsage(block, document.ReferenceStart.Value, document.ReferenceEnd.Value)
		for index := range usage {
			usage[index].Row = len(document.UsageRecords) + 1
			document.UsageRecords = append(document.UsageRecords, usage[index])
		}
	}

	finalizeClaroMetadata(&document, true)
	return document, nil
}

func (ClaroParser) ParseStream(reader io.Reader, chunkSize int, emit func(StreamChunk) error) (ParseStats, error) {
	if chunkSize <= 0 {
		chunkSize = 250
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	preamble := make([]string, 0, 80)
	var current *claroLineBlock
	metadataEmitted := false
	var header Document
	stats := ParseStats{}
	nextItemRow := 1
	nextUsageRow := 1

	emitMetadata := func() error {
		if metadataEmitted {
			return nil
		}
		header = parseClaroHeader(preamble)
		finalizeClaroMetadata(&header, false)
		metadataEmitted = true
		return emit(StreamChunk{Metadata: &header})
	}
	emitBlock := func(block *claroLineBlock) error {
		if block == nil {
			return nil
		}
		stats.LineCount++
		items := parseClaroServices(*block)
		for index := range items {
			items[index].Row = nextItemRow
			nextItemRow++
		}
		for start := 0; start < len(items); start += chunkSize {
			end := min(start+chunkSize, len(items))
			if err := emit(StreamChunk{Items: items[start:end]}); err != nil {
				return err
			}
		}
		stats.ItemCount += len(items)

		usage := parseClaroUsage(*block, header.ReferenceStart.Value, header.ReferenceEnd.Value)
		for index := range usage {
			usage[index].Row = nextUsageRow
			nextUsageRow++
		}
		for start := 0; start < len(usage); start += chunkSize {
			end := min(start+chunkSize, len(usage))
			if err := emit(StreamChunk{Usage: usage[start:end]}); err != nil {
				return err
			}
		}
		stats.UsageCount += len(usage)
		return nil
	}

	for scanner.Scan() {
		rawLine := scanner.Text()
		line := normalizeClaroLine(rawLine)
		if match := claroLinePattern.FindStringSubmatch(line); len(match) == 4 {
			if err := emitMetadata(); err != nil {
				return stats, err
			}
			if err := emitBlock(current); err != nil {
				return stats, err
			}
			current = &claroLineBlock{
				phoneNumber: normalizePhone(match[1] + match[2] + match[3]),
				lines:       []string{rawLine},
			}
			continue
		}
		if current == nil {
			if !metadataEmitted {
				preamble = append(preamble, rawLine)
			}
			continue
		}
		current.lines = append(current.lines, rawLine)
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("ler fatura Claro: %w", err)
	}
	if err := emitMetadata(); err != nil {
		return stats, err
	}
	if err := emitBlock(current); err != nil {
		return stats, err
	}
	if stats.LineCount == 0 {
		return stats, fmt.Errorf("nenhum bloco de linha Claro foi identificado")
	}
	return stats, nil
}

func finalizeClaroMetadata(document *Document, validateLines bool) {
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
	if validateLines && document.LineCount == 0 {
		document.MissingFields = append(document.MissingFields, "lines")
	}
	document.OverallConfidence = .96
	if len(document.MissingFields) > 0 {
		document.OverallConfidence = .65
	}
}

func parseClaroHeader(lines []string) Document {
	document := Document{
		SchemaVersion: "telecom-invoice.v1",
		OperatorCode:  Field[string]{Value: "CLARO", Confidence: .99},
	}
	joined := strings.Join(lines, "\n")
	if match := claroAccountPattern.FindStringSubmatch(joined); len(match) > 1 {
		document.ContractNumber = Field[string]{Value: match[1], Confidence: .99}
	}
	if match := claroCustomerPattern.FindStringSubmatch(joined); len(match) > 1 {
		document.CustomerCode = Field[string]{Value: match[1], Confidence: .99}
	}
	if match := claroDocumentPattern.FindStringSubmatch(joined); len(match) > 1 {
		document.CustomerDocument = Field[string]{Value: digits(match[1]), Confidence: .99}
	}
	if match := claroPeriodPattern.FindStringSubmatch(joined); len(match) > 2 {
		document.ReferenceStart = Field[string]{Value: isoDate(match[1]), Confidence: .99}
		document.ReferenceEnd = Field[string]{Value: isoDate(match[2]), Confidence: .99}
	}
	document.CustomerName = Field[string]{Value: findClaroCustomerName(lines), Confidence: .94}
	document.DueDate = Field[string]{Value: findClaroDueDate(lines), Confidence: .98}
	document.ReferenceMonth = Field[string]{Value: findClaroReferenceMonth(lines, document.ReferenceEnd.Value), Confidence: .95}
	document.TotalAmountCents = Field[int64]{Value: findClaroTotal(lines), Confidence: .98}
	if match := claroBarcodePattern.FindStringSubmatch(joined); len(match) == 5 {
		document.Barcode = Field[string]{Value: digits(strings.Join(match[1:], "")), Confidence: .99}
	}
	return document
}

func findClaroCustomerName(lines []string) string {
	limit := min(20, len(lines))
	rightColumn := findClaroRightColumn(lines[:limit])
	parts := make([]string, 0, 2)
	for _, rawLine := range lines[:limit] {
		line := strings.TrimRightFunc(strings.ReplaceAll(rawLine, "\f", ""), unicode.IsSpace)
		lineColumn := leadingSpaces(line)
		if strings.TrimSpace(line) == "" ||
			(rightColumn > 0 && lineColumn >= rightColumn-2) ||
			(rightColumn == 0 && lineColumn >= 80) {
			continue
		}
		leftColumn := line
		if rightColumn > 0 && len(leftColumn) > rightColumn {
			leftColumn = leftColumn[:rightColumn]
		}
		candidate := strings.TrimSpace(leftColumn)
		if candidate == "" {
			continue
		}
		if claroAddressStart.MatchString(candidate) || claroPostalCode.MatchString(candidate) {
			if len(parts) > 0 {
				break
			}
			continue
		}
		upper := strings.ToUpper(candidate)
		if strings.Contains(upper, "PERÍODO") || strings.Contains(upper, "VENCIMENTO") ||
			strings.Contains(upper, "Nº DA CONTA") || strings.Contains(upper, "CPF/CNPJ") ||
			strings.Contains(upper, "RAZÃO SOCIAL") || claroPeriodLine.MatchString(candidate) {
			continue
		}
		parts = append(parts, candidate)
	}
	return strings.Join(parts, " ")
}

func findClaroRightColumn(lines []string) int {
	rightColumn := 0
	for _, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if !strings.Contains(upper, "Nº DA CONTA") && !strings.Contains(upper, "RAZÃO SOCIAL") &&
			!strings.Contains(upper, "PERÍODO DE USO") {
			continue
		}
		column := leadingSpaces(line)
		if column > 0 && (rightColumn == 0 || column < rightColumn) {
			rightColumn = column
		}
	}
	return rightColumn
}

func findClaroDueDate(lines []string) string {
	for index, rawLine := range lines {
		line := normalizeClaroLine(rawLine)
		if !claroAccountPattern.MatchString(line) && !strings.Contains(strings.ToUpper(line), "VENCIMENTO") {
			continue
		}
		for offset := 0; offset <= 2 && index+offset < len(lines); offset++ {
			dates := claroDate.FindAllString(lines[index+offset], -1)
			if len(dates) > 0 {
				return isoDate(dates[len(dates)-1])
			}
		}
	}
	return ""
}

func findClaroReferenceMonth(lines []string, referenceEnd string) string {
	for index, rawLine := range lines {
		upper := strings.ToUpper(normalizeClaroLine(rawLine))
		if !strings.Contains(upper, "MÊS REFERÊNCIA") && !strings.Contains(upper, "MES REFERENCIA") &&
			!strings.Contains(upper, "COMPETÊNCIA") && !strings.Contains(upper, "REFERÊNCIA") {
			continue
		}
		windowEnd := min(index+3, len(lines))
		window := strings.Join(lines[index:windowEnd], "\n")
		if match := claroMonthYear.FindStringSubmatch(window); len(match) > 2 {
			if month := portugueseMonth(match[1]); month > 0 {
				return fmt.Sprintf("%s-%02d-01", match[2], month)
			}
		}
		matches := claroNumericMonth.FindAllStringSubmatch(window, -1)
		if len(matches) > 0 {
			match := matches[len(matches)-1]
			return match[2] + "-" + match[1] + "-01"
		}
	}
	if len(referenceEnd) >= 7 {
		return referenceEnd[:7] + "-01"
	}
	return ""
}

func findClaroTotal(lines []string) int64 {
	for _, rawLine := range lines {
		line := normalizeClaroLine(rawLine)
		if !strings.Contains(strings.ToUpper(line), "TOTAL A PAGAR") {
			continue
		}
		amounts := claroMoneyToken.FindAllString(line, -1)
		if len(amounts) > 0 {
			return moneyCents(amounts[len(amounts)-1])
		}
	}
	return 0
}

func splitClaroLineBlocks(lines []string) []claroLineBlock {
	blocks := make([]claroLineBlock, 0)
	var current *claroLineBlock
	for _, rawLine := range lines {
		line := normalizeClaroLine(rawLine)
		if match := claroLinePattern.FindStringSubmatch(line); len(match) == 4 {
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &claroLineBlock{
				phoneNumber: normalizePhone(match[1] + match[2] + match[3]),
				lines:       []string{rawLine},
			}
			continue
		}
		if current == nil {
			continue
		}
		current.lines = append(current.lines, rawLine)
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

func parseClaroServices(block claroLineBlock) []Item {
	items := make([]Item, 0)
	inServices := false
	lastRootService := ""
	for _, rawLine := range block.lines {
		line := strings.TrimSpace(normalizeClaroLine(rawLine))
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "MENSALIDADES E PACOTES PROMOCIONAIS") {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		if strings.HasPrefix(upper, "TOTAL") || isClaroUsageSection(upper) {
			break
		}
		if line == "" || strings.HasPrefix(upper, "DESCRIÇÃO") || strings.HasPrefix(upper, "DESCRICAO") {
			continue
		}
		match := claroServicePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		rawAmount := match[2]
		if name == "" {
			continue
		}
		included := rawAmount == "-"
		parent := ""
		if included {
			parent = lastRootService
		} else {
			lastRootService = name
		}
		amount := int64(0)
		if !included {
			amount = moneyCents(rawAmount)
		}
		items = append(items, Item{
			PhoneNumber: block.phoneNumber, ServiceName: name, Quantity: 1, Unit: "UN",
			AmountCents: amount, ProRata: isClaroProRata(name), Confidence: .97,
			Included: included, RawAmount: rawAmount, ParentServiceName: parent,
		})
	}
	return items
}

func parseClaroUsage(block claroLineBlock, referenceStart, referenceEnd string) []UsageRecord {
	records := make([]UsageRecord, 0)
	state := ""
	callCategory := ""
	for _, rawLine := range block.lines {
		line := strings.TrimSpace(normalizeClaroLine(rawLine))
		upper := strings.ToUpper(line)
		if line == "" {
			continue
		}
		switch {
		case strings.Contains(upper, "DETALHES DA INTERNET MÓVEL"):
			state = "DATA_DETAIL"
			continue
		case strings.HasPrefix(upper, "INTERNET (MB)"):
			state = "DATA_SUMMARY"
			continue
		case upper == "TORPEDOS" || strings.HasPrefix(upper, "TORPEDOS ("):
			state = "SMS_SUMMARY"
			continue
		case strings.Contains(upper, "ENVIO DE SMS"):
			state = "SMS_DETAIL"
			continue
		case strings.Contains(upper, "DATA HORA") && strings.Contains(upper, "DUR. EFETIVA"):
			state = "CALL"
			continue
		}

		if isClaroCallCategory(upper) {
			callCategory = line
			continue
		}
		if record, ok := parseClaroCallSummary(line, block.phoneNumber, callCategory); ok {
			records = append(records, record)
			continue
		}
		if record, ok := parseClaroCall(line, block.phoneNumber, callCategory, referenceStart, referenceEnd); ok {
			records = append(records, record)
			continue
		}
		switch state {
		case "DATA_SUMMARY":
			if record, ok := parseClaroSubtotal(line, block.phoneNumber, "DATA_TOTAL", "Internet móvel", "MB"); ok {
				records = append(records, record)
				continue
			}
			if record, ok := parseClaroDataSummary(line, block.phoneNumber); ok {
				records = append(records, record)
			}
		case "DATA_DETAIL":
			if record, ok := parseClaroDataDetail(line, block.phoneNumber, referenceStart, referenceEnd); ok {
				records = append(records, record)
			}
		case "SMS_SUMMARY":
			if record, ok := parseClaroSubtotal(line, block.phoneNumber, "SMS_TOTAL", "Torpedos", "UNIT"); ok {
				records = append(records, record)
				continue
			}
			if record, ok := parseClaroSMSSummary(line, block.phoneNumber); ok {
				records = append(records, record)
			}
		case "SMS_DETAIL":
			if record, ok := parseClaroSMSDetail(line, block.phoneNumber, referenceStart, referenceEnd); ok {
				records = append(records, record)
			}
		}
	}
	return reconcileClaroUsage(records)
}

func parseClaroCallSummary(line, phoneNumber, category string) (UsageRecord, bool) {
	if category == "" {
		return UsageRecord{}, false
	}
	match := claroCallTotalPattern.FindStringSubmatch(line)
	if len(match) != 6 {
		return UsageRecord{}, false
	}
	return UsageRecord{
		PhoneNumber:              phoneNumber,
		Type:                     "CALL_SUMMARY",
		Category:                 category,
		EffectiveDurationSeconds: durationSeconds(match[1]),
		BilledDurationSeconds:    durationSeconds(match[2]),
		TariffCents:              moneyCents(match[3]),
		TotalCents:               moneyCents(match[4]),
		ChargedCents:             moneyCents(match[5]),
		Unit:                     "SECOND",
		Confidence:               .98,
	}, true
}

func parseClaroSubtotal(line, phoneNumber, recordType, category, unit string) (UsageRecord, bool) {
	columns := splitClaroColumns(line)
	if len(columns) < 2 || !strings.EqualFold(columns[0], "Subtotal") {
		return UsageRecord{}, false
	}
	quantity, _ := parseLocaleDecimal(columns[1])
	amounts := claroMoneyToken.FindAllString(strings.Join(columns[2:], " "), -1)
	_, total, charged := trailingAmounts(amounts)
	return UsageRecord{
		PhoneNumber:  phoneNumber,
		Type:         recordType,
		Category:     category,
		Quantity:     quantity,
		Unit:         unit,
		TotalCents:   total,
		ChargedCents: charged,
		Confidence:   .98,
	}, true
}

func reconcileClaroUsage(records []UsageRecord) []UsageRecord {
	for index := range records {
		summary := &records[index]
		detailType := ""
		switch summary.Type {
		case "CALL_SUMMARY":
			detailType = "CALL"
		case "DATA_TOTAL":
			detailType = "DATA"
		case "SMS_TOTAL":
			detailType = "SMS"
		default:
			continue
		}
		for detailIndex := range records {
			detail := records[detailIndex]
			if detail.Type != detailType {
				continue
			}
			if summary.Type == "CALL_SUMMARY" && detail.Category != summary.Category {
				continue
			}
			summary.DetailCount++
			summary.DetailChargedCents += detail.ChargedCents
		}
		summary.DifferenceCents = summary.DetailChargedCents - summary.ChargedCents
		switch {
		case summary.DetailCount == 0:
			summary.ReconciliationStatus = "REVIEW_REQUIRED"
		case summary.DifferenceCents == 0:
			summary.ReconciliationStatus = "MATCHED"
		default:
			summary.ReconciliationStatus = "DIVERGENT"
		}
	}
	return records
}

func parseClaroCall(line, phoneNumber, category, referenceStart, referenceEnd string) (UsageRecord, bool) {
	match := claroCallPattern.FindStringSubmatch(line)
	if len(match) != 8 {
		return UsageRecord{}, false
	}
	amounts := claroMoneyToken.FindAllString(match[7], -1)
	tariff, total, charged := trailingAmounts(amounts)
	return UsageRecord{
		PhoneNumber: phoneNumber, Type: "CALL", Category: category,
		OccurredAt:        inferClaroDateTime(match[1], match[2], referenceStart, referenceEnd),
		OriginDestination: strings.TrimSpace(match[3]), DestinationNumber: normalizePhone(match[4]),
		EffectiveDurationSeconds: durationSeconds(match[5]), BilledDurationSeconds: durationSeconds(match[6]),
		TariffCents: tariff, TotalCents: total, ChargedCents: charged, Unit: "SECOND", Confidence: .94,
	}, true
}

func parseClaroDataSummary(line, phoneNumber string) (UsageRecord, bool) {
	columns := splitClaroColumns(line)
	if len(columns) < 2 || strings.EqualFold(columns[0], "Serviço") || strings.EqualFold(columns[0], "Subtotal") {
		return UsageRecord{}, false
	}
	quantity, ok := parseLocaleDecimal(columns[1])
	if !ok {
		return UsageRecord{}, false
	}
	amounts := make([]string, 0, len(columns)-2)
	for _, column := range columns[2:] {
		if claroMoneyToken.MatchString(column) {
			amounts = append(amounts, claroMoneyToken.FindString(column))
		}
	}
	tariff, total, charged := trailingAmounts(amounts)
	return UsageRecord{
		PhoneNumber: phoneNumber, Type: "DATA_SUMMARY", Category: columns[0],
		Quantity: quantity, Unit: "MB", TariffCents: tariff, TotalCents: total,
		ChargedCents: charged, Confidence: .96,
	}, true
}

func parseClaroDataDetail(line, phoneNumber, referenceStart, referenceEnd string) (UsageRecord, bool) {
	columns := splitClaroColumns(line)
	if len(columns) < 2 || !claroShortDate.MatchString(columns[0]) {
		return UsageRecord{}, false
	}
	quantity, ok := parseLocaleDecimal(columns[1])
	if !ok {
		return UsageRecord{}, false
	}
	amounts := make([]string, 0, len(columns)-2)
	for _, column := range columns[2:] {
		if claroMoneyToken.MatchString(column) {
			amounts = append(amounts, claroMoneyToken.FindString(column))
		}
	}
	tariff, total, charged := trailingAmounts(amounts)
	return UsageRecord{
		PhoneNumber: phoneNumber, Type: "DATA", Category: "Internet móvel",
		OccurredAt: inferClaroDateTime(columns[0], "00:00:00", referenceStart, referenceEnd),
		Quantity:   quantity, Unit: "MB", TariffCents: tariff, TotalCents: total,
		ChargedCents: charged, Confidence: .96,
	}, true
}

func parseClaroSMSSummary(line, phoneNumber string) (UsageRecord, bool) {
	columns := splitClaroColumns(line)
	if len(columns) < 2 || strings.EqualFold(columns[0], "Descrição") || strings.EqualFold(columns[0], "Subtotal") {
		return UsageRecord{}, false
	}
	quantity, ok := parseLocaleDecimal(columns[1])
	if !ok {
		return UsageRecord{}, false
	}
	amounts := make([]string, 0, len(columns)-2)
	for _, column := range columns[2:] {
		if claroMoneyToken.MatchString(column) {
			amounts = append(amounts, claroMoneyToken.FindString(column))
		}
	}
	tariff, total, charged := trailingAmounts(amounts)
	return UsageRecord{
		PhoneNumber: phoneNumber, Type: "SMS_SUMMARY", Category: columns[0],
		Quantity: quantity, Unit: "UNIT", TariffCents: tariff, TotalCents: total,
		ChargedCents: charged, Confidence: .94,
	}, true
}

func parseClaroSMSDetail(line, phoneNumber, referenceStart, referenceEnd string) (UsageRecord, bool) {
	match := claroSMSPattern.FindStringSubmatch(line)
	if len(match) != 7 {
		return UsageRecord{}, false
	}
	return UsageRecord{
		PhoneNumber: phoneNumber, Type: "SMS", Category: "Envio de SMS",
		OccurredAt:        inferClaroDateTime(match[1], match[2], referenceStart, referenceEnd),
		OriginDestination: strings.TrimSpace(match[3]), DestinationNumber: normalizePhone(match[4]),
		TariffCents: moneyCents(match[5]), ChargedCents: moneyCents(match[6]),
		Quantity: 1, Unit: "UNIT", Confidence: .94,
	}, true
}

func normalizeClaroLine(value string) string {
	value = strings.ReplaceAll(value, "\f", "")
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\ufeff", "")
	return strings.TrimRightFunc(value, unicode.IsSpace)
}

func splitClaroColumns(value string) []string {
	rawColumns := claroColumns.Split(strings.TrimSpace(value), -1)
	columns := make([]string, 0, len(rawColumns))
	for _, column := range rawColumns {
		if column = strings.TrimSpace(column); column != "" {
			columns = append(columns, column)
		}
	}
	return columns
}

func isClaroUsageSection(upper string) bool {
	return strings.Contains(upper, "LIGAÇÕES") || strings.Contains(upper, "LIGACOES") ||
		strings.Contains(upper, "INTERURBANAS") || strings.HasPrefix(upper, "SERVIÇOS (TORPEDOS") ||
		strings.HasPrefix(upper, "SERVICOS (TORPEDOS") || strings.HasPrefix(upper, "INTERNET (MB)") ||
		upper == "TORPEDOS"
}

func isClaroCallCategory(upper string) bool {
	if strings.Contains(upper, "CONTINUAÇÃO") || strings.Contains(upper, "CONTINUACAO") {
		return false
	}
	return strings.HasPrefix(upper, "LIGAÇÕES ") || strings.HasPrefix(upper, "LIGACOES ") ||
		strings.HasPrefix(upper, "INTERURBANAS")
}

func isClaroProRata(serviceName string) bool {
	upper := strings.ToUpper(serviceName)
	return strings.Contains(upper, " - DE ") && strings.Contains(upper, " A ") &&
		len(claroDate.FindAllString(serviceName, -1)) >= 2
}

func leadingSpaces(value string) int {
	return len(value) - len(strings.TrimLeft(value, " "))
}

func portugueseMonth(value string) int {
	normalized := strings.ToLower(value)
	normalized = strings.NewReplacer("ç", "c", "ã", "a").Replace(normalized)
	months := map[string]int{
		"janeiro": 1, "fevereiro": 2, "marco": 3, "abril": 4, "maio": 5, "junho": 6,
		"julho": 7, "agosto": 8, "setembro": 9, "outubro": 10, "novembro": 11, "dezembro": 12,
	}
	return months[normalized]
}

func parseLocaleDecimal(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, ",", ".")
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func trailingAmounts(amounts []string) (int64, int64, int64) {
	if len(amounts) == 0 {
		return 0, 0, 0
	}
	charged := moneyCents(amounts[len(amounts)-1])
	if len(amounts) == 1 {
		return 0, 0, charged
	}
	total := moneyCents(amounts[len(amounts)-2])
	if len(amounts) == 2 {
		return 0, total, charged
	}
	return moneyCents(amounts[len(amounts)-3]), total, charged
}

func durationSeconds(value string) int {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0
	}
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	seconds, _ := strconv.Atoi(parts[2])
	return hours*3600 + minutes*60 + seconds
}

func inferClaroDateTime(dayMonth, clock, referenceStart, referenceEnd string) string {
	end, err := time.Parse("2006-01-02", referenceEnd)
	if err != nil {
		return ""
	}
	start, err := time.Parse("2006-01-02", referenceStart)
	if err != nil {
		start = end
	}
	middle := start.Add(end.Sub(start) / 2)
	bestDistance := time.Duration(math.MaxInt64)
	var best time.Time
	for _, year := range []int{end.Year() - 1, end.Year(), end.Year() + 1} {
		candidate, err := time.Parse("02/01/2006 15:04:05", fmt.Sprintf("%s/%d %s", dayMonth, year, clock))
		if err != nil {
			continue
		}
		distance := candidate.Sub(middle)
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if best.IsZero() {
		return ""
	}
	return best.Format("2006-01-02T15:04:05")
}
