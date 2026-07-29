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

type Parser interface {
	Code() string
	Score(text string) int
	Parse(text string) (Document, error)
}

type StreamChunk struct {
	Metadata *Document
	Items    []Item
	Usage    []UsageRecord
}

type ParseStats struct {
	ItemCount  int
	UsageCount int
	LineCount  int
}

type StreamingParser interface {
	ParseStream(io.Reader, int, func(StreamChunk) error) (ParseStats, error)
}

type Registry struct{ parsers []Parser }

func NewRegistry(parsers ...Parser) Registry { return Registry{parsers: parsers} }

func (r Registry) Parse(text string) (Document, error) {
	bestScore := 0
	var best Parser
	for _, parser := range r.parsers {
		if score := parser.Score(text); score > bestScore {
			best, bestScore = parser, score
		}
	}
	if best == nil {
		return Document{}, fmt.Errorf("operadora não identificada")
	}
	return best.Parse(text)
}

func (r Registry) ParseStream(source io.ReadSeeker, chunkSize int, emit func(StreamChunk) error) (ParseStats, error) {
	parser, err := r.selectParser(source)
	if err != nil {
		return ParseStats{}, err
	}
	if streaming, ok := parser.(StreamingParser); ok {
		return streaming.ParseStream(source, chunkSize, emit)
	}

	text, err := io.ReadAll(source)
	if err != nil {
		return ParseStats{}, fmt.Errorf("ler texto extraído: %w", err)
	}
	document, err := parser.Parse(string(text))
	if err != nil {
		return ParseStats{}, err
	}
	metadata := document
	metadata.Items = nil
	metadata.UsageRecords = nil
	if err := emit(StreamChunk{Metadata: &metadata}); err != nil {
		return ParseStats{}, err
	}
	for start := 0; start < len(document.Items); start += chunkSize {
		end := min(start+chunkSize, len(document.Items))
		if err := emit(StreamChunk{Items: document.Items[start:end]}); err != nil {
			return ParseStats{}, err
		}
	}
	for start := 0; start < len(document.UsageRecords); start += chunkSize {
		end := min(start+chunkSize, len(document.UsageRecords))
		if err := emit(StreamChunk{Usage: document.UsageRecords[start:end]}); err != nil {
			return ParseStats{}, err
		}
	}
	return ParseStats{
		ItemCount:  len(document.Items),
		UsageCount: len(document.UsageRecords),
		LineCount:  document.LineCount,
	}, nil
}

func (r Registry) selectParser(source io.ReadSeeker) (Parser, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reposicionar texto extraído: %w", err)
	}
	scanner := bufio.NewScanner(io.LimitReader(source, 256<<10))
	scanner.Buffer(make([]byte, 64<<10), 256<<10)
	lines := make([]string, 0, 20)
	for scanner.Scan() && len(lines) < 20 {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("identificar operadora: %w", err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reposicionar texto extraído: %w", err)
	}

	sample := strings.Join(lines, "\n")
	bestScore := 0
	var best Parser
	for _, parser := range r.parsers {
		if score := parser.Score(sample); score > bestScore {
			best, bestScore = parser, score
		}
	}
	if best == nil {
		return nil, fmt.Errorf("operadora não identificada")
	}
	return best, nil
}

type TelecomParser struct {
	code    string
	markers []string
}

func NewTelecomParser(code string, markers ...string) TelecomParser {
	return TelecomParser{code: code, markers: markers}
}

func (p TelecomParser) Code() string { return p.code }

func (p TelecomParser) Score(text string) int {
	upper := strings.ToUpper(text)
	score := 0
	for _, marker := range p.markers {
		if strings.Contains(upper, strings.ToUpper(marker)) {
			score++
		}
	}
	return score
}

