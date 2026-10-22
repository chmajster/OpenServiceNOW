package assets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLifecycleAndExactFinancialValues(t *testing.T) {
	f := Fields{AssetTag: "LAPTOP-01", Name: "Laptop", OwnerID: 9007199254740993, SerialNumber: "serial-1", State: "IN_USE", PurchaseDate: "2026-01-01", WarrantyUntil: "2028-01-01", PurchaseCost: "9999999999.99", Currency: "PLN"}
	out, err := Normalize(f)
	if err != nil || out.AssetTag != "laptop-01" || out.SerialNumber != "SERIAL-1" || out.PurchaseCost != f.PurchaseCost {
		t.Fatal(out, err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"owner_id":"9007199254740993"`) {
		t.Fatal(string(raw))
	}
	for _, cost := range []string{"-1", "1e3", "10000000000", "1.001", "NaN"} {
		f.PurchaseCost = cost
		if _, err := Normalize(f); err == nil {
			t.Fatal(cost)
		}
	}
	f.PurchaseCost = "1"
	f.WarrantyUntil = "2025-12-31"
	if _, err := Normalize(f); err == nil {
		t.Fatal("warranty")
	}
	if Date("2026-02-29") {
		t.Fatal("invalid date")
	}
	if !Transition("ORDERED", "IN_STOCK") || Transition("DISPOSED", "IN_USE") || Transition("IN_USE", "ORDERED") {
		t.Fatal("lifecycle")
	}
}
