// apisettings package implements internal structs which configure the HTTP api.

package apisettings

import (
	"path/filepath"
)

const (
// ReverseExporterLatestApi gives the current latest API string.
//ReverseExporterLatestApi = "v1"
)

// APISettings holds http API specific parameters used to configure the web API.
type APISettings struct {
	// ContextPath is any URL-prefix being passed by a reverse proxy.
	ContextPath string
}

// WrapPath wraps a given URL string in the context path.
func (api *APISettings) WrapPath(path string) string {
	return filepath.Join(api.ContextPath, path)
}
