package invoice

import "testing"

func TestRegistryParsesVivoAndNormalizesFields(t *testing.T) {
	registry := NewRegistry(
		NewTelecomParser("VIVO", "VIVO", "TELEFÔNICA BRASIL"),
		NewTelecomParser("CLARO", "CLARO"),
	)
	document, err := registry.Parse(`TELEFÔNICA BRASIL - VIVO
Contrato: ABC-123
CNPJ 12.345.678/0001-90
Competência: 07/2026
(65) 99999-2222 Plano móvel 20 GB R$ 89,90
Vencimento 10/08/2026
Total R$ 89,90`)
	if err != nil {
		t.Fatal(err)
	}
	if document.OperatorCode.Value != "VIVO" || document.ContractNumber.Value != "ABC-123" {
		t.Fatalf("metadados inesperados: %#v", document)
	}
	if document.CustomerDocument.Value != "12345678000190" || document.ReferenceMonth.Value != "2026-07-01" {
		t.Fatalf("normalização inesperada: %#v", document)
	}
	if len(document.Items) != 1 || document.Items[0].PhoneNumber != "5565999992222" || document.Items[0].AmountCents != 8990 {
		t.Fatalf("item inesperado: %#v", document.Items)
	}
}

func TestRegistryRejectsUnknownOperator(t *testing.T) {
	registry := NewRegistry(NewTelecomParser("VIVO", "VIVO"))
	if _, err := registry.Parse("documento sem operadora"); err == nil {
		t.Fatal("esperava erro para operadora desconhecida")
	}
}
