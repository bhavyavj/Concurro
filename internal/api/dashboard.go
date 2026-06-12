package api

import (
	"html/template"
	"log/slog"
	"net/http"

	_ "embed"
)

//go:embed templates/dashboard.html
var dashboardHTML string

var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardHTML))

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTmpl.Execute(w, nil); err != nil {
		slog.Error("template execute error", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
