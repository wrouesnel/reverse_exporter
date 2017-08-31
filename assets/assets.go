package assets

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"path"
	"strings"

	"github.com/eknkc/amber"
	"github.com/elazarl/go-bindata-assetfs"
	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/go.log"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
)

const (
	templateDir = "templates"
	defaultPage = "index.html"
)

var assetFilesystem = &assetfs.AssetFS{Asset: Asset, AssetDir: AssetDir, Prefix: ""}

// Appends a new static files API to the supplied router
func StaticFiles(settings apisettings.APISettings, router *httprouter.Router) *httprouter.Router {
	// Static asset handling
	if settings.StaticProxy != nil {
		log.Infoln("Proxying static assets from", settings.StaticProxy)
		revProxy := httputil.NewSingleHostReverseProxy(settings.StaticProxy)
		router.Handler("GET", settings.WrapPath("/static/*filepath"), revProxy)
	} else {
		router.Handler("GET", settings.WrapPath("/static/*filepath"),
			http.StripPrefix(settings.ContextPath, http.FileServer(assetFilesystem)))
	}

	// Templates are the root of everything
	templateList, err := AssetDir(templateDir)
	if err != nil {
		panic(err)
	}

	// TODO: we need a library for this.
	for _, name := range templateList {
		templatePath := path.Join(templateDir, name)
		log.Debugln("Compiling template:", templatePath)

		templateContent, err := Asset(templatePath)
		if err != nil {
			log.Panicln("Template not found:", templatePath)
		}

		tmpl := amber.MustCompile(string(templateContent), amber.Options{PrettyPrint: true})

		executedPath := fmt.Sprintf("%s.html", strings.TrimSuffix(name, path.Ext(name)))
		templateFullPath := settings.WrapPath("/" + executedPath)

		log.Debugln("Register template:", templateFullPath)
		router.GET(templateFullPath, templateRenderer(name, tmpl, settings))

		if executedPath == defaultPage {
			router.GET(settings.WrapPath("/"), templateRenderer(name, tmpl, settings))
		}
	}

	return router
}

func templateRenderer(name string, tmpl *template.Template, data interface{}) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		err := tmpl.Execute(w, data)
		if err != nil {
			log.Errorln("Error executing template:", name)
		}
	}
}
