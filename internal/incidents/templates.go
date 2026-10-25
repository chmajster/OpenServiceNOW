package incidents

import "github.com/chmajster/OpenServiceNOW/internal/metadata"

func activeTemplates(c metadata.Configuration, a Actor) []metadata.IncidentTemplate {
	out := []metadata.IncidentTemplate{}
	for _, t := range c.IncidentProcess.Templates {
		if !t.Retired && (t.ServiceID == "" || a.Permissions["service_portfolio.read"] || a.Permissions["service_portfolio.manage"]) {
			out = append(out, t)
		}
	}
	return out
}