var (
	contractPattern  = regexp.MustCompile(`(?i)(?:n[úu]mero\s+(?:da\s+)?conta|conta|contrato)\s*[:\-]?\s*([A-Z0-9.\-/]+)`)
	cnpjPattern      = regexp.MustCompile(`\b\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}\b`)
	phonePattern     = regexp.MustCompile(`(?:\+?55\s*)?(?:\(?\d{2}\)?\s*)?9?\d{4}[-.\s]?\d{4}`)
	moneyPattern     = regexp.MustCompile(`(?i)(?:R\$\s*)?(\d{1,3}(?:\.\d{3})*,\d{2}|\d+,\d{2})`)
	datePattern      = regexp.MustCompile(`\b(\d{2})/(\d{2})/(\d{4})\b`)
	referencePattern = regexp.MustCompile(`(?i)(?:m[eê]s\s+(?:de\s+)?refer[eê]ncia|refer[eê]ncia|compet[eê]ncia)\s*[:\-]?\s*(\d{2})/(\d{4})`)
)

func (p TelecomParser) Parse(text string) (Document, error) {
	doc := Document{SchemaVersion: "telecom-invoice.v1"}
	doc.OperatorCode = Field[string]{Value: p.code, Confidence: 0.99}
	if match := contractPattern.FindStringSubmatch(text); len(match) > 1 {
		doc.ContractNumber = Field[string]{Value: normalizeIdentifier(match[1]), Confidence: .82}
	}
	if match := cnpjPattern.FindString(text); match != "" {
		doc.CustomerDocument = Field[string]{Value: digits(match), Confidence: .94}
	}
	if match := referencePattern.FindStringSubmatch(text); len(match) > 2 {
		doc.ReferenceMonth = Field[string]{Value: match[2] + "-" + match[1] + "-01", Confidence: .92}
	}
	dates := datePattern.FindAllString(text, 5)
	if len(dates) > 0 {
		doc.DueDate = Field[string]{Value: isoDate(dates[len(dates)-1]), Confidence: .55}
	}
	amounts := moneyPattern.FindAllStringSubmatch(text, -1)
	if len(amounts) > 0 {
		doc.TotalAmountCents = Field[int64]{Value: moneyCents(amounts[len(amounts)-1][1]), Confidence: .45}
	}
	doc.Items = parseCandidateItems(text)
	required := []struct {
		name  string
		value string
	}{
		{"contract_number", doc.ContractNumber.Value},
		{"customer_document", doc.CustomerDocument.Value},
		{"reference_month", doc.ReferenceMonth.Value},
	}
	for _, field := range required {
		if field.value == "" {
			doc.MissingFields = append(doc.MissingFields, field.name)
		}
	}
	doc.OverallConfidence = .85
	if len(doc.MissingFields) > 0 {
		doc.OverallConfidence = .55
	}
	return doc, nil
}

func parseCandidateItems(text string) []Item {
	lines := strings.Split(text, "\n")
	items := make([]Item, 0)
	for _, line := range lines {
		phone := phonePattern.FindString(line)
		amounts := moneyPattern.FindAllStringSubmatch(line, -1)
		if phone == "" || len(amounts) == 0 {
			continue
		}
		service := strings.TrimSpace(phonePattern.ReplaceAllString(line, ""))
		service = strings.TrimSpace(moneyPattern.ReplaceAllString(service, ""))
		if service == "" {
			service = "Serviço não identificado"
		}
		items = append(items, Item{
			Row: len(items) + 1, PhoneNumber: normalizePhone(phone), ServiceName: service,
			Quantity: 1, Unit: "UN", AmountCents: moneyCents(amounts[len(amounts)-1][1]), Confidence: .5,
		})
	}
	return items
}

func digits(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func normalizeIdentifier(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizePhone(value string) string {
	value = digits(value)
	if len(value) == 10 || len(value) == 11 {
		return "55" + value
	}
	return value
}

func moneyCents(value string) int64 {
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, ",", ".")
	parsed, _ := strconv.ParseFloat(value, 64)
	return int64(math.Round(parsed * 100))
}

func isoDate(value string) string {
	parsed, err := time.Parse("02/01/2006", value)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}
