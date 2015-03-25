// Package browse provides middleware for listing files in a directory
// when directory path is requested instead of a specific file.
package browse

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mholt/caddy/middleware"
)

// Browse is an http.Handler that can show a file listing when
// directories in the given paths are specified.
type Browse struct {
	Next    http.HandlerFunc
	Root    string
	Configs []BrowseConfig
}

// BrowseConfig is a configuration for browsing in a particular path.
type BrowseConfig struct {
	PathScope string
	Template  *template.Template
}

// A Listing is used to fill out a template.
type Listing struct {
	// The name of the directory (the last element of the path)
	Name string

	// The full path of the request
	Path string

	// Whether the parent directory is browsable
	CanGoUp bool

	// The items (files and folders) in the path
	Items []FileInfo
}

// FileInfo is the info about a particular file or directory
type FileInfo struct {
	IsDir   bool
	Name    string
	Size    int64
	URL     string
	ModTime time.Time
	Mode    os.FileMode
}

var IndexPages = []string{
	"index.html",
	"index.htm",
	"default.html",
	"default.htm",
}

// New creates a new instance of browse middleware.
func New(c middleware.Controller) (middleware.Middleware, error) {
	configs, err := parse(c)
	if err != nil {
		return nil, err
	}

	browse := Browse{
		Root:    c.Root(),
		Configs: configs,
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		browse.Next = next
		return browse.ServeHTTP
	}, nil
}

// ServeHTTP implements the http.Handler interface.
func (b Browse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := b.Root + r.URL.Path

	info, err := os.Stat(filename)
	if err != nil {
		// TODO: 404 Not Found
		b.Next(w, r)
		return
	}

	if !info.IsDir() {
		b.Next(w, r)
		return
	}

	// See if there's a browse configuration to match the path
	for _, bc := range b.Configs {
		if !middleware.Path(r.URL.Path).Matches(bc.PathScope) {
			continue
		}

		// Browsing navigation gets messed up if browsing a directory
		// that doesn't end in "/" (which it should, anyway)
		if r.URL.Path[len(r.URL.Path)-1] != '/' {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusTemporaryRedirect)
			return
		}

		// Load directory contents
		file, err := os.Open(b.Root + r.URL.Path)
		if err != nil {
			panic(err) // TODO
		}
		defer file.Close()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		files, err := file.Readdir(-1)
		if err != nil || len(files) == 0 {
			// TODO - second condition may not be necessary? See docs...
		}

		// Assemble listing of directory contents
		var fileinfos []FileInfo
		var abort bool // we bail early if we find an index file
		for _, f := range files {
			name := f.Name()

			// Directory is not browseable if it contains index file
			for _, indexName := range IndexPages {
				if name == indexName {
					abort = true
					break
				}
			}
			if abort {
				break
			}

			if f.IsDir() {
				name += "/"
			}
			url := url.URL{Path: name}

			fileinfos = append(fileinfos, FileInfo{
				IsDir:   f.IsDir(),
				Name:    f.Name(),
				Size:    f.Size(),
				URL:     url.String(),
				ModTime: f.ModTime(),
				Mode:    f.Mode(),
			})
		}
		if abort {
			// this dir has an index file, so not browsable
			continue
		}

		// Determine if user can browse up another folder
		var canGoUp bool
		curPath := strings.TrimSuffix(r.URL.Path, "/")
		for _, other := range b.Configs {
			if strings.HasPrefix(path.Dir(curPath), other.PathScope) {
				canGoUp = true
				break
			}
		}

		listing := Listing{
			Name:    path.Base(r.URL.Path),
			Path:    r.URL.Path,
			CanGoUp: canGoUp,
			Items:   fileinfos,
		}

		err = bc.Template.Execute(w, listing)
		if err != nil {
			panic(err) // TODO
		}

		return
	}

	// Didn't qualify; pass-thru
	b.Next(w, r)
}

// parse returns a list of browsing configurations
func parse(c middleware.Controller) ([]BrowseConfig, error) {
	var configs []BrowseConfig

	appendCfg := func(bc BrowseConfig) error {
		for _, c := range configs {
			if c.PathScope == bc.PathScope {
				return fmt.Errorf("Duplicate browsing config for %s", c.PathScope)
			}
		}
		configs = append(configs, bc)
		return nil
	}

	for c.Next() {
		var bc BrowseConfig

		if !c.NextArg() {
			bc.PathScope = "/"
			err := appendCfg(bc)
			if err != nil {
				return configs, err
			}
			continue
		}

		bc.PathScope = c.Val()

		if !c.NextArg() {
			err := appendCfg(bc)
			if err != nil {
				return configs, err
			}
			continue
		}

		tplFile := c.Val()
		var tplText string

		if tplFile != "" {
			tplBytes, err := ioutil.ReadFile(tplFile)
			if err != nil {
				return configs, err
			}
			tplText = string(tplBytes)
		} else {
			tplText = defaultTemplate
		}

		tpl, err := template.New("listing").Parse(tplText)
		if err != nil {
			return configs, err
		}
		bc.Template = tpl

		err = appendCfg(bc)
		if err != nil {
			return configs, err
		}
	}

	return configs, nil
}

const defaultTemplate = `
{{range .}}
	{{.Name}}<br>
{{end}}
`