package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type Chunk struct {
	Repo       string
	Path       string      // relative to repo root
	Package    string
	Symbol     string      // e.g. "UserService.CreateUser"
	Kind       string      // "func", "method", "type", "const", "var"
	Signature  string
	Doc        string
	Body       string
	StartLine  int
	EndLine    int
	Imports    []string
	Hash       string      // sha256 of Body
}

// Text returns the string that will be embedded. 
// Enrichment matters more than you'd think.
func (c *Chunk) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", c.Package)
	fmt.Fprintf(&b, "file: %s\n", c.Path)
	fmt.Fprintf(&b, "symbol: %s (%s)\n", c.Symbol, c.Kind)
	if c.Doc != "" {
		fmt.Fprintf(&b, "doc: %s\n", strings.TrimSpace(c.Doc))
	}
	if len(c.Imports) > 0 && len(c.Imports) < 15 {
		fmt.Fprintf(&b, "imports: %s\n", strings.Join(c.Imports, ", "))
	}
	fmt.Fprintf(&b, "---\n%s\n", c.Body)
	return b.String()
}

// StableID produces a deterministic UUID-like string for Qdrant.
func (c *Chunk) StableID() string {
	h := sha256.Sum256([]byte(c.Repo + "::" + c.Path + "::" + c.Symbol))
	// Qdrant accepts UUID or uint64. Use UUID formatted hex.
	hx := hex.EncodeToString(h[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hx[0:8], hx[8:12], hx[12:16], hx[16:20], hx[20:32])
}

func ChunkFile(repo, absPath, relPath string) ([]Chunk, error) {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relPath, err)
	}

	imports := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		imports = append(imports, strings.Trim(imp.Path.Value, `"`))
	}

	pkg := file.Name.Name
	var chunks []Chunk

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			chunks = append(chunks, funcChunk(repo, relPath, pkg, imports, fset, src, d))
		case *ast.GenDecl:
			chunks = append(chunks, genDeclChunks(repo, relPath, pkg, imports, fset, src, d)...)
		}
	}
	return chunks, nil
}

func funcChunk(repo, path, pkg string, imports []string,
	fset *token.FileSet, src []byte, fn *ast.FuncDecl) Chunk {

	start := fset.Position(fn.Pos())
	end := fset.Position(fn.End())
	body := string(src[start.Offset:end.Offset])

	symbol := fn.Name.Name
	kind := "func"
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = "method"
		recvType := exprString(fn.Recv.List[0].Type)
		symbol = recvType + "." + fn.Name.Name
	}

	doc := ""
	if fn.Doc != nil {
		doc = fn.Doc.Text()
	}

	sig := extractSignature(body)

	h := sha256.Sum256([]byte(body))

	return Chunk{
		Repo:      repo,
		Path:      path,
		Package:   pkg,
		Symbol:    symbol,
		Kind:      kind,
		Signature: sig,
		Doc:       doc,
		Body:      body,
		StartLine: start.Line,
		EndLine:   end.Line,
		Imports:   imports,
		Hash:      hex.EncodeToString(h[:]),
	}
}

func genDeclChunks(repo, path, pkg string, imports []string,
	fset *token.FileSet, src []byte, gd *ast.GenDecl) []Chunk {

	var out []Chunk
	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			start := fset.Position(s.Pos())
			end := fset.Position(s.End())
			body := string(src[start.Offset:end.Offset])
			doc := ""
			if gd.Doc != nil {
				doc = gd.Doc.Text()
			} else if s.Doc != nil {
				doc = s.Doc.Text()
			}
			h := sha256.Sum256([]byte(body))
			out = append(out, Chunk{
				Repo:      repo,
				Path:      path,
				Package:   pkg,
				Symbol:    s.Name.Name,
				Kind:      "type",
				Doc:       doc,
				Body:      "type " + body,
				StartLine: start.Line,
				EndLine:   end.Line,
				Imports:   imports,
				Hash:      hex.EncodeToString(h[:]),
			})
		}
	}
	return out
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.IndexExpr: // generic receivers
		return exprString(x.X)
	}
	return "unknown"
}

func extractSignature(body string) string {
	if i := strings.Index(body, "{"); i > 0 {
		return strings.TrimSpace(body[:i])
	}
	return ""
}