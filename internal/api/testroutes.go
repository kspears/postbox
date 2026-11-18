//go:build !citest

package api

import "net/http"

// testRoutes adds nothing to a release build. See testroutes_citest.go.
func testRoutes(*http.ServeMux, *apiHandler) {}
