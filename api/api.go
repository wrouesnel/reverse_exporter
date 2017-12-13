package api

import (
	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
)

// NewAPIv1 appends a new API to the supplied router and returns the router.
func NewAPIv1(settings apisettings.APISettings, router *httprouter.Router) *httprouter.Router {
	// Connect setup
	return router
}
