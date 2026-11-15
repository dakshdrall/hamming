package harness

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// boolParams gives the argument for each known bool parameter of wallet-backend's NewIndexer. A bool whose
// name is not listed is an error: its meaning has to be read and decided, not guessed.
var boolParams = map[string]string{
	// Added between 2c87335 and 3e22515. Production enables it for live ingestion when a protocol processor
	// needs contract data (internal/services/ingest.go, collectsContractData). The harness enables it so the
	// replay exercises the larger code path.
	"collectContractData": "true",
}

// NewIndexerCall reads the NewIndexer declaration in the Go package at pkgDir and returns the call the
// adapter makes, choosing one argument per parameter from its type: string -> pass (the entry's passphrase),
// pond.Pool -> pool, any pointer -> nil, bool -> boolParams. The adapter declares pass and pool.
func NewIndexerCall(pkgDir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return "", err
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "NewIndexer" {
				continue
			}
			args, err := newIndexerArgs(fn.Type.Params)
			if err != nil {
				return "", fmt.Errorf("%s: %w", fset.Position(fn.Pos()), err)
			}
			return "NewIndexer(" + strings.Join(args, ", ") + ")", nil
		}
	}
	return "", fmt.Errorf("no func NewIndexer in %s", pkgDir)
}

func newIndexerArgs(params *ast.FieldList) ([]string, error) {
	var args []string
	for _, field := range params.List {
		typ := types.ExprString(field.Type)
		names := field.Names
		if len(names) == 0 {
			names = []*ast.Ident{{Name: "_"}}
		}
		for _, name := range names {
			var arg string
			switch {
			case typ == "string":
				arg = "pass"
			case typ == "pond.Pool":
				arg = "pool"
			case strings.HasPrefix(typ, "*"):
				arg = "nil"
			case typ == "bool":
				v, ok := boolParams[name.Name]
				if !ok {
					return nil, fmt.Errorf("NewIndexer has an unknown bool parameter %q; decide its value and add it to boolParams", name.Name)
				}
				arg = v
			default:
				return nil, fmt.Errorf("NewIndexer parameter %s has type %s, which the adapter does not know how to supply", name.Name, typ)
			}
			args = append(args, arg)
		}
	}
	return args, nil
}
