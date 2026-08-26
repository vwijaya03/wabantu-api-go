package auth

import "net/http"

// ServeRegisterHTTP runs POST /api/v1/auth/register (integration smoke tests).
func ServeRegisterHTTP(w http.ResponseWriter, req *http.Request) {
	serveRegister(w, req)
}

// ServeLoginHTTP runs POST /api/v1/auth/login (integration smoke tests).
func ServeLoginHTTP(w http.ResponseWriter, req *http.Request) {
	serveLogin(w, req)
}

// ServeMeHTTP runs GET /api/v1/auth/me (integration smoke tests).
func ServeMeHTTP(w http.ResponseWriter, req *http.Request) {
	serveMe(w, req)
}
