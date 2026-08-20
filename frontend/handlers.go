package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type app struct {
	strategies *strategyStore
	api        *apiClient
	templates  *template.Template
	reportDir  string
	runs       map[string]runRecord
	runsMu     sync.RWMutex
}

type runRecord struct {
	ID           string
	StrategyName string
	CreatedAt    time.Time
}

type formView struct {
	Strategies []strategy
	Selected   strategy
	Request    backtestRequest
	Validation *validationResponse
	Error      string
}

type runView struct {
	Record runRecord
	Status *runStatus
	Error  string
}

func newApp(strategies *strategyStore, api *apiClient, templates *template.Template, reportDir string) *app {
	return &app{strategies: strategies, api: api, templates: templates, reportDir: reportDir, runs: make(map[string]runRecord)}
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("POST /validate", a.validate)
	mux.HandleFunc("POST /runs", a.startRun)
	mux.HandleFunc("GET /runs/{runID}", a.runStatus)
	mux.HandleFunc("GET /runs/{runID}/report", a.report)
	mux.HandleFunc("GET /reports/", a.localReport)
}

func (a *app) index(w http.ResponseWriter, _ *http.Request) {
	items := a.strategies.list()
	data := formView{Strategies: items, Selected: items[0], Request: defaultRequest()}
	a.render(w, "index.html", data)
}

func (a *app) validate(w http.ResponseWriter, req *http.Request) {
	view, ok := a.parseForm(w, req)
	if !ok {
		return
	}
	validation, err := a.api.validate(req.Context(), view.Request)
	if err != nil {
		view.Error = err.Error()
	} else {
		view.Validation = validation
	}
	a.render(w, "validation.html", view)
}

func (a *app) startRun(w http.ResponseWriter, req *http.Request) {
	view, ok := a.parseForm(w, req)
	if !ok {
		return
	}
	if _, err := a.api.validate(req.Context(), view.Request); err != nil {
		a.render(w, "validation.html", formView{Selected: view.Selected, Request: view.Request, Error: err.Error()})
		return
	}
	accepted, err := a.api.start(req.Context(), view.Request)
	if err != nil {
		a.render(w, "validation.html", formView{Selected: view.Selected, Request: view.Request, Error: err.Error()})
		return
	}
	record := runRecord{ID: accepted.RunID, StrategyName: view.Selected.DisplayName, CreatedAt: time.Now()}
	a.runsMu.Lock()
	a.runs[record.ID] = record
	a.runsMu.Unlock()
	a.render(w, "run.html", runView{Record: record, Status: &runStatus{RunID: record.ID, Status: accepted.Status}})
}

func (a *app) runStatus(w http.ResponseWriter, req *http.Request) {
	record, ok := a.knownRun(req.PathValue("runID"))
	if !ok {
		http.Error(w, "unknown run", http.StatusNotFound)
		return
	}
	status, err := a.api.status(req.Context(), record.ID)
	view := runView{Record: record, Status: status}
	if err != nil {
		view.Error = err.Error()
		view.Status = &runStatus{RunID: record.ID, Status: "running"}
	}
	a.render(w, "run.html", view)
}

func (a *app) report(w http.ResponseWriter, req *http.Request) {
	record, ok := a.knownRun(req.PathValue("runID"))
	if !ok {
		http.Error(w, "unknown run", http.StatusNotFound)
		return
	}
	status, err := a.api.status(req.Context(), record.ID)
	if err != nil || status.Status != "completed" {
		http.Error(w, "report is not ready", http.StatusConflict)
		return
	}
	path := status.ReportURL
	if path == "" {
		path = "/api/v1/backtests/runs/" + record.ID + "/report"
	}
	resp, err := a.api.report(req.Context(), record.ID, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", reportContentSecurityPolicy)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<20))
}

const reportContentSecurityPolicy = "default-src 'self'; img-src 'self' data: https:; script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; connect-src 'self'"

func (a *app) localReport(w http.ResponseWriter, req *http.Request) {
	relPath := strings.TrimPrefix(req.URL.Path, "/reports/")
	if relPath == "" {
		a.render(w, "reports.html", a.localReports())
		return
	}
	if filepath.Ext(relPath) != ".html" {
		http.NotFound(w, req)
		return
	}
	path, err := safeReportPath(a.reportDir, relPath)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	reportFile, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, req)
			return
		}
		http.Error(w, "open local report", http.StatusInternalServerError)
		return
	}
	defer reportFile.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", reportContentSecurityPolicy)
	_, _ = io.Copy(w, io.LimitReader(reportFile, 64<<20))
}

