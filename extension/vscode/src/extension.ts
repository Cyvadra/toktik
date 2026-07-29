import * as vscode from 'vscode';

type BuiltinItem = {
  name: string;
  kind: 'function' | 'property' | 'constant';
  params?: string[];
  signature: string;
  summary?: string;
  example?: string;
  returnValue?: string;
  snippet: string;
};

type DSLCompletionData = {
  keywords: string[];
  builtins: BuiltinItem[];
};

export async function activate(context: vscode.ExtensionContext) {
  const data = await loadCompletionData(context);
  const completionProvider = vscode.languages.registerCompletionItemProvider(
    { language: 'toktik-dsl' },
    {
      provideCompletionItems() {
        return [
          ...data.keywords.map(keywordCompletion),
          ...data.builtins.map(builtinCompletion),
        ];
      },
    },
    '.',
  );
  const builtinsByName = new Map(data.builtins.map(builtin => [builtin.name, builtin]));
  const hoverProvider = vscode.languages.registerHoverProvider(
    { language: 'toktik-dsl' },
    {
      provideHover(document, position) {
        const range = document.getWordRangeAtPosition(
          position,
          /[\p{L}_][\p{L}\p{N}_]*(?:\s*\.\s*[\p{L}_][\p{L}\p{N}_]*)*/u,
        );
        if (!range) {
          return undefined;
        }

        const name = document.getText(range).replace(/\s*\.\s*/g, '.');
        const builtin = builtinsByName.get(name);
        return builtin ? new vscode.Hover(documentation(builtin), range) : undefined;
      },
    },
  );

  context.subscriptions.push(completionProvider, hoverProvider);
}

export function deactivate() {}

async function loadCompletionData(context: vscode.ExtensionContext): Promise<DSLCompletionData> {
  const fileUri = vscode.Uri.joinPath(context.extensionUri, 'data', 'dsl-builtins.json');
  const data = await vscode.workspace.fs.readFile(fileUri);
  return JSON.parse(Buffer.from(data).toString('utf8')) as DSLCompletionData;
}

function keywordCompletion(keyword: string): vscode.CompletionItem {
  const item = new vscode.CompletionItem(keyword, vscode.CompletionItemKind.Keyword);
  item.detail = 'Toktik DSL keyword';
  return item;
}

function builtinCompletion(builtin: BuiltinItem): vscode.CompletionItem {
  const item = new vscode.CompletionItem(builtin.name, completionKind(builtin.kind));
  item.detail = builtin.signature;
  item.documentation = documentation(builtin);
  item.insertText = new vscode.SnippetString(builtin.snippet);
  return item;
}

function completionKind(kind: BuiltinItem['kind']): vscode.CompletionItemKind {
  switch (kind) {
    case 'property':
      return vscode.CompletionItemKind.Property;
    case 'constant':
      return vscode.CompletionItemKind.Constant;
    default:
      return vscode.CompletionItemKind.Function;
  }
}

function documentation(builtin: BuiltinItem): vscode.MarkdownString {
  const markdown = new vscode.MarkdownString(undefined, true);
  markdown.appendCodeblock(builtin.signature, 'toktik-dsl');
  if (builtin.returnValue) {
    markdown.appendMarkdown(`\nReturns: ${builtin.returnValue}\n`);
  }
  if (builtin.summary) {
    markdown.appendMarkdown(`\n${builtin.summary}\n`);
  }
  if (builtin.example) {
    markdown.appendMarkdown('\nExample:\n');
    markdown.appendCodeblock(builtin.example, 'toktik-dsl');
  }
  return markdown;
}