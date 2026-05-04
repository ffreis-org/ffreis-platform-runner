package cmd

import (
	"io"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/ffreis/platform-runner/internal/runner"
	"github.com/ffreis/platform-runner/internal/ui"
)

type commandOutput struct {
	out       io.Writer
	presenter *ui.Presenter
}

func newCommandOutput(out io.Writer, presenter *ui.Presenter) *commandOutput {
	return &commandOutput{out: out, presenter: presenter}
}

func (o *commandOutput) Line(text string) {
	if text == "" {
		_, _ = io.WriteString(o.out, "\n")
		return
	}
	_, _ = io.WriteString(o.out, text+"\n")
}

func (o *commandOutput) Blank() {
	o.Line("")
}

func (o *commandOutput) Header(title, subtitle string) {
	if o.presenter != nil {
		o.Line(o.presenter.Header(title, subtitle))
		return
	}
	if subtitle == "" {
		o.Line(title)
		return
	}
	o.Line(title)
	o.Line(subtitle)
}

func (o *commandOutput) Summary(title string, parts ...string) {
	if o.presenter != nil {
		o.Line(o.presenter.Summary(title, parts...))
		return
	}
	if len(parts) == 0 {
		o.Line(title)
		return
	}
	o.Line(title + ": " + joinParts(parts))
}

func (o *commandOutput) Status(kind, label, detail string) {
	if o.presenter != nil {
		o.Line(o.presenter.Status(kind, label, detail))
		return
	}
	o.Line("[" + label + "] " + detail)
}

func (o *commandOutput) ErrStatus(kind, label, detail string) {
	message := "[" + label + "] " + detail
	if o.presenter != nil {
		message = o.presenter.Status(kind, label, detail)
	}
	_, _ = io.WriteString(o.out, message+"\n")
}

func (o *commandOutput) Table(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	stripped := make([]string, len(headers))
	for i, h := range headers {
		stripped[i] = stripANSI(h)
	}
	_, _ = io.WriteString(w, strings.Join(stripped, "\t")+"\n")
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = stripANSI(cell)
		}
		_, _ = io.WriteString(w, strings.Join(cells, "\t")+"\n")
	}
	return w.Flush()
}

func (o *commandOutput) Report(report *runner.RunReport) {
	o.Header(reportTitle(report.Action), "")
	o.Summary("Summary", report.Summary())

	for _, res := range report.Results {
		label, kind, detail := reportLine(res)
		target := res.Repo
		if res.Env != "" {
			target += " [" + res.Env + "]"
		}
		if detail != "" {
			target += ": " + detail
		}
		o.Status(kind, label, target)
	}
}

func reportTitle(action string) string {
	switch action {
	case "plan":
		return "Platform Runner Plan"
	case "apply":
		return "Platform Runner Apply"
	case "sync-template":
		return "Platform Runner Template Sync"
	case "validate":
		return "Platform Runner Validation"
	default:
		return "Platform Runner"
	}
}

func reportLine(res runner.RepoResult) (label, kind, detail string) {
	switch {
	case res.Status == runner.RepoStatusFailed:
		return "fail", "error", res.ErrMsg
	case res.Action == "plan" && res.HasChanges:
		return "warn", "warn", durationDetail(res.Duration)
	case res.Output != "":
		return "ok", "ok", appendDuration(res.Output, res.Duration)
	default:
		return "ok", "ok", durationDetail(res.Duration)
	}
}

func appendDuration(detail, duration string) string {
	if duration == "" {
		return detail
	}
	if detail == "" {
		return durationDetail(duration)
	}
	return detail + " (" + duration + ")"
}

func durationDetail(duration string) string {
	if duration == "" {
		return ""
	}
	return "completed in " + duration
}

func joinParts(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	switch len(filtered) {
	case 0:
		return ""
	case 1:
		return filtered[0]
	default:
		result := filtered[0]
		for _, part := range filtered[1:] {
			result += "  " + part
		}
		return result
	}
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}
