package invoice

type Field[T any] struct {
	Value      T       `json:"value"`
	Confidence float64 `json:"confidence"`
	Page       int     `json:"page,omitempty"`
}

type Document struct {
	SchemaVersion     string        `json:"schema_version"`
	OperatorCode      Field[string] `json:"operator_code"`
	ContractNumber    Field[string] `json:"contract_number"`
	CustomerName      Field[string] `json:"customer_name"`
	CustomerDocument  Field[string] `json:"customer_document"`
	ReferenceMonth    Field[string] `json:"reference_month"`
	ReferenceStart    Field[string] `json:"reference_start"`
	ReferenceEnd      Field[string] `json:"reference_end"`
	DueDate           Field[string] `json:"due_date"`
	TotalAmountCents  Field[int64]  `json:"total_amount_cents"`
	Barcode           Field[string] `json:"barcode"`
	CustomerCode      Field[string] `json:"customer_code"`
	LineCount         int           `json:"line_count"`
	Items             []Item        `json:"items"`
	UsageRecords      []UsageRecord `json:"usage_records"`
	MissingFields     []string      `json:"missing_fields"`
	OverallConfidence float64       `json:"overall_confidence"`
}

type Item struct {
	Row                   int     `json:"row"`
	PhoneNumber           string  `json:"phone_number,omitempty"`
	ServiceName           string  `json:"service_name"`
	Quantity              float64 `json:"quantity"`
	Unit                  string  `json:"unit"`
	AmountCents           int64   `json:"amount_cents"`
	ProRata               bool    `json:"pro_rata"`
	SourcePage            int     `json:"source_page,omitempty"`
	Confidence            float64 `json:"confidence"`
	AdditionalInformation string  `json:"additional_information,omitempty"`
	Included              bool    `json:"included,omitempty"`
	RawAmount             string  `json:"raw_amount,omitempty"`
	ParentServiceName     string  `json:"parent_service_name,omitempty"`
}

type UsageRecord struct {
	Row                      int     `json:"row"`
	PhoneNumber              string  `json:"phone_number"`
	Type                     string  `json:"type"`
	Category                 string  `json:"category,omitempty"`
	OccurredAt               string  `json:"occurred_at,omitempty"`
	DestinationNumber        string  `json:"destination_number,omitempty"`
	OriginDestination        string  `json:"origin_destination,omitempty"`
	EffectiveDurationSeconds int     `json:"effective_duration_seconds,omitempty"`
	BilledDurationSeconds    int     `json:"billed_duration_seconds,omitempty"`
	Quantity                 float64 `json:"quantity,omitempty"`
	Unit                     string  `json:"unit,omitempty"`
	TariffCents              int64   `json:"tariff_cents,omitempty"`
	TotalCents               int64   `json:"total_cents,omitempty"`
	ChargedCents             int64   `json:"charged_cents,omitempty"`
	DetailChargedCents       int64   `json:"detail_charged_cents,omitempty"`
	DifferenceCents          int64   `json:"difference_cents,omitempty"`
	DetailCount              int     `json:"detail_count,omitempty"`
	ReconciliationStatus     string  `json:"reconciliation_status,omitempty"`
	Confidence               float64 `json:"confidence"`
}

type Event struct {
	Type       string        `json:"type"`
	ImportID   string        `json:"import_id"`
	Sequence   int           `json:"sequence"`
	Metadata   *Document     `json:"metadata,omitempty"`
	Items      []Item        `json:"items,omitempty"`
	Usage      []UsageRecord `json:"usage,omitempty"`
	ItemCount  int           `json:"item_count,omitempty"`
	UsageCount int           `json:"usage_count,omitempty"`
	LineCount  int           `json:"line_count,omitempty"`
	ErrorCode  string        `json:"error_code,omitempty"`
	Message    string        `json:"message,omitempty"`
}
