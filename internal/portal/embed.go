package portal

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

//go:embed dist
var staticFS embed.FS

type PortalConfig struct {
	ProxyURL string
}

// AddRoutes serves the static file system for the UI React App.
func AddRoutes(router *gin.Engine, config PortalConfig) {
	if config.ProxyURL != "" {
		// Proxy to Portal server
		remote, err := url.Parse(config.ProxyURL)
		if err != nil {
			panic(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(remote)
		router.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.Next()
				return
			}
			if c.Request.Method != "GET" {
				c.Next()
				return
			}
			log.Println(c.Request.Method, c.Request.URL.Path, c.Request.URL.RawQuery)
			proxy.ServeHTTP(c.Writer, c.Request)
		})
	} else {
		embeddedBuildFolder := newStaticFileSystem()
		fallbackFileSystem := newFallbackFileSystem(embeddedBuildFolder)
		router.Use(static.Serve("/", embeddedBuildFolder))
		router.Use(static.Serve("/", fallbackFileSystem))
	}
}

// staticFileSystem serves files out of the embedded build folder
type staticFileSystem struct {
	http.FileSystem
}

var _ static.ServeFileSystem = (*staticFileSystem)(nil)

func newStaticFileSystem() *staticFileSystem {
	sub, err := fs.Sub(staticFS, "dist")

	if err != nil {
		panic(err)
	}

	return &staticFileSystem{
		FileSystem: http.FS(sub),
	}
}

func (s *staticFileSystem) Exists(prefix string, path string) bool {
	buildpath := fmt.Sprintf("dist%s", path)

	// support for folders
	if strings.HasSuffix(path, "/") {
		_, err := staticFS.ReadDir(strings.TrimSuffix(buildpath, "/"))
		return err == nil
	}

	// support for files
	f, err := staticFS.Open(buildpath)
	if f != nil {
		_ = f.Close()
	}
	return err == nil
}

// fallbackFileSystem wraps a staticFileSystem and always serves /index.html
type fallbackFileSystem struct {
	staticFileSystem *staticFileSystem
}

var _ static.ServeFileSystem = (*fallbackFileSystem)(nil)
var _ http.FileSystem = (*fallbackFileSystem)(nil)

func newFallbackFileSystem(staticFileSystem *staticFileSystem) *fallbackFileSystem {
	return &fallbackFileSystem{
		staticFileSystem: staticFileSystem,
	}
}

func (f *fallbackFileSystem) Open(path string) (http.File, error) {
	return f.staticFileSystem.Open("/index.html")
}

func (f *fallbackFileSystem) Exists(prefix string, path string) bool {
	if strings.HasPrefix(path, "/api") {
		return false
	}
	return true
}