func safeReportPath(root, relPath string) (string, error) {
	if root == "" || filepath.IsAbs(relPath) {
		return "", fmt.Errorf("invalid report path")
	}
	path := filepath.Join(root, filepath.Clean(relPath))
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(root, resolved)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("report path escapes root")
	}
	return resolved, nil
}

func (a *app) localReports() []string {
	var reports []string
	_ = filepath.WalkDir(a.reportDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".html" {
			return nil
		}
		relPath, err := filepath.Rel(a.reportDir, path)
		if err == nil {
			reports = append(reports, filepath.ToSlash(relPath))
		}
		return nil
	})
	return reports
}

func (a *app) parseForm(w http.ResponseWriter, req *http.Request) (formView, bool) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return formView{}, false
	}
	selected, ok := a.strategies.get(req.FormValue("strategy_file"))
	if !ok {
		http.Error(w, "unknown strategy", http.StatusBadRequest)
		return formView{}, false
	}
	capital, err := strconv.ParseFloat(req.FormValue("capital"), 64)
	if err != nil || capital <= 0 {
		http.Error(w, "capital must be greater than zero", http.StatusBadRequest)
		return formView{}, false
	}
	payload := backtestRequest{
		Market:     req.FormValue("market"),
		Instrument: req.FormValue("instrument"),
		Asset:      strings.ToUpper(strings.TrimSpace(req.FormValue("asset"))),
		Symbols:    splitSymbols(req.FormValue("symbols")),
		Interval:   req.FormValue("interval"),
		From:       req.FormValue("from"),
		To:         req.FormValue("to"),
		Capital:    capital,
		DSL:        selected.Source,
		DSLParams:  make(map[string]any),
	}
	for key, values := range req.Form {
		if !strings.HasPrefix(key, "param.") || len(values) == 0 {
			continue
		}
		value := values[0]
		paramName := strings.TrimPrefix(key, "param.")
		switch req.FormValue("param_type." + paramName) {
		case "int":
			if parsed, parseErr := strconv.Atoi(value); parseErr == nil {
				payload.DSLParams[paramName] = parsed
			}
		case "float":
			if parsed, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
				payload.DSLParams[paramName] = parsed
			}
		case "bool":
			payload.DSLParams[paramName] = value == "true" || value == "on"
		default:
			payload.DSLParams[paramName] = value
		}
	}
	return formView{Selected: selected, Request: payload}, true
}

func (a *app) knownRun(runID string) (runRecord, bool) {
	a.runsMu.RLock()
	defer a.runsMu.RUnlock()
	record, ok := a.runs[runID]
	return record, ok
}

func (a *app) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, fmt.Sprintf("render %s: %v", name, err), http.StatusInternalServerError)
	}
}

func defaultRequest() backtestRequest {
	return backtestRequest{
		Market:     "us",
		Instrument: "auto",
		Asset:      "SPY",
		Symbols:    []string{"SPY"},
		Interval:   "1d",
		From:       "2023-01-01",
		To:         "2025-12-31",
		Capital:    100000,
	}
}

func splitSymbols(raw string) []string {
	parts := strings.FieldsFunc(raw, func(char rune) bool { return char == ',' || char == ' ' || char == '\n' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.ToUpper(strings.TrimSpace(part)); value != "" {
			result = append(result, value)
		}
	}
	return result
}
