package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/lexer"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
	"github.com/Cyvadra/toktik/pkg/dsl/token"
)

type completionItem struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Params      []string `json:"params,omitempty"`
	Signature   string   `json:"signature"`
	Summary     string   `json:"summary,omitempty"`
	Example     string   `json:"example,omitempty"`
	ReturnValue string   `json:"returnValue,omitempty"`
	Snippet     string   `json:"snippet"`
}

type completionData struct {
	GeneratedBy string               `json:"generatedBy"`
	Profile     string               `json:"profile"`
	Keywords    []string             `json:"keywords"`
	LexicalDocs []lexer.LexicalDoc   `json:"lexicalDocs"`
	Builtins    []completionItem     `json:"builtins"`
	Namespaces  map[string][]string  `json:"namespaces"`
	Counts      map[string]int       `json:"counts"`
	RawDocs     []runtime.BuiltinDoc `json:"rawDocs"`
}

func main() {
	outputDir := flag.String("output", "extension/vscode", "VS Code extension directory to update")
	flag.Parse()

	docs := runtime.BuiltinDocs(runtime.ProfileBacktest)
	keywords := token.Keywords()
	items := make([]completionItem, 0, len(docs))
	namespaces := make(map[string][]string)
	counts := make(map[string]int)

	for _, doc := range docs {
		item := completionItem{
			Name:        doc.Name,
			Kind:        string(doc.Kind),
			Params:      append([]string(nil), doc.Params...),
			Signature:   builtinSignature(doc),
			Summary:     doc.Summary,
			Example:     doc.Example,
			ReturnValue: doc.ReturnValue,
			Snippet:     builtinSnippet(doc),
		}
		items = append(items, item)
		ns := namespace(doc.Name)
		namespaces[ns] = append(namespaces[ns], doc.Name)
		counts[ns]++
	}

	for ns := range namespaces {
		sort.Strings(namespaces[ns])
	}

	data := completionData{
		GeneratedBy: "go run ./cmd/vscode-dsl-extension-data",
		Profile:     string(runtime.ProfileBacktest),
		Keywords:    keywords,
		LexicalDocs: lexer.LexicalDocs(),
		Builtins:    items,
		Namespaces:  namespaces,
		Counts:      counts,
		RawDocs:     docs,
	}

	if err := writeJSON(filepath.Join(*outputDir, "data", "dsl-builtins.json"), data); err != nil {
		fatal(err)
	}
	if err := writeJSON(filepath.Join(*outputDir, "syntaxes", "toktik-dsl.tmLanguage.json"), grammar(keywords, docs)); err != nil {
		fatal(err)
	}
	if err := writeJSON(filepath.Join(*outputDir, "snippets", "toktik-dsl.code-snippets"), snippets(docs)); err != nil {
		fatal(err)
	}
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func grammar(keywords []string, docs []runtime.BuiltinDoc) map[string]any {
	namespaces := make([]string, 0)
	seenNamespace := make(map[string]bool)
	builtinNames := make([]string, 0, len(docs))
	properties := make([]string, 0)
	constants := make([]string, 0)

	for _, doc := range docs {
		builtinNames = append(builtinNames, doc.Name)
		if doc.Kind == runtime.BuiltinProperty || doc.Kind == runtime.BuiltinConstant {
			leaf := doc.Name
			if idx := strings.LastIndexByte(doc.Name, '.'); idx >= 0 {
				leaf = doc.Name[idx+1:]
			}
			if doc.Kind == runtime.BuiltinProperty {
				properties = append(properties, leaf)
			} else {
				constants = append(constants, leaf)
			}
		}
		if ns := namespace(doc.Name); ns != "core" && !seenNamespace[ns] {
			seenNamespace[ns] = true
			namespaces = append(namespaces, ns)
		}
	}
	sort.Strings(namespaces)
	sort.Strings(builtinNames)
	sort.Strings(properties)
	sort.Strings(constants)

	return map[string]any{
		"$schema":   "https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json",
		"name":      "Toktik DSL",
		"scopeName": "source.toktik-dsl",
		"fileTypes": []string{"toktik", "tdsl"},
		"patterns": []map[string]any{
			{"include": "#comments"},
			{"include": "#strings"},
			{"include": "#numbers"},
			{"include": "#declarations"},
			{"include": "#builtins"},
			{"include": "#keywords"},
			{"include": "#operators"},
		},
		"repository": map[string]any{
			"comments": map[string]any{
				"patterns": []map[string]any{
					{"name": "comment.line.double-slash.toktik-dsl", "match": "//.*$"},
					{"name": "comment.block.toktik-dsl", "begin": "/\\*", "end": "\\*/"},
				},
			},
			"strings": map[string]any{
				"patterns": []map[string]any{
					{"name": "string.quoted.double.toktik-dsl", "begin": "\"", "end": "\"", "patterns": []map[string]string{{"name": "constant.character.escape.toktik-dsl", "match": "\\\\[ntr\\\\\"']"}}},
					{"name": "string.quoted.single.toktik-dsl", "begin": "'", "end": "'", "patterns": []map[string]string{{"name": "constant.character.escape.toktik-dsl", "match": "\\\\[ntr\\\\\"']"}}},
				},
			},
			"numbers": map[string]string{"name": "constant.numeric.toktik-dsl", "match": "\\b(?:\\d+\\.\\d*|\\.\\d+|\\d+)(?:[eE][+-]?\\d+)?\\b"},
			"declarations": map[string]any{
				"patterns": []map[string]string{
					{"name": "meta.declaration.toktik-dsl", "match": "\\b(?:strategy|indicator|library)\\s*\\(\\s*([\"']).*?\\1"},
					{"name": "storage.modifier.toktik-dsl", "match": "\\b(?:var|varip|export|method)\\b"},
					{"name": "storage.type.toktik-dsl", "match": "\\b(?:int|float|bool|string|color|series|simple|const)\\b"},
				},
			},
			"keywords": map[string]string{"name": "keyword.control.toktik-dsl", "match": wordPattern(keywords)},
			"builtins": map[string]any{
				"patterns": []map[string]string{
					{"name": "support.namespace.toktik-dsl", "match": wordPattern(namespaces)},
					{"name": "support.function.toktik-dsl", "match": builtinPattern(builtinNames)},
					{"name": "support.variable.property.toktik-dsl", "match": wordPattern(properties)},
					{"name": "constant.language.toktik-dsl", "match": wordPattern(constants)},
				},
			},
			"operators": map[string]string{"name": "keyword.operator.toktik-dsl", "match": "=>|:=|\\+\\+|--|[+\\-*/%]=?|==|!=|<=|>=|[<>?:=]"},
		},
	}
}

func snippets(docs []runtime.BuiltinDoc) map[string]any {
	result := make(map[string]any, len(docs)+2)
	result["strategy template"] = map[string]any{
		"prefix":      "strategy",
		"body":        []string{"//@version=6", "strategy(\"${1:Strategy Name}\")", "", "${0}"},
		"description": "Create a Toktik DSL strategy header",
	}
	result["if block"] = map[string]any{
		"prefix":      "if",
		"body":        []string{"if ${1:condition}", "    ${0}"},
		"description": "Create an indented if block",
	}
	for _, doc := range docs {
		if doc.Kind != runtime.BuiltinFunction {
			continue
		}
		result[doc.Name] = map[string]any{
			"prefix":      doc.Name,
			"body":        builtinSnippet(doc),
			"description": doc.Summary,
		}
	}
	return result
}

func builtinSignature(doc runtime.BuiltinDoc) string {
	if doc.Kind == runtime.BuiltinProperty || doc.Kind == runtime.BuiltinConstant {
		return doc.Name
	}
	return doc.Name + "(" + strings.Join(doc.Params, ", ") + ")"
}

func builtinSnippet(doc runtime.BuiltinDoc) string {
	if doc.Kind == runtime.BuiltinProperty || doc.Kind == runtime.BuiltinConstant {
		return doc.Name
	}
	parts := make([]string, 0, len(doc.Params))
	for i, param := range doc.Params {
		parts = append(parts, fmt.Sprintf("${%d:%s}", i+1, param))
	}
	return doc.Name + "(" + strings.Join(parts, ", ") + ")"
}

func namespace(name string) string {
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		return name[:idx]
	}
	return "core"
}

func wordPattern(words []string) string {
	if len(words) == 0 {
		return "(?!)"
	}
	escaped := make([]string, 0, len(words))
	for _, word := range words {
		escaped = append(escaped, regexp.QuoteMeta(word))
	}
	return "\\b(?:" + strings.Join(escaped, "|") + ")\\b"
}

func builtinPattern(words []string) string {
	if len(words) == 0 {
		return "(?!)"
	}
	escaped := make([]string, 0, len(words))
	for _, word := range words {
		escaped = append(escaped, strings.ReplaceAll(regexp.QuoteMeta(word), "\\.", "\\s*\\.\\s*"))
	}
	return "\\b(?:" + strings.Join(escaped, "|") + ")\\b"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
