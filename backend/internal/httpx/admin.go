package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// RequireAdmin protects every route under pathPrefix with the static admin
// token (SPEC.md §7.5). The prefix check fails closed: any route added under
// /api/admin/ is protected without further wiring. Tokens are compared in
// constant time over their SHA-256 digests, so the comparison does not leak
// the token length either. The token is never logged.
func RequireAdmin(token, pathPrefix string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, pathPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			sum := sha256.Sum256([]byte(got))
			if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
				slog.WarnContext(r.Context(), "admin request rejected",
					"request_id", middleware.GetReqID(r.Context()), "path", r.URL.Path)
				WriteError(w, r, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
