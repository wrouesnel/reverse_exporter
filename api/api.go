package api

import (
	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
)

// Appends a new API to the supplied router.
func NewAPI_v1(settings apisettings.APISettings, router *httprouter.Router) *httprouter.Router {
	// Connect setup
	return router
}
