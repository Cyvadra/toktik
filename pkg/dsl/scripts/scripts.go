package dslscripts

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed strategies/*.toktik
var files embed.FS

var strategyFileName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*\.toktik$`)

func ReadStrategy(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !strategyFileName.MatchString(name) {
		return "", fmt.Errorf("invalid DSL strategy file name %q", name)
	}

	data, err := files.ReadFile("strategies/" + name)
	if err != nil {
		return "", fmt.Errorf("read DSL strategy %q: %w", name, err)
	}
	return string(data), nil
}
