package destregistrydefault_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The registry drops config and credential keys a provider does not declare
// in its metadata or allow in code. These tests check that every key a
// provider reads is kept, and that nothing else survives a write.

// providerDirs maps a provider's source directory to its destination type.
var providerDirs = map[string]string{
	"destwebhook":         "webhook",
	"desthookdeck":        "hookdeck",
	"destawssqs":          "aws_sqs",
	"destawss3":           "aws_s3",
	"destawseventbridge":  "aws_eventbridge",
	"destawskinesis":      "aws_kinesis",
	"destgcppubsub":       "gcp_pubsub",
	"destazureservicebus": "azure_servicebus",
	"destcfqueues":        "cloudflare_queues",
	"destkafka":           "kafka",
	"destrabbitmq":        "rabbitmq",
	"destmcp":             "mcp",
}

// requestOnlyKeys are read from a request and consumed by Preprocess, so they
// are never stored and need not be known to the provider.
var requestOnlyKeys = map[string]map[string]bool{
	"webhook": {"credentials.rotate_secret": true},
}

type keyProvider interface {
	ConfigKeys() []string
	CredentialKeys() []string
}

// scanKeys returns the config and credential keys read in the non-test source
// of the package in dir, as "config.<key>" / "credentials.<key>".
//
// It finds index expressions on a `.Config` or `.Credentials` selector, or on
// a variable assigned directly from one in the same function. A key that is
// not a string literal or a string constant of the package or destregistry is
// returned as unresolved and fails the test unless listed in
// unresolvedAllowed after a check by hand. It misses keys reached without such
// an index: through a helper that takes the map as a parameter, a local map
// later assigned to Config or Credentials, or a map that is copied, cloned,
// ranged over or passed to another function.
func scanKeys(t *testing.T, dir string) (found map[string]bool, unresolved []string) {
	t.Helper()
	fset := token.NewFileSet()
	files := parseDir(t, fset, dir)

	consts := packageConsts(files)
	for name, value := range packageConsts(parseDir(t, fset, "..")) {
		consts["destregistry."+name] = value
	}

	found = map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			aliases := mapAliases(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.IndexExpr:
					kind := mapKind(n.X, aliases)
					if kind == "" {
						return true
					}
					if key, ok := constString(n.Index, consts); ok {
						found[kind+"."+key] = true
					} else {
						unresolved = append(unresolved, fn.Name.Name+": "+kind+"["+types.ExprString(n.Index)+"]")
					}
				case *ast.CallExpr:
					if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ValidateCustomEndpointAllowed" {
						found["config."+destregistry.ConfigKeyEndpoint] = true
					}
				}
				return true
			})
		}
	}
	return found, unresolved
}

func parseDir(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	require.NotEmpty(t, files, dir)
	return files
}

func packageConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							consts[name.Name] = v
						}
					}
				}
			}
		}
	}
	return consts
}

// mapKind returns "config" or "credentials" when expr is that map of a
// destination, directly or through a tracked alias.
func mapKind(expr ast.Expr, aliases map[string]string) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		switch e.Sel.Name {
		case "Config":
			return "config"
		case "Credentials":
			return "credentials"
		}
	case *ast.Ident:
		return aliases[e.Name]
	}
	return ""
}

// mapAliases finds identifiers assigned from a destination's Config or
// Credentials within fn.
func mapAliases(fn *ast.FuncDecl) map[string]string {
	aliases := map[string]string{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				return true
			}
			for i, lhs := range n.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if sel, ok := n.Rhs[i].(*ast.SelectorExpr); ok {
					if kind := mapKind(sel, nil); kind != "" {
						aliases[id.Name] = kind
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if i >= len(n.Values) {
					continue
				}
				if sel, ok := n.Values[i].(*ast.SelectorExpr); ok {
					if kind := mapKind(sel, nil); kind != "" {
						aliases[id.Name] = kind
					}
				}
			}
		}
		return true
	})
	return aliases
}

func constString(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			v, err := strconv.Unquote(e.Value)
			return v, err == nil
		}
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			v, ok := consts[pkg.Name+"."+e.Sel.Name]
			return v, ok
		}
	}
	return "", false
}

func newKeysTestRegistry(t *testing.T, allowCustomEndpoints bool) destregistry.Registry {
	t.Helper()
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge:       &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		MCP:                  &destmcp.Config{Client: &http.Client{}},
		AllowCustomEndpoints: allowCustomEndpoints,
	}))
	return registry
}

func providerKeys(t *testing.T, registry destregistry.Registry, destType string) keyProvider {
	t.Helper()
	provider, err := registry.ResolveProvider(&models.Destination{Type: destType})
	require.NoError(t, err)
	kp, ok := provider.(keyProvider)
	require.True(t, ok, destType)
	return kp
}

// unresolvedAllowed lists index keys the scan cannot resolve, checked by hand.
// In destwebhook, ObfuscateDestination copies every stored key, and
// updateSecret loops over secret, previous_secret and
// previous_secret_invalid_at, all allowed credential keys.
var unresolvedAllowed = map[string]map[string]bool{
	"destwebhook": {
		"ObfuscateDestination: config[key]":      true,
		"ObfuscateDestination: credentials[key]": true,
		"updateSecret: credentials[key]":         true,
	},
}

func TestProviders_EveryProviderDirIsScanned(t *testing.T) {
	dirs, err := filepath.Glob("dest*")
	require.NoError(t, err)
	require.NotEmpty(t, dirs)
	for _, dir := range dirs {
		assert.Contains(t, providerDirs, dir, "add %s to providerDirs and keyFixtures", dir)
	}
}

func TestProviders_ReadKeysAreKnown(t *testing.T) {
	scanned := map[string]map[string]bool{}
	for dir, destType := range providerDirs {
		found, unresolved := scanKeys(t, dir)
		scanned[destType] = found
		for _, u := range unresolved {
			assert.True(t, unresolvedAllowed[dir][u], "%s: index key cannot be resolved by the scan: %s", dir, u)
		}
	}

	// A scanner that finds nothing would pass every check below.
	assert.True(t, scanned["hookdeck"]["config.source_id"])
	assert.True(t, scanned["hookdeck"]["config.source_name"])
	assert.True(t, scanned["aws_sqs"]["credentials.session"])
	assert.True(t, scanned["aws_s3"]["config.endpoint"])
	assert.True(t, scanned["webhook"]["credentials.secret"])
	assert.True(t, scanned["mcp"]["config.url"])
	assert.True(t, scanned["kafka"]["credentials.username"])

	for _, allowCustomEndpoints := range []bool{false, true} {
		registry := newKeysTestRegistry(t, allowCustomEndpoints)
		for destType, keys := range scanned {
			provider := providerKeys(t, registry, destType)
			known := map[string]bool{}
			for _, k := range provider.ConfigKeys() {
				known["config."+k] = true
			}
			for _, k := range provider.CredentialKeys() {
				known["credentials."+k] = true
			}
			for key := range keys {
				if requestOnlyKeys[destType][key] {
					continue
				}
				assert.True(t, known[key], "%s reads %s but does not declare or allow it (custom endpoints allowed: %v)", destType, key, allowCustomEndpoints)
			}
		}
	}
}
