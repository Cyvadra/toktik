package dslscripts

import (
	"embed"
	"fmt"
)

//go:embed strategies/*.dsl
var files embed.FS

func ReadStrategy(name string) (string, error) {
	data, err := files.ReadFile("strategies/" + name)
	if err != nil {
		return "", fmt.Errorf("read DSL strategy %q: %w", name, err)
	}
	return string(data), nil
}
