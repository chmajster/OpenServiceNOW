package main

import (
	"reflect"
	"testing"
)

func TestPortfolioDataTablesForDowngrade(t *testing.T) {
	tests := []struct {
		name    string
		version uint
		command string
		want    []string
	}{
		{name: "permission rollback does not drop portfolio data", version: 104, command: "down"},
		{name: "one step checks the table being dropped", version: 102, command: "down", want: []string{"service_portfolio_offer_audience_groups"}},
		{name: "full rollback checks every installed portfolio table", version: 104, command: "down-all", want: []string{
			"service_portfolio_org_refs", "service_portfolio_items", "service_portfolio_relations",
			"service_portfolio_component_availability", "service_portfolio_offers", "service_portfolio_offer_audience_groups",
		}},
		{name: "partial installation checks only installed portfolio tables", version: 99, command: "down-all", want: []string{
			"service_portfolio_org_refs", "service_portfolio_items", "service_portfolio_relations",
		}},
		{name: "pre-module rollback has no portfolio tables", version: 96, command: "down-all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portfolioDataTablesForDowngrade(tt.version, tt.command); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("portfolioDataTablesForDowngrade(%d, %q) = %v, want %v", tt.version, tt.command, got, tt.want)
			}
		})
	}
}
