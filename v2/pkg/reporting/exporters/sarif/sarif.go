package sarif

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/owenrumney/go-sarif/sarif"
	"github.com/pkg/errors"
	"github.com/projectdiscovery/nuclei/v2/pkg/output"
	"github.com/projectdiscovery/nuclei/v2/pkg/reporting/format"
)

// Exporter is an exporter for nuclei sarif output format.
type Exporter struct {
	sarif *sarif.Report
	run   *sarif.Run
	mutex *sync.Mutex

	home     string
	tempFile string
	options  *Options
}

// Options contains the configuration options for sarif exporter client
type Options struct {
	// File is the file to export found sarif result to
	File string `yaml:"file"`
}

// New creates a new disk exporter integration client based on options.
func New(options *Options) (*Exporter, error) {
	report, err := sarif.New(sarif.Version210)
	if err != nil {
		return nil, errors.Wrap(err, "could not create sarif exporter")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(err, "could not get home dir")
	}
	templatePath := path.Join(home, "nuclei-templates")

	run := sarif.NewRun("nuclei", "https://github.com/projectdiscovery/nuclei")
	return &Exporter{options: options, home: templatePath, sarif: report, run: run, mutex: &sync.Mutex{}}, nil
}

// Export exports a passed result event to sarif structure
func (i *Exporter) Export(event *output.ResultEvent) error {
	templatePath := strings.TrimPrefix(event.TemplatePath, i.home)

	h := sha1.New()
	h.Write([]byte(event.Host))
	templateID := event.TemplateID + "-" + hex.EncodeToString(h.Sum(nil))

	fullDescription := format.MarkdownDescription(event)
	sarifSeverity := getSarifSeverity(event)

	var ruleName string
	if s, ok := event.Info["name"]; ok {
		ruleName = s.(string)
	}

	var templateURL string
	if strings.HasPrefix(event.TemplatePath, i.home) {
		templateURL = "https://github.com/projectdiscovery/nuclei-templates/blob/master" + templatePath
	}

	var ruleDescription string
	if d, ok := event.Info["description"]; ok {
		ruleDescription = d.(string)
	}

	i.mutex.Lock()
	defer i.mutex.Unlock()

	_ = i.run.AddRule(templateID).
		WithDescription(ruleName).
		WithHelp(fullDescription).
		WithHelpURI(templateURL).
		WithFullDescription(sarif.NewMultiformatMessageString(ruleDescription))
	_ = i.run.AddResult(templateID).
		WithMessage(sarif.NewMessage().WithText(event.Host)).
		WithLevel(sarifSeverity).
		WithLocation(sarif.NewLocation().WithMessage(sarif.NewMessage().WithText(event.Host)).WithPhysicalLocation(
			sarif.NewPhysicalLocation().
				WithArtifactLocation(sarif.NewArtifactLocation().WithUri(os.Getenv("github.action_path"))).
				WithRegion(sarif.NewRegion().WithStartColumn(1).WithStartLine(1).WithEndLine(1).WithEndColumn(1)),
		))
	return nil
}

// getSarifSeverity returns the sarif severity
func getSarifSeverity(event *output.ResultEvent) string {
	var ruleSeverity string
	if s, ok := event.Info["severity"]; ok {
		ruleSeverity = s.(string)
	}

	switch ruleSeverity {
	case "info":
		return "note"
	case "low", "medium":
		return "warning"
	case "high", "critical":
		return "error"
	default:
		return "note"
	}
}

// Close closes the exporter after operation
func (i *Exporter) Close() error {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.sarif.AddRun(i.run)
	if len(i.run.Results) == 0 {
		return nil // do not write when no results
	}
	file, err := os.Create(i.options.File)
	if err != nil {
		return errors.Wrap(err, "could not create sarif output file")
	}
	defer file.Close()
	return i.sarif.Write(file)
}
