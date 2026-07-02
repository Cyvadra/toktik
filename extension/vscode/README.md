# Toktik DSL VS Code Extension

This extension provides syntax highlighting, snippets, and completion items for Toktik DSL files (`.toktik`, `.tdsl`).

The DSL metadata is generated from the Go implementation rather than maintained by hand:

```bash
go run ./cmd/vscode-dsl-extension-data -output extension/vscode
```

The generator reads lexer keyword data from `pkg/dsl/token`, lexical notes from `pkg/dsl/lexer`, and backtest builtin metadata from `pkg/dsl/runtime.BuiltinDocs(runtime.ProfileBacktest)`.

```bash
npm --prefix extension/vscode install
npm --prefix extension/vscode run compile
```